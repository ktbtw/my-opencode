// Package promptregistry 定义可由控制台管理的提示词槽位（slot）。
//
// 每个槽位有稳定的 slot_id、分类、允许覆盖的层级、变量占位符、风险等级与内置默认文本。
// 默认文本来自 defaults.json（由仓库脚本从运行时/launcher 源码提取），
// 控制台保存的"平台默认"存库后即以库里的值为准。
package promptregistry

import (
	_ "embed"
	"encoding/json"
	"strings"
)

//go:embed defaults.json
var defaultsRaw []byte

// 覆盖层级。
const (
	ScopePlatform = "platform"
	ScopeSemantic = "semantic"
	ScopeDevice   = "device"
)

// 分类。
const (
	CategoryChat    = "chat"
	CategoryMode    = "mode"
	CategoryJob     = "job"
	CategoryMessage = "message"
)

// 风险等级：high 表示与输出解析耦合，控制台保存时需要校验必需占位符。
const (
	RiskLow    = "low"
	RiskMedium = "medium"
	RiskHigh   = "high"
)

// Slot 是一个可配置的提示词槽位。
type Slot struct {
	ID        string   `json:"id"`
	Category  string   `json:"category"`
	Scope     []string `json:"scope"`
	Variables []string `json:"variables,omitempty"`
	Risk      string   `json:"risk"`
	Default   string   `json:"default"`
}

// 第一批：主对话系统提示词。后续批次在此追加 mode/job/message 槽位。
var definitions = []Slot{
	{
		ID:       "chat.base_prompt",
		Category: CategoryChat,
		Scope:    []string{ScopePlatform, ScopeSemantic, ScopeDevice},
		Risk:     RiskLow,
	},
	{
		ID:       "chat.question_policy",
		Category: CategoryChat,
		Scope:    []string{ScopePlatform, ScopeSemantic, ScopeDevice},
		Risk:     RiskLow,
	},
	{
		ID:       "chat.orchestration_prompt",
		Category: CategoryChat,
		Scope:    []string{ScopePlatform, ScopeSemantic, ScopeDevice},
		Risk:     RiskLow,
	},
}

var defaults = func() map[string]string {
	out := map[string]string{}
	if err := json.Unmarshal(defaultsRaw, &out); err != nil {
		panic("promptregistry: 解析 defaults.json 失败: " + err.Error())
	}
	return out
}()

// All 返回全部槽位定义（带内置默认文本）。
func All() []Slot {
	out := make([]Slot, 0, len(definitions))
	for _, slot := range definitions {
		slot.Default = defaults[slot.ID]
		out = append(out, slot)
	}
	return out
}

// ByID 按 slot_id 查槽位。
func ByID(id string) (Slot, bool) {
	id = strings.TrimSpace(id)
	for _, slot := range definitions {
		if slot.ID == id {
			slot.Default = defaults[slot.ID]
			return slot, true
		}
	}
	return Slot{}, false
}

// Default 返回槽位的内置默认文本。
func Default(id string) string {
	return defaults[strings.TrimSpace(id)]
}

// SupportsScope 判断槽位是否允许在指定层级覆盖。
func SupportsScope(slot Slot, scope string) bool {
	for _, item := range slot.Scope {
		if item == scope {
			return true
		}
	}
	return false
}

// OverridableIDs 返回允许在指定层级覆盖的槽位 ID。
func OverridableIDs(scope string) []string {
	out := make([]string, 0, len(definitions))
	for _, slot := range definitions {
		if SupportsScope(slot, scope) {
			out = append(out, slot.ID)
		}
	}
	return out
}

// SanitizeOverrides 过滤掉未知槽位、不允许该层级覆盖的槽位，并去掉空白内容。
func SanitizeOverrides(scope string, raw map[string]string) map[string]string {
	out := map[string]string{}
	for id, content := range raw {
		slot, ok := ByID(id)
		if !ok || !SupportsScope(slot, scope) {
			continue
		}
		if strings.TrimSpace(content) == "" {
			continue
		}
		out[slot.ID] = content
	}
	return out
}

// MissingVariables 返回 content 里缺失的必需占位符。
func MissingVariables(slot Slot, content string) []string {
	missing := make([]string, 0, len(slot.Variables))
	for _, name := range slot.Variables {
		if !strings.Contains(content, "{{"+name+"}}") {
			missing = append(missing, name)
		}
	}
	return missing
}
