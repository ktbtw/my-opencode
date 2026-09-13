package notify

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"relay-server/internal/model"
	"relay-server/internal/push"
	"relay-server/internal/store"
)

type capturePushSender struct {
	requests []push.SendRequest
}

type captureNotificationMailer struct {
	email   string
	subject string
	body    string
	calls   int
}

func (m *captureNotificationMailer) SendNotification(email, subject, body string) error {
	m.email = email
	m.subject = subject
	m.body = body
	m.calls++
	return nil
}

func (s *capturePushSender) SendAndroid(ctx context.Context, req push.SendRequest) error {
	_ = ctx
	s.requests = append(s.requests, req)
	return nil
}

func TestNotifyTaskCompletedIncludesAgentAndTaskSummary(t *testing.T) {
	memory := store.NewMemory(nil)
	sender := &capturePushSender{}
	pushService := push.NewService(memory, sender)
	service := NewService(memory, pushService, nil, "码控")

	_, err := memory.UpsertPushDevice(model.PushDevice{
		OperatorID:     7,
		Platform:       "android",
		Vendor:         "jpush",
		RegistrationID: "rid-test",
		DeviceID:       "device-test",
	})
	if err != nil {
		t.Fatalf("register push device: %v", err)
	}

	task := &model.Task{
		ID:          "task_1",
		AgentID:     "agent_chat_codex",
		MachineID:   "machine_1",
		ProjectID:   "project_1",
		ProjectRoot: "/work/project_1",
		SessionID:   "sess_1",
		OperatorID:  7,
		Metadata: map[string]string{
			"agent_name": "chat-codex",
		},
		Parts: []model.Part{
			{Type: "text", Text: "帮我修复 todowrite 工具调用失败，并且确认工具状态显示正常"},
		},
	}
	service.PersistTaskCompleted(context.Background(), task)
	service.DeliverTaskCompleted(context.Background(), task)

	if len(sender.requests) != 0 {
		t.Fatalf("task completion must not use JPush, got %d requests", len(sender.requests))
	}
	page, err := memory.ListAppNotificationChanges(context.Background(), 7, 0, 20)
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("expected one synced app notification, got %d, err=%v", len(page.Items), err)
	}
	var record map[string]any
	if err := json.Unmarshal(page.Items[0].Record, &record); err != nil {
		t.Fatalf("decode app notification: %v", err)
	}
	if record["operation_id"] != "chat-task:task_1" || record["status"] != "succeeded" {
		t.Fatalf("unexpected app notification identity: %+v", record)
	}
	metadata, ok := record["metadata"].(map[string]any)
	if !ok || metadata["machine_id"] != "machine_1" || metadata["agent_id"] != "agent_chat_codex" ||
		metadata["project_id"] != "project_1" || metadata["project_root"] != "/work/project_1" {
		t.Fatalf("unexpected app notification metadata: %+v", record["metadata"])
	}
	actions, ok := record["actions"].([]any)
	if !ok || len(actions) != 1 {
		t.Fatalf("expected one app notification action: %+v", record["actions"])
	}
	action := actions[0].(map[string]any)
	payload := action["payload"].(map[string]any)
	route := payload["route"].(string)
	if action["type"] != "openRoute" ||
		!strings.Contains(route, "machineId=machine_1") ||
		!strings.Contains(route, "agentId=agent_chat_codex") ||
		!strings.Contains(route, "projectId=project_1") ||
		!strings.Contains(route, "projectRoot=%2Fwork%2Fproject_1") {
		t.Fatalf("unexpected app notification action: %+v", action)
	}
}

func TestTaskInputSummaryPrefersGoalAndCompactsText(t *testing.T) {
	summary := taskInputSummary(&model.Task{
		Metadata: map[string]string{
			"goal_objective": "  完成  这个目标\n并持续运行  ",
		},
		Parts: []model.Part{
			{Type: "text", Text: "普通对话内容"},
		},
	})
	if summary != "完成 这个目标 并持续运行" {
		t.Fatalf("unexpected summary: %q", summary)
	}
}

func TestNotifyRuntimeUserActionHonorsEmailSetting(t *testing.T) {
	memory := store.NewMemory(nil)
	mailer := &captureNotificationMailer{}
	service := &Service{
		store:  memory,
		mailer: mailer,
		operatorEmail: func(operatorID int64) (string, error) {
			return "owner@example.com", nil
		},
		brandName: "码控",
	}
	job := model.RuntimeInstallJob{ID: "preflight_1", OperatorID: 7, MachineID: "m_windows"}
	action := model.RuntimeUserAction{
		ID:           "ida-uac:preflight_1",
		Title:        "IDA 安装需要管理员确认",
		Message:      "请确认 UAC。",
		Instructions: []string{"点击“是”"},
	}

	service.NotifyRuntimeUserAction(context.Background(), job, action)
	if mailer.calls != 0 {
		t.Fatalf("expected disabled email setting to suppress mail, calls=%d", mailer.calls)
	}
	if err := memory.SetDeviceSetting(7, userSettingsAgentID, emailNotificationEnabledKey, "true"); err != nil {
		t.Fatalf("enable email notification: %v", err)
	}
	service.NotifyRuntimeUserAction(context.Background(), job, action)
	if mailer.calls != 1 {
		t.Fatalf("expected one email, calls=%d", mailer.calls)
	}
	if mailer.email != "owner@example.com" || mailer.subject != "[码控] IDA 安装需要管理员确认" {
		t.Fatalf("unexpected email envelope: email=%q subject=%q", mailer.email, mailer.subject)
	}
	for _, expected := range []string{"m_windows", "preflight_1", "请确认 UAC。", "点击“是”"} {
		if !strings.Contains(mailer.body, expected) {
			t.Fatalf("email body missing %q: %s", expected, mailer.body)
		}
	}
}
