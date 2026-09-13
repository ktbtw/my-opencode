export type HistoryTodo = {
  id?: string
  planID?: string
  content: string
  status: string
  priority: string
}

export type HistoryPart = {
  id?: string
  type: string
  text?: string
  callID?: string
  tool?: string
  state?: {
    status?: string
    input?: Record<string, unknown>
    output?: string
    title?: string
    error?: string
    metadata?: Record<string, unknown>
    time?: { start?: number; end?: number }
  }
  prompt?: string
  description?: string
  agent?: string
  attempt?: number
}

export type HistoryMessage = {
  info: {
    id?: string
    role: string
    time?: { created?: number; completed?: number }
    error?: { name?: string; data?: { message?: string } } | string
    metadata?: Record<string, unknown>
  }
  parts: HistoryPart[]
}

export type HistoryChild = {
  sessionID: string
  title?: string
  messages: HistoryMessage[]
  todos?: HistoryTodo[]
}

export type HistoryEvent = {
  sequence: number
  type: string
  task_id: string
  session_id?: string
  content?: string
  field?: string
  sent_at: string
  metadata?: Record<string, unknown>
  tool?: Record<string, unknown>
  plan?: {
    id: string
    title?: string
    status?: string
    session_id?: string
    items: Array<{ id: string; text: string; status: string; priority?: string }>
  }
  error?: string
  attempt?: number
  goal_id?: string
  objective?: string
  status?: string
  iteration?: number
  max?: number
  node_id?: string
  plan_id?: string
  child_session_id?: string
  state?: string
  prompt?: string
}

export type HistoryPage = {
  events: HistoryEvent[]
  next_cursor: string
  has_more: boolean
  complete: boolean
  error_code: string
  source_sequence_start: number
  source_sequence_end: number
}

function iso(ms?: number) {
  if (!ms || !Number.isFinite(ms) || ms <= 0) return new Date(0).toISOString()
  return new Date(ms).toISOString()
}

function pageEvents(events: HistoryEvent[], cursor: string, limit: number): HistoryPage {
  const afterSequence = Math.max(0, Number.parseInt(cursor || "0", 10) || 0)
  const size = Math.min(Math.max(limit, 1), 200)
  const available = events.filter((event) => event.sequence > afterSequence)
  const slice = available.slice(0, size)
  const hasMore = slice.length < available.length
  return {
    events: slice,
    next_cursor: hasMore ? String(slice.at(-1)?.sequence ?? afterSequence) : "",
    has_more: hasMore,
    complete: !hasMore,
    error_code: "",
    source_sequence_start: slice[0]?.sequence ?? 0,
    source_sequence_end: slice.at(-1)?.sequence ?? 0,
  }
}

function todosToPlan(sessionID: string, todos: HistoryTodo[]) {
  if (todos.length === 0) return undefined
  const planID = todos[0]?.planID?.trim() || `plan_${sessionID}`
  const items = todos.map((todo, index) => ({
    id: todo.id?.trim() || `${planID}_${index}`,
    text: todo.content,
    status: todo.status,
    priority: todo.priority,
  }))
  const completed = items.every((item) => item.status === "completed" || item.status === "cancelled")
  return {
    id: planID,
    title: "",
    status: completed ? "completed" : "in_progress",
    session_id: sessionID,
    items,
  }
}

function appendMessageEvents(
  events: HistoryEvent[],
  taskID: string,
  sessionID: string,
  message: HistoryMessage,
  extraMetadata: Record<string, unknown> = {},
) {
  const sentAt = iso(message.info.time?.created)
  const push = (event: Omit<HistoryEvent, "sequence" | "task_id">) => {
    events.push({
      sequence: events.length + 1,
      task_id: taskID,
      ...event,
    })
  }

  if (message.info.role === "user") {
    const metadata = { ...extraMetadata, ...(message.info.metadata ?? {}) }
    const isQueueInsert = metadata.context_type === "queueInsert" || metadata.source === "user_queue"
    if (!isQueueInsert) {
      return
    }
    const text = message.parts
      .filter((part) => part.type === "text" && part.text?.trim())
      .map((part) => part.text!.trim())
      .join("\n")
    if (text) {
      push({
        type: "input_applied",
        session_id: sessionID,
        content: text,
        sent_at: sentAt,
        metadata: { source: "user_queue", context_type: "queueInsert", ...extraMetadata },
      })
    }
    return
  }

  for (const part of message.parts) {
    const partAt = iso(part.state?.time?.end || part.state?.time?.start || message.info.time?.created)
    switch (part.type) {
      case "reasoning":
        if (part.text?.trim()) {
          push({
            type: "delta",
            field: "reasoning",
            session_id: sessionID,
            content: part.text,
            sent_at: partAt,
            metadata: extraMetadata,
          })
        }
        break
      case "text":
        if (part.text?.trim()) {
          push({
            type: "delta",
            field: "text",
            session_id: sessionID,
            content: part.text,
            sent_at: partAt,
            metadata: extraMetadata,
          })
        }
        break
      case "tool": {
        const state = part.state ?? {}
        const status =
          state.status ||
          (state.error ? "error" : state.time?.end ? "completed" : state.time?.start ? "running" : "pending")
        push({
          type: "tool_updated",
          session_id: sessionID,
          sent_at: partAt,
          metadata: extraMetadata,
          tool: {
            id: part.id || part.callID,
            call_id: part.callID,
            tool: part.tool,
            status,
            input: state.input,
            title: state.title,
            output: state.output,
            error: state.error,
            metadata: state.metadata,
            started_at: state.time?.start,
            ended_at: state.time?.end,
          },
        })
        break
      }
      case "retry":
        push({
          type: "retrying",
          session_id: sessionID,
          content: part.text || "retrying",
          attempt: part.attempt,
          sent_at: partAt,
          metadata: extraMetadata,
        })
        break
      case "compaction":
        push({
          type: "compaction_completed",
          session_id: sessionID,
          sent_at: partAt,
          metadata: extraMetadata,
        })
        break
      case "subtask":
        push({
          type: "subagent_started",
          session_id: sessionID,
          content: part.prompt || part.description,
          sent_at: partAt,
          metadata: {
            ...extraMetadata,
            node_id: part.id || part.description,
            title: part.description,
            subagent_type: part.agent,
            prompt: part.prompt,
          },
        })
        break
    }
  }

  const error =
    typeof message.info.error === "string"
      ? message.info.error
      : message.info.error?.data?.message || message.info.error?.name
  if (error) {
    push({
      type: "failed",
      session_id: sessionID,
      error,
      sent_at: iso(message.info.time?.completed || message.info.time?.created),
      metadata: extraMetadata,
    })
  }
}

