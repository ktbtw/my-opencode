/**
 * 生图供应商方言（dialect）。
 *
 * opencode 的生图请求要同时服务两类真实存在的 API：
 *
 * 1. OpenAI Images（`/v1/images/generations` + `/v1/images/edits`）
 *    - `gpt-image-*`：`size` 固定三档 + `auto`，`quality` = low/medium/high/auto，
 *      支持 `background` / `output_format` / `input_fidelity` / `moderation`。
 *      **不接受 `response_format`**（永远返回 b64），严格代理会因此 400。
 *    - `dall-e-3`：`size` = 1024x1024 / 1792x1024 / 1024x1792，`quality` = standard/hd。
 *    - `dall-e-2`：只有 1024x1024 + standard。
 *    - edits 走 multipart；只有 `gpt-image-*` 支持多参考图（重复的 `image[]`）。
 *
 * 2. xAI Grok Imagine（`https://api.x.ai/v1/images/*`）
 *    - **没有 `size`**，用 `aspect_ratio` + `resolution`(1k/2k)。
 *    - `quality` 只有 low/medium，且只有 `grok-imagine-image-2.0` 支持。
 *    - edits 走 **JSON**（不是 multipart）：单图 `image`、多图 `images`，
 *      元素形如 `{ type: "image_url", url: <data URL> }`，最多 3 张。
 *
 * 所以「哪些参数能发、用什么形状发」必须由方言决定，而不是一处硬编码。
 * 之前那种「无条件发 size 和 response_format」的写法对上面每一类都是错的。
 */

export type ImageArgs = {
  n?: number
  size?: string
  quality?: string
  background?: string
  format?: string
  fidelity?: string
  aspectRatio?: string
  resolution?: string
}

export type ImageCapabilities = {
  sizes: string[]
  aspectRatios: string[]
  resolutions: string[]
  qualities: string[]
  formats: string[]
  maxCount: number
  maxReferences: number
}

export type ImageRequestPart = {
  url: string
  mime: string
  filename?: string
}

export type ImageRequest =
  | { transport: "json"; path: string; body: Record<string, unknown>; mime: string }
  | { transport: "multipart"; path: string; form: FormData; mime: string }

export type BuildInput = {
  modelID: string
  prompt: string
  args: ImageArgs
  images: ImageRequestPart[]
  loadImage: (part: ImageRequestPart) => Promise<Blob>
}

export type Dialect = {
  id: "openai" | "xai"
  matches: (input: { providerID: string; npm: string; modelID: string }) => boolean
  capabilities: (modelID: string) => ImageCapabilities
  build: (input: BuildInput) => Promise<ImageRequest>
}

// ---------------------------------------------------------------------------
// OpenAI
// ---------------------------------------------------------------------------

export type OpenAIModelKind =
  | "gpt-image-1.5"
  | "gpt-image-1-mini"
  | "gpt-image-1"
  | "gpt-image"
  | "flux-kontext"
  | "dall-e-3"
  | "dall-e-2"
  | "unknown"

/** 家族判定：`gpt-image-*` 共用一套参数与 edits 形状。 */
export const isGptImageKind = (kind: OpenAIModelKind) =>
  kind === "gpt-image-1.5" || kind === "gpt-image-1-mini" || kind === "gpt-image-1" || kind === "gpt-image"

export function classifyOpenAIModel(modelID: string): OpenAIModelKind {
  const id = modelID.toLowerCase().trim()
  if (id.includes("gpt-image-1.5") || id.includes("gpt-image-1-5")) return "gpt-image-1.5"
  if (id.includes("gpt-image-1-mini")) return "gpt-image-1-mini"
  if (id.includes("gpt-image-1")) return "gpt-image-1"
  if (id.includes("gpt-image")) return "gpt-image"
  if (id.includes("flux-1-kontext")) return "flux-kontext"
  if (id.includes("dall-e-3")) return "dall-e-3"
  if (id.includes("dall-e-2")) return "dall-e-2"
  return "unknown"
}

