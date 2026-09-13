package model

import "time"

type ProjectScopeStatus string

const (
	ProjectScopeActive   ProjectScopeStatus = "active"
	ProjectScopeDetached ProjectScopeStatus = "detached"
	ProjectScopeMissing  ProjectScopeStatus = "missing"
	ProjectScopeDeleted  ProjectScopeStatus = "deleted"
)

type ProjectMemoryKind string

const (
	ProjectMemoryLockedRule   ProjectMemoryKind = "locked_rule"
	ProjectMemoryVerifiedFact ProjectMemoryKind = "verified_fact"
	ProjectMemoryInferredFact ProjectMemoryKind = "inferred_fact"
	ProjectMemoryProcedure    ProjectMemoryKind = "procedure"
	ProjectMemoryDecision     ProjectMemoryKind = "decision"
	ProjectMemoryIssue        ProjectMemoryKind = "issue"
	ProjectMemoryEpisode      ProjectMemoryKind = "episode"
)

type ProjectMemoryStatus string

const (
	ProjectMemoryActive     ProjectMemoryStatus = "active"
	ProjectMemorySuperseded ProjectMemoryStatus = "superseded"
	ProjectMemoryDisputed   ProjectMemoryStatus = "disputed"
	ProjectMemoryStale      ProjectMemoryStatus = "stale"
	ProjectMemoryArchived   ProjectMemoryStatus = "archived"
	ProjectMemoryDetached   ProjectMemoryStatus = "detached"
	ProjectMemoryMissing    ProjectMemoryStatus = "missing"
	ProjectMemoryDeleted    ProjectMemoryStatus = "deleted"
)

type ProjectMemoryVerificationStatus string

const (
	ProjectMemoryVerified   ProjectMemoryVerificationStatus = "verified"
	ProjectMemoryUnverified ProjectMemoryVerificationStatus = "unverified"
	ProjectMemoryFailed     ProjectMemoryVerificationStatus = "failed"
	ProjectMemoryStaleCheck ProjectMemoryVerificationStatus = "stale"
)

type ProjectMemoryJobStatus string

const (
	ProjectMemoryJobQueued    ProjectMemoryJobStatus = "queued"
	ProjectMemoryJobClaimed   ProjectMemoryJobStatus = "claimed"
	ProjectMemoryJobRunning   ProjectMemoryJobStatus = "running"
	ProjectMemoryJobCompleted ProjectMemoryJobStatus = "completed"
	ProjectMemoryJobFailed    ProjectMemoryJobStatus = "failed"
	ProjectMemoryJobCancelled ProjectMemoryJobStatus = "cancelled"
)

type ProjectMemoryTrigger string

const (
	ProjectMemoryTriggerTaskComplete   ProjectMemoryTrigger = "task_complete"
	ProjectMemoryTriggerGoalCheckpoint ProjectMemoryTrigger = "goal_checkpoint"
	ProjectMemoryTriggerCompaction     ProjectMemoryTrigger = "compaction"
	ProjectMemoryTriggerManual         ProjectMemoryTrigger = "manual"
	ProjectMemoryTriggerSourceRecheck  ProjectMemoryTrigger = "source_recheck"
	ProjectMemoryTriggerBriefRebuild   ProjectMemoryTrigger = "brief_rebuild"
)

type ProjectScope struct {
	ID             string             `json:"project_scope_id"`
	OperatorID     int64              `json:"-"`
	MachineID      string             `json:"machine_id"`
	DisplayName    string             `json:"display_name"`
	CurrentRoot    string             `json:"current_root,omitempty"`
	VolumeID       string             `json:"filesystem_volume_id,omitempty"`
	FileID         string             `json:"filesystem_file_id,omitempty"`
	InstanceNonce  string             `json:"instance_nonce,omitempty"`
	LineageScopeID string             `json:"lineage_project_scope_id,omitempty"`
	BindingEpoch   int64              `json:"binding_epoch"`
	Status         ProjectScopeStatus `json:"status"`
	Revision       int64              `json:"revision"`
	CreatedAt      time.Time          `json:"created_at"`
	UpdatedAt      time.Time          `json:"updated_at"`
}

