import { describe, expect } from "bun:test"
import { Effect, Layer } from "effect"
import { CrossSpawnSpawner } from "@opencode-ai/core/cross-spawn-spawner"
import { sql } from "drizzle-orm"
import { Database } from "@/storage/db"
import { Todo } from "../../src/session/todo"
import { SessionTable } from "../../src/session/session.sql"
import { ProjectTable } from "../../src/project/project.sql"
import { ProjectID } from "../../src/project/schema"
import { SessionID } from "../../src/session/schema"
import { resetDatabase } from "../fixture/db"
import { provideTmpdirInstance } from "../fixture/fixture"
import { testEffect } from "../lib/effect"

const it = testEffect(Layer.mergeAll(Todo.defaultLayer, CrossSpawnSpawner.defaultLayer))

function seedSession(sessionID: SessionID) {
  const now = Date.now()
  Database.use((db) => {
    db.insert(ProjectTable)
      .values({
        id: ProjectID.global,
        worktree: "/",
        time_created: now,
        time_updated: now,
        sandboxes: [],
      })
      .onConflictDoNothing()
      .run()
    db.insert(SessionTable)
      .values({
        id: sessionID,
        project_id: ProjectID.global,
        slug: "test",
        directory: "/tmp",
        title: "test",
        version: "0.0.0-test",
        time_created: now,
        time_updated: now,
      })
      .run()
  })
}

