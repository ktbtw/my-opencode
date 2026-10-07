import { Database } from "bun:sqlite"
import { expect, test } from "bun:test"
import { tmpdir } from "../fixture/fixture"
import { readTaskDisplayPage, recordTaskDisplayEvent, readSessionDisplayLog } from "../../src/server/session-display-log"

test("indexed terminal history matches full reconstruction across page boundaries", async () => {
  await using tmp = await tmpdir()
  const base = { session_id: "session", task_id: "task" }
  recordTaskDisplayEvent(tmp.path, "task.started", base)
  for (let i = 0; i < 405; i++) recordTaskDisplayEvent(tmp.path, "task.delta", { ...base, field: "text", content: String(i) })
  const active = readTaskDisplayPage(tmp.path, "session", "task")!
  expect(active.complete).toBe(true)
  expect(active.events.map((event) => event.type)).toEqual(["started", "delta"])
  expect(active.events[1].content).toBe(Array.from({ length: 405 }, (_, i) => String(i)).join(""))
  recordTaskDisplayEvent(tmp.path, "task.completed", { ...base, content: "done" })
  recordTaskDisplayEvent(tmp.path, "task.completed", { session_id: "session", task_id: "other", content: "other task" })
  const recordedEvents = await readSessionDisplayLog(tmp.path, "session", "task")
  let cursor = ""
  const delivered: { type: string; content?: string; sequence: number }[] = []
  do {
    const page = readTaskDisplayPage(tmp.path, "session", "task", cursor, 2)!
    delivered.push(...page.events)
    if (page.has_more) expect(Number(page.next_cursor)).toBeGreaterThan(Number(cursor || "0"))
    cursor = page.next_cursor
  } while (cursor)
  expect(delivered.map((event) => event.type)).toEqual(["started", "delta", "completed"])
  expect(delivered[1].content).toBe(recordedEvents.filter((event) => event.type === "delta").map((event) => event.content).join(""))
  expect(delivered.at(-1)?.sequence).toBe(407)
  expect(readTaskDisplayPage(tmp.path, "wrong-session", "task")).toBeUndefined()
  expect(readTaskDisplayPage(tmp.path, "session", "missing")).toBeUndefined()
  expect(readTaskDisplayPage(tmp.path, "session", "task", "407")?.events).toEqual([])
})


test("compact paging keeps text/tool/text order and never merges across reasoning or input", async () => {
  await using tmp = await tmpdir()
  const base = { session_id: "s", task_id: "t" }
  const delta = (content: string, field = "text") => recordTaskDisplayEvent(tmp.path, "task.delta", { ...base, content, field })
  delta("before-"); delta("tool")
  recordTaskDisplayEvent(tmp.path, "task.tool_updated", { ...base, tool: { id: "call", tool: "read", status: "completed" } })
  delta("thinking", "reasoning"); delta("again", "reasoning")
  delta("after-"); delta("tool")
  recordTaskDisplayEvent(tmp.path, "task.input_applied", { ...base, content: "continue" })
  delta("new round")
  recordTaskDisplayEvent(tmp.path, "task.completed", base)
  const all = []
  let cursor = ""
  do {
    const page = readTaskDisplayPage(tmp.path, "s", "t", cursor, 2)!
    all.push(...page.events)
    cursor = page.next_cursor
  } while (cursor)
  expect(all.map((event) => event.type)).toEqual(["delta", "tool_updated", "delta", "delta", "input_applied", "delta", "completed"])
  expect(all.filter((event) => event.type === "delta").map((event) => event.content)).toEqual(["before-tool", "thinkingagain", "after-tool", "new round"])
  expect(all.map((event) => event.sequence)).toEqual([2, 3, 5, 7, 8, 9, 10])
})


test("large legacy token histories advance bounded raw cursors without truncation", async () => {
  await using tmp = await tmpdir()
  recordTaskDisplayEvent(tmp.path, "task.started", { task_id: "t", session_id: "s" })
  const db = new Database(tmp.path + "/.chatcodex-artifacts/display-log.sqlite")
  const insert = db.prepare("INSERT INTO display_event(session_id,task_id,sequence,type,sent_at,data) VALUES ('s','t',?,'delta',?,?)")
  db.transaction(() => {
    for (let i = 2; i <= 20001; i++) {
      const event = { task_id: "t", session_id: "s", sequence: i, type: "delta", field: "reasoning", content: "x", sent_at: new Date(0).toISOString() }
      insert.run(i, event.sent_at, JSON.stringify(event))
    }
  })()
  db.close()
  recordTaskDisplayEvent(tmp.path, "task.completed", { task_id: "t", session_id: "s" })
  let cursor = ""
  let characters = 0
  let terminals = 0
  let pages = 0
  do {
    const page = readTaskDisplayPage(tmp.path, "s", "t", cursor)!
    expect(page.events.length).toBeLessThanOrEqual(200)
    if (page.has_more) expect(Number(page.next_cursor)).toBeGreaterThan(Number(cursor || "0"))
    characters += page.events.filter((event) => event.type === "delta").reduce((sum, event) => sum + (event.content?.length ?? 0), 0)
    terminals += page.events.filter((event) => event.type === "completed").length
    cursor = page.next_cursor
    pages++
    expect(pages).toBeLessThan(10)
  } while (cursor)
  expect(characters).toBe(20000)
  expect(terminals).toBe(1)
})


test("active compact pages retain the durable cursor when more output arrives", async () => {
  await using tmp = await tmpdir()
  const base = { session_id: "s", task_id: "active" }
  recordTaskDisplayEvent(tmp.path, "task.started", base)
  recordTaskDisplayEvent(tmp.path, "task.delta", { ...base, field: "text", content: "before" })
  recordTaskDisplayEvent(tmp.path, "task.tool_updated", { ...base, tool: { id: "tool", status: "running" } })
  const first = readTaskDisplayPage(tmp.path, "s", "active", "", 2)!
  expect(first.has_more).toBe(true)
  expect(first.next_cursor).toBe("2")
  recordTaskDisplayEvent(tmp.path, "task.delta", { ...base, field: "text", content: "after" })
  const next = readTaskDisplayPage(tmp.path, "s", "active", first.next_cursor, 2)!
  expect(next.events.map((event) => event.type)).toEqual(["tool_updated", "delta"])
  expect(next.events[1].content).toBe("after")
  expect(next.complete).toBe(true)
  expect(readTaskDisplayPage(tmp.path, "another-session", "active")).toBeUndefined()
})


test("task reads see durable writes without retaining a whole-session cache", async () => {
  await using tmp = await tmpdir()
  recordTaskDisplayEvent(tmp.path, "task.started", { task_id: "one", session_id: "s" })
  const db = new Database(tmp.path + "/.chatcodex-artifacts/display-log.sqlite")
  const incoming = { task_id: "one", session_id: "s", sequence: 2, type: "delta", field: "text", content: "durable body", sent_at: new Date(0).toISOString() }
  db.prepare("INSERT INTO display_event(session_id,task_id,sequence,type,sent_at,data) VALUES ('s','one',2,'delta',?,?)").run(incoming.sent_at, JSON.stringify(incoming))
  db.close()
  recordTaskDisplayEvent(tmp.path, "task.delta", { task_id: "two", session_id: "s", field: "text", content: "other task" })
  expect((await readSessionDisplayLog(tmp.path, "s", "one")).map((e) => e.content).filter(Boolean)).toEqual(["durable body"])
  expect((await readSessionDisplayLog(tmp.path, "s", "two")).map((e) => e.content)).toEqual(["other task"])
})
