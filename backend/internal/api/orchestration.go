package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"relay-server/internal/model"
	"relay-server/internal/store"
)

const SubagentNodesMetadataKey = "subagent_nodes_json"

type subagentControlRequest struct {
	Action      string         `json:"action"`
	Instruction string         `json:"instruction,omitempty"`
	Priority    *int           `json:"priority,omitempty"`
	Model       map[string]any `json:"model,omitempty"`
}

func (a *API) ListTaskSubagents(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	taskID := chi.URLParam(r, "taskID")
	task, found := a.store.GetTask(taskID)
	if !found || task.OperatorID != operator.ID {
		write(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	write(w, http.StatusOK, map[string]any{"task_id": taskID, "nodes": TaskSubagentNodes(task, a.store.SubagentEvents(taskID, 200))})
}

func (a *API) ListTaskSubagentLogs(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	taskID := chi.URLParam(r, "taskID")
	nodeID := chi.URLParam(r, "nodeID")
	task, found := a.store.GetTask(taskID)
	if !found || task.OperatorID != operator.ID {
		write(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit < 1 || limit > 200 {
		limit = 50
	}
	var cursor *store.EventCursor
	if raw := strings.TrimSpace(r.URL.Query().Get("cursor")); raw != "" {
		decoded, err := decodeEventCursor(raw)
		if err != nil {
			write(w, http.StatusBadRequest, map[string]string{"error": "invalid cursor"})
			return
		}
		cursor = decoded
	}
	page, err := a.store.ListEventPage(taskID, cursor, limit, []string{"subagent_state", "subagent_started", "subagent_result", "subagent_control_requested", "subagent_control_applied"}, nodeID)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": "load subagent logs failed"})
		return
	}
	nextCursor := ""
	if page.NextCursor != nil {
		nextCursor = encodeEventCursor(*page.NextCursor)
	}
	write(w, http.StatusOK, map[string]any{"task_id": taskID, "node_id": nodeID, "events": page.Items, "next_cursor": nextCursor, "has_more": page.HasMore})
}

func encodeEventCursor(cursor store.EventCursor) string {
	raw, _ := json.Marshal(cursor)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeEventCursor(raw string) (*store.EventCursor, error) {
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, err
	}
	var cursor store.EventCursor
	if err := json.Unmarshal(data, &cursor); err != nil || cursor.ID < 0 || cursor.SentAt.IsZero() {
		return nil, fmt.Errorf("invalid event cursor")
	}
	return &cursor, nil
}

func (a *API) ControlTaskSubagent(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	taskID := chi.URLParam(r, "taskID")
	nodeID := chi.URLParam(r, "nodeID")
	task, found := a.store.GetTask(taskID)
	if !found || task.OperatorID != operator.ID {
		write(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}
	var req subagentControlRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	req.Action = strings.TrimSpace(req.Action)
	if !validSubagentControl(req) {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid subagent control"})
		return
	}
	nodes := TaskSubagentNodes(task, a.store.SubagentEvents(taskID, 200))
	var node *model.SubagentNodeSnapshot
	for i := range nodes {
		if nodes[i].NodeID == nodeID {
			node = &nodes[i]
			break
		}
	}
	if node == nil {
		write(w, http.StatusNotFound, map[string]string{"error": "subagent not found"})
		return
	}
	if isSubagentTerminal(node.State) {
		write(w, http.StatusConflict, map[string]string{"error": "subagent is already finished"})
		return
	}
	payload := model.SubagentControlPayload{
		TaskID: taskID, NodeID: nodeID, Action: req.Action, Instruction: req.Instruction, Priority: req.Priority, Model: req.Model,
	}
	if err := a.broker.DispatchForOperator(context.Background(), operator.ID, task.AgentID, model.Envelope{
		Type: "task.subagent_control", RequestID: fmt.Sprintf("req_subagent_%s_%d", nodeID, time.Now().UnixNano()),
		SentAt: time.Now().UTC().Format(time.RFC3339), Payload: payload,
	}); err != nil {
		write(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	a.store.AddEvent(taskID, model.Event{TaskID: taskID, Type: "subagent_control_requested", SentAt: time.Now().UTC(), Metadata: map[string]any{
		"node_id": nodeID, "action": req.Action, "instruction": req.Instruction, "priority": req.Priority, "model": req.Model,
	}})
	PersistRecoverableSubagentNodes(a.store, taskID)
	write(w, http.StatusAccepted, payload)
}

func validSubagentControl(req subagentControlRequest) bool {
	switch req.Action {
	case "cancel", "pause", "resume":
		return true
	case "instruction":
		return strings.TrimSpace(req.Instruction) != ""
	case "priority":
		return req.Priority != nil && *req.Priority >= 0 && *req.Priority <= 100
	case "model":
		providerID, providerOK := req.Model["providerID"].(string)
		modelID, modelOK := req.Model["modelID"].(string)
		return providerOK && modelOK && strings.TrimSpace(providerID) != "" && strings.TrimSpace(modelID) != ""
	}
	return false
}

func DeriveSubagentNodes(events []model.Event) []model.SubagentNodeSnapshot {
	nodes := map[string]model.SubagentNodeSnapshot{}
	for _, event := range events {
		metadata := eventMetadata(event.Metadata)
		// A missing node_id used to become "<nil>" through fmt.Sprint, which
		// produced a phantom unknown node in the client task tree.
		nodeID := metadataString(metadata, "node_id")
		if nodeID == "" {
			continue
		}
		node := nodes[nodeID]
		node.NodeID = nodeID
		node.PlanID = firstNonEmpty(node.PlanID, metadataString(metadata, "plan_id"))
		node.ChildSessionID = firstNonEmpty(node.ChildSessionID, metadataString(metadata, "child_session_id"))
		node.SessionID = firstNonEmpty(node.SessionID, event.SessionID)
		switch event.Type {
		case "subagent_state":
			node.Role = firstNonEmpty(node.Role, metadataString(metadata, "subagent_type"))
			node.Title = firstNonEmpty(node.Title, metadataString(metadata, "title"))
			node.Prompt = firstNonEmpty(node.Prompt, metadataString(metadata, "prompt"))
			node.State = normalizedSnapshotState(metadataString(metadata, "state"))
			node.Attempt = max(node.Attempt, metadataInt(metadata, "attempt"))
			node.DependsOn = metadataStrings(metadata, "depends_on", node.DependsOn)
			node.BlockedBy = metadataStrings(metadata, "blocked_by", node.BlockedBy)
			if priority, ok := metadataIntPointer(metadata, "priority"); ok {
				node.Priority = priority
			}
			if modelValue, ok := metadata["model"].(map[string]any); ok {
				node.Model = modelValue
			}
			node.PendingInstructions = metadataStrings(metadata, "pending_instructions", node.PendingInstructions)
			if startedAt := metadataInt64(metadata, "started_at"); startedAt > 0 {
				node.StartedAt = startedAt
			}
			if completedAt := metadataInt64(metadata, "completed_at"); completedAt > 0 {
				node.CompletedAt = completedAt
			}
			node.Error = firstNonEmpty(event.Error, node.Error)
		case "subagent_started":
			node.Role = firstNonEmpty(node.Role, metadataString(metadata, "subagent_type"))
			node.ResolvedAgent = firstNonEmpty(node.ResolvedAgent, metadataString(metadata, "resolved_agent"))
			node.Title = firstNonEmpty(node.Title, metadataString(metadata, "title"))
			node.Background, _ = metadata["background"].(bool)
			node.StartedAt = metadataInt64(metadata, "started_at")
			node.State = "running"
			node.Attempt = max(node.Attempt, metadataInt(metadata, "attempt"))
		case "subagent_result":
			node.Role = firstNonEmpty(node.Role, metadataString(metadata, "subagent_type"))
			node.ResolvedAgent = firstNonEmpty(node.ResolvedAgent, metadataString(metadata, "resolved_agent"))
			node.Title = firstNonEmpty(node.Title, metadataString(metadata, "title"))
			node.State = normalizedSubagentState(metadataString(metadata, "status"))
			node.CompletedAt = metadataInt64(metadata, "completed_at")
			node.Output = event.Content
			node.Error = event.Error
			node.Artifacts = event.Artifacts
			node.WakeReason = metadataString(metadata, "wake_reason")
			node.Attempt = max(node.Attempt, metadataInt(metadata, "attempt"))
		case "subagent_control_requested", "subagent_control_applied":
			node.LastControl = metadata
		}
		nodes[nodeID] = node
	}
	out := make([]model.SubagentNodeSnapshot, 0, len(nodes))
	for _, node := range nodes {
		if node.State == "" {
			node.State = "unknown"
		}
		out = append(out, node)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].StartedAt == out[j].StartedAt {
			return out[i].NodeID < out[j].NodeID
		}
		return out[i].StartedAt < out[j].StartedAt
	})
	return out
}

func eventMetadata(raw any) map[string]any {
	if metadata, ok := raw.(map[string]any); ok {
		return metadata
	}
	return map[string]any{}
}

func metadataString(metadata map[string]any, key string) string {
	value, ok := metadata[key].(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(value)
}

func metadataInt64(metadata map[string]any, key string) int64 {
	switch value := metadata[key].(type) {
	case int64:
		return value
	case int:
		return int64(value)
	case float64:
		return int64(value)
	case json.Number:
		parsed, _ := value.Int64()
		return parsed
	}
	return 0
}

func metadataInt(metadata map[string]any, key string) int {
	return int(metadataInt64(metadata, key))
}

func metadataIntPointer(metadata map[string]any, key string) (*int, bool) {
	if _, ok := metadata[key]; !ok {
		return nil, false
	}
	value := metadataInt(metadata, key)
	return &value, true
}

func metadataStrings(metadata map[string]any, key string, fallback []string) []string {
	value, ok := metadata[key]
	if !ok {
		return fallback
	}
	values, ok := value.([]any)
	if !ok {
		if strings, ok := value.([]string); ok {
			return strings
		}
		return fallback
	}
	result := make([]string, 0, len(values))
	for _, item := range values {
		if text := strings.TrimSpace(fmt.Sprint(item)); text != "" {
			result = append(result, text)
		}
	}
	return result
}

func normalizedSubagentState(value string) string {
	switch value {
	case "completed", "failed", "cancelled", "timed_out":
		return value
	}
	return "unknown"
}

func normalizedSnapshotState(value string) string {
	switch value {
	case "planned", "queued", "running", "paused", "completed", "failed", "cancelled", "timed_out", "unknown", "recovering", "blocked":
		return value
	}
	return "unknown"
}

func isSubagentTerminal(state string) bool {
	return state == "completed" || state == "failed" || state == "cancelled" || state == "timed_out" || state == "blocked"
}

func ParseSubagentNodesMetadata(metadata map[string]string) []model.SubagentNodeSnapshot {
	if metadata == nil {
		return nil
	}
	raw := strings.TrimSpace(metadata[SubagentNodesMetadataKey])
	if raw == "" {
		return nil
	}
	var nodes []model.SubagentNodeSnapshot
	if err := json.Unmarshal([]byte(raw), &nodes); err != nil {
		return nil
	}
	return nodes
}

func RecoverableSubagentNode(node model.SubagentNodeSnapshot) bool {
	if strings.TrimSpace(node.PlanID) == "" || strings.TrimSpace(node.Prompt) == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(node.State)) {
	case "planned", "queued", "running", "paused", "unknown", "recovering":
		return true
	default:
		return false
	}
}

func RecoverableSubagentNodes(nodes []model.SubagentNodeSnapshot) []model.SubagentNodeSnapshot {
	out := make([]model.SubagentNodeSnapshot, 0, len(nodes))
	for _, node := range nodes {
		if !RecoverableSubagentNode(node) {
			continue
		}
		node.Output = ""
		node.Artifacts = nil
		out = append(out, node)
	}
	return out
}

func MergeSubagentNodeSnapshots(base, overlay []model.SubagentNodeSnapshot) []model.SubagentNodeSnapshot {
	nodes := make(map[string]model.SubagentNodeSnapshot, len(base)+len(overlay))
	order := make([]string, 0, len(base)+len(overlay))
	add := func(node model.SubagentNodeSnapshot, replace bool) {
		id := strings.TrimSpace(node.NodeID)
		if id == "" {
			return
		}
		if _, exists := nodes[id]; !exists {
			order = append(order, id)
			nodes[id] = node
			return
		}
		if replace {
			nodes[id] = node
		}
	}
	for _, node := range base {
		add(node, false)
	}
	for _, node := range overlay {
		add(node, true)
	}
	out := make([]model.SubagentNodeSnapshot, 0, len(order))
	for _, id := range order {
		out = append(out, nodes[id])
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].StartedAt == out[j].StartedAt {
			return out[i].NodeID < out[j].NodeID
		}
		return out[i].StartedAt < out[j].StartedAt
	})
	return out
}

func TaskSubagentNodes(task *model.Task, events []model.Event) []model.SubagentNodeSnapshot {
	var stored []model.SubagentNodeSnapshot
	if task != nil {
		stored = ParseSubagentNodesMetadata(task.Metadata)
	}
	return MergeSubagentNodeSnapshots(stored, DeriveSubagentNodes(events))
}

func PersistRecoverableSubagentNodes(st *store.Memory, taskID string) {
	if st == nil || strings.TrimSpace(taskID) == "" {
		return
	}
	task, ok := st.GetTask(taskID)
	if !ok {
		return
	}
	nodes := RecoverableSubagentNodes(TaskSubagentNodes(task, st.SubagentEvents(taskID, 200)))
	if len(nodes) == 0 {
		st.UpdateTaskMetadata(taskID, map[string]string{SubagentNodesMetadataKey: ""})
		return
	}
	raw, err := json.Marshal(nodes)
	if err != nil {
		log.Printf("encode recoverable subagent nodes failed: %v", err)
		return
	}
	st.UpdateTaskMetadata(taskID, map[string]string{SubagentNodesMetadataKey: string(raw)})
}
