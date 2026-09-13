package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net/url"
	"strings"
	"time"

	"relay-server/internal/mail"
	"relay-server/internal/model"
	"relay-server/internal/push"
	"relay-server/internal/store"
)

type notificationMailer interface {
	SendNotification(email, subject, htmlBody string) error
}

type Service struct {
	store         *store.Memory
	push          *push.Service
	mailer        notificationMailer
	operatorEmail func(int64) (string, error)
	brandName     string
}

const userSettingsAgentID = "__user__"
const emailNotificationEnabledKey = "email_notification_enabled"

func NewService(s *store.Memory, p *push.Service, m *mail.Mailer, brandName string) *Service {
	brand := strings.TrimSpace(brandName)
	if brand == "" {
		brand = "ChatCodex"
	}
	service := &Service{store: s, push: p, mailer: m, brandName: brand}
	if s != nil {
		service.operatorEmail = s.GetOperatorEmail
	}
	return service
}

func (s *Service) SendTestEmail(ctx context.Context, operatorID int64, title, body string) error {
	if s == nil || s.store == nil || s.mailer == nil {
		return fmt.Errorf("mail notify service unavailable")
	}
	_ = ctx
	email, err := s.getOperatorEmail(operatorID)
	if err != nil {
		return err
	}
	if email == "" {
		return fmt.Errorf("operator email not found")
	}
	masked := maskEmail(email)
	log.Printf("send test email start: operator=%d email=%s title=%q", operatorID, masked, title)
	if err := s.mailer.SendNotification(email, title, fmt.Sprintf("<p>%s</p>", body)); err != nil {
		log.Printf("send test email failed: operator=%d email=%s title=%q err=%v", operatorID, masked, title, err)
		return err
	}
	log.Printf("send test email succeeded: operator=%d email=%s title=%q", operatorID, masked, title)
	return nil
}

func (s *Service) NotifyTaskCompleted(ctx context.Context, task *model.Task) {
	if task == nil || task.OperatorID <= 0 {
		log.Printf("skip task completed notify: task_nil=%t operator_id=%d", task == nil, taskOperatorID(task))
		return
	}
	detail := s.taskNotificationDetail(task)
	s.writeTaskAppNotification(ctx, task, detail)
	s.deliverTaskCompleted(ctx, task, detail)
}

func (s *Service) PersistTaskCompleted(ctx context.Context, task *model.Task) {
	if task == nil || task.OperatorID <= 0 {
		return
	}
	s.writeTaskAppNotification(ctx, task, s.taskNotificationDetail(task))
}

func (s *Service) DeliverTaskCompleted(ctx context.Context, task *model.Task) {
	if task == nil || task.OperatorID <= 0 {
		return
	}
	s.deliverTaskCompleted(ctx, task, s.taskNotificationDetail(task))
}

func (s *Service) deliverTaskCompleted(_ context.Context, task *model.Task, detail taskNotificationDetail) {
	log.Printf("notify task completed start: task=%s operator=%d session=%s agent=%s", task.ID, task.OperatorID, task.SessionID, task.AgentID)
	s.sendTaskMail(task, fmt.Sprintf("%s 任务已完成", detail.AgentName), fmt.Sprintf("任务内容：%s。新的回复已生成，请打开应用查看详情。", detail.Summary))
}

