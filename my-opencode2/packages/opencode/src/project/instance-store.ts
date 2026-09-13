import { GlobalBus } from "@/bus/global"
import { WorkspaceContext } from "@/control-plane/workspace-context"
import { InstanceRef } from "@/effect/instance-ref"
import { disposeInstance as runDisposers } from "@/effect/instance-registry"
import { AppFileSystem } from "@opencode-ai/core/filesystem"
import { Context, Deferred, Duration, Effect, Exit, Layer, Scope } from "effect"
import { type InstanceContext } from "./instance-context"
import { InstanceBootstrap } from "./bootstrap-service"
import * as Project from "./project"

export interface LoadInput {
  directory: string
  worktree?: string
  project?: Project.Info
}

export interface Interface {
  readonly load: (input: LoadInput) => Effect.Effect<InstanceContext>
  readonly reload: (input: LoadInput) => Effect.Effect<InstanceContext>
  readonly dispose: (ctx: InstanceContext) => Effect.Effect<void>
  readonly disposeAll: () => Effect.Effect<void>
  readonly provide: <A, E, R>(input: LoadInput, effect: Effect.Effect<A, E, R>) => Effect.Effect<A, E, R>
}

export class Service extends Context.Service<Service, Interface>()("@opencode/InstanceStore") {}

interface Entry {
  readonly deferred: Deferred.Deferred<InstanceContext>
  readonly directory: string
  refs: number
  ctx?: InstanceContext
  disposed?: boolean
}

