import { Effect, Schema } from "effect"
import * as Tool from "./tool"

export const Parameters = Schema.Struct({
  summary: Schema.String.annotate({ description: "Brief summary of what was completed for the active goal" }),
})

type Metadata = {
  goal_completed: boolean
  summary: string
}

export const GoalCompleteTool = Tool.define<typeof Parameters, Metadata, never>(
  "goal_complete",
  Effect.succeed({
    description:
      "Mark the active long-running goal as completed. Use this only when the requested goal is fully finished and no further work is required.",
    parameters: Parameters,
    execute: (params: Schema.Schema.Type<typeof Parameters>, ctx) => {
      if (ctx.extra?.goalMode !== true) {
        return Effect.succeed({
          title: "Goal mode required",
          output: "当前会话未启用目标模式，此工具仅用于目标模式。请继续处理当前任务。",
          metadata: {
            goal_completed: false,
            summary: "",
          },
        })
      }
      return Effect.succeed({
        title: "Goal completed",
        output: params.summary,
        metadata: {
          goal_completed: true,
          summary: params.summary,
        },
      })
    },
  } satisfies Tool.DefWithoutID<typeof Parameters, Metadata>),
)
