import { describe, expect, test } from "bun:test"
import {
  PlanError,
  SubagentScheduler,
  allowedSubagentRoles,
  readonlySubagentTools,
  shouldWakeMainAgent,
  subagentResultMessage,
  validateSubagentPlan,
  type SubagentPlan,
} from "../../src/orchestration"

const plan = (nodes: SubagentPlan["nodes"]): SubagentPlan => ({
  id: "plan-1",
  semanticAgentID: "coding-assistant",
  nodes,
})

describe("subagent orchestration", () => {
  test("maps built-in semantic agents to isolated role pools", () => {
    expect(allowedSubagentRoles("coding-assistant")).toContain("repo-explorer")
    expect(allowedSubagentRoles("reverse-android")).toEqual(
      expect.arrayContaining(["apk-scout", "native-tracer", "verification-runner"]),
    )
    expect(allowedSubagentRoles("reverse-mac")).toContain("codesign-entitlements-auditor")
    expect(allowedSubagentRoles("custom-agent")).toEqual(allowedSubagentRoles("coding-assistant"))
  })

  test("derives a read-only tool policy without weakening parent restrictions", () => {
    expect(readonlySubagentTools({ read: "allow", edit: "ask", bash: "ask", publish: "allow" })).toEqual({
      read: "allow",
      edit: "deny",
      bash: "ask",
      publish: "deny",
    })
  })

  test("rejects duplicate nodes, missing dependencies, cycles and forbidden roles", () => {
    expect(() => validateSubagentPlan(plan([{ id: "a", role: "repo-explorer", prompt: "inspect" }, { id: "a", role: "test-runner", prompt: "test" }]))).toThrow(PlanError)
    expect(() => validateSubagentPlan(plan([{ id: "a", role: "repo-explorer", prompt: "inspect", dependsOn: ["missing"] }]))).toThrow("depends on missing")
    expect(() => validateSubagentPlan(plan([{ id: "a", role: "repo-explorer", prompt: "inspect", dependsOn: ["b"] }, { id: "b", role: "test-runner", prompt: "test", dependsOn: ["a"] }]))).toThrow("cycle")
    expect(() => validateSubagentPlan(plan([{ id: "a", role: "apk-scout", prompt: "inspect" }]))).toThrow("not available")
  })

  test("schedules ready nodes by priority with a hard concurrency limit", () => {
    const scheduler = new SubagentScheduler(
      plan([
        { id: "low", role: "repo-explorer", prompt: "low", priority: 1 },
        { id: "high", role: "test-runner", prompt: "high", priority: 99 },
        { id: "third", role: "code-reviewer", prompt: "third", priority: 50 },
      ]),
      { maxConcurrent: 2, now: 1 },
    )
    expect(scheduler.claim(2).map((node) => node.id)).toEqual(["high", "third"])
    expect(scheduler.claim(3)).toEqual([])
  })

  test("unblocks dependents after successful results and blocks them after failures", () => {
    const scheduler = new SubagentScheduler(
      plan([
        { id: "root", role: "repo-explorer", prompt: "root" },
        { id: "child", role: "test-runner", prompt: "child", dependsOn: ["root"] },
        { id: "failure", role: "code-reviewer", prompt: "failure" },
        { id: "blocked", role: "dependency-researcher", prompt: "blocked", dependsOn: ["failure"] },
      ]),
      { now: 1 },
    )
    const initial = scheduler.claim(2)
    expect(initial.map((node) => node.id)).toEqual(["root", "failure"])
    expect(scheduler.complete({ nodeID: "root", attempt: 1, status: "completed", summary: "done", completedAt: 3 }).applied).toBe(true)
    expect(scheduler.claim(4).map((node) => node.id)).toEqual(["child"])
    scheduler.complete({ nodeID: "failure", attempt: 1, status: "failed", summary: "bad", error: "bad", completedAt: 5 })
    expect(scheduler.snapshot().find((node) => node.id === "blocked")).toMatchObject({ state: "blocked", blockedBy: ["failure"] })
  })

  test("deduplicates terminal results and preserves model changes for a future round", () => {
    const scheduler = new SubagentScheduler(plan([{ id: "node", role: "repo-explorer", prompt: "inspect" }]), { now: 1 })
    scheduler.claim(2)
    scheduler.control("node", { type: "model", model: { providerID: "p", modelID: "m", variant: "high" } }, 3)
    expect(scheduler.complete({ nodeID: "node", attempt: 1, status: "completed", summary: "done", completedAt: 4 }).applied).toBe(true)
    expect(scheduler.complete({ nodeID: "node", attempt: 1, status: "completed", summary: "done", completedAt: 4 }).applied).toBe(false)
    expect(scheduler.snapshot()[0]).toMatchObject({ state: "completed", model: { providerID: "p", modelID: "m", variant: "high" } })
  })

  test("supports pause, resume, cancellation and batch completion", () => {
    const scheduler = new SubagentScheduler(plan([{ id: "node", role: "repo-explorer", prompt: "inspect" }]), { now: 1 })
    scheduler.control("node", { type: "pause" }, 2)
    expect(scheduler.snapshot()[0]?.state).toBe("paused")
    scheduler.control("node", { type: "resume" }, 3)
    expect(scheduler.claim(4)[0]?.state).toBe("running")
    scheduler.control("node", { type: "cancel" }, 5)
    expect(scheduler.batchComplete()).toBe(true)
  })

  test("only wakes the main agent for deliberate context boundaries", () => {
    expect(shouldWakeMainAgent({ result: { nodeID: "a", attempt: 1, status: "completed", summary: "ordinary", completedAt: 1 } })).toBeUndefined()
    expect(shouldWakeMainAgent({ result: { nodeID: "a", attempt: 1, status: "completed", summary: "key", completedAt: 1, critical: true } })).toBe("critical_result")
    expect(shouldWakeMainAgent({ batchComplete: true })).toBe("batch_complete")
    expect(shouldWakeMainAgent({ userInput: true, batchComplete: true })).toBe("user_input")
    expect(subagentResultMessage({ nodeID: "a", attempt: 1, status: "completed", summary: "done", output: "evidence", completedAt: 1 })).toContain("<subagent_result>")
  })
})
