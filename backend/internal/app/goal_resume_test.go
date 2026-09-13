package app

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"relay-server/internal/api"
	"relay-server/internal/auth"
	"relay-server/internal/broker"
	"relay-server/internal/model"
	"relay-server/internal/store"
)

func TestBackendRelayRouterRestoresActiveGoalAfterCapableRelayRestart(t *testing.T) {
	operator := model.Operator{ID: 7, OperatorKey: "resume-operator-key"}
	mem := store.NewMemory(relayTestArchive{operators: map[string]model.Operator{operator.OperatorKey: operator}})
	application := &App{
		store:           mem,
		broker:          broker.New(),
		auth:            auth.NewManager(time.Hour),
		taskCleanupTick: time.Hour,
		taskTimeout:     time.Hour,
		taskCancelGrace: time.Second,
		artifactSubs:    map[string]chan artifactStreamEvent{},
		aiConfigSubs:    map[string]chan model.DeviceAIConfigResultPayload{},
		mcpConfigSubs:   map[string]chan model.DeviceMCPConfigResultPayload{},
		launcherSubs:    map[string]chan model.DeviceLauncherResultPayload{},
		directoriesSubs: map[string]chan model.DeviceDirectoriesResultPayload{},
	}
	task := mem.CreateTask(
		"agent-resume",
		"machine-resume",
		"project-resume",
		"/tmp/project-resume",
		"ses_resume",
		[]model.Part{{Type: "text", Text: "原始目标输入"}},
		map[string]string{
			"goal":                "完成恢复目标",
			"goal_id":             "goal-resume",
			"goal_status":         "active",
			"goal_iteration":      "2",
			"goal_max_iterations": "5",
			"goal_updated_at":     time.Now().UTC().Format(time.RFC3339Nano),
		},
		operator.ID,
	)
	mem.SetStatus(task.ID, model.TaskRunning)

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
		RequestID: "resume-hello",
		SentAt:    time.Now().UTC().Format(time.RFC3339),
		Payload: model.HelloPayload{
			AgentID:      "agent-resume",
			MachineID:    "machine-resume",
			OperatorKey:  operator.OperatorKey,
			Hostname:     "resume-host",
			Version:      "1.15.60",
			Capabilities: []string{"task_resume_v1"},
			Projects:     []model.HelloProject{{ProjectID: "project-resume", Root: "/tmp/project-resume"}},
		},
	})
	if welcome := readEnvelope(t, ctx, conn); welcome.Type != "device.welcome" {
		t.Fatalf("expected device.welcome, got %s", welcome.Type)
	}
	run := readEnvelope(t, ctx, conn)
	if run.Type != "task.run" {
		t.Fatalf("expected task.run resume, got %s", run.Type)
	}
	var payload model.RunPayload
	decodePayload(t, run.Payload, &payload)
	if !payload.Resume || payload.TaskID != task.ID || payload.SessionID != "ses_resume" {
		t.Fatalf("unexpected resume payload: %+v", payload)
	}
	if payload.Metadata["goal_iteration"] != "2" || payload.Metadata["goal_resume"] != "true" {
		t.Fatalf("resume metadata lost goal state: %+v", payload.Metadata)
	}
	if len(payload.Parts) != 1 || !strings.Contains(payload.Parts[0].Text, "继续当前活动目标") {
		t.Fatalf("unexpected resume parts: %+v", payload.Parts)
	}

	if got := application.resumableGoalTask(operator.ID, "agent-resume", model.HelloPayload{
		MachineID:     "machine-resume",
		RunningTaskID: task.ID,
		Capabilities:  []string{"task_resume_v1"},
		Projects:      []model.HelloProject{{ProjectID: "project-resume"}},
	}); got != nil {
		t.Fatalf("an active task already reported by the relay must not be resumed twice: %+v", got)
	}

	subagentTask := mem.CreateTask(
		"agent-resume", "machine-resume", "project-resume", "/tmp/project-resume", "ses_subagent_resume",
		[]model.Part{{Type: "text", Text: "恢复子代理任务"}}, map[string]string{}, operator.ID,
	)
	mem.SetStatus(subagentTask.ID, model.TaskRunning)
	mem.AddEvent(subagentTask.ID, model.Event{
		TaskID: subagentTask.ID, Type: "subagent_state", SessionID: subagentTask.SessionID, SentAt: time.Now().UTC(),
		Metadata: map[string]any{
			"node_id": "inspect", "plan_id": "plan_resume", "child_session_id": "ses_child", "subagent_type": "repo-explorer",
			"prompt": "inspect repository", "state": "running", "attempt": 1,
		},
	})
	application.resumeGoalTaskAfterReconnect(operator.ID, "agent-resume", model.HelloPayload{
		MachineID: "machine-resume", Capabilities: []string{"task_resume_v1"},
		Projects: []model.HelloProject{{ProjectID: "project-resume"}},
	})
	resumedSubagent := readEnvelope(t, ctx, conn)
	var subagentPayload model.RunPayload
	decodePayload(t, resumedSubagent.Payload, &subagentPayload)
	if resumedSubagent.Type != "task.run" || subagentPayload.TaskID != subagentTask.ID || len(subagentPayload.Subagents) != 1 {
		t.Fatalf("expected subagent graph resume, envelope=%+v payload=%+v", resumedSubagent, subagentPayload)
	}
	if strings.Contains(subagentPayload.Parts[0].Text, "活动目标") {
		t.Fatalf("subagent task must use generic resume prompt: %+v", subagentPayload.Parts)
	}

	detachedTask := mem.CreateTask(
		"agent-resume", "machine-resume", "project-resume", "/tmp/project-resume", "ses_detached_parent",
		[]model.Part{{Type: "text", Text: "父任务已经异常结束"}}, nil, operator.ID,
	)
	mem.Fail(detachedTask.ID, detachedTask.SessionID, "parent failure")
	mem.AddEvent(detachedTask.ID, model.Event{TaskID: detachedTask.ID, Type: "subagent_state", SessionID: detachedTask.SessionID, SentAt: time.Now().UTC(), Metadata: map[string]any{
		"node_id": "detached-node", "plan_id": "plan-detached", "child_session_id": "ses_detached_child", "subagent_type": "repo-explorer",
		"prompt": "continue inspection", "state": "recovering", "attempt": 2,
	}})
	application.resumeDetachedSubagentsAfterReconnect(operator.ID, "agent-resume", model.HelloPayload{
		MachineID: "machine-resume", Capabilities: []string{"task_resume_v1"},
		Projects: []model.HelloProject{{ProjectID: "project-resume"}},
	})
	recovery := readEnvelope(t, ctx, conn)
	if recovery.Type != "task.subagent_recover" {
		t.Fatalf("expected detached subagent recovery envelope, got %s", recovery.Type)
	}
	var recoveryPayload model.RunPayload
	decodePayload(t, recovery.Payload, &recoveryPayload)
	if recoveryPayload.TaskID != detachedTask.ID || recoveryPayload.SessionID != detachedTask.SessionID || len(recoveryPayload.Subagents) != 1 || recoveryPayload.Subagents[0].ChildSessionID != "ses_detached_child" {
		t.Fatalf("unexpected detached recovery payload: %+v", recoveryPayload)
	}
}

