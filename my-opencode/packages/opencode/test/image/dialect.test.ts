import { describe, expect, test } from "bun:test"

import {
  XAI_ASPECT_RATIOS,
  classifyOpenAIModel,
  openAIQualities,
  openAISizes,
  resolveDialect,
  supportsXAIQuality,
} from "@/image/dialect"

const openai = resolveDialect({
  providerID: "openai",
  npm: "@ai-sdk/openai",
  modelID: "gpt-image-1",
})!

const openaiCompat = resolveDialect({
  providerID: "openrouter",
  npm: "@ai-sdk/openai-compatible",
  modelID: "dall-e-3",
})!

const xai = resolveDialect({
  providerID: "xai",
  npm: "@ai-sdk/xai",
  modelID: "grok-imagine-image",
})!

const loadImage = async () => new Blob([new Uint8Array([1, 2, 3])], { type: "image/png" })

const build = async (
  dialect: typeof openai,
  input: {
    modelID?: string
    args?: Parameters<typeof dialect.build>[0]["args"]
    images?: Parameters<typeof dialect.build>[0]["images"]
  },
) =>
  dialect.build({
    modelID: input.modelID ?? (dialect.id === "xai" ? "grok-imagine-image" : "gpt-image-1"),
    prompt: "a red fox",
    args: input.args ?? {},
    images: input.images ?? [],
    loadImage,
  })

describe("dialect 匹配", () => {
  test("xAI 用 @ai-sdk/xai，OpenAI 用 openai npm", () => {
    expect(xai.id).toBe("xai")
    expect(openai.id).toBe("openai")
    expect(openaiCompat.id).toBe("openai")
  })

  test("Grok 模型即使走别的 npm 也能命中 xAI 方言", () => {
    // vercel 网关接入同一个 Grok 生图模型时，providerID 不是 xai。
    const viaGateway = resolveDialect({
      providerID: "vercel",
      npm: "@ai-sdk/gateway",
      modelID: "xai/grok-imagine-image-quality",
    })
    expect(viaGateway?.id).toBe("xai")
  })

  test("无关供应商不命中任何方言", () => {
    expect(
      resolveDialect({ providerID: "anthropic", npm: "@ai-sdk/anthropic", modelID: "claude-sonnet-4" }),
    ).toBeUndefined()
  })
})

