package app

import (
	"testing"
)

func TestComposeSemanticPromptModes(t *testing.T) {
	// 模型提示词由后端在建任务时注入，launcher 侧只按生效模式决定角色提示词的取舍。
	if got := composeSemanticPrompt("agent", "", "agent"); got != "agent" {
		t.Fatalf("agent mode: %q", got)
	}
	if got := composeSemanticPrompt("agent", "", "model"); got != "" {
		t.Fatalf("model mode without model prompt: %q", got)
	}
	if got := composeSemanticPrompt("agent", "", "both"); got != "agent" {
		t.Fatalf("both mode without model prompt: %q", got)
	}

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
