package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"relay-server/internal/broker"
	"relay-server/internal/model"
	"relay-server/internal/projectmemory"
	"relay-server/internal/store"
)

var (
	projectMemoryPrivateKeyPattern = regexp.MustCompile(`(?s)-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----.*?-----END [A-Z0-9 ]*PRIVATE KEY-----`)
	projectMemoryBearerPattern     = regexp.MustCompile(`(?i)(\bBearer\s+)[A-Za-z0-9._~+/=-]{12,}`)
	projectMemoryJWTPattern        = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\b`)
	projectMemoryCredentialPattern = regexp.MustCompile(`(?i)\b(password|passwd|pwd|token|access_token|api[_-]?key|secret|cookie|session[_-]?key)\b(\s*[:=]\s*["']?)[^\s,"';}]{4,}`)
	projectMemorySHA256Pattern     = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
)

const (
	projectMemoryLeaseDuration   = 5 * time.Minute
	projectMemoryDispatchTick    = 2 * time.Second
	projectMemorySourceLimit     = 200 * 1024
	projectMemoryTaskSourceLimit = 96 * 1024
	projectMemoryTaskEventLimit  = 64
	projectMemoryTaskPageLimit   = 50
	projectMemoryRetention       = 30 * 24 * time.Hour
	projectMemoryMaintenance     = 6 * time.Hour
	projectMemorySchemaCurrent   = 1
	projectMemorySchemaPrevious  = 0
)

func (a *App) projectMemoryLoop() {
	ticker := time.NewTicker(projectMemoryDispatchTick)
	defer ticker.Stop()
	lastMaintenance := time.Time{}
	for range ticker.C {
		a.dispatchProjectMemoryJobs(context.Background())
		if lastMaintenance.IsZero() || time.Since(lastMaintenance) >= projectMemoryMaintenance {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if purged, err := a.store.PurgeDeletedProjectMemories(ctx, time.Now().UTC().Add(-projectMemoryRetention), 1_000); err != nil {
				log.Printf("[project-memory] tombstone maintenance failed: %v", err)
			} else if purged > 0 {
				log.Printf("[project-memory] purged %d expired tombstones", purged)
			}
			if retention, err := a.store.MaintainProjectMemoryRetention(ctx, 20_000, 200_000, 100); err != nil {
				log.Printf("[project-memory] retention maintenance failed: %v", err)
			} else if retention.RolledUp > 0 {
				log.Printf("[project-memory] retention rollups=%d archived=%d purged_history=%d", retention.RolledUp, retention.Archived, retention.PurgedHistory)
			}
			projectmemory.CleanupCache(time.Now().UTC().Add(-30 * time.Minute))
			a.enqueueProjectMemorySourceRechecks(ctx)
			cancel()
			lastMaintenance = time.Now()
		}
	}
}

func (a *App) enqueueProjectMemorySourceRechecks(ctx context.Context) {
	now := time.Now().UTC()
	seen := map[string]struct{}{}
	for _, agent := range a.broker.List() {
		if agent.OperatorID == 0 || agent.MachineID == "" || agent.Kind == "launcher" {
			continue
		}
		for _, project := range agent.Projects {
			scopeID := strings.TrimSpace(project.ScopeID)
			key := fmt.Sprintf("%d:%s", agent.OperatorID, scopeID)
			if scopeID == "" {
				continue
			}
			if !projectmemory.Resolve(ctx, a.store, agent.OperatorID, agent.MachineID, scopeID).Enabled {
				continue
			}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			jobs, err := a.store.ListProjectMemoryJobs(ctx, agent.OperatorID, scopeID, 20)
			if err != nil {
				continue
			}
			active := false
			for _, job := range jobs {
				if job.Status == model.ProjectMemoryJobQueued || job.Status == model.ProjectMemoryJobClaimed || job.Status == model.ProjectMemoryJobRunning {
					active = true
					break
				}
			}
			if active {
				continue
			}
			scope, err := a.store.GetProjectScope(ctx, agent.OperatorID, agent.MachineID, scopeID)
			if err != nil || scope == nil {
				continue
			}
			trigger := model.ProjectMemoryTriggerSourceRecheck
			message := "Source availability recheck queued"
			brief, briefErr := a.store.GetProjectMemoryBrief(ctx, agent.OperatorID, scopeID)
			if briefErr == nil && (brief == nil || brief.SourceRevision < scope.Revision) {
				trigger = model.ProjectMemoryTriggerBriefRebuild
				message = "Project brief rebuild queued"
			}
			job := model.ProjectMemoryJob{
				ID: fmt.Sprintf("pmjob_source_%d", time.Now().UnixNano()), OperatorID: agent.OperatorID,
				MachineID: agent.MachineID, ScopeID: scopeID, Trigger: trigger,
				Status: model.ProjectMemoryJobQueued, InputRevision: scope.Revision,
				CreatedAt: now, UpdatedAt: now,
			}
			if scheduled, created, err := a.store.ScheduleProjectMemoryJob(ctx, job); err == nil && created {
				_ = a.store.AppendProjectMemoryJobEvent(ctx, model.ProjectMemoryJobEvent{
					JobID: scheduled.ID, Type: "queued", Message: message, CreatedAt: now,
				})
			}
		}
	}
}

func (a *App) enqueueProjectMemory(task *model.Task, trigger model.ProjectMemoryTrigger, cursor string) {
	if !a.projectMemoryEnabled {
		return
	}
	if task == nil || task.OperatorID == 0 || task.AgentID == "" || task.MachineID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	scope, err := a.store.GetProjectScopeForAgent(ctx, task.OperatorID, task.MachineID, task.AgentID)
	if err != nil || scope == nil {
		return
	}
	if !projectmemory.Resolve(ctx, a.store, task.OperatorID, task.MachineID, scope.ID).Enabled {
		return
	}
	now := time.Now().UTC()
	job := model.ProjectMemoryJob{
		ID: fmt.Sprintf("pmjob_%d", now.UnixNano()), OperatorID: task.OperatorID,
		MachineID: task.MachineID, ScopeID: scope.ID, Trigger: trigger,
		Status: model.ProjectMemoryJobQueued, CursorEnd: cursor,
		InputRevision: scope.Revision, CreatedAt: now, UpdatedAt: now,
	}
	scheduled, created, err := a.store.ScheduleProjectMemoryJob(ctx, job)
	if err != nil {
		log.Printf("[project-memory] enqueue failed task=%s scope=%s err=%v", task.ID, scope.ID, err)
		return
	}
	eventType := "coalesced"
	message := "Project memory source cursor coalesced"
	if created {
		eventType = "queued"
		message = "Project memory job queued"
	}
	_ = a.store.AppendProjectMemoryJobEvent(ctx, model.ProjectMemoryJobEvent{
		JobID: scheduled.ID, Type: eventType, Message: message, CreatedAt: now,
	})
	go a.dispatchProjectMemoryJobs(context.Background())
}

func projectMemoryAgentForJob(job model.ProjectMemoryJob, agents []model.Agent) (model.Agent, model.HelloProject, bool) {
	for _, agent := range agents {
		if agent.OperatorID != job.OperatorID || agent.MachineID != job.MachineID || agent.Kind == "launcher" {
			continue
		}
		capable := false
		for _, capability := range agent.Capabilities {
			if capability == "project_memory_v1" {
				capable = true
				break
			}
		}
		if !capable {
			continue
		}
		for _, project := range agent.Projects {
			if project.ScopeID == job.ScopeID {
				return agent, project, true
			}
		}
	}
	return model.Agent{}, model.HelloProject{}, false
}

func projectMemoryTaskSourcePacket(task *model.Task, events []model.Event, limit int) []byte {
	if task == nil {
		return nil
	}
	if limit <= 0 {
		limit = projectMemoryTaskSourceLimit
	}
	type taskSource struct {
		TaskID          string            `json:"task_id"`
		SessionID       string            `json:"session_id,omitempty"`
		ProjectID       string            `json:"project_id"`
		Metadata        map[string]string `json:"metadata,omitempty"`
		Parts           []model.Part      `json:"parts,omitempty"`
		Result          string            `json:"result,omitempty"`
		Error           string            `json:"error,omitempty"`
		Artifacts       []model.Artifact  `json:"artifacts,omitempty"`
		Events          []model.Event     `json:"events,omitempty"`
		SourceTruncated bool              `json:"source_truncated,omitempty"`
	}
	payload := taskSource{
		TaskID: truncateProjectMemorySource(task.ID, 256), SessionID: truncateProjectMemorySource(task.SessionID, 256),
		ProjectID: truncateProjectMemorySource(task.ProjectID, 256),
		Metadata:  boundedProjectMemoryMetadata(task.Metadata, 8, 256),
		Parts:     boundedProjectMemoryParts(task.Parts, 4, 2*1024),
		Result:    truncateProjectMemorySource(task.Result, 24*1024),
		Error:     truncateProjectMemorySource(task.Error, 4*1024),
		Artifacts: boundedProjectMemoryArtifacts(task.Artifacts, 16),
		Events:    make([]model.Event, 0, minProjectMemoryInt(len(events), projectMemoryTaskEventLimit)),
	}
	payload.SourceTruncated = len(payload.Metadata) < len(task.Metadata) || len(payload.Parts) < len(task.Parts) ||
		len(payload.Result) < len(task.Result) || len(payload.Error) < len(task.Error) || len(payload.Artifacts) < len(task.Artifacts) ||
		len(events) > projectMemoryTaskEventLimit

	base, err := json.Marshal(payload)
	if err != nil {
		return projectMemoryMinimalTaskSource(task, limit)
	}
	const eventsEnvelopeSize = len(`,"events":[]`)
	eventsSize := eventsEnvelopeSize
	for index := len(events) - 1; index >= 0 && len(payload.Events) < projectMemoryTaskEventLimit; index-- {
		event, ok, truncated := boundedProjectMemoryEvent(events[index])
		if !ok {
			continue
		}
		eventData, marshalErr := json.Marshal(event)
		if marshalErr != nil {
			payload.SourceTruncated = true
			continue
		}
		separatorSize := 0
		if len(payload.Events) > 0 {
			separatorSize = 1
		}
		reserveTruncationMarker := 0
		if !payload.SourceTruncated && (truncated || index > 0) {
			reserveTruncationMarker = len(`,"source_truncated":true`)
		}
		if len(base)+eventsSize+separatorSize+len(eventData)+reserveTruncationMarker > limit {
			payload.SourceTruncated = true
			break
		}
		payload.SourceTruncated = payload.SourceTruncated || truncated
		payload.Events = append(payload.Events, event)
		eventsSize += separatorSize + len(eventData)
	}
	for left, right := 0, len(payload.Events)-1; left < right; left, right = left+1, right-1 {
		payload.Events[left], payload.Events[right] = payload.Events[right], payload.Events[left]
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return projectMemoryMinimalTaskSource(task, limit)
	}
	for len(data) > limit && len(payload.Events) > 0 {
		payload.Events = payload.Events[1:]
		payload.SourceTruncated = true
		data, err = json.Marshal(payload)
		if err != nil {
			return projectMemoryMinimalTaskSource(task, limit)
		}
	}
	if len(data) <= limit {
		return data
	}
	return projectMemoryMinimalTaskSource(task, limit)
}

func boundedProjectMemoryEvent(event model.Event) (model.Event, bool, bool) {
	switch event.Type {
	case "completed", "failed", "tool_updated", "goal_checkpoint", "goal_completed", "plan_updated", "compaction_completed":
	default:
		return model.Event{}, false, false
	}
	truncated := len(event.TaskID) > 256 || len(event.Type) > 128 || len(event.Content) > 12*1024 ||
		len(event.Field) > 128 || len(event.SessionID) > 256 || len(event.Error) > 8*1024 || len(event.Artifacts) > 16 ||
		event.Metadata != nil || len(event.Files) > 0 || event.Question != nil || len(event.Patterns) > 0 || len(event.Reply) > 0
	event.TaskID = truncateProjectMemorySource(event.TaskID, 256)
	event.Type = truncateProjectMemorySource(event.Type, 128)
	event.Content = truncateProjectMemorySource(event.Content, 12*1024)
	event.Field = truncateProjectMemorySource(event.Field, 128)
	event.SessionID = truncateProjectMemorySource(event.SessionID, 256)
	event.PermissionID = truncateProjectMemorySource(event.PermissionID, 256)
	event.Permission = truncateProjectMemorySource(event.Permission, 256)
	event.Error = truncateProjectMemorySource(event.Error, 8*1024)
	event.Metadata = nil
	event.Files = nil
	event.Question = nil
	event.Patterns = nil
	event.Reply = ""
	event.Artifacts = boundedProjectMemoryArtifacts(event.Artifacts, 16)
	if event.Plan != nil {
		plan := *event.Plan
		truncated = truncated || len(plan.ID) > 256 || len(plan.Title) > 512 || len(plan.Mode) > 64 || len(plan.Status) > 64 || len(plan.SessionID) > 256 || len(plan.Items) > 32
		plan.ID = truncateProjectMemorySource(plan.ID, 256)
		plan.Title = truncateProjectMemorySource(plan.Title, 512)
		plan.Mode = truncateProjectMemorySource(plan.Mode, 64)
		plan.Status = truncateProjectMemorySource(plan.Status, 64)
		plan.SessionID = truncateProjectMemorySource(plan.SessionID, 256)
		plan.Items = append([]model.PlanItem(nil), plan.Items[:minProjectMemoryInt(len(plan.Items), 32)]...)
		for index := range plan.Items {
			truncated = truncated || len(plan.Items[index].ID) > 256 || len(plan.Items[index].Text) > 1024 || len(plan.Items[index].Status) > 64 || len(plan.Items[index].Priority) > 64
			plan.Items[index].ID = truncateProjectMemorySource(plan.Items[index].ID, 256)
			plan.Items[index].Text = truncateProjectMemorySource(plan.Items[index].Text, 1024)
			plan.Items[index].Status = truncateProjectMemorySource(plan.Items[index].Status, 64)
			plan.Items[index].Priority = truncateProjectMemorySource(plan.Items[index].Priority, 64)
		}
		event.Plan = &plan
	}
	if event.Tool != nil {
		tool := *event.Tool
		truncated = truncated || len(tool.ID) > 256 || len(tool.CallID) > 256 || len(tool.Tool) > 256 || len(tool.Status) > 64 || tool.Input != nil || tool.Metadata != nil || len(tool.Attachments) > 0 ||
			len(tool.Output) > 8*1024 || len(tool.Error) > 4*1024 || len(tool.Title) > 512
		tool.ID = truncateProjectMemorySource(tool.ID, 256)
		tool.CallID = truncateProjectMemorySource(tool.CallID, 256)
		tool.Tool = truncateProjectMemorySource(tool.Tool, 256)
		tool.Status = truncateProjectMemorySource(tool.Status, 64)
		tool.Input = nil
		tool.Metadata = nil
		tool.Attachments = nil
		tool.Title = truncateProjectMemorySource(tool.Title, 512)
		tool.Output = truncateProjectMemorySource(tool.Output, 8*1024)
		tool.Error = truncateProjectMemorySource(tool.Error, 4*1024)
		tool.OutputTruncated = tool.OutputTruncated || len(tool.Output) < len(event.Tool.Output)
		event.Tool = &tool
	}
	return event, true, truncated
}

func boundedProjectMemoryMetadata(input map[string]string, countLimit, valueLimit int) map[string]string {
	if len(input) == 0 || countLimit <= 0 {
		return nil
	}
	output := make(map[string]string, minProjectMemoryInt(len(input), countLimit))
	for key, value := range input {
		if len(output) >= countLimit {
			break
		}
		output[truncateProjectMemorySource(key, 128)] = truncateProjectMemorySource(value, valueLimit)
	}
	return output
}

func boundedProjectMemoryParts(input []model.Part, countLimit, textLimit int) []model.Part {
	if len(input) == 0 || countLimit <= 0 {
		return nil
	}
	input = input[:minProjectMemoryInt(len(input), countLimit)]
	output := make([]model.Part, 0, len(input))
	for _, part := range input {
		part.Text = truncateProjectMemorySource(part.Text, textLimit)
		part.Prompt = truncateProjectMemorySource(part.Prompt, textLimit)
		part.Description = truncateProjectMemorySource(part.Description, 1024)
		part.Command = truncateProjectMemorySource(part.Command, 1024)
		part.Filename = truncateProjectMemorySource(part.Filename, 512)
		part.URL = truncateProjectMemorySource(part.URL, 2048)
		part.Name = truncateProjectMemorySource(part.Name, 256)
		part.Source = nil
		output = append(output, part)
	}
	return output
}

func boundedProjectMemoryArtifacts(input []model.Artifact, countLimit int) []model.Artifact {
	if len(input) == 0 || countLimit <= 0 {
		return nil
	}
	input = input[:minProjectMemoryInt(len(input), countLimit)]
	output := make([]model.Artifact, len(input))
	copy(output, input)
	for index := range output {
		output[index].ID = truncateProjectMemorySource(output[index].ID, 256)
		output[index].Filename = truncateProjectMemorySource(output[index].Filename, 512)
		output[index].MIME = truncateProjectMemorySource(output[index].MIME, 128)
		output[index].RelativePath = truncateProjectMemorySource(output[index].RelativePath, 2048)
	}
	return output
}

func projectMemoryMinimalTaskSource(task *model.Task, limit int) []byte {
	payload := map[string]any{
		"task_id": truncateProjectMemorySource(task.ID, 256), "session_id": truncateProjectMemorySource(task.SessionID, 256),
		"project_id":       truncateProjectMemorySource(task.ProjectID, 256),
		"source_truncated": true, "summary": truncateProjectMemorySource(task.Result, minProjectMemoryInt(32*1024, limit/2)),
	}
	data, _ := json.Marshal(payload)
	if len(data) <= limit {
		return data
	}
	payload["summary"] = ""
	data, _ = json.Marshal(payload)
	return data
}

func minProjectMemoryInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func projectMemorySourcePacket(task *model.Task, events []model.Event) string {
	if task == nil {
		return "No task cursor was supplied. Review the current project and existing memory using read-only tools."
	}
	return string(projectMemoryTaskSourcePacket(task, events, projectMemorySourceLimit))
}

func truncateProjectMemorySource(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(value) <= limit {
		return value
	}
	return strings.ToValidUTF8(value[:limit], "")
}

func projectMemoryTerminalTask(task *model.Task) bool {
	if task == nil {
		return false
	}
	switch task.Status {
	case model.TaskCompleted, model.TaskFailed, model.TaskCancelled:
		return true
	default:
		return false
	}
}

func projectMemoryTaskMatches(job *model.ProjectMemoryJob, projectID string, task *model.Task) bool {
	return task != nil && task.OperatorID == job.OperatorID && task.MachineID == job.MachineID &&
		task.ProjectID == projectID
}

func compareProjectMemoryTask(a, b *model.Task) int {
	if a.CreatedAt.Before(b.CreatedAt) {
		return -1
	}
	if a.CreatedAt.After(b.CreatedAt) {
		return 1
	}
	return strings.Compare(a.ID, b.ID)
}

type projectMemorySourceBatch struct {
	Source       string
	LastTask     *model.Task
	TargetCursor string
	HasMore      bool
	TaskCount    int
}

func projectMemorySourceEnvelope(tasks []json.RawMessage, targetCursor string, truncated bool) []byte {
	data, _ := json.Marshal(map[string]any{
		"task_count": len(tasks), "target_cursor": targetCursor,
		"source_truncated": truncated, "tasks": tasks,
	})
	return data
}

func (a *App) latestProjectMemoryTerminalTask(job *model.ProjectMemoryJob, projectID string) (*model.Task, error) {
	filter := model.TaskFilter{
		OperatorID: job.OperatorID, MachineID: job.MachineID, ProjectID: projectID,
		CreatedDesc: true, Limit: projectMemoryTaskPageLimit,
	}
	for {
		tasks, err := a.store.ListTasks(filter)
		if err != nil {
			return nil, err
		}
		for _, task := range tasks {
			if projectMemoryTerminalTask(task) {
				return task, nil
			}
		}
		if len(tasks) < projectMemoryTaskPageLimit {
			return nil, nil
		}
		last := tasks[len(tasks)-1]
		filter.BeforeCreatedAt = last.CreatedAt
		filter.BeforeTaskID = last.ID
	}
}

func (a *App) projectMemorySourceBatch(job *model.ProjectMemoryJob, projectID string) (projectMemorySourceBatch, error) {
	targetID := strings.TrimSpace(job.CursorEnd)
	var target *model.Task
	if targetID != "" {
		candidate, found := a.store.GetTask(targetID)
		if !found || !projectMemoryTaskMatches(job, projectID, candidate) || !projectMemoryTerminalTask(candidate) {
			return projectMemorySourceBatch{}, errors.New("project memory target cursor does not belong to this project")
		}
		target = candidate
	} else {
		var err error
		target, err = a.latestProjectMemoryTerminalTask(job, projectID)
		if err != nil {
			return projectMemorySourceBatch{}, err
		}
		if target == nil {
			return projectMemorySourceBatch{
				Source: "No terminal project tasks are available. Review the current project and existing memory using read-only tools.",
			}, nil
		}
		targetID = target.ID
	}

	startID := strings.TrimSpace(job.CursorStart)
	if startID == targetID {
		record := json.RawMessage(projectMemoryTaskSourcePacket(target, a.store.ProjectMemoryEvents(target.ID, projectMemoryTaskEventLimit), projectMemoryTaskSourceLimit))
		return projectMemorySourceBatch{
			Source:   string(projectMemorySourceEnvelope([]json.RawMessage{record}, targetID, false)),
			LastTask: target, TargetCursor: targetID, TaskCount: 1,
		}, nil
	}

	filter := model.TaskFilter{
		OperatorID: job.OperatorID, MachineID: job.MachineID, ProjectID: projectID,
		CreatedAsc: true, Limit: projectMemoryTaskPageLimit,
	}
	if startID != "" {
		start, found := a.store.GetTask(startID)
		if !found || !projectMemoryTaskMatches(job, projectID, start) {
			return projectMemorySourceBatch{}, errors.New("project memory start cursor does not belong to this project")
		}
		filter.AfterCreatedAt = start.CreatedAt
		filter.AfterTaskID = start.ID
	}

	records := make([]json.RawMessage, 0, 8)
	var lastIncluded *model.Task
	for {
		tasks, err := a.store.ListTasks(filter)
		if err != nil {
			return projectMemorySourceBatch{}, err
		}
		if len(tasks) == 0 {
			break
		}
		for _, task := range tasks {
			if compareProjectMemoryTask(task, target) > 0 {
				break
			}
			if !projectMemoryTerminalTask(task) {
				continue
			}
			record := json.RawMessage(projectMemoryTaskSourcePacket(task, a.store.ProjectMemoryEvents(task.ID, projectMemoryTaskEventLimit), projectMemoryTaskSourceLimit))
			candidate := append(append([]json.RawMessage{}, records...), record)
			truncated := task.ID != targetID
			if len(records) > 0 && len(projectMemorySourceEnvelope(candidate, targetID, truncated)) > projectMemorySourceLimit {
				return projectMemorySourceBatch{
					Source:   string(projectMemorySourceEnvelope(records, targetID, true)),
					LastTask: lastIncluded, TargetCursor: targetID, HasMore: true, TaskCount: len(records),
				}, nil
			}
			records = candidate
			lastIncluded = task
			if task.ID == targetID {
				return projectMemorySourceBatch{
					Source:   string(projectMemorySourceEnvelope(records, targetID, false)),
					LastTask: task, TargetCursor: targetID, TaskCount: len(records),
				}, nil
			}
		}
		last := tasks[len(tasks)-1]
		if compareProjectMemoryTask(last, target) >= 0 || len(tasks) < projectMemoryTaskPageLimit {
			break
		}
		filter.AfterCreatedAt = last.CreatedAt
		filter.AfterTaskID = last.ID
	}
	if lastIncluded == nil {
		return projectMemorySourceBatch{}, errors.New("project memory cursor range contains no terminal tasks")
	}
	return projectMemorySourceBatch{
		Source:   string(projectMemorySourceEnvelope(records, targetID, lastIncluded.ID != targetID)),
		LastTask: lastIncluded, TargetCursor: targetID, HasMore: lastIncluded.ID != targetID, TaskCount: len(records),
	}, nil
}

func (a *App) dispatchProjectMemoryJobs(ctx context.Context) {
	if !a.projectMemoryEnabled {
		return
	}
	jobs, err := a.store.ListClaimableProjectMemoryJobs(ctx, time.Now().UTC(), 50)
	projectmemory.DefaultMetrics.SetQueueDepth(len(jobs))
	if err != nil || len(jobs) == 0 {
		return
	}
	agents := a.broker.List()
	for _, pending := range jobs {
		if (pending.Status == model.ProjectMemoryJobClaimed || pending.Status == model.ProjectMemoryJobRunning) &&
			!pending.LeaseExpiresAt.IsZero() && pending.LeaseExpiresAt.Before(time.Now().UTC()) {
			projectmemory.DefaultMetrics.LeaseExpired()
		}
		agent, project, ok := projectMemoryAgentForJob(pending, agents)
		if !ok {
			continue
		}
		settings := projectmemory.Resolve(ctx, a.store, pending.OperatorID, pending.MachineID, pending.ScopeID)
		if !settings.Enabled {
			pending.Status = model.ProjectMemoryJobCancelled
			pending.Error = "project memory disabled"
			pending.UpdatedAt = time.Now().UTC()
			_ = a.store.UpdateProjectMemoryJob(ctx, pending)
			continue
		}
		now := time.Now().UTC()
		job, err := a.store.ClaimProjectMemoryJob(ctx, pending.ID, agent.ID, now, now.Add(projectMemoryLeaseDuration), 2)
		if err != nil || job == nil {
			continue
		}
		sourceBatch, sourceErr := a.projectMemorySourceBatch(job, project.ProjectID)
		if sourceErr != nil {
			job.Status = model.ProjectMemoryJobFailed
			job.Error = sourceErr.Error()
			job.LeaseOwner = ""
			job.LeaseExpiresAt = time.Time{}
			_ = a.store.UpdateProjectMemoryJob(context.Background(), *job)
			_ = a.store.AppendProjectMemoryJobEvent(context.Background(), model.ProjectMemoryJobEvent{
				JobID: job.ID, Type: "failed", Message: job.Error, CreatedAt: now,
			})
			continue
		}
		task := sourceBatch.LastTask
		if task != nil {
			pending := strings.TrimSpace(job.PendingCursorEnd)
			job.CursorEnd = task.ID
			if sourceBatch.HasMore && pending == "" {
				job.PendingCursorEnd = sourceBatch.TargetCursor
			}
			if err := a.store.UpdateProjectMemoryJob(context.Background(), *job); err != nil {
				continue
			}
		}
		brief, _ := a.store.GetProjectMemoryBrief(ctx, job.OperatorID, job.ScopeID)
		memories, _ := a.store.ListProjectMemories(ctx, model.ProjectMemoryFilter{
			OperatorID: job.OperatorID, MachineID: job.MachineID, ScopeID: job.ScopeID,
			Status: model.ProjectMemoryActive, Limit: 100,
		})
		filtered := memories[:0]
		for _, memory := range memories {
			if !memory.Sensitive {
				filtered = append(filtered, memory)
			}
		}
		payload := model.ProjectMemoryRunPayload{
			JobID: job.ID, ProjectScopeID: job.ScopeID, ProjectID: project.ProjectID,
			BindingEpoch: project.BindingEpoch, FencingToken: job.FencingToken,
			InputRevision: job.InputRevision, CursorStart: job.CursorStart, CursorEnd: job.CursorEnd,
			Trigger: job.Trigger, Source: sourceBatch.Source, CurrentMemory: filtered,
		}
		payload.Model = settings.Model
		payload.Variant = settings.Variant
		if payload.Model == "" && task != nil {
			payload.Model = task.Metadata["model"]
			payload.Variant = task.Metadata["variant"]
		}
		if brief != nil {
			payload.CurrentBrief = brief.Content
		}
		log.Printf("[project-memory] dispatch job=%s scope=%s source_tasks=%d source_bytes=%d cursor_start=%s cursor_end=%s target_cursor=%s has_more=%t",
			job.ID, job.ScopeID, sourceBatch.TaskCount, len(sourceBatch.Source), job.CursorStart, job.CursorEnd,
			sourceBatch.TargetCursor, sourceBatch.HasMore)
		dispatchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		err = a.broker.DispatchForOperator(dispatchCtx, job.OperatorID, agent.ID, model.Envelope{
			Type: "project.memory.run", RequestID: job.ID,
			SentAt: now.Format(time.RFC3339), Payload: payload,
		})
		cancel()
		if err != nil {
			job.Status = model.ProjectMemoryJobQueued
			job.LeaseOwner = ""
			job.LeaseExpiresAt = time.Time{}
			_ = a.store.UpdateProjectMemoryJob(context.Background(), *job)
			continue
		}
		_ = a.store.AppendProjectMemoryJobEvent(context.Background(), model.ProjectMemoryJobEvent{
			JobID: job.ID, Type: "dispatched", Message: "整理任务已下发到项目 Agent", CreatedAt: now,
			Metadata: map[string]any{
				"source_task_count": sourceBatch.TaskCount,
				"source_bytes":      len(sourceBatch.Source),
				"cursor_start":      job.CursorStart,
				"cursor_end":        job.CursorEnd,
				"target_cursor":     sourceBatch.TargetCursor,
				"has_more":          sourceBatch.HasMore,
			},
		})
	}
}

func validProjectMemoryKind(kind model.ProjectMemoryKind) bool {
	switch kind {
	case model.ProjectMemoryLockedRule, model.ProjectMemoryVerifiedFact, model.ProjectMemoryInferredFact,
		model.ProjectMemoryProcedure, model.ProjectMemoryDecision, model.ProjectMemoryIssue, model.ProjectMemoryEpisode:
		return true
	default:
		return false
	}
}

func redactProjectMemoryCredentials(value string) string {
	value = projectMemoryPrivateKeyPattern.ReplaceAllString(value, "[REDACTED_PRIVATE_KEY]")
	value = projectMemoryBearerPattern.ReplaceAllString(value, `${1}[REDACTED]`)
	value = projectMemoryJWTPattern.ReplaceAllString(value, "[REDACTED_TOKEN]")
	return projectMemoryCredentialPattern.ReplaceAllString(value, `${1}${2}[REDACTED]`)
}

func validProjectMemorySHA256(value string) bool {
	return projectMemorySHA256Pattern.MatchString(value)
}

func validProjectMemoryHookOffset(artifact model.ProjectMemoryArtifact) bool {
	value := strings.TrimSpace(strings.ToLower(artifact.RelativeOffset))
	offset, err := strconv.ParseUint(value, 0, 64)
	if err != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(artifact.InstructionSet)) {
	case "aarch64", "arm64":
		return offset%4 == 0
	case "arm", "aarch32":
		return offset%4 == 0
	case "thumb", "thumb2":
		return offset%2 == 0
	case "x86", "x86_64":
		return true
	default:
		return false
	}
}

func validateProjectMemoryBatch(batch *model.ProjectMemoryCandidateBatch, job *model.ProjectMemoryJob) error {
	if batch == nil || (batch.SchemaVersion != projectMemorySchemaCurrent && batch.SchemaVersion != projectMemorySchemaPrevious) ||
		batch.JobID != job.ID || batch.ProjectScopeID != job.ScopeID {
		return errors.New("invalid project memory candidate envelope")
	}
	if len(batch.Candidates) > 200 {
		return errors.New("too many project memory candidates")
	}
	if len(batch.Invalidations) > 200 {
		return errors.New("too many project memory invalidations")
	}
	for index := range batch.Invalidations {
		invalidation := &batch.Invalidations[index]
		invalidation.MemoryID = strings.TrimSpace(invalidation.MemoryID)
		invalidation.ContentHash = strings.ToLower(strings.TrimSpace(invalidation.ContentHash))
		invalidation.Reason = strings.TrimSpace(redactProjectMemoryCredentials(invalidation.Reason))
		if invalidation.MemoryID == "" || !validProjectMemorySHA256(invalidation.ContentHash) ||
			(invalidation.Status != model.ProjectMemoryStale && invalidation.Status != model.ProjectMemoryDisputed) ||
			invalidation.Reason == "" || len([]rune(invalidation.Reason)) > 1000 ||
			len(invalidation.EvidenceRefs) == 0 || len(invalidation.EvidenceRefs) > 32 {
			return fmt.Errorf("invalid project memory invalidation at index %d", index)
		}
		for evidenceIndex := range invalidation.EvidenceRefs {
			invalidation.EvidenceRefs[evidenceIndex] = strings.TrimSpace(redactProjectMemoryCredentials(invalidation.EvidenceRefs[evidenceIndex]))
			if invalidation.EvidenceRefs[evidenceIndex] == "" || len([]rune(invalidation.EvidenceRefs[evidenceIndex])) > 500 {
				return fmt.Errorf("invalid project memory invalidation evidence at index %d", index)
			}
		}
	}
	for index := range batch.Candidates {
		candidate := &batch.Candidates[index]
		candidate.ProjectScopeID = strings.TrimSpace(candidate.ProjectScopeID)
		candidate.JobID = strings.TrimSpace(candidate.JobID)
		candidate.SubjectKey = strings.TrimSpace(candidate.SubjectKey)
		candidate.Statement = strings.TrimSpace(redactProjectMemoryCredentials(candidate.Statement))
		for sourceIndex := range candidate.Sources {
			candidate.Sources[sourceIndex].Excerpt = redactProjectMemoryCredentials(candidate.Sources[sourceIndex].Excerpt)
		}
		if candidate.ProjectScopeID != "" && candidate.ProjectScopeID != job.ScopeID {
			return errors.New("candidate scope mismatch")
		}
		if candidate.JobID != "" && candidate.JobID != job.ID {
			return errors.New("candidate job mismatch")
		}
		if !validProjectMemoryKind(candidate.Kind) || candidate.SubjectKey == "" || candidate.Statement == "" || len([]rune(candidate.Statement)) > 2000 {
			return fmt.Errorf("invalid candidate at index %d", index)
		}
		if candidate.Confidence < 0 || candidate.Confidence > 1 || len(candidate.Sources) == 0 || len(candidate.Sources) > 32 {
			return fmt.Errorf("invalid candidate bounds at index %d", index)
		}
		for _, source := range candidate.Sources {
			hasReference := source.SessionID != "" || source.TaskID != "" || len(source.MessageIDs) > 0 ||
				len(source.ToolCallIDs) > 0 || source.RelativePath != "" || strings.TrimSpace(source.Excerpt) != ""
			if !hasReference || (source.RelativePath != "" && !validProjectMemorySHA256(source.SHA256)) {
				return fmt.Errorf("candidate source lacks traceable evidence at index %d", index)
			}
		}
		if candidate.Verification.Status == model.ProjectMemoryVerified {
			if strings.TrimSpace(candidate.Verification.Method) == "" || len(candidate.Verification.EvidenceRefs) == 0 {
				return fmt.Errorf("verified candidate lacks evidence at index %d", index)
			}
			for _, artifact := range candidate.Artifacts {
				hookRecord := artifact.RelativeOffset != "" || artifact.ShadowHookStatus != "" || artifact.NativeCallStatus != ""
				if hookRecord && (!validProjectMemorySHA256(artifact.SHA256) || artifact.PackageName == "" ||
					!validProjectMemorySHA256(artifact.APKSHA256) || artifact.BuildID == "" || artifact.ABI == "" ||
					artifact.AppVersion == "" || artifact.ModuleName == "" || artifact.Symbol == "" ||
					artifact.FunctionStart == "" || !validProjectMemoryHookOffset(artifact) ||
					artifact.IDAStatus != "complete" || artifact.IDADatabaseID == "" || artifact.HotUpdateStatus == "" ||
					artifact.ShadowHookStatus != "verified" || artifact.UICallStatus != "verified" ||
					artifact.NativeCallStatus != "verified") {
					return fmt.Errorf("verified native hook candidate lacks runtime evidence at index %d", index)
				}
			}
		}
		candidate.ProjectScopeID = job.ScopeID
		candidate.JobID = job.ID
	}
	if batch.BriefCandidate != nil {
		batch.BriefCandidate.Content = redactProjectMemoryCredentials(batch.BriefCandidate.Content)
	}
	return nil
}

func (a *App) validateProjectMemorySources(job *model.ProjectMemoryJob, candidates []model.ProjectMemoryCandidate) error {
	for index, candidate := range candidates {
		for _, source := range candidate.Sources {
			if strings.TrimSpace(source.TaskID) == "" {
				continue
			}
			task, found := a.store.GetTask(source.TaskID)
			if !found || task.OperatorID != job.OperatorID || task.MachineID != job.MachineID {
				return fmt.Errorf("candidate source does not belong to project at index %d", index)
			}
			if sourceScope := strings.TrimSpace(task.Metadata["project_memory_scope_id"]); sourceScope != "" && sourceScope != job.ScopeID {
				return fmt.Errorf("candidate source scope mismatch at index %d", index)
			}
		}
	}
	return nil
}

func (a *App) projectMemoryJobForWorker(device *broker.Device, payload model.ProjectMemoryWorkerPayload) (*model.ProjectMemoryJob, *model.ProjectScope, error) {
	if device == nil {
		return nil, nil, errors.New("missing worker device")
	}
	job, err := a.store.GetProjectMemoryJob(context.Background(), device.OperatorID, payload.JobID)
	if err != nil || job == nil {
		return nil, nil, errors.New("project memory job not found")
	}
	if job.ScopeID != payload.ProjectScopeID || job.LeaseOwner != device.ID || job.FencingToken != payload.FencingToken {
		return nil, nil, errors.New("stale project memory worker")
	}
	if (job.Status != model.ProjectMemoryJobClaimed && job.Status != model.ProjectMemoryJobRunning) ||
		job.LeaseExpiresAt.IsZero() || job.LeaseExpiresAt.Before(time.Now().UTC()) {
		return nil, nil, errors.New("expired project memory worker lease")
	}
	scope, err := a.store.GetProjectScope(context.Background(), device.OperatorID, device.MachineID, job.ScopeID)
	if err != nil || scope == nil || scope.BindingEpoch != payload.BindingEpoch {
		return nil, nil, errors.New("stale project binding")
	}
	return job, scope, nil
}

func (a *App) handleProjectMemoryWorker(device *broker.Device, eventType string, payload model.ProjectMemoryWorkerPayload) error {
	if !a.projectMemoryEnabled {
		return errors.New("project memory feature is disabled")
	}
	job, scope, err := a.projectMemoryJobForWorker(device, payload)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	recordedType := strings.TrimPrefix(eventType, "project.memory.")
	eventMetadata := map[string]any{"cursor": payload.Cursor, "result_revision": job.ResultRevision}
	switch eventType {
	case "project.memory.accepted":
		job.Status = model.ProjectMemoryJobRunning
		job.LeaseExpiresAt = now.Add(projectMemoryLeaseDuration)
	case "project.memory.progress":
		if ok, err := a.store.RenewProjectMemoryJob(context.Background(), job.ID, device.ID, job.FencingToken, now.Add(projectMemoryLeaseDuration)); err != nil || !ok {
			return errors.New("project memory lease renewal rejected")
		}
	case "project.memory.checkpoint":
		job.CheckpointCursor = payload.Cursor
		job.CheckpointProgress = payload.Progress
		job.CheckpointAt = now
		job.LeaseExpiresAt = now.Add(projectMemoryLeaseDuration)
	case "project.memory.candidates":
		if err := validateProjectMemoryBatch(payload.Batch, job); err != nil {
			job.Status = model.ProjectMemoryJobFailed
			job.Error = err.Error()
			job.LeaseOwner = ""
			job.LeaseExpiresAt = time.Time{}
			_ = a.store.UpdateProjectMemoryJob(context.Background(), *job)
			_ = a.store.AppendProjectMemoryJobEvent(context.Background(), model.ProjectMemoryJobEvent{
				JobID: job.ID, Type: "failed", Message: job.Error, CreatedAt: now,
			})
			log.Printf("[project-memory] candidate validation failed job=%s candidates=%d err=%v", job.ID, len(payload.Batch.Candidates), err)
			return err
		}
		if err := a.validateProjectMemorySources(job, payload.Batch.Candidates); err != nil {
			job.Status = model.ProjectMemoryJobFailed
			job.Error = err.Error()
			job.LeaseOwner = ""
			job.LeaseExpiresAt = time.Time{}
			_ = a.store.UpdateProjectMemoryJob(context.Background(), *job)
			_ = a.store.AppendProjectMemoryJobEvent(context.Background(), model.ProjectMemoryJobEvent{
				JobID: job.ID, Type: "failed", Message: job.Error, CreatedAt: now,
			})
			log.Printf("[project-memory] candidate source validation failed job=%s candidates=%d err=%v", job.ID, len(payload.Batch.Candidates), err)
			return err
		}
		var result model.ProjectMemoryReconcileResult
		for attempt := 0; attempt < 5; attempt++ {
			result, err = a.store.ReconcileProjectMemories(context.Background(), device.OperatorID, job.ScopeID, job.InputRevision,
				payload.Batch.Candidates, payload.Batch.Invalidations, payload.Batch.BriefCandidate)
			if !errors.Is(err, store.ErrProjectMemoryRevisionConflict) {
				break
			}
			projectmemory.DefaultMetrics.Conflict()
			latest, lookupErr := a.store.GetProjectScope(context.Background(), device.OperatorID, device.MachineID, scope.ID)
			if lookupErr != nil || latest == nil {
				break
			}
			job.InputRevision = latest.Revision
		}
		if err != nil {
			return err
		}
		job.ResultRevision = result.Revision
		job.CursorCommitted = strings.TrimSpace(payload.Batch.SourceCursor)
		projectmemory.DefaultMetrics.Candidates(len(payload.Batch.Candidates), len(result.AcceptedIDs))
		job.LeaseExpiresAt = now.Add(projectMemoryLeaseDuration)
		eventMetadata["candidate_count"] = len(payload.Batch.Candidates)
		eventMetadata["accepted_count"] = len(result.AcceptedIDs)
		eventMetadata["invalidation_count"] = len(payload.Batch.Invalidations)
		eventMetadata["invalidated_count"] = len(result.InvalidatedIDs)
		eventMetadata["changed"] = result.Changed
		eventMetadata["result_revision"] = result.Revision
		invalidated := make(map[string]struct{}, len(result.InvalidatedIDs))
		for _, memoryID := range result.InvalidatedIDs {
			invalidated[memoryID] = struct{}{}
		}
		for _, invalidation := range payload.Batch.Invalidations {
			if _, ok := invalidated[invalidation.MemoryID]; !ok {
				continue
			}
			_ = a.store.RecordProjectMemoryAudit(context.Background(), model.ProjectMemoryAudit{
				ScopeID: job.ScopeID, MemoryID: invalidation.MemoryID, JobID: job.ID,
				OperatorID: device.OperatorID, Action: "curator_invalidate",
				Metadata: map[string]any{"status": invalidation.Status, "reason": invalidation.Reason, "evidence_refs": invalidation.EvidenceRefs},
			})
		}
		log.Printf("[project-memory] candidates job=%s candidates=%d accepted=%d invalidations=%d invalidated=%d changed=%t revision=%d",
			job.ID, len(payload.Batch.Candidates), len(result.AcceptedIDs), len(payload.Batch.Invalidations),
			len(result.InvalidatedIDs), result.Changed, result.Revision)
		if result.Changed {
			payload.Message = fmt.Sprintf("已校验 %d 条候选记忆，接受 %d 条，失效 %d 条，项目记忆已更新",
				len(payload.Batch.Candidates), len(result.AcceptedIDs), len(result.InvalidatedIDs))
		} else {
			payload.Message = fmt.Sprintf("已校验 %d 条候选记忆，未发现需要更新的内容", len(payload.Batch.Candidates))
		}
	case "project.memory.sources":
		if len(payload.SourceUpdates) > 3_200 {
			return errors.New("too many project memory source updates")
		}
		for index := range payload.SourceUpdates {
			update := &payload.SourceUpdates[index]
			update.MemoryID = strings.TrimSpace(update.MemoryID)
			update.RelativePath = strings.TrimSpace(update.RelativePath)
			update.SHA256 = strings.ToLower(strings.TrimSpace(update.SHA256))
			if update.MemoryID == "" || update.RelativePath == "" ||
				(update.Status != "available" && update.Status != "source_missing" && update.Status != "changed") {
				return errors.New("invalid project memory source update")
			}
		}
		if _, err := a.store.UpdateProjectMemorySourceAvailability(context.Background(), device.OperatorID, job.ScopeID, payload.SourceUpdates); err != nil {
			return err
		}
		job.LeaseExpiresAt = now.Add(projectMemoryLeaseDuration)
	case "project.memory.completed":
		if job.Trigger != model.ProjectMemoryTriggerSourceRecheck && job.ResultRevision <= 0 {
			job.Error = "候选记忆批次未成功写入"
			job.LeaseOwner = ""
			job.LeaseExpiresAt = time.Time{}
			payload.Progress = job.CheckpointProgress
			if job.Attempt < 3 {
				projectmemory.DefaultMetrics.Retried()
				job.Status = model.ProjectMemoryJobQueued
				recordedType = "retrying"
				payload.Message = "候选记忆未成功写入，正在自动重试"
			} else {
				job.Status = model.ProjectMemoryJobFailed
				recordedType = "failed"
				payload.Message = job.Error
			}
		} else if job.ManualVisible && strings.TrimSpace(job.PendingCursorEnd) != "" &&
			strings.TrimSpace(job.PendingCursorEnd) != strings.TrimSpace(job.CursorCommitted) {
			nextCursor := strings.TrimSpace(job.PendingCursorEnd)
			job.Status = model.ProjectMemoryJobQueued
			job.CursorStart = strings.TrimSpace(job.CursorCommitted)
			job.CursorEnd = nextCursor
			job.PendingCursorEnd = ""
			job.CheckpointCursor = ""
			job.CheckpointProgress = 0
			job.CheckpointAt = time.Time{}
			job.Attempt = 0
			job.LeaseOwner = ""
			job.LeaseExpiresAt = time.Time{}
			job.InputRevision = job.ResultRevision
			job.ResultRevision = 0
			job.Error = ""
			recordedType = "queued"
			payload.Progress = 5
			payload.Message = "本批项目记忆已提交，正在继续整理剩余历史任务"
		} else {
			job.Status = model.ProjectMemoryJobCompleted
			job.Error = ""
			job.LeaseOwner = ""
			job.LeaseExpiresAt = time.Time{}
			projectmemory.DefaultMetrics.JobCompleted(now.Sub(job.CreatedAt))
			if strings.TrimSpace(payload.Message) == "" {
				payload.Message = "项目记忆整理完成"
			}
		}
	case "project.memory.failed":
		job.Error = strings.TrimSpace(payload.Error)
		if job.Attempt < 3 {
			projectmemory.DefaultMetrics.Retried()
			job.Status = model.ProjectMemoryJobQueued
			job.LeaseOwner = ""
			job.LeaseExpiresAt = time.Time{}
			recordedType = "retrying"
			payload.Message = "整理过程出现错误，正在自动重试"
		} else {
			job.Status = model.ProjectMemoryJobFailed
			job.LeaseOwner = ""
			job.LeaseExpiresAt = time.Time{}
			payload.Message = job.Error
		}
	default:
		return errors.New("unknown project memory worker event")
	}
	var successor *model.ProjectMemoryJob
	terminal := job.Status == model.ProjectMemoryJobCompleted || job.Status == model.ProjectMemoryJobFailed
	if terminal {
		successor, err = a.store.FinalizeProjectMemoryJob(context.Background(), *job,
			fmt.Sprintf("pmjob_%d", time.Now().UnixNano()))
		if err != nil {
			return err
		}
	} else if eventType != "project.memory.progress" {
		if err := a.store.UpdateProjectMemoryJob(context.Background(), *job); err != nil {
			return err
		}
	}
	if err := a.store.AppendProjectMemoryJobEvent(context.Background(), model.ProjectMemoryJobEvent{
		JobID: job.ID, Type: recordedType,
		Message: payload.Message, Progress: payload.Progress,
		Metadata: eventMetadata, CreatedAt: now,
	}); err != nil {
		return err
	}
	if successor != nil {
		return a.store.AppendProjectMemoryJobEvent(context.Background(), model.ProjectMemoryJobEvent{
			JobID: successor.ID, Type: "queued", Message: "已排队处理整理期间新增的项目内容", CreatedAt: now,
		})
	}
	return nil
}
