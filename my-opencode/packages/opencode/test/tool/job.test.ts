import { describe, expect } from "bun:test"
import { Agent } from "@/agent/agent"
import { BackgroundJob } from "@/background/job"
import { MessageID, SessionID } from "@/session/schema"
import { JobTool } from "@/tool/job"
import { Truncate } from "@/tool/truncate"
import { Deferred, Effect, Layer } from "effect"
import { testEffect } from "../lib/effect"

const it = testEffect(Layer.mergeAll(Agent.defaultLayer, BackgroundJob.defaultLayer, Truncate.defaultLayer))

const ctx = {
  sessionID: SessionID.make("ses_test"),
  messageID: MessageID.make("msg_test"),
  agent: "build",
  abort: new AbortController().signal,
  messages: [],
  metadata: () => Effect.void,
  ask: () => Effect.void,
}

describe("tool.job", () => {
  it.instance("lists and inspects live background jobs", () =>
    Effect.gen(function* () {
      const jobs = yield* BackgroundJob.Service
      const tool = yield* JobTool
      const def = yield* tool.init()
      const job = yield* jobs.start({ type: "shell", title: "server", run: Effect.never })
      yield* jobs.update({ id: job.id, output: "server ready" })

      const list = yield* def.execute({ action: "list" }, ctx)
      const status = yield* def.execute({ action: "status", job_id: job.id }, ctx)

      expect(list.output).toContain(job.id)
      expect(list.output).toContain("state: running")
      expect(status.output).toContain("server ready")
      yield* jobs.cancel(job.id)
    }),
  )

  it.instance("waits for completion and stops running jobs", () =>
    Effect.gen(function* () {
      const jobs = yield* BackgroundJob.Service
      const tool = yield* JobTool
      const def = yield* tool.init()
      const done = yield* Deferred.make<void>()
      const first = yield* jobs.start({
        type: "shell",
        run: Deferred.await(done).pipe(Effect.as("finished")),
      })
      yield* Deferred.succeed(done, undefined)

      const waited = yield* def.execute({ action: "wait", job_id: first.id, timeout_ms: 1_000 }, ctx)
      expect(waited.output).toContain("state: completed")
      expect(waited.output).toContain("finished")

      const second = yield* jobs.start({ type: "shell", run: Effect.never })
      const stopped = yield* def.execute({ action: "stop", job_id: second.id }, ctx)
      expect(stopped.output).toContain("state: cancelled")
    }),
  )

  it.instance("uses a short default wait to keep the Agent turn responsive", () =>
    Effect.gen(function* () {
      const jobs = yield* BackgroundJob.Service
      const tool = yield* JobTool
      const def = yield* tool.init()
      const job = yield* jobs.start({ type: "shell", run: Effect.never })
      const started = Date.now()
      const result = yield* def.execute({ action: "wait", job_id: job.id }, ctx)

      expect(result.metadata.timed_out).toBe(true)
      expect(result.metadata.wait_ms).toBe(1_000)
      expect(result.output).toContain("Still running after 1000ms")
      expect(Date.now() - started).toBeLessThan(2_000)
      yield* jobs.cancel(job.id)
    }),
  )
})
