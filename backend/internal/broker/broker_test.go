package broker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"relay-server/internal/model"
)

func TestRemoveIgnoresStaleConnection(t *testing.T) {
	b := New()
	hello := model.HelloPayload{
		AgentID:   "launcher:m_1",
		MachineID: "m_1",
		Hostname:  "pc",
		Kind:      "launcher",
	}

	stale := b.Add(nil, hello, 6)
	current := b.Add(nil, hello, 6)

	if b.Remove(stale) {
		t.Fatalf("expected stale connection removal to be ignored")
	}
	if _, ok := b.GetLauncher("m_1", 6); !ok {
		t.Fatalf("expected current launcher to remain online")
	}
	if !b.Remove(current) {
		t.Fatalf("expected current connection removal to succeed")
	}
	if _, ok := b.GetLauncher("m_1", 6); ok {
		t.Fatalf("expected launcher to be offline after current connection removal")
	}
}

func TestAddClosesReplacedConnection(t *testing.T) {
	b := New()
	ctx := context.Background()
	accepted := make(chan *websocket.Conn, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}
		accepted <- conn
		<-r.Context().Done()
	}))
	defer server.Close()

	dial := func() (*websocket.Conn, *websocket.Conn) {
		t.Helper()
		client, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
		if err != nil {
			t.Fatalf("dial websocket: %v", err)
		}
		select {
		case serverConn := <-accepted:
			return client, serverConn
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for accepted websocket")
			return nil, nil
		}
	}

	client1, serverConn1 := dial()
	defer client1.CloseNow()
	client2, serverConn2 := dial()
	defer client2.CloseNow()

	hello := model.HelloPayload{
		AgentID:   "launcher:m_1",
		MachineID: "m_1",
		Hostname:  "pc",
		Kind:      "launcher",
	}
	b.Add(serverConn1, hello, 6)
	b.Add(serverConn2, hello, 6)

	readCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, _, err := client1.Read(readCtx)
	if websocket.CloseStatus(err) != websocket.StatusNormalClosure {
		t.Fatalf("expected replaced connection to close normally, got %v", err)
	}
}

func TestAddAllowsSameLauncherIDAcrossOperators(t *testing.T) {
	b := New()
	hello := model.HelloPayload{
		AgentID:   "launcher:m_same",
		MachineID: "m_same",
		Hostname:  "pc-a",
		Kind:      "launcher",
	}

	first := b.Add(nil, hello, 4)
	hello.Hostname = "pc-b"
	second := b.Add(nil, hello, 10)

	if first == second {
		t.Fatalf("expected different device entries for different operators")
	}
	if agent, ok := b.GetLauncher("m_same", 4); !ok || agent.Hostname != "pc-a" {
		t.Fatalf("expected operator 4 launcher to remain online, got %+v ok=%v", agent, ok)
	}
	if agent, ok := b.GetLauncher("m_same", 10); !ok || agent.Hostname != "pc-b" {
		t.Fatalf("expected operator 10 launcher to remain online, got %+v ok=%v", agent, ok)
	}
	if !b.Remove(first) {
		t.Fatalf("expected first operator removal to succeed")
	}
	if _, ok := b.GetLauncher("m_same", 10); !ok {
		t.Fatalf("expected second operator launcher to remain online")
	}
}

func TestTouchDeviceIgnoresStaleConnection(t *testing.T) {
	b := New()
	hello := model.HelloPayload{
		AgentID:   "agent_1",
		MachineID: "m_1",
		Hostname:  "pc",
	}

	stale := b.Add(nil, hello, 6)
	current := b.Add(nil, hello, 6)

	if b.TouchDevice(stale, "task_stale") {
		t.Fatalf("expected stale touch to be ignored")
	}
	if agent, ok := b.Get("agent_1", 6); !ok || agent.CurrentTask != "" {
		t.Fatalf("expected current task to remain empty, got %+v", agent)
	}
	if !b.TouchDevice(current, "task_current") {
		t.Fatalf("expected current touch to succeed")
	}
	if agent, ok := b.Get("agent_1", 6); !ok || agent.CurrentTask != "task_current" {
		t.Fatalf("expected current task to update, got %+v", agent)
	}
}
