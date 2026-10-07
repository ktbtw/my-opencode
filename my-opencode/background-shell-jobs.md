# OpenCode Background Shell Jobs

## Goal

Prevent long-running shell commands from occupying an Agent tool call while keeping process control, logs, completion results, and cancellation inside OpenCode.

## Accepted Design

- Shell execution defaults to `auto`: wait briefly, then return a `job_id` while the command continues.
- `background` returns immediately; `foreground` preserves synchronous execution for interactive or explicitly blocking work.
- OpenCode's existing `BackgroundJob` service owns command fibers and live output. Launcher is not involved.
- A compact `job` tool lists, inspects, waits for, and stops jobs without high-frequency model polling.
- Background completion is persisted into the current conversation as a synthetic context message and resumes an idle loop.
- Shell tool guidance tells models to background persistent services and wait only when a later step needs the exit result.

## Assumptions

- The default auto-yield interval is 8 seconds and can be overridden per command.
- An explicit shell timeout remains an execution deadline. Auto/background commands without a timeout have no implicit two-minute deadline.
- Jobs live for the OpenCode instance lifetime; surviving an OpenCode process restart is outside this change.
- Cancelling or removing the owning session stops its running shell jobs and their process trees.

## Tasks

- [x] Add live progress updates to `BackgroundJob` and cover snapshot behavior.
- [x] Add the unified `job` tool and register it as a built-in.
- [x] Add shell execution modes, auto-yielding, background completion injection, and process cancellation.
- [x] Update shell guidance and parameter tests.
- [x] Run focused background-job tests and package typecheck.
- [x] Cross-build Windows x64 and run a one-hour-command Agent E2E check on Windows 10.

## Decision Log

- OpenCode owns jobs because it already owns shell execution and has instance-scoped background infrastructure.
- Auto-yield is implemented in code rather than inferred only by the model.
- One action-based `job` tool is used instead of several nearly identical tools to reduce tool-schema overhead.
- Launcher-level process survival and boot-time service supervision remain separate concerns.

## Verification

- `bun typecheck`: passed.
- Background job, shell, and job tool suites: 38 passed, 0 failed.
- Interrupted shell output persistence regression: passed when run serially.
- Windows x64 E2E command: `Start-Sleep -Seconds 3600` returned a running `job_id` after 8.16 seconds.
- The Windows Agent CLI exited with code 0 after 11.18 seconds and left no matching child process, including a delayed recheck.

## Done When

- A long command returns control after the yield interval and remains observable and stoppable.
- A quick auto command returns its normal inline result.
- Background completion enters the same conversation without dispatching a separate Agent task.
- Windows-compatible process spawning remains hidden and cancellation terminates the managed process.
