import { SubagentScheduler } from "./scheduler"
import type { SubagentControl, SubagentNode, SubagentPlan, SubagentResult } from "./types"

export type SubagentLaunch = (node: SubagentNode) => Promise<{ sessionID?: string } | void>
export type SubagentCancel = (node: SubagentNode) => Promise<void>
export type SubagentInstruction = (node: SubagentNode, text: string) => Promise<void>
export type SubagentNodeChanged = (node: SubagentNode) => Promise<void> | void

export class SubagentRunner {
  readonly scheduler: SubagentScheduler
  private readonly timers = new Map<string, ReturnType<typeof setTimeout>>()

  constructor(
    plan: SubagentPlan,
    private readonly callbacks: {
      launch: SubagentLaunch
      cancel: SubagentCancel
      instruction?: SubagentInstruction
      changed?: SubagentNodeChanged
    },
    input: { maxConcurrent?: number; now?: number; restored?: readonly SubagentNode[] } = {},
  ) {
    this.scheduler = new SubagentScheduler(plan, input)
  }

  static restore(
    plan: SubagentPlan,
    restored: readonly SubagentNode[],
    callbacks: ConstructorParameters<typeof SubagentRunner>[1],
    input: { maxConcurrent?: number; now?: number } = {},
  ) {
    return new SubagentRunner(plan, callbacks, { ...input, restored })
  }

  snapshot() {
    return this.scheduler.snapshot()
  }

  async start() {
    await this.publishAll()
    await this.dispatch()
  }

  async complete(result: SubagentResult) {
    const applied = this.scheduler.complete(result)
    if (!applied.applied) return applied
    this.clearTimeout(result.nodeID)
    await this.publishAll()
    await this.dispatch()
    return applied
  }

  async control(id: string, control: SubagentControl) {
    const current = this.node(id)
    const before: SubagentNode = {
      ...current,
      dependsOn: [...(current.dependsOn ?? [])],
      blockedBy: current.blockedBy ? [...current.blockedBy] : undefined,
      pendingInstructions: current.pendingInstructions ? [...current.pendingInstructions] : undefined,
    }
    const changed = this.scheduler.control(id, control)
    if (control.type === "pause" || control.type === "cancel") {
      this.clearTimeout(id)
      if (before.state === "running") await this.callbacks.cancel(before)
    }
    if (control.type === "instruction" && before.state === "running" && this.callbacks.instruction) {
      await this.callbacks.instruction(before, control.text)
    }
    await this.publish(changed)
    if (control.type === "resume" || control.type === "priority" || control.type === "model") await this.dispatch()
    return changed
  }

  async reconcile(
    resolve: (node: SubagentNode) => Promise<SubagentResult | "running" | "unknown">,
    recovered?: (result: SubagentResult) => Promise<void> | void,
  ) {
    for (const node of this.snapshot().filter((item) => item.state === "running")) {
      const resolved = await resolve(node)
      if (resolved === "running") continue
      if (resolved === "unknown") {
        const live = this.node(node.id)
        // The previous process may have disappeared while the child session
        // itself remained durable. Put the node back in the scheduler so the
        // launch callback can continue that same child session.
        live.state = "queued"
        live.updatedAt = Date.now()
        this.clearTimeout(live.id)
        await this.publish(live)
        continue
      }
      await this.complete(resolved)
      await recovered?.(resolved)
    }
    await this.dispatch()
  }

  async dispose() {
    for (const timer of this.timers.values()) clearTimeout(timer)
    this.timers.clear()
  }

  private async dispatch() {
    const claimed = this.scheduler.claim()
    await Promise.all(claimed.map((node) => this.launch(node)))
  }

  private async launch(node: SubagentNode) {
    try {
      const launched = await this.callbacks.launch(node)
      const live = this.node(node.id)
      if (live.state !== "running" || live.attempt !== node.attempt) return
      live.sessionID = launched?.sessionID
      this.armTimeout(live)
      await this.publish(live)
    } catch (error) {
      await this.complete({
        nodeID: node.id,
        attempt: node.attempt,
        status: "failed",
        summary: node.prompt,
        error: error instanceof Error ? error.message : String(error),
        completedAt: Date.now(),
      })
    }
  }

  private armTimeout(node: SubagentNode) {
    if (!node.timeoutMS) return
    this.clearTimeout(node.id)
    this.timers.set(
      node.id,
      setTimeout(() => {
        void this.timeout(node.id, node.attempt)
      }, node.timeoutMS),
    )
  }

  private async timeout(id: string, attempt: number) {
    const node = this.node(id)
    if (node.state !== "running" || node.attempt !== attempt) return
    const applied = this.scheduler.complete({
      nodeID: id,
      attempt,
      status: "timed_out",
      summary: node.prompt,
      error: "subagent timed out",
      completedAt: Date.now(),
    })
    if (!applied.applied) return
    this.clearTimeout(id)
    await this.publishAll()
    await this.callbacks.cancel(node)
    await this.dispatch()
  }

  private node(id: string) {
    const node = this.scheduler.nodes.get(id)
    if (!node) throw new Error(`unknown node: ${id}`)
    return node
  }

  private clearTimeout(id: string) {
    const timer = this.timers.get(id)
    if (timer) clearTimeout(timer)
    this.timers.delete(id)
  }

  private async publish(node: SubagentNode) {
    await this.callbacks.changed?.({
      ...node,
      dependsOn: [...(node.dependsOn ?? [])],
      blockedBy: node.blockedBy ? [...node.blockedBy] : undefined,
      pendingInstructions: node.pendingInstructions ? [...node.pendingInstructions] : undefined,
    })
  }

  private async publishAll() {
    for (const node of this.snapshot()) await this.publish(node)
  }
}