func TestResumeDetachedSubagentsUsesTaskMetadataWhenEventsAreGone(t *testing.T) {
	operator := model.Operator{ID: 9, OperatorKey: "metadata-operator-key"}
	mem := store.NewMemory(relayTestArchive{operators: map[string]model.Operator{operator.OperatorKey: operator}})
	application := &App{
		store:           mem,
		broker:          broker.New(),
		auth:            auth.NewManager(time.Hour),
		taskCleanupTick: time.Hour,
		taskTimeout:     time.Hour,
		taskCancelGrace: time.Second,
		artifactSubs:    map[string]chan artifactStreamEvent{},
		aiConfigSubs:    map[string]chan model.DeviceAIConfigResultPayload{},
		mcpConfigSubs:   map[string]chan model.DeviceMCPConfigResultPayload{},
		launcherSubs:    map[string]chan model.DeviceLauncherResultPayload{},
		directoriesSubs: map[string]chan model.DeviceDirectoriesResultPayload{},
	}
	task := mem.CreateTask(
		"agent-metadata", "machine-metadata", "project-metadata", "/tmp/project-metadata", "ses_metadata_parent",
		[]model.Part{{Type: "text", Text: "父任务结束后只留下 metadata"}}, nil, operator.ID,
	)
	mem.Fail(task.ID, task.SessionID, "parent failure")
	rawNodes, err := json.Marshal([]model.SubagentNodeSnapshot{{
		NodeID:         "metadata-node",
		PlanID:         "plan-metadata",
		ChildSessionID: "ses_metadata_child",
		Prompt:         "continue from metadata",
		State:          "running",
	}})
	if err != nil {
		t.Fatal(err)
	}
	mem.UpdateTaskMetadata(task.ID, map[string]string{api.SubagentNodesMetadataKey: string(rawNodes)})

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
		RequestID: "metadata-hello",
		SentAt:    time.Now().UTC().Format(time.RFC3339),
		Payload: model.HelloPayload{
			AgentID:      "agent-metadata",
			MachineID:    "machine-metadata",
			OperatorKey:  operator.OperatorKey,
			Hostname:     "metadata-host",
			Version:      "1.15.60",
			Capabilities: []string{"task_resume_v1"},
			Projects:     []model.HelloProject{{ProjectID: "project-metadata", Root: "/tmp/project-metadata"}},
		},
	})
	if welcome := readEnvelope(t, ctx, conn); welcome.Type != "device.welcome" {
		t.Fatalf("expected device.welcome, got %s", welcome.Type)
	}
	recovery := readEnvelope(t, ctx, conn)
	if recovery.Type != "task.subagent_recover" {
		t.Fatalf("expected metadata-backed subagent recovery, got %s", recovery.Type)
	}
	var payload model.RunPayload
	decodePayload(t, recovery.Payload, &payload)
	if payload.TaskID != task.ID || len(payload.Subagents) != 1 || payload.Subagents[0].NodeID != "metadata-node" {
		t.Fatalf("unexpected metadata recovery payload: %+v", payload)
	}
}
