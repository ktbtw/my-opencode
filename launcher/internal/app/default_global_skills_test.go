package app

import (
	"os"
	"path/filepath"
	"testing"

	"launcher/internal/model"
)

func TestEnsureDefaultGlobalSkillsInstallsAndEnables(t *testing.T) {
	runtimeDir := t.TempDir()

	if err := ensureDefaultGlobalSkills(runtimeDir); err != nil {
		t.Fatalf("ensureDefaultGlobalSkills: %v", err)
	}

	// 默认 Skill 应落盘到 <runtimeDir>/skills/<name>/
	root := filepath.Join(runtimeDir, "skills", "app-mcp-config")
	for _, name := range []string{"SKILL.md", "skill.json"} {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatalf("缺少 %s: %v", name, err)
		}
	}

	// 且默认启用。
	settings, err := loadGlobalSkillConfig(runtimeDir)
	if err != nil {
		t.Fatalf("loadGlobalSkillConfig: %v", err)
	}
	if !settings.Enabled {
		t.Fatal("首次安装后总开关应打开")
	}
	if !settings.Skills["app-mcp-config"] {
		t.Fatal("默认 Skill 应默认启用")
	}
}

func TestEnsureDefaultGlobalSkillsRespectsUserDisabledChoice(t *testing.T) {
	runtimeDir := t.TempDir()

	// 用户显式关闭了该 Skill 与总开关。
	if err := writeGlobalSkillConfig(runtimeDir, globalSkillConfigFile{
		Enabled: false,
		Skills:  map[string]bool{"app-mcp-config": false},
	}); err != nil {
		t.Fatalf("writeGlobalSkillConfig: %v", err)
	}

	if err := ensureDefaultGlobalSkills(runtimeDir); err != nil {
		t.Fatalf("ensureDefaultGlobalSkills: %v", err)
	}

	settings, err := loadGlobalSkillConfig(runtimeDir)
	if err != nil {
		t.Fatalf("loadGlobalSkillConfig: %v", err)
	}
	if settings.Skills["app-mcp-config"] {
		t.Fatal("不应覆盖用户手动关闭的选择")
	}
	if settings.Enabled {
		t.Fatal("不应覆盖用户手动关闭的总开关")
	}

	// 文件仍应保持安装（正文由 Launcher 管理）。
	if _, err := os.Stat(filepath.Join(runtimeDir, "skills", "app-mcp-config", "SKILL.md")); err != nil {
		t.Fatalf("SKILL.md 应仍然存在: %v", err)
	}
}

func TestEnsureDefaultGlobalSkillsPreservesOtherSkills(t *testing.T) {
	runtimeDir := t.TempDir()

	// 先有一个用户自己的 Skill 及其开关。
	other := "my-own-skill"
	if err := saveLocalSkill(runtimeDir, model.SemanticAgentSkill{
		Name:    other,
		Content: "# my own skill\n",
	}, true); err != nil {
		t.Fatalf("saveLocalSkill: %v", err)
	}
	if err := writeGlobalSkillConfig(runtimeDir, globalSkillConfigFile{
		Enabled: true,
		Skills:  map[string]bool{other: true},
	}); err != nil {
		t.Fatalf("writeGlobalSkillConfig: %v", err)
	}

	if err := ensureDefaultGlobalSkills(runtimeDir); err != nil {
		t.Fatalf("ensureDefaultGlobalSkills: %v", err)
	}

	settings, err := loadGlobalSkillConfig(runtimeDir)
	if err != nil {
		t.Fatalf("loadGlobalSkillConfig: %v", err)
	}
	if !settings.Skills[other] {
		t.Fatal("不应影响用户已有 Skill 的开关")
	}
	if !settings.Skills["app-mcp-config"] {
		t.Fatal("默认 Skill 应被启用")
	}
	// 用户已有 Skill 的正文不应被删除。
	if _, err := os.Stat(filepath.Join(runtimeDir, "skills", other, "SKILL.md")); err != nil {
		t.Fatalf("用户已有 Skill 应保留: %v", err)
	}
}

func TestEnsureDefaultGlobalSkillsRefreshesContent(t *testing.T) {
	runtimeDir := t.TempDir()

	// 模拟旧版本内容。
	if err := saveLocalSkill(runtimeDir, model.SemanticAgentSkill{
		Name:    "app-mcp-config",
		Content: "# outdated content\n",
	}, true); err != nil {
		t.Fatalf("saveLocalSkill: %v", err)
	}

	if err := ensureDefaultGlobalSkills(runtimeDir); err != nil {
		t.Fatalf("ensureDefaultGlobalSkills: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(runtimeDir, "skills", "app-mcp-config", "SKILL.md"))
	if err != nil {
		t.Fatalf("read SKILL.md: %v", err)
	}
	if string(data) == "# outdated content\n" {
		t.Fatal("升级后应刷新为内置最新内容")
	}
	if len(data) == 0 {
		t.Fatal("SKILL.md 不应为空")
	}
}
