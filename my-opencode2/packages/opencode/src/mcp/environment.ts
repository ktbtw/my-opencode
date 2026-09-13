function isCredentialEnvironmentKey(key: string) {
  const normalized = key.trim().toUpperCase()
  return (
    normalized.includes("TOKEN") ||
    normalized.includes("SECRET") ||
    normalized.includes("PASSWORD") ||
    normalized.includes("API_KEY") ||
    normalized.endsWith("_KEY")
  )
}

function isCredentialPlaceholder(value: string) {
  const normalized = value.trim().toLowerCase()
  if (
    normalized === "" ||
    normalized.includes("replace_me") ||
    normalized.includes("<redacted>") ||
    normalized.includes("****")
  ) {
    return true
  }
  return /^(vat|vpt)_x+$/.test(normalized)
}

export function mergeMCPEnvironment(parent: Record<string, string | undefined>, configured?: Record<string, string>) {
  const result: Record<string, string> = {}
  for (const [key, value] of Object.entries(parent)) {
    if (value !== undefined) result[key] = value
  }
  for (const [key, value] of Object.entries(configured ?? {})) {
    if (isCredentialEnvironmentKey(key) && isCredentialPlaceholder(value)) continue
    result[key] = value
  }
  return result
}
