package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"relay-server/internal/auth"
	"relay-server/internal/broker"
	"relay-server/internal/model"
	"relay-server/internal/store"
)

type API struct {
	store  *store.Memory
	broker *broker.Broker
	auth   *auth.Manager
}

type createTaskReq struct {
	AgentID   string            `json:"agent_id"`
	ProjectID string            `json:"project_id"`
	Parts     []model.Part      `json:"parts"`
	SessionID string            `json:"session_id"`
	Metadata  map[string]string `json:"metadata"`
}

type approveTaskReq struct {
	PermissionID string `json:"permission_id"`
	Reply        string `json:"reply"`
	Message      string `json:"message"`
}

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func New(store *store.Memory, broker *broker.Broker, authManager *auth.Manager) *API {
	return &API{store: store, broker: broker, auth: authManager}
}

func (a *API) Health(w http.ResponseWriter, _ *http.Request) {
	write(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (a *API) ListAgents(w http.ResponseWriter, _ *http.Request) {
	write(w, http.StatusOK, a.broker.List())
}

func (a *API) Login(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	operator, err := a.store.AuthenticateOperator(req.Username, req.Password)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "login failed"})
		return
	}
	if operator == nil {
		write(w, http.StatusUnauthorized, map[string]string{"error": "username or password invalid"})
		return
	}
	token, err := a.auth.Issue(*operator)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "issue token failed"})
		return
	}
	write(w, http.StatusOK, map[string]any{
		"access_token": token,
		"operator":     operator,
	})
}

func (a *API) Me(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	write(w, http.StatusOK, operator)
}

func (a *API) ListDevices(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	write(w, http.StatusOK, a.broker.ListMachines(operator.ID))
}

func (a *API) GetDevice(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	machineID := chi.URLParam(r, "machineID")
	device, ok := a.broker.GetMachine(operator.ID, machineID)
	if !ok {
		write(w, http.StatusNotFound, map[string]string{"error": "device not found"})
		return
	}
	write(w, http.StatusOK, device)
}

func (a *API) ListTasks(w http.ResponseWriter, r *http.Request) {
	filter := model.TaskFilter{
		AgentID:   r.URL.Query().Get("agent_id"),
		MachineID: r.URL.Query().Get("machine_id"),
		ProjectID: r.URL.Query().Get("project_id"),
		SessionID: r.URL.Query().Get("session_id"),
		Status:    model.TaskStatus(r.URL.Query().Get("status")),
		Limit:     50,
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		var limit int
		if _, err := fmt.Sscanf(raw, "%d", &limit); err != nil || limit <= 0 || limit > 200 {
			write(w, http.StatusBadRequest, map[string]string{"error": "invalid limit"})
			return
		}
		filter.Limit = limit
	}

	tasks, err := a.store.ListTasks(filter)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "list tasks failed"})
		return
	}
	write(w, http.StatusOK, tasks)
}

func (a *API) ListSessions(w http.ResponseWriter, r *http.Request) {
	filter := model.SessionFilter{
		AgentID:   r.URL.Query().Get("agent_id"),
		MachineID: r.URL.Query().Get("machine_id"),
		ProjectID: r.URL.Query().Get("project_id"),
		Status:    r.URL.Query().Get("status"),
		Limit:     50,
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		var limit int
		if _, err := fmt.Sscanf(raw, "%d", &limit); err != nil || limit <= 0 || limit > 200 {
			write(w, http.StatusBadRequest, map[string]string{"error": "invalid limit"})
			return
		}
		filter.Limit = limit
	}

	sessions, err := a.store.ListSessions(filter)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "list sessions failed"})
		return
	}
	write(w, http.StatusOK, sessions)
}

func (a *API) CreateTask(w http.ResponseWriter, r *http.Request) {
	var req createTaskReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	parts, err := normalize(req)
	if err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.AgentID == "" || req.ProjectID == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "missing required fields"})
		return
	}
	agent, ok := a.broker.Get(req.AgentID)
	if !ok {
		write(w, http.StatusConflict, map[string]string{"error": "agent offline"})
		return
	}
	projectRoot := ""
	for _, item := range agent.Projects {
		if item.ProjectID == req.ProjectID {
			projectRoot = item.Root
			break
		}
	}
	if projectRoot == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "agent does not own project"})
		return
	}

	task := a.store.CreateTask(req.AgentID, agent.MachineID, req.ProjectID, projectRoot, req.SessionID, parts)
	a.store.SetStatus(task.ID, model.TaskDispatched)
	a.store.AddEvent(task.ID, model.Event{
		TaskID:  task.ID,
		Type:    "dispatched",
		Content: summarize(parts),
		SentAt:  time.Now().UTC(),
	})

	env := model.Envelope{
		Type:      "task.run",
		RequestID: fmt.Sprintf("req_%s", task.ID),
		SentAt:    time.Now().UTC().Format(time.RFC3339),
		Payload: model.RunPayload{
			TaskID:    task.ID,
			AgentID:   req.AgentID,
			MachineID: agent.MachineID,
			ProjectID: req.ProjectID,
			SessionID: req.SessionID,
			Parts:     parts,
			Metadata:  req.Metadata,
		},
	}

	if err := a.broker.Dispatch(context.Background(), req.AgentID, env); err != nil {
		a.store.Fail(task.ID, req.SessionID, err.Error())
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}

	task, _ = a.store.GetTask(task.ID)
	write(w, http.StatusAccepted, task)
}

