import { describe, expect, test } from "bun:test"
import { shouldWakeMainAgent, SubagentRunner, type SubagentPlan } from "../../src/orchestration"

const plan: SubagentPlan = {
  id: "integration-five-nodes",
  semanticAgentID: "coding-assistant",
  nodes: [
    { id: "scan", role: "repo-explorer", prompt: "scan" },
    { id: "tests", role: "test-runner", prompt: "tests" },
    { id: "review", role: "code-reviewer", prompt: "review", dependsOn: ["scan"] },
    { id: "blocked", role: "implementation-planner", prompt: "plan", dependsOn: ["tests"] },
    { id: "final", role: "dependency-researcher", prompt: "research", dependsOn: ["review"] },
  ],
}

describe("five-node orchestration fixture", () => {
  test("preserves dependency, failure, recovery and wake invariants", async () => {
    const launched: string[] = []
    const changed: string[] = []
    const runner = new SubagentRunner(
      plan,
      {
        launch: async (node) => (launched.push(node.id), { sessionID: `child-${node.id}` }),
        cancel: async () => {},
        changed: (node) => void changed.push(`${node.id}:${node.state}`),
      },
      { maxConcurrent: 5, now: 1 },
    )
    await runner.start()
    expect(launched.sort()).toEqual(["scan", "tests"])

    await runner.complete({ nodeID: "tests", attempt: 1, status: "failed", summary: "failed", completedAt: 2 })
    expect(runner.snapshot().find((node) => node.id === "blocked")).toMatchObject({ state: "blocked", blockedBy: ["tests"] })
    expect(shouldWakeMainAgent({ result: { nodeID: "tests", attempt: 1, status: "failed", summary: "failed", critical: true, completedAt: 2 }, batchComplete: false })).toBe("critical_result")

    await runner.complete({ nodeID: "scan", attempt: 1, status: "completed", summary: "done", completedAt: 3 })
    expect(launched).toContain("review")
    await runner.complete({ nodeID: "review", attempt: 1, status: "completed", summary: "done", completedAt: 4 })
    expect(launched).toContain("final")
    await runner.complete({ nodeID: "final", attempt: 1, status: "completed", summary: "done", completedAt: 5 })

    expect(runner.scheduler.batchComplete()).toBe(true)
    expect(changed).toEqual(expect.arrayContaining(["blocked:blocked", "final:completed"]))
    expect(shouldWakeMainAgent({ result: { nodeID: "final", attempt: 1, status: "completed", summary: "done", completedAt: 5 }, batchComplete: true })).toBe("batch_complete")
  })

  test("records independent success, failure, timeout, and blocked dependents without exceeding the configured capacity", async () => {
    const launched: string[] = []
    const cancelled: string[] = []
    const runner = new SubagentRunner(
      {
        id: "integration-failure-timeout",
        semanticAgentID: "coding-assistant",
        nodes: [
          { id: "slow-scan", role: "repo-explorer", prompt: "scan", timeoutMS: 10 },
          { id: "failing-tests", role: "test-runner", prompt: "test" },
          { id: "independent-review", role: "code-reviewer", prompt: "review" },
          { id: "after-scan", role: "implementation-planner", prompt: "plan", dependsOn: ["slow-scan"] },
          { id: "after-tests", role: "dependency-researcher", prompt: "research", dependsOn: ["failing-tests"] },
        ],
      },
      {
        launch: async (node) => (launched.push(node.id), { sessionID: `child-${node.id}` }),
        cancel: async (node) => void cancelled.push(node.id),
      },
      { maxConcurrent: 5, now: 1 },
    )

    await runner.start()
    expect(launched.sort()).toEqual(["failing-tests", "independent-review", "slow-scan"])
    await runner.complete({ nodeID: "failing-tests", attempt: 1, status: "failed", summary: "test failure", completedAt: 2 })
    await runner.complete({ nodeID: "independent-review", attempt: 1, status: "completed", summary: "reviewed", completedAt: 3 })
    await new Promise((resolve) => setTimeout(resolve, 30))

    expect(cancelled).toEqual(["slow-scan"])
    expect(runner.snapshot()).toEqual(expect.arrayContaining([
      expect.objectContaining({ id: "slow-scan", state: "timed_out" }),
      expect.objectContaining({ id: "after-scan", state: "blocked", blockedBy: ["slow-scan"] }),
      expect.objectContaining({ id: "after-tests", state: "blocked", blockedBy: ["failing-tests"] }),
    ]))
    expect(runner.scheduler.batchComplete()).toBe(true)
  })
})
