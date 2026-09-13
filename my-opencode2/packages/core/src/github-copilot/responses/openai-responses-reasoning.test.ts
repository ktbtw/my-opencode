import { describe, expect, test } from "bun:test"
import { resolveResponsesReasoning } from "./openai-responses-language-model"

describe("resolveResponsesReasoning", () => {
  test("forwards camelCase effort from any model config", () => {
    expect(resolveResponsesReasoning({ reasoningEffort: "high" })).toEqual({ effort: "high" })
  })

  test("forwards nested reasoning.effort used by OpenRouter-shaped variants", () => {
    expect(resolveResponsesReasoning({ reasoning: { effort: "low" } })).toEqual({ effort: "low" })
  })

  test("forwards snake_case reasoning_effort", () => {
    expect(resolveResponsesReasoning({ reasoning_effort: "xhigh" })).toEqual({ effort: "xhigh" })
  })

  test("keeps summary when present without inventing it", () => {
    expect(resolveResponsesReasoning({ reasoningEffort: "medium", reasoningSummary: "auto" })).toEqual({
      effort: "medium",
      summary: "auto",
    })
  })

  test("drops empty, numeric, and non-token effort values", () => {
    expect(resolveResponsesReasoning({ reasoningEffort: "  " })).toEqual({})
    expect(resolveResponsesReasoning({ reasoningEffort: "16000" })).toEqual({})
    expect(resolveResponsesReasoning({ reasoningEffort: "极致" })).toEqual({})
    expect(resolveResponsesReasoning({})).toEqual({})
    expect(resolveResponsesReasoning(undefined)).toEqual({})
  })
})
