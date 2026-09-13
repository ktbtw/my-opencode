import path from "path"

export const VERIFY_PROJECT_CONFIG = ".verify/project.json"

const ANDROID_REVERSE_AGENTS = new Set(["reverse-android", "android-reverse", "安卓逆向"])

/**
 * Verify is configured as an MCP server by the user, while formal/test roles
 * are project-local concepts. Keep the adapter keyed to the stable server name
 * and agent slug instead of display labels.
 */
export function isAndroidReverseAgent(agentName: string) {
  return ANDROID_REVERSE_AGENTS.has(agentName.trim().toLowerCase())
}

export function isVerifyMcpConnected(status: Record<string, { status: string }>) {
  return Object.entries(status).some(
    ([name, value]) => /(^|[-_:])verify($|[-_:])/i.test(name) && value.status === "connected",
  )
}

export function hasVerifyIntent(text: string) {
  return /(?:\bverify\b\s*(?:mcp|framework|cloud\s*code|xposed|xp)|verify\s*(?:框架|云代码|mcp)|云代码|xp(?:osed)?\s*(?:应用|配置)|config\s*id|configId|publish[_ -]?cloud[_ -]?code|心跳回参|卡密校验)/i.test(
    text,
  )
}

export function hasFridaIntent(text: string) {
  return /\b(?:frida|objection)\b|Frida|Objection|附加.*宿主|注入.*进程/i.test(text)
}

export function hasApkTargetIntent(text: string) {
  return /(?:\.apk\b|apkPath|packageName|包名|远程\s*Patch|patch_local_apk|inspect_patched_apk|打(?:过)?\s*Patch|是否\s*Patch|有没有\s*(?:patch|Patch|打补丁)|检测.{0,16}(?:apk|APK|应用|安装包|包)|分析.{0,16}(?:apk|APK)|已安装应用)/i.test(
    text,
  )
}

export function verifyProjectConfigPath(directory: string) {
  return path.join(directory, VERIFY_PROJECT_CONFIG)
}

export function verifyPrompt(input: { configExists: boolean; fridaRequested?: boolean }) {
  const mode = input.configExists
    ? `The workspace already has ${VERIFY_PROJECT_CONFIG}. Read it before asking the user for Verify app information. Treat it as the local source of truth and update it incrementally when the user adds a channel or switches the active test config.`
    : `The workspace does not have ${VERIFY_PROJECT_CONFIG} yet. Before doing Verify work, call list_apps first, filter to appType XPOSED, show the fetched app names and numeric appIds, and ask the user to choose which is the formal XP app and which is the test XP app. If the user already supplied both names, resolve them against list_apps instead of asking again.`

  const frida = input.fridaRequested
    ? [
        "The user requested Frida/Objection analysis. Finish inspect_patched_apk first. Only attach after the target is a current-account Verify local Patch and the patched package is installed, or after patch_local_apk has succeeded and the output APK has been re-inspected.",
        "Identify the active test configId from .verify/project.json (or ask the user to choose one).",
        'Call mcp_search and mcp_call for get_config, inspect config.fridaDetectionEnabled, and if it is true call update_config with data containing {"fridaDetectionEnabled":false}; then call get_config again and require fridaDetectionEnabled=false before attaching Frida.',
        "Only change the selected analysis config. Do not change formal channel configs unless the user explicitly selects one. Keep packet-capture and VM detection settings unchanged.",
        "If update_config rejects the field, the read-back value remains true, or the host still detects Frida at runtime, stop retrying the same attach. Tell the user that this Verify host/module version still enforces Frida detection and recommend re-patching with patch_local_apk or downloading and re-attaching the latest module, then retry the runtime check.",
        "After the setting is confirmed false and the host/module is compatible, continue with the requested Frida analysis and record the selected configId and packageName in the result.",
      ]
    : []

  return [
    "<verify-project-adapter>",
    "You are running as the Android reverse-engineering semantic agent with the Verify MCP connected.",
    mode,
    "Use mcp_search, then mcp_call, to invoke the Verify MCP operations list_apps and list_configs. Do not invent appId or configId values.",
    "list_apps returns numeric appId, name, and appType. Only offer appType XPOSED apps. list_configs returns numeric config id, appId, packageName, and configuration flags; associate configs by config.appId.",
    "Store formal/test roles locally in .verify/project.json. The formal app keeps all of its channel configs; the test app keeps all discovered configs and one active testConfigId for the current workspace.",
    "Create or update that file with the existing write tool using an absolute path. Merge by configId, preserve existing entries, and never delete remote entries automatically.",
    "For Verify cloud-code publishing, pass the current workspace path and configId to publish_cloud_code_from_workspace. Formal defaults are enableR8Obfuscation=true, enableBlackObfuscation=true, useLocalCache=true. Test defaults are all false. Explicit user/project settings override defaults.",
    "Only ask a follow-up when app names are ambiguous or more than one test config matches the workspace. When the user requests a new channel/config, refresh list_configs and append the new config instead of restarting setup.",
    "When this is a Verify workspace, Verify configuration handling takes precedence over any Frida-first analysis skill. Do not attach, spawn, or run Frida/Objection against a Verify host until inspect_patched_apk and the Frida-specific configuration flow have completed.",
    "Do not expose or write API tokens or Protect tokens to the project file; Verify MCP environment credentials remain in their existing credential source.",
    "When the user gives a concrete APK path or package for detection, analysis, hook, attach, or runtime work, you decide patch state. Do not ask whether they already patched or installed it.",
    "Call inspect_patched_apk yourself. Prefer apkPath when a computer file exists. If only an installed app is known, pass packageName first; if ownedByCurrentAccount is null, adb pull the APK and re-inspect with apkPath. Pass serial when multiple devices are present. apkPath and packageName: at least one is required. MCP must be @ktbtw/verify-mcp@0.2.32+ with VERIFY_API_TOKEN.",
    "Read inspect_patched_apk as: patched=false means not a Verify local Patch; patched=true and ownedByCurrentAccount=true means trust appId/appName; patched=true and ownedByCurrentAccount=false means another account, stop mixing tokens; patched=true and ownedByCurrentAccount=null means re-inspect with apkPath and do not guess the account. xyz.patch.owner is not appId. source=dumpsys/aapt/manifest is new-package meta-data; source=runtime-config is old-package config.vcfg.",
    "Also decide whether the matching patched APK is installed via packageName inspect or dumpsys. Do not ask the user to check the launcher icon.",
    "If later work needs the Verify runtime on device and the target is unpatched or the patched package is not installed, load the verify-framework skill references/patch-local-apk.md and guide patch_local_apk: list_apps for appType=XPOSED (prefer the test XP app from .verify/project.json), dryRun first, then real patch. Xposed only, not DEX inject. Do not edit Template/verfiy_hook_embedded for this user flow.",
    "Load verify-framework references/inspect-patched-apk.md or references/patch-local-apk.md only when calling those tools. Keep payload details out of ordinary replies.",
    ...frida,
    "</verify-project-adapter>",
  ].join("\n")
}
