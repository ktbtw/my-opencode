import { isSubagentRoleAllowed } from "./policy"
import type { SubagentPlan, SubagentPlanNode } from "./types"

export class PlanError extends Error {
  constructor(
    readonly code: "duplicate_node" | "unknown_dependency" | "cycle" | "invalid_node" | "forbidden_role" | "too_many_nodes",
    message: string,
  ) {
    super(message)
  }
}

export function validateSubagentPlan(plan: SubagentPlan, input?: { maxNodes?: number; roles?: readonly import("./types").SubagentRole[] }) {
  if (!plan.id.trim()) throw new PlanError("invalid_node", "plan id is required")
  if (plan.nodes.length === 0) throw new PlanError("invalid_node", "plan needs at least one node")
  if (plan.nodes.length > (input?.maxNodes ?? 20)) throw new PlanError("too_many_nodes", "plan exceeds node limit")
  const ids = new Set<string>()
  for (const node of plan.nodes) {
    validateNode(node)
    if (ids.has(node.id)) throw new PlanError("duplicate_node", `duplicate node: ${node.id}`)
    if (!isSubagentRoleAllowed(plan.semanticAgentID, node.role, input?.roles)) {
      throw new PlanError("forbidden_role", `role ${node.role} is not available to ${plan.semanticAgentID}`)
    }
    ids.add(node.id)
  }
  for (const node of plan.nodes) {
    for (const dependency of node.dependsOn ?? []) {
      if (!ids.has(dependency)) throw new PlanError("unknown_dependency", `${node.id} depends on ${dependency}`)
      if (dependency === node.id) throw new PlanError("cycle", `${node.id} depends on itself`)
    }
  }
  const waiting = new Set(plan.nodes.map((node) => node.id))
  const resolved = new Set<string>()
  while (waiting.size) {
    const ready = plan.nodes.filter((node) => waiting.has(node.id) && (node.dependsOn ?? []).every((id) => resolved.has(id)))
    if (!ready.length) throw new PlanError("cycle", "plan dependencies contain a cycle")
    for (const node of ready) {
      waiting.delete(node.id)
      resolved.add(node.id)
    }
  }
  return plan
}

function validateNode(node: SubagentPlanNode) {
  if (!node.id.trim() || !node.prompt.trim()) throw new PlanError("invalid_node", "node id and prompt are required")
  if (node.priority !== undefined && (!Number.isInteger(node.priority) || node.priority < 0 || node.priority > 100)) {
    throw new PlanError("invalid_node", `invalid priority for ${node.id}`)
  }
  if (node.timeoutMS !== undefined && (!Number.isInteger(node.timeoutMS) || node.timeoutMS < 1_000)) {
    throw new PlanError("invalid_node", `invalid timeout for ${node.id}`)
  }
}
