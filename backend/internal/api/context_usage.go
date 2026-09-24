package api

import (
	"encoding/json"
	"relay-server/internal/model"
)

// Agent-local history reconstructs text/tools but older agents omit usage.
// Preserve the durable completion measurement without replaying the full archive.
func (a *API) restoreArchivedContextUsage(taskID string, events []model.Event) []model.Event {
	result := append([]model.Event(nil), events...)
	for _, event := range a.store.ReplyStatistics(taskID) {
		raw, _ := json.Marshal(event.Metadata)
		var usage map[string]any
		if json.Unmarshal(raw, &usage) != nil {
			continue
		}
		usage["source"] = "llm_usage"
		usage["_archived_statistics"] = true
		if event.Type == "delta" {
			usage["stage"] = "reply_timing_ready"
		}
		result = append(result, model.Event{TaskID: taskID, Type: "progress", SentAt: event.SentAt, Metadata: usage})
	}
	terminal, found := a.store.LatestTerminalEvent(taskID)
	if !found {
		return result
	}
	raw, err := json.Marshal(terminal.Metadata)
	if err != nil {
		return result
	}
	var metadata map[string]json.RawMessage
	if json.Unmarshal(raw, &metadata) != nil {
		return result
	}
	var usage map[string]any
	if json.Unmarshal(metadata["usage"], &usage) != nil || len(usage) == 0 {
		return result
	}
	usage["source"] = "llm_usage"
	usage["_archived_statistics"] = true
	restored := model.Event{TaskID: taskID, Type: "progress", SentAt: terminal.SentAt, Metadata: usage}
	// Work on a copy: the source may belong to an in-memory archive page.
	return append(result, restored)
}
