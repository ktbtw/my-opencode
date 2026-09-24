package store

import (
	"relay-server/internal/model"
	"testing"
	"time"
)

type statisticsTestArchive struct {
	noopArchive
	events []model.Event
}

func (a *statisticsTestArchive) AppendEvent(event model.Event) error {
	a.events = append(a.events, event)
	return nil
}
func (a *statisticsTestArchive) ListReplyStatistics(string) ([]model.Event, error) {
	var result []model.Event
	for _, e := range a.events {
		if replyStatisticsEvent(e) {
			result = append(result, e)
		}
	}
	return result, nil
}

func TestReplyStatisticsArchiveAndTiming(t *testing.T) {
	t.Setenv("ARCHIVE_HIGH_FREQ_EVENTS", "0")
	archive := &statisticsTestArchive{}
	memory := NewMemory(archive)
	task := memory.CreateTask("agent", "machine", "project", "/tmp", "session", nil, nil, 1)
	memory.replyTimings[task.ID] = replyTiming{startedAt: time.Now().Add(-2 * time.Second)}
	emit := func(kind, content, field string, meta any) {
		memory.AddEvent(task.ID, model.Event{TaskID: task.ID, Type: kind, Content: content, Field: field, SentAt: time.Now(), Metadata: meta})
	}
	emit("delta", "reasoning", "reasoning", nil)
	emit("delta", "first", "text", nil)
	emit("delta", "rest", "text", nil)
	emit("progress", "noise", "", nil)
	emit("progress", "usage", "", map[string]any{"source": "llm_usage", "stage": "request_usage_ready", "message_id": "a", "total_tokens": 120})
	if len(archive.events) != 2 {
		t.Fatalf("expected first text and usage only: %+v", archive.events)
	}
	first := statisticsMetadata(archive.events[0].Metadata)
	measured := first["reply_first_text_ms"].(float64)
	if measured < 2000 || measured > 3000 {
		t.Fatalf("latency=%v", measured)
	}
	usage := statisticsMetadata(archive.events[1].Metadata)
	if usage["reply_first_text_ms"] != measured || usage["reply_round_index"] != float64(0) {
		t.Fatalf("usage=%+v", usage)
	}
	emit("input_applied", "background result", "", map[string]any{"source": "subagent"})
	emit("progress", "usage", "", map[string]any{"source": "llm_usage", "stage": "request_usage_ready", "message_id": "b", "total_tokens": 80})
	if statisticsMetadata(archive.events[len(archive.events)-1].Metadata)["reply_round_index"] != float64(0) {
		t.Fatal("passive subagent result opened a round")
	}
	emit("input_applied", "next", "", nil)
	emit("delta", "new round", "text", nil)
	last := statisticsMetadata(archive.events[len(archive.events)-1].Metadata)
	if last["reply_round_index"] != float64(1) || last["reply_first_text_ms"].(float64) >= 1000 {
		t.Fatalf("next round=%+v", last)
	}
	// Simulate live window eviction: durable low frequency records still restore.
	delete(memory.events, task.ID)
	if len(memory.ReplyStatistics(task.ID)) != 4 {
		t.Fatalf("archived=%+v", memory.ReplyStatistics(task.ID))
	}
	emit("completed", "", "", nil)
	if _, found := memory.replyTimings[task.ID]; found {
		t.Fatal("terminal timing leaked")
	}
}

func TestReplyStatisticsDoesNotInventTimingAfterRestart(t *testing.T) {
	memory := NewMemory(nil)
	memory.AddEvent("restored", model.Event{TaskID: "restored", Type: "delta", Content: "midstream", SentAt: time.Now()})
	event := memory.Events("restored")[0]
	if statisticsMetadata(event.Metadata)["reply_first_text_ms"] != nil {
		t.Fatal("invented midstream measurement")
	}
	memory.AddEvent("restored", model.Event{TaskID: "restored", Type: "input_applied", SentAt: time.Now()})
	memory.AddEvent("restored", model.Event{TaskID: "restored", Type: "delta", Content: "new", SentAt: time.Now()})
	event = memory.Events("restored")[2]
	meta := statisticsMetadata(event.Metadata)
	if meta["reply_first_text_ms"] == nil || meta["reply_round_index"] != nil {
		t.Fatalf("restored round=%+v", meta)
	}
}