export const layer: Layer.Layer<Service, never, Project.Service | InstanceBootstrap.Service> = Layer.effect(
  Service,
  Effect.gen(function* () {
    const project = yield* Project.Service
    const bootstrap = yield* InstanceBootstrap.Service
    const scope = yield* Scope.Scope
    const cache = new Map<string, Entry>()
    const entries = new Set<Entry>()

    const boot = (input: LoadInput & { directory: string }) =>
      Effect.gen(function* () {
        const ctx: InstanceContext =
          input.project && input.worktree
            ? {
                directory: input.directory,
                worktree: input.worktree,
                project: input.project,
              }
            : yield* project.fromDirectory(input.directory).pipe(
                Effect.map((result) => ({
                  directory: input.directory,
                  worktree: result.sandbox,
                  project: result.project,
                })),
              )
        yield* bootstrap.run.pipe(Effect.provideService(InstanceRef, ctx))
        return ctx
      }).pipe(Effect.withSpan("InstanceStore.boot"))

    const removeEntry = (directory: string, entry: Entry) =>
      Effect.sync(() => {
        if (cache.get(directory) !== entry) return false
        cache.delete(directory)
        return true
      })

    const completeLoad = (directory: string, input: LoadInput, entry: Entry) =>
      Effect.gen(function* () {
        const exit = yield* Effect.exit(boot({ ...input, directory }))
        if (Exit.isFailure(exit)) {
          yield* removeEntry(directory, entry)
          yield* Effect.sync(() => entries.delete(entry))
        } else {
          entry.ctx = exit.value
        }
        yield* Deferred.done(entry.deferred, exit).pipe(Effect.asVoid)
      })

    const emitDisposed = (input: { directory: string; project?: string }) =>
      Effect.sync(() =>
        GlobalBus.emit("event", {
          directory: input.directory,
          project: input.project,
          workspace: WorkspaceContext.workspaceID,
          payload: {
            type: "server.instance.disposed",
            properties: {
              directory: input.directory,
            },
          },
        }),
      )

    const disposeContext = Effect.fn("InstanceStore.disposeContext")(function* (ctx: InstanceContext) {
      yield* Effect.logInfo("disposing instance").pipe(Effect.annotateLogs("directory", ctx.directory))
      yield* Effect.promise(() => runDisposers(ctx.directory))
      yield* emitDisposed({ directory: ctx.directory, project: ctx.project.id })
    })

    const releaseEntry = Effect.fnUntraced(function* (entry: Entry, force = false) {
      if (entry.disposed) return false
      if (!force) {
        entry.refs = Math.max(0, entry.refs - 1)
        if (entry.refs > 0) return false
      } else {
        entry.refs = 0
      }

      const exit = yield* Deferred.await(entry.deferred).pipe(Effect.exit)
      if (Exit.isFailure(exit)) {
        entry.disposed = true
        entries.delete(entry)
        if (cache.get(entry.directory) === entry) cache.delete(entry.directory)
        return true
      }

      entry.disposed = true
      yield* disposeContext(exit.value)
      entries.delete(entry)
      if (cache.get(entry.directory) === entry) cache.delete(entry.directory)
      return true
    })

    const load = (input: LoadInput): Effect.Effect<InstanceContext> => {
      const directory = AppFileSystem.resolve(input.directory)
      return Effect.uninterruptibleMask((restore) =>
        Effect.gen(function* () {
          const existing = cache.get(directory)
          if (existing) {
            existing.refs++
            return yield* restore(Deferred.await(existing.deferred))
          }

          const entry: Entry = {
            deferred: Deferred.makeUnsafe<InstanceContext>(),
            directory,
            refs: 1,
          }
          cache.set(directory, entry)
          entries.add(entry)
          yield* Effect.gen(function* () {
            yield* Effect.logInfo("creating instance").pipe(Effect.annotateLogs("directory", directory))
            yield* completeLoad(directory, input, entry)
          }).pipe(Effect.forkIn(scope, { startImmediately: true }))
          return yield* restore(Deferred.await(entry.deferred))
        }),
      ).pipe(Effect.withSpan("InstanceStore.load"))
    }

    const reload = (input: LoadInput): Effect.Effect<InstanceContext> => {
      const directory = AppFileSystem.resolve(input.directory)
      return Effect.uninterruptibleMask((restore) =>
        Effect.gen(function* () {
          const entry: Entry = {
            deferred: Deferred.makeUnsafe<InstanceContext>(),
            directory,
            refs: 1,
          }
          cache.set(directory, entry)
          entries.add(entry)
          yield* Effect.gen(function* () {
            yield* Effect.logInfo("reloading instance").pipe(Effect.annotateLogs("directory", directory))
            // Keep the previous generation alive until its callers release it.
            // Reload must not invalidate state used by an in-flight task.
            yield* completeLoad(directory, input, entry)
          }).pipe(Effect.forkIn(scope, { startImmediately: true }))
          return yield* restore(Deferred.await(entry.deferred))
        }),
      ).pipe(Effect.withSpan("InstanceStore.reload"))
    }

    const dispose = Effect.fn("InstanceStore.dispose")(function* (ctx: InstanceContext) {
      const entry = [...entries].find((item) => item.ctx === ctx)
      if (!entry) return
      yield* releaseEntry(entry)
    })

    const disposeAllOnce = Effect.fnUntraced(function* () {
      yield* Effect.logInfo("disposing all instances")
      yield* Effect.forEach(
        [...entries],
        (entry) =>
          Effect.gen(function* () {
            const exit = yield* Deferred.await(entry.deferred).pipe(Effect.exit)
            if (Exit.isFailure(exit)) {
              yield* Effect.logWarning("instance dispose failed").pipe(
                Effect.annotateLogs({ key: entry.directory, cause: exit.cause }),
              )
              entries.delete(entry)
              if (cache.get(entry.directory) === entry) cache.delete(entry.directory)
              return
            }
            yield* releaseEntry(entry, true)
          }),
        { discard: true },
      )
    })

    const cachedDisposeAll = yield* Effect.cachedWithTTL(disposeAllOnce(), Duration.zero)
    const disposeAll = Effect.fn("InstanceStore.disposeAll")(function* () {
      return yield* cachedDisposeAll
    })

    const provide = <A, E, R>(input: LoadInput, effect: Effect.Effect<A, E, R>): Effect.Effect<A, E, R> =>
      load(input).pipe(Effect.flatMap((ctx) => effect.pipe(Effect.provideService(InstanceRef, ctx))))

    yield* Effect.addFinalizer(() => disposeAll().pipe(Effect.ignore))

    return Service.of({
      load,
      reload,
      dispose,
      disposeAll,
      provide,
    })
  }),
)

export const defaultLayer = layer.pipe(Layer.provide(Project.defaultLayer))

export * as InstanceStore from "./instance-store"
