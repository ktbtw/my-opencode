package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"relay-server/internal/model"
)

var runtimePreflightStatusLocks [64]sync.Mutex

func (a *API) StartDeviceAgentSemanticPreflight(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	agentID := strings.TrimSpace(chi.URLParam(r, "agentID"))
	if machineID == "" || agentID == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "缺少设备或 Agent"})
		return
	}
	if _, ok := a.broker.GetMachine(operator.ID, machineID); !ok {
		write(w, http.StatusConflict, map[string]string{"error": "设备不在线或无权限"})
		return
	}
	var req model.RuntimePreflightRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	req.SemanticAgentID = strings.TrimSpace(req.SemanticAgentID)
	if req.SemanticAgentID == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "缺少 semantic_agent_id"})
		return
	}
	profile, err := a.semanticAgentByID(req.SemanticAgentID, false)
	if err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	launcherID, err := a.deviceLauncherAgent(operator.ID, machineID)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": "launcher 不在线或无权限"})
		return
	}
	runtimes, err := a.runtimeCatalogSnapshotForAgent(profile)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	now := time.Now().UTC()
	job := model.RuntimeInstallJob{
		ID:              fmt.Sprintf("preflight_%d", time.Now().UnixNano()),
		OperatorID:      operator.ID,
		MachineID:       machineID,
		LauncherAgentID: agentID,
		SemanticAgentID: profile.ID,
		Status:          "pending",
		CurrentStep:     "runtime",
		ProgressPercent: 0,
		RequestedBy:     operator.Username,
		StartedAt:       now,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	stored, err := a.store.UpsertRuntimeInstallJob(job)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	items := initialRuntimePreflightItems(stored.ID, profile, runtimes, req.DisableVerifyMCP)
	if err := a.store.ReplaceRuntimeInstallJobItems(stored.ID, items); err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	_, _ = a.store.AppendRuntimeInstallEvent(model.RuntimeInstallEvent{
		JobID:           stored.ID,
		ItemType:        "job",
		Phase:           "dispatch",
		Status:          "info",
		Message:         "已创建语义 Agent 切换预检任务",
		ProgressPercent: 1,
	})
	// Persist the waiting marker before dispatch. A fast launcher response must
	// always be ordered after this marker in the event stream.
	_, _ = a.store.AppendRuntimeInstallEvent(model.RuntimeInstallEvent{
		JobID:           stored.ID,
		ItemType:        "job",
		Phase:           "dispatch",
		Status:          "running",
		Message:         "预检任务正在下发，等待 launcher 开始执行",
		ProgressPercent: 3,
	})
	payload := model.RuntimePreflightStartPayload{
		JobID:               stored.ID,
		MachineID:           machineID,
		LauncherAgentID:     agentID,
		SemanticAgentID:     profile.ID,
		ApplyRecommendedMCP: req.ApplyRecommendedMCP,
		DisableVerifyMCP:    req.DisableVerifyMCP,
		AutoRepair:          req.AutoRepair,
		RestartAfterApply:   req.RestartAfterApply,
		SemanticAgent:       &profile,
		Runtimes:            runtimes,
	}
	if err := a.broker.DispatchForOperator(r.Context(), operator.ID, launcherID, model.Envelope{
		Type:      "device.launcher.agent_preflight.start",
		RequestID: stored.ID,
		SentAt:    time.Now().UTC().Format(time.RFC3339),
		Payload:   payload,
	}); err != nil {
		failed := *stored
		failed.Status = "failed"
		failed.Error = err.Error()
		failed.CompletedAt = time.Now().UTC()
		_, _ = a.store.UpsertRuntimeInstallJob(failed)
		_, _ = a.store.AppendRuntimeInstallEvent(model.RuntimeInstallEvent{
			JobID:           stored.ID,
			ItemType:        "job",
			Phase:           "dispatch",
			Status:          "error",
			Message:         "预检任务下发失败",
			ProgressPercent: 0,
			Details:         map[string]any{"error": err.Error()},
		})
		write(w, http.StatusConflict, map[string]string{"error": "预检任务下发失败：" + err.Error()})
		return
	}
	// Status reports can race the successful dispatch response. Only advance a
	// still-pending job here; never roll back a status or progress already
	// persisted by launcher.
	if current, ok := a.store.GetRuntimeInstallJob(0, stored.ID); ok {
		stored = current
		if stored.Status == "pending" {
			stored.Status = "running"
			stored.CurrentStep = "runtime"
			if stored.ProgressPercent < 3 {
				stored.ProgressPercent = 3
			}
			stored, _ = a.store.UpsertRuntimeInstallJob(*stored)
		}
	} else {
		stored.Status = "running"
		stored.CurrentStep = "runtime"
		if stored.ProgressPercent < 3 {
			stored.ProgressPercent = 3
		}
		stored, _ = a.store.UpsertRuntimeInstallJob(*stored)
	}
	events, _ := a.store.ListRuntimeInstallEvents(stored.ID, 0)
	stored.Items = items
	stored.LatestEvents = events
	write(w, http.StatusOK, map[string]any{"job": stored, "job_id": stored.ID})
}

