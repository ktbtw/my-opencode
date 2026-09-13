package overlay

import (
	"testing"
	"time"
)

func TestHubIsolatesOperators(t *testing.T) {
	hub := NewHub()
	ownerEvents, cancelOwner := hub.Subscribe(7)
	defer cancelOwner()
	otherEvents, cancelOther := hub.Subscribe(8)
	defer cancelOther()

	hub.Publish(7, "task.updated", map[string]string{"task_id": "task-1"})
	select {
	case event := <-ownerEvents:
		if event.Type != "task.updated" || event.OperatorID != 7 {
			t.Fatalf("unexpected event: %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("owner did not receive event")
	}
	select {
	case event := <-otherEvents:
		t.Fatalf("event leaked to another operator: %+v", event)
	case <-time.After(20 * time.Millisecond):
	}
}

func TestHubDoesNotBlockOnSlowSubscriber(t *testing.T) {
	hub := NewHub()
	_, cancel := hub.Subscribe(7)
	defer cancel()
	done := make(chan struct{})
	go func() {
		for index := 0; index < 200; index++ {
			hub.Publish(7, "agent.upsert", index)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("publisher blocked on slow subscriber")
	}
}

func TestHubClosesSlowSubscriberInsteadOfSilentlyDropping(t *testing.T) {
	hub := NewHub()
	events, cancel := hub.Subscribe(7)
	defer cancel()
	for index := 0; index < 65; index++ {
		hub.Publish(7, "task.updated", index)
	}
	for index := 0; index < 64; index++ {
		select {
		case _, ok := <-events:
			if !ok {
				t.Fatalf("subscriber closed before buffered event %d", index)
			}
		case <-time.After(time.Second):
			t.Fatal("timed out draining slow subscriber")
		}
	}
	select {
	case _, ok := <-events:
		if ok {
			t.Fatal("slow subscriber remained open after overflow")
		}
	case <-time.After(time.Second):
		t.Fatal("slow subscriber was not closed after overflow")
	}
}
