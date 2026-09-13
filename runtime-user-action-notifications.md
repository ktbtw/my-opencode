# Runtime User Action Notifications

## Goal

Add durable computer-side action reminders across Launcher, backend email notification, and Flutter preflight UI.

## Tasks

- [x] Extend backend and Launcher preflight models with `RuntimeUserAction` and waiting-state fields. Verify: Go model serialization tests pass.
- [x] Persist the active action and idempotent notification delivery state in MySQL and memory stores. Verify: store round-trip and duplicate-key tests pass.
- [x] Send account email on the first waiting transition while honoring `email_notification_enabled`. Verify: notification service and API status tests pass.
- [x] Emit and clear the Windows IDA UAC action around elevated installer execution. Verify: Windows Launcher tests assert report ordering and error behavior.
- [x] Parse and present the action in Flutter with one alert per action ID and a visible waiting state. Verify: Flutter model/widget tests pass.
- [x] Run focused Go tests, Flutter analysis/tests, and regression builds. Verify: all selected commands exit successfully.
- [x] Upgrade versions, build release artifacts, upload, and deploy backend/Launcher/Flutter assets. Verify: production health/version endpoints and logs are clean.

## Done When

- [x] Windows IDA UAC produces a visible Flutter reminder before elevation.
- [x] Exactly one eligible email is sent for each task/action ID.
- [x] Refresh and backend restart preserve the active action without duplicate notification.
- [x] Installation completion or failure clears the waiting state.
