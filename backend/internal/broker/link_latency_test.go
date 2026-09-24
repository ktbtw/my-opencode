package broker

import (
	"context"
	"errors"
	"github.com/coder/websocket"
	"net/http"
	"net/http/httptest"
	"relay-server/internal/model"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestLinkLatencySharesAndCachesProbe(t *testing.T) {
	dev := &Device{}
	var probes atomic.Int32
	var wg sync.WaitGroup
	ping := func(ctx context.Context) error { probes.Add(1); return nil }
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sample, err := dev.measureLink(context.Background(), ping)
			if err != nil || sample.Status != "connected" || sample.Milliseconds == nil {
				t.Errorf("sample=%+v err=%v", sample, err)
			}
		}()
	}
	wg.Wait()
	if probes.Load() != 1 {
		t.Fatalf("probes=%d", probes.Load())
	}
	dev.linkMu.Lock()
	dev.linkSample.MeasuredAt = time.Now().Add(-11 * time.Second)
	dev.linkMu.Unlock()
	sample, err := dev.measureLink(context.Background(), func(context.Context) error { return errors.New("no pong") })
	if err != nil || sample.Status != "timeout" || sample.Milliseconds != nil {
		t.Fatalf("stale success retained: %+v err=%v", sample, err)
	}
}

func TestCancelledLinkCallerDoesNotCancelOtherViewers(t *testing.T) {
	dev := &Device{}
	gate := make(chan struct{})
	started := make(chan struct{})
	ping := func(ctx context.Context) error {
		close(started)
		select {
		case <-gate:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := dev.measureLink(ctx, ping); done <- err }()
	<-started
	cancel()
	if !errors.Is(<-done, context.Canceled) {
		t.Fatal("caller did not cancel")
	}
	close(gate)
	sample, err := dev.measureLink(context.Background(), ping)
	if err != nil || sample.Status != "connected" {
		t.Fatalf("shared probe cancelled: %+v %v", sample, err)
	}
}

func TestAgentLinkLatencyUsesExistingSocketAndAccountScope(t *testing.T) {
	b := New()
	ready := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		dev := b.Add(conn, model.HelloPayload{AgentID: "agent", MachineID: "machine"}, 7)
		defer b.Remove(dev)
		reading := conn.CloseRead(r.Context())
		close(ready)
		<-reading.Done()
	}))
	defer server.Close()
	client, _, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseNow()
	client.CloseRead(context.Background())
	<-ready
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	sample, err := b.AgentLinkLatency(ctx, 7, "agent")
	if err != nil || sample.Status != "connected" || sample.Milliseconds == nil {
		t.Fatalf("sample=%+v err=%v", sample, err)
	}
	other, err := b.AgentLinkLatency(ctx, 8, "agent")
	if err != nil || other.Status != "offline" || other.Milliseconds != nil {
		t.Fatalf("cross-account sample=%+v err=%v", other, err)
	}
}