type ProjectScopeLocation struct {
	ID           string    `json:"location_id"`
	ScopeID      string    `json:"project_scope_id"`
	OperatorID   int64     `json:"-"`
	MachineID    string    `json:"machine_id"`
	Root         string    `json:"root,omitempty"`
	DisplayName  string    `json:"display_name"`
	VolumeID     string    `json:"filesystem_volume_id,omitempty"`
	FileID       string    `json:"filesystem_file_id,omitempty"`
	MarkerNonce  string    `json:"instance_nonce,omitempty"`
	BindingEpoch int64     `json:"binding_epoch"`
	ClaimState   string    `json:"claim_state,omitempty"`
	Reachable    bool      `json:"reachable"`
	FirstSeenAt  time.Time `json:"first_seen_at"`
	LastSeenAt   time.Time `json:"last_seen_at"`
}

type ProjectAgentBinding struct {
	OperatorID   int64     `json:"-"`
	MachineID    string    `json:"machine_id"`
	AgentID      string    `json:"agent_id"`
	ScopeID      string    `json:"project_scope_id"`
	BindingEpoch int64     `json:"binding_epoch"`
	Active       bool      `json:"active"`
	AttachedAt   time.Time `json:"attached_at"`
	DetachedAt   time.Time `json:"detached_at,omitempty"`
}

type ProjectMemoryArtifact struct {
	Path             string `json:"path,omitempty"`
	SHA256           string `json:"sha256,omitempty"`
	PackageName      string `json:"package_name,omitempty"`
	APKSHA256        string `json:"apk_sha256,omitempty"`
	BuildID          string `json:"build_id,omitempty"`
	ABI              string `json:"abi,omitempty"`
	AppVersion       string `json:"app_version,omitempty"`
	ModuleName       string `json:"module_name,omitempty"`
	Symbol           string `json:"symbol,omitempty"`
	RelativeOffset   string `json:"relative_offset,omitempty"`
	FunctionStart    string `json:"function_start,omitempty"`
	InstructionSet   string `json:"instruction_set,omitempty"`
	IDAStatus        string `json:"ida_analysis_status,omitempty"`
	IDADatabaseID    string `json:"ida_database_id,omitempty"`
	HotUpdateStatus  string `json:"hot_update_status,omitempty"`
	ShadowHookStatus string `json:"shadowhook_status,omitempty"`
	UICallStatus     string `json:"ui_call_status,omitempty"`
	NativeCallStatus string `json:"native_call_status,omitempty"`
}

type ProjectMemorySource struct {
	SessionID    string   `json:"session_id,omitempty"`
	TaskID       string   `json:"task_id,omitempty"`
	MessageIDs   []string `json:"message_ids,omitempty"`
	ToolCallIDs  []string `json:"tool_call_ids,omitempty"`
	RelativePath string   `json:"relative_path,omitempty"`
	SHA256       string   `json:"sha256,omitempty"`
	Excerpt      string   `json:"excerpt,omitempty"`
	Status       string   `json:"status,omitempty"`
}

type ProjectMemorySourceAvailability struct {
	MemoryID     string `json:"memory_id"`
	RelativePath string `json:"relative_path"`
	SHA256       string `json:"sha256,omitempty"`
	Status       string `json:"status"`
}

type ProjectMemoryVerification struct {
	Status       ProjectMemoryVerificationStatus `json:"status"`
	Method       string                          `json:"method,omitempty"`
	EvidenceRefs []string                        `json:"evidence_refs,omitempty"`
	VerifiedAt   time.Time                       `json:"verified_at,omitempty"`
}

