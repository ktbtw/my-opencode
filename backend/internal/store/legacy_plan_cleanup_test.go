package store

import (
	"encoding/json"
	"testing"

	"relay-server/internal/model"
)

func TestCleanLegacyTaskInput(t *testing.T) {
	raw, err := json.Marshal(struct {
		Parts []model.Part `json:"parts,omitempty"`
		Plan  *model.Plan  `json:"plan,omitempty"`
	}{
		Parts: []model.Part{{Type: "text", Text: "hello"}},
		Plan: &model.Plan{
			ID:        "plan_ses_demo",
			Title:     "执行计划",
			Status:    "in_progress",
			SessionID: "ses_demo",
			Items: []model.PlanItem{
				{ID: "1", Text: "first", Status: "pending"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	next, sid, ok, err := cleanLegacyTaskInput(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected legacy task plan to be cleaned")
	}
	if sid != "ses_demo" {
		t.Fatalf("unexpected sid: %s", sid)
	}

	var out map[string]any
	if err := json.Unmarshal([]byte(next), &out); err != nil {
		t.Fatal(err)
	}
	if _, exists := out["plan"]; exists {
		t.Fatal("expected plan to be removed")
	}
}

func TestCleanLegacyTaskInputKeepsRealPlan(t *testing.T) {
	raw, err := json.Marshal(struct {
		Plan *model.Plan `json:"plan,omitempty"`
	}{
		Plan: &model.Plan{
			ID:     "pln_real",
			Title:  "执行计划",
			Status: "pending",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	next, sid, ok, err := cleanLegacyTaskInput(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected real plan to stay untouched")
	}
	if sid != "" {
		t.Fatalf("unexpected sid: %s", sid)
	}
	if next != string(raw) {
		t.Fatal("expected json to stay unchanged")
	}
}

func TestLegacyEventPlanID(t *testing.T) {
	raw, err := json.Marshal(model.Event{
		Type:      "plan_updated",
		SessionID: "ses_demo",
		Plan: &model.Plan{
			ID:     "plan_ses_demo",
			Title:  "执行计划",
			Status: "completed",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	id, sid, ok, err := legacyEventPlanID(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected legacy event to match")
	}
	if id != "plan_ses_demo" || sid != "ses_demo" {
		t.Fatalf("unexpected values: id=%s sid=%s", id, sid)
	}
}
