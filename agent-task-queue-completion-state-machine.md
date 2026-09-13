# Agent Task Queue and Completion State Machine

## Understanding Summary

- Flutter chat must keep the composer usable while an Agent task is streaming.
- Sending during an active task creates a session-scoped queue item owned by the backend.
- Send and stop are separate icon actions; stop is visible only while a task is active.
- A queued item can be reordered, deleted, assigned a model, or inserted into the active task.
- Inserted content becomes durable session context and is removed from the queue only after Agent acknowledgement.
- The queue and the account-level "continue after stop" preference synchronize across devices.
- OpenCode must distinguish normal text completion from execution failure and must support long-running MCP work without false completion.

## Assumptions

- The backend database remains the source of truth for sessions, tasks, queue items, and account settings.
- One session has at most one actively executing task; queued items dispatch serially.
- Changing the composer model affects only messages created afterward. Each queued item keeps its own model snapshot and may be edited.
- An inserted item uses the active task model because it extends that running model turn.
- Normal greetings, explanations, and other text-only requests are valid completions and must not require a tool call.
- Build recovery permits up to 12 productive continuation rounds and fails after 2 consecutive rounds with no progress delta.
- Long MCP work reports job progress or heartbeats; waiting on it does not consume Build continuation rounds.

## Architecture

### Completion Protocol

OpenCode treats provider finish reasons as protocol states:

| Finish reason | Action |
| --- | --- |
| `stop` | Evaluate the latest round as a completion candidate. |
| `tool-calls` | Continue after tool results. |
| `error` | Retry according to policy or report failure; never complete. |
| `length` | Compact or continue; never silently complete. |
| `content-filter` | Report a terminal task failure. |
| unknown/empty | Continue only when tool state requires it; otherwise validate as a completion candidate. |

The completion guard evaluates the latest round before cumulative history. Pending tools, approvals, questions, unresolved todos, provider errors, and forward-looking text take precedence over historical successful tools. Progress is a delta of tool terminal states, patches, artifacts, resolved todo items, and substantive final text. Two identical no-progress rounds terminate with a precise error instead of looping indefinitely.

Text-only Build requests complete when the response is a direct answer and contains no promised future action. This prevents ordinary conversation such as "你好" from being marked as a failed Build operation.

### Long MCP Jobs

MCP connection and tool discovery use bounded startup timeouts. Tool execution uses a separate configurable timeout. Tools expected to run for minutes expose an asynchronous job contract: start returns a job identifier, status polling returns progress/heartbeat/output, and cancellation targets that job. Heartbeats keep the relay task active without counting as model continuation rounds.

### Session Queue

Queue items are stored by session with stable ordering, text/attachments, selected model/variant, timestamps, state, and an optimistic concurrency version. Backend APIs and WebSocket events create, update, reorder, delete, insert, acknowledge, and dispatch items. All connected devices converge from backend events.

When no task is active, the first queued item is atomically claimed and dispatched. On normal completion, the next item dispatches automatically. After user stop, dispatch follows the synchronized account preference. A failed task leaves later items queued.

Insertion sends `task.input` to the active OpenCode session with an injection version. The backend keeps the item in `inserting` state until OpenCode acknowledges that version. Completion is delayed while an unacknowledged injection exists, closing the race between finalization and inserted input.

## Error Handling

- Provider `finish=error` without a structured error is normalized into a task failure/retry signal.
- Empty assistant output is reported with model, finish reason, and prompt-attempt diagnostics.
- Queue mutation uses item/version checks so two devices cannot silently overwrite each other.
- Disconnects do not lose queue state; reconnecting clients fetch the current ordered snapshot.
- Insert acknowledgement timeout returns the item to `queued` with an error description.
- Stop cancels only the active task and never deletes pending queue items.

## Testing Strategy

- Unit-test the finish-state matrix, latest-round guard, forward-looking text, text-only answers, todo handling, progress and stagnation limits.
- Test MCP startup timeout separately from long tool execution and asynchronous job polling.
- Test queue ordering, model snapshots, insertion acknowledgement, stop preference, reconnect, and concurrent device edits in Go.
- Widget-test independent send/stop controls, queue editor, per-item model selection, and insertion state in Flutter.
- Run a Windows Agent regression reproducing the prior incomplete IDA task and verify it continues or reports a real error instead of emitting `task.completed`.

## Decision Log

1. Backend persistence was selected over a Flutter-only queue so sessions synchronize across devices and survive restarts.
2. WebSocket `task.input` with an acknowledgement barrier was selected for insertion so active context is updated immediately without racing task completion.
3. Per-item model snapshots were selected so changing the composer model affects only future messages.
4. Latest-round completion state was selected over cumulative tool history because old successful calls do not prove the current round finished.
5. A finish-state machine was selected over a single text heuristic because provider errors and token limits require different recovery behavior.
6. Asynchronous MCP jobs were selected for long work so a 30-minute operation does not hold one fragile JSON-RPC request.

