# Project Memory Design

Status: Accepted design  
Date: 2026-08-05  
Scope: Backend, Launcher/Relay, Flutter, and project-scoped Agents

## 1. Purpose

Project Memory provides durable shared context for every Agent working in one
project folder. The memory is stored by the Backend so that Flutter clients can
inspect and manage it from other devices, while source files and large local
artifacts remain on the machine that owns the folder.

The feature is project-scoped. It is not an operator-wide or cross-project
memory system.

## 2. Understanding Lock

- A project is one concrete folder on one concrete machine.
- Project identity is `operator_id + machine_id + project_scope_id`.
- Different machines and different folders are isolated, even when their
  contents or markers are identical.
- Folder rename, move, or cross-volume relocation retains memory when it can be
  identified as the same project.
- A copied folder is a new project by default.
- Automatic memory curation is invisible. Only a user-triggered organization
  job exposes progress and errors.
- Project Memory is generic for all Agents. Android/native-hook data is an
  optional domain extension.
- Existing project prompts remain available and are not overwritten or
  automatically migrated into Project Memory.

### Non-goals

- Cross-project global memory.
- Uploading or backing up complete source trees, APKs, SOs, IDA databases,
  build artifacts, or large logs.
- Replacing session history, task events, or context compaction.
- Promoting inference to verified fact without evidence.
- Letting the Curator edit, build, install, publish, or actively run hooks.

## 3. High-level Architecture

Project Memory has five logical components:

1. **Project Identity Resolver** in Launcher identifies the folder and announces
   a stable scope to Backend.
2. **Project Memory Curator Worker** in Relay reads bounded project evidence and
   emits strict candidate JSON.
3. **Backend Reconciler** validates, deduplicates, versions, and commits memory.
4. **Prompt Builder** retrieves a stable memory snapshot and adds it to the
   normal Agent system context.
5. **Flutter Memory UI** manages memory and exposes manual organization jobs.

MySQL is the source of truth for structured memory. The project machine remains
the source of truth for local files and artifacts.

## 4. Project Identity

### 4.1 Identifiers

- `project_scope_id`: permanent UUID for the folder project.
- `machine_id`: permanent device identity and an isolation boundary.
- `agent_id`: current Agent process identity; never used as project identity.
- `project_id`: legacy display name; gradually demoted from identity semantics.
- `project_root`: current path for display and local access, not identity.
- `binding_epoch`: monotonically increasing binding generation.
- `instance_nonce`: random marker instance used during copy/move reconciliation.

### 4.2 Local state

The preferred marker is:

```text
.chat-codex/project.json
```

Git projects add it to `.git/info/exclude`, avoiding tracked project changes.
Launcher also keeps a registry under its platform application-data directory.
The registry is the fallback for read-only folders and filesystems with unstable
file IDs.

### 4.3 Platform identity

- Windows: Volume Serial and File ID obtained with `CreateFileW`,
  `GetFileInformationByHandleEx(FileIdInfo)`, and
  `GetFinalPathNameByHandleW`.
- macOS: `st_dev`, `st_ino`, and canonical `realpath`.
- Linux: `st_dev`, `st_ino`, and canonical `realpath`.
- FAT, exFAT, SMB, and NAS: marker, Launcher registry, path history, and Backend
  claim reconciliation.

Recommended implementation files:

```text
directory_identity_windows.go
directory_identity_darwin.go
directory_identity_linux.go
directory_identity_fallback.go
```

### 4.4 Move, copy, and collision rules

- Same filesystem identity: rename or same-volume move; retain scope.
- Same marker and old location is reachable: new location is a copy; fork scope.
- Same marker and old location is absent: new location inherits the existing
  scope as a likely cross-volume move.
- If the old location later returns, the later claim forks to a new scope. The
  already active scope never flips back and forth.
- Identical marker on a different `machine_id`: always a separate scope.
- Every fork records `lineage_project_scope_id` for diagnostics only. Memory is
  not shared across the lineage.
- Flutter identity diagnostics provides "Keep memory here" and "Treat as new
  project" corrections for ambiguous cases.

## 5. Memory Model

### 5.1 Kinds

```text
locked_rule
verified_fact
inferred_fact
procedure
decision
issue
episode
```

### 5.2 Statuses

