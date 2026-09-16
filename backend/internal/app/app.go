package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"

	"relay-server/internal/api"
	"relay-server/internal/auth"
	"relay-server/internal/broker"
	"relay-server/internal/mail"
	"relay-server/internal/model"
	"relay-server/internal/notify"
	"relay-server/internal/overlay"
	"relay-server/internal/projectmemory"
	"relay-server/internal/push"
	"relay-server/internal/store"
)

const deviceWebSocketReadLimit = 64 << 20
const sessionHistoryRequestTimeout = 15 * time.Second

const tunnelClientPath = "/ws/tunnel/client"
const tunnelDevicePath = "/ws/tunnel/device"

const taskResumeCapability = "task_resume_v1"

func legacyProjectScopeID(operatorID int64, machineID, root, projectID string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d\x00%s\x00%s\x00%s", operatorID, machineID, root, projectID)))
	return "proj_legacy_" + hex.EncodeToString(sum[:12])
}

func projectLocationID(scopeID, volumeID, fileID, root string) string {
	sum := sha256.Sum256([]byte(scopeID + "\x00" + volumeID + "\x00" + fileID + "\x00" + root))
	return "loc_" + hex.EncodeToString(sum[:12])
}

func (a *App) registerHelloProjectScopes(ctx context.Context, operatorID int64, agentID string, hello *model.HelloPayload) {
	if hello == nil || strings.TrimSpace(hello.MachineID) == "" {
		return
	}
	now := time.Now().UTC()
	for index := range hello.Projects {
		project := &hello.Projects[index]
		project.Root = strings.TrimSpace(project.Root)
		project.ProjectID = strings.TrimSpace(project.ProjectID)
		project.ScopeID = strings.TrimSpace(project.ScopeID)
		if project.ScopeID == "" {
			project.ScopeID = legacyProjectScopeID(operatorID, hello.MachineID, project.Root, project.ProjectID)
		}
		displayName := strings.TrimSpace(project.DisplayName)
		if displayName == "" && project.Root != "" {
			displayName = filepath.Base(project.Root)
		}
		if displayName == "" {
			displayName = project.ProjectID
		}
		scope, err := a.store.GetProjectScope(ctx, operatorID, hello.MachineID, project.ScopeID)
		if err != nil {
			log.Printf("[project-memory] scope lookup failed agent_id=%s scope_id=%s err=%v", agentID, project.ScopeID, err)
			continue
		}
		if scope == nil {
			scope = &model.ProjectScope{
				ID: project.ScopeID, OperatorID: operatorID, MachineID: hello.MachineID,
				DisplayName: displayName, CurrentRoot: project.Root, VolumeID: project.VolumeID,
				FileID: project.FileID, InstanceNonce: project.InstanceNonce,
				LineageScopeID: project.LineageScopeID,
				BindingEpoch:   1, Status: model.ProjectScopeActive, Revision: 1,
				CreatedAt: now, UpdatedAt: now,
			}
		} else {
			if scope.CurrentRoot != project.Root || scope.VolumeID != project.VolumeID || scope.FileID != project.FileID {
				scope.BindingEpoch++
			}
			scope.DisplayName = displayName
			scope.CurrentRoot = project.Root
			scope.VolumeID = project.VolumeID
			scope.FileID = project.FileID
			scope.InstanceNonce = project.InstanceNonce
			if scope.LineageScopeID == "" {
				scope.LineageScopeID = project.LineageScopeID
			}
			scope.Status = model.ProjectScopeActive
			scope.UpdatedAt = now
		}
		if err := a.store.UpsertProjectScope(ctx, *scope); err != nil {
			log.Printf("[project-memory] scope upsert failed agent_id=%s scope_id=%s err=%v", agentID, project.ScopeID, err)
			continue
		}
		project.BindingEpoch = scope.BindingEpoch
		_ = a.store.UpsertProjectScopeLocation(ctx, model.ProjectScopeLocation{
			ID:      projectLocationID(scope.ID, project.VolumeID, project.FileID, project.Root),
			ScopeID: scope.ID, OperatorID: operatorID, MachineID: hello.MachineID,
			Root: project.Root, DisplayName: displayName, VolumeID: project.VolumeID,
			FileID: project.FileID, MarkerNonce: project.InstanceNonce, ClaimState: "active",
			BindingEpoch: scope.BindingEpoch, Reachable: true, FirstSeenAt: now, LastSeenAt: now,
		})
		_ = a.store.UpsertProjectAgentBinding(ctx, model.ProjectAgentBinding{
			OperatorID: operatorID, MachineID: hello.MachineID, AgentID: agentID,
			ScopeID: scope.ID, BindingEpoch: scope.BindingEpoch, Active: true, AttachedAt: now,
		})
	}
}

type taskLatencyState struct {
	firstTextDeltaLogged      bool
	firstReasoningDeltaLogged bool
}

type App struct {
	store                *store.Memory
	broker               *broker.Broker
	auth                 *auth.Manager
	mailer               *mail.Mailer
	notifyService        *notify.Service
	pushService          *push.Service
	taskCleanupTick      time.Duration
	taskTimeout          time.Duration
	goalHeartbeatTimeout time.Duration
	projectMemoryEnabled bool
	taskCancelGrace      time.Duration
	taskDisconnectGrace  time.Duration
	artifactMu           sync.Mutex
	artifactSubs         map[string]chan artifactStreamEvent
	aiConfigMu           sync.Mutex
	aiConfigSubs         map[string]chan model.DeviceAIConfigResultPayload
	goalOptimizeMu       sync.Mutex
	goalOptimizeSubs     map[string]chan model.GoalOptimizeResultPayload
	sessionHistoryMu     sync.Mutex
	sessionHistorySubs   map[string]chan model.SessionHistoryResultPayload
	mcpConfigMu          sync.Mutex
	mcpConfigSubs        map[string]chan model.DeviceMCPConfigResultPayload
	envConfigMu          sync.Mutex
	envConfigSubs        map[string]chan model.DeviceEnvConfigResultPayload
	launcherMu           sync.Mutex
	launcherSubs         map[string]chan model.DeviceLauncherResultPayload
	directoriesMu        sync.Mutex
	directoriesSubs      map[string]chan model.DeviceDirectoriesResultPayload
	tunnels              *tunnelManager
	launcherVersionFile  string
	latencyMu            sync.Mutex
	taskLatency          map[string]taskLatencyState
	overlayHub           *overlay.Hub
}

func New() (*App, error) {
	archive, err := store.NewTaskArchiveFromEnv()
	if err != nil {
		return nil, err
	}
	log.Printf("mysql archive initialized")
	a := &App{
		store:                store.NewMemory(archive),
		broker:               broker.New(),
		auth:                 auth.NewManagerWithSecret(7*24*time.Hour, loadAuthSecret()),
		mailer:               mail.New(mail.DefaultConfig()),
		taskCleanupTick:      envDuration("TASK_CLEANUP_INTERVAL", 5*time.Second),
		taskTimeout:          envDuration("TASK_TIMEOUT", 30*time.Minute),
		goalHeartbeatTimeout: envDuration("GOAL_HEARTBEAT_TIMEOUT", 3*time.Minute),
		projectMemoryEnabled: envEnabled("CHAT_CODEX_PROJECT_MEMORY_ENABLED", true),
		taskCancelGrace:      envDuration("TASK_CANCEL_GRACE", 15*time.Second),
		taskDisconnectGrace:  envDuration("TASK_DISCONNECT_GRACE", 45*time.Second),
		artifactSubs:         map[string]chan artifactStreamEvent{},
		aiConfigSubs:         map[string]chan model.DeviceAIConfigResultPayload{},
		goalOptimizeSubs:     map[string]chan model.GoalOptimizeResultPayload{},
		sessionHistorySubs:   map[string]chan model.SessionHistoryResultPayload{},
		mcpConfigSubs:        map[string]chan model.DeviceMCPConfigResultPayload{},
		envConfigSubs:        map[string]chan model.DeviceEnvConfigResultPayload{},
		launcherSubs:         map[string]chan model.DeviceLauncherResultPayload{},
		directoriesSubs:      map[string]chan model.DeviceDirectoriesResultPayload{},
		tunnels:              newTunnelManager(),
		overlayHub:           overlay.NewHub(),
		launcherVersionFile:  tunnelLauncherVersionFile(),
		taskLatency:          map[string]taskLatencyState{},
	}
	a.pushService = push.NewService(a.store, push.NewJPushSenderFromEnv())
	a.notifyService = notify.NewService(a.store, a.pushService, a.mailer, mail.DefaultConfig().Brand)
	a.store.SetTaskObserver(func(task *model.Task) {
		if task != nil {
			a.overlayHub.Publish(task.OperatorID, "task.updated", task)
		}
	})
	a.store.SetAppNotificationObserver(func(operatorID int64, change model.AppNotificationChange) {
		a.overlayHub.Publish(operatorID, "notification.updated", change)
	})
	a.broker.SetObserver(
		func(operatorID int64, agent model.Agent) {
			a.overlayHub.Publish(operatorID, "agent.upsert", agent)
		},
		func(operatorID int64, agent model.Agent) {
			a.overlayHub.Publish(operatorID, "agent.remove", map[string]string{
				"agent_id": agent.ID, "machine_id": agent.MachineID,
			})
		},
	)
	// 设备指标独立成事件：它挂在机器维度，且更新频率高于 agent 状态，
	// 客户端可就地合并而不必重取整个设备。
	a.broker.SetMetricsObserver(func(operatorID int64, update model.MachineMetricsUpdate) {
		a.overlayHub.Publish(operatorID, "device.metrics", update)
	})
	go a.cleanStuckTasks()
	if a.projectMemoryEnabled {
		go a.projectMemoryLoop()
	}
	return a, nil
}

func (a *App) Close() error {
	if a == nil || a.store == nil {
		return nil
	}
	return a.store.Close()
}

func loadAuthSecret() []byte {
	if value := strings.TrimSpace(os.Getenv("AUTH_SECRET")); value != "" {
		return []byte(value)
	}
	path := strings.TrimSpace(os.Getenv("AUTH_SECRET_FILE"))
	if path == "" {
		path = "/var/lib/chat-codex/auth.secret"
	}
	if secret, err := os.ReadFile(path); err == nil && len(secret) >= 32 {
		return secret
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		log.Printf("auth secret generation failed: %v", err)
		return []byte("development-only-auth-secret")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err == nil {
		if err := os.WriteFile(path, secret, 0600); err != nil {
			log.Printf("auth secret persistence failed path=%s err=%v", path, err)
		}
	} else {
		log.Printf("auth secret directory creation failed path=%s err=%v", path, err)
	}
	return secret
}

func appLatencyLog(component, stage string, fields map[string]any) {
	payload := map[string]any{
		"component": component,
		"stage":     stage,
	}
	for key, value := range fields {
		payload[key] = value
	}
	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[latency][chat] component=%s stage=%s", component, stage)
		return
	}
	log.Printf("[latency][chat] %s", string(data))
}

func taskAgeFields(t *model.Task) map[string]any {
	fields := map[string]any{}
	if t == nil {
		return fields
	}
	fields["task_status"] = t.Status
	fields["task_created_at"] = t.CreatedAt.Format(time.RFC3339Nano)
	fields["since_task_created_ms"] = time.Since(t.CreatedAt).Milliseconds()
	return fields
}

func (a *App) shouldLogFirstDelta(taskID, field string) bool {
	a.latencyMu.Lock()
	defer a.latencyMu.Unlock()
	if a.taskLatency == nil {
		a.taskLatency = map[string]taskLatencyState{}
	}
	state := a.taskLatency[taskID]
	switch field {
	case "reasoning":
		if state.firstReasoningDeltaLogged {
			return false
		}
		state.firstReasoningDeltaLogged = true
	case "text", "":
		if state.firstTextDeltaLogged {
			return false
		}
		state.firstTextDeltaLogged = true
	default:
		return false
	}
	a.taskLatency[taskID] = state
	return true
}

func (a *App) clearTaskLatency(taskID string) {
	a.latencyMu.Lock()
	defer a.latencyMu.Unlock()
	if a.taskLatency == nil {
		return
	}
	delete(a.taskLatency, taskID)
}

func (a *App) registerAIConfigRequest(requestID string) chan model.DeviceAIConfigResultPayload {
	ch := make(chan model.DeviceAIConfigResultPayload, 1)
	a.aiConfigMu.Lock()
	a.aiConfigSubs[requestID] = ch
	a.aiConfigMu.Unlock()
	return ch
}

func (a *App) unregisterAIConfigRequest(requestID string) {
	a.aiConfigMu.Lock()
	delete(a.aiConfigSubs, requestID)
	a.aiConfigMu.Unlock()
}

func (a *App) pushAIConfigResult(requestID string, payload model.DeviceAIConfigResultPayload) {
	if requestID == "" {
		return
	}
	a.aiConfigMu.Lock()
	ch := a.aiConfigSubs[requestID]
	a.aiConfigMu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- payload:
	default:
	}
}

