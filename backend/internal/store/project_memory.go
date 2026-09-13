package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"relay-server/internal/model"
)

var ErrProjectMemoryRevisionConflict = errors.New("project memory revision conflict")
var ErrProjectMemoryStaleJob = errors.New("stale project memory job fencing token")

const projectMemoryAutoQuietWindow = 60 * time.Second

func projectMemoryAutomaticTrigger(trigger model.ProjectMemoryTrigger) bool {
	return trigger == model.ProjectMemoryTriggerTaskComplete || trigger == model.ProjectMemoryTriggerGoalCheckpoint
}

func projectMemoryQueuedReady(job model.ProjectMemoryJob, now time.Time) bool {
	if job.Status != model.ProjectMemoryJobQueued {
		return false
	}
	if job.Attempt > 0 {
		backoff := time.Duration(1<<min(job.Attempt, 6)) * time.Second
		return !job.UpdatedAt.Add(backoff).After(now)
	}
	if job.ManualVisible || !projectMemoryAutomaticTrigger(job.Trigger) {
		return true
	}
	return !job.UpdatedAt.Add(projectMemoryAutoQuietWindow).After(now)
}

func (m *Memory) projectMemoryArchive() (ProjectMemoryArchive, bool) {
	archive, ok := m.archive.(ProjectMemoryArchive)
	return archive, ok
}

func cloneProjectMemoryScope(in model.ProjectScope) model.ProjectScope { return in }

func cloneProjectMemoryLocation(in model.ProjectScopeLocation) model.ProjectScopeLocation { return in }

func cloneProjectMemoryBinding(in model.ProjectAgentBinding) model.ProjectAgentBinding { return in }

func cloneProjectMemory(in model.ProjectMemory) model.ProjectMemory {
	out := in
	out.ScopePaths = append([]string(nil), in.ScopePaths...)
	out.Artifacts = append([]model.ProjectMemoryArtifact(nil), in.Artifacts...)
	out.Sources = make([]model.ProjectMemorySource, len(in.Sources))
	for i, source := range in.Sources {
		out.Sources[i] = source
		out.Sources[i].MessageIDs = append([]string(nil), source.MessageIDs...)
		out.Sources[i].ToolCallIDs = append([]string(nil), source.ToolCallIDs...)
	}
	return out
}

func cloneProjectMemoryJob(in model.ProjectMemoryJob) model.ProjectMemoryJob { return in }

func cloneProjectMemoryEvent(in model.ProjectMemoryJobEvent) model.ProjectMemoryJobEvent {
	out := in
	if in.Metadata != nil {
		data, err := json.Marshal(in.Metadata)
		if err == nil {
			_ = json.Unmarshal(data, &out.Metadata)
		}
	}
	return out
}

func cloneProjectMemoryBrief(in model.ProjectMemoryBrief) model.ProjectMemoryBrief { return in }

func projectMemoryScopeKey(operatorID int64, machineID, scopeID string) string {
	return fmt.Sprintf("%d\x00%s\x00%s", operatorID, strings.TrimSpace(machineID), strings.TrimSpace(scopeID))
}

func projectMemoryBindingKey(operatorID int64, machineID, agentID string) string {
	return fmt.Sprintf("%d\x00%s\x00%s", operatorID, strings.TrimSpace(machineID), strings.TrimSpace(agentID))
}

func normalizeProjectScope(scope model.ProjectScope) model.ProjectScope {
	scope.ID = strings.TrimSpace(scope.ID)
	scope.MachineID = strings.TrimSpace(scope.MachineID)
	scope.DisplayName = strings.TrimSpace(scope.DisplayName)
	scope.CurrentRoot = strings.TrimSpace(scope.CurrentRoot)
	if scope.Status == "" {
		scope.Status = model.ProjectScopeActive
	}
	if scope.BindingEpoch <= 0 {
		scope.BindingEpoch = 1
	}
	if scope.Revision <= 0 {
		scope.Revision = 1
	}
	now := time.Now().UTC()
	if scope.CreatedAt.IsZero() {
		scope.CreatedAt = now
	}
	if scope.UpdatedAt.IsZero() {
		scope.UpdatedAt = now
	}
	return scope
}

func normalizeProjectMemory(memory model.ProjectMemory) model.ProjectMemory {
	memory.ID = strings.TrimSpace(memory.ID)
	memory.LogicalID = strings.TrimSpace(memory.LogicalID)
	memory.ScopeID = strings.TrimSpace(memory.ScopeID)
	memory.SubjectKey = strings.TrimSpace(memory.SubjectKey)
	memory.Statement = strings.TrimSpace(memory.Statement)
	if memory.Status == "" {
		memory.Status = model.ProjectMemoryActive
	}
	if memory.Verification.Status == "" {
		memory.Verification.Status = model.ProjectMemoryUnverified
	}
	if memory.Confidence < 0 {
		memory.Confidence = 0
	}
	if memory.Confidence > 1 {
		memory.Confidence = 1
	}
	if memory.Version <= 0 {
		memory.Version = 1
	}
	if memory.ContentHash == "" {
		memory.ContentHash = projectMemoryContentHash(memory)
	}
	now := time.Now().UTC()
	if memory.CreatedAt.IsZero() {
		memory.CreatedAt = now
	}
	if memory.UpdatedAt.IsZero() {
		memory.UpdatedAt = now
	}
	return memory
}

