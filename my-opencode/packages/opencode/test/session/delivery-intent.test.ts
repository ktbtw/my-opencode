import { describe, expect, test } from "bun:test"
import { deliveryRequired, mayProduceFiles } from "../../src/session/delivery-intent"

const text = (value: string) => ({ parts: [{ type: "text", text: value }] })

describe("delivery intent", () => {
  test("pure chat never carries delivery context", () => {
    for (const message of ["你好", "叫爸爸", "在吗", "测试", "thanks", "ok"]) {
      expect(deliveryRequired(text(message))).toBe(false)
      expect(mayProduceFiles(text(message))).toBe(false)
    }
  })

  test("workspace work keeps the artifact scope without forcing delivery", () => {
    for (const message of ["修一下这个 bug", "帮我把这个函数改成 async", "分析下这段代码", "跑一遍测试"]) {
      expect(deliveryRequired(text(message))).toBe(false)
      expect(mayProduceFiles(text(message))).toBe(true)
    }
  })

  test("explicit file requests force delivery", () => {
    for (const message of ["给我一份 pdf 报告", "把结果作为附件发我", "导出成 csv 文件"]) {
      expect(deliveryRequired(text(message))).toBe(true)
      expect(mayProduceFiles(text(message))).toBe(true)
    }
  })

  test("metadata overrides the text heuristic", () => {
    expect(deliveryRequired({ ...text("你好"), metadata: { delivery_required: "true" } })).toBe(true)
    expect(deliveryRequired({ ...text("导出成 csv 文件"), metadata: { delivery_required: "false" } })).toBe(false)
  })

  test("empty and non-text parts carry nothing", () => {
    expect(deliveryRequired({})).toBe(false)
    expect(mayProduceFiles({})).toBe(false)
    expect(mayProduceFiles({ parts: [{ type: "file", text: undefined }] })).toBe(false)
  })
})