func (a *App) registerGoalOptimizeRequest(requestID string) chan model.GoalOptimizeResultPayload {
	ch := make(chan model.GoalOptimizeResultPayload, 1)
	a.goalOptimizeMu.Lock()
	a.goalOptimizeSubs[requestID] = ch
	a.goalOptimizeMu.Unlock()
	return ch
}

func (a *App) unregisterGoalOptimizeRequest(requestID string) {
	a.goalOptimizeMu.Lock()
	delete(a.goalOptimizeSubs, requestID)
	a.goalOptimizeMu.Unlock()
}

func (a *App) registerSessionHistoryRequest(requestID string) chan model.SessionHistoryResultPayload {
	ch := make(chan model.SessionHistoryResultPayload, 1)
	a.sessionHistoryMu.Lock()
	a.sessionHistorySubs[requestID] = ch
	a.sessionHistoryMu.Unlock()
	return ch
}

func (a *App) unregisterSessionHistoryRequest(requestID string) {
	a.sessionHistoryMu.Lock()
	delete(a.sessionHistorySubs, requestID)
	a.sessionHistoryMu.Unlock()
}

func (a *App) pushSessionHistoryResult(requestID string, payload model.SessionHistoryResultPayload) {
	if requestID == "" {
		return
	}
	a.sessionHistoryMu.Lock()
	ch := a.sessionHistorySubs[requestID]
	a.sessionHistoryMu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- payload:
	default:
	}
}

func (a *App) requestSessionHistory(
	ctx context.Context,
	operatorID int64,
	agentID string,
	requestID string,
	payload model.SessionHistoryRequestPayload,
) (model.SessionHistoryResultPayload, error) {
	resultCh := a.registerSessionHistoryRequest(requestID)
	defer a.unregisterSessionHistoryRequest(requestID)
	if err := a.broker.DispatchForOperator(ctx, operatorID, agentID, model.Envelope{
		Type:      "session.history.request",
		RequestID: requestID,
		SentAt:    time.Now().UTC().Format(time.RFC3339),
		Payload:   payload,
	}); err != nil {
		return model.SessionHistoryResultPayload{}, err
	}
	select {
	case <-ctx.Done():
		return model.SessionHistoryResultPayload{}, ctx.Err()
	case <-time.After(sessionHistoryRequestTimeout):
		return model.SessionHistoryResultPayload{}, fmt.Errorf("session history timeout")
	case result := <-resultCh:
		if result.Source == "" {
			result.Source = "agent_local"
		}
		return result, nil
	}
}

func (a *App) pushGoalOptimizeResult(requestID string, payload model.GoalOptimizeResultPayload) {
	if requestID == "" {
		return
	}
	a.goalOptimizeMu.Lock()
	ch := a.goalOptimizeSubs[requestID]
	a.goalOptimizeMu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- payload:
	default:
	}
}

func (a *App) registerMCPConfigRequest(requestID string) chan model.DeviceMCPConfigResultPayload {
	ch := make(chan model.DeviceMCPConfigResultPayload, 1)
	a.mcpConfigMu.Lock()
	a.mcpConfigSubs[requestID] = ch
	a.mcpConfigMu.Unlock()
	return ch
}

func (a *App) unregisterMCPConfigRequest(requestID string) {
	a.mcpConfigMu.Lock()
	delete(a.mcpConfigSubs, requestID)
	a.mcpConfigMu.Unlock()
}

func (a *App) pushMCPConfigResult(requestID string, payload model.DeviceMCPConfigResultPayload) {
	if requestID == "" {
		return
	}
	a.mcpConfigMu.Lock()
	ch := a.mcpConfigSubs[requestID]
	a.mcpConfigMu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- payload:
	default:
	}
}

func (a *App) registerEnvConfigRequest(requestID string) chan model.DeviceEnvConfigResultPayload {
	ch := make(chan model.DeviceEnvConfigResultPayload, 1)
	a.envConfigMu.Lock()
	a.envConfigSubs[requestID] = ch
	a.envConfigMu.Unlock()
	return ch
}

func (a *App) unregisterEnvConfigRequest(requestID string) {
	a.envConfigMu.Lock()
	delete(a.envConfigSubs, requestID)
	a.envConfigMu.Unlock()
}

func (a *App) pushEnvConfigResult(requestID string, payload model.DeviceEnvConfigResultPayload) {
	if requestID == "" {
		return
	}
	a.envConfigMu.Lock()
	ch := a.envConfigSubs[requestID]
	a.envConfigMu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- payload:
	default:
	}
}

func (a *App) registerLauncherRequest(requestID string) chan model.DeviceLauncherResultPayload {
	ch := make(chan model.DeviceLauncherResultPayload, 1)
	a.launcherMu.Lock()
	a.launcherSubs[requestID] = ch
	a.launcherMu.Unlock()
	return ch
}

func (a *App) unregisterLauncherRequest(requestID string) {
	a.launcherMu.Lock()
	delete(a.launcherSubs, requestID)
	a.launcherMu.Unlock()
}

func (a *App) pushLauncherResult(requestID string, payload model.DeviceLauncherResultPayload) {
	if requestID == "" {
		return
	}
	a.launcherMu.Lock()
	ch := a.launcherSubs[requestID]
	a.launcherMu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- payload:
	default:
	}
}

func (a *App) registerDirectoriesRequest(requestID string) chan model.DeviceDirectoriesResultPayload {
	ch := make(chan model.DeviceDirectoriesResultPayload, 1)
	a.directoriesMu.Lock()
	a.directoriesSubs[requestID] = ch
	a.directoriesMu.Unlock()
	return ch
}

func (a *App) unregisterDirectoriesRequest(requestID string) {
	a.directoriesMu.Lock()
	delete(a.directoriesSubs, requestID)
	a.directoriesMu.Unlock()
}

func (a *App) pushDirectoriesResult(requestID string, payload model.DeviceDirectoriesResultPayload) {
	if requestID == "" {
		return
	}
	a.directoriesMu.Lock()
	ch := a.directoriesSubs[requestID]
	a.directoriesMu.Unlock()
	if ch == nil {
		return
	}
	select {
	case ch <- payload:
	default:
	}
}

func (a *App) addTaskEvent(taskID, eventType, sessionID, content, errText string) {
	a.store.AddEvent(taskID, model.Event{
		TaskID:    taskID,
		Type:      eventType,
		SessionID: sessionID,
		Content:   content,
		Error:     errText,
		SentAt:    time.Now().UTC(),
	})
}

func (a *App) requestTaskCancel(t *model.Task, reason string) {
	if a.reconcileTerminalEvent(t) {
		return
	}
	if _, ok := a.store.MarkCancelling(t.ID, t.SessionID, reason); !ok {
		return
	}
	a.broker.ClearTaskForOperator(t.OperatorID, t.AgentID, t.ID)
	a.addTaskEvent(t.ID, "cancelling", t.SessionID, reason, reason)
	if err := a.broker.DispatchForOperator(context.Background(), t.OperatorID, t.AgentID, model.Envelope{
		Type:      "task.cancel",
		RequestID: fmt.Sprintf("req_timeout_%s", t.ID),
		SentAt:    time.Now().UTC().Format(time.RFC3339),
		Payload:   model.CancelPayload{TaskID: t.ID},
	}); err != nil {
		log.Printf("[cleanup] failed to dispatch cancel for task %s: %v", t.ID, err)
	}
	log.Printf("[cleanup] task %s entered cancelling: %s", t.ID, reason)
}

func (a *App) reconcileTerminalEvent(t *model.Task) bool {
	if t == nil {
		return false
	}
	evt, ok := a.store.LatestTerminalEvent(t.ID)
	if !ok {
		return false
	}

	switch evt.Type {
	case "completed":
		a.store.Complete(t.ID, evt.SessionID, evt.Content, evt.Artifacts)
		a.store.TrackSession(t.ID, evt.SessionID, "active", evt.Content)
	case "failed":
		a.store.Fail(t.ID, evt.SessionID, evt.Error)
		a.store.TrackSession(t.ID, evt.SessionID, "error", evt.Error)
	case "cancelled":
		a.store.Cancel(t.ID)
	}
	a.broker.ClearTaskForOperator(t.OperatorID, t.AgentID, t.ID)
	log.Printf("[cleanup] reconciled task %s from terminal event %s before timeout cancellation", t.ID, evt.Type)
	return true
}

func (a *App) finalizeTaskFailure(t *model.Task, reason, logPrefix string) {
	if a.reconcileTerminalEvent(t) {
		return
	}
	a.store.Fail(t.ID, t.SessionID, reason)
	a.broker.ClearTaskForOperator(t.OperatorID, t.AgentID, t.ID)
	a.addTaskEvent(t.ID, "failed", t.SessionID, "", reason)
	log.Printf("%s task %s failed: %s", logPrefix, t.ID, reason)
}

func (a *App) shouldIgnoreTaskEvent(taskID, eventType string) bool {
	task, ok := a.store.GetTask(taskID)
	if !ok {
		return false
	}
	// A failed parent task can still have detached subagents. Their terminal
	// events are the source of truth for the durable task graph and must remain
	// ingestible after the parent has entered a terminal state.
	if strings.HasPrefix(eventType, "task.subagent_") {
		return false
	}

	switch task.Status {
	case model.TaskCancelling:
		return eventType != "task.failed" && eventType != "task.cancelled"
	case model.TaskCompleted, model.TaskFailed, model.TaskCancelled:
		return true
	default:
		return false
	}
}

func (a *App) subagentResultAlreadyRecorded(taskID, nodeID string, completedAt int64) bool {
	for _, event := range a.store.SubagentEvents(taskID, 200) {
		if event.Type != "subagent_result" {
			continue
		}
		metadata, ok := event.Metadata.(map[string]any)
		if !ok || fmt.Sprint(metadata["node_id"]) != nodeID {
			continue
		}
		if fmt.Sprint(metadata["completed_at"]) == fmt.Sprint(completedAt) {
			return true
		}
	}
	return false
}

func goalMetadataUpdates(msg model.GoalEventPayload, eventType string) map[string]string {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	status := strings.TrimSpace(msg.Status)
	if status == "" {
		switch eventType {
		case "goal_completed":
			status = "completed"
		case "goal_paused":
			status = "paused"
		case "goal_failed":
			status = "failed"
		default:
			status = "active"
		}
	}
	updates := map[string]string{
		"goal_status":     status,
		"goal_last_event": eventType,
		"goal_updated_at": now,
	}
	if msg.GoalID != "" {
		updates["goal_id"] = msg.GoalID
	}
	if msg.Objective != "" {
		updates["goal_objective"] = msg.Objective
	}
	if msg.SessionID != "" {
		updates["goal_session_id"] = msg.SessionID
	}
	if msg.Reason != "" {
		updates["goal_reason"] = msg.Reason
	}
	if msg.Iteration > 0 || eventType == "goal_created" {
		updates["goal_iteration"] = fmt.Sprintf("%d", msg.Iteration)
	}
	if msg.Max > 0 {
		updates["goal_max_iterations"] = fmt.Sprintf("%d", msg.Max)
	}
	if msg.Metadata != nil {
		for _, key := range []string{"checkpoint", "checkpoint_summary", "summary"} {
			if value, ok := stringifyMetadata(msg.Metadata[key]); ok {
				updates["goal_checkpoint"] = value
				break
			}
		}
	}
	if eventType == "goal_heartbeat" {
		updates["goal_last_heartbeat_at"] = now
	}
	return updates
}

func stringifyMetadata(value any) (string, bool) {
	switch typed := value.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		return trimmed, trimmed != ""
	case fmt.Stringer:
		trimmed := strings.TrimSpace(typed.String())
		return trimmed, trimmed != ""
	case float64:
		return fmt.Sprintf("%.0f", typed), true
	case int:
		return fmt.Sprintf("%d", typed), true
	case int64:
		return fmt.Sprintf("%d", typed), true
	case bool:
		if typed {
			return "true", true
		}
		return "false", true
	default:
		return "", false
	}
}

func (a *App) failAgentTasks(operatorID int64, agentID string) {
	for _, status := range []model.TaskStatus{model.TaskRunning, model.TaskDispatched, "started"} {
		tasks, _ := a.store.ListTasks(model.TaskFilter{OperatorID: operatorID, AgentID: agentID, Status: status, Limit: 50})
		for _, t := range tasks {
			a.finalizeTaskFailure(t, "设备断开连接", "[disconnect]")
		}
	}
}

func (a *App) scheduleAgentDisconnectFailure(operatorID int64, agentID string) {
	grace := a.taskDisconnectGrace
	if grace <= 0 {
		a.failAgentTasks(operatorID, agentID)
		return
	}
	go func() {
		timer := time.NewTimer(grace)
		defer timer.Stop()
		<-timer.C
		if _, ok := a.broker.Get(agentID, operatorID); ok {
			log.Printf("[disconnect] agent %s operator_id=%d reconnected within %s, keep active tasks", agentID, operatorID, grace)
			return
		}
		a.failAgentTasks(operatorID, agentID)
	}()
}

