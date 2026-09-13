package api

import (
	"encoding/json"
	"testing"

	"relay-server/internal/model"
)

func TestOverlayNotificationCursorParsing(t *testing.T) {
	for _, test := range []struct {
		raw  string
		want int64
	}{
		{raw: "", want: 0},
		{raw: " 42 ", want: 42},
	} {
		got, err := parseOverlayNotificationCursor(test.raw)
		if err != nil || got != test.want {
			t.Fatalf("parse cursor %q: got=%d err=%v", test.raw, got, err)
		}
	}
}

func TestLatestOverlayNotificationChangesKeepsNewestOperationState(t *testing.T) {
	changes := []model.AppNotificationChange{
		{Version: 7, OperationID: "chat-task:1", Record: json.RawMessage(`{"unread":true}`)},
		{Version: 8, OperationID: "other", Record: json.RawMessage(`{"unread":true}`)},
		{Version: 9, OperationID: "chat-task:1", Record: json.RawMessage(`{"unread":false}`)},
	}
	latest := latestOverlayNotificationChanges(changes)
	if len(latest) != 2 || latest[0].Version != 8 || latest[1].Version != 9 {
		t.Fatalf("unexpected coalesced changes: %+v", latest)
	}
}

func TestOverlayNotificationCursorRejectsMalformedValue(t *testing.T) {
	for _, raw := range []string{"-1", "not-a-number", "1.5"} {
		if _, err := parseOverlayNotificationCursor(raw); err == nil {
			t.Fatalf("expected cursor %q to be rejected", raw)
		}
	}
}