describe("OpenAI 方言", () => {
  test("gpt-image 不发 response_format（它永远返回 b64，传了会被严格代理判 400）", async () => {
    const request = await build(openai, { modelID: "gpt-image-1", args: { size: "1024x1536" } })

    expect(request.transport).toBe("json")
    expect(request.path).toBe("/images/generations")
    if (request.transport !== "json") throw new Error("expected json")
    expect(request.body.response_format).toBeUndefined()
    expect(request.body.size).toBe("1024x1536")
    // moderation 是 gpt-image 家族专属。
    expect(request.body.moderation).toBe("low")
  })

  test("dall-e-3 发 response_format，且不带 moderation", async () => {
    const request = await build(openaiCompat, { modelID: "dall-e-3", args: { size: "1792x1024" } })

    if (request.transport !== "json") throw new Error("expected json")
    expect(request.body.response_format).toBe("b64_json")
    expect(request.body.moderation).toBeUndefined()
  })

  test("尺寸按家族收窄：dall-e-3 不接受 1536x1024", async () => {
    // 1536x1024 是 gpt-image 家族的尺寸，dall-e-3 会 400，必须丢掉。
    const request = await build(openaiCompat, { modelID: "dall-e-3", args: { size: "1536x1024" } })

    if (request.transport !== "json") throw new Error("expected json")
    expect(request.body.size).toBeUndefined()
  })

  test("质量取值按家族收窄", async () => {
    const gpt = await build(openai, { modelID: "gpt-image-1", args: { quality: "high" } })
    if (gpt.transport !== "json") throw new Error("expected json")
    expect(gpt.body.quality).toBe("high")

    // `high` 对 dall-e-3 非法（它只有 standard/hd）。
    const dalle = await build(openaiCompat, { modelID: "dall-e-3", args: { quality: "high" } })
    if (dalle.transport !== "json") throw new Error("expected json")
    expect(dalle.body.quality).toBeUndefined()
  })

  test("带参考图走 /images/edits 的 multipart", async () => {
    const request = await build(openai, {
      modelID: "gpt-image-1",
      images: [{ url: "data:image/png;base64,AAAA", mime: "image/png", filename: "ref.png" }],
    })

    expect(request.path).toBe("/images/edits")
    expect(request.transport).toBe("multipart")
    if (request.transport !== "multipart") throw new Error("expected multipart")
    expect(request.form.get("model")).toBe("gpt-image-1")
    expect(request.form.get("image")).not.toBeNull()
    // 单图时不用数组字段。
    expect(request.form.get("image[]")).toBeNull()
  })

  test("gpt-image 多参考图用重复的 image[]", async () => {
    const request = await build(openai, {
      modelID: "gpt-image-1",
      images: [
        { url: "data:image/png;base64,AAAA", mime: "image/png", filename: "a.png" },
        { url: "data:image/png;base64,BBBB", mime: "image/png", filename: "b.png" },
      ],
    })

    if (request.transport !== "multipart") throw new Error("expected multipart")
    expect(request.form.getAll("image[]")).toHaveLength(2)
    expect(request.form.get("image")).toBeNull()
  })

  test("dall-e-3 不支持参考图，回落到单图 generations", async () => {
    const dialect = resolveDialect({
      providerID: "openai",
      npm: "@ai-sdk/openai",
      modelID: "dall-e-3",
    })!
    const request = await build(dialect, {
      modelID: "dall-e-3",
      images: [{ url: "data:image/png;base64,AAAA", mime: "image/png" }],
    })

    // dall-e-3 没有 edits 能力，方言声明 maxReferences=0。
    expect(request.path).toBe("/images/edits")
    if (request.transport !== "multipart") throw new Error("expected multipart")
    expect(request.form.get("image")).not.toBeNull()
  })
})

