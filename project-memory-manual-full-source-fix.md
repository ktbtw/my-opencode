# Project Memory Manual Full-Source Fix

## Goal
Make a visible manual organization job consume all terminal task history for its isolated project scope instead of dispatching an empty source packet.

## Tasks
- [x] Add regression tests for chronological multi-task packets, scope filters, and bounded packet size.
- [x] Resolve blank manual cursors to the project's terminal task range and persist the actual batch cursor.
- [x] Continue oversized visible jobs under the same job ID until the target cursor is committed.
- [x] Verify backend unit, race, and Relay integration tests.
- [x] Publish only the backend and verify service health plus deployed artifact checksum.
- [ ] Confirm a production manual job receives non-empty source data.

## Done When
- [ ] A blank-cursor manual job processes historical tasks in order, remains visible through every batch, and finishes with the final task cursor committed.
