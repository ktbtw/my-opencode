// 模型专属提示词（控制台「模型提示词预设」）的包装标记。
// 后端按会话实际模型匹配到预设后，把整段提示词用这对标记包起来放进系统提示词；
// 这里负责把它取出来并置顶到基础提示词之前。
// 必须与 backend/internal/api/model_prompt.go 的 modelPromptWrapperStart/End 保持一致。
export const MODEL_PROMPT_START = "<!-- codex:model-prompt -->"
export const MODEL_PROMPT_END = "<!-- /codex:model-prompt -->"

export type ExtractedModelPrompt = {
  // 后端按模型匹配后注入的整段提示词，未注入时为 undefined。
  modelPrompt?: string
  // 剥掉模型提示词后剩下的系统提示词（全局/项目级提示词等）。
  rest?: string
}

// extractModelPrompt 从系统提示词里取出被包装的模型专属提示词。
// 没有包装标记时原样返回，保持未注入时的行为不变。
export function extractModelPrompt(system?: string): ExtractedModelPrompt {
  const raw = system ?? ""
  const start = raw.indexOf(MODEL_PROMPT_START)
  const end = start < 0 ? -1 : raw.indexOf(MODEL_PROMPT_END, start + MODEL_PROMPT_START.length)
  if (start < 0 || end < 0) {
    const rest = raw.trim()
    return rest ? { rest } : {}
  }
  const modelPrompt = raw.slice(start + MODEL_PROMPT_START.length, end).trim() || undefined
  const before = raw.slice(0, start).trim()
  const after = raw.slice(end + MODEL_PROMPT_END.length).trim()
  const rest = [before, after].filter(Boolean).join("\n\n") || undefined
  return { modelPrompt, rest }
}
