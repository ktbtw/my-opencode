package app

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"launcher/internal/model"
)

// defaultGlobalSkillsJSON 是随 Launcher 一起发布的「默认全局 Skill」清单。
// 这些 Skill 会在 Launcher 启动时自动落盘到 <runtimeDir>/skills/ 并默认启用，
// 让所有用户开箱即用，无需手动导入。
//
//go:embed builtin/default_global_skills.json
var defaultGlobalSkillsJSON []byte

type defaultGlobalSkill struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
	Enabled     bool   `json:"enabled"`
}

type defaultGlobalSkillsFile struct {
	Version int                  `json:"version"`
	Skills  []defaultGlobalSkill `json:"skills"`
}

// ensureDefaultGlobalSkills 把内置默认 Skill 安装到设备并默认启用。
//
// 语义：
//   - Skill 正文由 Launcher 管理，每次启动按内置内容刷新（与语义 Agent 的
//     prepareSemanticAgentSkills 行为一致），保证升级后用户拿到最新版本。
//   - 启用开关只在用户尚未表态（config.json 中缺少该 key）时写入默认值，
//     不覆盖用户后来手动关闭的选择。
//   - 不删除、不修改清单之外的任何 Skill。
func ensureDefaultGlobalSkills(runtimeDir string) error {
	var file defaultGlobalSkillsFile
	if err := json.Unmarshal(defaultGlobalSkillsJSON, &file); err != nil {
		return fmt.Errorf("解析内置默认 Skill 清单失败: %w", err)
	}
	if len(file.Skills) == 0 {
		return nil
	}

	configPath := filepath.Join(runtimeDir, "skills", "config.json")
	_, statErr := os.Stat(configPath)
	configExists := statErr == nil

	settings, err := loadGlobalSkillConfig(runtimeDir)
	if err != nil {
		return err
	}

	changed := false
	installed := make([]string, 0, len(file.Skills))
	for _, item := range file.Skills {
		name := strings.TrimSpace(item.Name)
		if name == "" {
			name = strings.TrimSpace(item.ID)
		}
		if name == "" {
			continue
		}
		skill := model.SemanticAgentSkill{
			Name:        name,
			Description: strings.TrimSpace(item.Description),
			Content:     strings.TrimSpace(item.Content),
		}
		if err := saveLocalSkill(runtimeDir, skill, true); err != nil {
			return fmt.Errorf("安装内置默认 Skill %s 失败: %w", name, err)
		}
		installed = append(installed, name)

		if _, exists := settings.Skills[name]; !exists {
			settings.Skills[name] = item.Enabled
			changed = true
		}
	}

	// 首次落地（此前没有 config.json）时把总开关打开；
	// 已有配置则尊重用户对总开关的选择。
	if !configExists && !settings.Enabled {
		settings.Enabled = true
		changed = true
	}
	if changed {
		if err := writeGlobalSkillConfig(runtimeDir, settings); err != nil {
			return err
		}
	}
	if len(installed) > 0 {
		log.Printf("[launcher][skills] 内置默认 Skill 已就绪: %v (总开关=%v)", installed, settings.Enabled)
	}
	return nil
}
