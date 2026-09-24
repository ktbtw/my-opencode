package app

import (
	"context"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"relay-server/internal/auth"
	"relay-server/internal/broker"
	"relay-server/internal/model"
	"relay-server/internal/store"
)

func cancelTestApp(t *testing.T, tick, grace time.Duration) (*App, model.Operator, *store.Memory, *httptest.Server, string) {
	t.Helper()
	operator := model.Operator{
		ID:          7,
		OperatorUID: "operator-cancel-race",
		Username:    "cancel-race",
		OperatorKey: "operator-key-cancel-race",
	}
	mem := store.NewMemory(relayTestArchive{
		operators: map[string]model.Operator{operator.OperatorKey: operator},
	})
	authManager := auth.NewManager(time.Hour)
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	application := &App{
		store:           mem,
		broker:          broker.New(),
		auth:            authManager,
		taskCleanupTick: tick,
		taskTimeout:     time.Hour,
		taskCancelGrace: grace,
		artifactSubs:    map[string]chan artifactStreamEvent{},
		aiConfigSubs:    map[string]chan model.DeviceAIConfigResultPayload{},
		mcpConfigSubs:   map[string]chan model.DeviceMCPConfigResultPayload{},
		launcherSubs:    map[string]chan model.DeviceLauncherResultPayload{},
		directoriesSubs: map[string]chan model.DeviceDirectoriesResultPayload{},
	}
	server := httptest.NewServer(application.Router())
	t.Cleanup(server.Close)
	return application, operator, mem, server, token
}

func cancelTestHello(agentID string, operator model.Operator) model.HelloPayload {
	return model.HelloPayload{
		AgentID:     agentID,
		MachineID:   "machine-" + agentID,
		Kind:        "opencode",
		OperatorKey: operator.OperatorKey,
		Hostname:    "cancel-race-host",
		Version:     "test",
		Projects:    []model.HelloProject{{ProjectID: "chat-codex", Root: "/tmp/chat-codex"}},
	}
}

func cancelTestTask(mem *store.Memory, operator model.Operator, agentID string) *model.Task {
	task := mem.CreateTask(agentID, "machine-"+agentID, "chat-codex", "/tmp/chat-codex", "session-"+agentID,
		[]model.Part{{Type: "text", Text: "你好"}}, nil, operator.ID)
	mem.SetStatus(task.ID, model.TaskRunning)
	mem.AddEvent(task.ID, model.Event{TaskID: task.ID, Type: "started", SessionID: "session-" + agentID, SentAt: time.Now().UTC()})
	return task
}

func cancelTestDevice(t *testing.T, serverURL string, operator model.Operator, agentID string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	wsURL := "ws" + strings.TrimPrefix(serverURL, "http") + "/ws/device"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial relay websocket: %v", err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	writeEnvelope(t, ctx, conn, model.Envelope{
		Type:    "device.hello",
		SentAt:  time.Now().UTC().Format(time.RFC3339),
		Payload: cancelTestHello(agentID, operator),
	})
	if welcome := readEnvelope(t, ctx, conn); welcome.Type != "device.welcome" {
		t.Fatalf("expected device.welcome, got %s", welcome.Type)
	}
}

func countEvents(mem *store.Memory, taskID, eventType string) int {
	count := 0
	for _, event := range mem.Events(taskID) {
		if event.Type == eventType {
			count++
		}
	}
	return count
}

// 并发/重复取消必须幂等：只产生一条 cancelling 事件，状态停留在 cancelling。
func TestCancelIsIdempotentUnderConcurrency(t *testing.T) {
	_, operator, mem, server, token := cancelTestApp(t, time.Hour, time.Hour)
	agentID := "agent-idempotent"
	cancelTestDevice(t, server.URL, operator, agentID)
	task := cancelTestTask(mem, operator, agentID)

	const requests = 32
	var wg sync.WaitGroup
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = postJSONWithStatus[model.Task](context.Background(),
				server.URL+"/api/tasks/"+task.ID+"/cancel", token, map[string]any{}, 200)
		}()
	}
	wg.Wait()

	final, _ := mem.GetTask(task.ID)
	if final.Status != model.TaskCancelling {
		t.Fatalf("expected cancelling, got %s", final.Status)
	}
	if got := countEvents(mem, task.ID, "cancelling"); got != 1 {
		t.Fatalf("expected exactly one cancelling event, got %d", got)
	}
}

// 取消之后设备才上报完成：完成结果是权威终态，必须保留，不能被当成失败丢掉。
func TestCompletedDuringCancellingKeepsResult(t *testing.T) {
	application, operator, mem, server, token := cancelTestApp(t, time.Hour, time.Hour)
	agentID := "agent-late-completed"
	cancelTestDevice(t, server.URL, operator, agentID)
	task := cancelTestTask(mem, operator, agentID)

	if _, err := postJSONWithStatus[model.Task](context.Background(),
		server.URL+"/api/tasks/"+task.ID+"/cancel", token, map[string]any{}, 200); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if current, _ := mem.GetTask(task.ID); current.Status != model.TaskCancelling {
		t.Fatalf("expected cancelling before completion, got %s", current.Status)
	}

	// Inject the device event without replacing the live websocket registration.
	device := &broker.Device{ID: agentID}
	if err := application.handle(device, mustEnvelopeBytes(t, model.Envelope{
		Type:   "task.completed",
		SentAt: time.Now().UTC().Format(time.RFC3339),
		Payload: model.CompletedPayload{
			TaskID: task.ID, SessionID: "session-" + agentID, Result: "设备已完成的真实结果",
		},
	})); err != nil {
		t.Fatalf("handle completed: %v", err)
	}

	final, _ := mem.GetTask(task.ID)
	if final.Status != model.TaskCompleted {
		t.Fatalf("expected completed to win over cancel, got %s", final.Status)
	}
	if final.Result != "设备已完成的真实结果" {
		t.Fatalf("expected real result preserved, got %q", final.Result)
	}
}

// 子代理事件持续刷新 UpdatedAt 时，取消 grace 仍应按「取消请求时刻」到期并收尾。
func TestCancelGraceIgnoresSubagentEventRefresh(t *testing.T) {
	application, operator, mem, server, token := cancelTestApp(t, 15*time.Millisecond, 80*time.Millisecond)
	go application.cleanStuckTasks()

	agentID := "agent-subagent-churn"
	cancelTestDevice(t, server.URL, operator, agentID)
	task := cancelTestTask(mem, operator, agentID)
	if _, err := postJSONWithStatus[model.Task](context.Background(),
		server.URL+"/api/tasks/"+task.ID+"/cancel", token, map[string]any{}, 200); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	device := &broker.Device{ID: agentID}
	subagentEvent := mustEnvelopeBytes(t, model.Envelope{
		Type:   "task.subagent_state",
		SentAt: time.Now().UTC().Format(time.RFC3339),
		Payload: model.SubagentStatePayload{
			TaskID: task.ID, SessionID: "session-" + agentID,
			NodeID: "node-1", State: "running", Title: "子代理心跳",
		},
	})
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				_ = application.handle(device, subagentEvent)
			}
		}
	}()
	time.Sleep(500 * time.Millisecond)
	close(stop)

	final, _ := mem.GetTask(task.ID)
	if final.Status != model.TaskFailed {
		t.Fatalf("expected cancel grace to finalize the task as failed despite subagent events, got %s", final.Status)
	}
	if _, ok := mem.LatestTerminalEvent(task.ID); !ok {
		t.Fatalf("expected a terminal event to be appended on finalize")
	}
}
