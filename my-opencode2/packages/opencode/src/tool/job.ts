import { BackgroundJob } from "@/background/job"
import { PositiveInt } from "@opencode-ai/core/schema"
import { Effect, Schema } from "effect"
import DESCRIPTION from "./job.txt"
import * as Tool from "./tool"

// A background job must not monopolize the only active Agent turn. The completion
// message resumes the session when the result is ready, so waiting is a short probe.
const DEFAULT_WAIT_MS = 1_000
const MAX_WAIT_MS = 5_000

const Parameters = Schema.Struct({
  action: Schema.Literals(["list", "status", "logs", "wait", "stop"]).annotate({
    description: "The background job operation to perform",
  }),
  job_id: Schema.optional(Schema.String).annotate({
    description: "Required for status, logs, wait, and stop",
  }),
  timeout_ms: Schema.optional(PositiveInt).annotate({
    description: "Maximum milliseconds to wait when action=wait (default: 1000, capped at 5000)",
  }),
})

function currentOutput(info: BackgroundJob.Info) {
  if (info.output) return info.output
  if (info.error) return info.error
  if (info.status === "running") return "Job is still running."
  if (info.status === "cancelled") return "Job was stopped."
  return "(no output)"
}

function format(info: BackgroundJob.Info, output = currentOutput(info)) {
  return [
    `job_id: ${info.id}`,
    `type: ${info.type}`,
    `state: ${info.status}`,
    `started_at: ${info.started_at}`,
    ...(info.completed_at ? [`completed_at: ${info.completed_at}`] : []),
    "",
    "<job_output>",
    output,
    "</job_output>",
  ].join("\n")
}

function formatList(items: BackgroundJob.Info[]) {
  if (items.length === 0) return "No background jobs."
  return items
    .map((item) =>
      [
        `job_id: ${item.id}`,
        `type: ${item.type}`,
        `state: ${item.status}`,
        ...(item.title ? [`title: ${item.title}`] : []),
      ].join("\n"),
    )
    .join("\n\n")
}

export const JobTool = Tool.define(
  "job",
  Effect.gen(function* () {
    const jobs = yield* BackgroundJob.Service
    const execute: (
      params: Schema.Schema.Type<typeof Parameters>,
      ctx: Tool.Context,
    ) => Effect.Effect<Tool.ExecuteResult> = (params, _ctx) =>
      Effect.gen(function* () {
        if (params.action === "list") {
          const items = yield* jobs.list()
          return {
            title: "Background jobs",
            metadata: { action: params.action, count: items.length },
            output: formatList(items),
          }
        }

        if (!params.job_id) return yield* Effect.fail(new Error(`job_id is required for action=${params.action}`))

        if (params.action === "stop") {
          const info = yield* jobs.cancel(params.job_id)
          if (!info) return yield* Effect.fail(new Error(`Background job not found: ${params.job_id}`))
          return {
            title: "Stop background job",
            metadata: { action: params.action, job_id: info.id, state: info.status },
            output: format(info),
          }
        }

        const requestedWait = params.timeout_ms ?? DEFAULT_WAIT_MS
        const waitMs = Math.min(requestedWait, MAX_WAIT_MS)
        const result =
          params.action === "wait"
            ? yield* jobs.wait({ id: params.job_id, timeout: waitMs })
            : { info: yield* jobs.get(params.job_id), timedOut: false }
        if (!result.info) return yield* Effect.fail(new Error(`Background job not found: ${params.job_id}`))
        const output = result.timedOut
          ? `Still running after ${waitMs}ms. Continue other work and use the completion message or job status when needed.\n\n${currentOutput(result.info)}`
          : currentOutput(result.info)

        return {
          title: params.action === "logs" ? "Background job logs" : "Background job status",
          metadata: {
            action: params.action,
            job_id: result.info.id,
            state: result.info.status,
            timed_out: result.timedOut,
            ...(params.action === "wait" ? { wait_ms: waitMs, wait_capped: requestedWait > MAX_WAIT_MS } : {}),
          },
          output: format(result.info, output),
        }
      }).pipe(Effect.orDie)

    return {
      description: DESCRIPTION,
      parameters: Parameters,
      execute,
    }
  }),
)
