/**
 * 破甲协议（DeepSeek 破甲模式）在设备侧的识别。
 *
 * 后端 `applyDeepSeekJailbreak` 把协议正文前置到任务的系统提示词，首行特征串与
 * `backend/internal/api/deepseek_jailbreak.go` 的 `deepSeekJailbreakMarker` 必须一致。
 * 命中后有两个下游行为：
 *   1. `session/llm.ts` 把整段置顶到基础提示词之前；
 *   2. `session/tools.ts` 让 MCP 工具常驻（不再折叠成 mcp_search/mcp_call）——
 *      破甲协议的效果依赖请求里有足量的工具 schema 作为"真实运行环境"的证据。
 */
export const ARMOR_PROTOCOL_MARKER = "操作者工作区运行约定"

/** 本次请求的系统提示词里是否携带破甲协议。 */
export function isArmorActive(system?: string) {
  return system?.includes(ARMOR_PROTOCOL_MARKER) ?? false
}

export * as Armor from "./armor"
