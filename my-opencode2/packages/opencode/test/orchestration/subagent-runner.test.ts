import { describe, expect, test } from "bun:test"
import { SubagentRunner, type SubagentPlan } from "../../src/orchestration"

const plan = (nodes: SubagentPlan["nodes"]): SubagentPlan => ({
  id: "plan-runner",
  semanticAgentID: "coding-assistant",
  nodes,
})

describe("SubagentRunner", () => {
  test("runs ready nodes concurrently and dispatches dependents after their dependency completes", async () => {
    const launched: string[] = []
    const runner = new SubagentRunner(
      plan([
        { id: "root", role: "repo-explorer", prompt: "inspect" },
        { id: "peer", role: "test-runner", prompt: "test" },
        { id: "child", role: "code-reviewer", prompt: "review", dependsOn: ["root"] },
      ]),
      { launch: async (node) => (launched.push(node.id), { sessionID: `session-${node.id}` }), cancel: async () => {} },
      { maxConcurrent: 2, now: 1 },
    )

    await runner.start()
    expect(launched).toEqual(["root", "peer"])
    await runner.complete({ nodeID: "root", attempt: 1, status: "completed", summary: "done", completedAt: 2 })
    expect(launched).toEqual(["root", "peer", "child"])
    expect(runner.snapshot().find((node) => node.id === "child")).toMatchObject({ state: "running", sessionID: "session-child" })
  })

  test("pauses a running node, cancels its active run, and relaunches it on resume", async () => {
    const launched: string[] = []
    const cancelled: string[] = []
    const runner = new SubagentRunner(
      plan([{ id: "node", role: "repo-explorer", prompt: "inspect" }]),
      {
        launch: async (node) => (launched.push(`${node.id}:${node.attempt}`), { sessionID: `session-${node.attempt}` }),
        cancel: async (node) => void cancelled.push(`${node.id}:${node.attempt}`),
      },
      { now: 1 },
    )

    await runner.start()
    await runner.control("node", { type: "pause" })
    expect(cancelled).toEqual(["node:1"])
    expect(runner.snapshot()[0]?.state).toBe("paused")
    await runner.control("node", { type: "resume" })
    expect(launched).toEqual(["node:1", "node:2"])
  })

  test("records instructions for the next round and forwards them to a running child", async () => {
    const delivered: string[] = []
    const runner = new SubagentRunner(
      plan([{ id: "node", role: "repo-explorer", prompt: "inspect" }]),
      {
        launch: async () => ({ sessionID: "session-node" }),
        cancel: async () => {},
        instruction: async (_, text) => void delivered.push(text),
      },
      { now: 1 },
    )

    await runner.start()
    await runner.control("node", { type: "instruction", text: "also inspect tests" })
    expect(delivered).toEqual(["also inspect tests"])
    expect(runner.snapshot()[0]?.pendingInstructions).toEqual(["also inspect tests"])
  })

  test("marks a timed out node terminal, cancels it, and blocks dependents", async () => {
    const cancelled: string[] = []
    const runner = new SubagentRunner(
      plan([
        { id: "slow", role: "repo-explorer", prompt: "inspect", timeoutMS: 10 },
        { id: "after", role: "test-runner", prompt: "test", dependsOn: ["slow"] },
      ]),
      { launch: async () => ({ sessionID: "session-slow" }), cancel: async (node) => void cancelled.push(node.id) },
      { now: 1 },
    )

    await runner.start()
    await new Promise((resolve) => setTimeout(resolve, 30))
    expect(cancelled).toEqual(["slow"])
    expect(runner.snapshot()).toEqual(expect.arrayContaining([expect.objectContaining({ id: "slow", state: "timed_out" }), expect.objectContaining({ id: "after", state: "blocked", blockedBy: ["slow"] })]))
  })

  test("reconciles lost running sessions to unknown without restarting them", async () => {
    const launched: string[] = []
    const runner = new SubagentRunner(
      plan([{ id: "node", role: "repo-explorer", prompt: "inspect" }]),
      { launch: async (node) => void launched.push(node.id), cancel: async () => {} },
      { now: 1 },
    )

    await runner.start()
    await runner.reconcile(async () => "unknown")
    expect(runner.snapshot()[0]?.state).toBe("unknown")
    expect(launched).toEqual(["node"])
  })

  test("restores persisted nodes without rerunning an in-flight child session", async () => {
    const launched: string[] = []
    const restoredAt = 50
    const runner = SubagentRunner.restore(
      plan([
        { id: "live", role: "repo-explorer", prompt: "inspect" },
        { id: "queued", role: "test-runner", prompt: "test" },
      ]),
      [
        {
          id: "live",
          role: "repo-explorer",
          prompt: "inspect",
          state: "running",
          attempt: 1,
          createdAt: 1,
          updatedAt: restoredAt,
          startedAt: 2,
          sessionID: "child-live",
        },
        {
          id: "queued",
          role: "test-runner",
          prompt: "test",
          state: "queued",
          attempt: 0,
          createdAt: 1,
          updatedAt: restoredAt,
        },
      ],
      { launch: async (node) => (launched.push(node.id), { sessionID: `child-${node.id}` }), cancel: async () => {} },
      { now: restoredAt },
    )

    await runner.start()
    expect(launched).toEqual(["queued"])
    await runner.reconcile(async (node) => (node.id === "live" ? "unknown" : "running"))
    expect(runner.snapshot().find((node) => node.id === "live")).toMatchObject({ state: "unknown", sessionID: "child-live" })
    expect(launched).toEqual(["queued"])
  })
})
