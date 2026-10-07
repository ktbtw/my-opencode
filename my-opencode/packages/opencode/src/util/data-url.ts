export function decodeDataUrl(url: string) {
  const idx = url.indexOf(",")
  if (idx === -1) return ""

  const head = url.slice(0, idx)
  const body = url.slice(idx + 1)
  if (head.includes(";base64")) return Buffer.from(body, "base64").toString("utf8")
  return decodeURIComponent(body)
}

export function decodeDataUrlBytes(url: string) {
  const idx = url.indexOf(",")
  if (idx === -1) return new Uint8Array()

  const head = url.slice(0, idx)
  const body = url.slice(idx + 1)
  if (head.includes(";base64")) return new Uint8Array(Buffer.from(body, "base64"))
  return new Uint8Array(Buffer.from(decodeURIComponent(body), "utf8"))
}

/**
 * Decodes a data URL only when the payload is usable text.
 * Returns undefined for binary payloads so callers can fall back to a notice
 * instead of forwarding bytes the model cannot read.
 */
export function decodeTextDataUrl(url: string) {
  const bytes = decodeDataUrlBytes(url)
  if (bytes.length === 0) return ""
  if (bytes.includes(0)) return undefined
  try {
    return new TextDecoder("utf-8", { fatal: true }).decode(bytes)
  } catch {
    return undefined
  }
}