```text
active
superseded
disputed
stale
archived
detached
missing
deleted
```

### 5.3 Verification status

```text
verified
unverified
failed
stale
```

Each memory version is immutable. Human edits and accepted replacements create
a new version linked by `logical_memory_id` and `supersedes_memory_id`.

### 5.4 Lock semantics

- Locking prevents automatic rewrite, deletion, or replacement of content.
- `locked_rule` remains active until a user unlocks or deletes it.
- Technical facts may acquire a `stale` or `disputed` overlay without changing
  their locked content.
- A locked technical fact marked `stale` or `disputed` is not injected as a
  currently valid fact.
- Human-edited but unlocked memory has higher source weight, but newer verified
  evidence may supersede it.

## 6. Android and Native-hook Extension

Project Memory is generic. Android records may additionally carry:

```text
package_name
app_version
apk_sha256
module_name
so_sha256
build_id
abi
symbol
relative_offset
function_start
instruction_set
ida_database_id
ida_analysis_status
hot_update_status
shadowhook_status
ui_call_status
native_call_status
```

Rules for native-hook memory:

- A Hook offset is always relative to a named module and bound to SO SHA256,
  Build ID, ABI, and app version.
- The function boundary and instruction set must be known before a candidate can
  become verified. This prevents accidental inline-function or mid-instruction
  hooks.
- The Android Agent probes for hot-update frameworks and runtime-downloaded code
  before finalizing a Hook target.
- Large SO full analysis may run as a background IDA process for more than 60
  minutes. Its process, sample fingerprint, database, and completion state are
  recorded; there is no 60-minute hard timeout.
- The Curator may query an existing IDA database but does not start a new full
  analysis solely for memory curation.
- ShadowHook installation alone is not success. Runtime evidence must prove that
  ShadowHook initialized and the intended callback executed.
- UI, bridge, and native calls must each have observable evidence. An empty UI
  implementation or an uncalled native callback remains `unverified` or
  `failed`.
- A changed APK/SO fingerprint makes old offsets stale immediately.

## 7. Storage

### 7.1 Server tables

```text
project_scopes
project_scope_locations
project_agent_bindings
project_memories
project_memory_sources
project_memory_artifacts
project_memory_jobs
project_memory_job_events
project_briefs
project_memory_usages
project_memory_audits
```

### 7.2 Table responsibilities

`project_scopes` stores permanent identity, operator, machine, display name,
status, lineage, revision, and timestamps.

`project_scope_locations` stores encrypted absolute paths, filesystem identity,
marker nonce, binding epoch, claim state, and location history.

`project_agent_bindings` maps replaceable Agent instances to stable scopes.

`project_memories` stores immutable versions, subject key, kind, status,
statement, confidence, lock state, sensitivity, content hash, and replacement
links.

`project_memory_sources` stores session, task, message, tool-call, relative-path,
encrypted excerpt, and source availability references.

`project_memory_artifacts` stores artifact hashes, Build ID, ABI, version,
module, and optional domain metadata.

`project_memory_jobs` stores trigger, cursor range, state, attempt, lease,
fencing token, revisions, checkpoint, and error.

`project_memory_job_events` stores internal progress. Automatic events are not
listed in the user task UI. They become visible only when a manual request is
attached to the job.

`project_briefs` stores brief content, token count, source revision, model, and
generation time.

`project_memory_usages` stores task ID, memory IDs, memory revision, and token
count. It does not store the complete assembled prompt.

`project_memory_audits` records human view, edit, lock, delete, restore,
organization, and conflict-resolution actions.

### 7.3 Encryption and search

- Absolute paths, source excerpts, and sensitive statements use authenticated
  field encryption with key IDs and rotation support.
- Keys come from server secret management and are never stored in the database.
- Directory display name, non-sensitive statements, entities, symbols, relative
  paths, hashes, and indexed status fields remain searchable.
- MySQL FULLTEXT indexes only non-sensitive search documents.
- Sensitive memory is excluded from Prompt injection unless explicitly enabled
  for the current session.

### 7.4 Device-local data

The device retains source, APK, SO, IDA database, build output, complete logs,
marker, Launcher registry, transient Curator input, and disposable caches. Large
local artifacts are represented on the server by structured facts, hashes, and
source references.

## 8. Curator Agent

