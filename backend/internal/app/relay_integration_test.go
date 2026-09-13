package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"relay-server/internal/auth"
	"relay-server/internal/broker"
	"relay-server/internal/model"
	"relay-server/internal/store"
)

type relayTestArchive struct {
	operators map[string]model.Operator
}

func (a relayTestArchive) UpsertTask(*model.Task) error             { return nil }
func (a relayTestArchive) AppendEvent(model.Event) error            { return nil }
func (a relayTestArchive) ListEvents(string) ([]model.Event, error) { return nil, nil }
func (a relayTestArchive) LastEventAt(string) (time.Time, bool, error) {
	return time.Time{}, false, nil
}
func (a relayTestArchive) GetTask(string) (*model.Task, error)               { return nil, nil }
func (a relayTestArchive) ListTasks(model.TaskFilter) ([]*model.Task, error) { return nil, nil }
func (a relayTestArchive) UpsertSession(*model.Session) error                { return nil }
func (a relayTestArchive) ListSessions(model.SessionFilter) ([]*model.Session, error) {
	return nil, nil
}
func (a relayTestArchive) AuthenticateOperator(string, string) (*model.Operator, error) {
	return nil, nil
}
func (a relayTestArchive) GetOperatorByKey(operatorKey string) (*model.Operator, error) {
	operator, ok := a.operators[operatorKey]
	if !ok {
		return nil, nil
	}
	return &operator, nil
}
func (a relayTestArchive) GetOperatorEmail(int64) (string, error) { return "", nil }
func (a relayTestArchive) UsernameExists(string) (bool, error)    { return false, nil }
func (a relayTestArchive) EmailExists(string) (bool, error)       { return false, nil }
func (a relayTestArchive) CreateOperator(string, string, string, string) (*model.Operator, error) {
	return nil, nil
}
func (a relayTestArchive) ChangePassword(int64, string) error                   { return nil }
func (a relayTestArchive) DeleteOperator(int64) error                           { return nil }
func (a relayTestArchive) SetDeviceSetting(int64, string, string, string) error { return nil }
func (a relayTestArchive) GetDeviceSettings(int64, string) (map[string]string, error) {
	return nil, nil
}
func (a relayTestArchive) DeleteDeviceSettings(int64, string) error { return nil }
func (a relayTestArchive) ListDeviceSettingAgentIDs(int64, string) ([]string, error) {
	return nil, nil
}
func (a relayTestArchive) UpsertPushDevice(model.PushDevice) (*model.PushDevice, error) {
	return nil, nil
}
func (a relayTestArchive) ListPushDevices(int64) ([]*model.PushDevice, error) {
	return nil, nil
}
func (a relayTestArchive) ListSkills(bool) ([]model.Skill, error) {
	return nil, nil
}
func (a relayTestArchive) UpsertSkill(model.Skill) (*model.Skill, error) {
	return nil, nil
}
func (a relayTestArchive) DeleteSkill(string) error { return nil }
func (a relayTestArchive) ListMCPCatalog(bool) ([]model.MCPCatalogItem, error) {
	return nil, nil
}
func (a relayTestArchive) UpsertMCPCatalogItem(model.MCPCatalogItem) (*model.MCPCatalogItem, error) {
	return nil, nil
}
func (a relayTestArchive) DeleteMCPCatalogItem(string) error { return nil }
func (a relayTestArchive) ListEnvironmentPresets(bool) ([]model.EnvironmentPreset, error) {
	return nil, nil
}
func (a relayTestArchive) UpsertEnvironmentPreset(model.EnvironmentPreset) (*model.EnvironmentPreset, error) {
	return nil, nil
}
func (a relayTestArchive) DeleteEnvironmentPreset(string) error { return nil }
func (a relayTestArchive) ListRuntimeCatalog(bool) ([]model.RuntimeCatalogItem, error) {
	return nil, nil
}
func (a relayTestArchive) UpsertRuntimeCatalogItem(model.RuntimeCatalogItem) (*model.RuntimeCatalogItem, error) {
	return nil, nil
}
func (a relayTestArchive) DeleteRuntimeCatalogItem(string) error { return nil }
func (a relayTestArchive) ListRuntimeVersions(bool) ([]model.RuntimeVersion, error) {
	return nil, nil
}
func (a relayTestArchive) UpsertRuntimeVersion(model.RuntimeVersion) (*model.RuntimeVersion, error) {
	return nil, nil
}
func (a relayTestArchive) DeleteRuntimeVersion(string) error { return nil }
func (a relayTestArchive) ListRuntimeArtifacts(bool) ([]model.RuntimeArtifact, error) {
	return nil, nil
}
func (a relayTestArchive) UpsertRuntimeArtifact(model.RuntimeArtifact) (*model.RuntimeArtifact, error) {
	return nil, nil
}
func (a relayTestArchive) DeleteRuntimeArtifact(string) error { return nil }
func (a relayTestArchive) ListRuntimeMirrors(bool) ([]model.RuntimeMirror, error) {
	return nil, nil
}
func (a relayTestArchive) UpsertRuntimeMirror(model.RuntimeMirror) (*model.RuntimeMirror, error) {
	return nil, nil
}
func (a relayTestArchive) DeleteRuntimeMirror(string) error { return nil }
func (a relayTestArchive) ListToolCatalog(bool) ([]model.ToolCatalogItem, error) {
	return nil, nil
}
func (a relayTestArchive) UpsertToolCatalogItem(model.ToolCatalogItem) (*model.ToolCatalogItem, error) {
	return nil, nil
}
func (a relayTestArchive) DeleteToolCatalogItem(string) error { return nil }
func (a relayTestArchive) ListSemanticAgents(bool) ([]model.SemanticAgentProfile, error) {
	return nil, nil
}
func (a relayTestArchive) UpsertSemanticAgent(model.SemanticAgentProfile) (*model.SemanticAgentProfile, error) {
	return nil, nil
}
func (a relayTestArchive) DeleteSemanticAgent(string) error { return nil }
func (a relayTestArchive) Close() error                     { return nil }

