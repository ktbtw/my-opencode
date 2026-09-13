package store

import (
	"testing"
	"time"

	"relay-server/internal/model"
)

func TestTaskObserverPublishesLifecycleButNotDeltas(t *testing.T) {
	memory := NewMemory(nil)
	observed := make(chan *model.Task, 2)
	memory.SetTaskObserver(func(task *model.Task) { observed <- task })
	task := memory.CreateTask("agent", "machine", "project", "/project", "session", nil, nil, 7)
	memory.SetStatus(task.ID, model.TaskRunning)
	memory.AddEvent(task.ID, model.Event{TaskID: task.ID, Type: "started", SentAt: time.Now()})

	select {
	case event := <-observed:
		if event.Status != model.TaskRunning || event.OperatorID != 7 {
			t.Fatalf("unexpected observed task: %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("lifecycle event was not observed")
	}

	memory.AddEvent(task.ID, model.Event{TaskID: task.ID, Type: "delta", Content: "text", SentAt: time.Now()})
	select {
	case event := <-observed:
		t.Fatalf("delta unexpectedly reached overlay observer: %+v", event)
	case <-time.After(20 * time.Millisecond):
	}
}
