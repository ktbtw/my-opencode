import { isRecord } from "./record"

const DELTA_KEYS = ["text", "delta", "content", "value"] as const

export function normalizeStreamDeltaText(input: unknown): string {
  if (typeof input === "string") return input
  if (input === undefined || input === null) return ""
  if (typeof input === "number" || typeof input === "boolean" || typeof input === "bigint") return String(input)
  if (Array.isArray(input)) return input.map(normalizeStreamDeltaText).join("")
  if (isRecord(input)) {
    for (const key of DELTA_KEYS) {
      const normalized = normalizeStreamDeltaText(input[key])
      if (normalized) return normalized
    }
    try {
      return JSON.stringify(input)
    } catch {
      return String(input)
    }
  }
  return String(input)
}
