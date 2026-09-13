package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"relay-server/internal/model"
)

const defaultModelTestPrompt = "请只回复“连接正常”，并保持流式输出。"

var defaultModelTestHub = newModelTestHub()

type createModelTestReq struct {
	AgentID   string `json:"agent_id"`
	ProjectID string `json:"project_id"`
	Model     string `json:"model"`
	Variant   string `json:"variant"`
	Prompt    string `json:"prompt"`
}

type modelTestJob struct {
	TestID     string    `json:"test_id"`
	AgentID    string    `json:"agent_id"`
	MachineID  string    `json:"machine_id,omitempty"`
	ProjectID  string    `json:"project_id"`
	ProviderID string    `json:"provider_id,omitempty"`
	ModelID    string    `json:"model_id,omitempty"`
	Model      string    `json:"model"`
	Variant    string    `json:"variant,omitempty"`
	Status     string    `json:"status"`
	Error      string    `json:"error,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	OperatorID int64     `json:"-"`
}

type modelTestEvent struct {
	TestID          string         `json:"test_id"`
	Type            string         `json:"type"`
	SessionID       string         `json:"session_id,omitempty"`
	Content         string         `json:"content,omitempty"`
	Field           string         `json:"field,omitempty"`
	Error           string         `json:"error,omitempty"`
	ErrorDetail     string         `json:"error_detail,omitempty"`
	ElapsedMS       int64          `json:"elapsed_ms,omitempty"`
	StepMS          int64          `json:"step_ms,omitempty"`
	TextLength      int            `json:"text_length,omitempty"`
	ReasoningLength int            `json:"reasoning_length,omitempty"`
	TokenCount      int            `json:"token_count,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
	SentAt          time.Time      `json:"sent_at"`
}

type modelTestHub struct {
	mu     sync.RWMutex
	jobs   map[string]*modelTestJob
	events map[string][]modelTestEvent
	subs   map[string][]chan modelTestEvent
}

func newModelTestHub() *modelTestHub {
	return &modelTestHub{
		jobs:   map[string]*modelTestJob{},
		events: map[string][]modelTestEvent{},
		subs:   map[string][]chan modelTestEvent{},
	}
}

func (h *modelTestHub) create(job *modelTestJob) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.jobs[job.TestID] = job
	h.events[job.TestID] = []modelTestEvent{}
}

func (h *modelTestHub) get(testID string) (*modelTestJob, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	job, ok := h.jobs[testID]
	if !ok || job == nil {
		return nil, false
	}
	out := *job
	return &out, true
}

func (h *modelTestHub) add(event modelTestEvent) {
	if event.TestID == "" {
		return
	}
	if event.SentAt.IsZero() {
		event.SentAt = time.Now().UTC()
	}
	h.mu.Lock()
	if job := h.jobs[event.TestID]; job != nil {
		job.UpdatedAt = event.SentAt
		switch event.Type {
		case "started", "progress", "delta":
			job.Status = "running"
		case "completed":
			job.Status = "completed"
		case "failed", "cancelled":
			job.Status = event.Type
			job.Error = event.Error
		}
	}
	h.events[event.TestID] = append(h.events[event.TestID], event)
	subs := append([]chan modelTestEvent(nil), h.subs[event.TestID]...)
	h.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- event:
		default:
		}
	}
}

func (h *modelTestHub) addPayload(eventType string, payload model.ModelTestEventPayload) {
	h.mu.RLock()
	job := h.jobs[payload.TestID]
	h.mu.RUnlock()
	if job == nil {
		return
	}
	if payload.AgentID != "" && payload.AgentID != job.AgentID {
		return
	}
	if payload.MachineID != "" && payload.MachineID != job.MachineID {
		return
	}
	if payload.ProjectID != "" && payload.ProjectID != job.ProjectID {
		return
	}
	h.add(modelTestEvent{
		TestID:          payload.TestID,
		Type:            strings.TrimPrefix(eventType, "model.test."),
		SessionID:       payload.SessionID,
		Content:         payload.Content,
		Field:           payload.Field,
		Error:           payload.Error,
		ErrorDetail:     payload.ErrorDetail,
		ElapsedMS:       payload.ElapsedMS,
		StepMS:          payload.StepMS,
		TextLength:      payload.TextLength,
		ReasoningLength: payload.ReasoningLength,
		TokenCount:      payload.TokenCount,
		Metadata:        payload.Metadata,
	})
}

func (h *modelTestHub) subscribe(testID string) (<-chan modelTestEvent, func()) {
	ch := make(chan modelTestEvent, 32)
	h.mu.Lock()
	h.subs[testID] = append(h.subs[testID], ch)
	h.mu.Unlock()
	cancel := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		items := h.subs[testID]
		for i, item := range items {
			if item == ch {
				h.subs[testID] = append(items[:i], items[i+1:]...)
				break
			}
		}
		close(ch)
	}
	return ch, cancel
}