func TestBackendRelayRouterDispatchesTaskAndRecordsCompletion(t *testing.T) {
	operator := model.Operator{
		ID:          1,
		OperatorUID: "operator-e2e",
		Username:    "relay-tester",
		OperatorKey: "operator-key-e2e",
	}
	authManager := auth.NewManager(time.Hour)
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	mem := store.NewMemory(relayTestArchive{
		operators: map[string]model.Operator{
			operator.OperatorKey: operator,
		},
	})
	application := &App{
		store:           mem,
		broker:          broker.New(),
		auth:            authManager,
		taskCleanupTick: time.Hour,
		taskTimeout:     time.Hour,
		taskCancelGrace: time.Second,
		artifactSubs:    map[string]chan artifactStreamEvent{},
		aiConfigSubs:    map[string]chan model.DeviceAIConfigResultPayload{},
		mcpConfigSubs:   map[string]chan model.DeviceMCPConfigResultPayload{},
		launcherSubs:    map[string]chan model.DeviceLauncherResultPayload{},
		directoriesSubs: map[string]chan model.DeviceDirectoriesResultPayload{},
	}
	server := httptest.NewServer(application.Router())
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws/device"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial relay websocket: %v", err)
	}
	defer conn.CloseNow()

	writeEnvelope(t, ctx, conn, model.Envelope{
		Type:      "device.hello",
		RequestID: "hello-1",
		SentAt:    time.Now().UTC().Format(time.RFC3339),
		Payload: model.HelloPayload{
			AgentID:     "agent-e2e",
			MachineID:   "machine-e2e",
			Kind:        "opencode",
			OperatorKey: operator.OperatorKey,
			Hostname:    "relay-e2e-host",
			Version:     "opencode2-e2e",
			Projects: []model.HelloProject{
				{ProjectID: "chat-codex", Root: "/tmp/chat-codex"},
			},
		},
	})

	welcome := readEnvelope(t, ctx, conn)
	if welcome.Type != "device.welcome" {
		t.Fatalf("expected device.welcome, got %s", welcome.Type)
	}
	var welcomePayload model.WelcomePayload
	decodePayload(t, welcome.Payload, &welcomePayload)
	if welcomePayload.AgentID != "agent-e2e" {
		t.Fatalf("unexpected welcome agent: %+v", welcomePayload)
	}

	agents := getJSON[[]model.Agent](t, server.URL+"/api/agents", token)
	if len(agents) != 1 || agents[0].ID != "agent-e2e" {
		t.Fatalf("expected broker to expose online agent, got %+v", agents)
	}

	taskBody := map[string]any{
		"agent_id":   "agent-e2e",
		"project_id": "chat-codex",
		"parts": []model.Part{
			{Type: "text", Text: "真实 backend relay 链路测试"},
		},
		"metadata": map[string]string{
			"source":              "relay-integration-test",
			"goal":                "完成真实 relay goal 测试",
			"goal_max_iterations": "3",
		},
	}
	taskResult := make(chan model.Task, 1)
	taskErr := make(chan error, 1)
	go func() {
		task, err := postJSON[model.Task](ctx, server.URL+"/api/tasks", token, taskBody)
		if err != nil {
			taskErr <- err
			return
		}
		taskResult <- task
	}()

	run := readEnvelope(t, ctx, conn)
	if run.Type != "task.run" {
		t.Fatalf("expected task.run, got %s", run.Type)
	}
	var runPayload model.RunPayload
	decodePayload(t, run.Payload, &runPayload)
	if runPayload.AgentID != "agent-e2e" || runPayload.MachineID != "machine-e2e" || runPayload.ProjectID != "chat-codex" {
		t.Fatalf("unexpected task.run payload: %+v", runPayload)
	}
	if len(runPayload.Parts) != 1 || runPayload.Parts[0].Text != "真实 backend relay 链路测试" {
		t.Fatalf("unexpected task.run parts: %+v", runPayload.Parts)
	}
	if runPayload.Metadata["goal"] != "完成真实 relay goal 测试" || runPayload.Metadata["goal_max_iterations"] != "3" {
		t.Fatalf("unexpected task.run metadata: %+v", runPayload.Metadata)
	}

	var created model.Task
	select {
	case err := <-taskErr:
		t.Fatalf("create task: %v", err)
	case created = <-taskResult:
	case <-ctx.Done():
		t.Fatalf("create task timed out: %v", ctx.Err())
	}
	if created.ID == "" || created.ID != runPayload.TaskID {
		t.Fatalf("created task and run payload mismatch: task=%+v run=%+v", created, runPayload)
	}
	if created.Status != model.TaskDispatched {
		t.Fatalf("expected dispatched task, got %s", created.Status)
	}
	if created.Metadata["goal"] != "完成真实 relay goal 测试" || created.Metadata["goal_max_iterations"] != "3" {
		t.Fatalf("unexpected created task metadata: %+v", created.Metadata)
	}
	if created.Metadata["goal_status"] != "pending" || created.Metadata["goal_iteration"] != "0" {
		t.Fatalf("expected initial goal metadata, got %+v", created.Metadata)
	}

	writeEnvelope(t, ctx, conn, model.Envelope{
		Type:    "task.started",
		SentAt:  time.Now().UTC().Format(time.RFC3339),
		Payload: model.StartedPayload{TaskID: runPayload.TaskID, SessionID: "session-e2e"},
	})
	started := waitForTaskStatus(t, ctx, server.URL, token, runPayload.TaskID, model.TaskRunning)
	if started.SessionID != "session-e2e" {
		t.Fatalf("expected started task session to be persisted, got %+v", started)
	}
	writeEnvelope(t, ctx, conn, model.Envelope{
		Type:    "task.delta",
		SentAt:  time.Now().UTC().Format(time.RFC3339),
		Payload: model.DeltaPayload{TaskID: runPayload.TaskID, Content: "delta from device", Field: "message"},
	})
	writeEnvelope(t, ctx, conn, model.Envelope{
		Type:   "task.tool_updated",
		SentAt: time.Now().UTC().Format(time.RFC3339),
		Payload: model.ToolUpdatedPayload{
			TaskID:    runPayload.TaskID,
			SessionID: "session-e2e",
			Tool: &model.ToolCall{
				ID:     "part-tool-1",
				CallID: "call-tool-1",
				Tool:   "read",
				Status: "running",
				Input: map[string]any{
					"path": "/tmp/chat-codex/README.md",
				},
				StartedAt: time.Now().UnixMilli(),
			},
		},
	})
	writeEnvelope(t, ctx, conn, model.Envelope{
		Type:   "task.tool_updated",
		SentAt: time.Now().UTC().Format(time.RFC3339),
		Payload: model.ToolUpdatedPayload{
			TaskID:    runPayload.TaskID,
			SessionID: "session-e2e",
			Tool: &model.ToolCall{
				ID:     "part-tool-1",
				CallID: "call-tool-1",
				Tool:   "read",
				Status: "completed",
				Input: map[string]any{
					"path": "/tmp/chat-codex/README.md",
				},
				Output:    "tool output from device",
				StartedAt: time.Now().Add(-time.Second).UnixMilli(),
				EndedAt:   time.Now().UnixMilli(),
			},
		},
	})
	writeEnvelope(t, ctx, conn, model.Envelope{
		Type:   "task.compaction_started",
		SentAt: time.Now().UTC().Format(time.RFC3339),
		Payload: model.CompactionPayload{
			TaskID:    runPayload.TaskID,
			SessionID: "session-e2e",
			Reason:    "auto",
		},
	})
	writeEnvelope(t, ctx, conn, model.Envelope{
		Type:   "task.compaction_completed",
		SentAt: time.Now().UTC().Format(time.RFC3339),
		Payload: model.CompactionPayload{
			TaskID:    runPayload.TaskID,
			SessionID: "session-e2e",
		},
	})
	writeEnvelope(t, ctx, conn, model.Envelope{
		Type:   "task.goal_created",
		SentAt: time.Now().UTC().Format(time.RFC3339),
		Payload: model.GoalEventPayload{
			TaskID:    runPayload.TaskID,
			SessionID: "session-e2e",
			GoalID:    "goal-e2e",
			Objective: "完成真实 relay goal 测试",
			Status:    "active",
			Iteration: 0,
			Max:       3,
		},
	})
	writeEnvelope(t, ctx, conn, model.Envelope{
		Type:   "task.goal_heartbeat",
		SentAt: time.Now().UTC().Format(time.RFC3339),
		Payload: model.GoalEventPayload{
			TaskID:    runPayload.TaskID,
			SessionID: "session-e2e",
			GoalID:    "goal-e2e",
			Objective: "完成真实 relay goal 测试",
			Status:    "active",
			Iteration: 1,
			Max:       3,
		},
	})
	writeEnvelope(t, ctx, conn, model.Envelope{
		Type:   "task.goal_checkpoint",
		SentAt: time.Now().UTC().Format(time.RFC3339),
		Payload: model.GoalEventPayload{
			TaskID:    runPayload.TaskID,
			SessionID: "session-e2e",
			GoalID:    "goal-e2e",
			Objective: "完成真实 relay goal 测试",
			Status:    "active",
			Iteration: 1,
			Max:       3,
			Metadata: map[string]any{
				"checkpoint": "first checkpoint",
			},
		},
	})
	writeEnvelope(t, ctx, conn, model.Envelope{
		Type:   "task.goal_completed",
		SentAt: time.Now().UTC().Format(time.RFC3339),
		Payload: model.GoalEventPayload{
			TaskID:    runPayload.TaskID,
			SessionID: "session-e2e",
			GoalID:    "goal-e2e",
			Objective: "完成真实 relay goal 测试",
			Status:    "completed",
			Iteration: 1,
			Max:       3,
		},
	})
	writeEnvelope(t, ctx, conn, model.Envelope{
		Type:   "task.completed",
		SentAt: time.Now().UTC().Format(time.RFC3339),
		Payload: model.CompletedPayload{
			TaskID:      runPayload.TaskID,
			SessionID:   "session-e2e",
			Result:      "completed from device",
			RoundResult: "latest injected round",
			Artifacts: []model.Artifact{
				{ID: "artifact-1", Filename: "result.txt", MIME: "text/plain", SizeBytes: 12},
			},
			Usage: map[string]any{
				"input_tokens":  120,
				"output_tokens": 480,
				"total_tokens":  600,
			},
		},
	})

	completed := waitForTaskStatus(t, ctx, server.URL, token, runPayload.TaskID, model.TaskCompleted)
	if completed.Result != "completed from device" {
		t.Fatalf("unexpected completed result: %+v", completed)
	}
	if completed.SessionID != "session-e2e" || len(completed.Artifacts) != 1 {
		t.Fatalf("expected session and artifact to be recorded, got %+v", completed)
	}
	if completed.Metadata["goal_status"] != "completed" ||
		completed.Metadata["goal_iteration"] != "1" ||
		completed.Metadata["goal_checkpoint"] != "first checkpoint" ||
		completed.Metadata["goal_last_event"] != "goal_completed" ||
		completed.Metadata["goal_session_id"] != "session-e2e" ||
		completed.Metadata["goal_last_heartbeat_at"] == "" {
		t.Fatalf("expected persisted goal metadata, got %+v", completed.Metadata)
	}

	eventsBody := getRaw(t, server.URL+"/api/tasks/"+runPayload.TaskID+"/events", token)
	if !strings.Contains(eventsBody, `"usage":{"input_tokens":120,"output_tokens":480,"total_tokens":600}`) {
		t.Fatalf("expected completed event usage to be persisted, got %s", eventsBody)
	}
	for _, expected := range []string{
		"event: dispatched",
		"event: started",
		"event: delta",
		"event: tool_updated",
		"event: compaction_started",
		"event: compaction_completed",
		"event: goal_created",
		"event: goal_checkpoint",
		"event: goal_completed",
		"event: completed",
		"delta from device",
		"tool output from device",
		"call-tool-1",
		"auto",
		"first checkpoint",
		"完成真实 relay goal 测试",
		"completed from device",
		"latest injected round",
	} {
		if !strings.Contains(eventsBody, expected) {
			t.Fatalf("task events missing %q in body:\n%s", expected, eventsBody)
		}
	}
	if strings.Contains(eventsBody, "event: goal_heartbeat") {
		t.Fatalf("goal heartbeat should update metadata without polluting SSE history:\n%s", eventsBody)
	}
}

