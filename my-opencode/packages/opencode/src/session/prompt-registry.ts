import { Effect } from "effect"

/**
 * 提示词注册表（设备侧读取端）。
 *
 * 控制台（后端）按「设备 Agent → 语义 Agent → 平台默认 → 内置默认」解析出各槽位文本，
 * launcher 原子写入本地文件，并在 opencode 配置里用 `prompt_slots`（slot_id → 文件路径）声明。
 * 这里按会话读取：文件存在且非空就用文件内容，否则回落到内置默认，
 * 因此独立 CLI（没有 prompt_slots）与未配置账号的行为完全不变；文件内容一变即生效，
 * 控制台改完不需要重启 Agent。
 */
export const resolvePromptSlot = Effect.fnUntraced(function* (
  slots: Record<string, string> | undefined,
  slotID: string,
  fallback: string,
) {
  const path = slots?.[slotID]
  if (!path) return fallback
  const file = Bun.file(path)
  if (!(yield* Effect.promise(() => file.exists()))) return fallback
  const content = yield* Effect.promise(() => file.text()).pipe(Effect.orElseSucceed(() => ""))
  if (!content.trim()) return fallback
  return content
})

export * as PromptRegistry from "./prompt-registry"
