# Device Card Scheme 2

## Goal
Implement the task-first device card design with account-scoped cloud names and stable device/Agent ordering.

## Tasks
- [x] Persist device display names and first-seen order per operator.
- [x] Apply preferences and deterministic Agent ordering in every device API path.
- [x] Add rename API validation and account access checks.
- [x] Implement the task-first Flutter card, rename actions, and detail hostname display.
- [x] Add long-press drag sorting for device and Agent cards.
- [x] Cover storage, API, model, widget, and responsive behavior with tests.

## Done When
- [x] Device cards do not move after refresh, status changes, or task changes.
- [x] Agent cards keep a deterministic order across online/offline merge paths.
- [x] Names sync per account and machine IDs remain hidden except for explicit copy.
- [x] Backend tests, Flutter tests, analysis, and responsive visual checks pass.

## Decisions
- Device order is assigned on first API visibility and then persisted per operator.
- Newly discovered devices are assigned in oldest-seen-first order.
- Agent order is deterministic by stable identity fields; transient status is excluded.
- Manual device order is account scoped; manual Agent order is account-and-device scoped.
- Completing sort mode restores normal card styling immediately while sync continues.
- Empty display names clear the override and fall back to hostname.
