package app

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"relay-server/internal/auth"
	"relay-server/internal/broker"
	"relay-server/internal/model"
	"relay-server/internal/store"
)

func TestRelayE2EServer(t *testing.T) {
	if os.Getenv("CHAT_CODEX_RELAY_E2E_SERVER") != "1" {
		t.Skip("set CHAT_CODEX_RELAY_E2E_SERVER=1 to run the cross-project relay E2E server")
	}

	operator := model.Operator{
		ID:          1,
		OperatorUID: "operator-cross-e2e",
		Username:    "cross-relay-tester",
		OperatorKey: "operator-key-cross-e2e",
	}
	authManager := auth.NewManager(time.Hour)
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	application := &App{
		store: store.NewMemory(relayTestArchive{
			operators: map[string]model.Operator{
				operator.OperatorKey: operator,
			},
		}),
		broker:               broker.New(),
		auth:                 authManager,
		projectMemoryEnabled: true,
		taskCleanupTick:      time.Hour,
		taskTimeout:          time.Hour,
		taskCancelGrace:      time.Second,
		artifactSubs:         map[string]chan artifactStreamEvent{},
		aiConfigSubs:         map[string]chan model.DeviceAIConfigResultPayload{},
		goalOptimizeSubs:     map[string]chan model.GoalOptimizeResultPayload{},
		mcpConfigSubs:        map[string]chan model.DeviceMCPConfigResultPayload{},
		launcherSubs:         map[string]chan model.DeviceLauncherResultPayload{},
		directoriesSubs:      map[string]chan model.DeviceDirectoriesResultPayload{},
	}
	server := httptest.NewServer(application.Router())
	defer server.Close()

	ready, err := json.Marshal(map[string]string{
		"url":          server.URL,
		"token":        token,
		"operator_key": operator.OperatorKey,
	})
	if err != nil {
		t.Fatalf("marshal ready payload: %v", err)
	}
	fmt.Println(string(ready))

	stopFile := os.Getenv("CHAT_CODEX_RELAY_E2E_STOP_FILE")
	deadline := time.After(2 * time.Minute)
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for E2E server stop signal")
		case <-ticker.C:
			if stopFile == "" {
				continue
			}
			if _, err := os.Stat(stopFile); err == nil {
				return
			}
		}
	}
}
