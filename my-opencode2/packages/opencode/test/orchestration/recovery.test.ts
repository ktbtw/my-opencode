import { describe, expect, test } from "bun:test"
import { recoveryCandidates, restoredPlan, type SubagentNode } from "../../src/orchestration"

const node = (id: string, state: SubagentNode["state"]): SubagentNode => ({
  id,
  role: "repo-explorer",
  prompt: `prompt ${id}`,
  state,
  attempt: state === "running" ? 1 : 0,
  createdAt: 1,
  updatedAt: 2,
})

describe("subagent recovery", () => {
  test("reconstructs a valid plan from durable node snapshots", () => {
    const plan = restoredPlan({
      id: "recovered-plan",
      semanticAgentID: "coding-assistant",
      nodes: [node("inspect", "completed"), { ...node("review", "queued"), dependsOn: ["inspect"] }],
    })
    expect(plan.nodes).toEqual(expect.arrayContaining([expect.objectContaining({ id: "review", dependsOn: ["inspect"] })]))
  })

  test("only considers queued and actual in-flight nodes for recovery checks", () => {
    expect(recoveryCandidates([node("done", "completed"), node("queued", "queued"), node("live", "running"), node("lost", "unknown")]).map((item) => item.id)).toEqual(["queued", "live"])
  })
})
