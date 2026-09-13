import { validateSubagentPlan } from "./planner"
import type { SubagentNode, SubagentPlan } from "./types"

export function restoredPlan(input: {
  id: string
  semanticAgentID: string
  nodes: readonly SubagentNode[]
}): SubagentPlan {
  return validateSubagentPlan({
    id: input.id,
    semanticAgentID: input.semanticAgentID,
    nodes: input.nodes.map((node) => ({
      id: node.id,
      role: node.role,
      prompt: node.prompt,
      dependsOn: [...(node.dependsOn ?? [])],
      priority: node.priority,
      model: node.model,
      critical: node.critical,
      timeoutMS: node.timeoutMS,
    })),
  })
}

export function recoveryCandidates(nodes: readonly SubagentNode[]) {
  return nodes.filter((node) => node.state === "queued" || node.state === "running")
}