func TestBackendRelayRouterPreservesFailedErrorDetailInTaskEvents(t *testing.T) {
	operator := model.Operator{
		ID:          1,
		OperatorUID: "operator-error-detail",
		Username:    "relay-error-detail",
		OperatorKey: "operator-key-error-detail",
	}
	authManager := auth.NewManager(time.Hour)
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	mem := store.NewMemory(relayTestArchive{
		operators: map[string]model.Operator{
			operator.OperatorKey: operator,
		},
	})
	application := &App{
		store:           mem,
		broker:          broker.New(),
		auth:            authManager,
		taskCleanupTick: time.Hour,
		taskTimeout:     time.Hour,
		taskCancelGrace: time.Second,
		artifactSubs:    map[string]chan artifactStreamEvent{},
		aiConfigSubs:    map[string]chan model.DeviceAIConfigResultPayload{},
		mcpConfigSubs:   map[string]chan model.DeviceMCPConfigResultPayload{},
		launcherSubs:    map[string]chan model.DeviceLauncherResultPayload{},
		directoriesSubs: map[string]chan model.DeviceDirectoriesResultPayload{},
	}
	device := application.broker.Add(nil, model.HelloPayload{
		AgentID:   "agent-error-detail",
		MachineID: "machine-error-detail",
		Kind:      "opencode",
		Hostname:  "error-detail-host",
		Version:   "test",
	}, operator.ID)
	task := mem.CreateTask(
		"agent-error-detail",
		"machine-error-detail",
		"chat-codex",
		"/tmp/chat-codex",
		"session-error-detail",
		[]model.Part{{Type: "text", Text: "触发空输出失败"}},
		nil,
		operator.ID,
	)
	mem.SetStatus(task.ID, model.TaskRunning)

	if err := application.handle(device, mustEnvelopeBytes(t, model.Envelope{
		Type:   "task.failed",
		SentAt: time.Now().UTC().Format(time.RFC3339),
		Payload: model.FailedPayload{
			TaskID:      task.ID,
			Error:       "模型未返回正文，本轮未产生可展示输出，请重试或切换供应商",
			ErrorDetail: "原因: empty_output\nattempt_1: message_id=msg_1 elapsed_ms=123 part_count=0 has_error=false",
		},
	})); err != nil {
		t.Fatalf("handle task.failed: %v", err)
	}
	failedTask, found := mem.GetTask(task.ID)
	if !found || failedTask.SessionID != "session-error-detail" {
		t.Fatalf("task.failed without session id lost its existing session: %+v", failedTask)
	}

	apiServer := httptest.NewServer(application.Router())
	defer apiServer.Close()
	eventsBody := getRaw(t, apiServer.URL+"/api/tasks/"+task.ID+"/events", token)
	for _, expected := range []string{
		"event: failed",
		`"error_detail":"原因: empty_output\nattempt_1: message_id=msg_1 elapsed_ms=123 part_count=0 has_error=false"`,
		"模型未返回正文",
	} {
		if !strings.Contains(eventsBody, expected) {
			t.Fatalf("task events missing %q in body:\n%s", expected, eventsBody)
		}
	}
}