describe("Todo.update", () => {
  it.live("writes ids when database schema requires todo.id", () =>
    provideTmpdirInstance(() =>
      Effect.gen(function* () {
        yield* Effect.promise(() => resetDatabase())
        yield* Effect.addFinalizer(() => Effect.promise(() => resetDatabase()).pipe(Effect.ignore))
        const sessionID = SessionID.descending()
        seedSession(sessionID)
        Database.use((db) => {
          db.run(sql`PRAGMA foreign_keys = OFF`)
          db.run(sql`
            CREATE TABLE todo_new (
              id text NOT NULL,
              session_id text NOT NULL,
              content text NOT NULL,
              status text NOT NULL,
              priority text NOT NULL,
              position integer NOT NULL,
              time_created integer NOT NULL,
              time_updated integer NOT NULL,
              CONSTRAINT todo_pk PRIMARY KEY(session_id, position)
            )
          `)
          db.run(sql`DROP TABLE todo`)
          db.run(sql`ALTER TABLE todo_new RENAME TO todo`)
          db.run(sql`CREATE INDEX todo_session_idx ON todo (session_id)`)
          db.run(sql`PRAGMA foreign_keys = ON`)
        })

        const todo = yield* Todo.Service
        yield* todo.update({
          sessionID,
          todos: [{ content: "验证 todo id", status: "pending", priority: "high" }],
        })

        const rows = Database.use(
          (db) =>
            db.all(sql`SELECT id, content, status, priority FROM todo`) as Array<{
              id: string
              content: string
              status: string
              priority: string
            }>,
        )
        expect(rows).toHaveLength(1)
        expect(rows[0].id.startsWith("todo_")).toBe(true)
        expect(rows[0].content).toBe("验证 todo id")
      }),
    ),
  )

  it.live("writes plan ids when database schema requires todo.plan_id", () =>
    provideTmpdirInstance(() =>
      Effect.gen(function* () {
        yield* Effect.promise(() => resetDatabase())
        yield* Effect.addFinalizer(() => Effect.promise(() => resetDatabase()).pipe(Effect.ignore))
        const sessionID = SessionID.descending()
        seedSession(sessionID)
        Database.use((db) => {
          db.run(sql`PRAGMA foreign_keys = OFF`)
          db.run(sql`
            CREATE TABLE todo_new (
              id text PRIMARY KEY NOT NULL,
              session_id text NOT NULL,
              plan_id text NOT NULL,
              content text NOT NULL,
              status text NOT NULL,
              priority text NOT NULL,
              position integer NOT NULL,
              time_created integer NOT NULL,
              time_updated integer NOT NULL
            )
          `)
          db.run(sql`DROP TABLE todo`)
          db.run(sql`ALTER TABLE todo_new RENAME TO todo`)
          db.run(sql`CREATE INDEX todo_session_idx ON todo (session_id)`)
          db.run(sql`CREATE INDEX todo_session_plan_idx ON todo (session_id, plan_id)`)
          db.run(sql`PRAGMA foreign_keys = ON`)
        })

        const todo = yield* Todo.Service
        yield* todo.update({
          sessionID,
          todos: [{ content: "验证 todo plan id", status: "completed", priority: "medium" }],
        })

        const rows = Database.use(
          (db) =>
            db.all(sql`SELECT id, plan_id, content, status, priority FROM todo`) as Array<{
              id: string
              plan_id: string
              content: string
              status: string
              priority: string
            }>,
        )
        expect(rows).toHaveLength(1)
        expect(rows[0].id.startsWith("todo_")).toBe(true)
        expect(rows[0].plan_id.startsWith("plan_")).toBe(true)
        expect(rows[0].plan_id).not.toBe(`plan_${sessionID}`)
        expect(rows[0].content).toBe("验证 todo plan id")
      }),
    ),
  )

  it.live("keeps completed todo batches in history while exposing the active batch", () =>
    provideTmpdirInstance(() =>
      Effect.gen(function* () {
        yield* Effect.promise(() => resetDatabase())
        yield* Effect.addFinalizer(() => Effect.promise(() => resetDatabase()).pipe(Effect.ignore))
        const sessionID = SessionID.descending()
        seedSession(sessionID)

        const todo = yield* Todo.Service
        const completed = yield* todo.update({
          sessionID,
          todos: [{ content: "完成第一批计划", status: "completed", priority: "high" }],
        })
        yield* Effect.promise(() => new Promise((resolve) => setTimeout(resolve, 2)))
        const active = yield* todo.update({
          sessionID,
          todos: [{ content: "开始第二批计划", status: "pending", priority: "medium" }],
        })

        expect(completed[0]?.planID).toBeTruthy()
        expect(active[0]?.planID).toBeTruthy()
        expect(active[0]?.planID).not.toBe(completed[0]?.planID)

        const history = yield* todo.list(sessionID)
        expect(history).toHaveLength(2)
        expect(history[0]?.[0]?.planID).toBe(active[0]?.planID)
        expect(history[1]?.[0]?.planID).toBe(completed[0]?.planID)

        const current = yield* todo.get(sessionID)
        expect(current).toHaveLength(1)
        expect(current[0]?.planID).toBe(active[0]?.planID)
      }),
    ),
  )

  it.live("reuses a provided plan id until the plan is completed", () =>
    provideTmpdirInstance(() =>
      Effect.gen(function* () {
        yield* Effect.promise(() => resetDatabase())
        yield* Effect.addFinalizer(() => Effect.promise(() => resetDatabase()).pipe(Effect.ignore))
        const sessionID = SessionID.descending()
        seedSession(sessionID)

        const todo = yield* Todo.Service
        const pending = yield* todo.update({
          sessionID,
          todos: [{ content: "复用计划批次", status: "pending", priority: "high" }],
        })
        const planID = pending[0]?.planID
        expect(planID).toBeTruthy()

        yield* todo.update({
          sessionID,
          todos: [{ id: pending[0]?.id, planID, content: "复用计划批次", status: "completed", priority: "high" }],
        })
        expect(yield* todo.get(sessionID)).toEqual([])
        expect(yield* todo.list(sessionID)).toHaveLength(1)

        yield* Effect.promise(() => new Promise((resolve) => setTimeout(resolve, 2)))
        const next = yield* todo.update({
          sessionID,
          todos: [{ content: "新计划批次", status: "pending", priority: "medium" }],
        })

        expect(next[0]?.planID).toBeTruthy()
        expect(next[0]?.planID).not.toBe(planID)
        expect(yield* todo.list(sessionID)).toHaveLength(2)
      }),
    ),
  )

  it.live("reuses the active plan id for the whole batch while the plan remains unfinished", () =>
    provideTmpdirInstance(() =>
      Effect.gen(function* () {
        yield* Effect.promise(() => resetDatabase())
        yield* Effect.addFinalizer(() => Effect.promise(() => resetDatabase()).pipe(Effect.ignore))
        const sessionID = SessionID.descending()
        seedSession(sessionID)

        const todo = yield* Todo.Service
        const first = yield* todo.update({
          sessionID,
          todos: [{ content: "第一步", status: "in_progress", priority: "high" }],
        })
        const planID = first[0]?.planID
        expect(planID).toBeTruthy()
        expect(yield* todo.activePlanID(sessionID)).toBe(planID)

        const second = yield* todo.update({
          sessionID,
          todos: [
            { content: "第一步", status: "completed", priority: "high" },
            { content: "第二步", status: "pending", priority: "medium" },
          ],
        })

        expect(second).toHaveLength(2)
        expect(second[0]?.planID).toBe(planID)
        expect(second[1]?.planID).toBe(planID)
        expect(yield* todo.activePlanID(sessionID)).toBe(planID)
        expect(yield* todo.list(sessionID)).toHaveLength(1)
      }),
    ),
  )
})