func projectMemoryContentHash(memory model.ProjectMemory) string {
	h := sha256.New()
	write := func(value string) {
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(value))
	}
	write(string(memory.Kind))
	write(memory.SubjectKey)
	write(memory.Statement)
	for _, scopePath := range memory.ScopePaths {
		write(scopePath)
	}
	for _, artifact := range memory.Artifacts {
		for _, value := range []string{artifact.Path, artifact.SHA256, artifact.PackageName,
			artifact.APKSHA256, artifact.BuildID, artifact.ABI, artifact.AppVersion,
			artifact.ModuleName, artifact.Symbol, artifact.RelativeOffset, artifact.FunctionStart,
			artifact.InstructionSet, artifact.IDADatabaseID} {
			write(value)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

func projectMemorySourceKey(source model.ProjectMemorySource) string {
	return strings.Join([]string{source.SessionID, source.TaskID, strings.Join(source.MessageIDs, ","),
		strings.Join(source.ToolCallIDs, ","), source.RelativePath, source.SHA256}, "\x00")
}

func mergeProjectMemorySources(existing, incoming []model.ProjectMemorySource) ([]model.ProjectMemorySource, bool) {
	out := append([]model.ProjectMemorySource(nil), existing...)
	seen := make(map[string]struct{}, len(out))
	for _, source := range out {
		seen[projectMemorySourceKey(source)] = struct{}{}
	}
	changed := false
	for _, source := range incoming {
		if len(out) >= 32 {
			break
		}
		key := projectMemorySourceKey(source)
		if _, found := seen[key]; found {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, source)
		changed = true
	}
	return out, changed
}

func newProjectMemoryID(prefix string) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s:%d", prefix, time.Now().UnixNano())))
	return prefix + "_" + hex.EncodeToString(h[:12])
}

func (m *Memory) UpsertProjectScope(ctx context.Context, scope model.ProjectScope) error {
	if archive, ok := m.projectMemoryArchive(); ok {
		return archive.UpsertProjectScope(ctx, scope)
	}
	scope = normalizeProjectScope(scope)
	if scope.ID == "" || scope.OperatorID == 0 || scope.MachineID == "" {
		return errors.New("invalid project scope")
	}
	m.mu.Lock()
	m.projectScopes[projectMemoryScopeKey(scope.OperatorID, scope.MachineID, scope.ID)] = cloneProjectMemoryScope(scope)
	m.mu.Unlock()
	return nil
}

func (m *Memory) GetProjectScope(ctx context.Context, operatorID int64, machineID, scopeID string) (*model.ProjectScope, error) {
	if archive, ok := m.projectMemoryArchive(); ok {
		return archive.GetProjectScope(ctx, operatorID, machineID, scopeID)
	}
	m.mu.RLock()
	scope, found := m.projectScopes[projectMemoryScopeKey(operatorID, machineID, scopeID)]
	m.mu.RUnlock()
	if !found {
		return nil, nil
	}
	out := cloneProjectMemoryScope(scope)
	return &out, nil
}

func (m *Memory) ListProjectScopes(ctx context.Context, operatorID int64, machineID string) ([]model.ProjectScope, error) {
	if archive, ok := m.projectMemoryArchive(); ok {
		return archive.ListProjectScopes(ctx, operatorID, machineID)
	}
	m.mu.RLock()
	out := make([]model.ProjectScope, 0)
	for _, scope := range m.projectScopes {
		if scope.OperatorID != operatorID || (machineID != "" && scope.MachineID != machineID) {
			continue
		}
		out = append(out, cloneProjectMemoryScope(scope))
	}
	m.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

func (m *Memory) UpsertProjectScopeLocation(ctx context.Context, location model.ProjectScopeLocation) error {
	if archive, ok := m.projectMemoryArchive(); ok {
		return archive.UpsertProjectScopeLocation(ctx, location)
	}
	if location.ID == "" {
		location.ID = newProjectMemoryID("loc")
	}
	if location.FirstSeenAt.IsZero() {
		location.FirstSeenAt = time.Now().UTC()
	}
	location.LastSeenAt = time.Now().UTC()
	m.mu.Lock()
	m.projectLocations[location.ID] = cloneProjectMemoryLocation(location)
	m.mu.Unlock()
	return nil
}

func (m *Memory) UpsertProjectAgentBinding(ctx context.Context, binding model.ProjectAgentBinding) error {
	if archive, ok := m.projectMemoryArchive(); ok {
		return archive.UpsertProjectAgentBinding(ctx, binding)
	}
	key := projectMemoryBindingKey(binding.OperatorID, binding.MachineID, binding.AgentID)
	m.mu.Lock()
	m.projectBindings[key] = cloneProjectMemoryBinding(binding)
	m.mu.Unlock()
	return nil
}

func (m *Memory) GetProjectScopeForAgent(ctx context.Context, operatorID int64, machineID, agentID string) (*model.ProjectScope, error) {
	if archive, ok := m.projectMemoryArchive(); ok {
		return archive.GetProjectScopeForAgent(ctx, operatorID, machineID, agentID)
	}
	m.mu.RLock()
	binding, found := m.projectBindings[projectMemoryBindingKey(operatorID, machineID, agentID)]
	scope := m.projectScopes[projectMemoryScopeKey(operatorID, machineID, binding.ScopeID)]
	m.mu.RUnlock()
	if !found {
		return nil, nil
	}
	out := cloneProjectMemoryScope(scope)
	return &out, nil
}

func projectMemoryMatchesFilter(memory model.ProjectMemory, filter model.ProjectMemoryFilter) bool {
	if filter.ScopeID != "" && memory.ScopeID != filter.ScopeID {
		return false
	}
	if filter.Kind != "" && memory.Kind != filter.Kind {
		return false
	}
	if len(filter.Kinds) > 0 {
		matched := false
		for _, kind := range filter.Kinds {
			if memory.Kind == kind {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if filter.Status != "" && memory.Status != filter.Status {
		return false
	}
	if !filter.IncludeDeleted && memory.Status == model.ProjectMemoryDeleted {
		return false
	}
	if filter.Verification != "" && memory.Verification.Status != filter.Verification {
		return false
	}
	if filter.Locked != nil && memory.Locked != *filter.Locked {
		return false
	}
	if filter.Query != "" {
		query := strings.ToLower(strings.TrimSpace(filter.Query))
		haystack := strings.ToLower(memory.Statement + " " + memory.SubjectKey + " " + strings.Join(memory.ScopePaths, " "))
		matched := false
		for _, token := range strings.FieldsFunc(query, func(r rune) bool {
			return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r > 127 || r == '_' || r == '.' || r == '/' || r == '-')
		}) {
			if strings.Contains(haystack, token) {
				matched = true
				break
			}
		}
		if !matched && query != "" {
			return false
		}
	}
	if filter.RelativePath != "" {
		found := false
		for _, path := range memory.ScopePaths {
			if strings.Contains(path, filter.RelativePath) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	if filter.ArtifactHash != "" {
		found := false
		for _, artifact := range memory.Artifacts {
			if strings.EqualFold(artifact.SHA256, filter.ArtifactHash) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (m *Memory) ListProjectMemories(ctx context.Context, filter model.ProjectMemoryFilter) ([]model.ProjectMemory, error) {
	if archive, ok := m.projectMemoryArchive(); ok {
		return archive.ListProjectMemories(ctx, filter)
	}
	m.mu.RLock()
	scopeOwned := false
	for _, scope := range m.projectScopes {
		if scope.OperatorID == filter.OperatorID && scope.ID == filter.ScopeID &&
			(filter.MachineID == "" || scope.MachineID == filter.MachineID) {
			scopeOwned = true
			break
		}
	}
	if !scopeOwned {
		m.mu.RUnlock()
		return []model.ProjectMemory{}, nil
	}
	out := make([]model.ProjectMemory, 0)
	for _, memory := range m.projectMemories {
		if memory.ScopeID != filter.ScopeID || !projectMemoryMatchesFilter(memory, filter) {
			continue
		}
		out = append(out, cloneProjectMemory(memory))
	}
	m.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	if filter.BeforeID != "" {
		start := -1
		if !filter.BeforeUpdatedAt.IsZero() {
			start = sort.Search(len(out), func(index int) bool {
				return out[index].UpdatedAt.Before(filter.BeforeUpdatedAt) ||
					(out[index].UpdatedAt.Equal(filter.BeforeUpdatedAt) && out[index].ID < filter.BeforeID)
			})
		} else {
			for index := range out {
				if out[index].ID == filter.BeforeID {
					start = index + 1
					break
				}
			}
		}
		if start < 0 {
			return []model.ProjectMemory{}, nil
		}
		out = out[start:]
	}
	limit := filter.Limit
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *Memory) GetProjectMemory(ctx context.Context, operatorID int64, scopeID, memoryID string) (*model.ProjectMemory, error) {
	if archive, ok := m.projectMemoryArchive(); ok {
		return archive.GetProjectMemory(ctx, operatorID, scopeID, memoryID)
	}
	m.mu.RLock()
	memory, found := m.projectMemories[memoryID]
	_, scopeFound := m.projectScopes[projectMemoryScopeKey(operatorID, "", scopeID)]
	if !scopeFound {
		for _, candidate := range m.projectScopes {
			if candidate.OperatorID == operatorID && candidate.ID == scopeID {
				scopeFound = true
				break
			}
		}
	}
	m.mu.RUnlock()
	if !found || !scopeFound || memory.ScopeID != scopeID {
		return nil, nil
	}
	out := cloneProjectMemory(memory)
	return &out, nil
}

func (m *Memory) ListProjectMemoryVersions(ctx context.Context, operatorID int64, scopeID, logicalID string) ([]model.ProjectMemory, error) {
	if archive, ok := m.projectMemoryArchive(); ok {
		return archive.ListProjectMemoryVersions(ctx, operatorID, scopeID, logicalID)
	}
	m.mu.RLock()
	owned := false
	for _, scope := range m.projectScopes {
		if scope.OperatorID == operatorID && scope.ID == scopeID {
			owned = true
			break
		}
	}
	out := make([]model.ProjectMemory, 0)
	if owned {
		for _, memory := range m.projectMemories {
			if memory.ScopeID == scopeID && memory.LogicalID == logicalID {
				out = append(out, cloneProjectMemory(memory))
			}
		}
	}
	m.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Version > out[j].Version })
	return out, nil
}

func (m *Memory) UpsertProjectMemory(ctx context.Context, memory model.ProjectMemory) error {
	if archive, ok := m.projectMemoryArchive(); ok {
		return archive.UpsertProjectMemory(ctx, memory)
	}
	memory = normalizeProjectMemory(memory)
	if memory.ID == "" || memory.ScopeID == "" || memory.SubjectKey == "" || memory.Statement == "" {
		return errors.New("invalid project memory")
	}
	m.mu.Lock()
	m.projectMemories[memory.ID] = cloneProjectMemory(memory)
	m.mu.Unlock()
	return nil
}

func (m *Memory) EditProjectMemoryVersion(ctx context.Context, operatorID int64, scopeID, memoryID string, next model.ProjectMemory) error {
	if archive, ok := m.projectMemoryArchive(); ok {
		return archive.EditProjectMemoryVersion(ctx, operatorID, scopeID, memoryID, next)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	previous, found := m.projectMemories[memoryID]
	if !found || previous.ScopeID != scopeID {
		return errors.New("project memory not found")
	}
	if next.Version != 0 && next.Version != previous.Version {
		return ErrProjectMemoryRevisionConflict
	}
	if previous.Status == model.ProjectMemorySuperseded ||
		(previous.Status == model.ProjectMemoryDeleted && (next.Status != model.ProjectMemoryActive || strings.TrimSpace(next.Statement) == "")) {
		return ErrProjectMemoryRevisionConflict
	}
	next = normalizeProjectMemory(next)
	next.ID = newProjectMemoryID("mem")
	next.LogicalID = previous.LogicalID
	next.ScopeID = scopeID
	next.Version = previous.Version + 1
	next.SupersedesID = previous.ID
	next.CreatedBy = "user"
	now := time.Now().UTC()
	next.CreatedAt = now
	next.UpdatedAt = now
	previous.Status = model.ProjectMemorySuperseded
	previous.UpdatedAt = now
	m.projectMemories[previous.ID] = cloneProjectMemory(previous)
	m.projectMemories[next.ID] = cloneProjectMemory(next)
	for key, scope := range m.projectScopes {
		if scope.OperatorID == operatorID && scope.ID == scopeID {
			scope.Revision++
			scope.UpdatedAt = now
			m.projectScopes[key] = scope
		}
	}
	return nil
}

func (m *Memory) MarkProjectMemoryDeleted(ctx context.Context, operatorID int64, scopeID, memoryID string, expectedVersion int64) error {
	if archive, ok := m.projectMemoryArchive(); ok {
		return archive.MarkProjectMemoryDeleted(ctx, operatorID, scopeID, memoryID, expectedVersion)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	memory, found := m.projectMemories[memoryID]
	owned := false
	for _, scope := range m.projectScopes {
		if scope.OperatorID == operatorID && scope.ID == scopeID {
			owned = true
			break
		}
	}
	if !found || !owned || memory.ScopeID != scopeID {
		return errors.New("project memory not found")
	}
	if expectedVersion > 0 && memory.Version != expectedVersion {
		return ErrProjectMemoryRevisionConflict
	}
	if memory.Status == model.ProjectMemorySuperseded {
		return ErrProjectMemoryRevisionConflict
	}
	if memory.Status == model.ProjectMemoryDeleted {
		return nil
	}
	memory.Status = model.ProjectMemoryDeleted
	if memory.Sensitive {
		memory.Statement = ""
	}
	now := time.Now().UTC()
	memory.UpdatedAt = now
	if memory.Sensitive {
		for index := range memory.Sources {
			memory.Sources[index].Excerpt = ""
		}
	}
	m.projectMemories[memoryID] = memory
	for key, scope := range m.projectScopes {
		if scope.OperatorID == operatorID && scope.ID == scopeID {
			scope.Revision++
			scope.UpdatedAt = now
			m.projectScopes[key] = scope
		}
	}
	return nil
}

func (m *Memory) CreateProjectMemoryJob(ctx context.Context, job model.ProjectMemoryJob) error {
	if archive, ok := m.projectMemoryArchive(); ok {
		return archive.CreateProjectMemoryJob(ctx, job)
	}
	if job.ID == "" || job.ScopeID == "" {
		return errors.New("invalid project memory job")
	}
	if job.Status == "" {
		job.Status = model.ProjectMemoryJobQueued
	}
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now().UTC()
	}
	job.UpdatedAt = time.Now().UTC()
	m.mu.Lock()
	m.projectMemoryJobs[job.ID] = cloneProjectMemoryJob(job)
	m.mu.Unlock()
	return nil
}

func normalizeProjectMemoryJob(job model.ProjectMemoryJob) model.ProjectMemoryJob {
	job.ID = strings.TrimSpace(job.ID)
	job.ScopeID = strings.TrimSpace(job.ScopeID)
	job.MachineID = strings.TrimSpace(job.MachineID)
	job.CursorStart = strings.TrimSpace(job.CursorStart)
	job.CursorEnd = strings.TrimSpace(job.CursorEnd)
	if job.Status == "" {
		job.Status = model.ProjectMemoryJobQueued
	}
	now := time.Now().UTC()
	if job.CreatedAt.IsZero() {
		job.CreatedAt = now
	}
	if job.UpdatedAt.IsZero() {
		job.UpdatedAt = now
	}
	return job
}

func (m *Memory) ScheduleProjectMemoryJob(ctx context.Context, job model.ProjectMemoryJob) (*model.ProjectMemoryJob, bool, error) {
	if archive, ok := m.projectMemoryArchive(); ok {
		return archive.ScheduleProjectMemoryJob(ctx, job)
	}
	job = normalizeProjectMemoryJob(job)
	if job.ID == "" || job.ScopeID == "" || job.OperatorID == 0 || job.MachineID == "" {
		return nil, false, errors.New("invalid project memory job")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	scope, found := m.projectScopes[projectMemoryScopeKey(job.OperatorID, job.MachineID, job.ScopeID)]
	if !found {
		return nil, false, errors.New("project scope not found")
	}
	for id, existing := range m.projectMemoryJobs {
		if existing.OperatorID != job.OperatorID || existing.ScopeID != job.ScopeID ||
			(existing.Status != model.ProjectMemoryJobQueued && existing.Status != model.ProjectMemoryJobClaimed && existing.Status != model.ProjectMemoryJobRunning) {
			continue
		}
		if job.ManualVisible {
			existing.ManualVisible = true
		}
		if existing.Status == model.ProjectMemoryJobQueued {
			if existing.CursorStart == "" {
				existing.CursorStart = job.CursorStart
			}
			if job.CursorEnd != "" {
				existing.CursorEnd = job.CursorEnd
			}
		} else if job.CursorEnd != "" && job.CursorEnd != existing.CursorEnd {
			existing.PendingCursorEnd = job.CursorEnd
		}
		existing.UpdatedAt = time.Now().UTC()
		m.projectMemoryJobs[id] = existing
		out := cloneProjectMemoryJob(existing)
		return &out, false, nil
	}
	job.InputRevision = scope.Revision
	m.projectMemoryJobs[job.ID] = cloneProjectMemoryJob(job)
	out := cloneProjectMemoryJob(job)
	return &out, true, nil
}

func (m *Memory) GetProjectMemoryJob(ctx context.Context, operatorID int64, jobID string) (*model.ProjectMemoryJob, error) {
	if archive, ok := m.projectMemoryArchive(); ok {
		return archive.GetProjectMemoryJob(ctx, operatorID, jobID)
	}
	m.mu.RLock()
	job, found := m.projectMemoryJobs[jobID]
	m.mu.RUnlock()
	if !found || job.OperatorID != operatorID {
		return nil, nil
	}
	out := cloneProjectMemoryJob(job)
	return &out, nil
}

func (m *Memory) ListProjectMemoryJobs(ctx context.Context, operatorID int64, scopeID string, limit int) ([]model.ProjectMemoryJob, error) {
	if archive, ok := m.projectMemoryArchive(); ok {
		return archive.ListProjectMemoryJobs(ctx, operatorID, scopeID, limit)
	}
	m.mu.RLock()
	out := make([]model.ProjectMemoryJob, 0)
	for _, job := range m.projectMemoryJobs {
		if job.OperatorID == operatorID && job.ScopeID == scopeID {
			out = append(out, cloneProjectMemoryJob(job))
		}
	}
	m.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *Memory) ListClaimableProjectMemoryJobs(ctx context.Context, now time.Time, limit int) ([]model.ProjectMemoryJob, error) {
	if archive, ok := m.projectMemoryArchive(); ok {
		return archive.ListClaimableProjectMemoryJobs(ctx, now, limit)
	}
	m.mu.RLock()
	out := make([]model.ProjectMemoryJob, 0)
	for _, job := range m.projectMemoryJobs {
		if projectMemoryQueuedReady(job, now) ||
			((job.Status == model.ProjectMemoryJobClaimed || job.Status == model.ProjectMemoryJobRunning) && !job.LeaseExpiresAt.IsZero() && job.LeaseExpiresAt.Before(now)) {
			out = append(out, cloneProjectMemoryJob(job))
		}
	}
	m.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *Memory) ClaimProjectMemoryJob(ctx context.Context, jobID, owner string, now, leaseUntil time.Time, maxPerDevice int) (*model.ProjectMemoryJob, error) {
	if archive, ok := m.projectMemoryArchive(); ok {
		return archive.ClaimProjectMemoryJob(ctx, jobID, owner, now, leaseUntil, maxPerDevice)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	job, found := m.projectMemoryJobs[jobID]
	if !found {
		return nil, nil
	}
	if job.Status != model.ProjectMemoryJobQueued && !(job.LeaseExpiresAt.Before(now) && job.Status != model.ProjectMemoryJobCompleted && job.Status != model.ProjectMemoryJobCancelled) {
		return nil, nil
	}
	if maxPerDevice > 0 {
		active := 0
		for id, current := range m.projectMemoryJobs {
			if id != job.ID && current.OperatorID == job.OperatorID && current.MachineID == job.MachineID &&
				(current.Status == model.ProjectMemoryJobClaimed || current.Status == model.ProjectMemoryJobRunning) &&
				!current.LeaseExpiresAt.IsZero() && !current.LeaseExpiresAt.Before(now) {
				active++
			}
		}
		if active >= maxPerDevice {
			return nil, nil
		}
	}
	job.Status = model.ProjectMemoryJobClaimed
	job.LeaseOwner = owner
	job.LeaseExpiresAt = leaseUntil
	job.FencingToken++
	job.Attempt++
	job.UpdatedAt = now.UTC()
	m.projectMemoryJobs[job.ID] = job
	out := cloneProjectMemoryJob(job)
	return &out, nil
}

func (m *Memory) RenewProjectMemoryJob(ctx context.Context, jobID, owner string, fencingToken int64, leaseUntil time.Time) (bool, error) {
	if archive, ok := m.projectMemoryArchive(); ok {
		return archive.RenewProjectMemoryJob(ctx, jobID, owner, fencingToken, leaseUntil)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	job, found := m.projectMemoryJobs[jobID]
	if !found || job.LeaseOwner != owner || job.FencingToken != fencingToken ||
		job.LeaseExpiresAt.IsZero() || job.LeaseExpiresAt.Before(time.Now().UTC()) {
		return false, nil
	}
	job.LeaseExpiresAt = leaseUntil
	job.UpdatedAt = time.Now().UTC()
	m.projectMemoryJobs[jobID] = job
	return true, nil
}

func (m *Memory) UpdateProjectMemoryJob(ctx context.Context, job model.ProjectMemoryJob) error {
	if archive, ok := m.projectMemoryArchive(); ok {
		return archive.UpdateProjectMemoryJob(ctx, job)
	}
	m.mu.Lock()
	current, found := m.projectMemoryJobs[job.ID]
	if !found {
		m.mu.Unlock()
		return errors.New("project memory job not found")
	}
	if current.FencingToken != job.FencingToken {
		m.mu.Unlock()
		return ErrProjectMemoryStaleJob
	}
	job.UpdatedAt = time.Now().UTC()
	m.projectMemoryJobs[job.ID] = cloneProjectMemoryJob(job)
	m.mu.Unlock()
	return nil
}

func (m *Memory) FinalizeProjectMemoryJob(ctx context.Context, job model.ProjectMemoryJob, successorID string) (*model.ProjectMemoryJob, error) {
	if archive, ok := m.projectMemoryArchive(); ok {
		return archive.FinalizeProjectMemoryJob(ctx, job, successorID)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	current, found := m.projectMemoryJobs[job.ID]
	if !found {
		return nil, errors.New("project memory job not found")
	}
	if current.FencingToken != job.FencingToken {
		return nil, ErrProjectMemoryStaleJob
	}
	job.UpdatedAt = time.Now().UTC()
	// Scheduling may attach a manual request or extend the cursor after the
	// worker loaded its snapshot. Preserve those fields from the locked record.
	job.ManualVisible = job.ManualVisible || current.ManualVisible
	pending := strings.TrimSpace(current.PendingCursorEnd)
	if pending == "" {
		pending = strings.TrimSpace(job.PendingCursorEnd)
	}
	job.PendingCursorEnd = ""
	m.projectMemoryJobs[job.ID] = cloneProjectMemoryJob(job)
	if pending == "" {
		return nil, nil
	}
	successor := normalizeProjectMemoryJob(model.ProjectMemoryJob{
		ID: successorID, OperatorID: job.OperatorID, MachineID: job.MachineID, ScopeID: job.ScopeID,
		Trigger: model.ProjectMemoryTriggerTaskComplete, Status: model.ProjectMemoryJobQueued,
		CursorStart: job.CursorCommitted, CursorEnd: pending, InputRevision: job.ResultRevision,
	})
	if successor.InputRevision <= 0 {
		if scope, ok := m.projectScopes[projectMemoryScopeKey(job.OperatorID, job.MachineID, job.ScopeID)]; ok {
			successor.InputRevision = scope.Revision
		}
	}
	m.projectMemoryJobs[successor.ID] = successor
	out := cloneProjectMemoryJob(successor)
	return &out, nil
}

func (m *Memory) MarkProjectMemoryJobManualVisible(ctx context.Context, operatorID int64, scopeID, jobID string) (bool, error) {
	if archive, ok := m.projectMemoryArchive(); ok {
		return archive.MarkProjectMemoryJobManualVisible(ctx, operatorID, scopeID, jobID)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	job, found := m.projectMemoryJobs[jobID]
	if !found || job.OperatorID != operatorID || job.ScopeID != scopeID ||
		(job.Status != model.ProjectMemoryJobQueued && job.Status != model.ProjectMemoryJobClaimed && job.Status != model.ProjectMemoryJobRunning) {
		return false, nil
	}
	job.ManualVisible = true
	job.UpdatedAt = time.Now().UTC()
	m.projectMemoryJobs[jobID] = job
	return true, nil
}

func (m *Memory) AppendProjectMemoryJobEvent(ctx context.Context, event model.ProjectMemoryJobEvent) error {
	if archive, ok := m.projectMemoryArchive(); ok {
		return archive.AppendProjectMemoryJobEvent(ctx, event)
	}
	m.mu.Lock()
	m.projectMemoryEventID++
	event.ID = m.projectMemoryEventID
	event.CreatedAt = time.Now().UTC()
	event.Sequence = int64(len(m.projectMemoryEvents[event.JobID]) + 1)
	m.projectMemoryEvents[event.JobID] = append(m.projectMemoryEvents[event.JobID], cloneProjectMemoryEvent(event))
	m.mu.Unlock()
	return nil
}

func (m *Memory) ListProjectMemoryJobEvents(ctx context.Context, operatorID int64, jobID string, afterSequence int64, visibleOnly bool) ([]model.ProjectMemoryJobEvent, error) {
	if archive, ok := m.projectMemoryArchive(); ok {
		return archive.ListProjectMemoryJobEvents(ctx, operatorID, jobID, afterSequence, visibleOnly)
	}
	m.mu.RLock()
	job, found := m.projectMemoryJobs[jobID]
	events := append([]model.ProjectMemoryJobEvent(nil), m.projectMemoryEvents[jobID]...)
	m.mu.RUnlock()
	if !found || job.OperatorID != operatorID || (!job.ManualVisible && visibleOnly) {
		return nil, nil
	}
	out := make([]model.ProjectMemoryJobEvent, 0, len(events))
	for _, event := range events {
		if event.Sequence > afterSequence {
			out = append(out, cloneProjectMemoryEvent(event))
		}
	}
	return out, nil
}

func (m *Memory) ReconcileProjectMemories(ctx context.Context, operatorID int64, scopeID string, expectedRevision int64, candidates []model.ProjectMemoryCandidate, invalidations []model.ProjectMemoryInvalidation, brief *model.ProjectMemoryBriefCandidate) (model.ProjectMemoryReconcileResult, error) {
	if archive, ok := m.projectMemoryArchive(); ok {
		return archive.ReconcileProjectMemories(ctx, operatorID, scopeID, expectedRevision, candidates, invalidations, brief)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var scope *model.ProjectScope
	for key, candidate := range m.projectScopes {
		if candidate.OperatorID == operatorID && candidate.ID == scopeID {
			copy := candidate
			scope = &copy
			m.projectScopes[key] = candidate
			break
		}
	}
	if scope == nil {
		return model.ProjectMemoryReconcileResult{}, errors.New("project scope not found")
	}
	if expectedRevision > 0 && scope.Revision != expectedRevision {
		return model.ProjectMemoryReconcileResult{}, ErrProjectMemoryRevisionConflict
	}
	result := model.ProjectMemoryReconcileResult{Revision: scope.Revision, AcceptedIDs: []string{}, InvalidatedIDs: []string{}}
	for _, invalidation := range invalidations {
		memory, found := m.projectMemories[invalidation.MemoryID]
		if !found || memory.ScopeID != scopeID || memory.ContentHash != invalidation.ContentHash ||
			(memory.Status != model.ProjectMemoryActive && memory.Status != model.ProjectMemoryDisputed) ||
			memory.Kind == model.ProjectMemoryLockedRule {
			continue
		}
		status := invalidation.Status
		if memory.Locked || memory.CreatedBy == "user" {
			status = model.ProjectMemoryDisputed
		}
		if status != model.ProjectMemoryStale && status != model.ProjectMemoryDisputed {
			continue
		}
		if memory.Status == status && (status != model.ProjectMemoryStale || memory.Verification.Status == model.ProjectMemoryStaleCheck) {
			continue
		}
		memory.Status = status
		if status == model.ProjectMemoryStale {
			memory.Verification.Status = model.ProjectMemoryStaleCheck
			memory.Verification.Method = "curator_invalidation"
			memory.Verification.EvidenceRefs = append([]string(nil), invalidation.EvidenceRefs...)
		}
		memory.UpdatedAt = time.Now().UTC()
		m.projectMemories[memory.ID] = cloneProjectMemory(memory)
		result.InvalidatedIDs = append(result.InvalidatedIDs, memory.ID)
		result.Changed = true
	}
	for _, candidate := range candidates {
		if candidate.ProjectScopeID != "" && candidate.ProjectScopeID != scopeID {
			return model.ProjectMemoryReconcileResult{}, errors.New("candidate scope mismatch")
		}
		memory := normalizeProjectMemory(model.ProjectMemory{
			ID: newProjectMemoryID("mem"), LogicalID: newProjectMemoryID("logical"), ScopeID: scopeID,
			Kind: candidate.Kind, SubjectKey: candidate.SubjectKey, Statement: candidate.Statement,
			Confidence: candidate.Confidence, ScopePaths: candidate.ScopePaths, Artifacts: candidate.Artifacts,
			Sources: candidate.Sources, Verification: candidate.Verification, Sensitive: candidate.Sensitive,
			CreatedBy: "curator",
		})
		if memory.SubjectKey == "" || memory.Statement == "" {
			continue
		}
		for _, artifact := range memory.Artifacts {
			if artifact.SHA256 == "" && artifact.APKSHA256 == "" {
				continue
			}
			for id, existing := range m.projectMemories {
				if existing.ScopeID != scopeID || (existing.Status != model.ProjectMemoryActive && existing.Status != model.ProjectMemoryDisputed) {
					continue
				}
				for _, oldArtifact := range existing.Artifacts {
					sameModule := artifact.ModuleName != "" && artifact.ModuleName == oldArtifact.ModuleName
					samePath := artifact.Path != "" && artifact.Path == oldArtifact.Path
					samePackage := artifact.PackageName != "" && artifact.PackageName == oldArtifact.PackageName
					soChanged := (sameModule || samePath) && oldArtifact.SHA256 != "" && artifact.SHA256 != "" && oldArtifact.SHA256 != artifact.SHA256
					apkChanged := samePackage && oldArtifact.APKSHA256 != "" && artifact.APKSHA256 != "" && oldArtifact.APKSHA256 != artifact.APKSHA256
					if soChanged || apkChanged {
						existing.Status = model.ProjectMemoryStale
						existing.Verification.Status = model.ProjectMemoryStaleCheck
						existing.UpdatedAt = time.Now().UTC()
						m.projectMemories[id] = cloneProjectMemory(existing)
						result.Changed = true
						break
					}
				}
			}
		}
		var latestSubject *model.ProjectMemory
		var same *model.ProjectMemory
		var tombstoned bool
		var conflicting []*model.ProjectMemory
		for id, existing := range m.projectMemories {
			if existing.ScopeID != scopeID || existing.SubjectKey != memory.SubjectKey {
				continue
			}
			if existing.Status == model.ProjectMemoryDeleted {
				tombstoned = true
				continue
			}
			if existing.ContentHash == memory.ContentHash {
				copy := cloneProjectMemory(existing)
				same = &copy
				_ = id
				break
			}
			if latestSubject == nil || existing.UpdatedAt.After(latestSubject.UpdatedAt) {
				copy := cloneProjectMemory(existing)
				latestSubject = &copy
			}
			if existing.Status == model.ProjectMemoryActive || existing.Status == model.ProjectMemoryDisputed {
				copy := cloneProjectMemory(existing)
				conflicting = append(conflicting, &copy)
			}
		}
		if tombstoned {
			continue
		}
		if same != nil {
			merged, changed := mergeProjectMemorySources(same.Sources, memory.Sources)
			if changed {
				same.Sources = merged
				same.UpdatedAt = time.Now().UTC()
				m.projectMemories[same.ID] = cloneProjectMemory(*same)
				result.Changed = true
			}
			result.AcceptedIDs = append(result.AcceptedIDs, same.ID)
			continue
		}
		if latestSubject != nil {
			memory.LogicalID = latestSubject.LogicalID
			memory.SupersedesID = latestSubject.ID
		}
		for _, existing := range conflicting {
			candidateIsNewerHumanEvidence := memory.Verification.Status == model.ProjectMemoryVerified &&
				!memory.Verification.VerifiedAt.IsZero() && memory.Verification.VerifiedAt.After(existing.UpdatedAt)
			if existing.Locked || (existing.CreatedBy == "user" && !candidateIsNewerHumanEvidence) ||
				(existing.Verification.Status == model.ProjectMemoryVerified && memory.Verification.Status == model.ProjectMemoryVerified) {
				existing.Status = model.ProjectMemoryDisputed
				m.projectMemories[existing.ID] = cloneProjectMemory(*existing)
				memory.Status = model.ProjectMemoryDisputed
			} else if memory.Verification.Status == model.ProjectMemoryVerified && !existing.Locked {
				existing.Status = model.ProjectMemorySuperseded
				m.projectMemories[existing.ID] = cloneProjectMemory(*existing)
				memory.SupersedesID = existing.ID
			}
		}
		m.projectMemories[memory.ID] = cloneProjectMemory(memory)
		result.AcceptedIDs = append(result.AcceptedIDs, memory.ID)
		result.Changed = true
	}
	if result.Changed || brief != nil {
		scope.Revision++
		result.Revision = scope.Revision
		for key, current := range m.projectScopes {
			if current.OperatorID == operatorID && current.ID == scopeID {
				current.Revision = scope.Revision
				current.UpdatedAt = time.Now().UTC()
				m.projectScopes[key] = current
			}
		}
	}
	if brief != nil && strings.TrimSpace(brief.Content) != "" {
		m.projectMemoryBriefs[scopeID] = model.ProjectMemoryBrief{ScopeID: scopeID, Content: brief.Content, SourceRevision: scope.Revision, GeneratedAt: time.Now().UTC()}
	}
	return result, nil
}

func (m *Memory) GetProjectMemoryBrief(ctx context.Context, operatorID int64, scopeID string) (*model.ProjectMemoryBrief, error) {
	if archive, ok := m.projectMemoryArchive(); ok {
		return archive.GetProjectMemoryBrief(ctx, operatorID, scopeID)
	}
	m.mu.RLock()
	owned := false
	for _, scope := range m.projectScopes {
		if scope.OperatorID == operatorID && scope.ID == scopeID {
			owned = true
			break
		}
	}
	brief, found := m.projectMemoryBriefs[scopeID]
	m.mu.RUnlock()
	if !found || !owned {
		return nil, nil
	}
	out := cloneProjectMemoryBrief(brief)
	return &out, nil
}

func (m *Memory) UpsertProjectMemoryBrief(ctx context.Context, brief model.ProjectMemoryBrief) error {
	if archive, ok := m.projectMemoryArchive(); ok {
		return archive.UpsertProjectMemoryBrief(ctx, brief)
	}
	m.mu.Lock()
	current, found := m.projectMemoryBriefs[brief.ScopeID]
	if !found || current.SourceRevision <= brief.SourceRevision {
		m.projectMemoryBriefs[brief.ScopeID] = cloneProjectMemoryBrief(brief)
	}
	m.mu.Unlock()
	return nil
}

func (m *Memory) RecordProjectMemoryUsage(ctx context.Context, usage model.ProjectMemoryUsage) error {
	if archive, ok := m.projectMemoryArchive(); ok {
		return archive.RecordProjectMemoryUsage(ctx, usage)
	}
	m.mu.Lock()
	m.projectMemoryUsages = append(m.projectMemoryUsages, usage)
	m.mu.Unlock()
	return nil
}

func (m *Memory) RecordProjectMemoryAudit(ctx context.Context, audit model.ProjectMemoryAudit) error {
	if archive, ok := m.projectMemoryArchive(); ok {
		return archive.RecordProjectMemoryAudit(ctx, audit)
	}
	m.mu.Lock()
	m.projectMemoryAudits = append(m.projectMemoryAudits, audit)
	m.mu.Unlock()
	return nil
}

func (m *Memory) UpdateProjectMemorySourceAvailability(ctx context.Context, operatorID int64, scopeID string, updates []model.ProjectMemorySourceAvailability) (int64, error) {
	if archive, ok := m.projectMemoryArchive(); ok {
		return archive.UpdateProjectMemorySourceAvailability(ctx, operatorID, scopeID, updates)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	owned := false
	for _, scope := range m.projectScopes {
		if scope.OperatorID == operatorID && scope.ID == scopeID {
			owned = true
			break
		}
	}
	if !owned {
		return 0, errors.New("project scope not found")
	}
	var changed int64
	for _, update := range updates {
		memory, found := m.projectMemories[update.MemoryID]
		if !found || memory.ScopeID != scopeID {
			continue
		}
		for index := range memory.Sources {
			source := &memory.Sources[index]
			if source.RelativePath != update.RelativePath || (source.Status == update.Status && (update.SHA256 == "" || source.SHA256 == update.SHA256)) {
				continue
			}
			source.Status = update.Status
			if update.SHA256 != "" {
				source.SHA256 = update.SHA256
			}
			changed++
		}
		m.projectMemories[update.MemoryID] = cloneProjectMemory(memory)
	}
	if changed > 0 {
		for key, scope := range m.projectScopes {
			if scope.OperatorID == operatorID && scope.ID == scopeID {
				scope.Revision++
				scope.UpdatedAt = time.Now().UTC()
				m.projectScopes[key] = scope
				break
			}
		}
	}
	return changed, nil
}

func projectMemoryRollup(scopeID string, items []model.ProjectMemory, reason string) model.ProjectMemory {
	parts := make([]string, 0, len(items))
	sources := make([]model.ProjectMemorySource, 0, 32)
	for _, item := range items {
		entry := strings.TrimSpace(item.SubjectKey + ": " + item.Statement)
		if entry != ":" {
			parts = append(parts, entry)
		}
		for _, source := range item.Sources {
			if len(sources) >= 32 {
				break
			}
			sources = append(sources, source)
		}
	}
	statement := fmt.Sprintf("Project memory %s rollup (%d records): %s", reason, len(items), strings.Join(parts, " | "))
	if len(statement) > 2_000 {
		statement = statement[:2_000]
	}
	now := time.Now().UTC()
	return normalizeProjectMemory(model.ProjectMemory{
		ID: newProjectMemoryID("mem"), LogicalID: newProjectMemoryID("logical"), ScopeID: scopeID,
		Kind: model.ProjectMemoryEpisode, SubjectKey: "episode:retention:" + reason + ":" + now.Format("20060102150405"),
		Statement: statement, Status: model.ProjectMemoryActive, Confidence: 1, Sources: sources,
		Verification: model.ProjectMemoryVerification{Status: model.ProjectMemoryVerified, Method: "deterministic_rollup", VerifiedAt: now},
		CreatedBy:    "maintenance", CreatedAt: now, UpdatedAt: now,
	})
}

func projectMemoryOldest(items []model.ProjectMemory) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			return items[i].ID < items[j].ID
		}
		return items[i].UpdatedAt.Before(items[j].UpdatedAt)
	})
}

func (m *Memory) MaintainProjectMemoryRetention(ctx context.Context, activeLimit, historyLimit, maxScopes int) (model.ProjectMemoryRetentionResult, error) {
	if archive, ok := m.projectMemoryArchive(); ok {
		return archive.MaintainProjectMemoryRetention(ctx, activeLimit, historyLimit, maxScopes)
	}
	if activeLimit <= 0 {
		activeLimit = 20_000
	}
	if historyLimit <= 0 {
		historyLimit = 200_000
	}
	if maxScopes <= 0 {
		maxScopes = 100
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	result := model.ProjectMemoryRetentionResult{}
	for scopeKey, scope := range m.projectScopes {
		if result.ScopesProcessed >= int64(maxScopes) {
			break
		}
		result.ScopesProcessed++
		changed := false
		history := []model.ProjectMemory{}
		for _, memory := range m.projectMemories {
			if memory.ScopeID != scope.ID || memory.Locked || memory.Kind == model.ProjectMemoryLockedRule || memory.Status == model.ProjectMemoryDeleted {
				continue
			}
			if memory.Status != model.ProjectMemoryActive && memory.Status != model.ProjectMemoryDisputed {
				history = append(history, memory)
			}
		}
		if len(history) > historyLimit {
			projectMemoryOldest(history)
			count := len(history) - historyLimit
			rolled := append([]model.ProjectMemory(nil), history[:count]...)
			for _, memory := range rolled {
				delete(m.projectMemories, memory.ID)
			}
			rollup := projectMemoryRollup(scope.ID, rolled, "history")
			m.projectMemories[rollup.ID] = rollup
			result.PurgedHistory += int64(count)
			result.RolledUp++
			changed = true
		}

		active := []model.ProjectMemory{}
		for _, memory := range m.projectMemories {
			if memory.ScopeID == scope.ID && !memory.Locked && memory.Kind != model.ProjectMemoryLockedRule &&
				(memory.Status == model.ProjectMemoryActive || memory.Status == model.ProjectMemoryDisputed) {
				active = append(active, memory)
			}
		}
		if len(active) > activeLimit {
			projectMemoryOldest(active)
			count := len(active) - activeLimit + 1
			rolled := append([]model.ProjectMemory(nil), active[:count]...)
			for _, memory := range rolled {
				memory.Status = model.ProjectMemoryArchived
				memory.UpdatedAt = time.Now().UTC()
				m.projectMemories[memory.ID] = memory
			}
			rollup := projectMemoryRollup(scope.ID, rolled, "active")
			m.projectMemories[rollup.ID] = rollup
			result.Archived += int64(count)
			result.RolledUp++
			changed = true
		}
		if changed {
			scope.Revision++
			scope.UpdatedAt = time.Now().UTC()
			m.projectScopes[scopeKey] = scope
		}
	}
	return result, nil
}

func (m *Memory) PurgeDeletedProjectMemories(ctx context.Context, before time.Time, limit int) (int64, error) {
	if archive, ok := m.projectMemoryArchive(); ok {
		return archive.PurgeDeletedProjectMemories(ctx, before, limit)
	}
	if limit <= 0 {
		limit = 1_000
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var purged int64
	for id, memory := range m.projectMemories {
		if memory.Status == model.ProjectMemoryDeleted && memory.UpdatedAt.Before(before) {
			delete(m.projectMemories, id)
			purged++
			if purged >= int64(limit) {
				break
			}
		}
	}
	return purged, nil
}
