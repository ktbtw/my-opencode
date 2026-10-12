package api

import (
	_ "embed"
	"strings"
)

// deepSeekJailbreakSettingKey 是 agent 级「破甲模式」开关的存储键。
// 值 "true" 表示开启；缺省 / 空 = 关闭（正常不会启动破甲）。
const deepSeekJailbreakSettingKey = "deepseek_jailbreak"

// deepSeekJailbreakMarker 是本协议的首行特征串。
// 必须与 my-opencode/packages/opencode/src/session/llm.ts 里的 BREAK_PROTOCOL_MARKER
// 保持一致：opencode 侧据此把本协议整体排到基础提示词（PROMPT_UNIFIED）之前，
// 等价于 hanshuang 破甲包把协议放到 order=-90 顶格的做法。
const deepSeekJailbreakMarker = "操作者工作区运行约定"

//go:embed builtin/deepseek_jailbreak.md
var deepSeekJailbreakPrompt string

// isDeepSeekModelRef 判定任务模型引用（形如 "providerID/modelID"）是否属于 DeepSeek 系列。
func isDeepSeekModelRef(modelRef string) bool {
	lower := strings.ToLower(strings.TrimSpace(modelRef))
	if lower == "" {
		return false
	}
	return strings.Contains(lower, "deepseek")
}

// jailbreakEnabledFor 读取该 agent 的破甲开关。读失败按关闭处理。
func (a *API) jailbreakEnabledFor(operatorID int64, agentID string) bool {
	value, err := a.getSettingValue(operatorID, agentID, deepSeekJailbreakSettingKey)
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(value), "true")
}

// applyDeepSeekJailbreak 在「开关开启」且「模型为 DeepSeek 系列」两个条件同时成立时，
// 把破甲协议整体前置到系统提示词最前面（置顶，而不是追加到末尾），返回是否已生效。
// 任一条件不成立时原样返回 base，避免影响非 DeepSeek 模型或未开启破甲的会话。
func applyDeepSeekJailbreak(base, modelRef string, enabled bool) (string, bool) {
	if !enabled || !isDeepSeekModelRef(modelRef) {
		return base, false
	}
	protocol := strings.TrimSpace(deepSeekJailbreakPrompt)
	if protocol == "" {
		return base, false
	}
	if trimmed := strings.TrimSpace(base); trimmed != "" {
		return protocol + "\n\n" + trimmed, true
	}
	return protocol, true
}