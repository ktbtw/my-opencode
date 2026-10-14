import { describe, expect, test } from "bun:test"
import { Effect } from "effect"
import path from "node:path"
import { tmpdir } from "../fixture/fixture"
import { PromptRegistry } from "../../src/session/prompt-registry"

const resolve = (slots: Record<string, string> | undefined, slotID: string, fallback: string) =>
  Effect.runPromise(PromptRegistry.resolvePromptSlot(slots, slotID, fallback))

describe("session.prompt-registry", () => {
  test("没有 prompt_slots 时使用内置默认", async () => {
    expect(await resolve(undefined, "chat.base_prompt", "内置默认")).toBe("内置默认")
    expect(await resolve({}, "chat.base_prompt", "内置默认")).toBe("内置默认")
  })

  test("槽位未声明时使用内置默认", async () => {
    await using tmp = await tmpdir()
    expect(
      await resolve({ "chat.question_policy": path.join(tmp.path, "q.txt") }, "chat.base_prompt", "内置默认"),
    ).toBe("内置默认")
  })

  test("文件不存在时回落内置默认", async () => {
    await using tmp = await tmpdir()
    expect(await resolve({ "chat.base_prompt": path.join(tmp.path, "missing.txt") }, "chat.base_prompt", "内置默认")).toBe(
      "内置默认",
    )
  })

  test("文件为空时回落内置默认", async () => {
    await using tmp = await tmpdir()
    const file = path.join(tmp.path, "empty.txt")
    await Bun.write(file, "   \n")
    expect(await resolve({ "chat.base_prompt": file }, "chat.base_prompt", "内置默认")).toBe("内置默认")
  })

  test("文件有内容时覆盖内置默认，内容变化后立即生效", async () => {
    await using tmp = await tmpdir()
    const file = path.join(tmp.path, "base.txt")
    await Bun.write(file, "控制台版本 v1")
    expect(await resolve({ "chat.base_prompt": file }, "chat.base_prompt", "内置默认")).toBe("控制台版本 v1")
    await Bun.write(file, "控制台版本 v2 更长的内容")
    expect(await resolve({ "chat.base_prompt": file }, "chat.base_prompt", "内置默认")).toBe("控制台版本 v2 更长的内容")
  })

  test("路径指向目录时回落内置默认", async () => {
    await using tmp = await tmpdir()
    expect(await resolve({ "chat.base_prompt": tmp.path }, "chat.base_prompt", "内置默认")).toBe("内置默认")
  })
})
