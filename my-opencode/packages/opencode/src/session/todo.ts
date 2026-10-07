import { BusEvent } from "@/bus/bus-event"
import { Bus } from "@/bus"
import { SessionID } from "./schema"
import { Effect, Layer, Context, Schema } from "effect"
import { Database, sql } from "@/storage/db"
import { asc, desc, eq, inArray } from "drizzle-orm"
import { Identifier } from "@/id/id"
import { TodoTable } from "./session.sql"

export const Info = Schema.Struct({
  id: Schema.optional(Schema.String).annotate({ description: "Stable todo id" }),
  planID: Schema.optional(Schema.String).annotate({ description: "Identifier for the plan batch this todo belongs to" }),
  content: Schema.String.annotate({ description: "Brief description of the task" }),
  status: Schema.String.annotate({
    description: "Current status of the task: pending, in_progress, completed, cancelled",
  }),
  priority: Schema.String.annotate({ description: "Priority level of the task: high, medium, low" }),
}).annotate({ identifier: "Todo" })
export type Info = Schema.Schema.Type<typeof Info>

export const Event = {
  Updated: BusEvent.define(
    "todo.updated",
    Schema.Struct({
      sessionID: SessionID,
      planID: Schema.String,
      todos: Schema.Array(Info),
    }),
  ),
}

export interface Interface {
  readonly update: (input: { sessionID: SessionID; todos: Info[] }) => Effect.Effect<Info[]>
  readonly get: (sessionID: SessionID) => Effect.Effect<Info[]>
  readonly list: (sessionID: SessionID) => Effect.Effect<Info[][]>
  readonly activePlanID: (sessionID: SessionID) => Effect.Effect<string | undefined>
}

export class Service extends Context.Service<Service, Interface>()("@opencode/SessionTodo") {}

function todoID(given?: string) {
  const value = given?.trim()
  if (value) return value
  return Identifier.create("todo", "ascending")
}

function todoPlanID(todos: Info[]) {
  const value = todos[0]?.planID?.trim()
  if (value) return value
  return Identifier.create("plan", "ascending")
}

function normalizeTodo(todo: Info, planID: string): Info {
  return {
    id: todoID(todo.id),
    planID: todo.planID?.trim() || planID,
    content: todo.content,
    status: todo.status,
    priority: todo.priority,
  }
}

type TodoRow = {
  id?: string | null
  session_id: SessionID
  plan_id?: string | null
  content: string
  status: string
  priority: string
  position: number
  time_created: number
  time_updated: number
}

function rowTodo(row: TodoRow): Info {
  return {
    id: "id" in row && typeof row.id === "string" ? row.id : undefined,
    planID: "plan_id" in row && typeof row.plan_id === "string" ? row.plan_id : undefined,
    content: row.content,
    status: row.status,
    priority: row.priority,
  }
}

function groupRows(rows: TodoRow[]) {
  const plans = new Map<string, Info[]>()
  for (const row of rows) {
    const item = rowTodo(row)
    const planID = item.planID || `plan_${row.session_id}`
    const list = plans.get(planID)
    if (list) {
      list.push(item)
      continue
    }
    plans.set(planID, [item])
  }
  return [...plans.values()]
}

function planStatus(items: Info[]) {
  if (items.length === 0) return "pending"
  if (items.every((item) => item.status === "completed" || item.status === "cancelled")) return "completed"
  if (items.some((item) => item.status === "in_progress" || item.status === "completed")) return "in_progress"
  return "pending"
}

function isActivePlan(items: Info[]) {
  return planStatus(items) !== "completed"
}

function todoColumns(db: { all(query: any): unknown[] }) {
  return (db.all(sql`PRAGMA table_info(todo)`) as Array<{ name?: string }>).map((row) => row.name)
}

