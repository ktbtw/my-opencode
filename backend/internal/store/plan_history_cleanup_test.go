package store

import (
	"encoding/json"
	"testing"
	"time"

	"relay-server/internal/model"
)

func TestParsePlanHistoryEventBuildsStableContentKey(t *testing.T) {
	raw := planHistoryEventJSON(t, model.Event{
		TaskID:    "task_1",
		Type:      "plan_updated",
		SessionID: "ses_1",
		SentAt:    time.Date(2026, 6, 5, 15, 27, 0, 0, time.UTC),
		Plan: &model.Plan{
			ID:        "todo_plan_ses_1",
			Title:     "执行计划",
			Mode:      "build",
			Status:    "completed",
			SessionID: "ses_1",
			Items: []model.PlanItem{
				{ID: "todo_a", Text: "检查计划时间", Status: "completed", Priority: "high"},
				{ID: "todo_b", Text: "清理重复计划", Status: "completed", Priority: "medium"},
			},
		},
	})

	evt, key, ok, err := parsePlanHistoryEvent(raw)
	if err != nil {
		t.Fatalf("parse event: %v", err)
	}
	if !ok {
		t.Fatal("expected plan history event")
	}
	if evt.TaskID != "task_1" {
		t.Fatalf("unexpected task id: %s", evt.TaskID)
	}

	rawLater := planHistoryEventJSON(t, model.Event{
		TaskID:    "task_1",
		Type:      "plan_updated",
		SessionID: "ses_1",
		SentAt:    time.Date(2026, 6, 5, 15, 28, 0, 0, time.UTC),
		Plan: &model.Plan{
			ID:        "todo_plan_ses_1",
			Title:     "执行计划",
			Mode:      "build",
			Status:    "completed",
			SessionID: "ses_1",
			Items: []model.PlanItem{
				{ID: "todo_a", Text: "检查计划时间", Status: "completed", Priority: "high"},
				{ID: "todo_b", Text: "清理重复计划", Status: "completed", Priority: "medium"},
			},
		},
	})
	_, laterKey, ok, err := parsePlanHistoryEvent(rawLater)
	if err != nil {
		t.Fatalf("parse later event: %v", err)
	}
	if !ok {
		t.Fatal("expected later plan history event")
	}
	if key != laterKey {
		t.Fatalf("same content should share key\nfirst=%q\nlater=%q", key, laterKey)
	}
}

func TestParsePlanHistoryEventKeepsChangedPlanDistinct(t *testing.T) {
	first := planHistoryEventJSON(t, model.Event{
		TaskID: "task_1",
		Type:   "plan_updated",
		Plan: &model.Plan{
			ID:        "todo_plan_ses_1",
			SessionID: "ses_1",
			Items: []model.PlanItem{
				{ID: "todo_a", Text: "检查计划时间", Status: "in_progress"},
			},
		},
	})
	second := planHistoryEventJSON(t, model.Event{
		TaskID: "task_1",
		Type:   "plan_updated",
		Plan: &model.Plan{
			ID:        "todo_plan_ses_1",
			SessionID: "ses_1",
			Items: []model.PlanItem{
				{ID: "todo_a", Text: "检查计划时间", Status: "completed"},
			},
		},
	})

	_, firstKey, ok, err := parsePlanHistoryEvent(first)
	if err != nil || !ok {
		t.Fatalf("parse first event ok=%v err=%v", ok, err)
	}
	_, secondKey, ok, err := parsePlanHistoryEvent(second)
	if err != nil || !ok {
		t.Fatalf("parse second event ok=%v err=%v", ok, err)
	}
	if firstKey == secondKey {
		t.Fatalf("changed plan state must not be treated as duplicate: %q", firstKey)
	}
}

func TestParsePlanHistoryEventIgnoresNonPlanEvent(t *testing.T) {
	raw := planHistoryEventJSON(t, model.Event{
		TaskID:  "task_1",
		Type:    "text_delta",
		Content: "hello",
	})
	_, _, ok, err := parsePlanHistoryEvent(raw)
	if err != nil {
		t.Fatalf("parse non-plan event: %v", err)
	}
	if ok {
		t.Fatal("non-plan event should not be treated as plan history")
	}
}

func planHistoryEventJSON(t *testing.T, evt model.Event) string {
	t.Helper()
	buf, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	return string(buf)
}
