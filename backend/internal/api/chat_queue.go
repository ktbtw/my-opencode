package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"relay-server/internal/model"
	"relay-server/internal/projectmemory"
)

const chatQueueInsertAckTimeout = 20 * time.Second

const chatQueueContinueAfterCancelSettingKey = "chat_queue_continue_after_cancel"

const (
	chatQueueAgentBusyRetryKey     = "_chat_queue_agent_busy_retries"
	chatQueueAgentBusyMaxRetries   = 5
	chatQueueDispatchAfterTerminal = 500 * time.Millisecond
)

func newChatQueueInjectionVersion() int64 {
	// This value crosses a JavaScript relay, so it must remain exactly
	// representable by Number. Unix microseconds are unique enough here and
	// stay below Number.MAX_SAFE_INTEGER for centuries.
	return time.Now().UnixMicro()
}

func (a *API) chatQueueSession(operatorID int64, sessionID string) (*model.Session, bool) {
	sessions, err := a.store.ListSessions(model.SessionFilter{OperatorID: operatorID, Limit: 200})
	if err == nil {
		for _, session := range sessions {
			if session.ID == sessionID {
				return session, true
			}
		}
	}
	tasks, err := a.store.ListTasks(model.TaskFilter{OperatorID: operatorID, SessionID: sessionID, Limit: 1, CreatedDesc: true})
	if err != nil || len(tasks) == 0 {
		return nil, false
	}
	task := tasks[0]
	return &model.Session{
		ID:         sessionID,
		AgentID:    task.AgentID,
		MachineID:  task.MachineID,
		ProjectID:  task.ProjectID,
		OperatorID: operatorID,
	}, true
}

