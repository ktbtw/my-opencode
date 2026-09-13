# Agent Task Queue Implementation

## Goal

Deliver reliable Agent completion, long-running MCP execution, and a backend-persisted Flutter chat queue with active-task insertion and cross-device settings.

## Tasks

- [ ] Add OpenCode finish-state normalization and latest-round completion tracking; verify `error`, `length`, greetings, unfinished text, todos, and stagnation in unit tests.
- [ ] Update Relay finalization to reject provider errors and wait for acknowledged task injections; verify the former Windows failure chain no longer emits `task.completed`.
- [ ] Separate MCP startup/discovery and execution timeouts, then add asynchronous job progress support; verify a simulated long tool survives past 30 seconds.
- [ ] Add backend queue/settings models, storage, API and WebSocket events; verify ordering, model overrides, reconnect, stop behavior, and concurrent mutations in Go tests.
- [ ] Add Flutter repository/state support for synchronized queue snapshots and mutations; verify JSON/event handling with data tests.
- [ ] Add independent send/stop buttons plus queue reorder/delete/insert/model UI; verify active, idle, inserting, failed, and narrow-window states with widget tests.
- [ ] Run TypeScript, Go, and Flutter suites, then perform a Windows end-to-end regression.
- [ ] Update versions, release notes, build platform artifacts, upload, and deploy after all regressions pass.

## Done When

- [ ] A provider `finish=error` never becomes `task.completed`.
- [ ] Normal text conversation completes without a false Build warning.
- [ ] Long MCP tasks expose progress and are not killed by the discovery timeout.
- [ ] Queue state and the stop preference synchronize across devices.
- [ ] Flutter can send while streaming, stop independently, edit queued models, and insert an item into the active task.
- [ ] Windows regression, builds, upload, and deployment complete successfully.

## Notes

Existing unrelated worktree changes remain untouched. Versioning and deployment happen only after the regression suite passes.
