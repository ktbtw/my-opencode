package store

import "testing"

func TestDevicePreferencesKeepFirstOrderAndSupportAccountScopedReorder(t *testing.T) {
	memory := NewMemory(nil)
	first, err := memory.EnsureDevicePreferences(1, []string{"machine-b", "machine-a"})
	if err != nil {
		t.Fatal(err)
	}
	if first[0].SortOrder != 1 || first[1].SortOrder != 2 {
		t.Fatalf("unexpected initial order: %+v", first)
	}
	again, err := memory.EnsureDevicePreferences(1, []string{"machine-a", "machine-b", "machine-c"})
	if err != nil {
		t.Fatal(err)
	}
	if again[0].SortOrder != 2 || again[1].SortOrder != 1 || again[2].SortOrder != 3 {
		t.Fatalf("existing order changed while ensuring preferences: %+v", again)
	}
	reordered, err := memory.ReorderDevicePreferences(1, []string{"machine-c", "machine-a", "machine-b"})
	if err != nil {
		t.Fatal(err)
	}
	for index, preference := range reordered {
		if preference.SortOrder != int64(index+1) {
			t.Fatalf("unexpected reordered preference at %d: %+v", index, preference)
		}
	}
	other, err := memory.EnsureDevicePreferences(2, []string{"machine-a", "machine-b"})
	if err != nil {
		t.Fatal(err)
	}
	if other[0].SortOrder != 1 || other[1].SortOrder != 2 {
		t.Fatalf("operator order leaked across accounts: %+v", other)
	}
}

func TestAgentPreferencesAreScopedByOperatorAndMachine(t *testing.T) {
	memory := NewMemory(nil)
	if _, err := memory.ReorderAgentPreferences(1, "machine-a", []string{"agent-b", "agent-a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := memory.ReorderAgentPreferences(1, "machine-b", []string{"agent-a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := memory.ReorderAgentPreferences(2, "machine-a", []string{"agent-a"}); err != nil {
		t.Fatal(err)
	}
	preferences, err := memory.ListAgentPreferences(1, []string{"machine-a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(preferences) != 2 {
		t.Fatalf("expected only operator 1 machine-a preferences, got %+v", preferences)
	}
	orders := map[string]int64{}
	for _, preference := range preferences {
		orders[preference.AgentID] = preference.SortOrder
	}
	if orders["agent-b"] != 1 || orders["agent-a"] != 2 {
		t.Fatalf("unexpected agent order: %+v", orders)
	}
}
