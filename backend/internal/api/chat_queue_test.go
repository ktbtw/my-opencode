package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"relay-server/internal/broker"
	"relay-server/internal/model"
	"relay-server/internal/store"
)

func TestDispatchNextChatQueueItemAddsProjectMemoryMetadataToNilMap(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mem := store.NewMemory(nil)
	b := broker.New()
	api := New(mem, b, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, true)
	const operatorID int64 = 17
	const agentID = "agent-project-memory"
	const machineID = "machine-project-memory"
	const projectID = "project-memory"
	const sessionID = "session-project-memory"

	completed := mem.CreateTask(agentID, machineID, projectID, "/tmp/project-memory", sessionID,
		[]model.Part{{Type: "text", Text: "completed"}}, nil, operatorID)
	mem.SetStatus(completed.ID, model.TaskCompleted)
	if err := mem.UpsertProjectScope(ctx, model.ProjectScope{
		ID: "scope-project-memory", OperatorID: operatorID, MachineID: machineID,
		Status: model.ProjectScopeActive, Revision: 7,
	}); err != nil {
		t.Fatal(err)
	}
	if err := mem.UpsertProjectAgentBinding(ctx, model.ProjectAgentBinding{
		OperatorID: operatorID, MachineID: machineID, AgentID: agentID,
		ScopeID: "scope-project-memory", BindingEpoch: 1, Active: true,
	}); err != nil {
		t.Fatal(err)
	}
	mem.CreateChatQueueItem(model.ChatQueueItem{
		OperatorID: operatorID, SessionID: sessionID, AgentID: agentID,
		MachineID: machineID, ProjectID: projectID,
		Parts: []model.Part{{Type: "text", Text: "queued prompt"}},
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}
		b.Add(conn, model.HelloPayload{
			AgentID: agentID, MachineID: machineID,
			Projects: []model.HelloProject{{ProjectID: projectID, Root: "/tmp/project-memory"}},
		}, operatorID)
	}))
	defer server.Close()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")
	for {
		if _, online := b.Get(agentID, operatorID); online {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("agent did not become online")
		case <-time.After(time.Millisecond):
		}
	}

	task, err := api.DispatchNextChatQueueItem(operatorID, sessionID)
	if err != nil || task == nil {
		t.Fatalf("dispatch queued task: task=%+v err=%v", task, err)
	}
	if task.Metadata["project_memory_scope_id"] != "scope-project-memory" ||
		task.Metadata["project_memory_revision"] != "7" {
		t.Fatalf("project memory metadata missing from queued task: %+v", task.Metadata)
	}

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read queued task envelope: %v", err)
	}
	var envelope model.Envelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatalf("decode queued task envelope: %v", err)
	}
	rawPayload, err := json.Marshal(envelope.Payload)
	if err != nil {
		t.Fatal(err)
	}
	var payload model.RunPayload
	if err := json.Unmarshal(rawPayload, &payload); err != nil {
		t.Fatalf("decode queued task payload: %v", err)
	}
	if payload.Metadata["project_memory_scope_id"] != "scope-project-memory" ||
		payload.Metadata["project_memory_revision"] != "7" {
		t.Fatalf("project memory metadata missing from queued payload: %+v", payload.Metadata)
	}
}

func activeQueueFixture(t *testing.T) (*API, *store.Memory, *model.Task, model.ChatQueueItem) {
	t.Helper()
	mem := store.NewMemory(nil)
	task := mem.CreateTask(
		"agent-1",
		"machine-1",
		"project-1",
		"/tmp/project-1",
		"session-1",
		[]model.Part{{Type: "text", Text: "initial"}},
		nil,
		17,
	)
	mem.SetStatus(task.ID, model.TaskRunning)
	item := mem.CreateChatQueueItem(model.ChatQueueItem{
		OperatorID: 17,
		SessionID:  "session-1",
		AgentID:    "agent-1",
		MachineID:  "machine-1",
		ProjectID:  "project-1",
		Parts:      []model.Part{{Type: "text", Text: "inserted prompt"}},
		Metadata: map[string]string{
			"model":   "provider/model-b",
			"variant": "high",
		},
	})
	item, _, err := mem.UpdateChatQueueItem(17, item.ID, item.Version, func(item *model.ChatQueueItem) error {
		item.Status = model.ChatQueueInserting
		item.TaskID = task.ID
		item.InjectionVersion = 88
		return nil
	})
	if err != nil {
		t.Fatalf("mark item inserting: %v", err)
	}
	return New(mem, broker.New(), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil), mem, task, item
}