func TestBackendRelayRouterPersistsSubagentResultArtifactsExactlyOnce(t *testing.T) {
	operator := model.Operator{ID: 1, OperatorKey: "operator-subagent-artifacts"}
	mem := store.NewMemory(relayTestArchive{operators: map[string]model.Operator{operator.OperatorKey: operator}})
	application := &App{
		store:        mem,
		broker:       broker.New(),
		auth:         auth.NewManager(time.Hour),
		artifactSubs: map[string]chan artifactStreamEvent{},
	}
	device := application.broker.Add(nil, model.HelloPayload{
		AgentID: "agent-subagent-artifacts", MachineID: "machine-subagent-artifacts", Kind: "opencode", Version: "test",
	}, operator.ID)
	task := mem.CreateTask("agent-subagent-artifacts", "machine-subagent-artifacts", "project", "/tmp/project", "parent-session", nil, nil, operator.ID)
	artifact := model.Artifact{
		ID: "artifact-report", Filename: "report.txt", RelativePath: ".chatcodex-artifacts/" + task.ID + "/subagents/inspect/report.txt", SizeBytes: 42,
	}
	envelope := model.Envelope{
		Type: "task.subagent_result",
		Payload: model.SubagentResultPayload{
			TaskID: task.ID, SessionID: "parent-session", NodeID: "inspect", PlanID: "plan-artifacts", ChildSessionID: "child-inspect",
			Attempt: 1, SubagentType: "repo-explorer", ResolvedAgent: "explore", Title: "Inspect repository", Status: "completed",
			Output: "inspection complete", Artifacts: []model.Artifact{artifact}, CompletedAt: 1_700_000_000_000,
		},
	}
	for i := 0; i < 2; i++ {
		if err := application.handle(device, mustEnvelopeBytes(t, envelope)); err != nil {
			t.Fatalf("handle subagent result %d: %v", i, err)
		}
	}
	events := mem.Events(task.ID)
	if len(events) != 1 {
		t.Fatalf("expected idempotent single subagent result event, got %+v", events)
	}
	if got := events[0]; got.Type != "subagent_result" || got.Content != "inspection complete" || len(got.Artifacts) != 1 || got.Artifacts[0] != artifact || got.Metadata.(map[string]any)["node_id"] != "inspect" {
		t.Fatalf("unexpected persisted subagent result: %+v", got)
	}
}

func TestBackendAcceptsDetachedSubagentResultAfterParentFailure(t *testing.T) {
	operator := model.Operator{ID: 1, OperatorKey: "operator-detached-result"}
	mem := store.NewMemory(relayTestArchive{operators: map[string]model.Operator{operator.OperatorKey: operator}})
	application := &App{store: mem, broker: broker.New(), auth: auth.NewManager(time.Hour), artifactSubs: map[string]chan artifactStreamEvent{}}
	device := application.broker.Add(nil, model.HelloPayload{AgentID: "agent-detached-result", MachineID: "machine-detached-result"}, operator.ID)
	task := mem.CreateTask("agent-detached-result", "machine-detached-result", "project", "/tmp/project", "parent-session", nil, nil, operator.ID)
	mem.Fail(task.ID, task.SessionID, "parent provider error")
	mem.AddEvent(task.ID, model.Event{TaskID: task.ID, Type: "subagent_state", SessionID: task.SessionID, Metadata: map[string]any{
		"node_id": "inspect", "plan_id": "plan-detached", "child_session_id": "child-inspect", "subagent_type": "repo-explorer",
		"prompt": "inspect repository", "state": "recovering", "attempt": 1,
	}})

	err := application.handle(device, mustEnvelopeBytes(t, model.Envelope{Type: "task.subagent_result", Payload: model.SubagentResultPayload{
		TaskID: task.ID, SessionID: task.SessionID, NodeID: "inspect", PlanID: "plan-detached", ChildSessionID: "child-inspect",
		Attempt: 1, Status: "completed", Output: "inspection complete", CompletedAt: 42,
	}}))
	if err != nil {
		t.Fatalf("handle detached subagent result: %v", err)
	}
	resultEvents := 0
	for _, event := range mem.Events(task.ID) {
		if event.Type == "subagent_result" {
			resultEvents++
		}
	}
	if resultEvents != 1 {
		t.Fatalf("expected detached result after parent failure, events=%+v", mem.Events(task.ID))
	}
}

func TestBackendRouterToolCatalogAdminCRUD(t *testing.T) {
	authManager := auth.NewManager(time.Hour)
	token, err := authManager.Issue(model.Operator{ID: 1, Username: "admin"})
	if err != nil {
		t.Fatalf("issue admin token: %v", err)
	}

	application := &App{
		store:            store.NewMemory(relayTestArchive{}),
		broker:           broker.New(),
		auth:             authManager,
		taskCleanupTick:  time.Hour,
		taskTimeout:      time.Hour,
		taskCancelGrace:  time.Second,
		artifactSubs:     map[string]chan artifactStreamEvent{},
		aiConfigSubs:     map[string]chan model.DeviceAIConfigResultPayload{},
		mcpConfigSubs:    map[string]chan model.DeviceMCPConfigResultPayload{},
		envConfigSubs:    map[string]chan model.DeviceEnvConfigResultPayload{},
		launcherSubs:     map[string]chan model.DeviceLauncherResultPayload{},
		directoriesSubs:  map[string]chan model.DeviceDirectoriesResultPayload{},
		goalOptimizeSubs: map[string]chan model.GoalOptimizeResultPayload{},
		taskLatency:      map[string]taskLatencyState{},
	}
	server := httptest.NewServer(application.Router())
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	type toolCatalogList struct {
		Items []model.ToolCatalogItem `json:"items"`
	}
	initial := getJSON[toolCatalogList](t, server.URL+"/api/admin/tool-catalog", token)
	if !hasToolCatalogID(initial.Items, "read") || !hasToolCatalogID(initial.Items, "bash-file") {
		t.Fatalf("expected default tool catalog items, got %+v", initial.Items)
	}

	created, err := postJSONWithStatus[model.ToolCatalogItem](ctx, server.URL+"/api/admin/tool-catalog", token, map[string]any{
		"id":             "router-test-tool",
		"name":           "路由测试工具",
		"description":    "通过 app router 验证工具库接口",
		"category":       "测试",
		"permission_key": "bash",
		"pattern":        "echo *",
		"default_action": "ask",
		"enabled":        true,
		"tags":           []string{"test", "router"},
		"sort_order":     6,
	}, http.StatusOK)
	if err != nil {
		t.Fatalf("create tool via router: %v", err)
	}
	if created.ID != "router-test-tool" || created.PermissionKey != "bash" || created.Pattern != "echo *" {
		t.Fatalf("unexpected created tool: %+v", created)
	}

	updated, err := postJSONWithStatus[model.ToolCatalogItem](ctx, server.URL+"/api/admin/tool-catalog/router-test-tool", token, map[string]any{
		"name":           "路由测试工具更新",
		"description":    "更新默认权限",
		"category":       "测试",
		"permission_key": "bash",
		"pattern":        "echo *",
		"default_action": "allow",
		"enabled":        true,
		"tags":           []string{"test", "router"},
		"sort_order":     6,
	}, http.StatusOK)
	if err != nil {
		t.Fatalf("update tool via router: %v", err)
	}
	if updated.DefaultAction != "allow" {
		t.Fatalf("expected updated default action, got %+v", updated)
	}

	listed := getJSON[toolCatalogList](t, server.URL+"/api/admin/tool-catalog", token)
	found := false
	for _, item := range listed.Items {
		if item.ID == "router-test-tool" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("created tool not found in router list: %+v", listed.Items)
	}

	if _, err := postJSONWithStatus[map[string]any](ctx, server.URL+"/api/admin/tool-catalog/router-test-tool/delete", token, map[string]any{}, http.StatusOK); err != nil {
		t.Fatalf("delete tool via router: %v", err)
	}
}