func (a *API) ListChatQueue(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	sessionID := strings.TrimSpace(chi.URLParam(r, "sessionID"))
	if _, exists := a.chatQueueSession(operator.ID, sessionID); !exists {
		write(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	write(w, http.StatusOK, a.store.ChatQueueSnapshot(operator.ID, sessionID))
}

func (a *API) ChatQueueEvents(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	sessionID := strings.TrimSpace(chi.URLParam(r, "sessionID"))
	if _, exists := a.chatQueueSession(operator.ID, sessionID); !exists {
		write(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, canFlush := w.(http.Flusher)
	send := func(snapshot model.ChatQueueSnapshot) {
		body, _ := json.Marshal(snapshot)
		fmt.Fprintf(w, "event: queue.snapshot\ndata: %s\n\n", body)
		if canFlush {
			flusher.Flush()
		}
	}
	updates, cancel := a.store.SubscribeChatQueue(operator.ID, sessionID)
	defer cancel()
	send(a.store.ChatQueueSnapshot(operator.ID, sessionID))
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprint(w, ": ping\n\n")
			if canFlush {
				flusher.Flush()
			}
		case snapshot, open := <-updates:
			if !open {
				return
			}
			send(snapshot)
		}
	}
}

func (a *API) CreateChatQueueItem(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	sessionID := strings.TrimSpace(chi.URLParam(r, "sessionID"))
	session, exists := a.chatQueueSession(operator.ID, sessionID)
	if !exists {
		write(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	var req createChatQueueItemReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	parts, err := normalize(createTaskReq{AgentID: req.AgentID, ProjectID: req.ProjectID, Parts: req.Parts, SessionID: sessionID, Metadata: req.Metadata})
	if err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if req.AgentID != session.AgentID || req.ProjectID != session.ProjectID {
		write(w, http.StatusBadRequest, map[string]string{"error": "queue target does not match session"})
		return
	}
	item := a.store.CreateChatQueueItem(model.ChatQueueItem{
		OperatorID: operator.ID,
		SessionID:  sessionID,
		AgentID:    session.AgentID,
		MachineID:  session.MachineID,
		ProjectID:  session.ProjectID,
		Parts:      parts,
		Metadata:   normalizeGoalTaskMetadata(req.Metadata),
	})
	if !a.hasActiveQueueTask(operator.ID, session.AgentID) {
		_, _ = a.DispatchNextChatQueueItem(operator.ID, sessionID)
		if latest, found := a.store.GetChatQueueItem(operator.ID, item.ID); found {
			item = latest
		}
	}
	write(w, http.StatusAccepted, item)
}

func (a *API) UpdateChatQueueItem(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var req updateChatQueueItemReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	item, found, err := a.store.UpdateChatQueueItem(operator.ID, chi.URLParam(r, "itemID"), req.ExpectedVersion, func(item *model.ChatQueueItem) error {
		if item.Status != model.ChatQueueQueued {
			return fmt.Errorf("queue item is already in progress")
		}
		if item.Metadata == nil {
			item.Metadata = map[string]string{}
		}
		modelRef := strings.TrimSpace(req.Model)
		variant := strings.TrimSpace(req.Variant)
		if modelRef == "" {
			delete(item.Metadata, "model")
		} else {
			item.Metadata["model"] = modelRef
		}
		if variant == "" {
			delete(item.Metadata, "variant")
		} else {
			item.Metadata["variant"] = variant
		}
		return nil
	})
	if !found {
		write(w, http.StatusNotFound, map[string]string{"error": "queue item not found"})
		return
	}
	if err != nil {
		write(w, http.StatusConflict, map[string]any{"error": err.Error(), "item": item})
		return
	}
	write(w, http.StatusOK, item)
}

func (a *API) DeleteChatQueueItem(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var req deleteChatQueueItemReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	item, found, err := a.store.DeleteChatQueueItem(operator.ID, chi.URLParam(r, "itemID"), req.ExpectedVersion)
	if !found {
		write(w, http.StatusNotFound, map[string]string{"error": "queue item not found"})
		return
	}
	if err != nil {
		write(w, http.StatusConflict, map[string]any{"error": err.Error(), "item": item})
		return
	}
	write(w, http.StatusOK, map[string]any{"deleted": true, "item": item})
}

func (a *API) ReorderChatQueue(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	sessionID := strings.TrimSpace(chi.URLParam(r, "sessionID"))
	if _, exists := a.chatQueueSession(operator.ID, sessionID); !exists {
		write(w, http.StatusNotFound, map[string]string{"error": "session not found"})
		return
	}
	var req reorderChatQueueReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	snapshot, err := a.store.ReorderChatQueue(operator.ID, sessionID, req.ItemIDs, req.ExpectedVersions)
	if err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	write(w, http.StatusOK, snapshot)
}

func (a *API) InsertChatQueueItem(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	var req insertChatQueueItemReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	itemID := chi.URLParam(r, "itemID")
	current, found := a.store.GetChatQueueItem(operator.ID, itemID)
	if !found {
		write(w, http.StatusNotFound, map[string]string{"error": "queue item not found"})
		return
	}
	active := a.activeQueueTask(operator.ID, current.AgentID)
	if active == nil || active.SessionID != current.SessionID {
		write(w, http.StatusConflict, map[string]string{"error": "session has no active task"})
		return
	}
	injectionVersion := newChatQueueInjectionVersion()
	item, _, err := a.store.UpdateChatQueueItem(operator.ID, itemID, req.ExpectedVersion, func(item *model.ChatQueueItem) error {
		if item.Status != model.ChatQueueQueued {
			return fmt.Errorf("queue item is already in progress")
		}
		item.Status = model.ChatQueueInserting
		item.InjectionVersion = injectionVersion
		item.TaskID = active.ID
		item.Error = ""
		return nil
	})
	if err != nil {
		write(w, http.StatusConflict, map[string]any{"error": err.Error(), "item": item})
		return
	}
	env := model.Envelope{
		Type:      "task.input",
		RequestID: fmt.Sprintf("req_input_%s_%d", item.ID, injectionVersion),
		SentAt:    time.Now().UTC().Format(time.RFC3339),
		Payload: model.TaskInputPayload{
			TaskID:           active.ID,
			QueueItemID:      item.ID,
			InjectionVersion: injectionVersion,
			Parts:            item.Parts,
			Metadata:         item.Metadata,
		},
	}
	if err := a.broker.DispatchForOperator(context.Background(), operator.ID, active.AgentID, env); err != nil {
		item, _, _ = a.store.UpdateChatQueueItem(operator.ID, item.ID, item.Version, func(item *model.ChatQueueItem) error {
			item.Status = model.ChatQueueQueued
			item.TaskID = ""
			item.Error = err.Error()
			return nil
		})
		write(w, http.StatusConflict, map[string]any{"error": err.Error(), "item": item})
		return
	}
	go a.expireChatQueueInsertion(operator.ID, item.ID, injectionVersion)
	write(w, http.StatusAccepted, item)
}

func (a *API) SendChatQueueItem(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	itemID := chi.URLParam(r, "itemID")
	item, found := a.store.GetChatQueueItem(operator.ID, itemID)
	if !found {
		write(w, http.StatusNotFound, map[string]string{"error": "queue item not found"})
		return
	}
	if a.hasActiveQueueTask(operator.ID, item.AgentID) {
		write(w, http.StatusConflict, map[string]string{"error": "session has an active task"})
		return
	}
	if !a.store.TryBeginChatQueueDispatch(operator.ID, item.SessionID) {
		write(w, http.StatusConflict, map[string]string{"error": "queue dispatch already in progress"})
		return
	}
	defer a.store.EndChatQueueDispatch(operator.ID, item.SessionID)
	claimed, ok := a.store.ClaimChatQueueItem(operator.ID, item.SessionID, item.ID)
	if !ok {
		write(w, http.StatusConflict, map[string]string{"error": "queue item is already in progress"})
		return
	}
	task, err := a.dispatchChatQueueItem(operator.ID, claimed)
	if err != nil {
		write(w, http.StatusConflict, map[string]any{"error": err.Error()})
		return
	}
	if updated, ok := a.store.GetChatQueueItem(operator.ID, claimed.ID); ok {
		write(w, http.StatusAccepted, updated)
		return
	}
	write(w, http.StatusAccepted, task)
}

func (a *API) expireChatQueueInsertion(operatorID int64, itemID string, injectionVersion int64) {
	timer := time.NewTimer(chatQueueInsertAckTimeout)
	defer timer.Stop()
	<-timer.C
	a.restoreExpiredChatQueueInsertion(operatorID, itemID, injectionVersion)
}

func (a *API) restoreExpiredChatQueueInsertion(operatorID int64, itemID string, injectionVersion int64) {
	item, found := a.store.GetChatQueueItem(operatorID, itemID)
	if !found || item.Status != model.ChatQueueInserting || item.InjectionVersion != injectionVersion {
		return
	}
	_, _, _ = a.store.UpdateChatQueueItem(operatorID, itemID, item.Version, func(item *model.ChatQueueItem) error {
		item.Status = model.ChatQueueQueued
		item.TaskID = ""
		item.Error = "插入确认超时，已保留在队列"
		return nil
	})
}

func (a *API) HandleTaskInputAck(msg model.TaskInputAckPayload) error {
	task, found := a.store.GetTask(msg.TaskID)
	if !found {
		return fmt.Errorf("task not found")
	}
	item, found := a.store.GetChatQueueItem(task.OperatorID, msg.QueueItemID)
	if !found {
		return nil
	}
	if item.Status != model.ChatQueueInserting || item.TaskID != msg.TaskID || item.InjectionVersion != msg.InjectionVersion {
		return fmt.Errorf("stale task input acknowledgement")
	}
	if !msg.Accepted {
		_, _, err := a.store.UpdateChatQueueItem(task.OperatorID, item.ID, item.Version, func(item *model.ChatQueueItem) error {
			item.Status = model.ChatQueueQueued
			item.TaskID = ""
			item.InjectionVersion = 0
			item.Error = strings.TrimSpace(msg.Error)
			if item.Error == "" {
				item.Error = "Agent 未接收插入消息"
			}
			return nil
		})
		return err
	}
	if _, _, err := a.store.DeleteChatQueueItem(task.OperatorID, item.ID, item.Version); err != nil {
		return err
	}
	a.store.AddEvent(task.ID, model.Event{
		TaskID:    task.ID,
		Type:      "input_applied",
		SessionID: task.SessionID,
		Content:   chatQueueInputContent(item.Parts),
		Metadata: map[string]any{
			"queue_item_id":     item.ID,
			"injection_version": msg.InjectionVersion,
		},
		SentAt: time.Now().UTC(),
	})
	return nil
}

func chatQueueInputContent(parts []model.Part) string {
	textParts := make([]string, 0, len(parts))
	files := 0
	for _, part := range parts {
		if text := strings.TrimSpace(part.Text); text != "" {
			textParts = append(textParts, text)
		}
		if part.Type == "file" {
			files++
		}
	}
	content := strings.Join(textParts, "\n")
	if files == 0 {
		return content
	}
	attachmentSummary := fmt.Sprintf("[%d 个附件]", files)
	if content == "" {
		return attachmentSummary
	}
	return content + "\n" + attachmentSummary
}

func (a *API) HandleChatQueueTaskTerminal(task *model.Task, terminalType string) {
	if task == nil || strings.TrimSpace(task.SessionID) == "" {
		return
	}
	snapshot := a.store.ChatQueueSnapshot(task.OperatorID, task.SessionID)
	retryAgentBusy := false
	for _, item := range snapshot.Items {
		if item.TaskID != task.ID {
			continue
		}
		if item.Status == model.ChatQueueInserting {
			_, _, _ = a.store.UpdateChatQueueItem(task.OperatorID, item.ID, item.Version, func(item *model.ChatQueueItem) error {
				item.Status = model.ChatQueueQueued
				item.TaskID = ""
				item.InjectionVersion = 0
				item.Error = "当前任务已结束，消息已保留在队列"
				return nil
			})
			continue
		}
		if terminalType == "task.failed" && strings.HasPrefix(strings.ToLower(strings.TrimSpace(task.Error)), "agent busy:") {
			retries, _ := strconv.Atoi(item.Metadata[chatQueueAgentBusyRetryKey])
			retries++
			_, _, _ = a.store.UpdateChatQueueItem(task.OperatorID, item.ID, item.Version, func(item *model.ChatQueueItem) error {
				item.Status = model.ChatQueueQueued
				item.TaskID = ""
				item.InjectionVersion = 0
				if item.Metadata == nil {
					item.Metadata = map[string]string{}
				}
				item.Metadata[chatQueueAgentBusyRetryKey] = strconv.Itoa(retries)
				if retries <= chatQueueAgentBusyMaxRetries {
					item.Error = "Agent 正在完成上一条消息，稍后自动重试"
				} else {
					item.Error = "Agent 持续繁忙，消息已保留在队列"
				}
				return nil
			})
			retryAgentBusy = retryAgentBusy || retries <= chatQueueAgentBusyMaxRetries
			continue
		}
		_, _, _ = a.store.DeleteChatQueueItem(task.OperatorID, item.ID, item.Version)
	}
	if retryAgentBusy {
		a.scheduleNextChatQueueItem(task.OperatorID, task.SessionID)
		return
	}

	continueQueue := terminalType == "task.completed"
	if terminalType == "task.cancelled" {
		settings, err := a.store.GetDeviceSettings(task.OperatorID, userSettingsAgentID)
		continueQueue = err == nil && strings.EqualFold(strings.TrimSpace(settings[chatQueueContinueAfterCancelSettingKey]), "true")
	}
	if continueQueue {
		a.scheduleNextChatQueueItem(task.OperatorID, task.SessionID)
	}
}

func (a *API) scheduleNextChatQueueItem(operatorID int64, sessionID string) {
	go func() {
		timer := time.NewTimer(chatQueueDispatchAfterTerminal)
		defer timer.Stop()
		<-timer.C
		if _, err := a.DispatchNextChatQueueItem(operatorID, sessionID); err != nil {
			log.Printf("dispatch next chat queue item failed: operator=%d session=%s err=%v", operatorID, sessionID, err)
		}
	}()
}

func (a *API) activeQueueTask(operatorID int64, agentID string) *model.Task {
	for _, status := range activeTaskStatuses {
		tasks, err := a.store.ListTasks(model.TaskFilter{OperatorID: operatorID, AgentID: agentID, Status: status, Limit: 1})
		if err == nil && len(tasks) > 0 {
			return tasks[0]
		}
	}
	return nil
}

func (a *API) hasActiveQueueTask(operatorID int64, agentID string) bool {
	return a.activeQueueTask(operatorID, agentID) != nil
}

func (a *API) DispatchNextChatQueueItem(operatorID int64, sessionID string) (*model.Task, error) {
	if !a.store.TryBeginChatQueueDispatch(operatorID, sessionID) {
		return nil, nil
	}
	defer a.store.EndChatQueueDispatch(operatorID, sessionID)
	session, exists := a.chatQueueSession(operatorID, sessionID)
	if !exists || a.hasActiveQueueTask(operatorID, session.AgentID) {
		return nil, nil
	}
	item, found := a.store.ClaimNextChatQueueItem(operatorID, sessionID)
	if !found {
		return nil, nil
	}
	return a.dispatchChatQueueItem(operatorID, item)
}

func (a *API) dispatchChatQueueItem(operatorID int64, item model.ChatQueueItem) (*model.Task, error) {
	restoreDispatchFailure := func(err error) {
		if err == nil {
			return
		}
		_, _, updateErr := a.store.UpdateChatQueueItem(operatorID, item.ID, item.Version, func(item *model.ChatQueueItem) error {
			item.Status = model.ChatQueueQueued
			item.TaskID = ""
			item.InjectionVersion = 0
			item.Error = err.Error()
			return nil
		})
		if updateErr != nil {
			log.Printf("restore failed chat queue dispatch: item=%s err=%v update=%v", item.ID, err, updateErr)
		}
	}
	agent, online := a.broker.Get(item.AgentID, operatorID)
	if !online {
		err := fmt.Errorf("agent offline")
		restoreDispatchFailure(err)
		return nil, err
	}
	projectRoot := ""
	for _, project := range agent.Projects {
		if project.ProjectID == item.ProjectID {
			projectRoot = project.Root
			break
		}
	}
	if projectRoot == "" {
		err := fmt.Errorf("agent does not own project")
		restoreDispatchFailure(err)
		return nil, err
	}
	baseSystemPrompt, err := a.taskSystemPrompt(operatorID, item.AgentID)
	if err != nil {
		restoreDispatchFailure(err)
		return nil, err
	}
	metadata := normalizeGoalTaskMetadata(item.Metadata)
	delete(metadata, chatQueueAgentBusyRetryKey)
	metadata = enrichTaskMetadata(metadata, agent)
	metadata = a.enrichOrchestrationTaskMetadata(operatorID, metadata)
	memorySnapshot := projectmemory.Snapshot{System: baseSystemPrompt}
	memoryEnabled := a.projectMemoryEnabled
	if scope, scopeErr := a.store.GetProjectScopeForAgent(context.Background(), operatorID, agent.MachineID, item.AgentID); scopeErr == nil && scope != nil {
		memoryEnabled = memoryEnabled && projectmemory.Resolve(context.Background(), a.store, operatorID, agent.MachineID, scope.ID).Enabled
	}
	if memoryEnabled {
		memorySnapshot = projectmemory.Build(context.Background(), a.store, baseSystemPrompt, operatorID,
			agent.MachineID, item.AgentID, projectmemory.QueryFromParts(item.Parts))
	}
	if memorySnapshot.ScopeID != "" {
		if metadata == nil {
			metadata = map[string]string{}
		}
		metadata["project_memory_scope_id"] = memorySnapshot.ScopeID
		metadata["project_memory_revision"] = strconv.FormatInt(memorySnapshot.Revision, 10)
	}
	task := a.store.CreateTask(item.AgentID, agent.MachineID, item.ProjectID, projectRoot, item.SessionID, item.Parts, metadata, operatorID)
	if memoryEnabled {
		projectmemory.RecordUsage(context.Background(), a.store, task.ID, memorySnapshot)
	}
	a.store.SetStatus(task.ID, model.TaskDispatched)
	a.store.AddEvent(task.ID, model.Event{TaskID: task.ID, Type: "dispatched", Content: summarize(item.Parts), SentAt: time.Now().UTC()})
	env := model.Envelope{
		Type:      "task.run",
		RequestID: fmt.Sprintf("req_%s", task.ID),
		SentAt:    time.Now().UTC().Format(time.RFC3339),
		Payload: model.RunPayload{
			TaskID: task.ID, AgentID: item.AgentID, MachineID: agent.MachineID,
			ProjectID: item.ProjectID, SessionID: item.SessionID, System: memorySnapshot.System,
			Parts: item.Parts, Metadata: metadata,
		},
	}
	if err := a.broker.DispatchForOperator(context.Background(), operatorID, item.AgentID, env); err != nil {
		a.store.Fail(task.ID, item.SessionID, err.Error())
		restoreDispatchFailure(err)
		return nil, err
	}
	updated, _, updateErr := a.store.UpdateChatQueueItem(operatorID, item.ID, item.Version, func(item *model.ChatQueueItem) error {
		item.Status = model.ChatQueueDispatched
		item.TaskID = task.ID
		item.Error = ""
		return nil
	})
	if updateErr != nil {
		return task, updateErr
	}
	_ = updated
	if refreshed, ok := a.store.GetTask(task.ID); ok {
		task = refreshed
	}
	return task, nil
}