func TestTaskInputAckAcceptedRemovesQueueItemAndPersistsEvent(t *testing.T) {
	api, mem, task, item := activeQueueFixture(t)

	err := api.HandleTaskInputAck(model.TaskInputAckPayload{
		TaskID: task.ID, QueueItemID: item.ID, InjectionVersion: 88, Accepted: true,
	})
	if err != nil {
		t.Fatalf("accept input acknowledgement: %v", err)
	}
	if _, found := mem.GetChatQueueItem(17, item.ID); found {
		t.Fatal("accepted insertion remained in queue")
	}
	events := mem.Events(task.ID)
	if len(events) != 1 || events[0].Type != "input_applied" || events[0].Content != "inserted prompt" {
		t.Fatalf("unexpected persisted input event: %+v", events)
	}
	metadata, ok := events[0].Metadata.(map[string]any)
	if !ok || metadata["queue_item_id"] != item.ID || metadata["injection_version"] != int64(88) {
		t.Fatalf("unexpected input event metadata: %+v", events[0].Metadata)
	}
}

func TestChatQueueInputContentKeepsTextAndAttachmentCount(t *testing.T) {
	content := chatQueueInputContent([]model.Part{
		{Type: "text", Text: "first line"},
		{Type: "file", Filename: "report.txt"},
		{Type: "text", Text: "second line"},
	})
	if content != "first line\nsecond line\n[1 个附件]" {
		t.Fatalf("unexpected input content: %q", content)
	}
}

func TestChatQueueInjectionVersionRoundTripsThroughJavaScriptNumber(t *testing.T) {
	version := newChatQueueInjectionVersion()
	if version <= 0 || int64(float64(version)) != version {
		t.Fatalf("injection version is not a JavaScript-safe integer: %d", version)
	}
}

