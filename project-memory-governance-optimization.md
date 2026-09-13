# Project Memory Governance Optimization

## Goal

Reduce redundant automatic curation and prevent stale, conflicted, or unsupported memories from entering normal Agent prompts while preserving audit history and human precedence.

## Decisions

- Automatic `task_complete` and `goal_checkpoint` jobs use a persistent 60-second quiet window. Repeated triggers update the queued job timestamp and cursor, so work starts after activity settles.
- Manual organization, source rechecks, brief rebuilds, retries, and expired-lease recovery remain immediate.
- A memory whose file-backed sources are all `changed` or `source_missing` is excluded from prompt assembly. Non-file task/message evidence remains valid.
- Curator output may include evidence-backed invalidations with `memory_id`, `content_hash`, target status (`stale` or `disputed`), reason, and evidence references.
- The backend validates and applies invalidations transactionally. Content-hash mismatch, deleted history, and superseded history are left unchanged.
- Curator invalidation changes status only; it never physically deletes memory. User locks, edits, tombstones, immutable history, and project isolation remain authoritative.

## Tasks

- [x] Add quiet-window filtering to in-memory and MySQL claim queries.
- [x] Exclude memories with no usable file source during prompt assembly.
- [x] Extend Curator protocol and reconciliation with evidence-backed invalidations.
- [x] Add scheduler, prompt, reconciliation, and Relay parser tests.
- [x] Run focused backend and Relay test suites.

## Done When

- [x] Repeated automatic triggers coalesce for 60 seconds while manual jobs run immediately.
- [x] Changed or missing-only file memories do not appear in `<project_memory>`.
- [x] Valid Curator invalidations stop target memories from future injection without deleting history.
- [x] Stale invalidations cannot overwrite a newer content hash.
