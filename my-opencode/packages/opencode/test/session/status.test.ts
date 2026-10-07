import { describe, expect } from "bun:test"
import { Bus } from "@/bus"
import { SessionStatus } from "@/session/status"
import { Effect, Fiber, Layer } from "effect"
import { SessionID } from "../../src/session/schema"
import { testEffect } from "../lib/effect"

const it = testEffect(Layer.mergeAll(Bus.defaultLayer, SessionStatus.defaultLayer))

describe("SessionStatus", () => {
  it.instance("wakes idle waiters from a status event", () =>
    Effect.gen(function* () {
      const status = yield* SessionStatus.Service
      const sessionID = SessionID.make("ses_status_wait")

      yield* status.set(sessionID, { type: "busy" })
      const waiting = yield* status.waitUntilIdle(sessionID).pipe(Effect.forkChild)
      yield* Effect.yieldNow

      yield* status.set(sessionID, { type: "idle" })
      yield* Fiber.join(waiting)
      expect((yield* status.get(sessionID)).type).toBe("idle")
    }),
  )
})