func TestDispatchFailureRestoresQueueItemForRetry(t *testing.T) {
	mem := store.NewMemory(nil)
	api := New(mem, broker.New(), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
	item := mem.CreateChatQueueItem(model.ChatQueueItem{
		OperatorID: 17, SessionID: "session-retry", AgentID: "offline-agent",
		MachineID: "machine-1", ProjectID: "project-1",
		Parts: []model.Part{{Type: "text", Text: "retry me"}},
	})
	claimed, ok := mem.ClaimChatQueueItem(17, item.SessionID, item.ID)
	if !ok {
		t.Fatal("claim queue item")
	}
	if _, err := api.dispatchChatQueueItem(17, claimed); err == nil {
		t.Fatal("expected dispatch failure")
	}
	restored, found := mem.GetChatQueueItem(17, item.ID)
	if !found || restored.Status != model.ChatQueueQueued || restored.TaskID != "" {
		t.Fatalf("queue item was not restored: %+v", restored)
	}
	if restored.Error != "agent offline" {
		t.Fatalf("unexpected retry error: %q", restored.Error)
	}
}

func TestTaskInputAckRejectedRestoresQueueItem(t *testing.T) {
	api, mem, task, item := activeQueueFixture(t)

	err := api.HandleTaskInputAck(model.TaskInputAckPayload{
		TaskID: task.ID, QueueItemID: item.ID, InjectionVersion: 88, Accepted: false, Error: "session busy",
	})
	if err != nil {
		t.Fatalf("reject input acknowledgement: %v", err)
	}
	restored, found := mem.GetChatQueueItem(17, item.ID)
	if !found || restored.Status != model.ChatQueueQueued || restored.TaskID != "" || restored.InjectionVersion != 0 {
		t.Fatalf("rejected insertion was not restored: %+v", restored)
	}
	if restored.Error != "session busy" {
		t.Fatalf("unexpected restored error: %q", restored.Error)
	}
	if events := mem.Events(task.ID); len(events) != 0 {
		t.Fatalf("rejected insertion created task events: %+v", events)
	}
}

func TestExpiredTaskInputRestoresOnlyMatchingInsertion(t *testing.T) {
	api, mem, _, item := activeQueueFixture(t)

	api.restoreExpiredChatQueueInsertion(17, item.ID, 87)
	unchanged, _ := mem.GetChatQueueItem(17, item.ID)
	if unchanged.Status != model.ChatQueueInserting {
		t.Fatalf("stale timeout changed insertion: %+v", unchanged)
	}

	api.restoreExpiredChatQueueInsertion(17, item.ID, 88)
	restored, found := mem.GetChatQueueItem(17, item.ID)
	if !found || restored.Status != model.ChatQueueQueued || restored.TaskID != "" {
		t.Fatalf("expired insertion was not restored: %+v", restored)
	}
	if !strings.Contains(restored.Error, "超时") {
		t.Fatalf("unexpected timeout error: %q", restored.Error)
	}
}

func TestTerminalTaskRestoresPendingInsertion(t *testing.T) {
	api, mem, task, item := activeQueueFixture(t)

	api.HandleChatQueueTaskTerminal(task, "task.failed")
	restored, found := mem.GetChatQueueItem(17, item.ID)
	if !found || restored.Status != model.ChatQueueQueued || restored.TaskID != "" || restored.InjectionVersion != 0 {
		t.Fatalf("terminal task did not restore insertion: %+v", restored)
	}
	if !strings.Contains(restored.Error, "任务已结束") {
		t.Fatalf("unexpected terminal restoration error: %q", restored.Error)
	}
}

func TestTransientAgentBusyRestoresDispatchedQueueItem(t *testing.T) {
	mem := store.NewMemory(nil)
	task := mem.CreateTask(
		"agent-1",
		"machine-1",
		"project-1",
		"/tmp/project-1",
		"session-1",
		[]model.Part{{Type: "text", Text: "queued prompt"}},
		nil,
		17,
	)
	item := mem.CreateChatQueueItem(model.ChatQueueItem{
		OperatorID: 17,
		SessionID:  "session-1",
		AgentID:    "agent-1",
		MachineID:  "machine-1",
		ProjectID:  "project-1",
		Parts:      []model.Part{{Type: "text", Text: "queued prompt"}},
	})
	item, _, err := mem.UpdateChatQueueItem(17, item.ID, item.Version, func(item *model.ChatQueueItem) error {
		item.Status = model.ChatQueueDispatched
		item.TaskID = task.ID
		return nil
	})
	if err != nil {
		t.Fatalf("mark item dispatched: %v", err)
	}
	task.Error = "agent busy: previous-task"
	api := New(mem, broker.New(), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	api.HandleChatQueueTaskTerminal(task, "task.failed")

	restored, found := mem.GetChatQueueItem(17, item.ID)
	if !found || restored.Status != model.ChatQueueQueued || restored.TaskID != "" {
		t.Fatalf("agent busy removed queue item: %+v", restored)
	}
	if restored.Metadata[chatQueueAgentBusyRetryKey] != "1" {
		t.Fatalf("unexpected agent busy retry metadata: %+v", restored.Metadata)
	}
	if !strings.Contains(restored.Error, "自动重试") {
		t.Fatalf("unexpected restored queue error: %q", restored.Error)
	}
}

func TestAgentBusyRetryLimitKeepsMessageQueued(t *testing.T) {
	mem := store.NewMemory(nil)
	task := mem.CreateTask(
		"agent-1",
		"machine-1",
		"project-1",
		"/tmp/project-1",
		"session-1",
		[]model.Part{{Type: "text", Text: "queued prompt"}},
		nil,
		17,
	)
	item := mem.CreateChatQueueItem(model.ChatQueueItem{
		OperatorID: 17,
		SessionID:  "session-1",
		AgentID:    "agent-1",
		MachineID:  "machine-1",
		ProjectID:  "project-1",
		Parts:      []model.Part{{Type: "text", Text: "queued prompt"}},
		Metadata: map[string]string{
			chatQueueAgentBusyRetryKey: "5",
		},
	})
	item, _, err := mem.UpdateChatQueueItem(17, item.ID, item.Version, func(item *model.ChatQueueItem) error {
		item.Status = model.ChatQueueDispatched
		item.TaskID = task.ID
		return nil
	})
	if err != nil {
		t.Fatalf("mark item dispatched: %v", err)
	}
	task.Error = "agent busy: previous-task"
	api := New(mem, broker.New(), nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	api.HandleChatQueueTaskTerminal(task, "task.failed")

	restored, found := mem.GetChatQueueItem(17, item.ID)
	if !found || restored.Status != model.ChatQueueQueued || restored.TaskID != "" {
		t.Fatalf("retry limit removed queue item: %+v", restored)
	}
	if restored.Metadata[chatQueueAgentBusyRetryKey] != "6" || !strings.Contains(restored.Error, "已保留") {
		t.Fatalf("unexpected retry-limit state: %+v", restored)
	}
}
