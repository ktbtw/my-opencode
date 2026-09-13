# Provider Finish Error Retry Fix

## Goal
Recover a running Agent task when a provider closes a completed stream with `finish_reason=error`, and prevent the Flutter queue tray from showing a transient error before its first snapshot arrives.

## Tasks
- [x] Trace the session-loop exit path and queue synchronization race.
- [x] Continue provider finish errors in a bounded new model round while preserving prior tool results.
- [x] Relay retry progress to the backend and client.
- [x] Keep initial queue synchronization in loading state until REST synchronization succeeds or fails.
- [x] Add regression tests for recovery, retry exhaustion, preserved tool context, and initial queue state.
- [x] Run focused OpenCode and Flutter tests plus type analysis.

## Done When
- [x] A first `finish_reason=error` followed by a successful response completes the task.
- [x] Five consecutive finish errors fail once without completing the task.
- [x] Completed tool results are present in the continuation request.
- [x] Initial queue stream errors do not flash an error before the first REST snapshot.