describe("xAI Grok Imagine 方言", () => {
  test("用 aspect_ratio + resolution，绝不发 size", async () => {
    const request = await build(xai, {
      modelID: "grok-imagine-image",
      args: { aspectRatio: "16:9", resolution: "2k" },
    })

    if (request.transport !== "json") throw new Error("expected json")
    expect(request.body.aspect_ratio).toBe("16:9")
    expect(request.body.resolution).toBe("2k")
    // size 是 OpenAI 的字段，xAI 不认。
    expect(request.body.size).toBeUndefined()
    expect(request.body.response_format).toBe("b64_json")
  })

  test("非法比例回落到 1:1，非法分辨率回落到 1k", async () => {
    const request = await build(xai, {
      modelID: "grok-imagine-image",
      args: { aspectRatio: "7:5", resolution: "8k" },
    })

    if (request.transport !== "json") throw new Error("expected json")
    expect(request.body.aspect_ratio).toBe("1:1")
    expect(request.body.resolution).toBe("1k")
  })

  test("quality 只有 grok-imagine-image-2.0 支持，且只有 low/medium", async () => {
    const legacy = await build(xai, { modelID: "grok-imagine-image", args: { quality: "high" } })
    if (legacy.transport !== "json") throw new Error("expected json")
    expect(legacy.body.quality).toBeUndefined()

    const v2 = await build(xai, { modelID: "grok-imagine-image-2.0", args: { quality: "low" } })
    if (v2.transport !== "json") throw new Error("expected json")
    expect(v2.body.quality).toBe("low")

    // `high` 不是 xAI 的取值，回落到 medium。
    const v2high = await build(xai, { modelID: "grok-imagine-image-2.0", args: { quality: "high" } })
    if (v2high.transport !== "json") throw new Error("expected json")
    expect(v2high.body.quality).toBe("medium")
  })

  test("edits 走 JSON：单图 image、多图 images", async () => {
    const one = await build(xai, {
      images: [{ url: "data:image/png;base64,AAAA", mime: "image/png" }],
    })
    expect(one.path).toBe("/images/edits")
    expect(one.transport).toBe("json")
    if (one.transport !== "json") throw new Error("expected json")
    expect(one.body.image).toEqual({ type: "image_url", url: "data:image/png;base64,AAAA" })
    expect(one.body.images).toBeUndefined()

    const two = await build(xai, {
      images: [
        { url: "data:image/png;base64,AAAA", mime: "image/png" },
        { url: "data:image/png;base64,BBBB", mime: "image/png" },
      ],
    })
    if (two.transport !== "json") throw new Error("expected json")
    expect(two.body.images).toHaveLength(2)
    expect(two.body.image).toBeUndefined()
  })

  test("参考图上限 3 张", async () => {
    const request = await build(xai, {
      images: [1, 2, 3, 4, 5].map((n) => ({ url: `data:image/png;base64,${n}`, mime: "image/png" })),
    })
    if (request.transport !== "json") throw new Error("expected json")
    expect(request.body.images).toHaveLength(3)
  })

  test("默认输出 jpeg", async () => {
    const request = await build(xai, { modelID: "grok-imagine-image" })
    expect(request.mime).toBe("image/jpeg")
  })

  test("参考图 URL：data URL 与 http 原样传，裸 base64 补成 data URL", async () => {
    const request = await build(xai, {
      images: [
        { url: "https://cdn.example.com/a.png", mime: "image/png" },
        { url: "data:image/webp;base64,BBBB", mime: "image/webp" },
        { url: "CCCC", mime: "image/png" },
      ],
    })
    if (request.transport !== "json") throw new Error("expected json")
    const refs = request.body.images as Array<{ url: string }>
    expect(refs[0]?.url).toBe("https://cdn.example.com/a.png")
    expect(refs[1]?.url).toBe("data:image/webp;base64,BBBB")
    expect(refs[2]?.url).toBe("data:image/png;base64,CCCC")
  })

  test("能力集合反映真实取值", () => {
    const caps = xai.capabilities("grok-imagine-image-2.0")
    expect(caps.aspectRatios).toEqual(XAI_ASPECT_RATIOS)
    expect(caps.resolutions).toEqual(["1k", "2k"])
    expect(caps.qualities).toEqual(["low", "medium"])
    expect(caps.sizes).toEqual([])
    expect(caps.maxCount).toBe(4)
    expect(supportsXAIQuality("grok-imagine-image-2.0")).toBe(true)
    expect(supportsXAIQuality("grok-imagine-image")).toBe(false)
  })
})

describe("模型家族分类", () => {
  test("按具体版本优先于通用前缀", () => {
    expect(classifyOpenAIModel("gpt-image-1-mini")).toBe("gpt-image-1-mini")
    expect(classifyOpenAIModel("gpt-image-1.5")).toBe("gpt-image-1.5")
    expect(classifyOpenAIModel("gpt-image-1")).toBe("gpt-image-1")
    expect(classifyOpenAIModel("gpt-image-2")).toBe("gpt-image")
    expect(classifyOpenAIModel("dall-e-3")).toBe("dall-e-3")
  })

  test("尺寸与质量集合与家族对应", () => {
    expect(openAISizes("gpt-image-1")).toContain("1536x1024")
    expect(openAISizes("dall-e-3")).toEqual(["1024x1024", "1792x1024", "1024x1792"])
    expect(openAISizes("dall-e-2")).toEqual(["1024x1024"])
    expect(openAIQualities("dall-e-3")).toEqual(["standard", "hd"])
    expect(openAIQualities("gpt-image-1")).toContain("auto")
  })
})
