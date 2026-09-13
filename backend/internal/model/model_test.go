package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestQuestionItemMarshalsExplicitCustomFalse(t *testing.T) {
	payload, err := json.Marshal(QuestionItem{Question: "Continue?", Custom: false})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(payload), `"custom":false`) {
		t.Fatalf("expected explicit custom=false, got %s", payload)
	}
}
