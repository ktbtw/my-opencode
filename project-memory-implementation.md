# Project Memory Implementation

## Goal

Implement `docs/project-memory-design.md` end to end across Backend, Launcher/
Relay, and Flutter, with production-grade isolation, concurrency, recovery, and
tests.

## Tasks

- [x] Define shared domain models, store interfaces, MySQL migrations, indexes,
  and in-memory test storage. Verify migrations are idempotent and store tests
  cover versioning, tombstones, locks, and scope isolation.
- [x] Add cross-platform project identity resolution and Scope/Agent binding
  announcements. Verify rename/move/copy behavior on supported platforms and
  deterministic collision handling.
- [x] Add asynchronous Curator Job scheduling, coalesced cursors, leases,
  fencing tokens, recovery, and device protocol capability negotiation. Verify
  duplicate workers and stale messages are rejected.
- [x] Add candidate validation, Reconciler transactions, Brief generation,
  structured/FULLTEXT retrieval, Prompt Builder injection, usage records, and
  audit records. Verify stale, disputed, sensitive, and locked memory rules.
- [x] Add authenticated Backend memory APIs and Flutter project memory screens,
  manual organization flow, search/filter/detail/edit/lock/delete/conflict
  handling, and invisible automatic-job behavior.
- [x] Add production tests for API authorization, MySQL and memory stores,
  concurrency, reconnects, long-running jobs, cross-device access, Flutter
  models/UI behavior, and end-to-end prompt injection. Run all relevant Go and
  Flutter test suites plus formatting and static checks.

## Done When

- [x] Every accepted design requirement has an implementation and a direct test
  or runtime verification.
- [x] Different scopes cannot share memory or caches, while moves retain scope
  and copies fork scope.
- [x] Automatic curation is invisible and manual curation is observable.
- [x] Long jobs survive reconnects and stale workers cannot commit candidates.
- [x] Project Memory failures do not regress existing chat or project prompts.
- [x] Full verification output is recorded in the final response.

## Verification

Verified on 2026-08-05.

| Design area | Implementation | Direct evidence |
| --- | --- | --- |
| Folder identity and isolation | `launcher/internal/projectidentity`, Scope announcements, marker/registry reconciliation | Rename, move, copy, unstable-ID, read-only, machine-isolation, correction, and concurrent-registry tests |
| Structured server storage | Project Memory models, in-memory Store, MySQL schema/migrations, sources, artifacts, jobs, briefs, usages, and audits | Store unit tests plus real MySQL legacy migration and idempotency tests |
| Privacy and ownership | Field encryption/key rotation, redaction, non-sensitive `search_text`, server-side Scope ownership checks | Cipher tamper/rotation, credential redaction, sensitive FULLTEXT, API object-enumeration, and usage/audit association tests |
| Curator scheduling | Hidden automatic scheduling, manual attachment, cursor coalescing, leases, fencing, checkpoints, restart recovery, two-worker device limit | Race tests and real MySQL atomic schedule/finalize, restart/lease, checkpoint, and device-limit tests |
| Curator protocol | `project_memory_v1`, strict candidate JSON, current/previous schema support, source hash recheck, cooperative cancellation, and shutdown joining | Backend protocol tests, Relay parser/hash/feature-flag tests, cross-process cleanup, and cancellation-during-source-inspection test |
| Reconciliation | Source merge, immutable versions, tombstones, human precedence, stale artifacts, Brief CAS | In-memory and real MySQL reconciliation consistency tests; stale/superseded delete rejection and idempotent tombstone tests |
| Retrieval and Prompt | Structured filters, pure MySQL FULLTEXT over `search_text`, revision-keyed cache, 12k/16k token bounds, usage trace | Prompt validity/ranking/cache-isolation tests, subject-key FULLTEXT test, and 20,000-record benchmarks |
| Android native-hook proof | APK/SO fingerprints, IDA database/status, function boundary, aligned relative offset, ISA, hot-update, ShadowHook/UI/native runtime states | Candidate validation matrix rejects each missing or unverified proof field; changed artifact test marks old Hook facts stale |
| API and Flutter | Authenticated CRUD/history/conflict/identity/job APIs, route, filters, detail/actions, manual progress dialog, offline browsing | API ownership/conflict/hidden-job tests; composite pagination, fact union, deleted browse/restore, stale resolve/delete, and sensitive-tombstone Widget tests |
| End-to-end injection | Backend schedules Curator after completion; Relay emits candidates; Backend reconciles; next task receives stable memory snapshot | Cross-process Relay/Backend E2E asserts stored source task and `<project_memory>` in the next real model input |

### Command Results

- Backend: `go test -race -count=1 ./...` passed; `go vet ./...` passed.
- Backend Queue regression: nil task metadata is initialized before attaching
  `project_memory_scope_id` and `project_memory_revision`; the real WebSocket
  dispatch test passed.
- MySQL: all nine functional Project Memory integration cases passed, covering
  legacy migration/idempotency, source retention, sensitive and subject-key
  FULLTEXT, usage/audit Scope association, atomic scheduling/finalization,
  restart lease takeover, fencing/checkpoints, reconciliation, artifact
  invalidation, ownership isolation, stable composite pagination, delete CAS,
  immutable historical versions, and device claim limits. Cold schema setup is
  allowed five minutes in the test fixture; this is not a Curator job timeout.
- MySQL performance at 20,000 active records: retrieval P95 `89.390417ms`,
  maximum `92.12525ms`; the enforced threshold is `500ms`.
- Complete 20,000-record Prompt assembly: P95 `44.11ms`, maximum `48.42ms`;
  the enforced threshold is `500ms`.
- Launcher: `go test -p 1 -count=1 ./...` passed, confirming the parallel-only
  stuck-port upgrade failure is package resource contention. Windows compilation tests passed with
  `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go test -exec=true ./...`.
- Relay: `bun run typecheck` passed. The two targeted Relay suites passed with
  `68 pass`, `0 fail`, including the cross-process Backend E2E and immediate
  Curator cancellation during source inspection. Relay shutdown cancels and
  joins the active Curator before temporary project cleanup.
- Flutter: all `113` tests passed; the eight Project Memory tests cover models,
  offline browsing, composite-cursor loading, fact-union filtering, and
  sensitive tombstone actions. Targeted Project Memory analyze reported
  `No issues found`.
- Flutter full-project analyze reports 15 existing info-level diagnostics in
  unrelated Web, authentication, and markdown files; Project Memory paths are
  clean.
