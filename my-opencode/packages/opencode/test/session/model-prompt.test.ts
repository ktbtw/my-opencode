import { describe, expect, test } from "bun:test"
import { extractModelPrompt, MODEL_PROMPT_END, MODEL_PROMPT_START } from "../../src/session/model-prompt"

describe("extractModelPrompt", () => {
  test("无包装标记时原样返回", () => {
    expect(extractModelPrompt("【全局提示词】rule")).toEqual({ rest: "【全局提示词】rule" })
    expect(extractModelPrompt("")).toEqual({})
    expect(extractModelPrompt(undefined)).toEqual({})
  })

  test("取出包装内的模型提示词并保留其余系统提示词", () => {
    const system = `${MODEL_PROMPT_START}\nds破甲协议\n${MODEL_PROMPT_END}\n\n【全局提示词】rule`
    expect(extractModelPrompt(system)).toEqual({ modelPrompt: "ds破甲协议", rest: "【全局提示词】rule" })
  })

  test("只有包装块时没有其余提示词", () => {
    const system = `${MODEL_PROMPT_START}\nonly\n${MODEL_PROMPT_END}`
    expect(extractModelPrompt(system)).toEqual({ modelPrompt: "only" })
  })

  test("包装标记不完整时不误判", () => {
    const broken = `${MODEL_PROMPT_START}\nbroken`
    expect(extractModelPrompt(broken)).toEqual({ rest: broken })
  })

  test("包装块前后的内容都保留", () => {
    const system = `before\n${MODEL_PROMPT_START}\nprompt\n${MODEL_PROMPT_END}\nafter`
    expect(extractModelPrompt(system)).toEqual({ modelPrompt: "prompt", rest: "before\n\nafter" })
  })
})