### 8.1 Execution

- One Curator Worker per project scope.
- At most two Curator Workers per device by default.
- Curator state and Session are separate from Relay's foreground `state.task`.
- Curator runs at low priority and may run concurrently with a user task.
- There is no fixed 60-minute timeout.

### 8.2 Permissions

The Curator can read sessions, task events, project files, Git state, build logs,
test reports, artifact metadata, and existing tool output. It can run read-only
commands such as `rg`, `git log/show/diff`, hashing, and file-type inspection.

It does not edit files, compile, install, publish, execute hooks, or start a new
full binary analysis. Missing runtime evidence produces an unverified candidate
or an issue, never a verified fact.

### 8.3 Triggers

- Task completion: automatically enqueue or extend a hidden job.
- Long Goal checkpoint: incrementally enqueue up to the checkpoint cursor.
- Compaction: record and advance source boundaries; do not create duplicate
  memory for an already processed range.
- Flutter "Organize project memory now": create or attach to a visible manual
  job.

### 8.4 Candidate output

The Curator returns strict JSON and never requests a database mutation directly:

```json
{
  "schema_version": 1,
  "project_scope_id": "proj_UUID",
  "job_id": "job_UUID",
  "source_cursor": "CURSOR",
  "candidates": [
    {
      "subject_key": "native_hook:MODULE:FUNCTION",
      "kind": "verified_fact",
      "statement": "FUNCTION is hooked at RELATIVE_OFFSET.",
      "confidence": 0.98,
      "scope_paths": ["PATH"],
      "artifact_refs": [
        {
          "path": "MODULE",
          "sha256": "SO_SHA256",
          "build_id": "BUILD_ID",
          "abi": "arm64-v8a",
          "app_version": "VERSION"
        }
      ],
      "native_hook": {
        "symbol": "FUNCTION",
        "relative_offset": "OFFSET",
        "function_start": "FUNCTION_START",
        "instruction_set": "aarch64",
        "ida_analysis_status": "complete",
        "shadowhook_status": "verified",
        "ui_call_status": "verified",
        "native_call_status": "verified"
      },
      "verification": {
        "status": "verified",
        "method": "runtime_log",
        "evidence_refs": ["TOOL_CALL_ID", "LOG_PATH"]
      },
      "source_refs": {
        "session_id": "SESSION_ID",
        "task_id": "TASK_ID",
        "message_ids": ["MESSAGE_ID"]
      },
      "sensitive": false
    }
  ],
  "brief_candidate": {
    "content": "PROJECT_BRIEF",
    "included_subject_keys": ["SUBJECT_KEY"]
  }
}
```

Backend validates schema, sizes, enums, scope, source ownership, artifact
fingerprints, lease, and fencing token before reconciliation. An invalid schema
or over-limit batch is quarantined as a whole.

## 9. Reconciliation

- Same `content_hash + scope_paths + artifact_hashes`: merge sources and refresh
  metadata without duplicating the memory.
- New conclusion for the same subject: create a version and link the previous
  version through `supersedes_memory_id`.
- Verified evidence may supersede inference for the same artifact version.
- Inference does not silently supersede verified evidence.
- Conflicting verified facts remain present and become `disputed`.
- Human locks and edits win over candidates based on an older input revision.
- Deletion tombstones have the highest merge priority and prevent offline
  resurrection.
- Artifact fingerprint changes make bound technical facts stale without deleting
  their history.
- Curator invalidations carry the target content hash, reason, and evidence
  references. The reconciler applies only matching current records and preserves
  human-created or locked records as disputed rather than overwriting them.
- Failed ShadowHook, UI-call, or native-call evidence remains available as failed
  evidence but is excluded from valid context.

All writes for one scope use a short MySQL transaction. Lock ordering is Scope,
Memory IDs in deterministic order, then Brief. No model or device calls occur
inside a transaction.

## 10. Job Scheduling and Concurrency

### 10.1 Job lifecycle

Backend creates an asynchronous job and immediately returns for manual API
requests. The worker renews a five-minute lease every 30 seconds. Disconnects or
process exits allow another worker to claim the job after lease expiration.

Each claim increments `fencing_token`. Every accepted, progress, checkpoint,
candidate, completion, and failure message carries the token. Backend rejects
messages from a previous claim even when the old worker continues running.