func hasCapability(capabilities []string, expected string) bool {
	for _, capability := range capabilities {
		if strings.EqualFold(strings.TrimSpace(capability), expected) {
			return true
		}
	}
	return false
}

func helloHasProject(hello model.HelloPayload, projectID string) bool {
	for _, project := range hello.Projects {
		if project.ProjectID == projectID {
			return true
		}
	}
	return false
}

func (a *App) resumableGoalTask(operatorID int64, agentID string, hello model.HelloPayload) *model.Task {
	if strings.TrimSpace(hello.RunningTaskID) != "" || !hasCapability(hello.Capabilities, taskResumeCapability) {
		return nil
	}
	var selected *model.Task
	for _, status := range []model.TaskStatus{model.TaskRunning, "started", model.TaskDispatched} {
		tasks, _ := a.store.ListTasks(model.TaskFilter{
			OperatorID: operatorID,
			AgentID:    agentID,
			Status:     status,
			Limit:      50,
		})
		for _, task := range tasks {
			if task == nil {
				continue
			}
			goalActive := strings.EqualFold(strings.TrimSpace(task.Metadata["goal_status"]), "active")
			hasSubagents := len(api.TaskSubagentNodes(task, a.store.SubagentEvents(task.ID, 200))) > 0
			if !goalActive && !hasSubagents {
				continue
			}
			if task.SessionID == "" || !helloHasProject(hello, task.ProjectID) {
				continue
			}
			if hello.MachineID != "" && task.MachineID != "" && task.MachineID != hello.MachineID {
				continue
			}
			if selected == nil || task.UpdatedAt.After(selected.UpdatedAt) {
				selected = task
			}
		}
	}
	return selected
}