type ProjectMemory struct {
	ID               string                    `json:"memory_id"`
	LogicalID        string                    `json:"logical_memory_id"`
	ScopeID          string                    `json:"project_scope_id"`
	Kind             ProjectMemoryKind         `json:"kind"`
	SubjectKey       string                    `json:"subject_key"`
	Statement        string                    `json:"statement"`
	Status           ProjectMemoryStatus       `json:"status"`
	Confidence       float64                   `json:"confidence"`
	Locked           bool                      `json:"locked"`
	Sensitive        bool                      `json:"sensitive"`
	ScopePaths       []string                  `json:"scope_paths,omitempty"`
	Artifacts        []ProjectMemoryArtifact   `json:"artifact_refs,omitempty"`
	Sources          []ProjectMemorySource     `json:"source_refs,omitempty"`
	Verification     ProjectMemoryVerification `json:"verification"`
	ContentHash      string                    `json:"content_hash"`
	EmbeddingModel   string                    `json:"embedding_model,omitempty"`
	EmbeddingVersion string                    `json:"embedding_version,omitempty"`
	SupersedesID     string                    `json:"supersedes_memory_id,omitempty"`
	Version          int64                     `json:"version"`
	CreatedBy        string                    `json:"created_by"`
	CreatedAt        time.Time                 `json:"created_at"`
	UpdatedAt        time.Time                 `json:"updated_at"`
}

type ProjectMemoryFilter struct {
	OperatorID      int64
	MachineID       string
	ScopeID         string
	Kind            ProjectMemoryKind
	Kinds           []ProjectMemoryKind
	Status          ProjectMemoryStatus
	Verification    ProjectMemoryVerificationStatus
	Locked          *bool
	Query           string
	RelativePath    string
	ArtifactHash    string
	IncludeDeleted  bool
	Limit           int
	BeforeID        string
	BeforeUpdatedAt time.Time
}

type ProjectMemoryJob struct {
	ID                 string                 `json:"job_id"`
	OperatorID         int64                  `json:"-"`
	MachineID          string                 `json:"machine_id"`
	ScopeID            string                 `json:"project_scope_id"`
	Trigger            ProjectMemoryTrigger   `json:"trigger"`
	Status             ProjectMemoryJobStatus `json:"status"`
	CursorStart        string                 `json:"cursor_start,omitempty"`
	CursorEnd          string                 `json:"cursor_end,omitempty"`
	CursorCommitted    string                 `json:"cursor_committed,omitempty"`
	CheckpointCursor   string                 `json:"checkpoint_cursor,omitempty"`
	CheckpointProgress int                    `json:"checkpoint_progress,omitempty"`
	CheckpointAt       time.Time              `json:"checkpoint_at,omitempty"`
	PendingCursorEnd   string                 `json:"pending_cursor_end,omitempty"`
	Attempt            int                    `json:"attempt"`
	LeaseOwner         string                 `json:"lease_owner,omitempty"`
	LeaseExpiresAt     time.Time              `json:"lease_expires_at,omitempty"`
	FencingToken       int64                  `json:"fencing_token"`
	InputRevision      int64                  `json:"input_revision"`
	ResultRevision     int64                  `json:"result_revision"`
	Error              string                 `json:"error,omitempty"`
	ManualVisible      bool                   `json:"manual_visible"`
	CreatedAt          time.Time              `json:"created_at"`
	UpdatedAt          time.Time              `json:"updated_at"`
}