func (s *Service) writeTaskAppNotification(ctx context.Context, task *model.Task, detail taskNotificationDetail) {
	if s == nil || s.store == nil || task == nil || task.OperatorID <= 0 {
		return
	}
	if strings.TrimSpace(task.ID) == "" {
		return
	}
	scopeID := ""
	if task.Metadata != nil {
		scopeID = strings.TrimSpace(task.Metadata["project_memory_scope_id"])
	}
	if scopeID == "" {
		if scope, err := s.store.GetProjectScopeForAgent(ctx, task.OperatorID, task.MachineID, task.AgentID); err == nil && scope != nil {
			scopeID = scope.ID
		}
	}
	query := url.Values{
		"machineId":      {strings.TrimSpace(task.MachineID)},
		"agentId":        {strings.TrimSpace(task.AgentID)},
		"projectId":      {strings.TrimSpace(task.ProjectID)},
		"projectRoot":    {strings.TrimSpace(task.ProjectRoot)},
		"projectScopeId": {scopeID},
	}
	operationID := "chat-task:" + strings.TrimSpace(task.ID)
	message := detail.Summary
	if project := compactText(task.ProjectID); project != "" {
		message = project + " · " + message
	}
	now := time.Now().UTC()
	record := map[string]any{
		"id":            operationID,
		"operation_id":  operationID,
		"title":         detail.AgentName + " · 任务已完成",
		"message":       message,
		"status":        "succeeded",
		"progress_mode": "determinate",
		"display_style": "compact",
		"kind":          "agent",
		"scope":         "synced",
		"attention":     "none",
		"progress":      1,
		"stages":        []any{},
		"actions": []map[string]any{{
			"label": "查看对话",
			"type":  "openRoute",
			"payload": map[string]string{
				"route": "/chat?" + query.Encode(),
			},
			"primary": true,
		}},
		"source_label": "Agent",
		"metadata": map[string]string{
			"machine_id":   strings.TrimSpace(task.MachineID),
			"agent_id":     strings.TrimSpace(task.AgentID),
			"project_id":   strings.TrimSpace(task.ProjectID),
			"project_root": strings.TrimSpace(task.ProjectRoot),
			"session_id":   strings.TrimSpace(task.SessionID),
			"task_id":      strings.TrimSpace(task.ID),
		},
		"created_at":   now.Format(time.RFC3339Nano),
		"updated_at":   now.Format(time.RFC3339Nano),
		"completed_at": now.Format(time.RFC3339Nano),
		"unread":       true,
	}
	raw, err := json.Marshal(record)
	if err != nil {
		log.Printf("app notification encode failed: task=%s err=%v", task.ID, err)
		return
	}
	if _, err := s.store.UpsertAppNotification(ctx, task.OperatorID, operationID, raw); err != nil {
		log.Printf("app notification persist failed: task=%s err=%v", task.ID, err)
	}
}

func (s *Service) NotifyTaskWaitingApproval(ctx context.Context, task *model.Task) {
	if task == nil || task.OperatorID <= 0 {
		log.Printf("skip task waiting approval notify: task_nil=%t operator_id=%d", task == nil, taskOperatorID(task))
		return
	}
	detail := s.taskNotificationDetail(task)
	log.Printf("notify task waiting approval start: task=%s operator=%d session=%s agent=%s", task.ID, task.OperatorID, task.SessionID, task.AgentID)
	if s.push != nil {
		if err := s.push.SendToOperator(ctx, task.OperatorID, detail.Title, fmt.Sprintf("等待确认：%s", detail.Summary), map[string]string{
			"type":       "chat_task_waiting_approval",
			"task_id":    task.ID,
			"session_id": task.SessionID,
			"agent_id":   task.AgentID,
			"agent_name": detail.AgentName,
			"summary":    detail.Summary,
		}); err != nil {
			log.Printf("push notify approval failed: task=%s err=%v", task.ID, err)
		}
	}
	s.sendTaskMail(task, fmt.Sprintf("%s 任务等待确认", detail.AgentName), fmt.Sprintf("任务内容：%s。有一条操作等待你的确认，请尽快处理。", detail.Summary))
}

func (s *Service) NotifyTaskQuestionAsked(ctx context.Context, task *model.Task) {
	if task == nil || task.OperatorID <= 0 {
		log.Printf("skip task question notify: task_nil=%t operator_id=%d", task == nil, taskOperatorID(task))
		return
	}
	detail := s.taskNotificationDetail(task)
	log.Printf("notify task question start: task=%s operator=%d session=%s agent=%s", task.ID, task.OperatorID, task.SessionID, task.AgentID)
	if s.push != nil {
		if err := s.push.SendToOperator(ctx, task.OperatorID, detail.Title, fmt.Sprintf("等待选择：%s", detail.Summary), map[string]string{
			"type":       "chat_task_question_asked",
			"task_id":    task.ID,
			"session_id": task.SessionID,
			"agent_id":   task.AgentID,
			"agent_name": detail.AgentName,
			"summary":    detail.Summary,
		}); err != nil {
			log.Printf("push notify question asked failed: task=%s err=%v", task.ID, err)
		}
	}
	summary := "有一项任务决策等待你的选择，请打开应用继续。"
	if task.Question != nil && strings.TrimSpace(task.Question.RequestID) != "" {
		summary = fmt.Sprintf("%s request_id：%s", summary, task.Question.RequestID)
	}
	s.sendTaskMail(task, fmt.Sprintf("%s 任务等待选择", detail.AgentName), fmt.Sprintf("任务内容：%s。%s", detail.Summary, summary))
}

