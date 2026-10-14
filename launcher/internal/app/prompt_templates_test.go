package app

import (
	"os"
	"path/filepath"
	"testing"

	"launcher/internal/config"
	"launcher/internal/model"
)

func TestSaveAgentPromptTemplatesWritesFilesAndManifest(t *testing.T) {
	dir := t.TempDir()
	svc := &service{cfg: config.Config{RuntimeDir: dir}}
	revision, err := svc.SaveAgentPromptTemplates(model.DeviceAgentPromptTemplatesInput{
		AgentID:  "agent_test",
		Revision: "rev-1",
		Templates: map[string]string{
			"chat.base_prompt":     "控制台基础提示词",
			"chat.question_policy": "",
		},
	})
	if err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if revision != "rev-1" {
		t.Fatalf("revision 应回传 rev-1，实际 %s", revision)
	}
	content, err := os.ReadFile(filepath.Join(dir, "prompts", "agent_test", "chat.base_prompt.txt"))
	if err != nil {
		t.Fatalf("槽位文件未写入: %v", err)
	}
	if string(content) != "控制台基础提示词" {
		t.Fatalf("槽位内容不符: %s", content)
	}
	if _, err := os.Stat(filepath.Join(dir, "prompts", "agent_test", "chat.question_policy.txt")); !os.IsNotExist(err) {
		t.Fatal("空内容槽位不应写文件（应回落到内置默认）")
	}

	slots := svc.PromptSlotPaths("agent_test")
	if len(slots) != 2 {
		t.Fatalf("manifest 应声明两个槽位，实际 %d: %v", len(slots), slots)
	}
	if slots["chat.base_prompt"] != filepath.Join(dir, "prompts", "agent_test", "chat.base_prompt.txt") {
		t.Fatalf("槽位路径不符: %v", slots)
	}
}

func TestSaveAgentPromptTemplatesRemovesEmptySlotAndRequiresAgentID(t *testing.T) {
	dir := t.TempDir()
	svc := &service{cfg: config.Config{RuntimeDir: dir}}
	if _, err := svc.SaveAgentPromptTemplates(model.DeviceAgentPromptTemplatesInput{AgentID: "  "}); err == nil {
		t.Fatal("空 agent_id 应报错")
	}
	if _, err := svc.SaveAgentPromptTemplates(model.DeviceAgentPromptTemplatesInput{
		AgentID:   "agent_a",
		Templates: map[string]string{"chat.base_prompt": "v1"},
	}); err != nil {
		t.Fatalf("首次写入失败: %v", err)
	}
	if _, err := svc.SaveAgentPromptTemplates(model.DeviceAgentPromptTemplatesInput{
		AgentID:   "agent_a",
		Templates: map[string]string{"chat.base_prompt": "  "},
	}); err != nil {
		t.Fatalf("清空写入失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "prompts", "agent_a", "chat.base_prompt.txt")); !os.IsNotExist(err) {
		t.Fatal("清空后应删除槽位文件")
	}
	if len(svc.PromptSlotPaths("agent_a")) != 1 {
		t.Fatal("清空内容后仍应声明槽位路径（便于后续免重启生效）")
	}
}

func TestPromptSlotPathsWithoutManifest(t *testing.T) {
	svc := &service{cfg: config.Config{RuntimeDir: t.TempDir()}}
	if slots := svc.PromptSlotPaths("agent_missing"); slots != nil {
		t.Fatalf("无 manifest 时应返回 nil，实际 %v", slots)
	}
}
