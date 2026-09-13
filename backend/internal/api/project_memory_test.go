package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"relay-server/internal/auth"
	"relay-server/internal/broker"
	"relay-server/internal/model"
	"relay-server/internal/store"
)

func projectMemoryRequest(method, target, body, token, machineID, scopeID string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("machineID", machineID)
	rctx.URLParams.Add("scopeID", scopeID)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func projectMemoryParam(req *http.Request, key, value string) *http.Request {
	rctx := chi.RouteContext(req.Context())
	rctx.URLParams.Add(key, value)
	return req
}

func seedAPIMemory(t *testing.T, memory *store.Memory) {
	t.Helper()
	ctx := context.Background()
	for _, scope := range []model.ProjectScope{
		{ID: "scope-a", OperatorID: 1, MachineID: "machine-a", DisplayName: "A", Status: model.ProjectScopeActive, Revision: 1},
		{ID: "scope-b", OperatorID: 2, MachineID: "machine-b", DisplayName: "B", Status: model.ProjectScopeActive, Revision: 1},
	} {
		if err := memory.UpsertProjectScope(ctx, scope); err != nil {
			t.Fatal(err)
		}
	}
	if err := memory.UpsertProjectMemory(ctx, model.ProjectMemory{
		ID: "memory-a", LogicalID: "logical-a", ScopeID: "scope-a", Kind: model.ProjectMemoryDecision,
		SubjectKey: "decision", Statement: "Use MySQL", Status: model.ProjectMemoryActive, Version: 1,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestProjectMemoryAPIEnforcesScopeAndObjectOwnership(t *testing.T) {
	memory := store.NewMemory(nil)
	seedAPIMemory(t, memory)
	authManager := auth.NewManager(time.Hour)
	handler := newTestAPI(memory, broker.New(), authManager, testAPIOptions{})
	token, err := authManager.Issue(model.Operator{ID: 2, Username: "operator-b"})
	if err != nil {
		t.Fatal(err)
	}

	listReq := projectMemoryRequest(http.MethodGet, "/api/devices/machine-a/projects/scope-a/memories", "", token, "machine-a", "scope-a")
	listRR := httptest.NewRecorder()
	handler.ListProjectMemories(listRR, listReq)
	if listRR.Code != http.StatusNotFound {
		t.Fatalf("cross-operator scope returned %d: %s", listRR.Code, listRR.Body.String())
	}

	getReq := projectMemoryRequest(http.MethodGet, "/api/devices/machine-b/projects/scope-b/memories/memory-a", "", token, "machine-b", "scope-b")
	getReq = projectMemoryParam(getReq, "memoryID", "memory-a")
	getRR := httptest.NewRecorder()
	handler.GetProjectMemory(getRR, getReq)
	if getRR.Code != http.StatusNotFound {
		t.Fatalf("cross-scope object enumeration returned %d: %s", getRR.Code, getRR.Body.String())
	}
}

func TestProjectMemoryAPIRejectsStaleEditAndReturnsHistory(t *testing.T) {
	memory := store.NewMemory(nil)
	seedAPIMemory(t, memory)
	authManager := auth.NewManager(time.Hour)
	handler := newTestAPI(memory, broker.New(), authManager, testAPIOptions{})
	token, _ := authManager.Issue(model.Operator{ID: 1, Username: "operator-a"})

	patchReq := projectMemoryRequest(http.MethodPatch, "/api/devices/machine-a/projects/scope-a/memories/memory-a", `{"version":9,"statement":"new"}`, token, "machine-a", "scope-a")
	patchReq = projectMemoryParam(patchReq, "memoryID", "memory-a")
	patchRR := httptest.NewRecorder()
	handler.UpdateProjectMemory(patchRR, patchReq)
	if patchRR.Code != http.StatusConflict {
		t.Fatalf("stale edit returned %d: %s", patchRR.Code, patchRR.Body.String())
	}

	getReq := projectMemoryRequest(http.MethodGet, "/api/devices/machine-a/projects/scope-a/memories/memory-a", "", token, "machine-a", "scope-a")
	getReq = projectMemoryParam(getReq, "memoryID", "memory-a")
	getRR := httptest.NewRecorder()
	handler.GetProjectMemory(getRR, getReq)
	if getRR.Code != http.StatusOK {
		t.Fatalf("detail returned %d: %s", getRR.Code, getRR.Body.String())
	}
	var detail struct {
		Memory  model.ProjectMemory   `json:"memory"`
		History []model.ProjectMemory `json:"history"`
	}
	if err := json.Unmarshal(getRR.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Memory.ID != "memory-a" || len(detail.History) != 1 {
		t.Fatalf("unexpected detail history: %+v", detail)
	}
}

func TestProjectMemoryAPIRejectsStaleResolveAndDeleteThenRestoresTombstone(t *testing.T) {
	memory := store.NewMemory(nil)
	seedAPIMemory(t, memory)
	authManager := auth.NewManager(time.Hour)
	handler := newTestAPI(memory, broker.New(), authManager, testAPIOptions{})
	token, _ := authManager.Issue(model.Operator{ID: 1, Username: "operator-a"})

	request := func(method, suffix, body string) *http.Request {
		req := projectMemoryRequest(method, "/memories/memory-a"+suffix, body, token, "machine-a", "scope-a")
		return projectMemoryParam(req, "memoryID", "memory-a")
	}
	resolveRR := httptest.NewRecorder()
	handler.ResolveProjectMemory(resolveRR, request(http.MethodPost, "/resolve", `{"version":9,"status":"archived"}`))
	if resolveRR.Code != http.StatusConflict {
		t.Fatalf("stale resolve returned %d: %s", resolveRR.Code, resolveRR.Body.String())
	}
	deleteRR := httptest.NewRecorder()
	handler.DeleteProjectMemory(deleteRR, request(http.MethodDelete, "", `{"version":9}`))
	if deleteRR.Code != http.StatusConflict {
		t.Fatalf("stale delete returned %d: %s", deleteRR.Code, deleteRR.Body.String())
	}
	unchanged, _ := memory.GetProjectMemory(context.Background(), 1, "scope-a", "memory-a")
	if unchanged == nil || unchanged.Status != model.ProjectMemoryActive {
		t.Fatalf("stale mutations changed memory: %+v", unchanged)
	}

	deleteRR = httptest.NewRecorder()
	handler.DeleteProjectMemory(deleteRR, request(http.MethodDelete, "", `{"version":1}`))
	if deleteRR.Code != http.StatusOK {
		t.Fatalf("delete returned %d: %s", deleteRR.Code, deleteRR.Body.String())
	}
	listRR := httptest.NewRecorder()
	handler.ListProjectMemories(listRR, projectMemoryRequest(http.MethodGet, "/memories?status=deleted", "", token, "machine-a", "scope-a"))
	if listRR.Code != http.StatusOK {
		t.Fatalf("deleted list returned %d: %s", listRR.Code, listRR.Body.String())
	}
	var deleted []model.ProjectMemory
	if err := json.Unmarshal(listRR.Body.Bytes(), &deleted); err != nil || len(deleted) != 1 || deleted[0].Status != model.ProjectMemoryDeleted {
		t.Fatalf("deleted memory is not browsable: items=%+v err=%v", deleted, err)
	}

	restoreRR := httptest.NewRecorder()
	handler.ResolveProjectMemory(restoreRR, request(http.MethodPost, "/resolve", `{"version":1,"status":"active"}`))
	if restoreRR.Code != http.StatusOK {
		t.Fatalf("restore returned %d: %s", restoreRR.Code, restoreRR.Body.String())
	}
	active, err := memory.ListProjectMemories(context.Background(), model.ProjectMemoryFilter{
		OperatorID: 1, MachineID: "machine-a", ScopeID: "scope-a", Status: model.ProjectMemoryActive,
	})
	if err != nil || len(active) != 1 || active[0].Version != 2 || active[0].SupersedesID != "memory-a" {
		t.Fatalf("restore did not create an immutable successor: items=%+v err=%v", active, err)
	}
}

func TestProjectMemoryAPIValidatesCompositeCursorAndFactFilter(t *testing.T) {
	memory := store.NewMemory(nil)
	seedAPIMemory(t, memory)
	for _, item := range []model.ProjectMemory{
		{ID: "verified", LogicalID: "verified", ScopeID: "scope-a", Kind: model.ProjectMemoryVerifiedFact, SubjectKey: "v", Statement: "verified", Status: model.ProjectMemoryActive},
		{ID: "inferred", LogicalID: "inferred", ScopeID: "scope-a", Kind: model.ProjectMemoryInferredFact, SubjectKey: "i", Statement: "inferred", Status: model.ProjectMemoryActive},
	} {
		if err := memory.UpsertProjectMemory(context.Background(), item); err != nil {
			t.Fatal(err)
		}
	}
	authManager := auth.NewManager(time.Hour)
	handler := newTestAPI(memory, broker.New(), authManager, testAPIOptions{})
	token, _ := authManager.Issue(model.Operator{ID: 1, Username: "operator-a"})

	factsRR := httptest.NewRecorder()
	handler.ListProjectMemories(factsRR, projectMemoryRequest(http.MethodGet, "/memories?kind=fact", "", token, "machine-a", "scope-a"))
	var facts []model.ProjectMemory
	if factsRR.Code != http.StatusOK || json.Unmarshal(factsRR.Body.Bytes(), &facts) != nil || len(facts) != 2 {
		t.Fatalf("fact union filter failed: status=%d body=%s", factsRR.Code, factsRR.Body.String())
	}
	for _, target := range []string{
		"/memories?before_updated_at=2026-08-05T00:00:00Z",
		"/memories?before_id=verified&before_updated_at=bad-time",
	} {
		rr := httptest.NewRecorder()
		handler.ListProjectMemories(rr, projectMemoryRequest(http.MethodGet, target, "", token, "machine-a", "scope-a"))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("invalid cursor %q returned %d: %s", target, rr.Code, rr.Body.String())
		}
	}
}

func TestProjectMemoryAPIAutomaticJobEventsRemainHidden(t *testing.T) {
	memory := store.NewMemory(nil)
	seedAPIMemory(t, memory)
	now := time.Now().UTC()
	if err := memory.CreateProjectMemoryJob(context.Background(), model.ProjectMemoryJob{
		ID: "automatic-job", OperatorID: 1, MachineID: "machine-a", ScopeID: "scope-a",
		Trigger: model.ProjectMemoryTriggerTaskComplete, Status: model.ProjectMemoryJobFailed,
		ManualVisible: false, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	authManager := auth.NewManager(time.Hour)
	handler := newTestAPI(memory, broker.New(), authManager, testAPIOptions{})
	token, _ := authManager.Issue(model.Operator{ID: 1, Username: "operator-a"})
	req := projectMemoryRequest(http.MethodGet, "/events", "", token, "machine-a", "scope-a")
	req = projectMemoryParam(req, "jobID", "automatic-job")
	rr := httptest.NewRecorder()
	handler.ProjectMemoryJobEvents(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("hidden automatic events returned %d: %s", rr.Code, rr.Body.String())
	}
}

func TestProjectMemoryIdentityCorrectionCreatesForkScope(t *testing.T) {
	memory := store.NewMemory(nil)
	seedAPIMemory(t, memory)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()
	b.Add(nil, model.HelloPayload{
		MachineID: "machine-a", DeviceID: "launcher:machine-a", Kind: "launcher",
	}, 1)
	b.Add(nil, model.HelloPayload{
		MachineID: "machine-a", AgentID: "agent-a",
		Projects: []model.HelloProject{{ProjectID: "A", Root: "/project/a", ScopeID: "scope-a"}},
	}, 1)
	handler := newTestAPI(memory, b, authManager, testAPIOptions{
		launcherRequest: func(_ context.Context, operatorID int64, launcherID, _, eventType string, payload any) (model.DeviceLauncherResultPayload, error) {
			if operatorID != 1 || launcherID != "launcher:machine-a" || eventType != "device.launcher.project_identity_correct" {
				t.Fatalf("unexpected launcher request operator=%d launcher=%s type=%s", operatorID, launcherID, eventType)
			}
			input, ok := payload.(model.ProjectIdentityCorrectionInput)
			if !ok || input.AgentID != "agent-a" || input.Action != "treat_as_new" {
				t.Fatalf("unexpected identity payload: %#v", payload)
			}
			return model.DeviceLauncherResultPayload{Success: true, Identity: &model.ProjectIdentityCorrectionResult{
				ProjectScopeID: "scope-new", InstanceNonce: "nonce-new",
				LineageProjectScopeID: "scope-a", Root: "/project/a", MarkerWritable: true,
			}}, nil
		},
	})
	token, _ := authManager.Issue(model.Operator{ID: 1, Username: "operator-a"})
	req := projectMemoryRequest(http.MethodPost, "/identity", `{"action":"treat_as_new"}`, token, "machine-a", "scope-a")
	rr := httptest.NewRecorder()
	handler.CorrectProjectIdentity(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("identity correction returned %d: %s", rr.Code, rr.Body.String())
	}
	created, err := memory.GetProjectScope(context.Background(), 1, "machine-a", "scope-new")
	if err != nil || created == nil || created.LineageScopeID != "scope-a" {
		t.Fatalf("corrected scope not created: scope=%+v err=%v", created, err)
	}
}
