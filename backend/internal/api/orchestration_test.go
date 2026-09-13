package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"relay-server/internal/auth"
	"relay-server/internal/broker"
	"relay-server/internal/model"
	"relay-server/internal/store"
)

func TestDeriveSubagentNodesKeepsTerminalResultAndLatestControl(t *testing.T) {
	events := []model.Event{
		{Type: "subagent_started", SessionID: "parent", SentAt: time.Unix(1, 0), Metadata: map[string]any{
			"node_id": "node_a", "subagent_type": "repo-explorer", "resolved_agent": "explore", "title": "Inspect repository", "started_at": int64(100),
		}},
		{Type: "subagent_control_requested", SentAt: time.Unix(2, 0), Metadata: map[string]any{
			"node_id": "node_a", "action": "cancel",
		}},
		{Type: "subagent_result", SessionID: "parent", Content: "evidence", Artifacts: []model.Artifact{{ID: "artifact-report", Filename: "report.txt", RelativePath: ".chatcodex-artifacts/task/subagents/node_a/report.txt"}}, SentAt: time.Unix(3, 0), Metadata: map[string]any{
			"node_id": "node_a", "status": "completed", "completed_at": int64(200), "wake_reason": "batch_complete",
		}},
	}

	nodes := DeriveSubagentNodes(events)
	if len(nodes) != 1 {
		t.Fatalf("expected one node, got %d", len(nodes))
	}
	if got := nodes[0]; got.State != "completed" || got.Output != "evidence" || got.Role != "repo-explorer" || got.LastControl["action"] != "cancel" || len(got.Artifacts) != 1 || got.Artifacts[0].Filename != "report.txt" {
		t.Fatalf("unexpected snapshot: %+v", got)
	}
}

func TestEnrichOrchestrationTaskMetadataInitializesNilMap(t *testing.T) {
	mem := store.NewMemory(nil)
	if err := mem.SetDeviceSetting(1, userSettingsAgentID, orchestrationEnabledSettingKey, "true"); err != nil {
		t.Fatal(err)
	}
	api := &API{store: mem}
	metadata := api.enrichOrchestrationTaskMetadata(1, nil)
	if metadata == nil || metadata[orchestrationEnabledSettingKey] != "true" {
		t.Fatalf("orchestration metadata was not attached to nil map: %+v", metadata)
	}
}

