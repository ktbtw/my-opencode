import { describe, expect, test } from "bun:test"
import { buildSessionHistory, type HistoryEvent } from "./session-history"

describe("buildSessionHistory", () => {
  test("projects text, reasoning, tools, plan, and child sessions without token deltas", () => {
    const page = buildSessionHistory({
      taskID: "task_1",
      sessionID: "ses_parent",
      messages: [
        {
          info: { id: "usr_1", role: "user", time: { created: Date.parse("2026-01-01T00:00:00Z") } },
          parts: [{ type: "text", text: "hello" }],
        },
        {
          info: { id: "asst_1", role: "assistant", time: { created: Date.parse("2026-01-01T00:00:01Z") } },
          parts: [
            { type: "reasoning", text: "think" },
            { type: "text", text: "world" },
            {
              type: "tool",
              id: "prt_read",
              callID: "call_1",
              tool: "read",
              state: { status: "completed", title: "README", output: "ok", time: { start: 1, end: 2 } },
            },
          ],
        },
      ],
      todos: [{ id: "todo_1", planID: "plan_1", content: "inspect", status: "completed", priority: "high" }],
      children: [
        {
          sessionID: "ses_child",
          title: "explore",
          messages: [
            {
              info: { role: "assistant", time: { created: Date.parse("2026-01-01T00:00:02Z") } },
              parts: [{ type: "text", text: "child done" }],
            },
          ],
        },
      ],
    })

    expect(page.events.map((event) => event.type)).toEqual([
      "delta",
      "delta",
      "tool_updated",
      "plan_updated",
      "subagent_started",
      "delta",
      "subagent_result",
    ])
    expect(page.events.find((event) => event.field === "reasoning")?.content).toBe("think")
    expect(page.events.find((event) => event.field === "text")?.content).toBe("world")
    expect(page.events.find((event) => event.type === "tool_updated")?.tool).toMatchObject({
      call_id: "call_1",
      tool: "read",
      status: "completed",
    })
    expect(page.events.find((event) => event.type === "plan_updated")?.plan?.items[0]?.text).toBe("inspect")
    expect(page.events.find((event) => event.type === "subagent_result")?.metadata).toMatchObject({
      node_id: "ses_child",
      status: "completed",
    })
    expect(page.has_more).toBe(false)
    expect(page.error_code).toBe("")
    expect(page.events.some((event) => event.type === "input_applied")).toBe(false)
    expect(page.events.some((event) => event.type === "completed" || event.type === "failed")).toBe(false)
    expect(page.events.some((event) => event.content === "hello")).toBe(false)
  })

  test("prefers recorded live display events over reconstructed parts", () => {
    const page = buildSessionHistory({
      taskID: "task_1",
      sessionID: "ses_parent",
      messages: [
        {
          info: { role: "assistant", time: { created: Date.parse("2026-01-01T00:00:01Z") } },
          parts: [
            {
              type: "tool",
              id: "prt_a",
              callID: "call_a",
              tool: "read",
              state: { status: "completed", output: "ok", time: { start: 1, end: 2 } },
            },
            {
              type: "tool",
              id: "prt_b",
              callID: "call_b",
              tool: "read",
              state: { status: "completed", output: "ok", time: { start: 3, end: 4 } },
            },
            { type: "text", text: "全部正文挤在最后" },
          ],
        },
      ],
      recordedEvents: [
        {
          type: "delta",
          task_id: "task_1",
          session_id: "ses_parent",
          field: "text",
          content: "先核对。",
          sent_at: "2026-01-01T00:00:01.000Z",
        },
        {
          type: "tool_updated",
          task_id: "task_1",
          session_id: "ses_parent",
          sent_at: "2026-01-01T00:00:02.000Z",
          tool: { id: "prt_a", call_id: "call_a", tool: "read", status: "completed" },
        },
        {
          type: "delta",
          task_id: "task_1",
          session_id: "ses_parent",
          field: "text",
          content: "再推包。",
          sent_at: "2026-01-01T00:00:03.000Z",
        },
        {
          type: "tool_updated",
          task_id: "task_1",
          session_id: "ses_parent",
          sent_at: "2026-01-01T00:00:04.000Z",
          tool: { id: "prt_b", call_id: "call_b", tool: "read", status: "completed" },
        },
      ],
    })
    expect(page.events.map((event) => event.type)).toEqual(["delta", "tool_updated", "delta", "tool_updated"])
    expect(page.events[0].content).toBe("先核对。")
    expect(page.events[2].content).toBe("再推包。")
    expect(page.events.some((event) => event.content === "全部正文挤在最后")).toBe(false)
  })

  test("review: live display log must not rebuild as 83 tools then the whole body", () => {
    const live: Array<Omit<HistoryEvent, "sequence">> = [
      { type: "delta", task_id: "t", session_id: "s", field: "text", content: "先核对速通 max。", sent_at: "2026-01-01T00:00:01.000Z" },
      { type: "tool_updated", task_id: "t", session_id: "s", sent_at: "2026-01-01T00:00:02.000Z", tool: { id: "g1", tool: "grep", status: "completed" } },
      { type: "tool_updated", task_id: "t", session_id: "s", sent_at: "2026-01-01T00:00:03.000Z", tool: { id: "g2", tool: "grep", status: "completed" } },
      { type: "tool_updated", task_id: "t", session_id: "s", sent_at: "2026-01-01T00:00:04.000Z", tool: { id: "g3", tool: "grep", status: "completed" } },
      { type: "delta", task_id: "t", session_id: "s", field: "text", content: "协议出售走同一套背包。", sent_at: "2026-01-01T00:00:05.000Z" },
      { type: "tool_updated", task_id: "t", session_id: "s", sent_at: "2026-01-01T00:00:06.000Z", tool: { id: "r1", tool: "read", status: "completed" } },
      { type: "delta", task_id: "t", session_id: "s", field: "text", content: "接着核对副本页。", sent_at: "2026-01-01T00:00:07.000Z" },
      { type: "tool_updated", task_id: "t", session_id: "s", sent_at: "2026-01-01T00:00:08.000Z", tool: { id: "r2", tool: "read", status: "error" } },
      { type: "delta", task_id: "t", session_id: "s", field: "text", content: "所以仍可能卖掉。", sent_at: "2026-01-01T00:00:09.000Z" },
    ]
    const fromParts = buildSessionHistory({
      taskID: "t",
      sessionID: "s",
      messages: [
        {
          info: { role: "assistant", time: { created: Date.parse("2026-01-01T00:00:01Z") } },
          parts: [
            ...live.filter((event) => event.type === "tool_updated").map((event) => ({
              type: "tool" as const,
              id: String(event.tool?.id),
              tool: String(event.tool?.tool),
              state: { status: "completed", time: { start: 1, end: 2 } },
            })),
            { type: "text", text: live.filter((event) => event.type === "delta").map((event) => event.content).join("") },
          ],
        },
      ],
    })
    expect(fromParts.events[0].type).toBe("tool_updated")
    expect(fromParts.events.at(-1)?.type).toBe("delta")

    const fromLog = buildSessionHistory({
      taskID: "t",
      sessionID: "s",
      messages: fromParts.events.length ? [{ info: { role: "assistant" }, parts: [] }] : [],
      recordedEvents: live,
    })
    expect(fromLog.events.map((event) => event.type)).toEqual([
      "delta",
      "tool_updated",
      "tool_updated",
      "tool_updated",
      "delta",
      "tool_updated",
      "delta",
      "tool_updated",
      "delta",
    ])
    expect(fromLog.events[0].content).toBe("先核对速通 max。")
    expect(fromLog.events.at(-1)?.content).toBe("所以仍可能卖掉。")
  })

  test("emits a running tool_updated when the stored part has no terminal status", () => {
    const page = buildSessionHistory({
      taskID: "task_1",
      sessionID: "ses_parent",
      messages: [
        {
          info: { role: "assistant", time: { created: Date.parse("2026-01-01T00:00:01Z") } },
          parts: [
            { type: "text", text: "working" },
            {
              type: "tool",
              callID: "call_open",
              tool: "read",
              state: { status: "running", title: "README", time: { start: 1 } },
            },
          ],
        },
      ],
    })
    expect(page.events.find((event) => event.type === "tool_updated")?.tool).toMatchObject({
      status: "running",
    })
    expect(page.events.some((event) => event.type === "completed")).toBe(false)
  })

  test("infers tool status from error or end time when the stored status is missing", () => {
    const ended = buildSessionHistory({
      taskID: "task_1",
      sessionID: "ses_parent",
      messages: [
        {
          info: { role: "assistant", time: { created: 1 } },
          parts: [
            {
              type: "tool",
              callID: "call_end",
              tool: "read",
              state: { output: "ok", time: { start: 1, end: 2 } },
            },
          ],
        },
      ],
    })
    const errored = buildSessionHistory({
      taskID: "task_1",
      sessionID: "ses_parent",
      messages: [
        {
          info: { role: "assistant", time: { created: 1 } },
          parts: [
            {
              type: "tool",
              callID: "call_err",
              tool: "read",
              state: { error: "boom", time: { start: 1 } },
            },
          ],
        },
      ],
    })
    expect(ended.events.find((event) => event.type === "tool_updated")?.tool).toMatchObject({
      status: "completed",
    })
    expect(errored.events.find((event) => event.type === "tool_updated")?.tool).toMatchObject({
      status: "error",
    })
  })

  test("emits completed when the last assistant has time.completed", () => {
    const page = buildSessionHistory({
      taskID: "task_1",
      sessionID: "ses_parent",
      messages: [
        {
          info: {
            role: "assistant",
            time: { created: Date.parse("2026-01-01T00:00:01Z"), completed: Date.parse("2026-01-01T00:00:02Z") },
          },
          parts: [
            { type: "text", text: "done" },
            {
              type: "tool",
              callID: "call_1",
              tool: "read",
              state: { status: "running", time: { start: 1 } },
            },
          ],
        },
      ],
    })
    expect(page.events.at(-1)).toMatchObject({
      type: "completed",
      content: "done",
    })
  })

  test("does not emit a second failed when the assistant already has an error", () => {
    const page = buildSessionHistory({
      taskID: "task_1",
      sessionID: "ses_parent",
      messages: [
        {
          info: {
            role: "assistant",
            time: { created: 1, completed: 2 },
            error: { name: "OutputLengthError", data: { message: "truncated" } },
          },
          parts: [{ type: "text", text: "partial" }],
        },
      ],
    })
    expect(page.events.filter((event) => event.type === "failed")).toHaveLength(1)
    expect(page.events.some((event) => event.type === "completed")).toBe(false)
    expect(page.events.find((event) => event.type === "failed")?.error).toBe("truncated")
  })

  test("keeps queue-inserted user turns as input_applied", () => {
    const page = buildSessionHistory({
      taskID: "task_1",
      sessionID: "ses_parent",
      messages: [
        {
          info: {
            role: "user",
            time: { created: Date.parse("2026-01-01T00:00:00Z") },
            metadata: { source: "user_queue", context_type: "queueInsert" },
          },
          parts: [{ type: "text", text: "queued follow-up" }],
        },
      ],
    })
    expect(page.events.map((event) => event.type)).toEqual(["input_applied"])
    expect(page.events[0]?.content).toBe("queued follow-up")
  })

  test("pages by cursor without skipping or duplicating", () => {
    const messages = Array.from({ length: 5 }, (_, index) => ({
      info: { role: "assistant" as const, time: { created: index + 1 } },
      parts: [{ type: "text", text: `m${index + 1}` }],
    }))
    const first = buildSessionHistory({ taskID: "t", sessionID: "s", messages, limit: 2 })
    const second = buildSessionHistory({
      taskID: "t",
      sessionID: "s",
      messages,
      cursor: first.next_cursor,
      limit: 2,
    })
    const third = buildSessionHistory({
      taskID: "t",
      sessionID: "s",
      messages,
      cursor: second.next_cursor,
      limit: 2,
    })
    const contents = [...first.events, ...second.events, ...third.events].map((event) => event.content)
    expect(contents).toEqual(["m1", "m2", "m3", "m4", "m5"])
    expect(first.has_more).toBe(true)
    expect(second.has_more).toBe(true)
    expect(third.has_more).toBe(false)
  })

  test("keeps recorded sequence cursors stable when new events are appended", () => {
    const recordedEvents = [
      { sequence: 1, type: "delta", task_id: "t", session_id: "s", content: "one", sent_at: "2026-01-01T00:00:01Z" },
      { sequence: 2, type: "delta", task_id: "t", session_id: "s", content: "two", sent_at: "2026-01-01T00:00:02Z" },
      { sequence: 3, type: "delta", task_id: "t", session_id: "s", content: "three", sent_at: "2026-01-01T00:00:03Z" },
    ]
    const first = buildSessionHistory({ taskID: "t", sessionID: "s", messages: [], recordedEvents, limit: 1 })
    expect(first.events.map((event) => event.sequence)).toEqual([1])
    const appended = [...recordedEvents, { sequence: 4, type: "delta", task_id: "t", session_id: "s", content: "four", sent_at: "2026-01-01T00:00:04Z" }]
    const second = buildSessionHistory({
      taskID: "t",
      sessionID: "s",
      messages: [],
      recordedEvents: appended,
      cursor: first.next_cursor,
      limit: 10,
    })
    expect(second.events.map((event) => event.content)).toEqual(["two", "three", "four"])
    expect(second.events.map((event) => event.sequence)).toEqual([2, 3, 4])
  })

  test("does not mix task sequences when recorded history is filtered by task", () => {
    const page = buildSessionHistory({
      taskID: "task_1",
      sessionID: "s",
      messages: [],
      recordedEvents: [
        { sequence: 1, type: "delta", task_id: "task_1", session_id: "s", content: "a", sent_at: "2026-01-01T00:00:01Z" },
        { sequence: 1, type: "delta", task_id: "task_2", session_id: "s", content: "b", sent_at: "2026-01-01T00:00:02Z" },
        { sequence: 2, type: "delta", task_id: "task_1", session_id: "s", content: "c", sent_at: "2026-01-01T00:00:03Z" },
      ].filter((event) => event.task_id === "task_1"),
      cursor: "1",
      limit: 10,
    })
    expect(page.events.map((event) => event.content)).toEqual(["c"])
  })
})
