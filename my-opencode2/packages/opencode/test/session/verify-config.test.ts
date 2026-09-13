import { describe, expect, test } from "bun:test"
import {
  hasApkTargetIntent,
  hasFridaIntent,
  hasVerifyIntent,
  isAndroidReverseAgent,
  isVerifyMcpConnected,
  verifyProjectConfigPath,
  verifyPrompt,
} from "../../src/session/verify-config"

describe("Verify project adapter", () => {
  test("recognizes the Android reverse semantic agent", () => {
    expect(isAndroidReverseAgent("reverse-android")).toBe(true)
    expect(isAndroidReverseAgent("build")).toBe(false)
  })

  test("requires a connected Verify MCP server", () => {
    expect(isVerifyMcpConnected({ verify: { status: "connected" } })).toBe(true)
    expect(isVerifyMcpConnected({ verify: { status: "failed" } })).toBe(false)
    expect(isVerifyMcpConnected({ other: { status: "connected" } })).toBe(false)
  })

  test("detects Verify-specific intent without matching generic verification", () => {
    expect(hasVerifyIntent("我要使用 Verify 云代码编译")).toBe(true)
    expect(hasVerifyIntent("verify this function")).toBe(false)
    expect(hasVerifyIntent("给 XP 应用切换测试配置")).toBe(true)
  })

  test("detects explicit Frida analysis intent", () => {
    expect(hasFridaIntent("用 Frida 附加当前宿主进程")).toBe(true)
    expect(hasFridaIntent("分析这个 Java 类")).toBe(false)
  })

  test("detects a concrete APK or package target", () => {
    expect(hasApkTargetIntent("检测这个 APK 有没有 Patch")).toBe(true)
    expect(hasApkTargetIntent("用 patch_local_apk 远程 Patch")).toBe(true)
    expect(hasApkTargetIntent("/tmp/game.apk")).toBe(true)
    expect(hasApkTargetIntent("分析这个 Java 类")).toBe(false)
  })

  test("builds a workspace-local config path and prompt", () => {
    expect(verifyProjectConfigPath("/tmp/project")).toBe("/tmp/project/.verify/project.json")
    expect(verifyPrompt({ configExists: false })).toContain("does not have .verify/project.json")
    expect(verifyPrompt({ configExists: false })).toContain("call list_apps first")
    expect(verifyPrompt({ configExists: true })).toContain("already has .verify/project.json")
    expect(verifyPrompt({ configExists: true })).toContain("inspect_patched_apk")
    expect(verifyPrompt({ configExists: true })).toContain("Do not ask whether they already patched")
    expect(verifyPrompt({ configExists: true })).toContain("dryRun first")
    expect(verifyPrompt({ configExists: true, fridaRequested: true })).toContain("fridaDetectionEnabled")
    expect(verifyPrompt({ configExists: true, fridaRequested: true })).toContain("Finish inspect_patched_apk first")
  })
})