export const layer = Layer.effect(
  Service,
  Effect.gen(function* () {
    const bus = yield* Bus.Service

    const currentActivePlanID = (sessionID: SessionID) =>
      Effect.sync(() => {
        const plans = groupRows(historyRowsSync(sessionID))
        const current = plans.find(isActivePlan)
        const planID = current?.[0]?.planID?.trim()
        return planID ? planID : undefined
      })

    const historyRowsSync = (sessionID: SessionID) =>
      Database.use((db) => {
        const columns = todoColumns(db)
        const idColumn = columns.includes("id") ? sql`id` : sql`NULL`
        const planIDColumn = columns.includes("plan_id") ? sql`plan_id` : sql`${`plan_${sessionID}`}`
        if (columns.includes("plan_id")) {
          return db.all(sql`
            select ${idColumn} as id, session_id, ${planIDColumn} as plan_id, content, status, priority, position, time_created, time_updated
            from todo
            where session_id = ${sessionID}
            order by time_created desc, plan_id asc, position asc
          `) as TodoRow[]
        }
        return db.all(sql`
          select ${idColumn} as id, session_id, ${planIDColumn} as plan_id, content, status, priority, position, time_created, time_updated
          from todo
          where session_id = ${sessionID}
          order by position asc
        `) as TodoRow[]
      })

    const update = Effect.fn("Todo.update")(function* (input: { sessionID: SessionID; todos: Info[] }) {
      const providedPlanID = input.todos[0]?.planID?.trim()
      const existingPlanID = yield* currentActivePlanID(input.sessionID)
      const planID = providedPlanID || existingPlanID || todoPlanID(input.todos)
      const todos = input.todos.map((todo) => normalizeTodo(todo, planID))
      yield* Effect.sync(() =>
        Database.transaction((db) => {
          const columns = todoColumns(db)
          const hasID = columns.includes("id")
          const hasPlanID = columns.includes("plan_id")
          if (hasPlanID) {
            db.run(sql`delete from todo where session_id = ${input.sessionID} and plan_id = ${planID}`)
          } else {
            db.delete(TodoTable).where(eq(TodoTable.session_id, input.sessionID)).run()
          }
          if (input.todos.length === 0) return
          if (hasID || hasPlanID) {
            const now = Date.now()
            for (const [position, todo] of todos.entries()) {
              if (hasID && hasPlanID) {
                db.run(sql`
                  insert into todo (id, session_id, plan_id, content, status, priority, position, time_created, time_updated)
                  values (${todo.id}, ${input.sessionID}, ${todo.planID}, ${todo.content}, ${todo.status}, ${todo.priority}, ${position}, ${now}, ${now})
                `)
                continue
              }
              if (hasID) {
                db.run(sql`
                  insert into todo (id, session_id, content, status, priority, position, time_created, time_updated)
                  values (${todo.id}, ${input.sessionID}, ${todo.content}, ${todo.status}, ${todo.priority}, ${position}, ${now}, ${now})
                `)
                continue
              }
              db.run(sql`
                insert into todo (session_id, plan_id, content, status, priority, position, time_created, time_updated)
                values (${input.sessionID}, ${todo.planID}, ${todo.content}, ${todo.status}, ${todo.priority}, ${position}, ${now}, ${now})
              `)
            }
            return
          }
          const now = Date.now()
          for (const [position, todo] of input.todos.entries()) {
            db.run(sql`
              insert into todo (session_id, content, status, priority, position, time_created, time_updated)
              values (${input.sessionID}, ${todo.content}, ${todo.status}, ${todo.priority}, ${position}, ${now}, ${now})
            `)
          }
        }),
      )
      yield* bus.publish(Event.Updated, { sessionID: input.sessionID, planID, todos })
      return todos
    })

    const historyRows = (sessionID: SessionID) => Effect.sync(() => historyRowsSync(sessionID))

    const list = Effect.fn("Todo.list")(function* (sessionID: SessionID) {
      return groupRows(yield* historyRows(sessionID))
    })

    const get = Effect.fn("Todo.get")(function* (sessionID: SessionID) {
      const plans = yield* list(sessionID)
      if (plans.length === 0) return []
      return plans.find(isActivePlan) ?? []
    })

    const activePlanID = Effect.fn("Todo.activePlanID")(function* (sessionID: SessionID) {
      return yield* currentActivePlanID(sessionID)
    })

    const prune = Effect.fn("Todo.prune")(function* (sessionID: SessionID) {
      const currentRows = yield* historyRows(sessionID)
      const plans = groupRows(currentRows)
      if (plans.length <= 20) return
      const keep = new Set(plans.slice(0, 20).map((items) => items[0]?.planID).filter(Boolean))
      const remove = currentRows.filter((row) => row.id && row.plan_id && !keep.has(row.plan_id)).map((row) => row.id!)
      if (remove.length === 0) return
      yield* Effect.sync(() => Database.use((db) => db.delete(TodoTable).where(inArray(TodoTable.id, remove)).run()))
    })

    return Service.of({
      update: (input) =>
        Effect.gen(function* () {
          const todos = yield* update(input)
          yield* prune(input.sessionID)
          return todos
        }),
      get,
      list,
      activePlanID,
    })
  }),
)

export const defaultLayer = layer.pipe(Layer.provide(Bus.layer))

export * as Todo from "./todo"