func hasToolCatalogID(items []model.ToolCatalogItem, id string) bool {
	for _, item := range items {
		if item.ID == id {
			return true
		}
	}
	return false
}

func TestBackendRelayRouterForwardsAIConfigAPIMode(t *testing.T) {
	operator := model.Operator{
		ID:          1,
		OperatorUID: "operator-ai-config-e2e",
		Username:    "relay-ai-config-tester",
		OperatorKey: "operator-key-ai-config-e2e",
	}
	authManager := auth.NewManager(time.Hour)
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	application := &App{
		store: store.NewMemory(relayTestArchive{
			operators: map[string]model.Operator{
				operator.OperatorKey: operator,
			},
		}),
		broker:           broker.New(),
		auth:             authManager,
		taskCleanupTick:  time.Hour,
		taskTimeout:      time.Hour,
		taskCancelGrace:  time.Second,
		artifactSubs:     map[string]chan artifactStreamEvent{},
		aiConfigSubs:     map[string]chan model.DeviceAIConfigResultPayload{},
		goalOptimizeSubs: map[string]chan model.GoalOptimizeResultPayload{},
		mcpConfigSubs:    map[string]chan model.DeviceMCPConfigResultPayload{},
		launcherSubs:     map[string]chan model.DeviceLauncherResultPayload{},
		directoriesSubs:  map[string]chan model.DeviceDirectoriesResultPayload{},
	}
	server := httptest.NewServer(application.Router())
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws/device", nil)
	if err != nil {
		t.Fatalf("dial relay websocket: %v", err)
	}
	defer conn.CloseNow()

	writeEnvelope(t, ctx, conn, model.Envelope{
		Type:      "device.hello",
		RequestID: "hello-ai-config-1",
		SentAt:    time.Now().UTC().Format(time.RFC3339),
		Payload: model.HelloPayload{
			AgentID:     "machine-ai-config:chat-codex",
			MachineID:   "machine-ai-config",
			Kind:        "launcher",
			OperatorKey: operator.OperatorKey,
			Hostname:    "relay-ai-config-host",
			Version:     "opencode2-ai-config-e2e",
		},
	})
	welcome := readEnvelope(t, ctx, conn)
	if welcome.Type != "device.welcome" {
		t.Fatalf("expected device.welcome, got %s", welcome.Type)
	}

	saveResult := make(chan map[string]any, 1)
	saveErr := make(chan error, 1)
	go func() {
		data, err := postJSONWithStatus[map[string]any](
			ctx,
			server.URL+"/api/devices/machine-ai-config/ai-config/save",
			token,
			map[string]any{
				"provider": "demo-responses",
				"base_url": "https://api.example.com/v1",
				"api_key":  "sk-test",
				"api_mode": "responses",
				"model":    "gpt-5.4",
				"models":   []model.DeviceAIModel{{ID: "gpt-5.4", Name: "gpt-5.4"}},
				"force":    true,
			},
			http.StatusOK,
		)
		if err != nil {
			saveErr <- err
			return
		}
		saveResult <- data
	}()

	type envelopeResult struct {
		envelope model.Envelope
		err      error
	}
	runResult := make(chan envelopeResult, 1)
	go func() {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			runResult <- envelopeResult{err: err}
			return
		}
		var envelope model.Envelope
		if err := json.Unmarshal(raw, &envelope); err != nil {
			runResult <- envelopeResult{err: err}
			return
		}
		runResult <- envelopeResult{envelope: envelope}
	}()
	var run model.Envelope
	select {
	case err := <-saveErr:
		t.Fatalf("save ai config before dispatch: %v", err)
	case result := <-runResult:
		if result.err != nil {
			t.Fatalf("read ai config dispatch: %v", result.err)
		}
		run = result.envelope
	case <-ctx.Done():
		t.Fatalf("wait ai config dispatch: %v", ctx.Err())
	}
	if run.Type != "device.ai_config.save" {
		t.Fatalf("expected device.ai_config.save, got %s", run.Type)
	}
	var payload model.DeviceAIConfigSavePayload
	decodePayload(t, run.Payload, &payload)
	if payload.Provider != "demo-responses" ||
		payload.BaseURL != "https://api.example.com/v1" ||
		payload.APIMode != "responses" ||
		payload.Config.APIMode != "responses" ||
		payload.Config.Model != "gpt-5.4" ||
		len(payload.Config.Models) != 1 ||
		payload.Config.Models[0].ID != "gpt-5.4" {
		t.Fatalf("unexpected ai config save payload: %+v", payload)
	}

	writeEnvelope(t, ctx, conn, model.Envelope{
		Type:      "device.ai_config.result",
		RequestID: run.RequestID,
		SentAt:    time.Now().UTC().Format(time.RFC3339),
		Payload: model.DeviceAIConfigResultPayload{
			Action:  "save",
			Success: true,
			Config: &model.DeviceAIConfigPreview{
				Exists:       true,
				ConfigPath:   "/tmp/opencode.json",
				Provider:     "demo-responses",
				BaseURL:      "https://api.example.com/v1",
				APIKeyMasked: "sk-****",
				APIMode:      "responses",
				Model:        "gpt-5.4",
				Models: []model.DeviceAIModel{{
					ID:   "gpt-5.4",
					Name: "gpt-5.4",
				}},
				Providers: []model.DeviceAIProvider{{
					ID:           "demo-responses",
					BaseURL:      "https://api.example.com/v1",
					APIKeyMasked: "sk-****",
					APIMode:      "responses",
					Models: []model.DeviceAIModel{{
						ID:   "gpt-5.4",
						Name: "gpt-5.4",
					}},
				}},
			},
		},
	})

	var response map[string]any
	select {
	case err := <-saveErr:
		t.Fatalf("save ai config: %v", err)
	case response = <-saveResult:
	case <-ctx.Done():
		t.Fatalf("save ai config timed out: %v", ctx.Err())
	}
	config, ok := response["config"].(map[string]any)
	if !ok {
		t.Fatalf("expected config response, got %#v", response)
	}
	if config["api_mode"] != "responses" {
		t.Fatalf("expected response api_mode responses, got %#v", config)
	}
	providers, ok := config["providers"].([]any)
	if !ok || len(providers) != 1 {
		t.Fatalf("expected response providers, got %#v", config["providers"])
	}
	provider, ok := providers[0].(map[string]any)
	if !ok || provider["api_mode"] != "responses" {
		t.Fatalf("expected provider api_mode responses, got %#v", providers[0])
	}
}