const GPT_IMAGE_SIZES = ["1024x1024", "1536x1024", "1024x1536", "auto"]
const DALL_E_3_SIZES = ["1024x1024", "1792x1024", "1024x1792"]
const DALL_E_2_SIZES = ["1024x1024"]
const UNKNOWN_SIZES = ["1024x1024", "1536x1024", "1024x1536", "1792x1024", "1024x1792", "auto"]

/** `size` 的合法取值按家族收窄：给 dall-e-3 发 1536x1024 会直接 400。 */
export function openAISizes(kind: OpenAIModelKind): string[] {
  if (isGptImageKind(kind)) return GPT_IMAGE_SIZES
  if (kind === "dall-e-3") return DALL_E_3_SIZES
  if (kind === "dall-e-2") return DALL_E_2_SIZES
  return UNKNOWN_SIZES
}

/** `quality` 的取值集合同样按家族区分。 */
export function openAIQualities(kind: OpenAIModelKind): string[] {
  if (isGptImageKind(kind)) return ["low", "medium", "high", "auto"]
  if (kind === "dall-e-3") return ["standard", "hd"]
  if (kind === "dall-e-2") return ["standard"]
  return ["low", "medium", "high", "auto", "standard", "hd"]
}

/** 参考图上限：只有 `gpt-image-*` 支持多图 edits。 */
export function openAIMaxReferences(kind: OpenAIModelKind): number {
  if (isGptImageKind(kind)) return 5
  if (kind === "flux-kontext") return 1
  if (kind === "dall-e-2") return 1
  return 0
}

const openai: Dialect = {
  id: "openai",
  matches: ({ npm }) => npm === "@ai-sdk/openai" || npm === "@ai-sdk/openai-compatible",
  capabilities: (modelID) => {
    const kind = classifyOpenAIModel(modelID)
    return {
      sizes: openAISizes(kind),
      aspectRatios: [],
      resolutions: [],
      qualities: openAIQualities(kind),
      // `output_format` 只有 gpt-image 家族认得。
      formats: isGptImageKind(kind) ? ["png", "jpeg", "webp"] : [],
      maxCount: 10,
      maxReferences: openAIMaxReferences(kind),
    }
  },
  build: async ({ modelID, prompt, args, images, loadImage }) => {
    const kind = classifyOpenAIModel(modelID)
    const gptImage = isGptImageKind(kind)
    const fields: Record<string, unknown> = { model: modelID, prompt }
    if (args.n) fields.n = args.n
    // dall-e-3 / dall-e-2 不认 `auto`，只有 gpt-image 家族接受。
    const sizes = openAISizes(kind)
    const size = args.size && sizes.includes(args.size) ? args.size : undefined
    if (size) fields.size = size
    const qualities = openAIQualities(kind)
    if (args.quality && qualities.includes(args.quality)) fields.quality = args.quality
    if (gptImage && args.background) fields.background = args.background
    if (gptImage && args.format) fields.output_format = args.format
    // `response_format` 只对 dall-e-* 有意义；gpt-image 永远返回 b64，
    // 显式传它会被严格实现判成非法参数。
    if (!gptImage) fields.response_format = "b64_json"
    // `moderation: low` 是 gpt-image 家族专属。
    if (gptImage) fields.moderation = "low"
    if (gptImage && args.fidelity && images.length) fields.input_fidelity = args.fidelity

    if (images.length === 0) {
      return { transport: "json", path: "/images/generations", body: fields, mime: "image/png" }
    }

    const form = new FormData()
    for (const [key, value] of Object.entries(fields)) {
      form.append(key, typeof value === "string" ? value : String(value))
    }
    // gpt-image 家族接受多张参考图（重复的 `image[]`），
    // 其余（dall-e-2 / flux-kontext）只接受单张 `image`。
    const multiple = gptImage && images.length > 1
    const selected = multiple ? images.slice(0, openAIMaxReferences(kind)) : images.slice(0, 1)
    for (const [index, part] of selected.entries()) {
      const blob = await loadImage(part)
      form.append(
        multiple ? "image[]" : "image",
        new File([blob], part.filename ?? `reference-${index}.png`, {
          type: part.mime || blob.type || "image/png",
        }),
      )
    }
    return { transport: "multipart", path: "/images/edits", form, mime: "image/png" }
  },
}

