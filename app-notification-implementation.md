# Global In-App Notifications

## Goal

Build the confirmed cross-platform in-app notification system and connect it to real Agent, MCP/runtime, update, and file workflows.

## Tasks

- [x] Add notification models, state transitions, redaction, and account-isolated JSON persistence. Verify: focused model/repository tests pass.
- [x] Add the Riverpod controller with hydration, deduplication, throttled writes, auto-collapse, and 50-record retention. Verify: controller tests cover restart, identity changes, and concurrent updates.
- [x] Build the responsive global host, full notices, compact island, and notification center. Verify: widget tests cover expand, collapse, tabs, and actions.
- [x] Mount the host above `GoRouter` and add a global notification-center entry. Verify: notifications survive route changes.
- [x] Connect Agent creation/removal, semantic preflight/MCP, and Launcher update progress. Verify: operation IDs update one notification instead of duplicating it.
- [x] Connect upload, download, create, rename, and delete file workflows. Verify: device transfer and local file-save results use the correct sync scope.
- [x] Add authenticated cross-device synchronization with version cursors, tombstones, read state, account isolation, and reconnect retry. Verify: backend and Flutter sync tests pass.
- [x] Run formatting, focused/full tests, analysis, builds, and desktop/mobile visual checks. Verify: selected commands pass without overflow or console errors.

## Done When

- [x] Android, Windows, macOS, and Web share one responsive notification UI.
- [x] Running notifications recover locally after restart and recent history is capped at 50.
- [x] Determinate, indeterminate, waiting-sync, success, failure, and cancellation states render correctly.
- [x] The first integration workflows expose real progress and terminal status rather than static UI placeholders.
- [x] Synced notifications converge across signed-in devices while local notifications stay on their initiating device.

## Notes

Server synchronization uses stable operation IDs and an operator-scoped monotonic version cursor. Progress uploads are coalesced for 500 ms, clients poll every 5 seconds, and read/dismiss actions propagate as versioned changes. Purely local file-save notifications remain device-local. Existing push and email notification systems remain independent.

Verification completed with all Go packages, 140 Flutter tests, full Flutter analysis (15 pre-existing informational notices), Web/Android/macOS builds, mobile and desktop browser screenshots, and a zero-error browser console check. Windows uses the same analyzed Dart implementation; a native Windows build requires the Windows build host.
