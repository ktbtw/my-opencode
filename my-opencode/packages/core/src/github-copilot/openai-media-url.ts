import { convertToBase64 } from "@ai-sdk/provider-utils"

const DATA_OR_REMOTE_URL = /^(data:|https?:\/\/)/i

/**
 * Chat Codex file parts arrive as data URLs (`data:image/png;base64,...`).
 * AI SDK converters historically assume `data` is raw base64 and prefix
 * another `data:${mediaType};base64,`, which produces an invalid image.
 */
export function openaiImageUrl(data: unknown, mediaType: string): string {
  if (data instanceof URL) return data.toString()
  if (typeof data === "string" && DATA_OR_REMOTE_URL.test(data)) return data
  return `data:${mediaType};base64,${convertToBase64(data as string | Uint8Array)}`
}
