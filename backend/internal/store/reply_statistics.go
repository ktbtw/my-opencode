package store

import (
	"encoding/json"
	"log"
	"relay-server/internal/model"
	"strings"
	"time"
)

type replyTiming struct {
	startedAt   time.Time
	firstTextAt time.Time
	round       int
	hasOutput   bool
}

func statisticsMetadata(value any) map[string]any {
	raw, _ := json.Marshal(value)
	result := map[string]any{}
	_ = json.Unmarshal(raw, &result)
	if result == nil {
		result = map[string]any{}
	}
	return result
}

func replyStatisticsEvent(event model.Event) bool {
	// Inspect only low-frequency usage or the first text delta of a round.
	if event.Type != "progress" && event.Type != "delta" {
		return false
	}
	meta := statisticsMetadata(event.Metadata)
	if event.Type == "delta" {
		return meta["reply_first_text_ms"] != nil
	}
	return meta["source"] == "llm_usage" && (meta["stage"] == "request_usage_ready" || meta["stage"] == "reply_timing_ready")
}

// Called under m.mu. Server arrival times share the task creation clock. We
// deliberately do not invent a start time for tasks restored mid-flight.
func (m *Memory) measureReplyLocked(id string, event *model.Event) {
	timing, known := m.replyTimings[id]
	meta := map[string]any{}
	if event.Metadata != nil && (event.Type == "input_applied" || event.Type == "progress" || event.Type == "delta") {
		meta = statisticsMetadata(event.Metadata)
	}
	boundary := event.Type == "input_applied"
	if boundary && (meta["source"] == "subagent" || meta["context_type"] == "subagentResult") && strings.TrimSpace(stringValue(meta["wake_reason"])) == "" {
		boundary = false
	}
	if (event.Type == "goal_created" || event.Type == "goal_continued") && (!known || timing.hasOutput) {
		boundary = true
	}
	if boundary {
		round := -1
		if known && timing.round >= 0 {
			round = timing.round + 1
		}
		timing = replyTiming{startedAt: time.Now().UTC(), round: round}
		// After a restart we know the time but not the absolute round number.
		if round < 0 {
			timing.round = -1
		}
		known = true
	}
	if !known {
		return
	}
	first := event.Type == "delta" && event.Field != "reasoning" && event.Content != "" && timing.firstTextAt.IsZero()
	if first {
		timing.firstTextAt = time.Now().UTC()
	}
	switch event.Type {
	case "delta", "tool_updated", "plan_updated", "goal_created", "goal_continued", "subagent_started", "subagent_result":
		timing.hasOutput = true
	}
	if first || replyStatisticsEvent(*event) {
		meta["reply_round_started_at"] = timing.startedAt.Format(time.RFC3339Nano)
		if timing.round >= 0 {
			meta["reply_round_index"] = timing.round
		}
		if !timing.firstTextAt.IsZero() {
			meta["reply_first_text_ms"] = timing.firstTextAt.Sub(timing.startedAt).Milliseconds()
		}
		event.Metadata = meta
	}
	m.replyTimings[id] = timing
}

func stringValue(value any) string { s, _ := value.(string); return s }

type TaskReplyStatisticsReader interface {
	ListReplyStatistics(taskID string) ([]model.Event, error)
}

// Measurements are small and independent from the truncated text replay window.
func (m *Memory) ReplyStatistics(taskID string) []model.Event {
	var result []model.Event
	if reader, ok := m.archive.(TaskReplyStatisticsReader); ok {
		var err error
		result, err = reader.ListReplyStatistics(taskID)
		if err != nil {
			log.Printf("read reply statistics failed: %v", err)
		}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	seen := map[int64]bool{}
	for _, event := range result {
		seen[event.Sequence] = true
	}
	for _, event := range m.events[taskID] {
		if !seen[event.Sequence] && replyStatisticsEvent(event) {
			result = append(result, event)
		}
	}
	return result
}