func (a *API) GetTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	task, ok := a.store.GetTask(taskID)
	if !ok {
		write(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	write(w, http.StatusOK, task)
}

func (a *API) TaskEvents(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	if _, ok := a.store.GetTask(taskID); !ok {
		write(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	for _, evt := range a.store.Events(taskID) {
		body, _ := json.Marshal(evt)
		fmt.Fprintf(w, "event: %s\n", evt.Type)
		fmt.Fprintf(w, "data: %s\n\n", body)
	}
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func (a *API) CancelTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	task, ok := a.store.Cancel(taskID)
	if !ok {
		write(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}

	env := model.Envelope{
		Type:      "task.cancel",
		RequestID: fmt.Sprintf("req_cancel_%s", task.ID),
		SentAt:    time.Now().UTC().Format(time.RFC3339),
		Payload: model.CancelPayload{
			TaskID: task.ID,
		},
	}
	_ = a.broker.Dispatch(context.Background(), task.AgentID, env)

	a.store.AddEvent(task.ID, model.Event{
		TaskID:  task.ID,
		Type:    "cancelled",
		SentAt:  time.Now().UTC(),
		Content: "task cancelled",
	})

	write(w, http.StatusOK, task)
}

func (a *API) ApproveTask(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskID")
	task, ok := a.store.GetTask(taskID)
	if !ok {
		write(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	if task.Status != model.TaskWaitingApproval || task.Approval == nil {
		write(w, http.StatusConflict, map[string]string{"error": "task is not waiting approval"})
		return
	}

	var req approveTaskReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if req.Reply != "once" && req.Reply != "always" && req.Reply != "reject" {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid reply"})
		return
	}
	if req.PermissionID == "" {
		req.PermissionID = task.Approval.PermissionID
	}
	if req.PermissionID != task.Approval.PermissionID {
		write(w, http.StatusBadRequest, map[string]string{"error": "permission_id mismatch"})
		return
	}

	env := model.Envelope{
		Type:      "task.approval_response",
		RequestID: fmt.Sprintf("req_approval_%s", task.ID),
		SentAt:    time.Now().UTC().Format(time.RFC3339),
		Payload: model.ApprovalResponsePayload{
			TaskID:       task.ID,
			PermissionID: req.PermissionID,
			Reply:        req.Reply,
			Message:      req.Message,
		},
	}

	if err := a.broker.Dispatch(context.Background(), task.AgentID, env); err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}

	a.store.AddEvent(task.ID, model.Event{
		TaskID:       task.ID,
		Type:         "approval_requested",
		SessionID:    task.SessionID,
		PermissionID: req.PermissionID,
		Reply:        req.Reply,
		Content:      "approval response dispatched",
		SentAt:       time.Now().UTC(),
	})

	task, _ = a.store.GetTask(task.ID)
	write(w, http.StatusAccepted, task)
}

func write(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func (a *API) currentOperator(r *http.Request) (model.Operator, bool) {
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if authHeader == "" {
		return model.Operator{}, false
	}
	token := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	return a.auth.Verify(token)
}

func normalize(req createTaskReq) ([]model.Part, error) {
	if len(req.Parts) == 0 {
		return nil, errors.New("missing parts")
	}

	out := make([]model.Part, 0, len(req.Parts))

	for _, part := range req.Parts {
		switch part.Type {
		case "text":
			if part.Text == "" {
				return nil, errors.New("text part cannot be empty")
			}
			out = append(out, part)
		case "file":
			if part.URL == "" {
				return nil, errors.New("file part missing url")
			}
			out = append(out, part)
		case "agent":
			if part.Name == "" {
				return nil, errors.New("agent part missing name")
			}
			out = append(out, part)
		case "subtask":
			if part.Prompt == "" || part.Description == "" || part.Agent == "" {
				return nil, errors.New("subtask part missing required fields")
			}
			out = append(out, part)
		default:
			return nil, fmt.Errorf("unsupported part type: %s", part.Type)
		}
	}
	return out, nil
}

func summarize(parts []model.Part) string {
	files := 0
	for _, part := range parts {
		if part.Type == "file" {
			files++
		}
	}
	if files == 0 {
		return "multi-part input"
	}
	if files == 1 && len(parts) == 1 {
		return "file input"
	}
	return fmt.Sprintf("multi-part input (%d files)", files)
}
