package api

import (
	"strings"

	"relay-server/internal/model"
)

// 模型专属提示词的包装标记。后端按会话实际模型匹配到预设后，把整段提示词用这对标记包起来
// 前置到系统提示词最前；opencode 侧（my-opencode/packages/opencode/src/session/llm.ts）
// 据此把整段内容取出来置顶到基础提示词之前，而不是留在末尾被压过去。
// 两侧标记必须保持一致。
const (
	modelPromptWrapperStart = "<!-- codex:model-prompt -->"
	modelPromptWrapperEnd   = "<!-- /codex:model-prompt -->"
)

// selectedModelSettingKey 是客户端按 agent 持久化的当前模型引用（providerID/modelID）。
// 客户端每次切换模型都会写这个设置，任务 metadata 里缺模型时用它兜底。
const selectedModelSettingKey = "selected_model"

// matchModelPromptPreset 按 launcher 端一致的规则匹配预设：按顺序先匹配优先，
// 供应商与模型名都以「包含」比较，任一字段为空则不作过滤条件。
func matchModelPromptPreset(presets []model.SemanticAgentModelPromptPreset, modelRef string) (string, bool) {
	provider, modelID := splitPresetModelRef(modelRef)
	for _, preset := range presets {
		prompt := strings.TrimSpace(preset.Prompt)
		if !preset.Enabled || prompt == "" {
			continue
		}
		wantProvider := strings.ToLower(strings.TrimSpace(preset.Provider))
		if wantProvider != "" && !strings.Contains(provider, wantProvider) {
			continue
		}
		pattern := strings.ToLower(strings.TrimSpace(preset.ModelPattern))
		if pattern != "" && !strings.Contains(modelID, pattern) {
			continue
		}
		return prompt, true
	}
	return "", false
}

// splitPresetModelRef 把 "providerID/modelID" 拆成小写的供应商与模型名；
// 没有斜杠时整体按模型名处理，与 launcher 的 splitProviderModel 行为一致。
func splitPresetModelRef(modelRef string) (string, string) {
	lower := strings.ToLower(strings.TrimSpace(modelRef))
	parts := strings.SplitN(lower, "/", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	}
	return "", lower
}

// modelPromptProfile 读取语义 Agent 的模型提示词预设与生效模式。
// 全局预设按控制台的语义前置（先匹配优先），且不展开技能引用，避免每次建任务都做重活。
func (a *API) modelPromptProfile(semanticAgentID string) (model.SemanticAgentProfile, bool) {
	semanticAgentID = strings.TrimSpace(semanticAgentID)
	if semanticAgentID == "" {
		return model.SemanticAgentProfile{}, false
	}
	items, err := a.store.ListSemanticAgents(false)
	if err != nil {
		return model.SemanticAgentProfile{}, false
	}
	var profile model.SemanticAgentProfile
	found := false
	for _, item := range items {
		if item.ID == semanticAgentID {
			profile = item
			found = true
			break
		}
	}
	if !found {
		return model.SemanticAgentProfile{}, false
	}
	if globals, err := a.store.ListSemanticAgentModelPromptPresets(false); err == nil && len(globals) > 0 {
		merged := make([]model.SemanticAgentModelPromptPreset, 0, len(globals)+len(profile.ModelPromptPresets))
		merged = append(merged, globals...)
		merged = append(merged, profile.ModelPromptPresets...)
		profile.ModelPromptPresets = merged
	}
	return profile, true
}

// modelPromptForTask 返回本次任务应注入的模型专属提示词。
// 生效模式为 model/both 时按会话实际模型匹配预设；模型优先取任务 metadata，
// 缺省回退到客户端持久化的 selected_model，保证老会话与排队任务也能命中。
func (a *API) modelPromptForTask(operatorID int64, agentID, semanticAgentID, modelRef string) (string, bool) {
	if strings.TrimSpace(semanticAgentID) == "" {
		return "", false
	}
	modelRef = strings.TrimSpace(modelRef)
	if modelRef == "" {
		if selected, err := a.getSettingValue(operatorID, agentID, selectedModelSettingKey); err == nil {
			modelRef = strings.TrimSpace(selected)
		}
	}
	if modelRef == "" {
		return "", false
	}
	profile, ok := a.modelPromptProfile(semanticAgentID)
	if !ok {
		return "", false
	}
	switch strings.ToLower(strings.TrimSpace(profile.PromptMode)) {
	case "model", "both":
	default:
		return "", false
	}
	return matchModelPromptPreset(profile.ModelPromptPresets, modelRef)
}

// applyModelPrompt 把匹配到的模型提示词包装后前置到系统提示词最前，返回是否已生效。
func applyModelPrompt(base, prompt string) (string, bool) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return base, false
	}
	wrapped := modelPromptWrapperStart + "\n" + prompt + "\n" + modelPromptWrapperEnd
	if trimmed := strings.TrimSpace(base); trimmed != "" {
		return wrapped + "\n\n" + trimmed, true
	}
	return wrapped, true
}