Automatic jobs retry three times with backoff. Manual users can continue from
the last checkpoint after failure.

### 10.2 Coalescing

- A scope has at most one active Curator job.
- Overlapping automatic triggers extend or merge the pending cursor range.
- Automatic task-completion and goal-checkpoint jobs wait for a persistent
  60-second quiet window after the latest trigger. Manual jobs remain immediate.
- A manual trigger attaches to an active automatic job and makes that job visible
  to the requesting user.
- `cursor_end` is fixed for each execution attempt. New source events belong to a
  later job.
- The committed source cursor advances only after successful reconciliation.

### 10.3 File consistency

Curator captures the relevant Git state, artifact fingerprints, and referenced
file hashes. It rechecks those hashes after reading. Candidates based on changed
files are discarded or deferred. A full source-tree snapshot is not required.

### 10.4 Revision consistency

- Scope Revision is incremented by every committed memory mutation.
- Reconciler rebases stale candidates and gives human changes priority.
- Brief updates use `source_revision` compare-and-swap.
- Prompt assembly reads one committed Revision and uses it for the complete task.
- Cache keys include Scope Revision and query fingerprint.
- Project relocation increments `binding_epoch`; messages from an old path binding
  are rejected.
- Backend multi-instance safety relies on MySQL claims, leases, transactions, and
  fencing, not process-local locks.

## 11. Retrieval and Prompt Building

### 11.1 Search implementation

The first release uses structured filtering plus MySQL FULLTEXT. Curator emits
entities, symbols, relative paths, artifact hashes, and topics. Retrieval filters
by scope and validity first, then ranks text candidates.

The schema reserves `embedding_model` and `embedding_version` extension fields.
An in-process HNSW or dedicated vector store is deferred until measured recall
shows a need.

### 11.2 Ranking

Ranking considers, in order:

1. Manual locked rule.
2. Exact artifact fingerprint, ABI, module, symbol, or relative path.
3. Verification status and memory kind.
4. FULLTEXT relevance over statement, entity, symbol, path, and topic.
5. Recency and confidence.

`stale`, `failed`, and `disputed` items are excluded from normal valid context.
They may be included as warnings when the current task explicitly concerns
history, regressions, or failed verification.

### 11.3 Prompt shape

```text
<project_locked_rules>
Human-locked active rules
</project_locked_rules>

<project_memory>
Project brief and task-relevant context
</project_memory>
```

Project source excerpts remain untrusted data and cannot override higher-priority
instructions. Locked rules are separated from descriptive memory.

### 11.4 Token budget

- Project brief target: 3,000 tokens.
- Dynamic retrieval target: 7,000 tokens.
- Locked rules and essential metadata reserve: approximately 2,000 tokens.
- Normal shared-context ceiling: 12,000 tokens.
- Absolute ceiling: 16,000 tokens.
- Overflow removes low-confidence, old Episode, and weakly related memory first.
- Active locked rules are never removed by ordinary relevance trimming.

Retrieval target is P95 under 500 ms at 20,000 active memories. Complete Prompt
assembly target is under one second.

## 12. Failure Behavior

- Prompt retrieval exceeding one second falls back to cached locked rules and
  brief, omitting dynamic memory.
- Memories whose file-backed sources are all missing or changed are excluded from
  normal prompt context; non-file task and message evidence remains usable.
- With no valid cache, the task continues with the existing project prompt.
- Automatic curation failures retry silently and never create chat messages,
  task cards, badges, notifications, or user-visible errors.
- Manual curation exposes progress, checkpoint, failure, and continue actions.
- Device reconnect resumes from the committed cursor and valid fencing token.
- Missing local source marks references `source_missing`; memory remains stored.
- A reappearing source with the same hash restores availability.
- Read-only folders use the Launcher registry when marker writes fail.
- Optimistic merge conflicts retry up to five times, then retain candidates for a
  later job.
- Backend restart reconstructs active jobs from MySQL. Retrieval caches are
  disposable.
- Project Memory failures do not block existing chat, project prompt, file, or
  device-management behavior.

## 13. API

