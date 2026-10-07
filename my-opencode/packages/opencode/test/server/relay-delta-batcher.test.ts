import { describe, expect, test } from "bun:test"
import { RelayDeltaBatcher } from "../../src/server/relay-delta-batcher"

function fixture() {
  const received: { type: string; payload: Record<string, unknown>; requestID: string }[] = []
  const batch = new RelayDeltaBatcher((event) => { received.push(event); return true })
  const send = (content: string, field = "text", task = "task") => batch.send({ type: "task.delta", requestID: "id", payload: { task_id: task, session_id: "session", field, content } })
  return { received, batch, send }
}

describe("relay delta batching", () => {
  test("first character is immediate, burst is complete and terminal follows the body", () => {
    const f = fixture()
    f.send("首")
    expect(f.received.length).toBe(1)
    for (let i = 0; i < 1000; i++) f.send("文")
    f.batch.send({ type: "task.completed", payload: { task_id: "task" }, requestID: "done" })
    expect(f.received.length).toBeLessThan(10)
    expect(f.received.slice(0, -1).map((v) => v.payload.content).join("")).toBe("首" + "文".repeat(1000))
    expect(f.received.at(-1)?.type).toBe("task.completed")
  })
  test("reasoning, tasks, and tool boundaries stay ordered", () => {
    const f = fixture()
    f.send("a", "reasoning"); f.send("b", "reasoning"); f.send("c")
    f.send("d"); f.send("e", "text", "other")
    f.batch.send({ type: "task.tool", payload: {}, requestID: "tool" })
    expect(f.received.map((v) => v.payload.content ?? v.type)).toEqual(["a", "b", "c", "d", "e", "task.tool"])
  })
  test("a quiet stream flushes without another event", async () => {
    const f = fixture()
    f.send("a"); f.send("b")
    await new Promise((resolve) => setTimeout(resolve, 65))
    expect(f.received.map((v) => v.payload.content).join("")).toBe("ab")
  })
  test("large bursts flush before the timer and manual flush has no late duplicate", async () => {
    const f = fixture()
    f.send("a"); f.send("b".repeat(16384)); f.send("c"); f.batch.flush()
    const count = f.received.length
    await new Promise((resolve) => setTimeout(resolve, 65))
    expect(f.received.length).toBe(count)
    expect(f.received.map((v) => v.payload.content).join("")).toBe("a" + "b".repeat(16384) + "c")
  })
})
