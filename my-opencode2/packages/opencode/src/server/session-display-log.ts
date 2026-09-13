import { Database } from "bun:sqlite"
import { existsSync, mkdirSync, readFileSync } from "node:fs"
import path from "node:path"
import type { HistoryEvent } from "./session-history"

/** Durable, append-only source for the events shown in the chat UI. */
export type DisplayEvent = Omit<HistoryEvent, "sequence"> & { sequence?: number }

const databases = new Map<string, Database>()
const caches = new Map<string, { events: DisplayEvent[]; root: string; sessionID: string }>()
const DISPLAY_PAYLOAD_KEYS = new Set([
  "task_id", "session_id", "content", "message", "field", "sent_at",
  "metadata", "tool", "plan", "error", "attempt", "goal_id", "objective",
  "status", "iteration", "max", "node_id", "plan_id", "child_session_id",
  "state", "prompt",
])

const DISPLAY_TASK_TYPES: Record<string, string> = {
  "task.started": "started",
  "task.waiting_approval": "waiting_approval",
  "task.delta": "delta",
  "task.tool_updated": "tool_updated",
  "task.plan_updated": "plan_updated",
  "task.progress": "progress",
  "task.retrying": "retrying",
  "task.compaction_started": "compaction_started",
  "task.compaction_completed": "compaction_completed",
  "task.subagent_started": "subagent_started",
  "task.subagent_result": "subagent_result",
  "task.subagent_state": "subagent_state",
  "task.goal_created": "goal_created",
  "task.goal_heartbeat": "goal_heartbeat",
  "task.goal_paused": "goal_paused",
  "task.goal_continued": "goal_continued",
  "task.goal_checkpoint": "goal_checkpoint",
  "task.goal_completed": "goal_completed",
  "task.goal_failed": "goal_failed",
  "task.cancelling": "cancelling",
  "task.input_applied": "input_applied",
  "task.completed": "completed",
  "task.failed": "failed",
  "task.cancelled": "cancelled",
}

export function displayLogPath(root: string, sessionID: string) {
  return path.join(root, ".chatcodex-artifacts", "display-log", `${sessionID}.json`)
}

function databasePath(root: string) {
  return path.join(root, ".chatcodex-artifacts", "display-log.sqlite")
}

function db(root: string) {
  const existing = databases.get(root)
  if (existing) return existing
  mkdirSync(path.dirname(databasePath(root)), { recursive: true })
  const database = new Database(databasePath(root), { create: true })
  database.exec("PRAGMA journal_mode = WAL; PRAGMA synchronous = FULL; PRAGMA busy_timeout = 5000;")
  database.exec(`
    CREATE TABLE IF NOT EXISTS display_event (
      id INTEGER PRIMARY KEY AUTOINCREMENT,
      session_id TEXT NOT NULL,
      task_id TEXT NOT NULL,
      sequence INTEGER NOT NULL,
      type TEXT NOT NULL,
      sent_at TEXT NOT NULL,
      data TEXT NOT NULL,
      UNIQUE(task_id, sequence)
    );
    CREATE INDEX IF NOT EXISTS display_event_session_idx ON display_event(session_id, sequence, id);
    CREATE INDEX IF NOT EXISTS display_event_task_idx ON display_event(task_id, sequence);
  `)
  databases.set(root, database)
  return database
}

export function displayEventFromTaskEnvelope(type: string, payload: Record<string, unknown>): DisplayEvent | undefined {
  const mapped = DISPLAY_TASK_TYPES[type]
  if (!mapped) return undefined
  const sessionID = String(payload.session_id ?? "").trim()
  const taskID = String(payload.task_id ?? "").trim()
  if (!sessionID || !taskID) return undefined
  const content = typeof payload.content === "string"
    ? payload.content
    : typeof payload.message === "string"
      ? payload.message
      : undefined
  if (mapped === "delta" && content === undefined) return undefined
  const metadata = {
    ...(isRecord(payload.metadata) ? payload.metadata : {}),
    ...Object.fromEntries(
      Object.entries(payload).filter(([key]) => !DISPLAY_PAYLOAD_KEYS.has(key)),
    ),
  }
  return {
    type: mapped,
    task_id: taskID,
    session_id: sessionID,
    content,
    field: typeof payload.field === "string" ? payload.field : undefined,
    sent_at: typeof payload.sent_at === "string" && payload.sent_at ? payload.sent_at : new Date().toISOString(),
    metadata: Object.keys(metadata).length > 0 ? metadata : undefined,
    tool: isRecord(payload.tool) ? payload.tool : undefined,
    plan: isPlan(payload.plan) ? payload.plan : undefined,
    error: typeof payload.error === "string" ? payload.error : undefined,
    attempt: typeof payload.attempt === "number" ? payload.attempt : undefined,
    goal_id: typeof payload.goal_id === "string" ? payload.goal_id : undefined,
    objective: typeof payload.objective === "string" ? payload.objective : undefined,
    status: typeof payload.status === "string" ? payload.status : undefined,
    iteration: typeof payload.iteration === "number" ? payload.iteration : undefined,
    max: typeof payload.max === "number" ? payload.max : undefined,
    node_id: typeof payload.node_id === "string" ? payload.node_id : undefined,
    plan_id: typeof payload.plan_id === "string" ? payload.plan_id : undefined,
    child_session_id: typeof payload.child_session_id === "string" ? payload.child_session_id : undefined,
    state: typeof payload.state === "string" ? payload.state : undefined,
    prompt: typeof payload.prompt === "string" ? payload.prompt : undefined,
  }
}