```text
GET    /api/devices/{machineID}/projects
GET    /api/devices/{machineID}/projects/{scopeID}/memory
GET    /api/devices/{machineID}/projects/{scopeID}/memories
GET    /api/devices/{machineID}/projects/{scopeID}/memories/{memoryID}
PATCH  /api/devices/{machineID}/projects/{scopeID}/memories/{memoryID}
DELETE /api/devices/{machineID}/projects/{scopeID}/memories/{memoryID}
POST   /api/devices/{machineID}/projects/{scopeID}/memories/{memoryID}/resolve
POST   /api/devices/{machineID}/projects/{scopeID}/memory-jobs
GET    /api/devices/{machineID}/projects/{scopeID}/memory-jobs/{jobID}
GET    /api/devices/{machineID}/projects/{scopeID}/memory-jobs/{jobID}/events
```

Memory listing supports kind, status, verification, locked, query, relative path,
artifact hash, cursor, and limit filters. Pagination is cursor-based.

Every API operation validates `operator_id + machine_id + project_scope_id` by
server-side lookup. Object IDs supplied by clients are never trusted as proof of
ownership.

## 14. Device Protocol

Hello advertises a `project_memory_v1` capability. New message types are:

```text
project.scope.announce
project.memory.run
project.memory.accepted
project.memory.progress
project.memory.checkpoint
project.memory.candidates
project.memory.completed
project.memory.failed
project.memory.cancel
```

Old Launchers ignore the feature and continue normal chat behavior. Backend
accepts the current candidate schema and the immediately previous schema during
rolling upgrades.

## 15. Flutter Experience

- Keep the existing Project Prompt control unchanged.
- Add Project Memory to the Chat project menu.
- Route: `/devices/:machineId/projects/:scopeId/memory`.
- Header displays directory name, search, filters, and "Organize now".
- Tabs: All, Rules, Facts, Procedures, Decisions, and Issues.
- Rows are compact and open a detail view with content, version, verification,
  artifacts, source, and history.
- Detail actions: edit, lock, unlock, delete, restore, and resolve conflict.
- Absolute path appears only in source detail and identity diagnostics.
- Offline project machines still allow server-side browse, search, and edits.
  Manual organization and local-source opening are disabled.
- Detached or missing projects show state without deleting memory.
- Automatic jobs have no visible state anywhere in the normal user workflow.
- Manual jobs display queue, read, candidate, merge, completion, and error state.

## 16. Scale and Retention

- Up to 20,000 active structured memories per project.
- Up to 200,000 archived, stale, or historical versions per project.
- Older history is rolled into Episode summaries when limits are exceeded; source
  task/session archives remain separate.
- Statement limit: 2,000 characters.
- Source references per memory: 32.
- Large output is represented by path, hash, and summary.
- Deleted tombstones remain for 30 days. Sensitive ciphertext is cleared
  immediately while ID, version, and deletion time remain for synchronization.

## 17. Security and Privacy

- Project ownership is checked on every list and object operation.
- Complete project files are not uploaded by default.
- Automatic Curator output redacts raw passwords, tokens, private keys, and
  cookies. It records credential names and required configuration instead.
- Sensitive memory is encrypted and excluded from default Prompt injection.
- Complete paths and excerpts are encrypted at rest and redacted in operational
  logs.
- View, edit, lock, delete, restore, manual organization, and conflict resolution
  are auditable.
- A deleted sensitive record retains only the minimum tombstone metadata.

## 18. Observability and Maintenance

Backend owns Scope, Job, Reconciler, retrieval, Prompt Builder, audit, and cleanup.
Launcher/Relay owns local identity, read-only evidence access, Worker execution,
lease heartbeat, and checkpoints. Flutter owns presentation and manual actions.

Internal metrics include queue depth, job duration, lease expiry, retries,
candidate acceptance, conflict rate, retrieval latency, injected token count,
and cache fallback count.

Scheduled maintenance handles tombstone purge, Episode rollup, orphaned source
checks, Brief rebuild, and stale cache cleanup. Database rollout adds nullable
fields first, enables the new capability second, then tightens constraints.

## 19. Test and Acceptance Plan

### Identity

- macOS, Linux, and Windows directory identity.
- Rename, same-volume move, cross-volume move, copy, read-only directory,
  unstable filesystem ID, and collision recovery.

### Isolation and security

- No memory, source, search, usage, audit, or cache leakage across operator,
  machine, or scope boundaries.