func (s *Service) NotifyRuntimeUserAction(ctx context.Context, job model.RuntimeInstallJob, action model.RuntimeUserAction) {
	_ = ctx
	if s == nil || s.store == nil || s.mailer == nil || job.OperatorID <= 0 {
		log.Printf("skip runtime user action email: service_unavailable=%t operator_id=%d", s == nil || s.store == nil || s.mailer == nil, job.OperatorID)
		return
	}
	enabled, err := s.isEmailNotificationEnabled(job.OperatorID)
	if err != nil {
		log.Printf("load runtime user action email setting failed: job=%s operator=%d err=%v", job.ID, job.OperatorID, err)
		return
	}
	if !enabled {
		log.Printf("skip runtime user action email: job=%s operator=%d action=%s reason=disabled", job.ID, job.OperatorID, action.ID)
		return
	}
	email, err := s.getOperatorEmail(job.OperatorID)
	if err != nil {
		log.Printf("load runtime user action email failed: job=%s operator=%d err=%v", job.ID, job.OperatorID, err)
		return
	}
	if email == "" {
		log.Printf("skip runtime user action email: job=%s operator=%d action=%s reason=empty_email", job.ID, job.OperatorID, action.ID)
		return
	}
	title := emptyFallback(action.Title, "任务等待电脑端操作")
	message := emptyFallback(action.Message, "请在目标电脑完成系统提示的操作。")
	instructions := ""
	if len(action.Instructions) > 0 {
		parts := make([]string, 0, len(action.Instructions))
		for _, instruction := range action.Instructions {
			if instruction = strings.TrimSpace(instruction); instruction != "" {
				parts = append(parts, "<li>"+html.EscapeString(instruction)+"</li>")
			}
		}
		if len(parts) > 0 {
			instructions = "<ol>" + strings.Join(parts, "") + "</ol>"
		}
	}
	body := fmt.Sprintf(
		"<h3>【%s】%s</h3><p>%s</p>%s<p>设备：%s</p><p>任务ID：%s</p>",
		html.EscapeString(s.brandName),
		html.EscapeString(title),
		html.EscapeString(message),
		instructions,
		html.EscapeString(emptyFallback(job.MachineID, "-")),
		html.EscapeString(job.ID),
	)
	masked := maskEmail(email)
	log.Printf("send runtime user action email start: job=%s operator=%d action=%s email=%s", job.ID, job.OperatorID, action.ID, masked)
	if err := s.mailer.SendNotification(email, fmt.Sprintf("[%s] %s", s.brandName, title), body); err != nil {
		log.Printf("send runtime user action email failed: job=%s action=%s err=%v", job.ID, action.ID, err)
		return
	}
	log.Printf("send runtime user action email succeeded: job=%s operator=%d action=%s email=%s", job.ID, job.OperatorID, action.ID, masked)
}

type taskNotificationDetail struct {
	AgentName string
	Title     string
	Summary   string
}

func (s *Service) taskNotificationDetail(task *model.Task) taskNotificationDetail {
	agentName := taskAgentName(task)
	summary := taskInputSummary(task)
	return taskNotificationDetail{
		AgentName: agentName,
		Title:     fmt.Sprintf("%s · %s", s.brandName, agentName),
		Summary:   summary,
	}
}

func taskAgentName(task *model.Task) string {
	if task == nil {
		return "Agent"
	}
	if task.Metadata != nil {
		for _, key := range []string{"agent_name", "agent_display_name"} {
			if value := compactText(task.Metadata[key]); value != "" {
				return truncateRunes(value, 24)
			}
		}
	}
	if value := compactText(task.ProjectID); value != "" {
		return truncateRunes(value, 24)
	}
	if value := compactText(task.AgentID); value != "" {
		return truncateRunes(value, 24)
	}
	return "Agent"
}