func (a *API) GetDeviceAgentSemanticPreflightJob(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	jobID := chi.URLParam(r, "jobID")
	job, ok := a.store.GetRuntimeInstallJob(operator.ID, jobID)
	if !ok {
		write(w, http.StatusNotFound, map[string]string{"error": "预检任务不存在"})
		return
	}
	write(w, http.StatusOK, map[string]any{"job": job})
}

func (a *API) GetDeviceAgentSemanticPreflightEvents(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	jobID := chi.URLParam(r, "jobID")
	if _, ok := a.store.GetRuntimeInstallJob(operator.ID, jobID); !ok {
		write(w, http.StatusNotFound, map[string]string{"error": "预检任务不存在"})
		return
	}
	after := int64(0)
	if raw := strings.TrimSpace(r.URL.Query().Get("after")); raw != "" {
		_, _ = fmt.Sscanf(raw, "%d", &after)
	}
	events, err := a.store.ListRuntimeInstallEvents(jobID, after)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, map[string]any{"items": events})
}

func (a *API) HandleRuntimePreflightStatus(payload model.RuntimePreflightStatusPayload) error {
	jobID := strings.TrimSpace(payload.JobID)
	if jobID == "" {
		return fmt.Errorf("预检状态缺少 job_id")
	}
	lock := runtimePreflightStatusLock(jobID)
	lock.Lock()
	defer lock.Unlock()
	job, ok := a.store.GetRuntimeInstallJob(0, jobID)
	if !ok {
		return fmt.Errorf("预检任务不存在")
	}
	incomingStatus := strings.TrimSpace(payload.Status)
	if incomingStatus != "" && !validRuntimePreflightStatus(incomingStatus) {
		return fmt.Errorf("预检状态无效：%s", incomingStatus)
	}
	if incomingStatus != "" && !validRuntimePreflightStatusTransition(job.Status, incomingStatus) {
		return nil
	}
	if incomingStatus != "" {
		job.Status = incomingStatus
	}
	if step := strings.TrimSpace(payload.CurrentStep); step != "" &&
		runtimePreflightStepRank(step) >= runtimePreflightStepRank(job.CurrentStep) {
		job.CurrentStep = step
	}
	nextProgress := clampPercent(payload.ProgressPercent)
	if nextProgress < job.ProgressPercent {
		nextProgress = job.ProgressPercent
	}
	job.ProgressPercent = nextProgress
	if job.Status == "completed" {
		job.Error = ""
	} else if payload.Error != "" {
		job.Error = payload.Error
	}
	if payload.SemanticAgentID != "" {
		job.SemanticAgentID = payload.SemanticAgentID
	}
	if payload.LauncherAgentID != "" {
		job.LauncherAgentID = payload.LauncherAgentID
	}
	if payload.MachineID != "" {
		job.MachineID = payload.MachineID
	}
	job.RequiresUserAction = payload.RequiresUserAction
	if payload.RequiresUserAction {
		action := normalizeRuntimeUserAction(jobID, payload.UserAction)
		job.UserAction = &action
	} else {
		job.UserAction = nil
	}
	switch job.Status {
	case "completed", "failed", "cancelled":
		job.RequiresUserAction = false
		job.UserAction = nil
		if job.CompletedAt.IsZero() {
			job.CompletedAt = time.Now().UTC()
		}
	}
	if len(payload.Items) > 0 {
		if err := a.store.ReplaceRuntimeInstallJobItems(jobID, payload.Items); err != nil {
			return err
		}
	}
	stored, err := a.store.UpsertRuntimeInstallJob(*job)
	if err != nil {
		return err
	}
	for _, event := range payload.Events {
		event.JobID = jobID
		if event.ProgressPercent == 0 {
			event.ProgressPercent = stored.ProgressPercent
		}
		if _, err := a.store.AppendRuntimeInstallEvent(event); err != nil {
			return err
		}
	}
	if stored.RequiresUserAction && stored.UserAction != nil {
		claimed, err := a.store.ClaimRuntimeUserActionNotification(stored.OperatorID, stored.ID, stored.UserAction.ID, "email")
		if err != nil {
			return err
		}
		if claimed && a.notifyService != nil {
			jobSnapshot := *stored
			actionSnapshot := *stored.UserAction
			go a.notifyService.NotifyRuntimeUserAction(context.Background(), jobSnapshot, actionSnapshot)
		}
	}
	return nil
}