func (a *App) resumeGoalTaskAfterReconnect(operatorID int64, agentID string, hello model.HelloPayload) {
	task := a.resumableGoalTask(operatorID, agentID, hello)
	if task == nil {
		return
	}

	metadata := make(map[string]string, len(task.Metadata)+2)
	for key, value := range task.Metadata {
		metadata[key] = value
	}
	resumeCount, _ := strconv.Atoi(metadata["goal_resume_count"])
	metadata["goal_resume"] = "true"
	metadata["goal_resume_count"] = strconv.Itoa(resumeCount + 1)

	baseSystemPrompt, err := api.TaskSystemPrompt(a.store, operatorID, agentID)
	if err != nil {
		log.Printf("[goal-resume] task %s system prompt load failed: %v", task.ID, err)
		return
	}
	resumeText := "Relay 进程已恢复。继续当前任务，从会话中的最新检查点接着执行，不要重复已经完成的工作。"
	if strings.EqualFold(strings.TrimSpace(task.Metadata["goal_status"]), "active") {
		resumeText = "Relay 进程已恢复。继续当前活动目标，从会话中的最新检查点接着执行，不要重复已经完成的工作。"
	}
	memorySnapshot := projectmemory.Snapshot{System: baseSystemPrompt}
	memoryEnabled := a.projectMemoryEnabled
	if scope, scopeErr := a.store.GetProjectScopeForAgent(context.Background(), operatorID, task.MachineID, agentID); scopeErr == nil && scope != nil {
		memoryEnabled = memoryEnabled && projectmemory.Resolve(context.Background(), a.store, operatorID, task.MachineID, scope.ID).Enabled
	}
	if memoryEnabled {
		memorySnapshot = projectmemory.Build(context.Background(), a.store, baseSystemPrompt, operatorID,
			task.MachineID, agentID, resumeText)
	}
	if memorySnapshot.ScopeID != "" {
		metadata["project_memory_scope_id"] = memorySnapshot.ScopeID
		metadata["project_memory_revision"] = strconv.FormatInt(memorySnapshot.Revision, 10)
	}
	if memoryEnabled {
		projectmemory.RecordUsage(context.Background(), a.store, task.ID, memorySnapshot)
	}
	env := model.Envelope{
		Type:      "task.run",
		RequestID: fmt.Sprintf("req_resume_%s_%d", task.ID, resumeCount+1),
		SentAt:    time.Now().UTC().Format(time.RFC3339),
		Payload: model.RunPayload{
			TaskID:    task.ID,
			AgentID:   task.AgentID,
			MachineID: task.MachineID,
			ProjectID: task.ProjectID,
			SessionID: task.SessionID,
			System:    memorySnapshot.System,
			Parts: []model.Part{{
				Type: "text",
				Text: resumeText,
			}},
			Metadata:  metadata,
			Resume:    true,
			Subagents: api.TaskSubagentNodes(task, a.store.SubagentEvents(task.ID, 200)),
		},
	}
	if err := a.broker.DispatchForOperator(context.Background(), operatorID, agentID, env); err != nil {
		log.Printf("[goal-resume] task %s dispatch failed: %v", task.ID, err)
		return
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	a.store.UpdateTaskMetadata(task.ID, map[string]string{
		"goal_resume":         "true",
		"goal_resume_count":   strconv.Itoa(resumeCount + 1),
		"goal_last_resume_at": now,
		"goal_updated_at":     now,
		"goal_last_event":     "goal_resumed",
	})
	a.addTaskEvent(task.ID, "goal_resumed", task.SessionID, "Relay 重启后已自动恢复目标任务", "")
	log.Printf("[goal-resume] task %s resumed on agent %s session %s iteration %s", task.ID, agentID, task.SessionID, metadata["goal_iteration"])
}

func (a *App) resumeDetachedSubagentsAfterReconnect(operatorID int64, agentID string, hello model.HelloPayload) {
	if strings.TrimSpace(hello.RunningTaskID) != "" || !hasCapability(hello.Capabilities, taskResumeCapability) {
		return
	}
	for _, task := range a.detachedSubagentTasks(operatorID, agentID, hello) {
		recoverable := api.RecoverableSubagentNodes(api.TaskSubagentNodes(task, a.store.SubagentEvents(task.ID, 200)))
		if len(recoverable) == 0 {
			continue
		}

		recoveryCount, _ := strconv.Atoi(task.Metadata["subagent_recovery_count"])
		env := model.Envelope{
			Type:      "task.subagent_recover",
			RequestID: fmt.Sprintf("req_subagent_recover_%s_%d", task.ID, recoveryCount+1),
			SentAt:    time.Now().UTC().Format(time.RFC3339),
			Payload: model.RunPayload{
				TaskID:    task.ID,
				AgentID:   task.AgentID,
				MachineID: task.MachineID,
				ProjectID: task.ProjectID,
				SessionID: task.SessionID,
				Metadata:  task.Metadata,
				Resume:    true,
				Subagents: recoverable,
			},
		}
		if err := a.broker.DispatchForOperator(context.Background(), operatorID, agentID, env); err != nil {
			log.Printf("[subagent-recover] task %s dispatch failed: %v", task.ID, err)
			continue
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		a.store.UpdateTaskMetadata(task.ID, map[string]string{
			"subagent_recovery_count":   strconv.Itoa(recoveryCount + 1),
			"subagent_last_recovery_at": now,
		})
		a.store.AddEvent(task.ID, model.Event{
			TaskID: task.ID, Type: "subagent_recovery_requested", SessionID: task.SessionID,
			Metadata: map[string]any{"attempt": recoveryCount + 1, "node_count": len(recoverable)}, SentAt: time.Now().UTC(),
		})
		log.Printf("[subagent-recover] task %s sent %d nodes to agent %s", task.ID, len(recoverable), agentID)
	}
}

func (a *App) detachedSubagentTasks(operatorID int64, agentID string, hello model.HelloPayload) []*model.Task {
	var tasks []*model.Task
	for _, status := range []model.TaskStatus{model.TaskFailed, model.TaskCompleted} {
		items, _ := a.store.ListTasks(model.TaskFilter{OperatorID: operatorID, AgentID: agentID, Status: status, Limit: 200})
		for _, task := range items {
			if task == nil || task.SessionID == "" || !helloHasProject(hello, task.ProjectID) {
				continue
			}
			if task.MachineID != "" && hello.MachineID != "" && task.MachineID != hello.MachineID {
				continue
			}
			if status == model.TaskCancelled {
				continue
			}
			if evt, ok := a.store.LatestTerminalEvent(task.ID); ok && evt.Type == "cancelled" {
				continue
			}
			hasRecoverable := false
			for _, node := range api.TaskSubagentNodes(task, a.store.SubagentEvents(task.ID, 200)) {
				if api.RecoverableSubagentNode(node) {
					hasRecoverable = true
					break
				}
			}
			if hasRecoverable {
				tasks = append(tasks, task)
			}
		}
	}
	return tasks
}

func isActiveTaskStatus(status model.TaskStatus) bool {
	switch status {
	case model.TaskPending, model.TaskDispatched, model.TaskRunning, model.TaskCancelling, model.TaskWaitingApproval, "started":
		return true
	default:
		return false
	}
}

func (a *App) taskLastProgressAt(t *model.Task) time.Time {
	if t == nil {
		return time.Time{}
	}
	if latest, ok := a.store.LastEventAt(t.ID); ok && !latest.IsZero() {
		return latest
	}
	if !t.CreatedAt.IsZero() {
		return t.CreatedAt
	}
	return t.UpdatedAt
}

func metadataTime(metadata map[string]string, key string) time.Time {
	if metadata == nil {
		return time.Time{}
	}
	raw := strings.TrimSpace(metadata[key])
	if raw == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func (a *App) taskActivityLease(t *model.Task) (time.Time, time.Duration) {
	last := a.taskLastProgressAt(t)
	timeout := a.taskTimeout
	if t == nil || !strings.EqualFold(strings.TrimSpace(t.Metadata["goal_status"]), "active") {
		return last, timeout
	}
	for _, key := range []string{"goal_last_heartbeat_at", "goal_updated_at"} {
		if current := metadataTime(t.Metadata, key); current.After(last) {
			last = current
		}
	}
	if a.goalHeartbeatTimeout > 0 {
		timeout = a.goalHeartbeatTimeout
	}
	return last, timeout
}

func (a *App) cleanStuckTasks() {
	ticker := time.NewTicker(a.taskCleanupTick)
	for range ticker.C {
		for _, status := range []model.TaskStatus{model.TaskDispatched, "started", model.TaskRunning} {
			// 第一阶段：把长时间无事件的活动任务切到 cancelling，并通知 relay 真正终止。
			// 普通设备 heartbeat 不参与续租；活动 Goal 只使用自己的 Goal 心跳租约。
			tasks, _ := a.store.ListTasks(model.TaskFilter{Status: status, Limit: 100})
			for _, t := range tasks {
				lastActivity, timeout := a.taskActivityLease(t)
				if lastActivity.Before(time.Now().Add(-timeout)) {
					a.requestTaskCancel(t, fmt.Sprintf("任务超时，正在终止执行（%s 无响应）", timeout))
				}
			}
		}

		// 第二阶段：取消请求发出后仍未收口的任务，最终标记为失败并补发终态事件。
		cancelling, _ := a.store.ListTasks(model.TaskFilter{Status: model.TaskCancelling, Limit: 100})
		finalizeCutoff := time.Now().Add(-a.taskCancelGrace)
		for _, t := range cancelling {
			if t.UpdatedAt.Before(finalizeCutoff) {
				a.finalizeTaskFailure(
					t,
					fmt.Sprintf("任务超时自动终止（取消请求发出后 %s 未完成）", a.taskCancelGrace),
					"[cleanup]",
				)
			}
		}
	}
}

func envDuration(key string, fallback time.Duration) time.Duration {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		log.Printf("invalid %s=%q, fallback to %s", key, raw, fallback)
		return fallback
	}
	return value
}

func envEnabled(key string, fallback bool) bool {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if raw == "" {
		return fallback
	}
	switch raw {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		log.Printf("invalid %s=%q, fallback to %t", key, raw, fallback)
		return fallback
	}
}

func (a *App) requestDeviceAIConfig(
	ctx context.Context,
	operatorID int64,
	agentID string,
	requestID string,
	envType string,
	payload any,
) (model.DeviceAIConfigResultPayload, error) {
	resultCh := a.registerAIConfigRequest(requestID)
	defer a.unregisterAIConfigRequest(requestID)
	if err := a.broker.DispatchForOperator(ctx, operatorID, agentID, model.Envelope{
		Type:      envType,
		RequestID: requestID,
		SentAt:    time.Now().UTC().Format(time.RFC3339),
		Payload:   payload,
	}); err != nil {
		return model.DeviceAIConfigResultPayload{}, err
	}
	select {
	case <-ctx.Done():
		return model.DeviceAIConfigResultPayload{}, ctx.Err()
	case <-time.After(20 * time.Second):
		return model.DeviceAIConfigResultPayload{}, fmt.Errorf("设备响应超时")
	case result := <-resultCh:
		if !result.Success {
			if result.Error != "" {
				return result, fmt.Errorf("%s", result.Error)
			}
			return result, fmt.Errorf("设备返回失败")
		}
		return result, nil
	}
}

func (a *App) requestGoalOptimize(
	ctx context.Context,
	operatorID int64,
	agentID string,
	requestID string,
	envType string,
	payload any,
) (model.GoalOptimizeResultPayload, error) {
	resultCh := a.registerGoalOptimizeRequest(requestID)
	defer a.unregisterGoalOptimizeRequest(requestID)
	if err := a.broker.DispatchForOperator(ctx, operatorID, agentID, model.Envelope{
		Type:      envType,
		RequestID: requestID,
		SentAt:    time.Now().UTC().Format(time.RFC3339),
		Payload:   payload,
	}); err != nil {
		return model.GoalOptimizeResultPayload{}, err
	}
	select {
	case <-ctx.Done():
		return model.GoalOptimizeResultPayload{}, ctx.Err()
	case <-time.After(90 * time.Second):
		return model.GoalOptimizeResultPayload{}, fmt.Errorf("目标提示词优化超时")
	case result := <-resultCh:
		if !result.Success {
			if result.Error != "" {
				return result, fmt.Errorf("%s", result.Error)
			}
			return result, fmt.Errorf("目标提示词优化失败")
		}
		return result, nil
	}
}

func (a *App) requestDeviceMCPConfig(
	ctx context.Context,
	operatorID int64,
	agentID string,
	requestID string,
	envType string,
	payload any,
) (model.DeviceMCPConfigResultPayload, error) {
	resultCh := a.registerMCPConfigRequest(requestID)
	defer a.unregisterMCPConfigRequest(requestID)
	if err := a.broker.DispatchForOperator(ctx, operatorID, agentID, model.Envelope{
		Type:      envType,
		RequestID: requestID,
		SentAt:    time.Now().UTC().Format(time.RFC3339),
		Payload:   payload,
	}); err != nil {
		return model.DeviceMCPConfigResultPayload{}, err
	}
	select {
	case <-ctx.Done():
		return model.DeviceMCPConfigResultPayload{}, ctx.Err()
	case <-time.After(20 * time.Second):
		return model.DeviceMCPConfigResultPayload{}, fmt.Errorf("设备响应超时")
	case result := <-resultCh:
		if !result.Success {
			if result.Error != "" {
				return result, fmt.Errorf("%s", result.Error)
			}
			return result, fmt.Errorf("设备返回失败")
		}
		return result, nil
	}
}

func (a *App) requestDeviceEnvConfig(
	ctx context.Context,
	operatorID int64,
	agentID string,
	requestID string,
	envType string,
	payload any,
) (model.DeviceEnvConfigResultPayload, error) {
	resultCh := a.registerEnvConfigRequest(requestID)
	defer a.unregisterEnvConfigRequest(requestID)
	if err := a.broker.DispatchForOperator(ctx, operatorID, agentID, model.Envelope{
		Type:      envType,
		RequestID: requestID,
		SentAt:    time.Now().UTC().Format(time.RFC3339),
		Payload:   payload,
	}); err != nil {
		return model.DeviceEnvConfigResultPayload{}, err
	}
	select {
	case <-ctx.Done():
		return model.DeviceEnvConfigResultPayload{}, ctx.Err()
	case <-time.After(20 * time.Second):
		return model.DeviceEnvConfigResultPayload{}, fmt.Errorf("设备响应超时")
	case result := <-resultCh:
		if !result.Success {
			if result.Error != "" {
				return result, fmt.Errorf("%s", result.Error)
			}
			return result, fmt.Errorf("设备返回失败")
		}
		return result, nil
	}
}

func (a *App) requestDeviceLauncher(
	ctx context.Context,
	operatorID int64,
	agentID string,
	requestID string,
	envType string,
	payload any,
) (model.DeviceLauncherResultPayload, error) {
	resultCh := a.registerLauncherRequest(requestID)
	defer a.unregisterLauncherRequest(requestID)
	if err := a.broker.DispatchForOperator(ctx, operatorID, agentID, model.Envelope{
		Type:      envType,
		RequestID: requestID,
		SentAt:    time.Now().UTC().Format(time.RFC3339),
		Payload:   payload,
	}); err != nil {
		return model.DeviceLauncherResultPayload{}, err
	}
	select {
	case <-ctx.Done():
		return model.DeviceLauncherResultPayload{}, ctx.Err()
	case <-time.After(30 * time.Second):
		return model.DeviceLauncherResultPayload{}, fmt.Errorf("设备 launcher 响应超时")
	case result := <-resultCh:
		if !result.Success {
			if result.Error != "" {
				return result, fmt.Errorf("%s", result.Error)
			}
			return result, fmt.Errorf("设备 launcher 返回失败")
		}
		return result, nil
	}
}

func (a *App) requestDeviceDirectories(
	ctx context.Context,
	operatorID int64,
	deviceID string,
	requestID string,
	envType string,
	payload any,
) (model.DeviceDirectoriesResultPayload, error) {
	resultCh := a.registerDirectoriesRequest(requestID)
	defer a.unregisterDirectoriesRequest(requestID)
	if err := a.broker.DispatchForOperator(ctx, operatorID, deviceID, model.Envelope{
		Type:      envType,
		RequestID: requestID,
		SentAt:    time.Now().UTC().Format(time.RFC3339),
		Payload:   payload,
	}); err != nil {
		return model.DeviceDirectoriesResultPayload{}, err
	}
	select {
	case <-ctx.Done():
		return model.DeviceDirectoriesResultPayload{}, ctx.Err()
	case <-time.After(directoryRequestTimeout(envType)):
		return model.DeviceDirectoriesResultPayload{}, fmt.Errorf("设备目录响应超时")
	case result := <-resultCh:
		if !result.Success {
			if result.Error != "" {
				return result, fmt.Errorf("%s", result.Error)
			}
			return result, fmt.Errorf("设备目录返回失败")
		}
		return result, nil
	}
}

func directoryRequestTimeout(envType string) time.Duration {
	switch envType {
	case "device.project_files.upload",
		"device.project_files.download",
		"device.project_files.download_create",
		"device.project_files.download_chunk",
		"device.project_files.upload_create",
		"device.project_files.upload_chunk",
		"device.project_files.upload_complete",
		"device.project_files.create_file",
		"device.project_files.mkdir",
		"device.project_files.delete",
		"device.project_files.rename":
		return 2 * time.Minute
	default:
		return 20 * time.Second
	}
}

func (a *App) Router() http.Handler {
	r := chi.NewRouter()
	h := a.apiHandler()
	r.Use(requestID)
	r.Use(recoverer)
	r.Use(cors)

	r.Get("/healthz", h.Health)
	r.Get("/readyz", h.Ready)
	r.Post("/api/auth/login", h.Login)
	r.Get("/api/auth/me", h.Me)
	r.Get("/api/upload-policy", h.GetUploadPolicy)
	r.Post("/api/push/devices", h.RegisterPushDevice)
	r.Post("/api/push/test", h.PushTest)
	r.Post("/api/notify/email-test", h.EmailTest)
	r.Get("/api/notifications", h.ListAppNotificationChanges)
	r.Post("/api/notifications/sync", h.UpsertAppNotification)
	r.Post("/api/notifications/read", h.MarkAppNotificationsRead)
	r.Post("/api/notifications/dismiss", h.DismissAppNotifications)
	r.Post("/api/support/logs", h.ReportSupportLogs)
	r.Get("/api/auth/captcha", h.GetCaptcha)
	r.Post("/api/auth/email-code", h.SendEmailCode)
	r.Post("/api/auth/register", h.Register)
	r.Post("/api/auth/change-password", h.ChangePassword)
	r.Post("/api/auth/delete-account", h.DeleteAccount)
	r.Get("/api/agents", h.ListAgents)
	r.Get("/api/devices", h.ListDevices)
	r.Get("/api/overlay/events", h.OverlayEvents)
	r.Patch("/api/devices/order", h.ReorderDevices)
	r.Get("/api/devices/{machineID}", h.GetDevice)
	r.Patch("/api/devices/{machineID}/profile", h.UpdateDeviceProfile)
	r.Patch("/api/devices/{machineID}/agents/order", h.ReorderDeviceAgents)
	r.Get("/api/devices/{machineID}/ai-config", h.GetDeviceAIConfig)
	r.Get("/api/devices/{machineID}/project-memory/settings", h.GetDeviceProjectMemorySettings)
	r.Patch("/api/devices/{machineID}/project-memory/settings", h.UpdateDeviceProjectMemorySettings)
	r.Post("/api/devices/{machineID}/ai-config/models", h.ListDeviceAIModels)
	r.Post("/api/devices/{machineID}/ai-config/save", h.SaveDeviceAIConfig)
	r.Post("/api/devices/{machineID}/ai-config/save-text", h.SaveDeviceAIConfigText)
	r.Post("/api/devices/{machineID}/ai-config/clear-provider", h.ClearDeviceAIProvider)
	r.Get("/api/devices/{machineID}/mcp-config", h.GetDeviceMCPConfig)
	r.Post("/api/devices/{machineID}/mcp-config/save", h.SaveDeviceMCPConfig)
	r.Post("/api/devices/{machineID}/mcp-config/remove", h.RemoveDeviceMCPConfig)
	r.Get("/api/devices/{machineID}/skills-config", h.GetDeviceSkillConfig)
	r.Post("/api/devices/{machineID}/skills-config/save", h.SaveDeviceSkillConfig)
	r.Post("/api/devices/{machineID}/skills-config/import", h.ImportDeviceSkill)
	r.Get("/api/devices/{machineID}/env-config", h.GetDeviceEnvConfig)
	r.Post("/api/devices/{machineID}/env-config/save", h.SaveDeviceEnvConfig)
	r.Get("/api/devices/{machineID}/launcher/agents/{agentID}/compaction-config", h.GetDeviceAgentCompactionConfig)
	r.Post("/api/devices/{machineID}/launcher/agents/{agentID}/compaction-config", h.SaveDeviceAgentCompactionConfig)
	r.Get("/api/environment-presets", h.ListEnvironmentPresets)
	r.Get("/api/runtime-catalog", h.ListRuntimeCatalog)
	r.Get("/api/tool-catalog", h.ListToolCatalog)
	r.Get("/api/mcp/catalog", h.ListMCPCatalog)
	r.Get("/api/admin/skills", h.ListSkills)
	r.Post("/api/admin/skills", h.UpsertSkill)
	r.Post("/api/admin/skills/{id}", h.UpsertSkill)
	r.Post("/api/admin/skills/{id}/delete", h.DeleteSkill)
	r.Get("/api/admin/mcp-catalog", h.ListAdminMCPCatalog)
	r.Post("/api/admin/mcp-catalog", h.UpsertMCPCatalogItem)
	r.Post("/api/admin/mcp-catalog/{id}", h.UpsertMCPCatalogItem)
	r.Post("/api/admin/mcp-catalog/{id}/delete", h.DeleteMCPCatalogItem)
	r.Get("/api/admin/environment-presets", h.ListAdminEnvironmentPresets)
	r.Post("/api/admin/environment-presets", h.UpsertEnvironmentPreset)
	r.Post("/api/admin/environment-presets/{id}", h.UpsertEnvironmentPreset)
	r.Post("/api/admin/environment-presets/{id}/enabled", h.SetEnvironmentPresetEnabled)
	r.Post("/api/admin/environment-presets/{id}/delete", h.DeleteEnvironmentPreset)
	r.Get("/api/admin/runtime-catalog", h.ListAdminRuntimeCatalog)
	r.Post("/api/admin/runtime-catalog", h.UpsertRuntimeCatalogItem)
	r.Post("/api/admin/runtime-catalog/{id}", h.UpsertRuntimeCatalogItem)
	r.Post("/api/admin/runtime-catalog/{id}/delete", h.DeleteRuntimeCatalogItem)
	r.Post("/api/admin/runtime-versions", h.UpsertRuntimeVersion)
	r.Post("/api/admin/runtime-versions/{id}", h.UpsertRuntimeVersion)
	r.Post("/api/admin/runtime-versions/{id}/delete", h.DeleteRuntimeVersion)
	r.Post("/api/admin/runtime-artifacts", h.UpsertRuntimeArtifact)
	r.Post("/api/admin/runtime-artifacts/{id}", h.UpsertRuntimeArtifact)
	r.Post("/api/admin/runtime-artifacts/{id}/delete", h.DeleteRuntimeArtifact)
	r.Post("/api/admin/runtime-mirrors", h.UpsertRuntimeMirror)
	r.Post("/api/admin/runtime-mirrors/{id}", h.UpsertRuntimeMirror)
	r.Post("/api/admin/runtime-mirrors/{id}/delete", h.DeleteRuntimeMirror)
	r.Get("/api/admin/tool-catalog", h.ListAdminToolCatalog)
	r.Post("/api/admin/tool-catalog", h.UpsertToolCatalogItem)
	r.Post("/api/admin/tool-catalog/{id}", h.UpsertToolCatalogItem)
	r.Post("/api/admin/tool-catalog/{id}/delete", h.DeleteToolCatalogItem)
	r.Get("/api/admin/semantic-agents", h.ListSemanticAgents)
	r.Post("/api/admin/semantic-agents/{id}", h.UpdateSemanticAgent)
	r.Post("/api/admin/semantic-agents/{id}/enabled", h.SetSemanticAgentEnabled)
	r.Post("/api/admin/semantic-agents/{id}/delete", h.DeleteSemanticAgent)
	r.Get("/api/admin/operators", h.ListAdminOperators)
	r.Post("/api/admin/operators/{id}/membership", h.SetOperatorMembership)
	r.Get("/api/skills", h.ListPublicSkills)
	r.Get("/api/devices/{machineID}/launcher", h.GetDeviceLauncherState)
	r.Get("/api/devices/{machineID}/launcher/diagnostics", h.GetDeviceLauncherDiagnostics)
	r.Post("/api/devices/{machineID}/launcher/ssh/setup", h.SetupDeviceLauncherSSH)
	r.Post("/api/devices/{machineID}/tunnels", a.createTunnel)
	if a.projectMemoryEnabled {
		r.Get("/api/devices/{machineID}/projects", h.ListProjectScopes)
		r.Get("/api/devices/{machineID}/projects/{scopeID}/memory", h.GetProjectMemoryOverview)
		r.Get("/api/devices/{machineID}/projects/{scopeID}/memory/settings", h.GetProjectMemorySettings)
		r.Patch("/api/devices/{machineID}/projects/{scopeID}/memory/settings", h.UpdateProjectMemorySettings)
		r.Get("/api/devices/{machineID}/projects/{scopeID}/memories", h.ListProjectMemories)
		r.Get("/api/devices/{machineID}/projects/{scopeID}/memories/{memoryID}", h.GetProjectMemory)
		r.Patch("/api/devices/{machineID}/projects/{scopeID}/memories/{memoryID}", h.UpdateProjectMemory)
		r.Delete("/api/devices/{machineID}/projects/{scopeID}/memories/{memoryID}", h.DeleteProjectMemory)
		r.Post("/api/devices/{machineID}/projects/{scopeID}/memories/{memoryID}/resolve", h.ResolveProjectMemory)
		r.Post("/api/devices/{machineID}/projects/{scopeID}/memory-jobs", h.CreateProjectMemoryJob)
		r.Post("/api/devices/{machineID}/projects/{scopeID}/identity", h.CorrectProjectIdentity)
		r.Get("/api/devices/{machineID}/projects/{scopeID}/memory-jobs/{jobID}", h.GetProjectMemoryJob)
		r.Get("/api/devices/{machineID}/projects/{scopeID}/memory-jobs/{jobID}/events", h.ProjectMemoryJobEvents)
	}
	r.Get("/api/devices/{machineID}/launcher/agents/{agentID}/mcp-tools", h.GetDeviceAgentMCPStatus)
	r.Get("/api/devices/{machineID}/launcher/agents/{agentID}/mcp-selection", h.GetDeviceAgentMCPSelection)
	r.Post("/api/devices/{machineID}/launcher/agents/{agentID}/mcp-selection", h.SaveDeviceAgentMCPSelection)
	r.Get("/api/devices/{machineID}/launcher/agents/{agentID}/skills", h.GetDeviceAgentSkillSelection)
	r.Post("/api/devices/{machineID}/launcher/agents/{agentID}/skills", h.SaveDeviceAgentSkillSelection)
	r.Post("/api/devices/{machineID}/launcher/agents/{agentID}/skills/import", h.ImportDeviceAgentSkill)
	r.Get("/api/devices/{machineID}/launcher/agents/{agentID}/semantic-agent", h.GetDeviceAgentSemanticSelection)
	r.Post("/api/devices/{machineID}/launcher/agents/{agentID}/semantic-agent", h.SaveDeviceAgentSemanticSelection)
	r.Post("/api/devices/{machineID}/launcher/agents/{agentID}/semantic-agent/preflight", h.StartDeviceAgentSemanticPreflight)
	r.Get("/api/devices/{machineID}/launcher/preflight-jobs/{jobID}", h.GetDeviceAgentSemanticPreflightJob)
	r.Get("/api/devices/{machineID}/launcher/preflight-jobs/{jobID}/events", h.GetDeviceAgentSemanticPreflightEvents)
	r.Get("/api/devices/{machineID}/directory-access", h.GetDeviceDirectoryAccess)
	r.Post("/api/devices/{machineID}/directory-access", h.UpdateDeviceDirectoryAccess)
	r.Post("/api/devices/{machineID}/directory-access/email-code", h.SendDeviceDirectoryAccessEmailCode)
	r.Get("/api/devices/{machineID}/directories", h.GetDeviceDirectories)
	r.Post("/api/devices/{machineID}/directories/files/upload/create", h.CreateDeviceDirectoryFileUpload)
	r.Post("/api/devices/{machineID}/directories/files/upload/chunk", h.UploadDeviceDirectoryFileChunk)
	r.Post("/api/devices/{machineID}/directories/files/upload/complete", h.CompleteDeviceDirectoryFileUpload)
	r.Post("/api/devices/{machineID}/directories/files/upload/status", h.DeviceDirectoryFileUploadStatus)
	r.Post("/api/devices/{machineID}/directories/files/create", h.CreateDeviceDirectoryEmptyFile)
	r.Post("/api/devices/{machineID}/directories/folders/create", h.CreateDeviceDirectoryFolder)
	r.Post("/api/devices/{machineID}/directories/files/delete", h.DeleteDeviceDirectoryFile)
	r.Get("/api/devices/{machineID}/launcher/agents/{agentID}/files", h.ListDeviceAgentFiles)
	r.Post("/api/devices/{machineID}/launcher/agents/{agentID}/files/upload", h.UploadDeviceAgentFile)
	r.Post("/api/devices/{machineID}/launcher/agents/{agentID}/files/upload/create", h.CreateDeviceAgentFileUpload)
	r.Post("/api/devices/{machineID}/launcher/agents/{agentID}/files/upload/chunk", h.UploadDeviceAgentFileChunk)
	r.Post("/api/devices/{machineID}/launcher/agents/{agentID}/files/upload/complete", h.CompleteDeviceAgentFileUpload)
	r.Post("/api/devices/{machineID}/launcher/agents/{agentID}/files/upload/status", h.DeviceAgentFileUploadStatus)
	r.Get("/api/devices/{machineID}/launcher/agents/{agentID}/files/download", h.DownloadDeviceAgentFile)
	r.Post("/api/devices/{machineID}/launcher/agents/{agentID}/files/download/create", h.CreateDeviceAgentFileDownload)
	r.Post("/api/devices/{machineID}/launcher/agents/{agentID}/files/download/chunk", h.DownloadDeviceAgentFileChunk)
	r.Post("/api/devices/{machineID}/launcher/agents/{agentID}/files/create", h.CreateDeviceAgentEmptyFile)
	r.Post("/api/devices/{machineID}/launcher/agents/{agentID}/files/folders/create", h.CreateDeviceAgentFolder)
	r.Post("/api/devices/{machineID}/launcher/agents/{agentID}/files/delete", h.DeleteDeviceAgentFile)
	r.Post("/api/devices/{machineID}/launcher/agents/{agentID}/files/rename", h.RenameDeviceAgentFile)
	r.Post("/api/devices/{machineID}/launcher/agents", h.CreateDeviceAgent)
	r.Post("/api/devices/{machineID}/launcher/agents/rename", h.RenameDeviceAgent)
	r.Post("/api/devices/{machineID}/launcher/agents/restart", h.RestartDeviceAgent)
	r.Post("/api/devices/{machineID}/launcher/agents/enabled", h.SetDeviceAgentEnabled)
	r.Post("/api/devices/{machineID}/launcher/agents/remove", h.RemoveDeviceAgent)
	r.Post("/api/devices/{machineID}/launcher/upgrade", h.UpgradeDeviceLauncher)
	r.Post("/api/devices/{machineID}/launcher/rollback", h.RollbackDeviceLauncher)
	r.Get("/api/devices/{machineID}/launcher/autostart", h.GetDeviceLauncherAutostart)
	r.Post("/api/devices/{machineID}/launcher/autostart/enable", h.EnableDeviceLauncherAutostart)
	r.Post("/api/devices/{machineID}/launcher/autostart/disable", h.DisableDeviceLauncherAutostart)
	r.Post("/api/devices/{machineID}/launcher/self-update", h.SelfUpdateDeviceLauncher)
	r.Post("/api/devices/{machineID}/launcher/directory-permission", h.RequestDeviceLauncherDirectoryPermission)
	r.Get("/api/tasks", h.ListTasks)
	r.Get("/api/tasks/delta", h.ListTaskDelta)
	r.Get("/api/sessions", h.ListSessions)
	r.Get("/api/sessions/{sessionID}/queue", h.ListChatQueue)
	r.Get("/api/sessions/{sessionID}/queue/events", h.ChatQueueEvents)
	r.Post("/api/sessions/{sessionID}/queue", h.CreateChatQueueItem)
	r.Post("/api/sessions/{sessionID}/queue/reorder", h.ReorderChatQueue)
	r.Patch("/api/chat-queue/{itemID}", h.UpdateChatQueueItem)
	r.Delete("/api/chat-queue/{itemID}", h.DeleteChatQueueItem)
	r.Post("/api/chat-queue/{itemID}/insert", h.InsertChatQueueItem)
	r.Post("/api/chat-queue/{itemID}/send", h.SendChatQueueItem)
	r.Post("/api/tasks", h.CreateTask)
	r.Post("/api/model-tests", h.CreateModelTest)
	r.Post("/api/goal/optimize", h.OptimizeGoal)
	r.Get("/api/tasks/{taskID}", h.GetTask)
	r.Get("/api/tasks/{taskID}/events", h.TaskEvents)
	r.Get("/api/tasks/{taskID}/event-pages", h.TaskEventPage)
	r.Get("/api/tasks/{taskID}/agent-history", h.TaskAgentHistory)
	r.Get("/api/tasks/{taskID}/subagents", h.ListTaskSubagents)
	r.Get("/api/tasks/{taskID}/subagents/{nodeID}/logs", h.ListTaskSubagentLogs)
	r.Post("/api/tasks/{taskID}/subagents/{nodeID}/control", h.ControlTaskSubagent)
	r.Get("/api/model-tests/{testID}/events", h.ModelTestEvents)
	r.Get("/api/tasks/{taskID}/artifacts/{artifactID}/download", h.DownloadTaskArtifact)
	r.Post("/api/tasks/{taskID}/approval", h.ApproveTask)
	r.Post("/api/tasks/{taskID}/question", h.AnswerTaskQuestion)
	r.Post("/api/tasks/{taskID}/cancel", h.CancelTask)
	r.Get("/api/models", h.ListModels)
	r.Get("/api/agents/{agentID}/settings", h.GetDeviceSettings)
	r.Post("/api/agents/{agentID}/settings", h.UpdateDeviceSettings)
	r.Get("/api/cli/version", h.CliVersion)
	r.Get("/api/launcher/version", h.LauncherVersion)
	r.Get("/api/launcher/downloads", h.LauncherDownloads)
	r.Get("/api/launcher/download", h.LauncherDownload)
	r.Get("/api/runtime/uv/download", h.RuntimeUVDownload)
	r.Get("/api/runtime/node/download", h.RuntimeNodeDownload)
	r.Get("/api/runtime/java/download", h.RuntimeJavaDownload)
	r.Get("/api/runtime/go/download", h.RuntimeGoDownload)
	r.Get("/api/runtime/ghidra/download", h.RuntimeGhidraDownload)
	r.Get("/api/runtime/ida/download", h.RuntimeIDADownload)
	r.Get("/api/runtime/tool/download", h.RuntimeToolDownload)
	r.Get("/api/runtime/mcp/download", h.RuntimeMCPDownload)
	r.Get("/api/runtime/mcp/download/{artifact}", h.RuntimeMCPDownload)
	r.Get("/api/app/version", h.AppVersion)
	r.Get("/api/app/releases", h.AppReleases)
	r.Get("/api/app/downloads", h.AppDownloads)
	r.Get("/api/app/download", h.AppDownload)
	r.Get("/admin", adminWebRedirect)
	r.Handle("/admin/*", adminWebHandler())
	r.Get("/ws/device", a.device)
	r.Post("/b/{bootstrapToken}/authorized-key", a.tunnelBootstrapAuthorizedKey)
	r.Get("/b/{bootstrapToken}/{platform}", a.tunnelBootstrap)
	r.Get(tunnelClientPath, a.tunnelClient)
	r.Get(tunnelDevicePath, a.tunnelDevice)
	return r
}

func (a *App) apiHandler() *api.API {
	handler := api.New(a.store, a.broker, a.auth, a.mailer, a.streamArtifact, a.notifyService, a.pushService, a.requestDeviceAIConfig, a.requestGoalOptimize, a.requestDeviceMCPConfig, a.requestDeviceEnvConfig, a.requestDeviceLauncher, a.requestDeviceDirectories, a.projectMemoryEnabled)
	handler.SetOverlayHub(a.overlayHub)
	handler.SetSessionHistoryRequest(a.requestSessionHistory)
	return handler
}

func adminWebRedirect(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/admin/", http.StatusFound)
}

func adminWebHandler() http.Handler {
	root := adminWebRoot()
	files := http.StripPrefix("/admin/", http.FileServer(http.Dir(root)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		files.ServeHTTP(w, r)
	})
}

func adminWebRoot() string {
	if dir := strings.TrimSpace(os.Getenv("CHAT_CODEX_ADMIN_WEB_DIR")); dir != "" {
		return dir
	}
	candidates := []string{
		"/opt/chat-codex/admin-web",
		"admin_web",
		filepath.Join("..", "admin_web"),
	}
	for _, dir := range candidates {
		if _, err := os.Stat(filepath.Join(dir, "index.html")); err == nil {
			return dir
		}
	}
	return "/opt/chat-codex/admin-web"
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PATCH,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Cache-Control, Accept")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if id == "" || len(id) > 128 {
			id = fmt.Sprintf("req_%d", time.Now().UnixNano())
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r)
	})
}

