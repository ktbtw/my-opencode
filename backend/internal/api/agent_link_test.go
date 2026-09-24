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

func TestAgentLinkRequiresAccountAndReportsUnknownConnection(t *testing.T) {
	manager := auth.NewManager(time.Hour)
	api := newTestAPI(store.NewMemory(nil), broker.New(), manager, testAPIOptions{})
	request := httptest.NewRequest(http.MethodGet, "/api/agents/agent/connection-latency", nil)
	response := httptest.NewRecorder()
	api.AgentLinkLatency(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", response.Code)
	}
	token, err := manager.Issue(model.Operator{ID: 1, Username: "test"})
	if err != nil {
		t.Fatal(err)
	}
	request = request.WithContext(context.WithValue(request.Context(), chi.RouteCtxKey, routeContext("agentID", "agent")))
	request.Header.Set("Authorization", "Bearer "+token)
	response = httptest.NewRecorder()
	api.AgentLinkLatency(response, request)
	var data map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusOK || data["status"] != "offline" || data["rtt_ms"] != nil {
		t.Fatalf("status=%d data=%+v", response.Code, data)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing cache policy")
	}
}
