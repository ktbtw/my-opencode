import { isTerminalState, successfulState, type SubagentControl, type SubagentNode, type SubagentPlan, type SubagentResult } from "./types"

export class SubagentScheduler {
  readonly nodes: Map<string, SubagentNode>
  readonly maxConcurrent: number
  private results = new Set<string>()

  constructor(plan: SubagentPlan, input: { maxConcurrent?: number; now?: number; restored?: readonly SubagentNode[] } = {}) {
    const now = input.now ?? Date.now()
    this.maxConcurrent = input.maxConcurrent ?? 5
    if (!Number.isInteger(this.maxConcurrent) || this.maxConcurrent < 1) throw new Error("maxConcurrent must be positive")
    this.nodes = new Map(
      plan.nodes.map((node) => [node.id, { ...node, dependsOn: [...(node.dependsOn ?? [])], state: "planned", attempt: 0, createdAt: now, updatedAt: now }]),
    )
    this.advance(now)
    for (const restored of input.restored ?? []) {
      const node = this.nodes.get(restored.id)
      if (!node) continue
      Object.assign(node, {
        ...restored,
        dependsOn: [...(restored.dependsOn ?? node.dependsOn ?? [])],
        blockedBy: restored.blockedBy ? [...restored.blockedBy] : undefined,
        pendingInstructions: restored.pendingInstructions ? [...restored.pendingInstructions] : undefined,
      })
      if (isTerminalState(node.state)) this.results.add(node.resultKey ?? `${node.id}:${node.attempt}:${node.completedAt ?? 0}`)
    }
    this.advance(now)
  }

  snapshot() {
    return [...this.nodes.values()].map((node) => ({
      ...node,
      dependsOn: [...(node.dependsOn ?? [])],
      blockedBy: node.blockedBy ? [...node.blockedBy] : undefined,
      pendingInstructions: node.pendingInstructions ? [...node.pendingInstructions] : undefined,
    }))
  }

  claim(now = Date.now()) {
    this.advance(now)
    const capacity = this.maxConcurrent - this.running().length
    if (capacity <= 0) return []
    return this.snapshot()
      .filter((node) => node.state === "queued")
      .sort((a, b) => (b.priority ?? 50) - (a.priority ?? 50) || a.createdAt - b.createdAt)
      .slice(0, capacity)
      .map((node) => this.start(node.id, now))
  }

  start(id: string, now = Date.now()) {
    const node = this.require(id)
    if (node.state !== "queued") throw new Error(`node ${id} is not queued`)
    node.state = "running"
    node.attempt++
    node.startedAt = now
    node.updatedAt = now
    return { ...node }
  }

  complete(result: SubagentResult) {
    const key = `${result.nodeID}:${result.attempt}:${result.completedAt}`
    if (this.results.has(key)) return { applied: false, node: this.require(result.nodeID) }
    const node = this.require(result.nodeID)
    if (node.attempt !== result.attempt) return { applied: false, node }
    if (isTerminalState(node.state)) return { applied: false, node }
    this.results.add(key)
    node.state = result.status
    node.error = result.error
    node.resultKey = key
    node.completedAt = result.completedAt
    node.updatedAt = result.completedAt
    this.advance(result.completedAt)
    return { applied: true, node: { ...node } }
  }

  control(id: string, control: SubagentControl, now = Date.now()) {
    const node = this.require(id)
    if (isTerminalState(node.state) || node.state === "blocked") throw new Error(`node ${id} is terminal`)
    if (control.type === "pause") {
      if (node.state !== "running" && node.state !== "queued") throw new Error(`node ${id} cannot pause`)
      node.state = "paused"
    }
    if (control.type === "resume") {
      if (node.state !== "paused") throw new Error(`node ${id} cannot resume`)
      node.state = "queued"
    }
    if (control.type === "cancel") node.state = "cancelled"
    if (control.type === "instruction") node.pendingInstructions = [...(node.pendingInstructions ?? []), control.text]
    if (control.type === "priority") node.priority = control.priority
    if (control.type === "model") node.model = control.model
    node.updatedAt = now
    this.advance(now)
    return { ...node }
  }

  batchComplete() {
    return this.snapshot().every((node) => isTerminalState(node.state) || node.state === "blocked")
  }

  private advance(now: number) {
    for (const node of this.nodes.values()) {
      if (node.state !== "planned") continue
      const dependencies = (node.dependsOn ?? []).map((id) => this.require(id))
      const failed = dependencies.filter((dependency) => !successfulState(dependency.state) && (isTerminalState(dependency.state) || dependency.state === "blocked"))
      if (failed.length) {
        node.state = "blocked"
        node.blockedBy = failed.map((dependency) => dependency.id)
        node.updatedAt = now
        continue
      }
      if (dependencies.every((dependency) => dependency.state === "completed")) {
        node.state = "queued"
        node.updatedAt = now
      }
    }
  }

  private running() {
    return this.snapshot().filter((node) => node.state === "running")
  }

  private require(id: string) {
    const node = this.nodes.get(id)
    if (!node) throw new Error(`unknown node: ${id}`)
    return node
  }
}
