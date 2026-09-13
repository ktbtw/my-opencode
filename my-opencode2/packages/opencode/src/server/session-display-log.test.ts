import { describe, expect, test } from "bun:test"
import { mkdir, mkdtemp } from "node:fs/promises"
import os from "node:os"
import path from "node:path"
import {
  appendDisplayEvent,
  displayEventFromTaskEnvelope,
  displayLogPath,
  flushSessionDisplayLog,
  readSessionDisplayLog,
  recordTaskDisplayEvent,
  sequenceDisplayEvents,
  type DisplayEvent,
} from "./session-display-log"

function text(content: string): DisplayEvent {
  return {
    type: "delta",
    task_id: "task_1",
    session_id: "ses_1",
    field: "text",
    content,
    sent_at: "2026-01-01T00:00:00.000Z",
  }
}

function tool(id: string, status: string, call = id): DisplayEvent {
  return {
    type: "tool_updated",
    task_id: "task_1",
    session_id: "ses_1",
    sent_at: "2026-01-01T00:00:01.000Z",
    tool: { id, call_id: call, tool: "read", status },
  }
}

describe("session display log", () => {
  test("keeps whitespace-only deltas because they are valid body content", () => {
    expect(
      displayEventFromTaskEnvelope("task.delta", {
        task_id: "task_1",
        session_id: "ses_1",
        field: "text",
        content: "   ",
      })?.content,
    ).toBe("   ")
  })

  test("persists lifecycle, goal, progress, and subagent state payloads", () => {
    const started = displayEventFromTaskEnvelope("task.started", {
      task_id: "task_1",
      session_id: "ses_1",
      resumed: true,
    })
    const goal = displayEventFromTaskEnvelope("task.goal_checkpoint", {
      task_id: "task_1",
      session_id: "ses_1",
      goal_id: "goal_1",
      iteration: 2,
      status: "active",
      objective: "keep history",
    })
    const progress = displayEventFromTaskEnvelope("task.progress", {
      task_id: "task_1",
      session_id: "ses_1",
      message: "still running",
    })
    const subagent = displayEventFromTaskEnvelope("task.subagent_state", {
      task_id: "task_1",
      session_id: "ses_1",
      node_id: "node_1",
      state: "recovering",
    })
    expect(started?.type).toBe("started")
    expect(started?.metadata).toMatchObject({ resumed: true })
    expect(goal).toMatchObject({ goal_id: "goal_1", iteration: 2, status: "active", objective: "keep history" })
    expect(progress).toMatchObject({ type: "progress", content: "still running" })
    expect(subagent).toMatchObject({ node_id: "node_1", state: "recovering" })
  })

  test("keeps each source event immutable and in insertion order", () => {
    const events: DisplayEvent[] = []
    appendDisplayEvent(events, text("先核对。"))
    appendDisplayEvent(events, text("再看配置。"))
    appendDisplayEvent(events, tool("prt_a", "running"))
    appendDisplayEvent(events, tool("prt_a", "completed"))
    appendDisplayEvent(events, text("结论。"))
    appendDisplayEvent(events, tool("prt_b", "completed"))

    expect(events.map((event) => event.type)).toEqual([
      "delta",
      "delta",
      "tool_updated",
      "tool_updated",
      "delta",
      "tool_updated",
    ])
    expect(events[0].content).toBe("先核对。")
    expect(events[1].content).toBe("再看配置。")
    expect(events[2].tool).toMatchObject({ id: "prt_a", status: "running" })
    expect(events[3].tool).toMatchObject({ id: "prt_a", status: "completed" })
  })

  test("does not merge two tools that only share a call id when both have part ids", () => {
    const events: DisplayEvent[] = []
    appendDisplayEvent(events, tool("prt_1", "running", "call_shared"))
    appendDisplayEvent(events, tool("prt_2", "running", "call_shared"))
    expect(events).toHaveLength(2)
    expect(events[0].tool?.id).toBe("prt_1")
    expect(events[1].tool?.id).toBe("prt_2")
  })

  test("persists live order for session history replay", async () => {
    const root = await mkdtemp(path.join(os.tmpdir(), "display-log-"))
    await mkdir(path.join(root, ".chatcodex-artifacts", "display-log"), { recursive: true })
    recordTaskDisplayEvent(root, "task.delta", {
      task_id: "task_1",
      session_id: "ses_1",
      field: "text",
      content: "先核对。",
    })
    recordTaskDisplayEvent(root, "task.tool_updated", {
      task_id: "task_1",
      session_id: "ses_1",
      tool: { id: "prt_a", call_id: "call_a", tool: "grep", status: "completed" },
    })
    recordTaskDisplayEvent(root, "task.delta", {
      task_id: "task_1",
      session_id: "ses_1",
      field: "text",
      content: "再推包。",
    })
    await flushSessionDisplayLog(root, "ses_1")

    const loaded = await readSessionDisplayLog(root, "ses_1")
    expect(displayLogPath(root, "ses_1")).toContain("ses_1.json")
    expect(sequenceDisplayEvents(loaded, "task_1").map((event) => event.type)).toEqual([
      "delta",
      "tool_updated",
      "delta",
    ])
    expect(loaded[0].content).toBe("先核对。")
    expect(loaded[2].content).toBe("再推包。")
  })

  test("assigns stable task sequences and filters another task in the same session", async () => {
    const root = await mkdtemp(path.join(os.tmpdir(), "display-log-filter-"))
    recordTaskDisplayEvent(root, "task.delta", {
      task_id: "task_1",
      session_id: "ses_shared",
      field: "text",
      content: "one",
    })
    recordTaskDisplayEvent(root, "task.delta", {
      task_id: "task_2",
      session_id: "ses_shared",
      field: "text",
      content: "two",
    })
    recordTaskDisplayEvent(root, "task.delta", {
      task_id: "task_1",
      session_id: "ses_shared",
      field: "text",
      content: "three",
    })
    const loaded = await readSessionDisplayLog(root, "ses_shared", "task_1")
    expect(loaded.map((event) => event.content)).toEqual(["one", "three"])
    expect(loaded.map((event) => event.sequence)).toEqual([1, 2])
  })
})