func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := &responseWriter{ResponseWriter: w}
		defer func() {
			if recovered := recover(); recovered != nil {
				log.Printf("http panic request_id=%s method=%s path=%s panic=%v", rw.Header().Get("X-Request-ID"), r.Method, r.URL.Path, recovered)
				if !rw.wroteHeader {
					writeJSONError(rw, http.StatusInternalServerError, "internal server error")
				}
			}
		}()
		next.ServeHTTP(rw, r)
	})
}

// responseWriter tracks whether a handler has started its response so the
// panic middleware does not append JSON after an SSE or streaming response.
type responseWriter struct {
	http.ResponseWriter
	wroteHeader bool
}

func (w *responseWriter) WriteHeader(code int) {
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(code)
}
func (w *responseWriter) Write(p []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}
func (w *responseWriter) Flush() {
	w.wroteHeader = true
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
func (w *responseWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func writeJSONError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(fmt.Sprintf(`{"error":%q}`, message)))
}

type wsRequestMeta struct {
	Path       string
	Host       string
	RemoteAddr string
	ClientIP   string
	XForwarded string
	UserAgent  string
}

func newWSRequestMeta(r *http.Request) wsRequestMeta {
	if r == nil {
		return wsRequestMeta{}
	}
	return wsRequestMeta{
		Path:       r.URL.Path,
		Host:       r.Host,
		RemoteAddr: strings.TrimSpace(r.RemoteAddr),
		ClientIP:   requestClientIP(r),
		XForwarded: shortLogValue(r.Header.Get("X-Forwarded-For"), 160),
		UserAgent:  shortLogValue(r.UserAgent(), 160),
	}
}