func TestHeartbeatDoesNotKeepSilentTaskRunningForever(t *testing.T) {
	operator := model.Operator{
		ID:          1,
		OperatorUID: "operator-stale",
		Username:    "relay-stale-tester",
		OperatorKey: "operator-key-stale",
	}
	mem := store.NewMemory(relayTestArchive{
		operators: map[string]model.Operator{
			operator.OperatorKey: operator,
		},
	})
	application := &App{
		store:            mem,
		broker:           broker.New(),
		auth:             auth.NewManager(time.Hour),
		taskCleanupTick:  time.Hour,
		taskTimeout:      20 * time.Millisecond,
		taskCancelGrace:  time.Hour,
		artifactSubs:     map[string]chan artifactStreamEvent{},
		aiConfigSubs:     map[string]chan model.DeviceAIConfigResultPayload{},
		goalOptimizeSubs: map[string]chan model.GoalOptimizeResultPayload{},
		mcpConfigSubs:    map[string]chan model.DeviceMCPConfigResultPayload{},
		launcherSubs:     map[string]chan model.DeviceLauncherResultPayload{},
		directoriesSubs:  map[string]chan model.DeviceDirectoriesResultPayload{},
	}

	device := application.broker.Add(nil, model.HelloPayload{
		AgentID:   "agent-stale",
		MachineID: "machine-stale",
		Kind:      "opencode",
		Hostname:  "stale-host",
		Version:   "test",
	}, operator.ID)
	task := mem.CreateTask("agent-stale", "machine-stale", "chat-codex", "/tmp/chat-codex", "session-stale", []model.Part{
		{Type: "text", Text: "继续"},
	}, nil, operator.ID)
	mem.SetStatus(task.ID, model.TaskRunning)
	mem.AddEvent(task.ID, model.Event{
		TaskID:    task.ID,
		Type:      "started",
		SessionID: "session-stale",
		SentAt:    time.Now().UTC().Add(-time.Minute),
	})

	if err := application.handle(device, mustEnvelopeBytes(t, model.Envelope{
		Type:      "device.heartbeat",
		RequestID: "heartbeat-stale",
		SentAt:    time.Now().UTC().Format(time.RFC3339),
		Payload: model.HeartbeatPayload{
			RunningTaskID: task.ID,
		},
	})); err != nil {
		t.Fatalf("handle heartbeat: %v", err)
	}
	if got, ok := application.broker.Get("agent-stale", operator.ID); !ok || got.CurrentTask != task.ID {
		t.Fatalf("expected active heartbeat to keep current task on broker, got ok=%v agent=%+v", ok, got)
	}

	cutoff := time.Now().UTC().Add(-application.taskTimeout)
	if !application.taskLastProgressAt(task).Before(cutoff) {
		t.Fatalf("heartbeat must not refresh task progress: progress=%s cutoff=%s", application.taskLastProgressAt(task), cutoff)
	}
	stored, ok := mem.GetTask(task.ID)
	if !ok || stored.Status != model.TaskRunning {
		t.Fatalf("task missing")
	}
	mem.MarkCancelling(task.ID, "session-stale", "任务超时，正在终止执行（20ms 无响应）")
	application.broker.ClearTaskForOperator(operator.ID, "agent-stale", task.ID)
	stored, _ = mem.GetTask(task.ID)
	if stored.Status != model.TaskCancelling {
		t.Fatalf("expected stale task to enter cancelling, got %+v", stored)
	}
	if got, ok := application.broker.Get("agent-stale", operator.ID); !ok || got.CurrentTask != "" {
		t.Fatalf("expected stale task cleared from broker, got ok=%v agent=%+v", ok, got)
	}
}

func TestDuplicateWaitingApprovalAfterRelayReconnectIsIdempotent(t *testing.T) {
	operator := model.Operator{
		ID:          1,
		OperatorUID: "operator-approval-reconnect",
		Username:    "approval-reconnect-tester",
		OperatorKey: "operator-key-approval-reconnect",
	}
	mem := store.NewMemory(relayTestArchive{
		operators: map[string]model.Operator{operator.OperatorKey: operator},
	})
	application := &App{
		store:  mem,
		broker: broker.New(),
		auth:   auth.NewManager(time.Hour),
	}
	device := application.broker.Add(nil, model.HelloPayload{
		AgentID:   "agent-approval-reconnect",
		MachineID: "machine-approval-reconnect",
		Kind:      "opencode",
	}, operator.ID)
	task := mem.CreateTask(
		"agent-approval-reconnect",
		"machine-approval-reconnect",
		"chat-codex",
		"/tmp/chat-codex",
		"session-approval-reconnect",
		[]model.Part{{Type: "text", Text: "更新待办"}},
		nil,
		operator.ID,
	)
	payload := model.WaitingApprovalPayload{
		TaskID:       task.ID,
		SessionID:    task.SessionID,
		PermissionID: "permission-reconnect-1",
		Permission:   "todowrite",
		Patterns:     []string{"*"},
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := application.handle(device, mustEnvelopeBytes(t, model.Envelope{
			Type:      "task.waiting_approval",
			RequestID: "approval-reconnect",
			SentAt:    time.Now().UTC().Format(time.RFC3339),
			Payload:   payload,
		})); err != nil {
			t.Fatalf("handle waiting approval attempt %d: %v", attempt, err)
		}
	}

	stored, ok := mem.GetTask(task.ID)
	if !ok {
		t.Fatal("task missing")
	}
	if stored.Status != model.TaskWaitingApproval || stored.Approval == nil || stored.Approval.PermissionID != payload.PermissionID {
		t.Fatalf("expected unresolved approval to remain unchanged, got %+v", stored)
	}
	events := mem.Events(task.ID)
	if len(events) != 1 || events[0].Type != "waiting_approval" {
		t.Fatalf("expected exactly one approval wait event after reconnect replay, got %+v", events)
	}
}

func TestDisconnectGraceKeepsTaskWhenAgentReconnects(t *testing.T) {
	operator := model.Operator{ID: 1, OperatorKey: "operator-key-disconnect-grace"}
	mem := store.NewMemory(relayTestArchive{
		operators: map[string]model.Operator{operator.OperatorKey: operator},
	})
	application := &App{
		store:               mem,
		broker:              broker.New(),
		auth:                auth.NewManager(time.Hour),
		taskDisconnectGrace: 20 * time.Millisecond,
	}
	device := application.broker.Add(nil, model.HelloPayload{
		AgentID:   "agent-disconnect-grace",
		MachineID: "machine-disconnect-grace",
		Kind:      "opencode",
	}, operator.ID)
	task := mem.CreateTask("agent-disconnect-grace", "machine-disconnect-grace", "chat-codex", "/tmp/chat-codex", "session-disconnect-grace", []model.Part{
		{Type: "text", Text: "继续任务"},
	}, nil, operator.ID)
	mem.SetStatus(task.ID, model.TaskRunning)

	if !application.broker.Remove(device) {
		t.Fatalf("expected broker remove to succeed")
	}
	application.scheduleAgentDisconnectFailure(operator.ID, "agent-disconnect-grace")
	application.broker.Add(nil, model.HelloPayload{
		AgentID:   "agent-disconnect-grace",
		MachineID: "machine-disconnect-grace",
		Kind:      "opencode",
	}, operator.ID)

	time.Sleep(60 * time.Millisecond)
	stored, ok := mem.GetTask(task.ID)
	if !ok || stored.Status != model.TaskRunning {
		t.Fatalf("expected task to keep running after reconnect, got ok=%v task=%+v", ok, stored)
	}
}

