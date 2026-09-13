# Release 1.6.56 / OpenCode 1.15.87

## Goal

Publish the backend reliability, single native notification SSE, and Verify project adapter changes.

## Versions

- Flutter app: `1.6.56+203`
- OpenCode CLI: `1.15.87`
- Launcher: unchanged (`0.1.175`)

## Changes

- Android background notifications use one native SSE connection with EventChannel delivery to Flutter.
- Notification reconnect, cursor replay, token refresh, persistent delivery queue, and network recovery are coordinated.
- Backend adds stable signed authentication, readiness checks, panic/request tracking middleware, graceful shutdown, and incremental task event replay.
- Nginx deployment applies dedicated unbuffered SSE proxy settings and readiness routing.
- Android reverse semantic agent gets Verify MCP app/config discovery and workspace-local `.verify/project.json` handling.

## Verification

- Flutter notification tests: 25 passed.
- Backend `go test ./...`: passed.
- Android `:app:compileDebugKotlin`: passed.
- OpenCode Verify adapter tests: 5 passed.
- Backend `/readyz`: healthy; systemd service active; Nginx `nginx -t`: successful.
- Flutter downloads for Android, Windows, and macOS: HTTP `206` and SHA-256 verified.
- Flutter Web and Codex Web bundles deployed; Nginx syntax and public `200` response verified.
- OpenCode downloads for Windows, macOS ARM64, and Linux x64: HTTP `206` and SHA-256 verified.

### Published SHA-256

- Android APK: `20b4c66e0dd5ef5ef3d56c49ecfa2d68ff1ac63340c9067ef9b888d82c4752b7`
- Windows installer: `e7816384a0101b1a5f9c03716ee98ea7ab376c5d34c83d589a84692fb53ef6bc`
- macOS DMG: `a66a1c64a703dcbd965433c6ca2f212097adf1b3a7e1fb62a9401e85d3c232ac`
- OpenCode Windows: `3f688026b3b351743fda4b1c80c462867e187af41d91bfa3cdee1d8d3b38511e`
- OpenCode macOS ARM64: `6bde1a4ace8dc6da8d94ba56efe1a1c060b14fa26c32752780424a5ce9ed616a`
- OpenCode Linux x64: `741d977cf7bb3deaba0e5e38ae1b2aa5679e9148b0ea948891df72c1c2454568`
