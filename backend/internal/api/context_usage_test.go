package api

import (
	"context"
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"net/http"
	"net/http/httptest"
	"relay-server/internal/auth"
	"relay-server/internal/broker"
	"relay-server/internal/model"
	"relay-server/internal/store"
	"testing"
	"time"
)

func TestAgentHistoryRestoresArchivedUsageWithoutAgentUpgrade(t *testing.T) {
	archive := &contextUsageArchive{}
	mem := store.NewMemory(archive)
	manager := auth.NewManager(time.Hour)
	operator := model.Operator{ID: 1, Username: "usage-reader"}
	token, err := manager.Issue(operator)
	if err != nil {
		t.Fatal(err)
	}
	task := mem.CreateTask("agent", "machine", "project", "/tmp", "session", nil, nil, operator.ID)
	stamp := time.Now().UTC()
	mem.AddEvent(task.ID, model.Event{TaskID: task.ID, Type: "completed", SentAt: stamp, Metadata: map[string]any{"usage": map[string]any{"context_tokens": 24000, "context_limit": 128000, "model_id": "model-a"}}})
	api := newTestAPI(mem, broker.New(), manager, testAPIOptions{})
	api.SetSessionHistoryRequest(func(context.Context, int64, string, string, model.SessionHistoryRequestPayload) (model.SessionHistoryResultPayload, error) {
		return model.SessionHistoryResultPayload{Source: "agent_local", Events: []model.Event{{TaskID: task.ID, Type: "completed", Content: "restored text", SentAt: stamp}}, Complete: true}, nil
	})
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/"+task.ID+"/agent-history", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeContext("taskID", task.ID)))
	req.Header.Set("Authorization", "Bearer "+token)
	result := httptest.NewRecorder()
	api.TaskAgentHistory(result, req)
	if result.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", result.Code, result.Body.String())
	}
	var payload struct {
		Events []model.Event `json:"events"`
	}
	if err := json.Unmarshal(result.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Events) != 2 || payload.Events[0].Content != "restored text" {
		t.Fatalf("events=%+v", payload.Events)
	}
	usage := payload.Events[1].Metadata.(map[string]any)
	if usage["source"] != "llm_usage" || usage["context_tokens"] != float64(24000) {
		t.Fatalf("usage=%+v", usage)
	}
}

// Model a durable archive: AddEvent evicts terminal events from live memory.
type contextUsageArchive struct {
	statistics []model.Event
	store.TaskArchive
	task  *model.Task
	event model.Event
}

func (a *contextUsageArchive) UpsertTask(task *model.Task) error {
	copy := *task
	a.task = &copy
	return nil
}
func (a *contextUsageArchive) UpsertSession(*model.Session) error  { return nil }
func (a *contextUsageArchive) AppendEvent(event model.Event) error { a.event = event; return nil }
func (a *contextUsageArchive) GetTask(string) (*model.Task, error) { return a.task, nil }
func (a *contextUsageArchive) LatestTerminalEvent(string) (model.Event, bool, error) {
	return a.event, a.event.Type != "", nil
}
func (a *contextUsageArchive) ListEventPage(string, *store.EventCursor, int, []string, string) (store.EventPage, error) {
	return store.EventPage{}, nil
}

func (a *contextUsageArchive) ListReplyStatistics(string) ([]model.Event, error) {
	return a.statistics, nil
}

func TestAgentHistoryRestoresEveryCallWithoutReorderingText(t *testing.T) {
	stamp := time.Now().UTC()
	archive := &contextUsageArchive{statistics: []model.Event{
		{Type: "delta", Content: "first", SentAt: stamp, Metadata: map[string]any{"reply_round_index": 0, "reply_first_text_ms": 1234}},
		{Type: "progress", SentAt: stamp, Metadata: map[string]any{"source": "llm_usage", "stage": "request_usage_ready", "message_id": "a", "total_tokens": 100, "reply_round_index": 0}},
		{Type: "progress", SentAt: stamp, Metadata: map[string]any{"source": "llm_usage", "stage": "request_usage_ready", "message_id": "b", "total_tokens": 200, "reply_round_index": 1}},
	}}
	api := newTestAPI(store.NewMemory(archive), broker.New(), auth.NewManager(time.Hour), testAPIOptions{})
	source := []model.Event{{Type: "delta", Content: "original history"}}
	result := api.restoreArchivedContextUsage("task", source)
	if len(result) != 4 || result[0].Content != source[0].Content {
		t.Fatalf("events=%+v", result)
	}
	timing := result[1].Metadata.(map[string]any)
	if timing["stage"] != "reply_timing_ready" || timing["reply_first_text_ms"] != float64(1234) {
		t.Fatalf("timing=%+v", timing)
	}
	for _, event := range result[1:] {
		if event.Type != "progress" || event.Content != "" || event.Metadata.(map[string]any)["_archived_statistics"] != true {
			t.Fatalf("supplement=%+v", event)
		}
	}
}