/** Synchronous commit: relay broadcasts only after this returns successfully. */
export function recordTaskDisplayEvent(root: string, type: string, payload: Record<string, unknown>): DisplayEvent | undefined {
  const event = displayEventFromTaskEnvelope(type, payload)
  if (!event || !event.session_id) return undefined
  const key = cacheKey(root, event.session_id)
  const cached = caches.get(key) ?? { events: loadEvents(root, event.session_id), root, sessionID: event.session_id }
  const database = db(root)
  const row = database.prepare("SELECT COALESCE(MAX(sequence), 0) + 1 AS sequence FROM display_event WHERE task_id = ?").get(event.task_id) as { sequence?: number } | undefined
  const sequence = Number(row?.sequence ?? 1)
  const stored = { ...event, sequence }
  database.prepare("INSERT INTO display_event(session_id, task_id, sequence, type, sent_at, data) VALUES (?, ?, ?, ?, ?, ?)").run(
    event.session_id,
    event.task_id,
    sequence,
    event.type,
    event.sent_at,
    JSON.stringify(stored),
  )
  cached.events.push(stored)
  caches.set(key, cached)
  return stored
}

export async function readSessionDisplayLog(root: string, sessionID: string, taskID?: string): Promise<DisplayEvent[]> {
  const key = cacheKey(root, sessionID)
  const cached = caches.get(key)
  const events = cached ? cached.events : loadEvents(root, sessionID)
  return events.filter((event) => !taskID || event.task_id === taskID).map((event) => ({ ...event }))
}

// Retained for compatibility with existing callers; writes are now immediate.
export async function flushSessionDisplayLog(_root: string, _sessionID: string) {}

// Kept as a small pure helper for callers/tests; it no longer coalesces events.
export function appendDisplayEvent(events: DisplayEvent[], incoming: DisplayEvent): DisplayEvent[] {
  events.push({ ...incoming })
  return events
}

export function sequenceDisplayEvents(events: DisplayEvent[], taskID: string): HistoryEvent[] {
  let fallback = 0
  return events.map((event) => ({
    ...event,
    task_id: event.task_id || taskID,
    sequence: event.sequence ?? ++fallback,
  }))
}

function loadEvents(root: string, sessionID: string): DisplayEvent[] {
  const database = db(root)
  // `id` is the append order across tasks; sequence is scoped to task_id.
  // Sorting by sequence here would interleave independent tasks and make a
  // session-wide replay appear to reorder the live stream.
  const rows = database.prepare("SELECT data FROM display_event WHERE session_id = ? ORDER BY id").all(sessionID) as Array<{ data: string }>
  if (rows.length > 0) return rows.flatMap((row) => parseEvent(row.data))

  // One-time import of the previous JSON log format.
  const legacy = displayLogPath(root, sessionID)
  if (!existsSync(legacy)) return []
  let parsed: unknown
  try {
    parsed = JSON.parse(readFileSync(legacy, "utf8"))
  } catch {
    return []
  }
  if (!Array.isArray(parsed)) return []
  const imported: DisplayEvent[] = []
  for (const item of parsed) {
    if (!isRecord(item) || typeof item.task_id !== "string") continue
    const event = {
      ...item,
      session_id: sessionID,
      type: typeof item.type === "string" ? item.type : "delta",
      sent_at: typeof item.sent_at === "string" && item.sent_at ? item.sent_at : new Date(0).toISOString(),
    } as DisplayEvent
    const row = database.prepare("SELECT COALESCE(MAX(sequence), 0) + 1 AS sequence FROM display_event WHERE task_id = ?").get(event.task_id) as { sequence?: number } | undefined
    const sequence = Number(row?.sequence ?? 1)
    const stored = { ...event, sequence }
    database.prepare("INSERT OR IGNORE INTO display_event(session_id, task_id, sequence, type, sent_at, data) VALUES (?, ?, ?, ?, ?, ?)").run(
      sessionID, event.task_id, sequence, event.type, event.sent_at, JSON.stringify(stored),
    )
    imported.push(stored)
  }
  return imported
}

function parseEvent(data: string): DisplayEvent[] {
  try {
    const value = JSON.parse(data)
    return isRecord(value) ? [value as DisplayEvent] : []
  } catch {
    return []
  }
}

function cacheKey(root: string, sessionID: string) {
  return `${root}::${sessionID}`
}

function isRecord(value: unknown): value is Record<string, any> {
  return !!value && typeof value === "object" && !Array.isArray(value)
}

function isPlan(value: unknown): value is NonNullable<HistoryEvent["plan"]> {
  return isRecord(value) && typeof value.id === "string" && Array.isArray(value.items)
}
