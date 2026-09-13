import { BusEvent } from "@/bus/bus-event"
import { Bus } from "@/bus"
import { InstanceState } from "@/effect/instance-state"
import { SessionID } from "./schema"
import { NonNegativeInt } from "@opencode-ai/core/schema"
import { Effect, Layer, Context, Schema, Stream } from "effect"

export const Info = Schema.Union([
  Schema.Struct({
    type: Schema.Literal("idle"),
  }),
  Schema.Struct({
    type: Schema.Literal("retry"),
    attempt: NonNegativeInt,
    maxAttempts: Schema.optional(NonNegativeInt),
    message: Schema.String,
    action: Schema.optional(
      Schema.Struct({
        reason: Schema.String,
        provider: Schema.String,
        title: Schema.String,
        message: Schema.String,
        label: Schema.String,
        link: Schema.optional(Schema.String),
      }),
    ),
    next: NonNegativeInt,
  }),
  Schema.Struct({
    type: Schema.Literal("busy"),
  }),
]).annotate({ identifier: "SessionStatus" })
export type Info = Schema.Schema.Type<typeof Info>

export const Event = {
  Status: BusEvent.define(
    "session.status",
    Schema.Struct({
      sessionID: SessionID,
      status: Info,
    }),
  ),
  // deprecated
  Idle: BusEvent.define(
    "session.idle",
    Schema.Struct({
      sessionID: SessionID,
    }),
  ),
}

export interface Interface {
  readonly get: (sessionID: SessionID) => Effect.Effect<Info>
  readonly list: () => Effect.Effect<Map<SessionID, Info>>
  readonly set: (sessionID: SessionID, status: Info) => Effect.Effect<void>
  /** Resolves on the next idle transition, without repeatedly polling session state. */
  readonly waitUntilIdle: (sessionID: SessionID) => Effect.Effect<void>
}

export class Service extends Context.Service<Service, Interface>()("@opencode/SessionStatus") {}

export const layer = Layer.effect(
  Service,
  Effect.gen(function* () {
    const bus = yield* Bus.Service

    const state = yield* InstanceState.make(
      Effect.fn("SessionStatus.state")(() => Effect.succeed(new Map<SessionID, Info>())),
    )

    const get = Effect.fn("SessionStatus.get")(function* (sessionID: SessionID) {
      const data = yield* InstanceState.get(state)
      return data.get(sessionID) ?? { type: "idle" as const }
    })

    const list = Effect.fn("SessionStatus.list")(function* () {
      return new Map(yield* InstanceState.get(state))
    })

    const set = Effect.fn("SessionStatus.set")(function* (sessionID: SessionID, status: Info) {
      const data = yield* InstanceState.get(state)
      if (status.type === "idle") {
        data.delete(sessionID)
      } else {
        data.set(sessionID, status)
      }
      yield* bus.publish(Event.Status, { sessionID, status })
      if (status.type === "idle") yield* bus.publish(Event.Idle, { sessionID })
    })

    const waitUntilIdle: Interface["waitUntilIdle"] = Effect.fn("SessionStatus.waitUntilIdle")(function* (sessionID) {
      yield* Effect.scoped(
        Effect.gen(function* () {
          if ((yield* get(sessionID)).type === "idle") return

          // Subscribe before checking again so an idle transition cannot be missed.
          const events = yield* bus.subscribe(Event.Status)
          if ((yield* get(sessionID)).type === "idle") return
          yield* events.pipe(
            Stream.filter(
              (event) => event.properties.sessionID === sessionID && event.properties.status.type === "idle",
            ),
            Stream.take(1),
            Stream.runDrain,
          )
        }),
      )
    })

    return Service.of({ get, list, set, waitUntilIdle })
  }),
)

export const defaultLayer = layer.pipe(Layer.provide(Bus.layer))

export * as SessionStatus from "./status"
