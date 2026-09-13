# Runtime User Action Notification Design

## Goal

When a Launcher workflow needs a person to act on the target computer, persist that state, show a Flutter reminder, and send one account email when email notifications are enabled.

## Understanding

- Launcher reports a first-class waiting-for-user-action state before opening UAC or another system prompt.
- The backend persists the active action so polling and page reloads do not lose it.
- Flutter shows one additional dialog for each new action ID while continuing to poll installation progress.
- Closing the Flutter reminder does not confirm, cancel, or resume the computer-side operation.
- Email uses the account-bound address and respects the existing account email notification switch.
- Notification failures never change the installation result.
- The first integration is Windows IDA UAC elevation; the contract remains generic for future workflows.

## Assumptions

- The current 900 ms Flutter polling interval remains adequate.
- Launcher can determine completion from the child installer result without a separate confirmation API.
- Email content excludes credentials, tokens, and complete local paths.
- A task has at most one active user action at a time.

## Design

Add a shared `RuntimeUserAction` contract with an ID, kind, title, message, instructions, and request timestamp. Runtime preflight jobs and Launcher status payloads carry `requires_user_action` and the current action.

For Windows IDA, Launcher emits `waiting_user_action` immediately before invoking the elevated installer. The action ID is stable for the preflight job. Once the elevation process returns, Launcher clears the action and resumes `running`, or continues through the existing failure path.

The backend persists the action on the preflight job. A separate notification record keyed by operator, job, action, and channel makes email delivery idempotent. On the first transition to an action, the backend asynchronously calls the notification service. The service reuses the existing account email lookup and `email_notification_enabled` setting.

Flutter compares the polled action ID with IDs already presented during the dialog lifecycle. A new action opens an alert above the preflight progress dialog and also renders a waiting status in the progress view. Dismissing the alert only acknowledges the local presentation.

## Failure Handling

- Persistence errors reject the status update so Launcher can retry it.
- Email failures are logged and do not fail the preflight job.
- Duplicate Launcher status messages do not create duplicate emails.
- Missing or malformed action data is ignored unless `requires_user_action` is true, in which case the backend retains a clear generic message.
- Flutter remains usable when polling or dialog presentation fails and retries through its existing polling loop.

## Testing

- Launcher unit tests verify waiting status is emitted before elevation and cleared afterward.
- Backend tests verify model persistence, reload behavior, notification deduplication, and the account email switch.
- Flutter tests verify JSON parsing, waiting labels, and one dialog per action ID.
- Existing runtime preflight, mail notification, Go, and Flutter analysis/tests must remain green.

## Decision Log

- Chose task-level user action state over event-only metadata because it survives refresh and avoids event scanning.
- Chose an idempotent notification record instead of in-memory deduplication because backend restarts must not duplicate mail.
- Chose polling integration instead of a new socket because the preflight UI already polls frequently.
- Chose local dialog acknowledgement instead of a resume API because UAC completion is detected by Launcher.
- Chose the existing account email preference rather than mandatory delivery.