func (m wsRequestMeta) fields() string {
	return fmt.Sprintf(
		"path=%s host=%s client_ip=%s remote=%s xff=%q ua=%q",
		m.Path,
		m.Host,
		m.ClientIP,
		m.RemoteAddr,
		m.XForwarded,
		m.UserAgent,
	)
}

func requestClientIP(r *http.Request) string {
	if r == nil {
		return ""
	}
	if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
		if first := strings.TrimSpace(strings.Split(xff, ",")[0]); first != "" {
			return first
		}
	}
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func shortLogValue(raw string, max int) string {
	value := strings.TrimSpace(raw)
	if max <= 0 || len(value) <= max {
		return value
	}
	return value[:max] + "..."
}

func maskOperatorKey(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if len(value) <= 8 {
		return value
	}
	return value[:8] + "..."
}

func wsCloseStatus(err error) any {
	status := websocket.CloseStatus(err)
	if status == -1 {
		return "n/a"
	}
	return status
}

func (a *App) device(w http.ResponseWriter, r *http.Request) {
	meta := newWSRequestMeta(r)
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		log.Printf("[ws.device] accept failed %s err=%v", meta.fields(), err)
		return
	}
	defer c.CloseNow()
	c.SetReadLimit(deviceWebSocketReadLimit)

	ctx := r.Context()
	_, buf, err := c.Read(ctx)
	if err != nil {
		log.Printf("[ws.device] hello read failed %s close_status=%v err=%v", meta.fields(), wsCloseStatus(err), err)
		return
	}

	var env model.Envelope
	if err := json.Unmarshal(buf, &env); err != nil {
		log.Printf("[ws.device] invalid hello envelope %s bytes=%d err=%v", meta.fields(), len(buf), err)
		c.Write(ctx, websocket.MessageText, []byte(`{"type":"error","payload":{"error":"invalid hello"}}`))
		return
	}
	if env.Type != "device.hello" {
		log.Printf("[ws.device] unexpected first message %s type=%s request_id=%s", meta.fields(), env.Type, env.RequestID)
		c.Write(ctx, websocket.MessageText, []byte(`{"type":"error","payload":{"error":"invalid hello"}}`))
		return
	}

	body, err := json.Marshal(env.Payload)
	if err != nil {
		return
	}

	var hello model.HelloPayload
	if err := json.Unmarshal(body, &hello); err != nil {
		log.Printf("[ws.device] invalid hello payload %s request_id=%s err=%v", meta.fields(), env.RequestID, err)
		c.Write(ctx, websocket.MessageText, []byte(`{"type":"error","payload":{"error":"invalid payload"}}`))
		return
	}
	if hello.AgentID == "" && hello.DeviceID == "" {
		log.Printf("[ws.device] missing agent identifier %s request_id=%s machine_id=%s hostname=%s", meta.fields(), env.RequestID, hello.MachineID, hello.Hostname)
		c.Write(ctx, websocket.MessageText, []byte(`{"type":"error","payload":{"error":"invalid payload"}}`))
		return
	}

	agentID := hello.AgentID
	if agentID == "" {
		agentID = hello.DeviceID
	}
	if agentID == "" {
		c.Write(ctx, websocket.MessageText, []byte(`{"type":"error","payload":{"error":"missing agent_id"}}`))
		return
	}

	operator, err := a.store.GetOperatorByKey(hello.OperatorKey)
	if err != nil {
		log.Printf("[ws.device] operator lookup failed %s agent_id=%s operator_key=%s err=%v", meta.fields(), agentID, maskOperatorKey(hello.OperatorKey), err)
		c.Write(ctx, websocket.MessageText, []byte(`{"type":"error","payload":{"error":"invalid operator_key"}}`))
		return
	}
	if operator == nil {
		log.Printf("[ws.device] invalid operator key %s agent_id=%s operator_key=%s", meta.fields(), agentID, maskOperatorKey(hello.OperatorKey))
		c.Write(ctx, websocket.MessageText, []byte(`{"type":"error","payload":{"error":"invalid operator_key"}}`))
		return
	}
	if a.projectMemoryEnabled {
		a.registerHelloProjectScopes(ctx, operator.ID, agentID, &hello)
	}

	device := a.broker.Add(c, hello, operator.ID)
	capabilities := strings.Join(hello.Capabilities, ",")
	scopeIDs := make([]string, 0, len(hello.Projects))
	for _, project := range hello.Projects {
		if project.ScopeID != "" {
			scopeIDs = append(scopeIDs, project.ScopeID)
		}
	}
	log.Printf(
		"[ws.device] connected %s agent_id=%s operator_id=%d machine_id=%s hostname=%s version=%s projects=%d scope_ids=%s capabilities=%s",
		meta.fields(),
		device.ID,
		operator.ID,
		hello.MachineID,
		hello.Hostname,
		hello.Version,
		len(hello.Projects),
		strings.Join(scopeIDs, ","),
		capabilities,
	)
	defer func() {
		log.Printf("[ws.device] disconnected %s agent_id=%s", meta.fields(), device.ID)
		if a.broker.Remove(device) {
			if a.tunnels != nil {
				a.tunnels.cancelLauncher(operator.ID, hello.MachineID, device.ID)
			}
			a.scheduleAgentDisconnectFailure(operator.ID, agentID)
		}
	}()

	msg := model.Envelope{
		Type:      "device.welcome",
		RequestID: env.RequestID,
		SentAt:    time.Now().UTC().Format(time.RFC3339),
		Payload: model.WelcomePayload{
			AgentID:              device.ID,
			HeartbeatIntervalSec: 15,
			Projects:             hello.Projects,
		},
	}

	raw, _ := json.Marshal(msg)
	if err := c.Write(ctx, websocket.MessageText, raw); err != nil {
		log.Printf("[ws.device] welcome write failed %s agent_id=%s close_status=%v err=%v", meta.fields(), device.ID, wsCloseStatus(err), err)
		return
	}
	if a.projectMemoryEnabled {
		go a.dispatchProjectMemoryJobs(context.Background())
	}
	a.resumeGoalTaskAfterReconnect(operator.ID, agentID, hello)
	a.resumeDetachedSubagentsAfterReconnect(operator.ID, agentID, hello)

	for {
		_, buf, err := c.Read(ctx)
		if err != nil {
			log.Printf("[ws.device] read failed %s agent_id=%s close_status=%v err=%v", meta.fields(), device.ID, wsCloseStatus(err), err)
			return
		}
		if err := a.handle(device, buf); err != nil {
			log.Printf("[ws.device] handle failed %s agent_id=%s err=%v", meta.fields(), device.ID, err)
			fail := fmt.Sprintf(`{"type":"error","payload":{"error":%q}}`, err.Error())
			if writeErr := c.Write(ctx, websocket.MessageText, []byte(fail)); writeErr != nil {
				log.Printf("[ws.device] error reply failed %s agent_id=%s close_status=%v err=%v", meta.fields(), device.ID, wsCloseStatus(writeErr), writeErr)
				return
			}
		}
	}
}

