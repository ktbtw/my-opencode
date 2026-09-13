import type { SubagentResult } from "./types"

export type ContextWakeReason = "critical_result" | "batch_complete" | "main_waiting" | "user_input"

export function shouldWakeMainAgent(input: {
  result?: SubagentResult
  batchComplete?: boolean
  mainWaiting?: boolean
  userInput?: boolean
}): ContextWakeReason | undefined {
  if (input.userInput) return "user_input"
  if (input.result?.critical) return "critical_result"
  if (input.batchComplete) return "batch_complete"
  if (input.mainWaiting && input.result) return "main_waiting"
  return undefined
}

export function subagentResultMessage(result: SubagentResult) {
  return [
    result.status === "completed" ? "子代理完成" : "子代理未完成",
    `node_id: ${result.nodeID}`,
    `attempt: ${result.attempt}`,
    `status: ${result.status}`,
    `completed_at: ${result.completedAt}`,
    "",
    "<subagent_result>",
    result.summary,
    result.output ? `\n${result.output}` : "",
    result.error ? `\nerror: ${result.error}` : "",
    "</subagent_result>",
  ]
    .filter(Boolean)
    .join("\n")
}
