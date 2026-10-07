import { describe, expect, test } from "bun:test"
import { Database as SQLite } from "bun:sqlite"
import { drizzle } from "drizzle-orm/bun-sqlite"
import path from "path"
import { Effect } from "effect"
import { Global } from "@opencode-ai/core/global"
import { InstallationChannel } from "@opencode-ai/core/installation/version"
import { RuntimeFlags } from "@/effect/runtime-flags"
import { Database, repairSessionMessageSchema } from "@/storage/db"
import { it } from "../lib/effect"

describe("Database.getChannelPath", () => {
  it.effect("returns database path for the current channel", () =>
    Effect.gen(function* () {
      const flags = yield* RuntimeFlags.Service
      const expected = ["latest", "beta", "prod"].includes(InstallationChannel)
        ? path.join(Global.Path.data, "opencode.db")
        : path.join(Global.Path.data, `opencode-${InstallationChannel.replace(/[^a-zA-Z0-9._-]/g, "-")}.db`)

      expect(Database.getChannelPath(flags)).toBe(expected)
    }).pipe(Effect.provide(RuntimeFlags.layer())),
  )

  it.effect("uses the shared database path when channel databases are disabled", () =>
    Effect.gen(function* () {
      const flags = yield* RuntimeFlags.Service

      expect(Database.getChannelPath(flags)).toBe(path.join(Global.Path.data, "opencode.db"))
    }).pipe(Effect.provide(RuntimeFlags.layer({ disableChannelDb: true }))),
  )

  it.effect("accepts RuntimeFlags with skipMigrations for database callers", () =>
    Effect.gen(function* () {
      const flags = yield* RuntimeFlags.Service

      expect(flags.skipMigrations).toBe(true)
      expect(Database.getChannelPath(flags)).toBe(Database.getChannelPath({ disableChannelDb: flags.disableChannelDb }))
    }).pipe(Effect.provide(RuntimeFlags.layer({ skipMigrations: true }))),
  )
})

describe("repairSessionMessageSchema", () => {
  test("removes legacy seq column while preserving messages", () => {
    const sqlite = new SQLite(":memory:")
    const db = drizzle({ client: sqlite })
    sqlite.exec(`
      create table session (
        id text primary key
      );
      create table session_message (
        id text primary key,
        session_id text not null,
        seq integer not null,
        type text not null,
        time_created integer not null,
        time_updated integer not null,
        data text not null
      );
      insert into session (id) values ('ses_1');
      insert into session_message (id, session_id, seq, type, time_created, time_updated, data)
      values ('msg_1', 'ses_1', 1, 'user', 100, 100, '{}');
    `)

    repairSessionMessageSchema(db)

    const columns = sqlite.prepare("PRAGMA table_info(session_message)").all() as { name: string }[]
    expect(columns.map((column) => column.name)).not.toContain("seq")
    expect(sqlite.prepare("select count(*) as count from session_message").get()).toEqual({ count: 1 })
    sqlite.exec(`
      insert into session_message (id, session_id, type, time_created, time_updated, data)
      values ('msg_2', 'ses_1', 'assistant', 101, 101, '{}');
    `)
    expect(sqlite.prepare("select count(*) as count from session_message").get()).toEqual({ count: 2 })
    sqlite.close()
  })
})