- Sensitive memory is excluded unless explicitly enabled.
- Object-ID enumeration does not bypass ownership checks.

### Jobs and concurrency

- Duplicate triggers, cursor coalescing, automatic/manual overlap, reconnect,
  Backend restart, lease expiry, stale fencing token, and checkpoint resume.
- Simulated jobs exceeding 90 minutes complete without a hard timeout.
- Concurrent human edit, lock, delete, and Curator merge obey precedence.
- File or artifact changes during Curator reads invalidate affected candidates.
- A stale Brief never overwrites a newer revision.

### Retrieval

- P95 under 500 ms with 20,000 active records per project.
- Correct 12,000 and 16,000 token trimming.
- Failed, stale, disputed, and sensitive records do not enter normal valid
  context.
- Exact path, symbol, artifact, ABI, and Build ID matches outrank weak text.

### Android extension

- IDA function boundary, SO fingerprint, relative offset, and instruction-set
  requirements.
- Hot-update detection state.
- ShadowHook initialization and callback evidence.
- UI, bridge, and native-call evidence; empty UI or uncalled native code remains
  unverified.
- Changed APK/SO marks prior Hook facts stale.

### Flutter and compatibility

- Automatic curation is invisible.
- Manual curation shows progress and continuation.
- Offline server-side memory management remains available.
- Old Launcher and Flutter versions preserve existing behavior.
- Project Memory failure does not regress chat or Project Prompt.

### End-to-end acceptance

- A newly accepted memory is available to the next relevant task in the same
  scope.
- A copied folder does not inherit memory.
- A moved folder retains memory.
- Every injected memory can be traced to stored source references.

## 20. Decision Log

1. **Backend-held structured memory.** Chosen for cross-device Flutter access and
   centralized conflict handling. Raw project data stays local.
2. **Folder-scoped identity.** Chosen over basename, Agent ID, and Git root commit
   because those identities do not preserve the required folder isolation and
   lifecycle.
3. **Machine as an isolation boundary.** Identical folders on different machines
   remain separate projects.
4. **Marker plus filesystem identity plus registry.** Chosen because no single
   method handles moves, copies, read-only folders, and all target filesystems.
5. **Prefer move retention in ambiguous cases.** If the old path is absent, the
   new claim keeps memory; later collisions fork without identity oscillation.
6. **Structured memory as fact source.** Chosen over a single rolling summary so
   that facts remain versioned, searchable, verifiable, and editable.
7. **Brief plus dynamic retrieval.** Chosen to keep stable context while fitting
   task-specific details into a bounded token budget.
8. **MySQL structured and FULLTEXT retrieval first.** Chosen over immediate HNSW
   or a vector database to match current infrastructure and expected scale.
9. **Dedicated invisible Curator.** Chosen so normal Agents receive memory
   automatically without user-managed summarization steps.
10. **Read-only Curator.** Chosen to prevent invisible background work from
    modifying, building, installing, publishing, or running project behavior.
11. **Candidates plus Backend reconciliation.** Chosen over direct Agent writes
    for validation, idempotency, conflict handling, and auditing.
12. **Independent Curator execution state.** Chosen because the existing Relay
    foreground task state supports only one current task.
13. **Lease plus fencing token.** Chosen because lease expiry alone does not stop
    a stale worker from submitting results after a split-brain reconnect.
14. **Project-level serial commit and Revision snapshots.** Chosen for consistent
    Prompt reads and deterministic human-versus-Curator precedence.
15. **Automatic jobs hidden, manual jobs visible.** Chosen to keep normal chat
    quiet while preserving explicit user control and diagnostics.
16. **Human locks protect content, not false validity.** Chosen so old technical
    facts can become stale without silently changing human-authored content.
17. **Android as an optional extension.** Chosen so one Project Memory system can
    serve all Agents while enforcing stronger evidence for native hooks.
18. **Existing Project Prompt remains separate.** Chosen for backward
    compatibility and to avoid silently changing user-authored configuration.

## 21. Implementation Gate

Implementation starts only after this accepted design is converted into a
dependency-ordered implementation plan. The plan must preserve unrelated changes
in the current worktree and introduce schema, protocol, Backend behavior,
Launcher/Relay behavior, and Flutter UI incrementally behind a feature flag.
