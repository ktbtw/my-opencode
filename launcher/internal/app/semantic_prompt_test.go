package app

import (
	"testing"

	"launcher/internal/model"
)

func TestModelPromptForProviderAndModel(t *testing.T) {
	profile := model.SemanticAgentProfile{ModelPromptPresets: []model.SemanticAgentModelPromptPreset{
		{ID: "openai-codex", Provider: "openai", ModelPattern: "codex", Prompt: "codex prompt", Enabled: true, SortOrder: 10},
		{ID: "xai-grok", Provider: "xai", ModelPattern: "grok", Prompt: "grok prompt", Enabled: true, SortOrder: 20},
	}}
	if got := modelPromptFor(profile, map[string]any{"model": "openai/gpt-5.3-codex"}); got != "codex prompt" {
		t.Fatalf("expected codex preset, got %q", got)
	}
	if got := modelPromptFor(profile, map[string]any{"model": "xai/grok-4"}); got != "grok prompt" {
		t.Fatalf("expected grok preset, got %q", got)
	}
	if got := modelPromptFor(profile, map[string]any{"model": "anthropic/claude"}); got != "" {
		t.Fatalf("expected no preset, got %q", got)
	}
}

func TestComposeSemanticPromptModes(t *testing.T) {
	if got := composeSemanticPrompt("agent", "model", "agent"); got != "agent" {
		t.Fatalf("agent mode: %q", got)
	}
	if got := composeSemanticPrompt("agent", "model", "model"); got != "model" {
		t.Fatalf("model mode: %q", got)
	}
	if got := composeSemanticPrompt("agent", "model", "both"); got != "agent\n\n--- 模型专属提示词 ---\nmodel" {
		t.Fatalf("both mode: %q", got)
	}
}
