export type SubagentNodeState =
  | "planned"
  | "queued"
  | "running"
  | "paused"
  | "completed"
  | "failed"
  | "cancelled"
  | "timed_out"
  | "unknown"
  | "recovering"
  | "blocked"

export type SubagentRole =
  | "repo-explorer"
  | "implementation-planner"
  | "test-runner"
  | "code-reviewer"
  | "dependency-researcher"
  | "apk-scout"
  | "manifest-mapper"
  | "native-tracer"
  | "protocol-analyst"
  | "verification-runner"
  | "ipa-scout"
  | "macho-mapper"
  | "objc-swift-tracer"
  | "runtime-observer"
  | "pe-scout"
  | "import-export-mapper"
  | "control-flow-analyst"
  | "installer-analyzer"
  | "bundle-scout"
  | "codesign-entitlements-auditor"
  | "asset-scout"
  | "js-wasm-analyst"
  | "request-flow-mapper"
  | "browser-observer"

export type SubagentPlanNode = {
  id: string
  role: SubagentRole
  prompt: string
  dependsOn?: string[]
  priority?: number
  model?: { providerID: string; modelID: string; variant?: string }
  critical?: boolean
  timeoutMS?: number
}

export type SubagentPlan = {
  id: string
  semanticAgentID: string
  nodes: SubagentPlanNode[]
}

export type SubagentNode = SubagentPlanNode & {
  state: SubagentNodeState
  attempt: number
  createdAt: number
  updatedAt: number
  startedAt?: number
  completedAt?: number
  sessionID?: string
  error?: string
  resultKey?: string
  blockedBy?: string[]
  pendingInstructions?: string[]
}

export type SubagentResult = {
  nodeID: string
  attempt: number
  status: Extract<SubagentNodeState, "completed" | "failed" | "cancelled" | "timed_out">
  summary: string
  output?: string
  error?: string
  artifacts?: Array<{ id: string; path: string; name?: string }>
  completedAt: number
  critical?: boolean
}

export type SubagentControl =
  | { type: "pause" }
  | { type: "resume" }
  | { type: "cancel" }
  | { type: "instruction"; text: string }
  | { type: "priority"; priority: number }
  | { type: "model"; model: NonNullable<SubagentPlanNode["model"]> }

export const terminalStates = new Set<SubagentNodeState>(["completed", "failed", "cancelled", "timed_out"])

export const successfulState = (state: SubagentNodeState) => state === "completed"

export const isTerminalState = (state: SubagentNodeState) => terminalStates.has(state)
