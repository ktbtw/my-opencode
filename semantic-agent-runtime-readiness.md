# Semantic Agent Runtime Readiness

## Goal
Make every semantic Agent switch apply its own tools reliably and fail with actionable MCP diagnostics when a declared capability is not runnable.

## Tasks
- [x] Embed the canonical Agent, Skill, MCP, and runtime catalog in the backend and Launcher.
- [x] Audit all stored/default Agent profiles against their MCP, runtime, and external application requirements.
- [x] Make Launcher apply active Agent changes through a restart and verify selected MCP status/tools before completing preflight.
- [x] Add Windows backend discovery/environment repair for installed tools such as IDA and Ghidra.
- [x] Preserve concrete MCP startup and tools/list errors in opencode and expose them through `/mcp`.
- [x] Tighten per-Agent MCP isolation without deleting user global MCP definitions.
- [x] Add regression tests for restart, readiness validation, dependency discovery, isolation, and error propagation.
- [x] Add managed IDA Professional 9.2 installation for Windows/macOS, including license generation, runtime patching, IDALib activation, batch-license initialization, signing, and headless analysis verification.
- [x] Verify the managed macOS IDA install with both `idat -A` and `idalib-mcp` `idb_open`; auto-analysis and Hex-Rays initialization are ready.
- [x] Build, deploy, and verify the affected Launcher/opencode versions on the Windows LAN device.

## Done When
- [x] Switching each tested Agent results in only its intended MCP set being active, every required MCP returns tools, and failures name the missing dependency or command output.

## Remaining Device Coverage

- Windows: Launcher `0.1.118` is deployed and verified on LAN device `m_7e078b8368081aedb5a9680047934fac` with Agent `agent_098f8ac6`. Preflight job `preflight_1785217443258697047` completed with IDA, Ghidra, Python, Java, and uv ready; `binary-mcp` and `idalib-mcp` both connected and returned full tool lists. The real self-update path from `0.1.117` to `0.1.118` replaced the running EXE, restarted the background Launcher, and produced an installed SHA-256 matching the published archive entry.
- Android: dynamic tools require a connected ADB/root test device.
- iOS: dynamic tools require a macOS host and a reachable Frida target.
- Web: browser MCP validation requires a local browser/CDP runtime.
