import { describe, expect, test } from "bun:test"
import {
  abandonMessage,
  budgetExceeded,
  budgetWarning,
  charsForTokens,
  defaultBudget,
  isEcho,
  isLooping,
  inspect,
  stalled,
} from "../../src/session/reasoning-guard"

/// 非周期噪声：用来验证"只有重复才算循环"，任何真实长文本都不该被误判。
function noise(length: number) {
  let out = ""
  let seed = 1
  while (out.length < length) {
    seed = (seed * 1103515245 + 12345) & 0x7fffffff
    out += String.fromCharCode(97 + (seed % 26))
  }
  return out.slice(0, length)
}

describe("reasoning guard", () => {
  test("echo detection needs the whole reasoning, not a shared opening", () => {
    expect(isEcho("同样的内容", "同样的内容")).toBe(true)
    expect(isEcho("同样的内容\n", "  同样的内容")).toBe(true)
    expect(isEcho("同样的内容，后面还有", "同样的内容")).toBe(false)
    expect(isEcho("", "同样的内容")).toBe(false)
    expect(isEcho("任意正文", "")).toBe(false)
  })

  // Windows gg同行 实测：5 次重发里 4 次不是"整段相同"，而是"正文是思考里的一大段"
  // （正文 82k–88k 字符，原样出现在该步 179k–478k 字符的思考里）。
  test("echo detection also covers a long block copied out of the reasoning", () => {
    const reasoning = noise(50_000)
    expect(isEcho(reasoning.slice(10_000, 95_000), reasoning)).toBe(true)
    // 正常回答不会整段从思考里搬
    expect(isEcho(reasoning.slice(0, 300), reasoning)).toBe(false)
    expect(isEcho("**重大突破**：算法已破解，KEY = d6d2aab4…", reasoning)).toBe(false)
  })

  test("budget uses the same 4-chars-per-token scale as Token.estimate", () => {
    expect(charsForTokens(1_000)).toBe(4_000)
    expect(budgetExceeded("x".repeat(3_999), 1_000)).toBe(false)
    expect(budgetExceeded("x".repeat(4_000), 1_000)).toBe(true)
    expect(budgetExceeded("x".repeat(10), 0)).toBe(false)
    expect(budgetWarning("x".repeat(2_400), 1_000)).toBe(true)
    expect(budgetWarning("x".repeat(2_399), 1_000)).toBe(false)
  })

  test("looping repeats a window, while long unique reasoning does not", () => {
    const block = "同一段推理。".repeat(70).slice(0, 400)
    expect(isLooping(block + "过渡" + block + "过渡" + block)).toBe(true)
    expect(isLooping("别的话题。" + block)).toBe(false)
    expect(isLooping(block.repeat(2))).toBe(false)
    // 退化重复（同一文字输出几千字符）同样按循环处理
    expect(isLooping("x".repeat(4_000))).toBe(true)
  })

  test("inspect reports degenerate repetition first, then budget", () => {
    const block = "重复内容。".repeat(90).slice(0, 400)
    expect(inspect({ reasoning: block + "a" + block + "a" + block, budgetTokens: 10_000 }).reason).toBe("repeat")
    expect(inspect({ reasoning: noise(8_000), budgetTokens: 1_000 }).reason).toBe("budget")
    expect(inspect({ reasoning: "正常长度的思考", budgetTokens: 1_000 }).abandon).toBe(false)
  })

  // 实测：真实"打转"没有任何逐字重复（400 字符窗口 9 个样本 0 命中），
  // 它的文字形态是"反复重来"，密度与正常收敛的长思考完全重叠 —— 所以只能靠预算判。
  test("real circling is textually unique and only the budget catches it", () => {
    const circling = Array.from({ length: 400 }, (_, i) =>
      `Let me ${i % 2 ? "reconsider" : "check"} point ${i}: whether the pool should ${i % 3 ? "fail over" : "cooldown"}.`,
    ).join(" ")
    expect(isLooping(circling)).toBe(false)
    expect(inspect({ reasoning: circling, budgetTokens: 1_000_000 }).abandon).toBe(false)
    expect(inspect({ reasoning: circling, budgetTokens: 1_000 }).reason).toBe("budget")
  })

  // 实测：合法长思考到过 82k token（Windows 一步 327k 字符），所以预算要跟着输出上限走。
  test("budget scales with the model's output limit", () => {
    expect(defaultBudget(128_000)).toBe(96_000)
    expect(defaultBudget(384_000)).toBe(288_000)
    expect(defaultBudget(32_000)).toBe(24_000)
    expect(defaultBudget(0)).toBe(96_000)
  })

  test("stalled needs both a large reasoning and no answer", () => {
    expect(stalled({ reasoning: noise(3_000), budgetTokens: 1_000, hasContent: false })).toBe(true)
    expect(stalled({ reasoning: noise(3_000), budgetTokens: 1_000, hasContent: true })).toBe(false)
    expect(stalled({ reasoning: noise(500), budgetTokens: 1_000, hasContent: false })).toBe(false)
  })

  test("abandon message is actionable", () => {
    expect(abandonMessage("repeat", 48_000)).toContain("重复")
    expect(abandonMessage("budget", 48_000)).toContain("48000")
    expect(abandonMessage("budget", 48_000)).toContain("思考强度")
  })
})
