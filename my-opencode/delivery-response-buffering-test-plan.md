# Delivery Response Buffering Test

## Goal
Keep completion-guard correction rounds out of the user chat while committing one complete, coherent delivery response after artifact validation.

## Tasks
- [ ] Add per-round text/reasoning buffering for delivery-required Relay tasks -> Verify non-delivery tasks still stream immediately.
- [ ] Define round commit, discard, and output-length retention rules -> Verify accepted content preserves exact chunk order and rejected content never reaches `task.delta`.
- [ ] Make missing-artifact correction request a complete self-contained final response -> Verify the second round does not depend on hidden wording.
- [ ] Cover success, correction, fragmented thinking, failure, cancellation, and duplicate terminal events in Relay tests -> Verify one terminal event and one downloadable artifact.
- [ ] Run focused tests, the Relay suite, and typecheck -> Verify no regressions in task/question/tool behavior.
- [ ] Start the source Runtime on an isolated port and create disposable local sessions -> Verify real session output and cleanup without touching existing Agent sessions.

## Done When
- [ ] A corrected delivery exposes one coherent answer, no hidden correction text, and one artifact entry.
- [ ] Failures and cancellation leak no buffered body and leave no late completion event.
- [ ] Automated tests and isolated local-session checks are stable across repeated runs.

## Notes
All delivery-required tasks are buffered, including goal mode. `output_length` keeps partial text for continuation; corrective rounds such as `missing_artifact` discard the rejected draft and require the next round to restate the final answer completely.
