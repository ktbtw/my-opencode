package store

import (
	"encoding/json"
	"strings"
	"testing"

	"relay-server/internal/model"
)

func TestCapArchivedToolPayload(t *testing.T) {
	big := strings.Repeat("A", 500000)
	event := &model.Event{
		Type: "tool_updated",
		Tool: &model.ToolCall{
			Tool: "read",
			Attachments: []model.Part{
				{Type: "image", MIME: "image/png", Text: big, Source: map[string]any{"data": big}},
			},
		},
	}
	before, _ := json.Marshal(event.Tool)
	capArchivedToolPayload(event)
	after, _ := json.Marshal(event.Tool)
	t.Logf("before=%d after=%d text=%d source=%v", len(before), len(after), len(event.Tool.Attachments[0].Text), event.Tool.Attachments[0].Source)
	if len(after) >= len(before) {
		t.Fatalf("payload not capped: %d -> %d", len(before), len(after))
	}
}
