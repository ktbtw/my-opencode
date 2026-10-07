import type { SubagentRole } from "./types"

const roles = {
  "coding-assistant": ["repo-explorer", "implementation-planner", "test-runner", "code-reviewer", "dependency-researcher"],
  "reverse-android": ["apk-scout", "manifest-mapper", "native-tracer", "protocol-analyst", "verification-runner"],
  "reverse-ios": ["ipa-scout", "macho-mapper", "objc-swift-tracer", "runtime-observer", "verification-runner"],
  "reverse-windows": ["pe-scout", "import-export-mapper", "control-flow-analyst", "installer-analyzer", "verification-runner"],
  "reverse-mac": ["bundle-scout", "codesign-entitlements-auditor", "macho-mapper", "objc-swift-tracer", "runtime-observer"],
  "reverse-web": ["asset-scout", "js-wasm-analyst", "request-flow-mapper", "browser-observer", "verification-runner"],
} as const satisfies Record<string, readonly SubagentRole[]>

const generic = roles["coding-assistant"]

export function allowedSubagentRoles(semanticAgentID: string, override?: readonly SubagentRole[]) {
  if (override?.length) return [...new Set(override)]
  return [...(roles[semanticAgentID as keyof typeof roles] ?? generic)]
}

export function isSubagentRoleAllowed(semanticAgentID: string, role: SubagentRole, override?: readonly SubagentRole[]) {
  return allowedSubagentRoles(semanticAgentID, override).includes(role)
}

export function readonlySubagentTools(parent: Record<string, unknown>) {
  const blocked = new Set(["edit", "apply_patch", "write", "install", "publish", "deploy"])
  return Object.fromEntries(
    Object.entries(parent).map(([tool, value]) => [tool, blocked.has(tool) ? "deny" : value]),
  )
}