func TestDisconnectGraceFailsTaskWhenAgentStaysOffline(t *testing.T) {
	operator := model.Operator{ID: 1, OperatorKey: "operator-key-disconnect-offline"}
	mem := store.NewMemory(relayTestArchive{
		operators: map[string]model.Operator{operator.OperatorKey: operator},
	})
	application := &App{
		store:               mem,
		broker:              broker.New(),
		auth:                auth.NewManager(time.Hour),
		taskDisconnectGrace: 10 * time.Millisecond,
	}
	device := application.broker.Add(nil, model.HelloPayload{
		AgentID:   "agent-disconnect-offline",
		MachineID: "machine-disconnect-offline",
		Kind:      "opencode",
	}, operator.ID)
	task := mem.CreateTask("agent-disconnect-offline", "machine-disconnect-offline", "chat-codex", "/tmp/chat-codex", "session-disconnect-offline", []model.Part{
		{Type: "text", Text: "继续任务"},
	}, nil, operator.ID)
	mem.SetStatus(task.ID, model.TaskRunning)

	if !application.broker.Remove(device) {
		t.Fatalf("expected broker remove to succeed")
	}
	application.scheduleAgentDisconnectFailure(operator.ID, "agent-disconnect-offline")

	time.Sleep(50 * time.Millisecond)
	stored, ok := mem.GetTask(task.ID)
	if !ok || stored.Status != model.TaskFailed || stored.Error != "设备断开连接" {
		t.Fatalf("expected task to fail after disconnect grace, got ok=%v task=%+v", ok, stored)
	}
}

func TestHeartbeatClearsFinishedRunningTaskFromBroker(t *testing.T) {
	operator := model.Operator{
		ID:          1,
		OperatorUID: "operator-finished-heartbeat",
		Username:    "relay-finished-tester",
		OperatorKey: "operator-key-finished",
	}
	mem := store.NewMemory(relayTestArchive{
		operators: map[string]model.Operator{
			operator.OperatorKey: operator,
		},
	})
	application := &App{
		store:            mem,
		broker:           broker.New(),
		auth:             auth.NewManager(time.Hour),
		taskCleanupTick:  time.Hour,
		taskTimeout:      time.Hour,
		taskCancelGrace:  time.Hour,
		artifactSubs:     map[string]chan artifactStreamEvent{},
		aiConfigSubs:     map[string]chan model.DeviceAIConfigResultPayload{},
		goalOptimizeSubs: map[string]chan model.GoalOptimizeResultPayload{},
		mcpConfigSubs:    map[string]chan model.DeviceMCPConfigResultPayload{},
		launcherSubs:     map[string]chan model.DeviceLauncherResultPayload{},
		directoriesSubs:  map[string]chan model.DeviceDirectoriesResultPayload{},
	}
	device := application.broker.Add(nil, model.HelloPayload{
		AgentID:   "agent-finished",
		MachineID: "machine-finished",
		Kind:      "opencode",
		Hostname:  "finished-host",
		Version:   "test",
	}, operator.ID)
	task := mem.CreateTask("agent-finished", "machine-finished", "chat-codex", "/tmp/chat-codex", "session-finished", []model.Part{
		{Type: "text", Text: "你好"},
	}, nil, operator.ID)
	mem.Complete(task.ID, "session-finished", "done", nil)

	if err := application.handle(device, mustEnvelopeBytes(t, model.Envelope{
		Type:      "device.heartbeat",
		RequestID: "heartbeat-finished",
		SentAt:    time.Now().UTC().Format(time.RFC3339),
		Payload: model.HeartbeatPayload{
			RunningTaskID: task.ID,
		},
	})); err != nil {
		t.Fatalf("handle heartbeat: %v", err)
	}
	got, ok := application.broker.Get("agent-finished", operator.ID)
	if !ok {
		t.Fatalf("agent missing")
	}
	if got.CurrentTask != "" {
		t.Fatalf("expected finished task to be ignored by heartbeat, got %+v", got)
	}
}

func TestCleanupReconcilesTerminalEventBeforeCancelling(t *testing.T) {
	operator := model.Operator{
		ID:          1,
		OperatorUID: "operator-terminal-reconcile",
		Username:    "relay-terminal-reconcile",
		OperatorKey: "operator-key-terminal-reconcile",
	}
	mem := store.NewMemory(relayTestArchive{
		operators: map[string]model.Operator{
			operator.OperatorKey: operator,
		},
	})
	application := &App{
		store:            mem,
		broker:           broker.New(),
		auth:             auth.NewManager(time.Hour),
		taskCleanupTick:  time.Hour,
		taskTimeout:      20 * time.Millisecond,
		taskCancelGrace:  time.Hour,
		artifactSubs:     map[string]chan artifactStreamEvent{},
		aiConfigSubs:     map[string]chan model.DeviceAIConfigResultPayload{},
		goalOptimizeSubs: map[string]chan model.GoalOptimizeResultPayload{},
		mcpConfigSubs:    map[string]chan model.DeviceMCPConfigResultPayload{},
		launcherSubs:     map[string]chan model.DeviceLauncherResultPayload{},
		directoriesSubs:  map[string]chan model.DeviceDirectoriesResultPayload{},
	}
	application.broker.Add(nil, model.HelloPayload{
		AgentID:   "agent-terminal-reconcile",
		MachineID: "machine-terminal-reconcile",
		Kind:      "opencode",
		Hostname:  "terminal-host",
		Version:   "test",
	}, operator.ID)
	task := mem.CreateTask(
		"agent-terminal-reconcile",
		"machine-terminal-reconcile",
		"chat-codex",
		"/tmp/chat-codex",
		"session-terminal-reconcile",
		[]model.Part{{Type: "text", Text: "你好"}},
		nil,
		operator.ID,
	)
	mem.SetStatus(task.ID, model.TaskRunning)
	mem.AddEvent(task.ID, model.Event{
		TaskID:    task.ID,
		Type:      "completed",
		Content:   "terminal result",
		SessionID: "session-terminal-reconcile",
		SentAt:    time.Now().UTC().Add(-time.Minute),
	})

	stale, ok := mem.GetTask(task.ID)
	if !ok {
		t.Fatalf("task missing")
	}
	if stale.Status != model.TaskRunning {
		t.Fatalf("expected test fixture to stay running before reconciliation, got %+v", stale)
	}
	application.requestTaskCancel(stale, "任务超时，正在终止执行（20ms 无响应）")

	reconciled, ok := mem.GetTask(task.ID)
	if !ok {
		t.Fatalf("task missing after reconciliation")
	}
	if reconciled.Status != model.TaskCompleted || reconciled.Result != "terminal result" {
		t.Fatalf("expected terminal event to reconcile task as completed, got %+v", reconciled)
	}
	if events := mem.Events(task.ID); len(events) != 1 {
		t.Fatalf("expected no cancelling event to be appended, got %+v", events)
	}
}

