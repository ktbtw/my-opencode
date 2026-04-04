package app

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/coder/websocket"
	"github.com/go-chi/chi/v5"

	"relay-server/internal/api"
	"relay-server/internal/broker"
	"relay-server/internal/model"
	"relay-server/internal/store"
)

type App struct {
	store  *store.Memory
	broker *broker.Broker
}

func New() (*App, error) {
	archive, err := store.NewTaskArchiveFromEnv()
	if err != nil {
		return nil, err
	}
	log.Printf("mysql archive initialized")
	return &App{
		store:  store.NewMemory(archive),
		broker: broker.New(),
	}, nil
}

func (a *App) Router() http.Handler {
	r := chi.NewRouter()
	h := api.New(a.store, a.broker)
	r.Use(cors)

	r.Get("/healthz", h.Health)
	r.Get("/api/agents", h.ListAgents)
	r.Get("/api/tasks", h.ListTasks)
	r.Get("/api/sessions", h.ListSessions)
	r.Post("/api/tasks", h.CreateTask)
	r.Get("/api/tasks/{taskID}", h.GetTask)
	r.Get("/api/tasks/{taskID}/events", h.TaskEvents)
	r.Post("/api/tasks/{taskID}/approval", h.ApproveTask)
	r.Post("/api/tasks/{taskID}/cancel", h.CancelTask)
	r.Get("/ws/device", a.device)
	return r
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) device(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		OriginPatterns: []string{"*"},
	})
	if err != nil {
		return
	}
	defer c.CloseNow()

	ctx := r.Context()
	_, buf, err := c.Read(ctx)
	if err != nil {
		return
	}

	var env model.Envelope
	if err := json.Unmarshal(buf, &env); err != nil || env.Type != "device.hello" {
		c.Write(ctx, websocket.MessageText, []byte(`{"type":"error","payload":{"error":"invalid hello"}}`))
		return
	}

	body, err := json.Marshal(env.Payload)
	if err != nil {
		return
	}

	var hello model.HelloPayload
	if err := json.Unmarshal(body, &hello); err != nil || (hello.AgentID == "" && hello.DeviceID == "") {
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

	device := a.broker.Add(c, hello)
	defer a.broker.Remove(agentID)

	msg := model.Envelope{
		Type:      "device.welcome",
		RequestID: env.RequestID,
		SentAt:    time.Now().UTC().Format(time.RFC3339),
		Payload: model.WelcomePayload{
			AgentID:              device.ID,
			HeartbeatIntervalSec: 15,
		},
	}

	raw, _ := json.Marshal(msg)
	if err := c.Write(ctx, websocket.MessageText, raw); err != nil {
		return
	}

	for {
		_, buf, err := c.Read(ctx)
		if err != nil {
			return
		}
		if err := a.handle(device.ID, buf); err != nil {
			fail := fmt.Sprintf(`{"type":"error","payload":{"error":%q}}`, err.Error())
			if c.Write(ctx, websocket.MessageText, []byte(fail)) != nil {
				return
			}
		}
	}
}

func (a *App) handle(agentID string, buf []byte) error {
	var env model.Envelope
	if err := json.Unmarshal(buf, &env); err != nil {
		return err
	}

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
		a.broker.Touch(agentID, msg.RunningTaskID)
		return nil
	case "task.started":
		var msg model.StartedPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		a.store.SetStatus(msg.TaskID, model.TaskRunning)
		a.store.TrackSession(msg.TaskID, msg.SessionID, "active", "任务运行中")
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
		a.store.AddEvent(msg.TaskID, model.Event{
			TaskID:  msg.TaskID,
			Type:    "delta",
			Content: msg.Content,
			SentAt:  time.Now().UTC(),
		})
		return nil
	case "task.completed":
		var msg model.CompletedPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		a.store.Complete(msg.TaskID, msg.SessionID, msg.Result)
		a.store.TrackSession(msg.TaskID, msg.SessionID, "active", msg.Result)
		a.store.AddEvent(msg.TaskID, model.Event{
			TaskID:    msg.TaskID,
			Type:      "completed",
			Content:   msg.Result,
			SessionID: msg.SessionID,
			SentAt:    time.Now().UTC(),
		})
		return nil
	case "task.waiting_approval":
		var msg model.WaitingApprovalPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
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
		return nil
	case "task.approval_applied":
		var msg model.ApprovalAppliedPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
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
	case "task.failed":
		var msg model.FailedPayload
		if err := json.Unmarshal(body, &msg); err != nil {
			return err
		}
		a.store.Fail(msg.TaskID, msg.SessionID, msg.Error)
		a.store.TrackSession(msg.TaskID, msg.SessionID, "error", msg.Error)
		a.store.AddEvent(msg.TaskID, model.Event{
			TaskID:    msg.TaskID,
			Type:      "failed",
			Error:     msg.Error,
			SessionID: msg.SessionID,
			SentAt:    time.Now().UTC(),
		})
		return nil
	default:
		return nil
	}
}
