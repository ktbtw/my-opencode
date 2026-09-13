package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestValidateSyncedAppNotification(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	record := map[string]any{
		"id": "notice-1", "operation_id": "agent:create:1",
		"title": "创建 Agent", "message": "准备中",
		"status": "running", "progress_mode": "determinate",
		"kind": "agent", "scope": "synced", "attention": "none",
		"stages": []any{}, "actions": []any{},
		"created_at": now, "updated_at": now,
	}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	operationID, ok := validateSyncedAppNotification(raw)
	if !ok || operationID != "agent:create:1" {
		t.Fatalf("valid record rejected: %q, %v", operationID, ok)
	}

	record["scope"] = "local"
	raw, _ = json.Marshal(record)
	if _, ok := validateSyncedAppNotification(raw); ok {
		t.Fatal("local notification accepted by sync API")
	}
	record["scope"] = "synced"
	record["operation_id"] = "bad\nidentifier"
	raw, _ = json.Marshal(record)
	if _, ok := validateSyncedAppNotification(raw); ok {
		t.Fatal("invalid operation ID accepted")
	}
	record["operation_id"] = "agent:create:1"
	record["message"] = strings.Repeat("x", 2001)
	raw, _ = json.Marshal(record)
	if _, ok := validateSyncedAppNotification(raw); ok {
		t.Fatal("oversized message accepted")
	}
}

func TestParseNotificationCursor(t *testing.T) {
	if value, err := parseNonNegativeInt64("42"); err != nil || value != 42 {
		t.Fatalf("unexpected cursor result: %d, %v", value, err)
	}
	for _, raw := range []string{"-1", "nope"} {
		if _, err := parseNonNegativeInt64(raw); err == nil {
			t.Fatalf("invalid cursor accepted: %q", raw)
		}
	}
}