func TestGoalHeartbeatRefreshesActiveGoalLease(t *testing.T) {
	operator := model.Operator{
		ID:          1,
		OperatorUID: "operator-goal-heartbeat-stale",
		Username:    "relay-goal-heartbeat-stale",
		OperatorKey: "operator-key-goal-heartbeat-stale",
	}
	mem := store.NewMemory(relayTestArchive{
		operators: map[string]model.Operator{
			operator.OperatorKey: operator,
		},
	})
	application := &App{
		store:                mem,
		broker:               broker.New(),
		auth:                 auth.NewManager(time.Hour),
		taskCleanupTick:      time.Hour,
		taskTimeout:          20 * time.Millisecond,
		goalHeartbeatTimeout: time.Second,
		taskCancelGrace:      time.Hour,
		artifactSubs:         map[string]chan artifactStreamEvent{},
		aiConfigSubs:         map[string]chan model.DeviceAIConfigResultPayload{},
		goalOptimizeSubs:     map[string]chan model.GoalOptimizeResultPayload{},
		mcpConfigSubs:        map[string]chan model.DeviceMCPConfigResultPayload{},
		launcherSubs:         map[string]chan model.DeviceLauncherResultPayload{},
		directoriesSubs:      map[string]chan model.DeviceDirectoriesResultPayload{},
	}
	device := application.broker.Add(nil, model.HelloPayload{
		AgentID:   "agent-goal-heartbeat-stale",
		MachineID: "machine-goal-heartbeat-stale",
		Kind:      "opencode",
		Hostname:  "goal-heartbeat-host",
		Version:   "test",
	}, operator.ID)
	task := mem.CreateTask(
		"agent-goal-heartbeat-stale",
		"machine-goal-heartbeat-stale",
		"chat-codex",
		"/tmp/chat-codex",
		"session-goal-heartbeat-stale",
		[]model.Part{{Type: "text", Text: "继续 goal"}},
		nil,
		operator.ID,
	)
	mem.SetStatus(task.ID, model.TaskRunning)
	mem.AddEvent(task.ID, model.Event{
		TaskID:    task.ID,
		Type:      "started",
		SessionID: "session-goal-heartbeat-stale",
		SentAt:    time.Now().UTC().Add(-time.Minute),
	})

	if err := application.handle(device, mustEnvelopeBytes(t, model.Envelope{
		Type:      "task.goal_heartbeat",
		RequestID: "goal-heartbeat-stale",
		SentAt:    time.Now().UTC().Format(time.RFC3339),
		Payload: model.GoalEventPayload{
			TaskID:    task.ID,
			SessionID: "session-goal-heartbeat-stale",
			GoalID:    "goal-heartbeat-stale",
			Objective: "验证 goal 心跳续租活动任务",
			Status:    "active",
			Iteration: 1,
			Max:       30,
		},
	})); err != nil {
		t.Fatalf("handle goal heartbeat: %v", err)
	}

	stored, ok := mem.GetTask(task.ID)
	if !ok {
		t.Fatalf("task missing")
	}
	if stored.Metadata["goal_last_heartbeat_at"] == "" {
		t.Fatalf("expected goal heartbeat metadata to be recorded, got %+v", stored.Metadata)
	}
	lastActivity, timeout := application.taskActivityLease(stored)
	cutoff := time.Now().UTC().Add(-timeout)
	if lastActivity.Before(cutoff) {
		t.Fatalf("goal heartbeat must refresh active goal lease: activity=%s cutoff=%s", lastActivity, cutoff)
	}

	mem.UpdateTaskMetadata(task.ID, map[string]string{"goal_status": "paused"})
	paused, ok := mem.GetTask(task.ID)
	if !ok {
		t.Fatalf("paused task missing")
	}
	pausedActivity, pausedTimeout := application.taskActivityLease(paused)
	if pausedTimeout != application.taskTimeout {
		t.Fatalf("paused goal must use the normal task timeout, got %s", pausedTimeout)
	}
	if !pausedActivity.Before(time.Now().UTC().Add(-pausedTimeout)) {
		t.Fatalf("paused goal heartbeat must not keep the task leased: activity=%s timeout=%s", pausedActivity, pausedTimeout)
	}
}

func writeEnvelope(t *testing.T, ctx context.Context, conn *websocket.Conn, env model.Envelope) {
	t.Helper()
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, raw); err != nil {
		t.Fatalf("write websocket envelope %s: %v", env.Type, err)
	}
}

func mustEnvelopeBytes(t *testing.T, env model.Envelope) []byte {
	t.Helper()
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	return raw
}

func readEnvelope(t *testing.T, ctx context.Context, conn *websocket.Conn) model.Envelope {
	t.Helper()
	_, raw, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read websocket envelope: %v", err)
	}
	var env model.Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatalf("decode websocket envelope %s: %v", string(raw), err)
	}
	return env
}

func decodePayload(t *testing.T, payload any, out any) {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
}

func getJSON[T any](t *testing.T, url, token string) T {
	t.Helper()
	body := getRaw(t, url, token)
	var out T
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatalf("decode response from %s: %v body=%s", url, err, body)
	}
	return out
}

func postJSON[T any](ctx context.Context, url, token string, body any) (T, error) {
	return postJSONWithStatus[T](ctx, url, token, body, http.StatusAccepted)
}

func postJSONWithStatus[T any](ctx context.Context, url, token string, body any, expectedStatus int) (T, error) {
	var zero T
	raw, err := json.Marshal(body)
	if err != nil {
		return zero, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return zero, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return zero, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return zero, err
	}
	if resp.StatusCode != expectedStatus {
		return zero, &unexpectedStatusError{status: resp.Status, body: string(respBody)}
	}
	var out T
	if err := json.Unmarshal(respBody, &out); err != nil {
		return zero, err
	}
	return out, nil
}

func getRaw(t *testing.T, url, token string) string {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("create get request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response from %s: %v", url, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		t.Fatalf("unexpected get status %s body=%s", resp.Status, string(body))
	}
	return string(body)
}

func waitForTaskStatus(t *testing.T, ctx context.Context, baseURL, token, taskID string, status model.TaskStatus) model.Task {
	t.Helper()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()

	for {
		task := getJSON[model.Task](t, baseURL+"/api/tasks/"+taskID, token)
		if task.Status == status {
			return task
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait for task %s status %s timed out, last status=%s", taskID, status, task.Status)
		case <-ticker.C:
		}
	}
}

type unexpectedStatusError struct {
	status string
	body   string
}

func (e *unexpectedStatusError) Error() string {
	return "unexpected status " + e.status + ": " + e.body
}
