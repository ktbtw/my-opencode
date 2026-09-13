import { Effect, Schema } from "effect"
import * as Tool from "./tool"
import DESCRIPTION from "./orchestrate.txt"
import { GlobalBus } from "@/bus/global"
import { InstanceState } from "@/effect/instance-state"
import { validateSubagentPlan, type SubagentPlan } from "@/orchestration"

const Model = Schema.Struct({
  providerID: Schema.String,
  modelID: Schema.String,
  variant: Schema.optional(Schema.String),
})

const Node = Schema.Struct({
  id: Schema.String,
  role: Schema.String,
  prompt: Schema.String,
  dependsOn: Schema.optional(Schema.Array(Schema.String)),
  priority: Schema.optional(Schema.Number),
  model: Schema.optional(Model),
  critical: Schema.optional(Schema.Boolean),
  timeoutMS: Schema.optional(Schema.Number),
})

export const Parameters = Schema.Struct({
  plan_id: Schema.String,
  nodes: Schema.Array(Node),
})

type Metadata = { planID: string; nodeCount: number }

export const OrchestrateTool = Tool.define<typeof Parameters, Metadata, never>(
  "orchestrate",
  Effect.sync(() => ({
    description: DESCRIPTION,
    parameters: Parameters,
    execute: (params, ctx) =>
      Effect.gen(function* () {
        yield* ctx.ask({
          permission: "task",
          patterns: ["orchestrate"],
          always: ["*"],
          metadata: { plan_id: params.plan_id, node_count: params.nodes.length },
        })
        const plan: SubagentPlan = validateSubagentPlan({
          id: params.plan_id,
          semanticAgentID: process.env.OPENCODE_SEMANTIC_AGENT_ID?.trim() || "coding-assistant",
          nodes: params.nodes.map((node) => ({
            ...node,
            dependsOn: node.dependsOn ? [...node.dependsOn] : undefined,
            role: node.role as SubagentPlan["nodes"][number]["role"],
            priority: node.priority === undefined ? undefined : Math.trunc(node.priority),
            timeoutMS: node.timeoutMS === undefined ? undefined : Math.trunc(node.timeoutMS),
          })),
        })
        const instance = yield* InstanceState.context
        GlobalBus.emit("event", {
          directory: instance.directory,
          payload: {
            type: "opencode.orchestration.plan",
            properties: { parentSessionID: ctx.sessionID, plan },
          },
        })
        return {
          title: `Subagent plan: ${plan.nodes.length} nodes`,
          metadata: { planID: plan.id, nodeCount: plan.nodes.length },
          output: `Submitted ${plan.id} with ${plan.nodes.length} subagent nodes. Continue primary work while the plan runs.`,
        }
      }),
  })),
)