export function buildSessionHistory(input: {
  taskID: string
  sessionID: string
  messages: HistoryMessage[]
  todos?: HistoryTodo[]
  children?: HistoryChild[]
  recordedEvents?: Array<Partial<Omit<HistoryEvent, "sequence">> & { sequence?: number }>
  cursor?: string
  limit?: number
}): HistoryPage {
  if (input.recordedEvents && input.recordedEvents.length > 0) {
    const explicitSequences = input.recordedEvents
      .map((event) => event.sequence)
      .filter((sequence): sequence is number => typeof sequence === "number" && sequence > 0)
    let nextSequence = Math.max(0, ...explicitSequences)
    const events: HistoryEvent[] = input.recordedEvents.map((event) => ({
      ...event,
      task_id: event.task_id || input.taskID,
      type: event.type || "delta",
      sent_at: event.sent_at || new Date(0).toISOString(),
      sequence: event.sequence ?? ++nextSequence,
    })) as HistoryEvent[]
    const hasTerminal = events.some(
      (event) => event.type === "completed" || event.type === "failed" || event.type === "cancelled",
    )
    if (!hasTerminal) appendTerminalHistoryEvent(events, input)
    return pageEvents(events, input.cursor ?? "", input.limit ?? 200)
  }
  const events: HistoryEvent[] = []
  for (const message of input.messages) {
    appendMessageEvents(events, input.taskID, input.sessionID, message)
  }
  const plan = todosToPlan(input.sessionID, input.todos ?? [])
  if (plan) {
    events.push({
      sequence: events.length + 1,
      task_id: input.taskID,
      type: "plan_updated",
      session_id: input.sessionID,
      sent_at: events.at(-1)?.sent_at || new Date(0).toISOString(),
      plan,
    })
  }
  for (const child of input.children ?? []) {
    const nodeID = child.sessionID
    events.push({
      sequence: events.length + 1,
      task_id: input.taskID,
      type: "subagent_started",
      session_id: input.sessionID,
      content: child.title,
      sent_at: child.messages[0] ? iso(child.messages[0].info.time?.created) : new Date(0).toISOString(),
      metadata: {
        node_id: nodeID,
        child_session_id: child.sessionID,
        title: child.title,
      },
    })
    for (const message of child.messages) {
      appendMessageEvents(events, input.taskID, child.sessionID, message, {
        node_id: nodeID,
        child_session_id: child.sessionID,
      })
    }
    const last = child.messages.at(-1)
    if (last) {
      const text = last.parts
        .filter((part) => part.type === "text" && part.text?.trim())
        .map((part) => part.text!.trim())
        .join("\n")
      events.push({
        sequence: events.length + 1,
        task_id: input.taskID,
        type: "subagent_result",
        session_id: input.sessionID,
        content: text,
        sent_at: iso(last.info.time?.completed || last.info.time?.created),
        metadata: {
          node_id: nodeID,
          child_session_id: child.sessionID,
          status: last.info.error ? "failed" : "completed",
        },
      })
    }
  }
  appendTerminalHistoryEvent(events, input)
  return pageEvents(events, input.cursor ?? "", input.limit ?? 200)
}

function appendTerminalHistoryEvent(
  events: HistoryEvent[],
  input: {
    taskID: string
    sessionID: string
    messages: HistoryMessage[]
  },
) {
  const lastAssistant = [...input.messages].reverse().find((message) => message.info.role === "assistant")
  const completedAt = lastAssistant?.info.time?.completed
  const lastError =
    typeof lastAssistant?.info.error === "string"
      ? lastAssistant.info.error
      : lastAssistant?.info.error?.data?.message || lastAssistant?.info.error?.name
  if (completedAt && completedAt > 0 && !lastError) {
    const text = (lastAssistant?.parts ?? [])
      .filter((part) => part.type === "text" && part.text?.trim())
      .map((part) => part.text!.trim())
      .join("\n")
    const lastSequence = events.reduce((max, event) => Math.max(max, event.sequence), 0)
    events.push({
      sequence: lastSequence + 1,
      task_id: input.taskID,
      type: "completed",
      session_id: input.sessionID,
      content: text,
      sent_at: iso(completedAt),
    })
  }
}