func runtimePreflightStatusLock(jobID string) *sync.Mutex {
	var hash uint32 = 2166136261
	for index := 0; index < len(jobID); index++ {
		hash ^= uint32(jobID[index])
		hash *= 16777619
	}
	return &runtimePreflightStatusLocks[hash%uint32(len(runtimePreflightStatusLocks))]
}

func validRuntimePreflightStatus(status string) bool {
	switch status {
	case "pending", "running", "waiting_user_action", "completed", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func isTerminalRuntimePreflightStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case "completed", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func validRuntimePreflightStatusTransition(current string, next string) bool {
	current = strings.TrimSpace(current)
	next = strings.TrimSpace(next)
	if current == "" || current == next {
		return true
	}
	if isTerminalRuntimePreflightStatus(current) {
		return false
	}
	if next == "pending" {
		return current == "pending"
	}
	return true
}

func runtimePreflightStepRank(step string) int {
	switch strings.ToLower(strings.TrimSpace(step)) {
	case "runtime":
		return 1
	case "mcp":
		return 2
	case "skill":
		return 3
	case "mcp_ready", "apply":
		return 4
	case "completed":
		return 5
	default:
		return 0
	}
}

func normalizeRuntimeUserAction(jobID string, input *model.RuntimeUserAction) model.RuntimeUserAction {
	action := model.RuntimeUserAction{}
	if input != nil {
		action = *input
		action.Instructions = append([]string(nil), input.Instructions...)
	}
	action.ID = strings.TrimSpace(action.ID)
	if action.ID == "" {
		action.ID = "runtime-action:" + strings.TrimSpace(jobID)
	}
	action.Kind = strings.TrimSpace(action.Kind)
	if action.Kind == "" {
		action.Kind = "computer_confirmation"
	}
	action.Title = strings.TrimSpace(action.Title)
	if action.Title == "" {
		action.Title = "需要在电脑上确认"
	}
	action.Message = strings.TrimSpace(action.Message)
	if action.Message == "" {
		action.Message = "请在目标电脑完成系统提示的操作。"
	}
	instructions := make([]string, 0, len(action.Instructions))
	for _, instruction := range action.Instructions {
		if instruction = strings.TrimSpace(instruction); instruction != "" {
			instructions = append(instructions, instruction)
		}
	}
	action.Instructions = instructions
	if action.RequestedAt.IsZero() {
		action.RequestedAt = time.Now().UTC()
	}
	return action
}

func (a *API) runtimeCatalogSnapshotForAgent(profile model.SemanticAgentProfile) ([]model.RuntimeCatalogItem, error) {
	all, err := a.store.ListRuntimeCatalog(false)
	if err != nil {
		return nil, err
	}
	byID := map[string]model.RuntimeCatalogItem{}
	for _, item := range all {
		byID[item.ID] = item
	}
	out := make([]model.RuntimeCatalogItem, 0, len(profile.RuntimeRequirements))
	seen := map[string]bool{}
	for _, req := range profile.RuntimeRequirements {
		item, ok := byID[req.RuntimeID]
		if !ok {
			continue
		}
		if seen[item.ID] {
			continue
		}
		out = append(out, item)
		seen[item.ID] = true
		if strings.EqualFold(strings.TrimSpace(item.InstallStrategy), "uv_python") {
			if uv, ok := byID["uv"]; ok && !seen[uv.ID] {
				out = append(out, uv)
				seen[uv.ID] = true
			}
		}
		if strings.EqualFold(strings.TrimSpace(item.InstallStrategy), "npm_global_tool") {
			if node, ok := byID["node"]; ok && !seen[node.ID] {
				out = append(out, node)
				seen[node.ID] = true
			}
		}
		if strings.EqualFold(strings.TrimSpace(item.InstallStrategy), "ida_installer") {
			for _, dependencyID := range []string{"python", "uv"} {
				if dependency, ok := byID[dependencyID]; ok && !seen[dependency.ID] {
					out = append(out, dependency)
					seen[dependency.ID] = true
				}
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].SortOrder < out[j].SortOrder
	})
	return out, nil
}

func initialRuntimePreflightItems(jobID string, profile model.SemanticAgentProfile, runtimes []model.RuntimeCatalogItem, disableVerifyMCP bool) []model.RuntimeInstallJobItem {
	if disableVerifyMCP {
		profile = withoutVerifyMCP(profile)
	}
	items := make([]model.RuntimeInstallJobItem, 0, len(profile.RuntimeRequirements)+len(profile.MCPIDs)+len(profile.MCPDependencies)+len(profile.SkillIDs)+1)
	runtimeNames := map[string]string{}
	for _, runtime := range runtimes {
		runtimeNames[runtime.ID] = runtime.Name
	}
	mcpNames := map[string]string{}
	for index, id := range profile.MCPIDs {
		name := id
		if index < len(profile.RecommendedMCPServers) && strings.TrimSpace(profile.RecommendedMCPServers[index]) != "" {
			name = strings.TrimSpace(profile.RecommendedMCPServers[index])
		}
		mcpNames[id] = name
	}
	for _, dependency := range profile.MCPDependencies {
		id := strings.TrimSpace(dependency.ID)
		if id == "" {
			id = strings.TrimSpace(dependency.Name)
		}
		if id == "" {
			continue
		}
		mcpNames[id] = firstNonEmpty(dependency.Title, dependency.Name, mcpNames[id], id)
	}
	for _, req := range profile.RuntimeRequirements {
		name := runtimeNames[req.RuntimeID]
		if name == "" {
			name = req.RuntimeID
		}
		items = append(items, model.RuntimeInstallJobItem{
			JobID:    jobID,
			ItemType: "runtime",
			ItemID:   req.RuntimeID,
			Name:     name,
			Required: req.Required,
			Status:   "pending",
		})
	}
	seenMCP := map[string]bool{}
	for _, id := range profile.MCPIDs {
		id = strings.TrimSpace(id)
		if id == "" || seenMCP[id] {
			continue
		}
		seenMCP[id] = true
		name := mcpNames[id]
		if name == "" {
			name = id
		}
		items = append(items, model.RuntimeInstallJobItem{
			JobID:    jobID,
			ItemType: "mcp",
			ItemID:   id,
			Name:     name,
			Required: true,
			Status:   "pending",
		})
	}
	for _, dependency := range profile.MCPDependencies {
		id := strings.TrimSpace(dependency.ID)
		if id == "" {
			id = strings.TrimSpace(dependency.Name)
		}
		if id == "" || seenMCP[id] {
			continue
		}
		seenMCP[id] = true
		items = append(items, model.RuntimeInstallJobItem{
			JobID:    jobID,
			ItemType: "mcp",
			ItemID:   id,
			Name:     firstNonEmpty(dependency.Title, dependency.Name, id),
			Required: true,
			Status:   "pending",
		})
	}
	for _, id := range profile.SkillIDs {
		items = append(items, model.RuntimeInstallJobItem{
			JobID:    jobID,
			ItemType: "skill",
			ItemID:   id,
			Name:     id,
			Required: true,
			Status:   "pending",
		})
	}
	items = append(items, model.RuntimeInstallJobItem{
		JobID:    jobID,
		ItemType: "agent_apply",
		ItemID:   profile.ID,
		Name:     profile.Name,
		Required: true,
		Status:   "pending",
	})
	return items
}

func withoutVerifyMCP(profile model.SemanticAgentProfile) model.SemanticAgentProfile {
	filteredIDs := make([]string, 0, len(profile.MCPIDs))
	filteredServers := make([]string, 0, len(profile.RecommendedMCPServers))
	for index, id := range profile.MCPIDs {
		name := id
		if index < len(profile.RecommendedMCPServers) {
			name = profile.RecommendedMCPServers[index]
		}
		if isVerifyMCPName(id) || isVerifyMCPName(name) {
			continue
		}
		filteredIDs = append(filteredIDs, id)
		if index < len(profile.RecommendedMCPServers) {
			filteredServers = append(filteredServers, profile.RecommendedMCPServers[index])
		}
	}
	for index := len(profile.MCPIDs); index < len(profile.RecommendedMCPServers); index++ {
		name := profile.RecommendedMCPServers[index]
		if !isVerifyMCPName(name) {
			filteredServers = append(filteredServers, name)
		}
	}
	profile.MCPIDs = filteredIDs
	profile.RecommendedMCPServers = filteredServers
	profile.RecommendedMCPConfigs = filterVerifyMCPConfigs(profile.RecommendedMCPConfigs)
	filteredDependencies := make([]model.SemanticAgentMCPDependency, 0, len(profile.MCPDependencies))
	for _, dependency := range profile.MCPDependencies {
		if isVerifyMCPName(dependency.ID) || isVerifyMCPName(dependency.Name) ||
			isVerifyMCPName(dependency.Title) || isVerifyMCPName(dependency.Config.Name) {
			continue
		}
		filteredDependencies = append(filteredDependencies, dependency)
	}
	profile.MCPDependencies = filteredDependencies
	return profile
}

func filterVerifyMCPConfigs(input []model.DeviceMCPServer) []model.DeviceMCPServer {
	out := make([]model.DeviceMCPServer, 0, len(input))
	for _, server := range input {
		if isVerifyMCPName(server.Name) {
			continue
		}
		out = append(out, server)
	}
	return out
}

func isVerifyMCPName(value string) bool {
	return strings.Contains(strings.ToLower(strings.TrimSpace(value)), "verify")
}

func clampPercent(value int) int {
	if value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}
	return value
}