func (h *modelTestHub) history(testID string) []modelTestEvent {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return append([]modelTestEvent(nil), h.events[testID]...)
}

func (a *API) CreateModelTest(w http.ResponseWriter, r *http.Request) {
	startedAt := time.Now()
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var req createModelTestReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	req.AgentID = strings.TrimSpace(req.AgentID)
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	req.Model = strings.TrimSpace(req.Model)
	req.Variant = strings.TrimSpace(req.Variant)
	req.Prompt = strings.TrimSpace(req.Prompt)
	if req.Prompt == "" {
		req.Prompt = defaultModelTestPrompt
	}
	if req.AgentID == "" || req.ProjectID == "" || req.Model == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "missing required fields"})
		return
	}
	providerID, modelID := splitModelRef(req.Model)
	if providerID == "" || modelID == "" {
		write(w, http.StatusBadRequest, map[string]string{"error": "模型格式必须是 provider/model"})
		return
	}
	agent, ok := a.broker.Get(req.AgentID, operator.ID)
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
	testID := fmt.Sprintf("model_test_%d", time.Now().UnixNano())
	job := &modelTestJob{
		TestID:     testID,
		AgentID:    req.AgentID,
		MachineID:  agent.MachineID,
		ProjectID:  req.ProjectID,
		ProviderID: providerID,
		ModelID:    modelID,
		Model:      req.Model,
		Variant:    req.Variant,
		Status:     "dispatched",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
		OperatorID: operator.ID,
	}
	defaultModelTestHub.create(job)
	defaultModelTestHub.add(modelTestEvent{
		TestID: testID,
		Type:   "dispatched",
		Metadata: map[string]any{
			"agent_id":     req.AgentID,
			"machine_id":   agent.MachineID,
			"project_id":   req.ProjectID,
			"project_root": projectRoot,
			"model":        req.Model,
			"variant":      req.Variant,
		},
	})
	latencyLog("backend_api", "model_test_created", map[string]any{
		"elapsed_ms":  time.Since(startedAt).Milliseconds(),
		"operator_id": operator.ID,
		"test_id":     testID,
		"agent_id":    req.AgentID,
		"machine_id":  agent.MachineID,
		"project_id":  req.ProjectID,
		"model":       req.Model,
		"variant":     req.Variant,
	})
	env := model.Envelope{
		Type:      "model.test.run",
		RequestID: fmt.Sprintf("req_%s", testID),
		SentAt:    time.Now().UTC().Format(time.RFC3339),
		Payload: model.ModelTestRunPayload{
			TestID:     testID,
			AgentID:    req.AgentID,
			MachineID:  agent.MachineID,
			ProjectID:  req.ProjectID,
			ProviderID: providerID,
			ModelID:    modelID,
			Model:      req.Model,
			Variant:    req.Variant,
			Prompt:     req.Prompt,
		},
	}
	if err := a.broker.DispatchForOperator(context.Background(), operator.ID, req.AgentID, env); err != nil {
		defaultModelTestHub.add(modelTestEvent{
			TestID: testID,
			Type:   "failed",
			Error:  err.Error(),
		})
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusAccepted, job)
}

func (a *API) ModelTestEvents(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	testID := chi.URLParam(r, "testID")
	job, found := defaultModelTestHub.get(testID)
	if !found {
		write(w, http.StatusNotFound, map[string]string{"error": "model test not found"})
		return
	}
	if job.OperatorID != operator.ID {
		write(w, http.StatusNotFound, map[string]string{"error": "model test not found"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, canFlush := w.(http.Flusher)
	sendEvent := func(evt modelTestEvent) {
		body, _ := json.Marshal(evt)
		fmt.Fprintf(w, "event: %s\n", evt.Type)
		fmt.Fprintf(w, "data: %s\n\n", body)
		if canFlush {
			flusher.Flush()
		}
	}
	ch, cancel := defaultModelTestHub.subscribe(testID)
	defer cancel()
	history := defaultModelTestHub.history(testID)
	for _, evt := range history {
		sendEvent(evt)
	}
	isTerminal := func(t string) bool {
		return t == "completed" || t == "failed" || t == "cancelled"
	}
	for _, evt := range history {
		if isTerminal(evt.Type) {
			return
		}
	}
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprintf(w, ": ping\n\n")
			if canFlush {
				flusher.Flush()
			}
		case evt, ok := <-ch:
			if !ok {
				return
			}
			sendEvent(evt)
			if isTerminal(evt.Type) {
				return
			}
		}
	}
}

func HandleModelTestEvent(eventType string, payload model.ModelTestEventPayload) {
	defaultModelTestHub.addPayload(eventType, payload)
}