type ProjectMemoryJobEvent struct {
	ID        int64     `json:"event_id"`
	JobID     string    `json:"job_id"`
	Sequence  int64     `json:"sequence"`
	Type      string    `json:"type"`
	Message   string    `json:"message,omitempty"`
	Progress  int       `json:"progress,omitempty"`
	Metadata  any       `json:"metadata,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type ProjectMemoryBrief struct {
	ScopeID        string    `json:"project_scope_id"`
	Content        string    `json:"content"`
	TokenCount     int       `json:"token_count"`
	SourceRevision int64     `json:"source_revision"`
	Model          string    `json:"model,omitempty"`
	GeneratedAt    time.Time `json:"generated_at"`
}

type ProjectMemoryUsage struct {
	ScopeID    string    `json:"project_scope_id"`
	TaskID     string    `json:"task_id"`
	Revision   int64     `json:"revision"`
	MemoryIDs  []string  `json:"memory_ids,omitempty"`
	TokenCount int       `json:"token_count"`
	CreatedAt  time.Time `json:"created_at"`
}

type ProjectMemoryAudit struct {
	ScopeID    string `json:"project_scope_id"`
	MemoryID   string `json:"memory_id,omitempty"`
	JobID      string `json:"job_id,omitempty"`
	OperatorID int64  `json:"-"`
	Action     string `json:"action"`
	Metadata   any    `json:"metadata,omitempty"`
}

type ProjectMemoryCandidate struct {
	ProjectScopeID string                    `json:"project_scope_id"`
	JobID          string                    `json:"job_id"`
	SubjectKey     string                    `json:"subject_key"`
	Kind           ProjectMemoryKind         `json:"kind"`
	Statement      string                    `json:"statement"`
	Confidence     float64                   `json:"confidence"`
	ScopePaths     []string                  `json:"scope_paths,omitempty"`
	Artifacts      []ProjectMemoryArtifact   `json:"artifact_refs,omitempty"`
	Sources        []ProjectMemorySource     `json:"source_refs,omitempty"`
	Verification   ProjectMemoryVerification `json:"verification"`
	Sensitive      bool                      `json:"sensitive"`
}

type ProjectMemoryBriefCandidate struct {
	Content             string   `json:"content"`
	IncludedSubjectKeys []string `json:"included_subject_keys,omitempty"`
}

type ProjectMemoryInvalidation struct {
	MemoryID     string              `json:"memory_id"`
	ContentHash  string              `json:"content_hash"`
	Status       ProjectMemoryStatus `json:"status"`
	Reason       string              `json:"reason"`
	EvidenceRefs []string            `json:"evidence_refs"`
}

type ProjectMemoryCandidateBatch struct {
	SchemaVersion  int                          `json:"schema_version"`
	ProjectScopeID string                       `json:"project_scope_id"`
	JobID          string                       `json:"job_id"`
	SourceCursor   string                       `json:"source_cursor"`
	Candidates     []ProjectMemoryCandidate     `json:"candidates"`
	Invalidations  []ProjectMemoryInvalidation  `json:"invalidations,omitempty"`
	BriefCandidate *ProjectMemoryBriefCandidate `json:"brief_candidate,omitempty"`
}

type ProjectMemoryReconcileResult struct {
	Revision       int64
	AcceptedIDs    []string
	InvalidatedIDs []string
	Changed        bool
}

type ProjectMemoryRetentionResult struct {
	ScopesProcessed int64 `json:"scopes_processed"`
	Archived        int64 `json:"archived"`
	RolledUp        int64 `json:"rolled_up"`
	PurgedHistory   int64 `json:"purged_history"`
}

type ProjectMemoryRunPayload struct {
	JobID          string               `json:"job_id"`
	ProjectScopeID string               `json:"project_scope_id"`
	ProjectID      string               `json:"project_id"`
	BindingEpoch   int64                `json:"binding_epoch"`
	FencingToken   int64                `json:"fencing_token"`
	InputRevision  int64                `json:"input_revision"`
	CursorStart    string               `json:"cursor_start,omitempty"`
	CursorEnd      string               `json:"cursor_end,omitempty"`
	Trigger        ProjectMemoryTrigger `json:"trigger"`
	Model          string               `json:"model,omitempty"`
	Variant        string               `json:"variant,omitempty"`
	Source         string               `json:"source"`
	CurrentBrief   string               `json:"current_brief,omitempty"`
	CurrentMemory  []ProjectMemory      `json:"current_memory,omitempty"`
}

type ProjectMemoryWorkerPayload struct {
	JobID          string                            `json:"job_id"`
	ProjectScopeID string                            `json:"project_scope_id"`
	BindingEpoch   int64                             `json:"binding_epoch"`
	FencingToken   int64                             `json:"fencing_token"`
	Cursor         string                            `json:"cursor,omitempty"`
	Progress       int                               `json:"progress,omitempty"`
	Message        string                            `json:"message,omitempty"`
	Error          string                            `json:"error,omitempty"`
	Batch          *ProjectMemoryCandidateBatch      `json:"batch,omitempty"`
	SourceUpdates  []ProjectMemorySourceAvailability `json:"source_updates,omitempty"`
}