// ---------------------------------------------------------------------------
// xAI Grok Imagine
// ---------------------------------------------------------------------------

export const XAI_ASPECT_RATIOS = [
  "auto",
  "1:1",
  "16:9",
  "9:16",
  "4:3",
  "3:4",
  "3:2",
  "2:3",
  "2:1",
  "1:2",
  "19.5:9",
  "9:19.5",
  "20:9",
  "9:20",
]
export const XAI_RESOLUTIONS = ["1k", "2k"]
/** `quality` 只有 grok-imagine-image-2.0 认得，且只有 low/medium。 */
export const XAI_QUALITIES = ["low", "medium"]
export const XAI_MAX_REFERENCES = 3
export const XAI_MAX_COUNT = 4

export const supportsXAIQuality = (modelID: string) =>
  modelID.toLowerCase().includes("grok-imagine-image-2.0")

/**
 * xAI 的参考图是 `{ type: "image_url", url }`，url 可以是 data URL 或 http 地址。
 * 直接给 http 地址就原样传，裸 base64 才需要补成 data URL。
 */
export function toImageUrl(part: ImageRequestPart): string {
  if (part.url.startsWith("data:")) return part.url
  if (/^https?:\/\//i.test(part.url)) return part.url
  return `data:${part.mime || "image/png"};base64,${part.url}`
}

const xai: Dialect = {
  id: "xai",
  matches: ({ providerID, modelID }) =>
    providerID === "xai" || modelID.toLowerCase().includes("grok-imagine"),
  capabilities: (modelID) => ({
    sizes: [],
    aspectRatios: XAI_ASPECT_RATIOS,
    resolutions: XAI_RESOLUTIONS,
    qualities: supportsXAIQuality(modelID) ? XAI_QUALITIES : [],
    formats: [],
    maxCount: XAI_MAX_COUNT,
    maxReferences: XAI_MAX_REFERENCES,
  }),
  build: async ({ modelID, prompt, args, images }) => {
    const body: Record<string, unknown> = {
      model: modelID,
      prompt,
      n: args.n ?? 1,
      response_format: "b64_json",
      aspect_ratio:
        args.aspectRatio && XAI_ASPECT_RATIOS.includes(args.aspectRatio) ? args.aspectRatio : "1:1",
      resolution:
        args.resolution && XAI_RESOLUTIONS.includes(args.resolution) ? args.resolution : "1k",
    }
    if (supportsXAIQuality(modelID)) {
      body.quality = args.quality === "low" ? "low" : "medium"
    }

    // edits 走 JSON：单图 `image`、多图 `images`。
    const selected = images.slice(0, XAI_MAX_REFERENCES)
    const refs = selected.map((part) => ({
      type: "image_url",
      url: toImageUrl(part),
    }))
    if (refs.length === 1) body.image = refs[0]
    else if (refs.length > 1) body.images = refs

    return {
      transport: "json",
      path: refs.length > 0 ? "/images/edits" : "/images/generations",
      body,
      // Grok Imagine 默认输出 jpeg。
      mime: "image/jpeg",
    }
  },
}

// ---------------------------------------------------------------------------

export const dialects: Dialect[] = [xai, openai]

/** 命中方言即表示该模型支持生图；不再用 npm 白名单。 */
export function resolveDialect(input: {
  providerID: string
  npm: string
  modelID: string
}): Dialect | undefined {
  return dialects.find((dialect) => dialect.matches(input))
}

export * as ImageDialect from "./dialect"