func TestListTaskSubagentsUsesBoundedSubagentWindow(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	api := newTestAPI(mem, broker.New(), authManager, testAPIOptions{})
	operator := model.Operator{ID: 1, Username: "owner"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatal(err)
	}
	task := mem.CreateTask("agent", "machine", "project", "/tmp/project", "parent-session", nil, nil, operator.ID)
	stamp := time.Unix(1, 0).UTC()
	for index := 0; index < 80; index++ {
		mem.AddEvent(task.ID, model.Event{TaskID: task.ID, Type: "delta", Content: "noise", SentAt: stamp})
	}
	mem.AddEvent(task.ID, model.Event{
		TaskID: task.ID, Type: "subagent_started", SentAt: stamp.Add(time.Second),
		Metadata: map[string]any{"node_id": "node-keep", "title": "Inspect", "started_at": int64(10)},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID+"/subagents", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeContext("taskID", task.ID)))
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	api.ListTaskSubagents(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var payload struct {
		Nodes []model.SubagentNodeSnapshot `json:"nodes"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Nodes) != 1 || payload.Nodes[0].NodeID != "node-keep" {
		t.Fatalf("expected bounded subagent tree without delta noise, got %+v", payload.Nodes)
	}
}

func TestListTaskSubagentLogsPaginatesAndIsolatesOperators(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	api := newTestAPI(mem, broker.New(), authManager, testAPIOptions{})
	owner := model.Operator{ID: 1, Username: "owner"}
	other := model.Operator{ID: 2, Username: "other"}
	ownerToken, err := authManager.Issue(owner)
	if err != nil {
		t.Fatalf("issue owner token: %v", err)
	}
	otherToken, err := authManager.Issue(other)
	if err != nil {
		t.Fatalf("issue other token: %v", err)
	}
	task := mem.CreateTask("agent", "machine", "project", "/tmp/project", "parent-session", nil, nil, owner.ID)
	for _, event := range []model.Event{
		{TaskID: task.ID, Type: "subagent_state", Content: "queued", SentAt: time.Unix(1, 0), Metadata: map[string]any{"node_id": "node-a"}},
		{TaskID: task.ID, Type: "subagent_started", Content: "started", SentAt: time.Unix(2, 0), Metadata: map[string]any{"node_id": "node-a"}},
		{TaskID: task.ID, Type: "subagent_result", Content: "result", SentAt: time.Unix(3, 0), Metadata: map[string]any{"node_id": "node-a"}},
		{TaskID: task.ID, Type: "subagent_state", Content: "other node", SentAt: time.Unix(4, 0), Metadata: map[string]any{"node_id": "node-b"}},
	} {
		mem.AddEvent(task.ID, event)
	}

	request := func(token, cursor string, limit int) *http.Request {
		query := "?limit=" + fmt.Sprint(limit)
		if cursor != "" {
			query += "&cursor=" + cursor
		}
		req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID+"/subagents/node-a/logs"+query, nil)
		ctx := routeContext("taskID", task.ID)
		ctx.URLParams.Add("nodeID", "node-a")
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, ctx))
		req.Header.Set("Authorization", "Bearer "+token)
		return req
	}

	denied := httptest.NewRecorder()
	api.ListTaskSubagentLogs(denied, request(otherToken, "", 1))
	if denied.Code != http.StatusNotFound {
		t.Fatalf("expected other operator to be isolated, got %d: %s", denied.Code, denied.Body.String())
	}

	first := httptest.NewRecorder()
	api.ListTaskSubagentLogs(first, request(ownerToken, "", 2))
	if first.Code != http.StatusOK {
		t.Fatalf("expected first page, got %d: %s", first.Code, first.Body.String())
	}
	var firstPage struct {
		Events     []model.Event `json:"events"`
		NextCursor string        `json:"next_cursor"`
		HasMore    bool          `json:"has_more"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &firstPage); err != nil {
		t.Fatalf("decode first page: %v", err)
	}
	if len(firstPage.Events) != 2 || firstPage.Events[0].Content != "queued" || firstPage.NextCursor == "" || !firstPage.HasMore {
		t.Fatalf("unexpected first page: %+v", firstPage)
	}

	second := httptest.NewRecorder()
	api.ListTaskSubagentLogs(second, request(ownerToken, firstPage.NextCursor, 2))
	var secondPage struct {
		Events     []model.Event `json:"events"`
		NextCursor string        `json:"next_cursor"`
		HasMore    bool          `json:"has_more"`
	}
	if err := json.Unmarshal(second.Body.Bytes(), &secondPage); err != nil {
		t.Fatalf("decode second page: %v", err)
	}
	if second.Code != http.StatusOK || len(secondPage.Events) != 1 || secondPage.Events[0].Content != "result" || secondPage.HasMore {
		t.Fatalf("unexpected second page status=%d body=%+v", second.Code, secondPage)
	}
}

func TestDeriveSubagentNodesOrdersByStartedAtAndNormalizesUnknownResult(t *testing.T) {
	nodes := DeriveSubagentNodes([]model.Event{
		{Type: "subagent_started", Metadata: map[string]any{"node_id": "later", "started_at": int64(20)}},
		{Type: "subagent_started", Metadata: map[string]any{"node_id": "first", "started_at": int64(10)}},
		{Type: "subagent_result", Metadata: map[string]any{"node_id": "later", "status": "unexpected"}},
	})
	if len(nodes) != 2 || nodes[0].NodeID != "first" || nodes[1].State != "unknown" {
		t.Fatalf("unexpected ordered snapshot: %+v", nodes)
	}
}

func TestDeriveSubagentNodesSkipsEventsWithoutANodeID(t *testing.T) {
	nodes := DeriveSubagentNodes([]model.Event{
		{Type: "subagent_state", Metadata: map[string]any{"node_id": nil}},
		{Type: "subagent_started", Metadata: map[string]any{}},
		{Type: "subagent_started", Metadata: map[string]any{"node_id": "real-node"}},
	})
	if len(nodes) != 1 || nodes[0].NodeID != "real-node" {
		t.Fatalf("expected only real subagent nodes, got %+v", nodes)
	}
}

func TestValidSubagentControlRequiresActionPayload(t *testing.T) {
	priority := 60
	if !validSubagentControl(subagentControlRequest{Action: "cancel"}) || !validSubagentControl(subagentControlRequest{Action: "pause"}) || !validSubagentControl(subagentControlRequest{Action: "resume"}) || !validSubagentControl(subagentControlRequest{Action: "priority", Priority: &priority}) {
		t.Fatal("expected valid controls")
	}
	if validSubagentControl(subagentControlRequest{Action: "instruction"}) || validSubagentControl(subagentControlRequest{Action: "model"}) {
		t.Fatal("expected incomplete and unsupported controls to be rejected")
	}
}

func TestDeriveSubagentNodesRetainsDAGStateAcrossReconnect(t *testing.T) {
	priority := 75
	nodes := DeriveSubagentNodes([]model.Event{
		{Type: "subagent_state", SessionID: "parent", Error: "dependency failed", Metadata: map[string]any{
			"node_id": "verify", "plan_id": "plan_1", "child_session_id": "child_1", "subagent_type": "test-runner",
			"title": "Run verification", "state": "blocked", "attempt": 2, "depends_on": []string{"build"},
			"blocked_by": []string{"build"}, "priority": priority, "model": map[string]any{"providerID": "openai", "modelID": "gpt-5"},
			"pending_instructions": []string{"Focus on regression"}, "started_at": int64(12), "updated_at": int64(18),
		}},
	})
	if len(nodes) != 1 {
		t.Fatalf("expected one node, got %d", len(nodes))
	}
	node := nodes[0]
	if node.PlanID != "plan_1" || node.ChildSessionID != "child_1" || node.State != "blocked" || node.Attempt != 2 || len(node.DependsOn) != 1 || len(node.BlockedBy) != 1 || node.Priority == nil || *node.Priority != 75 || node.Error != "dependency failed" {
		t.Fatalf("unexpected durable DAG snapshot: %+v", node)
	}
}

func TestTaskSubagentNodesMergesStoredRecoverableNodes(t *testing.T) {
	task := &model.Task{
		ID: "task-meta",
		Metadata: map[string]string{
			SubagentNodesMetadataKey: `[{"node_id":"kept","plan_id":"plan-kept","prompt":"continue","state":"running","started_at":1}]`,
		},
	}
	events := []model.Event{{
		Type: "subagent_state",
		Metadata: map[string]any{
			"node_id": "fresh", "plan_id": "plan-fresh", "prompt": "inspect", "state": "queued", "started_at": int64(2),
		},
	}}
	nodes := TaskSubagentNodes(task, events)
	if len(nodes) != 2 || nodes[0].NodeID != "kept" || nodes[1].NodeID != "fresh" {
		t.Fatalf("expected stored and live nodes to merge, got %+v", nodes)
	}
}

func TestPersistRecoverableSubagentNodesWritesTaskMetadata(t *testing.T) {
	mem := store.NewMemory(nil)
	task := mem.CreateTask("agent", "machine", "project", "/tmp", "session", nil, nil, 1)
	mem.AddEvent(task.ID, model.Event{
		TaskID: task.ID, Type: "subagent_state", SessionID: task.SessionID, SentAt: time.Now().UTC(),
		Metadata: map[string]any{
			"node_id": "inspect", "plan_id": "plan-1", "prompt": "inspect repository", "state": "running",
			"child_session_id": "ses_child", "attempt": 1,
		},
	})
	mem.AddEvent(task.ID, model.Event{
		TaskID: task.ID, Type: "subagent_result", SessionID: task.SessionID, Content: "huge-output", SentAt: time.Now().UTC(),
		Metadata: map[string]any{
			"node_id": "done", "plan_id": "plan-1", "prompt": "finished work", "status": "completed",
		},
	})
	PersistRecoverableSubagentNodes(mem, task.ID)
	updated, ok := mem.GetTask(task.ID)
	if !ok {
		t.Fatal("task missing after metadata persist")
	}
	nodes := ParseSubagentNodesMetadata(updated.Metadata)
	if len(nodes) != 1 || nodes[0].NodeID != "inspect" || nodes[0].Prompt != "inspect repository" || nodes[0].Output != "" {
		t.Fatalf("expected only compact recoverable nodes in metadata, got %+v", nodes)
	}
}
