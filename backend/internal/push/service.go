package push

import (
	"context"
	"errors"
	"log"
	"strings"

	"relay-server/internal/model"
	"relay-server/internal/store"
)

type Sender interface {
	SendAndroid(ctx context.Context, req SendRequest) error
}

type Service struct {
	store  *store.Memory
	sender Sender
}

type SendRequest struct {
	RegistrationIDs []string
	Title           string
	Body            string
	Extras          map[string]string
	NotificationID  int
	ChannelID       string
}

func NewService(s *store.Memory, sender Sender) *Service {
	return &Service{store: s, sender: sender}
}

func (s *Service) RegisterDevice(device model.PushDevice) (*model.PushDevice, error) {
	if s == nil || s.store == nil {
		return nil, errors.New("push service unavailable")
	}
	if device.OperatorID <= 0 {
		return nil, errors.New("missing operator_id")
	}
	if strings.TrimSpace(device.Platform) == "" {
		device.Platform = "android"
	}
	if strings.TrimSpace(device.Vendor) == "" {
		device.Vendor = "jpush"
	}
	if strings.TrimSpace(device.RegistrationID) == "" {
		return nil, errors.New("missing registration_id")
	}
	return s.store.UpsertPushDevice(device)
}

func (s *Service) SendTestToOperator(ctx context.Context, operatorID int64, title, body string) error {
	return s.SendToOperator(ctx, operatorID, title, body, map[string]string{
		"type": "push_test",
	})
}

func (s *Service) NotifyTaskCompleted(ctx context.Context, task *model.Task) {
	if task == nil || task.OperatorID <= 0 {
		return
	}
	if err := s.SendToOperator(ctx, task.OperatorID, "码控", "新的回复已生成", map[string]string{
		"type":       "chat_task_completed",
		"task_id":    task.ID,
		"session_id": task.SessionID,
		"agent_id":   task.AgentID,
	}); err != nil {
		log.Printf("push task completed failed: task=%s err=%v", task.ID, err)
	}
}

func (s *Service) NotifyTaskWaitingApproval(ctx context.Context, task *model.Task) {
	if task == nil || task.OperatorID <= 0 {
		return
	}
	if err := s.SendToOperator(ctx, task.OperatorID, "码控", "有一条操作等待确认", map[string]string{
		"type":       "chat_task_waiting_approval",
		"task_id":    task.ID,
		"session_id": task.SessionID,
		"agent_id":   task.AgentID,
	}); err != nil {
		log.Printf("push task waiting approval failed: task=%s err=%v", task.ID, err)
	}
}

func (s *Service) SendToOperator(ctx context.Context, operatorID int64, title, body string, extras map[string]string) error {
	if s == nil || s.store == nil {
		return errors.New("push service unavailable")
	}
	devices, err := s.store.ListPushDevices(operatorID)
	if err != nil {
		return err
	}
	registrationIDs := make([]string, 0, len(devices))
	for _, device := range devices {
		if device == nil || !device.Enabled {
			continue
		}
		if strings.TrimSpace(device.Platform) != "android" {
			continue
		}
		if strings.TrimSpace(device.Vendor) != "jpush" {
			continue
		}
		registrationIDs = append(registrationIDs, strings.TrimSpace(device.RegistrationID))
	}
	if len(registrationIDs) == 0 {
		return errors.New("no android push device registered")
	}
	if s.sender == nil {
		return errors.New("push sender unavailable")
	}
	return s.sender.SendAndroid(ctx, SendRequest{
		RegistrationIDs: registrationIDs,
		Title:           title,
		Body:            body,
		Extras:          extras,
		ChannelID:       "chat_codex_default",
	})
}