func (a *App) handle(device *broker.Device, buf []byte) error {
	var env model.Envelope
	if err := json.Unmarshal(buf, &env); err != nil {
		log.Printf("[handle] unmarshal error: %v", err)
		return err
	}
	agentID := ""
	if device != nil {
		agentID = device.ID
	}
	log.Printf("[handle] agentID=%s type=%s", agentID, env.Type)

	body, err := json.Marshal(env.Payload)
	if err != nil {
		return err
	}

	switch env.Type {
	case "device.heartbeat":
		var msg model.HeartbeatPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		runningTaskID := strings.TrimSpace(msg.RunningTaskID)
		if runningTaskID != "" {
			if task, ok := a.store.GetTask(runningTaskID); !ok || !isActiveTaskStatus(task.Status) {
				runningTaskID = ""
			}
		}
		a.broker.TouchDevice(device, runningTaskID, msg.Metrics)
		return nil
	case "device.ai_config.result":
		var msg model.DeviceAIConfigResultPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		a.pushAIConfigResult(env.RequestID, msg)
		return nil
	case "goal.optimize.result":
		var msg model.GoalOptimizeResultPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		a.pushGoalOptimizeResult(env.RequestID, msg)
		return nil
	case "session.history.response":
		var msg model.SessionHistoryResultPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		a.pushSessionHistoryResult(env.RequestID, msg)
		return nil
	case "model.test.started", "model.test.progress", "model.test.delta", "model.test.completed", "model.test.failed":
		var msg model.ModelTestEventPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		api.HandleModelTestEvent(env.Type, msg)
		return nil
	case "device.mcp_config.result":
		var msg model.DeviceMCPConfigResultPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		a.pushMCPConfigResult(env.RequestID, msg)
		return nil
	case "device.env_config.result":
		var msg model.DeviceEnvConfigResultPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		a.pushEnvConfigResult(env.RequestID, msg)
		return nil
	case "device.launcher.result":
		var msg model.DeviceLauncherResultPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		a.pushLauncherResult(env.RequestID, msg)
		return nil
	case "device.launcher.agent_preflight.status":
		var msg model.RuntimePreflightStatusPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		return api.New(a.store, a.broker, a.auth, a.mailer, a.streamArtifact, a.notifyService, a.pushService, a.requestDeviceAIConfig, a.requestGoalOptimize, a.requestDeviceMCPConfig, a.requestDeviceEnvConfig, a.requestDeviceLauncher, a.requestDeviceDirectories, a.projectMemoryEnabled).HandleRuntimePreflightStatus(msg)
	case "device.directories.result":
		var msg model.DeviceDirectoriesResultPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		a.pushDirectoriesResult(env.RequestID, msg)
		return nil
	case "project.memory.accepted", "project.memory.progress", "project.memory.checkpoint", "project.memory.candidates", "project.memory.sources", "project.memory.completed", "project.memory.failed":
		var msg model.ProjectMemoryWorkerPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		return a.handleProjectMemoryWorker(device, env.Type, msg)
	case "task.started":
		var msg model.StartedPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		if a.shouldIgnoreTaskEvent(msg.TaskID, env.Type) {
			log.Printf("[handle] ignore late %s for task %s", env.Type, msg.TaskID)
			return nil
		}
		a.broker.Touch(agentID, msg.TaskID)
		a.store.TrackSession(msg.TaskID, msg.SessionID, "active", "任务运行中")
		a.store.SetStatus(msg.TaskID, model.TaskRunning)
		startedFields := map[string]any{
			"task_id":    msg.TaskID,
			"agent_id":   agentID,
			"session_id": msg.SessionID,
		}
		if task, ok := a.store.GetTask(msg.TaskID); ok {
			for key, value := range taskAgeFields(task) {
				startedFields[key] = value
			}
			startedFields["machine_id"] = task.MachineID
			startedFields["project_id"] = task.ProjectID
		}
		appLatencyLog("backend_ws", "task_started_received", startedFields)
		a.store.AddEvent(msg.TaskID, model.Event{
			TaskID:    msg.TaskID,
			Type:      "started",
			SessionID: msg.SessionID,
			SentAt:    time.Now().UTC(),
		})
		return nil
	case "task.delta":
		var msg model.DeltaPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		if a.shouldIgnoreTaskEvent(msg.TaskID, env.Type) {
			log.Printf("[handle] ignore late %s for task %s", env.Type, msg.TaskID)
			return nil
		}
		field := msg.Field
		if field == "" {
			field = "text"
		}
		if a.shouldLogFirstDelta(msg.TaskID, field) {
			deltaFields := map[string]any{
				"task_id":        msg.TaskID,
				"agent_id":       agentID,
				"field":          field,
				"content_length": len(msg.Content),
			}
			if task, ok := a.store.GetTask(msg.TaskID); ok {
				for key, value := range taskAgeFields(task) {
					deltaFields[key] = value
				}
				deltaFields["machine_id"] = task.MachineID
				deltaFields["project_id"] = task.ProjectID
				deltaFields["session_id"] = task.SessionID
			}
			appLatencyLog("backend_ws", "first_delta_received", deltaFields)
		}
		a.store.AddEvent(msg.TaskID, model.Event{
			TaskID:  msg.TaskID,
			Type:    "delta",
			Content: msg.Content,
			Field:   msg.Field,
			SentAt:  time.Now().UTC(),
		})
		return nil
	case "task.progress":
		var msg model.ProgressPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		if a.shouldIgnoreTaskEvent(msg.TaskID, env.Type) {
			log.Printf("[handle] ignore late %s for task %s", env.Type, msg.TaskID)
			return nil
		}
		a.broker.Touch(agentID, msg.TaskID)
		a.store.TouchTask(msg.TaskID)
		a.store.AddEvent(msg.TaskID, model.Event{
			TaskID:    msg.TaskID,
			Type:      "progress",
			Content:   msg.Message,
			SessionID: msg.SessionID,
			Metadata:  msg.Metadata,
			SentAt:    time.Now().UTC(),
		})
		return nil
	case "task.retrying":
		var msg model.RetryingPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		if a.shouldIgnoreTaskEvent(msg.TaskID, env.Type) {
			log.Printf("[handle] ignore late %s for task %s", env.Type, msg.TaskID)
			return nil
		}
		a.broker.Touch(agentID, msg.TaskID)
		a.store.SetStatus(msg.TaskID, model.TaskRunning)
		summary := fmt.Sprintf("连接模型失败，正在重试（第 %d 次）", msg.Attempt)
		if msg.Message != "" {
			summary = fmt.Sprintf("%s：%s", summary, msg.Message)
		}
		a.store.TrackSession(msg.TaskID, msg.SessionID, "active", summary)
		a.store.AddEvent(msg.TaskID, model.Event{
			TaskID:    msg.TaskID,
			Type:      "retrying",
			Content:   msg.Message,
			Attempt:   msg.Attempt,
			Next:      msg.Next,
			SessionID: msg.SessionID,
			SentAt:    time.Now().UTC(),
		})
		return nil
	case "task.compaction_started", "task.compaction_completed":
		var msg model.CompactionPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		if a.shouldIgnoreTaskEvent(msg.TaskID, env.Type) {
			log.Printf("[handle] ignore late %s for task %s", env.Type, msg.TaskID)
			return nil
		}
		eventType := strings.TrimPrefix(env.Type, "task.")
		a.broker.Touch(agentID, msg.TaskID)
		a.store.AddEvent(msg.TaskID, model.Event{
			TaskID:    msg.TaskID,
			Type:      eventType,
			Content:   msg.Reason,
			SessionID: msg.SessionID,
			Metadata: map[string]any{
				"reason": msg.Reason,
			},
			SentAt: time.Now().UTC(),
		})
		return nil
	case "task.tool_updated":
		var msg model.ToolUpdatedPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		if a.shouldIgnoreTaskEvent(msg.TaskID, env.Type) {
			log.Printf("[handle] ignore late %s for task %s", env.Type, msg.TaskID)
			return nil
		}
		a.broker.Touch(agentID, msg.TaskID)
		a.store.AddEvent(msg.TaskID, model.Event{
			TaskID:    msg.TaskID,
			Type:      "tool_updated",
			Tool:      msg.Tool,
			SessionID: msg.SessionID,
			SentAt:    time.Now().UTC(),
		})
		return nil
	case "task.input_ack":
		var msg model.TaskInputAckPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		if task, ok := a.store.GetTask(msg.TaskID); ok && task.AgentID != agentID {
			return fmt.Errorf("task input acknowledgement agent mismatch")
		}
		return a.apiHandler().HandleTaskInputAck(msg)
	case "task.input_applied":
		var msg model.TaskInputAppliedPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		task, ok := a.store.GetTask(msg.TaskID)
		if !ok {
			return fmt.Errorf("task input application task not found")
		}
		if task.AgentID != agentID {
			return fmt.Errorf("task input application agent mismatch")
		}
		if a.shouldIgnoreTaskEvent(msg.TaskID, env.Type) {
			log.Printf("[handle] ignore late %s for task %s", env.Type, msg.TaskID)
			return nil
		}
		metadata := map[string]any{
			"queue_item_id":     msg.QueueItemID,
			"injection_version": msg.InjectionVersion,
		}
		for key, value := range msg.Metadata {
			metadata[key] = value
		}
		a.broker.Touch(agentID, msg.TaskID)
		a.store.AddEvent(msg.TaskID, model.Event{
			TaskID:    msg.TaskID,
			Type:      "input_applied",
			SessionID: msg.SessionID,
			Content:   msg.Content,
			Metadata:  metadata,
			SentAt:    time.Now().UTC(),
		})
		return nil
	case "task.subagent_started":
		var msg model.SubagentStartedPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		task, ok := a.store.GetTask(msg.TaskID)
		if !ok || task.AgentID != agentID {
			return fmt.Errorf("subagent start task ownership mismatch")
		}
		if task.Status == model.TaskCancelled {
			return nil
		}
		a.broker.Touch(agentID, msg.TaskID)
		a.store.AddEvent(msg.TaskID, model.Event{
			TaskID: msg.TaskID, Type: "subagent_started", SessionID: msg.SessionID, SentAt: time.Now().UTC(),
			Metadata: map[string]any{
				"node_id": msg.NodeID, "plan_id": msg.PlanID, "child_session_id": msg.ChildSessionID, "attempt": msg.Attempt,
				"subagent_type": msg.SubagentType, "resolved_agent": msg.ResolvedAgent,
				"title": msg.Title, "background": msg.Background, "started_at": msg.StartedAt,
			},
		})
		api.PersistRecoverableSubagentNodes(a.store, msg.TaskID)
		return nil
	case "task.subagent_result":
		var msg model.SubagentResultPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		task, ok := a.store.GetTask(msg.TaskID)
		if !ok || task.AgentID != agentID {
			return fmt.Errorf("subagent result task ownership mismatch")
		}
		if task.Status == model.TaskCancelled {
			return nil
		}
		if a.subagentResultAlreadyRecorded(msg.TaskID, msg.NodeID, msg.CompletedAt) {
			return nil
		}
		a.broker.Touch(agentID, msg.TaskID)
		a.store.AddEvent(msg.TaskID, model.Event{
			TaskID: msg.TaskID, Type: "subagent_result", SessionID: msg.SessionID, Content: msg.Output, Error: msg.Error, Artifacts: msg.Artifacts, SentAt: time.Now().UTC(),
			Metadata: map[string]any{
				"node_id": msg.NodeID, "plan_id": msg.PlanID, "child_session_id": msg.ChildSessionID, "attempt": msg.Attempt,
				"subagent_type": msg.SubagentType, "resolved_agent": msg.ResolvedAgent,
				"title": msg.Title, "status": msg.Status, "completed_at": msg.CompletedAt, "wake_reason": msg.WakeReason,
			},
		})
		api.PersistRecoverableSubagentNodes(a.store, msg.TaskID)
		return nil
	case "task.subagent_state":
		var msg model.SubagentStatePayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		task, ok := a.store.GetTask(msg.TaskID)
		if !ok || task.AgentID != agentID {
			return fmt.Errorf("subagent state task ownership mismatch")
		}
		if task.Status == model.TaskCancelled {
			return nil
		}
		a.broker.Touch(agentID, msg.TaskID)
		a.store.AddEvent(msg.TaskID, model.Event{
			TaskID: msg.TaskID, Type: "subagent_state", SessionID: msg.SessionID, Error: msg.Error, SentAt: time.Now().UTC(),
			Metadata: map[string]any{
				"node_id": msg.NodeID, "plan_id": msg.PlanID, "child_session_id": msg.ChildSessionID,
				"subagent_type": msg.SubagentType, "title": msg.Title, "prompt": msg.Prompt, "state": msg.State, "attempt": msg.Attempt,
				"depends_on": msg.DependsOn, "blocked_by": msg.BlockedBy, "priority": msg.Priority, "model": msg.Model,
				"pending_instructions": msg.PendingInstructions, "started_at": msg.StartedAt, "completed_at": msg.CompletedAt,
				"updated_at": msg.UpdatedAt,
			},
		})
		api.PersistRecoverableSubagentNodes(a.store, msg.TaskID)
		return nil
	case "task.subagent_control_applied":
		var msg model.SubagentControlPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		task, ok := a.store.GetTask(msg.TaskID)
		if !ok || task.AgentID != agentID {
			return fmt.Errorf("subagent control task ownership mismatch")
		}
		a.store.AddEvent(msg.TaskID, model.Event{
			TaskID: msg.TaskID, Type: "subagent_control_applied", SentAt: time.Now().UTC(),
			Metadata: map[string]any{
				"node_id": msg.NodeID, "action": msg.Action, "instruction": msg.Instruction,
				"priority": msg.Priority, "model": msg.Model, "applied": msg.Applied, "error": msg.Error,
			},
		})
		api.PersistRecoverableSubagentNodes(a.store, msg.TaskID)
		return nil
	case "task.completed":
		var msg model.CompletedPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		if a.shouldIgnoreTaskEvent(msg.TaskID, env.Type) {
			log.Printf("[handle] ignore late %s for task %s", env.Type, msg.TaskID)
			return nil
		}
		completedFields := map[string]any{
			"task_id":    msg.TaskID,
			"agent_id":   agentID,
			"session_id": msg.SessionID,
		}
		if task, ok := a.store.GetTask(msg.TaskID); ok {
			for key, value := range taskAgeFields(task) {
				completedFields[key] = value
			}
			completedFields["machine_id"] = task.MachineID
			completedFields["project_id"] = task.ProjectID
		}
		appLatencyLog("backend_ws", "task_completed_received", completedFields)
		terminalTask, _ := a.store.GetTask(msg.TaskID)
		a.clearTaskLatency(msg.TaskID)
		a.broker.ClearTask(agentID, msg.TaskID)
		a.store.Complete(msg.TaskID, msg.SessionID, msg.Result, msg.Artifacts)
		a.store.TrackSession(msg.TaskID, msg.SessionID, "active", msg.Result)
		a.store.AddEvent(msg.TaskID, model.Event{
			TaskID:  msg.TaskID,
			Type:    "completed",
			Content: msg.Result,
			Metadata: map[string]any{
				"round_result": msg.RoundResult,
				"usage":        msg.Usage,
			},
			Artifacts: msg.Artifacts,
			Files:     msg.Files,
			SessionID: msg.SessionID,
			SentAt:    time.Now().UTC(),
		})
		if task, ok := a.store.GetTask(msg.TaskID); ok {
			a.enqueueProjectMemory(task, model.ProjectMemoryTriggerTaskComplete, msg.TaskID)
		}
		if a.notifyService != nil {
			if task, ok := a.store.GetTask(msg.TaskID); ok {
				log.Printf("dispatch task completed notify: task=%s operator=%d session=%s agent=%s status=%s", task.ID, task.OperatorID, task.SessionID, task.AgentID, task.Status)
				a.notifyService.PersistTaskCompleted(context.Background(), task)
				go a.notifyService.DeliverTaskCompleted(context.Background(), task)
			} else {
				log.Printf("skip task completed notify: task=%s reason=get_task_failed", msg.TaskID)
			}
		} else {
			log.Printf("skip task completed notify: task=%s reason=notify_service_nil", msg.TaskID)
		}
		a.apiHandler().HandleChatQueueTaskTerminal(terminalTask, env.Type)
		return nil
	case "task.waiting_approval":
		var msg model.WaitingApprovalPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		if a.shouldIgnoreTaskEvent(msg.TaskID, env.Type) {
			log.Printf("[handle] ignore late %s for task %s", env.Type, msg.TaskID)
			return nil
		}
		if task, ok := a.store.GetTask(msg.TaskID); ok && task.Status == model.TaskWaitingApproval && task.Approval != nil && task.Approval.PermissionID == msg.PermissionID {
			// A relay reconnect repeats the unresolved approval so the backend can
			// restore state after a restart. It must not create another user alert.
			log.Printf("[handle] ignore duplicate waiting approval for task %s permission %s", msg.TaskID, msg.PermissionID)
			return nil
		}
		a.store.WaitApproval(msg.TaskID, msg.SessionID, &model.Approval{
			PermissionID: msg.PermissionID,
			Permission:   msg.Permission,
			Patterns:     msg.Patterns,
			Metadata:     msg.Metadata,
		})
		a.store.TrackSession(msg.TaskID, msg.SessionID, "waiting_approval", "等待审批")
		a.store.AddEvent(msg.TaskID, model.Event{
			TaskID:       msg.TaskID,
			Type:         "waiting_approval",
			SessionID:    msg.SessionID,
			PermissionID: msg.PermissionID,
			Permission:   msg.Permission,
			Patterns:     msg.Patterns,
			Metadata:     msg.Metadata,
			Content:      "waiting for approval",
			SentAt:       time.Now().UTC(),
		})
		if a.notifyService != nil {
			if task, ok := a.store.GetTask(msg.TaskID); ok {
				log.Printf("dispatch task waiting approval notify: task=%s operator=%d session=%s agent=%s status=%s", task.ID, task.OperatorID, task.SessionID, task.AgentID, task.Status)
				go a.notifyService.NotifyTaskWaitingApproval(context.Background(), task)
			} else {
				log.Printf("skip task waiting approval notify: task=%s reason=get_task_failed", msg.TaskID)
			}
		} else {
			log.Printf("skip task waiting approval notify: task=%s reason=notify_service_nil", msg.TaskID)
		}
		return nil
	case "task.approval_applied":
		var msg model.ApprovalAppliedPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		if a.shouldIgnoreTaskEvent(msg.TaskID, env.Type) {
			log.Printf("[handle] ignore late %s for task %s", env.Type, msg.TaskID)
			return nil
		}
		a.store.Resume(msg.TaskID, msg.SessionID)
		a.store.TrackSession(msg.TaskID, msg.SessionID, "active", "审批已通过")
		a.store.AddEvent(msg.TaskID, model.Event{
			TaskID:       msg.TaskID,
			Type:         "approval_applied",
			SessionID:    msg.SessionID,
			PermissionID: msg.PermissionID,
			Reply:        msg.Reply,
			Content:      "approval applied",
			SentAt:       time.Now().UTC(),
		})
		return nil
	case "task.approval_auto_approved":
		var msg model.ApprovalAutoApprovedPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		if a.shouldIgnoreTaskEvent(msg.TaskID, env.Type) {
			log.Printf("[handle] ignore late %s for task %s", env.Type, msg.TaskID)
			return nil
		}
		a.store.AddEvent(msg.TaskID, model.Event{
			TaskID:       msg.TaskID,
			Type:         "approval_auto_approved",
			SessionID:    msg.SessionID,
			PermissionID: msg.PermissionID,
			Permission:   msg.Permission,
			Patterns:     msg.Patterns,
			Content:      "approval auto approved",
			SentAt:       time.Now().UTC(),
		})
		return nil
	case "task.question_asked":
		var msg model.QuestionAskedPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		if a.shouldIgnoreTaskEvent(msg.TaskID, env.Type) {
			log.Printf("[handle] ignore late %s for task %s", env.Type, msg.TaskID)
			return nil
		}
		a.store.WaitQuestion(msg.TaskID, msg.SessionID, &model.QuestionRequest{
			RequestID: msg.RequestID,
			SessionID: msg.SessionID,
			Questions: msg.Questions,
		})
		a.store.TrackSession(msg.TaskID, msg.SessionID, "active", "等待用户选择")
		a.store.AddEvent(msg.TaskID, model.Event{
			TaskID:    msg.TaskID,
			Type:      "question_asked",
			SessionID: msg.SessionID,
			Question: &model.QuestionRequest{
				RequestID: msg.RequestID,
				SessionID: msg.SessionID,
				Questions: msg.Questions,
			},
			Content: "waiting for question answer",
			SentAt:  time.Now().UTC(),
		})
		if a.notifyService != nil {
			if task, ok := a.store.GetTask(msg.TaskID); ok {
				log.Printf("dispatch task question notify: task=%s operator=%d session=%s agent=%s status=%s", task.ID, task.OperatorID, task.SessionID, task.AgentID, task.Status)
				go a.notifyService.NotifyTaskQuestionAsked(context.Background(), task)
			} else {
				log.Printf("skip task question notify: task=%s reason=get_task_failed", msg.TaskID)
			}
		} else {
			log.Printf("skip task question notify: task=%s reason=notify_service_nil", msg.TaskID)
		}
		return nil
	case "task.question_replied":
		var msg model.QuestionRepliedPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		if a.shouldIgnoreTaskEvent(msg.TaskID, env.Type) {
			log.Printf("[handle] ignore late %s for task %s", env.Type, msg.TaskID)
			return nil
		}
		a.store.ResolveQuestion(msg.TaskID, msg.SessionID)
		a.store.AddEvent(msg.TaskID, model.Event{
			TaskID:    msg.TaskID,
			Type:      "question_replied",
			SessionID: msg.SessionID,
			Question: &model.QuestionRequest{
				RequestID: msg.RequestID,
				SessionID: msg.SessionID,
				Questions: msg.Questions,
			},
			Content: "question answered",
			SentAt:  time.Now().UTC(),
		})
		return nil
	case "task.question_rejected":
		var msg model.QuestionRejectedPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		if a.shouldIgnoreTaskEvent(msg.TaskID, env.Type) {
			log.Printf("[handle] ignore late %s for task %s", env.Type, msg.TaskID)
			return nil
		}
		a.store.ResolveQuestion(msg.TaskID, msg.SessionID)
		a.store.AddEvent(msg.TaskID, model.Event{
			TaskID:    msg.TaskID,
			Type:      "question_rejected",
			SessionID: msg.SessionID,
			Question: &model.QuestionRequest{
				RequestID: msg.RequestID,
				SessionID: msg.SessionID,
				Questions: msg.Questions,
			},
			Content: "question rejected",
			SentAt:  time.Now().UTC(),
		})
		return nil
	case "task.plan_updated":
		var msg model.PlanUpdatedPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		if a.shouldIgnoreTaskEvent(msg.TaskID, env.Type) {
			log.Printf("[handle] ignore late %s for task %s", env.Type, msg.TaskID)
			return nil
		}
		a.store.UpdatePlan(msg.TaskID, msg.SessionID, msg.Plan)
		a.store.AddEvent(msg.TaskID, model.Event{
			TaskID:    msg.TaskID,
			Type:      "plan_updated",
			SessionID: msg.SessionID,
			Plan:      msg.Plan,
			SentAt:    time.Now().UTC(),
		})
		return nil
	case "task.goal_created", "task.goal_continued", "task.goal_checkpoint", "task.goal_heartbeat", "task.goal_paused", "task.goal_completed", "task.goal_failed":
		var msg model.GoalEventPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		if a.shouldIgnoreTaskEvent(msg.TaskID, env.Type) {
			log.Printf("[handle] ignore late %s for task %s", env.Type, msg.TaskID)
			return nil
		}
		eventType := strings.TrimPrefix(env.Type, "task.")
		a.store.UpdateTaskMetadata(msg.TaskID, goalMetadataUpdates(msg, eventType))
		if eventType == "goal_heartbeat" {
			return nil
		}
		a.store.AddEvent(msg.TaskID, model.Event{
			TaskID:    msg.TaskID,
			Type:      eventType,
			Content:   msg.Objective,
			SessionID: msg.SessionID,
			Metadata: map[string]any{
				"goal_id":   msg.GoalID,
				"objective": msg.Objective,
				"status":    msg.Status,
				"reason":    msg.Reason,
				"iteration": msg.Iteration,
				"max":       msg.Max,
				"metadata":  msg.Metadata,
			},
			SentAt: time.Now().UTC(),
		})
		if eventType == "goal_checkpoint" {
			if task, ok := a.store.GetTask(msg.TaskID); ok {
				a.enqueueProjectMemory(task, model.ProjectMemoryTriggerGoalCheckpoint, msg.TaskID)
			}
		}
		return nil
	case "task.failed":
		var msg model.FailedPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		if a.shouldIgnoreTaskEvent(msg.TaskID, env.Type) {
			log.Printf("[handle] ignore late %s for task %s", env.Type, msg.TaskID)
			return nil
		}
		failedFields := map[string]any{
			"task_id":    msg.TaskID,
			"agent_id":   agentID,
			"session_id": msg.SessionID,
			"error":      msg.Error,
		}
		if task, ok := a.store.GetTask(msg.TaskID); ok {
			for key, value := range taskAgeFields(task) {
				failedFields[key] = value
			}
			failedFields["machine_id"] = task.MachineID
			failedFields["project_id"] = task.ProjectID
		}
		appLatencyLog("backend_ws", "task_failed_received", failedFields)
		terminalTask, _ := a.store.GetTask(msg.TaskID)
		if terminalTask != nil {
			terminalTask.Error = msg.Error
		}
		failureSessionID := strings.TrimSpace(msg.SessionID)
		if failureSessionID == "" && terminalTask != nil {
			failureSessionID = terminalTask.SessionID
		}
		a.clearTaskLatency(msg.TaskID)
		a.broker.ClearTask(agentID, msg.TaskID)
		a.store.Fail(msg.TaskID, failureSessionID, msg.Error)
		a.store.TrackSession(msg.TaskID, failureSessionID, "error", msg.Error)
		a.store.AddEvent(msg.TaskID, model.Event{
			TaskID:    msg.TaskID,
			Type:      "failed",
			Error:     msg.Error,
			SessionID: failureSessionID,
			Metadata: map[string]any{
				"error_detail": msg.ErrorDetail,
			},
			SentAt: time.Now().UTC(),
		})
		a.apiHandler().HandleChatQueueTaskTerminal(terminalTask, env.Type)
		return nil
	case "task.cancelled":
		var msg model.FailedPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		if a.shouldIgnoreTaskEvent(msg.TaskID, env.Type) {
			log.Printf("[handle] ignore late %s for task %s", env.Type, msg.TaskID)
			return nil
		}
		cancelledFields := map[string]any{
			"task_id":    msg.TaskID,
			"agent_id":   agentID,
			"session_id": msg.SessionID,
		}
		if task, ok := a.store.GetTask(msg.TaskID); ok {
			for key, value := range taskAgeFields(task) {
				cancelledFields[key] = value
			}
			cancelledFields["machine_id"] = task.MachineID
			cancelledFields["project_id"] = task.ProjectID
		}
		appLatencyLog("backend_ws", "task_cancelled_received", cancelledFields)
		terminalTask, _ := a.store.GetTask(msg.TaskID)
		a.clearTaskLatency(msg.TaskID)
		a.broker.ClearTask(agentID, msg.TaskID)
		a.store.Cancel(msg.TaskID)
		a.store.TrackSession(msg.TaskID, msg.SessionID, "cancelled", msg.Error)
		a.store.AddEvent(msg.TaskID, model.Event{
			TaskID:    msg.TaskID,
			Type:      "cancelled",
			Content:   msg.Error,
			Error:     msg.Error,
			SessionID: msg.SessionID,
			SentAt:    time.Now().UTC(),
		})
		a.apiHandler().HandleChatQueueTaskTerminal(terminalTask, env.Type)
		return nil
	case "artifact.chunk":
		var msg model.ArtifactChunkPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		a.pushArtifactStream(env.RequestID, artifactStreamEvent{
			kind:  "chunk",
			chunk: &msg,
		})
		return nil
	case "artifact.done":
		var msg model.ArtifactDonePayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		a.pushArtifactStream(env.RequestID, artifactStreamEvent{
			kind: "done",
			done: &msg,
		})
		return nil
	case "artifact.failed":
		var msg model.ArtifactFailedPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		a.pushArtifactStream(env.RequestID, artifactStreamEvent{
			kind:   "failed",
			failed: &msg,
		})
		return nil
	default:
		return nil
	}
}