func taskInputSummary(task *model.Task) string {
	if task == nil {
		return "打开应用查看详情"
	}
	if task.Metadata != nil {
		for _, key := range []string{"goal_objective", "goal", "objective"} {
			if value := compactText(task.Metadata[key]); value != "" {
				return truncateRunes(value, 48)
			}
		}
	}
	for _, part := range task.Parts {
		if part.Type == "text" {
			if value := compactText(part.Text); value != "" {
				return truncateRunes(value, 48)
			}
		}
	}
	for _, part := range task.Parts {
		if part.Type == "file" {
			if name := compactText(part.Filename); name != "" {
				return truncateRunes(fmt.Sprintf("附件 %s", name), 48)
			}
			if name := compactText(part.URL); name != "" {
				return truncateRunes("附件输入", 48)
			}
		}
	}
	if len(task.Parts) > 0 {
		return fmt.Sprintf("%d 段输入", len(task.Parts))
	}
	return "打开应用查看详情"
}

func compactText(value string) string {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) == 0 {
		return ""
	}
	return strings.Join(fields, " ")
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "..."
}

func (s *Service) sendTaskMail(task *model.Task, title, summary string) {
	if s == nil || s.store == nil || s.mailer == nil || task == nil {
		log.Printf("skip notify email: service_unavailable=%t task_nil=%t", s == nil || s.store == nil || s.mailer == nil, task == nil)
		return
	}
	enabled, err := s.isEmailNotificationEnabled(task.OperatorID)
	if err != nil {
		log.Printf("load email notification setting failed: operator=%d err=%v", task.OperatorID, err)
		return
	}
	if !enabled {
		log.Printf("skip notify email: task=%s operator=%d reason=disabled", task.ID, task.OperatorID)
		return
	}
	email, err := s.getOperatorEmail(task.OperatorID)
	if err != nil {
		log.Printf("load operator email failed: task=%s err=%v", task.ID, err)
		return
	}
	if email == "" {
		log.Printf("skip notify email: task=%s operator=%d reason=empty_email", task.ID, task.OperatorID)
		return
	}
	masked := maskEmail(email)
	body := fmt.Sprintf(
		"<h3>【%s】%s</h3><p>%s</p><p>任务ID：%s</p><p>会话ID：%s</p><p>Agent：%s</p>",
		s.brandName,
		title,
		summary,
		task.ID,
		emptyFallback(task.SessionID, "-"),
		emptyFallback(task.AgentID, "-"),
	)
	log.Printf("send notify email start: task=%s operator=%d email=%s title=%q", task.ID, task.OperatorID, masked, title)
	if err := s.mailer.SendNotification(email, fmt.Sprintf("[%s] %s", s.brandName, title), body); err != nil {
		log.Printf("send notify email failed: task=%s err=%v", task.ID, err)
		return
	}
	log.Printf("send notify email succeeded: task=%s operator=%d email=%s title=%q", task.ID, task.OperatorID, masked, title)
}

func (s *Service) isEmailNotificationEnabled(operatorID int64) (bool, error) {
	if s == nil || s.store == nil || operatorID <= 0 {
		return false, nil
	}
	settings, err := s.store.GetDeviceSettings(operatorID, userSettingsAgentID)
	if err != nil {
		return false, err
	}
	if settings == nil {
		return false, nil
	}
	return strings.TrimSpace(settings[emailNotificationEnabledKey]) == "true", nil
}

func (s *Service) getOperatorEmail(operatorID int64) (string, error) {
	if s == nil || s.operatorEmail == nil || operatorID <= 0 {
		return "", nil
	}
	email, err := s.operatorEmail(operatorID)
	return strings.TrimSpace(email), err
}

func emptyFallback(value, fallback string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func taskOperatorID(task *model.Task) int64 {
	if task == nil {
		return 0
	}
	return task.OperatorID
}

func maskEmail(email string) string {
	email = strings.TrimSpace(email)
	if email == "" {
		return ""
	}
	parts := strings.SplitN(email, "@", 2)
	if len(parts) != 2 {
		return email
	}
	local := []rune(parts[0])
	if len(local) <= 2 {
		return string(local[:1]) + "***@" + parts[1]
	}
	return string(local[:1]) + "***" + string(local[len(local)-1:]) + "@" + parts[1]
}
