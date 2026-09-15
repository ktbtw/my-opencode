package model

import "time"

type TaskStatus string

const (
	TaskPending         TaskStatus = "pending"
	TaskDispatched      TaskStatus = "dispatched"
	TaskRunning         TaskStatus = "running"
	TaskCancelling      TaskStatus = "cancelling"
	TaskWaitingApproval TaskStatus = "waiting_approval"
	TaskCompleted       TaskStatus = "completed"
	TaskFailed          TaskStatus = "failed"
	TaskCancelled       TaskStatus = "cancelled"
)

type Task struct {
	ID          string            `json:"task_id"`
	AgentID     string            `json:"agent_id"`
	MachineID   string            `json:"machine_id,omitempty"`
	ProjectID   string            `json:"project_id"`
	ProjectRoot string            `json:"project_root,omitempty"`
	SessionID   string            `json:"session_id,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
	OperatorID  int64             `json:"-"`
	Approval    *Approval         `json:"approval,omitempty"`
	Question    *QuestionRequest  `json:"question,omitempty"`
	Plan        *Plan             `json:"plan,omitempty"`
	Parts       []Part            `json:"parts,omitempty"`
	Artifacts   []Artifact        `json:"artifacts,omitempty"`
	Status      TaskStatus        `json:"status"`
	Result      string            `json:"result,omitempty"`
	Error       string            `json:"error,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

type TaskFilter struct {
	OperatorID         int64
	AgentID            string
	MachineID          string
	ProjectID          string
	SessionID          string
	Status             TaskStatus
	Limit              int
	BeforeCreatedAt    time.Time
	BeforeTaskID       string
	AfterCreatedAt     time.Time
	AfterTaskID        string
	AfterUpdatedAt     time.Time
	AfterUpdatedTaskID string
	CreatedDesc        bool
	CreatedAsc         bool
}

func TaskStatusPriority(status TaskStatus) int {
	switch status {
	case TaskRunning:
		return 0
	case TaskPending, TaskDispatched, TaskCancelling, TaskWaitingApproval:
		return 1
	case TaskFailed:
		return 2
	case TaskCompleted, TaskCancelled:
		return 3
	default:
		return 4
	}
}

type Session struct {
	ID         string    `json:"session_id"`
	AgentID    string    `json:"agent_id"`
	MachineID  string    `json:"machine_id"`
	ProjectID  string    `json:"project_id"`
	Status     string    `json:"status"`
	LastTaskID string    `json:"last_task_id,omitempty"`
	Summary    string    `json:"summary,omitempty"`
	OperatorID int64     `json:"-"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type SessionFilter struct {
	OperatorID int64
	AgentID    string
	MachineID  string
	ProjectID  string
	Status     string
	Limit      int
}

type ChatQueueItemStatus string

const (
	ChatQueueQueued      ChatQueueItemStatus = "queued"
	ChatQueueDispatching ChatQueueItemStatus = "dispatching"
	ChatQueueDispatched  ChatQueueItemStatus = "dispatched"
	ChatQueueInserting   ChatQueueItemStatus = "inserting"
)

type ChatQueueItem struct {
	ID               string              `json:"queue_item_id"`
	OperatorID       int64               `json:"-"`
	SessionID        string              `json:"session_id"`
	AgentID          string              `json:"agent_id"`
	MachineID        string              `json:"machine_id,omitempty"`
	ProjectID        string              `json:"project_id"`
	ProjectRoot      string              `json:"project_root,omitempty"`
	Parts            []Part              `json:"parts"`
	Metadata         map[string]string   `json:"metadata,omitempty"`
	Position         int64               `json:"position"`
	Status           ChatQueueItemStatus `json:"status"`
	Version          int64               `json:"version"`
	TaskID           string              `json:"task_id,omitempty"`
	InjectionVersion int64               `json:"injection_version,omitempty"`
	Error            string              `json:"error,omitempty"`
	CreatedAt        time.Time           `json:"created_at"`
	UpdatedAt        time.Time           `json:"updated_at"`
}

type ChatQueueSnapshot struct {
	SessionID string          `json:"session_id"`
	Version   int64           `json:"version"`
	Items     []ChatQueueItem `json:"items"`
}

type Approval struct {
	PermissionID string   `json:"permission_id"`
	Permission   string   `json:"permission"`
	Patterns     []string `json:"patterns,omitempty"`
	Metadata     any      `json:"metadata,omitempty"`
}

type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type QuestionItem struct {
	Question string           `json:"question"`
	Header   string           `json:"header,omitempty"`
	Options  []QuestionOption `json:"options,omitempty"`
	Multiple bool             `json:"multiple,omitempty"`
	Custom   bool             `json:"custom"`
}

type QuestionRequest struct {
	RequestID string         `json:"request_id"`
	SessionID string         `json:"session_id,omitempty"`
	Questions []QuestionItem `json:"questions,omitempty"`
}

type PlanItem struct {
	ID       string `json:"id"`
	Text     string `json:"text"`
	Status   string `json:"status"`
	Priority string `json:"priority,omitempty"`
}

type Plan struct {
	ID        string     `json:"id"`
	Title     string     `json:"title"`
	Mode      string     `json:"mode,omitempty"`
	Status    string     `json:"status,omitempty"`
	Items     []PlanItem `json:"items,omitempty"`
	SessionID string     `json:"session_id,omitempty"`
}

type Part struct {
	Type        string     `json:"type"`
	Text        string     `json:"text,omitempty"`
	MIME        string     `json:"mime,omitempty"`
	Filename    string     `json:"filename,omitempty"`
	URL         string     `json:"url,omitempty"`
	Source      any        `json:"source,omitempty"`
	Name        string     `json:"name,omitempty"`
	Prompt      string     `json:"prompt,omitempty"`
	Description string     `json:"description,omitempty"`
	Agent       string     `json:"agent,omitempty"`
	Model       *PartModel `json:"model,omitempty"`
	Command     string     `json:"command,omitempty"`
}

type PartModel struct {
	ProviderID string `json:"providerID"`
	ModelID    string `json:"modelID"`
}

type ToolCall struct {
	ID              string         `json:"id"`
	CallID          string         `json:"call_id"`
	Tool            string         `json:"tool"`
	Status          string         `json:"status"`
	Input           map[string]any `json:"input,omitempty"`
	Title           string         `json:"title,omitempty"`
	Output          string         `json:"output,omitempty"`
	OutputTruncated bool           `json:"output_truncated,omitempty"`
	Error           string         `json:"error,omitempty"`
	Metadata        any            `json:"metadata,omitempty"`
	StartedAt       int64          `json:"started_at,omitempty"`
	EndedAt         int64          `json:"ended_at,omitempty"`
	Attachments     []Part         `json:"attachments,omitempty"`
}

type Event struct {
	TaskID string `json:"task_id"`
	// ID is the durable task_events row id. It is populated on archive reads
	// and is intentionally optional for live events emitted before persistence.
	ID           int64            `json:"id,omitempty"`
	Sequence     int64            `json:"sequence,omitempty"`
	Type         string           `json:"type"`
	Content      string           `json:"content,omitempty"`
	Field        string           `json:"field,omitempty"`
	Attempt      int              `json:"attempt,omitempty"`
	Next         int64            `json:"next,omitempty"`
	Artifacts    []Artifact       `json:"artifacts,omitempty"`
	Files        []Part           `json:"files,omitempty"`
	SessionID    string           `json:"session_id,omitempty"`
	PermissionID string           `json:"permission_id,omitempty"`
	Permission   string           `json:"permission,omitempty"`
	Patterns     []string         `json:"patterns,omitempty"`
	Reply        string           `json:"reply,omitempty"`
	Question     *QuestionRequest `json:"question,omitempty"`
	Plan         *Plan            `json:"plan,omitempty"`
	Tool         *ToolCall        `json:"tool,omitempty"`
	Metadata     any              `json:"metadata,omitempty"`
	Error        string           `json:"error,omitempty"`
	SentAt       time.Time        `json:"sent_at"`
}

type Operator struct {
	ID          int64  `json:"id"`
	OperatorUID string `json:"operator_uid"`
	Username    string `json:"username"`
	Name        string `json:"name"`
	Email       string `json:"email,omitempty"`
	OperatorKey string `json:"operator_key,omitempty"`
	// MembershipTier 会员等级：free（普通） / plus / pro。空值按 free 处理。
	MembershipTier string `json:"membership_tier"`
}

type PushDevice struct {
	ID             int64     `json:"id"`
	OperatorID     int64     `json:"-"`
	Platform       string    `json:"platform"`
	Vendor         string    `json:"vendor"`
	RegistrationID string    `json:"registration_id"`
	DeviceID       string    `json:"device_id,omitempty"`
	DeviceBrand    string    `json:"device_brand,omitempty"`
	DeviceModel    string    `json:"device_model,omitempty"`
	AppVersion     string    `json:"app_version,omitempty"`
	Enabled        bool      `json:"enabled"`
	LastSeenAt     time.Time `json:"last_seen_at"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type Envelope struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	SentAt    string `json:"sent_at"`
	Payload   any    `json:"payload"`
}

type SessionHistoryRequestPayload struct {
	TaskID          string `json:"task_id"`
	SessionID       string `json:"session_id"`
	AgentID         string `json:"agent_id,omitempty"`
	MachineID       string `json:"machine_id,omitempty"`
	ProjectID       string `json:"project_id,omitempty"`
	Cursor          string `json:"cursor,omitempty"`
	Limit           int    `json:"limit,omitempty"`
	IncludeChildren bool   `json:"include_children"`
}

type SessionHistoryResultPayload struct {
	TaskID              string  `json:"task_id"`
	SessionID           string  `json:"session_id"`
	Source              string  `json:"source"`
	Events              []Event `json:"events"`
	NextCursor          string  `json:"next_cursor,omitempty"`
	HasMore             bool    `json:"has_more"`
	Complete            bool    `json:"complete"`
	ErrorCode           string  `json:"error_code,omitempty"`
	Error               string  `json:"error,omitempty"`
	SourceSequenceStart int64   `json:"source_sequence_start,omitempty"`
	SourceSequenceEnd   int64   `json:"source_sequence_end,omitempty"`
}

type Agent struct {
	ID                string         `json:"agent_id"`
	OperatorID        int64          `json:"operator_id,omitempty"`
	MachineID         string         `json:"machine_id"`
	Hostname          string         `json:"hostname"`
	Name              string         `json:"name,omitempty"`
	ProjectDir        string         `json:"project_dir,omitempty"`
	SemanticAgentID   string         `json:"semantic_agent_id,omitempty"`
	SemanticAgentName string         `json:"semantic_agent_name,omitempty"`
	Version           string         `json:"version"`
	Enabled           bool           `json:"enabled"`
	Status            string         `json:"status,omitempty"`
	Kind              string         `json:"kind,omitempty"`
	Projects          []HelloProject `json:"projects,omitempty"`
	Capabilities      []string       `json:"capabilities,omitempty"`
	SeenAt            time.Time      `json:"seen_at"`
	CurrentTask       string         `json:"current_task_id,omitempty"`
}

type Machine struct {
	MachineID      string    `json:"machine_id"`
	Hostname       string    `json:"hostname"`
	DisplayName    string    `json:"display_name,omitempty"`
	SortOrder      int64     `json:"sort_order,omitempty"`
	Status         string    `json:"status"`
	SeenAt         time.Time `json:"seen_at"`
	LauncherOnline bool      `json:"launcher_online,omitempty"`
	Agents         []Agent   `json:"agents,omitempty"`
}

type DevicePreference struct {
	OperatorID  int64     `json:"-"`
	MachineID   string    `json:"machine_id"`
	DisplayName string    `json:"display_name,omitempty"`
	SortOrder   int64     `json:"sort_order"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type AgentPreference struct {
	OperatorID int64     `json:"-"`
	MachineID  string    `json:"machine_id"`
	AgentID    string    `json:"agent_id"`
	SortOrder  int64     `json:"sort_order"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type HelloProject struct {
	ProjectID      string `json:"project_id"`
	Root           string `json:"root"`
	ScopeID        string `json:"project_scope_id,omitempty"`
	DisplayName    string `json:"display_name,omitempty"`
	VolumeID       string `json:"filesystem_volume_id,omitempty"`
	FileID         string `json:"filesystem_file_id,omitempty"`
	InstanceNonce  string `json:"instance_nonce,omitempty"`
	LineageScopeID string `json:"lineage_project_scope_id,omitempty"`
	BindingEpoch   int64  `json:"binding_epoch,omitempty"`
}

type HelloPayload struct {
	AgentID       string         `json:"agent_id,omitempty"`
	MachineID     string         `json:"machine_id,omitempty"`
	DeviceID      string         `json:"device_id,omitempty"`
	Kind          string         `json:"kind,omitempty"`
	OperatorKey   string         `json:"operator_key,omitempty"`
	Hostname      string         `json:"hostname"`
	Version       string         `json:"version"`
	Projects      []HelloProject `json:"projects"`
	RunningTaskID string         `json:"running_task_id,omitempty"`
	Capabilities  []string       `json:"capabilities,omitempty"`
}

type WelcomePayload struct {
	AgentID              string         `json:"agent_id"`
	HeartbeatIntervalSec int            `json:"heartbeat_interval_sec"`
	Projects             []HelloProject `json:"projects,omitempty"`
}

type HeartbeatPayload struct {
	AgentID       string `json:"agent_id,omitempty"`
	RunningTaskID string `json:"running_task_id,omitempty"`
}

type RunPayload struct {
	TaskID    string                 `json:"task_id"`
	AgentID   string                 `json:"agent_id"`
	MachineID string                 `json:"machine_id,omitempty"`
	ProjectID string                 `json:"project_id"`
	SessionID string                 `json:"session_id,omitempty"`
	System    string                 `json:"system,omitempty"`
	Parts     []Part                 `json:"parts,omitempty"`
	Metadata  map[string]string      `json:"metadata,omitempty"`
	Resume    bool                   `json:"resume,omitempty"`
	Subagents []SubagentNodeSnapshot `json:"subagents,omitempty"`
}

type GoalOptimizePayload struct {
	AgentID       string `json:"agent_id,omitempty"`
	MachineID     string `json:"machine_id,omitempty"`
	ProjectID     string `json:"project_id,omitempty"`
	Goal          string `json:"goal,omitempty"`
	ProviderID    string `json:"provider_id,omitempty"`
	ModelID       string `json:"model_id,omitempty"`
	Variant       string `json:"variant,omitempty"`
	MaxIterations int    `json:"max_iterations,omitempty"`
}

type GoalOptimizeResultPayload struct {
	AgentID       string `json:"agent_id,omitempty"`
	MachineID     string `json:"machine_id,omitempty"`
	ProjectID     string `json:"project_id,omitempty"`
	OriginalGoal  string `json:"original_goal,omitempty"`
	OptimizedGoal string `json:"optimized_goal,omitempty"`
	Success       bool   `json:"success"`
	Error         string `json:"error,omitempty"`
}

type ModelTestRunPayload struct {
	TestID     string `json:"test_id"`
	AgentID    string `json:"agent_id,omitempty"`
	MachineID  string `json:"machine_id,omitempty"`
	ProjectID  string `json:"project_id,omitempty"`
	ProviderID string `json:"provider_id,omitempty"`
	ModelID    string `json:"model_id,omitempty"`
	Model      string `json:"model,omitempty"`
	Variant    string `json:"variant,omitempty"`
	Prompt     string `json:"prompt,omitempty"`
}

type ModelTestEventPayload struct {
	TestID          string         `json:"test_id"`
	AgentID         string         `json:"agent_id,omitempty"`
	MachineID       string         `json:"machine_id,omitempty"`
	ProjectID       string         `json:"project_id,omitempty"`
	SessionID       string         `json:"session_id,omitempty"`
	ProviderID      string         `json:"provider_id,omitempty"`
	ModelID         string         `json:"model_id,omitempty"`
	Model           string         `json:"model,omitempty"`
	Variant         string         `json:"variant,omitempty"`
	Stage           string         `json:"stage,omitempty"`
	Content         string         `json:"content,omitempty"`
	Field           string         `json:"field,omitempty"`
	Error           string         `json:"error,omitempty"`
	ErrorDetail     string         `json:"error_detail,omitempty"`
	ElapsedMS       int64          `json:"elapsed_ms,omitempty"`
	StepMS          int64          `json:"step_ms,omitempty"`
	TextLength      int            `json:"text_length,omitempty"`
	ReasoningLength int            `json:"reasoning_length,omitempty"`
	TokenCount      int            `json:"token_count,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
}

type StartedPayload struct {
	TaskID    string `json:"task_id"`
	SessionID string `json:"session_id,omitempty"`
}

type DeltaPayload struct {
	TaskID  string `json:"task_id"`
	Content string `json:"content"`
	Field   string `json:"field,omitempty"`
}

type ProgressPayload struct {
	TaskID    string         `json:"task_id"`
	SessionID string         `json:"session_id,omitempty"`
	Message   string         `json:"message,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type Artifact struct {
	ID           string `json:"id"`
	Filename     string `json:"filename"`
	MIME         string `json:"mime,omitempty"`
	SizeBytes    int64  `json:"size_bytes,omitempty"`
	RelativePath string `json:"relative_path,omitempty"`
}

type CompletedPayload struct {
	TaskID      string         `json:"task_id"`
	SessionID   string         `json:"session_id,omitempty"`
	Result      string         `json:"result"`
	RoundResult string         `json:"round_result,omitempty"`
	Artifacts   []Artifact     `json:"artifacts,omitempty"`
	Files       []Part         `json:"files,omitempty"`
	Usage       map[string]any `json:"usage,omitempty"`
}

type FailedPayload struct {
	TaskID      string `json:"task_id"`
	SessionID   string `json:"session_id,omitempty"`
	Error       string `json:"error"`
	ErrorDetail string `json:"error_detail,omitempty"`
}

type RetryingPayload struct {
	TaskID    string `json:"task_id"`
	SessionID string `json:"session_id,omitempty"`
	Attempt   int    `json:"attempt,omitempty"`
	Message   string `json:"message,omitempty"`
	Next      int64  `json:"next,omitempty"`
}

type CompactionPayload struct {
	TaskID    string `json:"task_id"`
	SessionID string `json:"session_id,omitempty"`
	Reason    string `json:"reason,omitempty"`
}

type ToolUpdatedPayload struct {
	TaskID    string    `json:"task_id"`
	SessionID string    `json:"session_id,omitempty"`
	Tool      *ToolCall `json:"tool,omitempty"`
}

type WaitingApprovalPayload struct {
	TaskID       string   `json:"task_id"`
	SessionID    string   `json:"session_id,omitempty"`
	PermissionID string   `json:"permission_id"`
	Permission   string   `json:"permission"`
	Patterns     []string `json:"patterns,omitempty"`
	Metadata     any      `json:"metadata,omitempty"`
}

type ApprovalAppliedPayload struct {
	TaskID       string `json:"task_id"`
	SessionID    string `json:"session_id,omitempty"`
	PermissionID string `json:"permission_id"`
	Reply        string `json:"reply"`
}

type ApprovalAutoApprovedPayload struct {
	TaskID       string   `json:"task_id"`
	SessionID    string   `json:"session_id,omitempty"`
	PermissionID string   `json:"permission_id"`
	Permission   string   `json:"permission"`
	Patterns     []string `json:"patterns,omitempty"`
}

type ApprovalResponsePayload struct {
	TaskID       string `json:"task_id"`
	PermissionID string `json:"permission_id"`
	Reply        string `json:"reply"`
	Message      string `json:"message,omitempty"`
}

type QuestionAskedPayload struct {
	TaskID    string         `json:"task_id"`
	SessionID string         `json:"session_id,omitempty"`
	RequestID string         `json:"request_id"`
	Questions []QuestionItem `json:"questions,omitempty"`
}

type QuestionRepliedPayload struct {
	TaskID    string         `json:"task_id"`
	SessionID string         `json:"session_id,omitempty"`
	RequestID string         `json:"request_id"`
	Questions []QuestionItem `json:"questions,omitempty"`
	Answers   [][]string     `json:"answers,omitempty"`
}

type QuestionRejectedPayload struct {
	TaskID    string         `json:"task_id"`
	SessionID string         `json:"session_id,omitempty"`
	RequestID string         `json:"request_id"`
	Questions []QuestionItem `json:"questions,omitempty"`
}

type PlanUpdatedPayload struct {
	TaskID    string `json:"task_id"`
	SessionID string `json:"session_id,omitempty"`
	Plan      *Plan  `json:"plan,omitempty"`
}

type GoalEventPayload struct {
	TaskID    string         `json:"task_id"`
	SessionID string         `json:"session_id,omitempty"`
	GoalID    string         `json:"goal_id,omitempty"`
	Objective string         `json:"objective,omitempty"`
	Status    string         `json:"status,omitempty"`
	Reason    string         `json:"reason,omitempty"`
	Iteration int            `json:"iteration,omitempty"`
	Max       int            `json:"max,omitempty"`
	Metadata  map[string]any `json:"metadata,omitempty"`
}

type QuestionResponsePayload struct {
	TaskID    string     `json:"task_id"`
	RequestID string     `json:"request_id"`
	Answers   [][]string `json:"answers,omitempty"`
	Rejected  bool       `json:"rejected,omitempty"`
}

type CancelPayload struct {
	TaskID string `json:"task_id"`
}

type TaskInputPayload struct {
	TaskID           string            `json:"task_id"`
	QueueItemID      string            `json:"queue_item_id"`
	InjectionVersion int64             `json:"injection_version"`
	Parts            []Part            `json:"parts"`
	Metadata         map[string]string `json:"metadata,omitempty"`
}

type TaskInputAckPayload struct {
	TaskID           string `json:"task_id"`
	QueueItemID      string `json:"queue_item_id"`
	InjectionVersion int64  `json:"injection_version"`
	Accepted         bool   `json:"accepted"`
	Error            string `json:"error,omitempty"`
}

// TaskInputAppliedPayload records a context item that Relay appended directly
// to an active task, such as a completed background job.
type TaskInputAppliedPayload struct {
	TaskID           string         `json:"task_id"`
	SessionID        string         `json:"session_id"`
	QueueItemID      string         `json:"queue_item_id"`
	InjectionVersion int64          `json:"injection_version"`
	Content          string         `json:"content"`
	Metadata         map[string]any `json:"metadata,omitempty"`
}

// SubagentStartedPayload and SubagentResultPayload are durable task graph
// events emitted by Relay. NodeID is stable within a plan; ChildSessionID is
// the ephemeral OpenCode session used to execute that node.
type SubagentStartedPayload struct {
	TaskID         string `json:"task_id"`
	SessionID      string `json:"session_id,omitempty"`
	NodeID         string `json:"node_id"`
	PlanID         string `json:"plan_id,omitempty"`
	ChildSessionID string `json:"child_session_id,omitempty"`
	Attempt        int    `json:"attempt,omitempty"`
	SubagentType   string `json:"subagent_type,omitempty"`
	ResolvedAgent  string `json:"resolved_agent,omitempty"`
	Title          string `json:"title,omitempty"`
	Background     bool   `json:"background,omitempty"`
	StartedAt      int64  `json:"started_at,omitempty"`
}

type SubagentResultPayload struct {
	TaskID         string     `json:"task_id"`
	SessionID      string     `json:"session_id,omitempty"`
	NodeID         string     `json:"node_id"`
	PlanID         string     `json:"plan_id,omitempty"`
	ChildSessionID string     `json:"child_session_id,omitempty"`
	Attempt        int        `json:"attempt,omitempty"`
	SubagentType   string     `json:"subagent_type,omitempty"`
	ResolvedAgent  string     `json:"resolved_agent,omitempty"`
	Title          string     `json:"title,omitempty"`
	Status         string     `json:"status"`
	Output         string     `json:"output,omitempty"`
	Error          string     `json:"error,omitempty"`
	Artifacts      []Artifact `json:"artifacts,omitempty"`
	CompletedAt    int64      `json:"completed_at,omitempty"`
	WakeReason     string     `json:"wake_reason,omitempty"`
}

type SubagentStatePayload struct {
	TaskID              string         `json:"task_id"`
	SessionID           string         `json:"session_id,omitempty"`
	PlanID              string         `json:"plan_id,omitempty"`
	NodeID              string         `json:"node_id"`
	ChildSessionID      string         `json:"child_session_id,omitempty"`
	SubagentType        string         `json:"subagent_type,omitempty"`
	Title               string         `json:"title,omitempty"`
	Prompt              string         `json:"prompt,omitempty"`
	State               string         `json:"state"`
	Attempt             int            `json:"attempt,omitempty"`
	DependsOn           []string       `json:"depends_on,omitempty"`
	BlockedBy           []string       `json:"blocked_by,omitempty"`
	Priority            *int           `json:"priority,omitempty"`
	Model               map[string]any `json:"model,omitempty"`
	PendingInstructions []string       `json:"pending_instructions,omitempty"`
	StartedAt           int64          `json:"started_at,omitempty"`
	CompletedAt         int64          `json:"completed_at,omitempty"`
	Error               string         `json:"error,omitempty"`
	UpdatedAt           int64          `json:"updated_at,omitempty"`
}

type SubagentControlPayload struct {
	TaskID      string         `json:"task_id"`
	NodeID      string         `json:"node_id"`
	Action      string         `json:"action"`
	Applied     bool           `json:"applied"`
	Error       string         `json:"error,omitempty"`
	Instruction string         `json:"instruction,omitempty"`
	Priority    *int           `json:"priority,omitempty"`
	Model       map[string]any `json:"model,omitempty"`
}

type SubagentNodeSnapshot struct {
	NodeID              string         `json:"node_id"`
	PlanID              string         `json:"plan_id,omitempty"`
	SessionID           string         `json:"session_id,omitempty"`
	ChildSessionID      string         `json:"child_session_id,omitempty"`
	Role                string         `json:"role,omitempty"`
	ResolvedAgent       string         `json:"resolved_agent,omitempty"`
	Title               string         `json:"title,omitempty"`
	Prompt              string         `json:"prompt,omitempty"`
	State               string         `json:"state"`
	Attempt             int            `json:"attempt,omitempty"`
	DependsOn           []string       `json:"depends_on,omitempty"`
	BlockedBy           []string       `json:"blocked_by,omitempty"`
	Priority            *int           `json:"priority,omitempty"`
	Model               map[string]any `json:"model,omitempty"`
	PendingInstructions []string       `json:"pending_instructions,omitempty"`
	Background          bool           `json:"background,omitempty"`
	StartedAt           int64          `json:"started_at,omitempty"`
	CompletedAt         int64          `json:"completed_at,omitempty"`
	Output              string         `json:"output,omitempty"`
	Artifacts           []Artifact     `json:"artifacts,omitempty"`
	Error               string         `json:"error,omitempty"`
	WakeReason          string         `json:"wake_reason,omitempty"`
	LastControl         map[string]any `json:"last_control,omitempty"`
}

type ArtifactFetchPayload struct {
	TaskID       string `json:"task_id"`
	ArtifactID   string `json:"artifact_id"`
	RelativePath string `json:"relative_path"`
}

type ArtifactChunkPayload struct {
	TaskID     string `json:"task_id"`
	ArtifactID string `json:"artifact_id"`
	Seq        int    `json:"seq"`
	Data       string `json:"data"`
}

type ArtifactDonePayload struct {
	TaskID     string `json:"task_id"`
	ArtifactID string `json:"artifact_id"`
}

type ArtifactFailedPayload struct {
	TaskID     string `json:"task_id"`
	ArtifactID string `json:"artifact_id"`
	Error      string `json:"error"`
}

type DeviceAIConfig struct {
	Provider   string          `json:"provider"`
	BaseURL    string          `json:"base_url,omitempty"`
	ConsoleURL string          `json:"console_url,omitempty"`
	APIKey     string          `json:"api_key,omitempty"`
	APIMode    string          `json:"api_mode,omitempty"`
	Model      string          `json:"model,omitempty"`
	Models     []DeviceAIModel `json:"models,omitempty"`
}

type DeviceAIModalities struct {
	Input  []string `json:"input,omitempty"`
	Output []string `json:"output,omitempty"`
}

type DeviceAIThinking struct {
	Supported           bool           `json:"supported"`
	Source              string         `json:"source,omitempty"`
	Control             string         `json:"control,omitempty"`
	Protocol            string         `json:"protocol,omitempty"`
	SupportedParameters []string       `json:"supported_parameters,omitempty"`
	OverrideEnabled     bool           `json:"override_enabled,omitempty"`
	Variants            map[string]any `json:"variants,omitempty"`
	OverrideVariants    map[string]any `json:"override_variants,omitempty"`
}

type DeviceAIModel struct {
	ID         string              `json:"id"`
	Name       string              `json:"name,omitempty"`
	Owned      string              `json:"owned_by,omitempty"`
	Modalities *DeviceAIModalities `json:"modalities,omitempty"`
	Context    int64               `json:"context_limit,omitempty"`
	Variants   map[string]any      `json:"variants,omitempty"`
	Thinking   *DeviceAIThinking   `json:"thinking,omitempty"`
}

type DeviceAIProvider struct {
	ID           string          `json:"id"`
	BaseURL      string          `json:"base_url,omitempty"`
	ConsoleURL   string          `json:"console_url,omitempty"`
	APIKeyMasked string          `json:"api_key_masked,omitempty"`
	APIMode      string          `json:"api_mode,omitempty"`
	Models       []DeviceAIModel `json:"models,omitempty"`
}

type DeviceAIConfigPreview struct {
	Exists       bool               `json:"exists"`
	ConfigPath   string             `json:"config_path,omitempty"`
	Provider     string             `json:"provider,omitempty"`
	BaseURL      string             `json:"base_url,omitempty"`
	ConsoleURL   string             `json:"console_url,omitempty"`
	APIKeyMasked string             `json:"api_key_masked,omitempty"`
	APIMode      string             `json:"api_mode,omitempty"`
	Model        string             `json:"model,omitempty"`
	Models       []DeviceAIModel    `json:"models,omitempty"`
	Providers    []DeviceAIProvider `json:"providers,omitempty"`
	RawJSON      string             `json:"raw_json,omitempty"`
	PreviewJSON  string             `json:"preview_json,omitempty"`
	ChangedKeys  []string           `json:"changed_keys,omitempty"`
	Warning      string             `json:"warning,omitempty"`
}

type DeviceAIConfigGetPayload struct {
	AgentID string `json:"agent_id"`
}

type DeviceAIConfigListModelsPayload struct {
	AgentID    string         `json:"agent_id"`
	Provider   string         `json:"provider"`
	BaseURL    string         `json:"base_url,omitempty"`
	ConsoleURL string         `json:"console_url,omitempty"`
	APIKey     string         `json:"api_key,omitempty"`
	APIMode    string         `json:"api_mode,omitempty"`
	Config     DeviceAIConfig `json:"config"`
}

type DeviceAIConfigSavePayload struct {
	AgentID    string         `json:"agent_id"`
	Provider   string         `json:"provider"`
	BaseURL    string         `json:"base_url,omitempty"`
	ConsoleURL string         `json:"console_url,omitempty"`
	APIKey     string         `json:"api_key,omitempty"`
	APIMode    string         `json:"api_mode,omitempty"`
	Force      bool           `json:"force,omitempty"`
	Config     DeviceAIConfig `json:"config"`
}

type DeviceAIConfigTextSavePayload struct {
	AgentID string `json:"agent_id"`
	Text    string `json:"text"`
}

type DeviceAIConfigProviderCleanupPayload struct {
	AgentID  string `json:"agent_id"`
	Provider string `json:"provider"`
}

type DeviceAIConfigResultPayload struct {
	AgentID   string                 `json:"agent_id,omitempty"`
	MachineID string                 `json:"machine_id,omitempty"`
	Action    string                 `json:"action"`
	Config    *DeviceAIConfigPreview `json:"config,omitempty"`
	Success   bool                   `json:"success"`
	Error     string                 `json:"error,omitempty"`
}

type DeviceMCPServer struct {
	Name                    string            `json:"name"`
	Type                    string            `json:"type"`
	Enabled                 bool              `json:"enabled"`
	Timeout                 int               `json:"timeout,omitempty"`
	ConnectTimeout          int               `json:"connect_timeout,omitempty"`
	DiscoveryTimeout        int               `json:"discovery_timeout,omitempty"`
	ToolTimeout             int               `json:"tool_timeout,omitempty"`
	AsyncTools              []string          `json:"async_tools,omitempty"`
	URL                     string            `json:"url,omitempty"`
	Headers                 map[string]string `json:"headers,omitempty"`
	Command                 []string          `json:"command,omitempty"`
	Environment             map[string]string `json:"environment,omitempty"`
	OAuthMode               string            `json:"oauth_mode,omitempty"`
	OAuthClientID           string            `json:"oauth_client_id,omitempty"`
	OAuthClientSecret       string            `json:"oauth_client_secret,omitempty"`
	OAuthClientSecretMasked string            `json:"oauth_client_secret_masked,omitempty"`
	OAuthScope              string            `json:"oauth_scope,omitempty"`
}

type SkillPackageFile struct {
	Path       string `json:"path"`
	Content    string `json:"content,omitempty"`
	Executable bool   `json:"executable,omitempty"`
}

type Skill struct {
	ID           string             `json:"id"`
	Name         string             `json:"name"`
	Description  string             `json:"description,omitempty"`
	Content      string             `json:"content,omitempty"`
	PackageFiles []SkillPackageFile `json:"package_files,omitempty"`
	Category     string             `json:"category,omitempty"`
	Source       string             `json:"source,omitempty"`
	Enabled      bool               `json:"enabled"`
	Tags         []string           `json:"tags,omitempty"`
	SortOrder    int                `json:"sort_order,omitempty"`
	BuiltIn      bool               `json:"built_in,omitempty"`
	CreatedAt    time.Time          `json:"created_at,omitempty"`
	UpdatedAt    time.Time          `json:"updated_at,omitempty"`
}

type MCPCatalogItem struct {
	ID                string          `json:"id"`
	Name              string          `json:"name"`
	Title             string          `json:"title,omitempty"`
	Description       string          `json:"description,omitempty"`
	Type              string          `json:"type,omitempty"`
	Source            string          `json:"source,omitempty"`
	SourceURL         string          `json:"source_url,omitempty"`
	CredentialURL     string          `json:"credential_url,omitempty"`
	Category          string          `json:"category,omitempty"`
	Recommended       bool            `json:"recommended"`
	Enabled           bool            `json:"enabled"`
	Tags              []string        `json:"tags,omitempty"`
	SortOrder         int             `json:"sort_order,omitempty"`
	BuiltIn           bool            `json:"built_in,omitempty"`
	Config            DeviceMCPServer `json:"config"`
	Install           MCPInstallInfo  `json:"install,omitempty"`
	LaunchReady       bool            `json:"launch_ready"`
	LaunchBlockReason string          `json:"launch_block_reason,omitempty"`
	CreatedAt         time.Time       `json:"created_at,omitempty"`
	UpdatedAt         time.Time       `json:"updated_at,omitempty"`
}

type MCPInstallInfo struct {
	PackageManager     string            `json:"package_manager,omitempty"`
	SourceURL          string            `json:"source_url,omitempty"`
	Assets             []MCPInstallAsset `json:"assets,omitempty"`
	Files              []MCPInstallFile  `json:"files,omitempty"`
	InstallCommands    []string          `json:"install_commands,omitempty"`
	BuildCommands      []string          `json:"build_commands,omitempty"`
	RunCommandTemplate []string          `json:"run_command_template,omitempty"`
	ConfigPathTemplate string            `json:"config_path_template,omitempty"`
	ExecutableNames    []string          `json:"executable_names,omitempty"`
	Environment        map[string]string `json:"environment,omitempty"`
	InstallNotes       string            `json:"install_notes,omitempty"`
}

type MCPInstallAsset struct {
	Name        string            `json:"name,omitempty"`
	URL         string            `json:"url,omitempty"`
	Filename    string            `json:"filename,omitempty"`
	SHA256      string            `json:"sha256,omitempty"`
	Extract     bool              `json:"extract,omitempty"`
	PackageKind string            `json:"package_kind,omitempty"`
	TargetDir   string            `json:"target_dir,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
}

type MCPInstallFile struct {
	Path    string `json:"path,omitempty"`
	Content string `json:"content,omitempty"`
	Mode    int    `json:"mode,omitempty"`
}

type SemanticAgentMCPDependency struct {
	ID                string          `json:"id"`
	Name              string          `json:"name"`
	Title             string          `json:"title,omitempty"`
	Type              string          `json:"type,omitempty"`
	SourceURL         string          `json:"source_url,omitempty"`
	Config            DeviceMCPServer `json:"config"`
	Install           MCPInstallInfo  `json:"install,omitempty"`
	LaunchReady       bool            `json:"launch_ready"`
	LaunchBlockReason string          `json:"launch_block_reason,omitempty"`
}

type EnvironmentPreset struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Category    string            `json:"category,omitempty"`
	Source      string            `json:"source,omitempty"`
	Enabled     bool              `json:"enabled"`
	Tags        []string          `json:"tags,omitempty"`
	Variables   map[string]string `json:"variables,omitempty"`
	SortOrder   int               `json:"sort_order,omitempty"`
	BuiltIn     bool              `json:"built_in,omitempty"`
	CreatedAt   time.Time         `json:"created_at,omitempty"`
	UpdatedAt   time.Time         `json:"updated_at,omitempty"`
}

type RuntimeCatalogItem struct {
	ID                       string            `json:"id"`
	Name                     string            `json:"name"`
	Description              string            `json:"description,omitempty"`
	RuntimeKind              string            `json:"runtime_kind,omitempty"`
	DefaultVersionConstraint string            `json:"default_version_constraint,omitempty"`
	ExecutableNames          []string          `json:"executable_names,omitempty"`
	EnvTemplate              map[string]string `json:"env_template,omitempty"`
	InstallStrategy          string            `json:"install_strategy,omitempty"`
	Enabled                  bool              `json:"enabled"`
	Tags                     []string          `json:"tags,omitempty"`
	SortOrder                int               `json:"sort_order,omitempty"`
	BuiltIn                  bool              `json:"built_in,omitempty"`
	Versions                 []RuntimeVersion  `json:"versions,omitempty"`
	Artifacts                []RuntimeArtifact `json:"artifacts,omitempty"`
	Mirrors                  []RuntimeMirror   `json:"mirrors,omitempty"`
	CreatedAt                time.Time         `json:"created_at,omitempty"`
	UpdatedAt                time.Time         `json:"updated_at,omitempty"`
}

type RuntimeVersion struct {
	ID              string            `json:"id"`
	RuntimeID       string            `json:"runtime_id"`
	Version         string            `json:"version"`
	Channel         string            `json:"channel,omitempty"`
	Status          string            `json:"status,omitempty"`
	VersionOrder    int               `json:"version_order,omitempty"`
	DefaultSelected bool              `json:"default_selected"`
	MinOSVersion    map[string]string `json:"min_os_version,omitempty"`
	InstallManifest map[string]any    `json:"install_manifest,omitempty"`
	Enabled         bool              `json:"enabled"`
	SortOrder       int               `json:"sort_order,omitempty"`
	CreatedAt       time.Time         `json:"created_at,omitempty"`
	UpdatedAt       time.Time         `json:"updated_at,omitempty"`
}

type RuntimeArtifact struct {
	ID           string            `json:"id"`
	RuntimeID    string            `json:"runtime_id"`
	VersionID    string            `json:"version_id"`
	Platform     string            `json:"platform"`
	Arch         string            `json:"arch"`
	PackageKind  string            `json:"package_kind,omitempty"`
	Filename     string            `json:"filename"`
	SizeBytes    int64             `json:"size_bytes,omitempty"`
	SHA256       string            `json:"sha256,omitempty"`
	StorageKey   string            `json:"storage_key,omitempty"`
	DownloadPath string            `json:"download_path,omitempty"`
	ExtractRoot  string            `json:"extract_root,omitempty"`
	BinPaths     []string          `json:"bin_paths,omitempty"`
	EnvPatch     map[string]string `json:"env_patch,omitempty"`
	Priority     int               `json:"priority,omitempty"`
	Enabled      bool              `json:"enabled"`
	CreatedAt    time.Time         `json:"created_at,omitempty"`
	UpdatedAt    time.Time         `json:"updated_at,omitempty"`
}

type RuntimeMirror struct {
	ID                  string            `json:"id"`
	RuntimeID           string            `json:"runtime_id,omitempty"`
	VersionID           string            `json:"version_id,omitempty"`
	Name                string            `json:"name"`
	Description         string            `json:"description,omitempty"`
	BaseURL             string            `json:"base_url,omitempty"`
	URLTemplate         string            `json:"url_template,omitempty"`
	ChecksumURLTemplate string            `json:"checksum_url_template,omitempty"`
	Platform            string            `json:"platform,omitempty"`
	Arch                string            `json:"arch,omitempty"`
	Priority            int               `json:"priority,omitempty"`
	TimeoutSeconds      int               `json:"timeout_seconds,omitempty"`
	Enabled             bool              `json:"enabled"`
	Headers             map[string]string `json:"headers,omitempty"`
	Tags                []string          `json:"tags,omitempty"`
	CreatedAt           time.Time         `json:"created_at,omitempty"`
	UpdatedAt           time.Time         `json:"updated_at,omitempty"`
}

type SemanticAgentRuntime struct {
	RuntimeID         string `json:"runtime_id"`
	VersionConstraint string `json:"version_constraint,omitempty"`
	Required          bool   `json:"required"`
	Purpose           string `json:"purpose,omitempty"`
	SortOrder         int    `json:"sort_order,omitempty"`
}

type ToolCatalogItem struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Description   string    `json:"description,omitempty"`
	Category      string    `json:"category,omitempty"`
	PermissionKey string    `json:"permission_key"`
	Pattern       string    `json:"pattern,omitempty"`
	DefaultAction string    `json:"default_action,omitempty"`
	Enabled       bool      `json:"enabled"`
	Tags          []string  `json:"tags,omitempty"`
	SortOrder     int       `json:"sort_order,omitempty"`
	BuiltIn       bool      `json:"built_in,omitempty"`
	CreatedAt     time.Time `json:"created_at,omitempty"`
	UpdatedAt     time.Time `json:"updated_at,omitempty"`
}

type SemanticAgentSkill struct {
	Name         string             `json:"name"`
	Description  string             `json:"description,omitempty"`
	Content      string             `json:"content,omitempty"`
	PackageFiles []SkillPackageFile `json:"package_files,omitempty"`
}

type DeviceMCPConfigPreview struct {
	Exists      bool              `json:"exists"`
	ConfigPath  string            `json:"config_path,omitempty"`
	RawJSON     string            `json:"raw_json,omitempty"`
	PreviewJSON string            `json:"preview_json,omitempty"`
	ChangedKeys []string          `json:"changed_keys,omitempty"`
	Warning     string            `json:"warning,omitempty"`
	Servers     []DeviceMCPServer `json:"servers,omitempty"`
}

type DeviceMCPConfigGetPayload struct {
	AgentID string `json:"agent_id"`
}

type DeviceMCPConfigSavePayload struct {
	AgentID string            `json:"agent_id"`
	Servers []DeviceMCPServer `json:"servers"`
}

type DeviceMCPConfigRemovePayload struct {
	AgentID string `json:"agent_id"`
	Name    string `json:"name"`
}

type DeviceAgentMCPSelection struct {
	AgentID          string            `json:"agent_id"`
	Mode             string            `json:"mode"`
	SelectedServers  []string          `json:"selected_servers"`
	AvailableServers []DeviceMCPServer `json:"available_servers,omitempty"`
}

type DeviceAgentMCPSelectionPayload struct {
	AgentID string   `json:"agent_id"`
	Mode    string   `json:"mode,omitempty"`
	Servers []string `json:"servers,omitempty"`
}

type DeviceAgentSkillSelection struct {
	AgentID         string               `json:"agent_id"`
	DefaultSkills   []string             `json:"default_skills,omitempty"`
	ExtraSkills     []string             `json:"extra_skills,omitempty"`
	EffectiveSkills []string             `json:"effective_skills,omitempty"`
	AvailableSkills []SemanticAgentSkill `json:"available_skills,omitempty"`
}

type DeviceAgentSkillSelectionPayload struct {
	AgentID       string               `json:"agent_id"`
	ExtraSkillIDs []string             `json:"extra_skill_ids,omitempty"`
	Skills        []SemanticAgentSkill `json:"skills,omitempty"`
}

type DeviceAgentSkillImportPayload struct {
	AgentID   string             `json:"agent_id"`
	Skill     SemanticAgentSkill `json:"skill"`
	Overwrite bool               `json:"overwrite,omitempty"`
}

type DeviceSkillConfigEntry struct {
	Name         string             `json:"name"`
	Description  string             `json:"description,omitempty"`
	Content      string             `json:"content,omitempty"`
	PackageFiles []SkillPackageFile `json:"package_files,omitempty"`
	Enabled      bool               `json:"enabled"`
}

type DeviceSkillConfigInfo struct {
	MachineID string                   `json:"machine_id,omitempty"`
	Enabled   bool                     `json:"enabled"`
	Skills    []DeviceSkillConfigEntry `json:"skills,omitempty"`
}

type DeviceSkillImportPayload struct {
	Skill     SemanticAgentSkill `json:"skill"`
	Overwrite bool               `json:"overwrite,omitempty"`
}

type SemanticAgentProfile struct {
	ID                    string                       `json:"id"`
	Name                  string                       `json:"name"`
	Description           string                       `json:"description,omitempty"`
	Icon                  string                       `json:"icon,omitempty"`
	Color                 string                       `json:"color,omitempty"`
	OpencodeName          string                       `json:"opencode_name,omitempty"`
	Prompt                string                       `json:"prompt,omitempty"`
	SkillIDs              []string                     `json:"skill_ids,omitempty"`
	MCPIDs                []string                     `json:"mcp_ids,omitempty"`
	Skills                []string                     `json:"skills,omitempty"`
	SkillDefinitions      []SemanticAgentSkill         `json:"skill_definitions,omitempty"`
	RecommendedMCPServers []string                     `json:"recommended_mcp_servers,omitempty"`
	RecommendedMCPConfigs []DeviceMCPServer            `json:"recommended_mcp_configs,omitempty"`
	MCPDependencies       []SemanticAgentMCPDependency `json:"mcp_dependencies,omitempty"`
	ToolPermissions       map[string]any               `json:"tool_permissions,omitempty"`
	RuntimeRequirements   []SemanticAgentRuntime       `json:"runtime_requirements,omitempty"`
	Model                 string                       `json:"model,omitempty"`
	Enabled               bool                         `json:"enabled"`
	SortOrder             int                          `json:"sort_order,omitempty"`
	BuiltIn               bool                         `json:"built_in,omitempty"`
	CreatedAt             time.Time                    `json:"created_at,omitempty"`
	UpdatedAt             time.Time                    `json:"updated_at,omitempty"`
}

type DeviceAgentSemanticSelection struct {
	AgentID          string                 `json:"agent_id"`
	SemanticAgentID  string                 `json:"semantic_agent_id"`
	VerifyMCPEnabled bool                   `json:"verify_mcp_enabled"`
	Profile          *SemanticAgentProfile  `json:"profile,omitempty"`
	AvailableAgents  []SemanticAgentProfile `json:"available_agents,omitempty"`
}

type DeviceAgentSemanticSelectionPayload struct {
	AgentID             string                `json:"agent_id"`
	SemanticAgentID     string                `json:"semantic_agent_id,omitempty"`
	ApplyRecommendedMCP bool                  `json:"apply_recommended_mcp,omitempty"`
	DisableVerifyMCP    bool                  `json:"disable_verify_mcp,omitempty"`
	SemanticAgent       *SemanticAgentProfile `json:"semantic_agent,omitempty"`
}

type RuntimeUserAction struct {
	ID           string    `json:"id"`
	Kind         string    `json:"kind,omitempty"`
	Title        string    `json:"title,omitempty"`
	Message      string    `json:"message,omitempty"`
	Instructions []string  `json:"instructions,omitempty"`
	RequestedAt  time.Time `json:"requested_at,omitempty"`
}

type RuntimeInstallJob struct {
	ID                 string                  `json:"id"`
	OperatorID         int64                   `json:"-"`
	MachineID          string                  `json:"machine_id"`
	LauncherAgentID    string                  `json:"launcher_agent_id"`
	SemanticAgentID    string                  `json:"semantic_agent_id"`
	Status             string                  `json:"status"`
	CurrentStep        string                  `json:"current_step,omitempty"`
	ProgressPercent    int                     `json:"progress_percent,omitempty"`
	RequiresUserAction bool                    `json:"requires_user_action,omitempty"`
	UserAction         *RuntimeUserAction      `json:"user_action,omitempty"`
	RequestedBy        string                  `json:"requested_by,omitempty"`
	StartedAt          time.Time               `json:"started_at,omitempty"`
	CompletedAt        time.Time               `json:"completed_at,omitempty"`
	Error              string                  `json:"error,omitempty"`
	Items              []RuntimeInstallJobItem `json:"items,omitempty"`
	LatestEvents       []RuntimeInstallEvent   `json:"latest_events,omitempty"`
	CreatedAt          time.Time               `json:"created_at,omitempty"`
	UpdatedAt          time.Time               `json:"updated_at,omitempty"`
}

type RuntimeInstallJobItem struct {
	JobID           string    `json:"job_id"`
	ItemType        string    `json:"item_type"`
	ItemID          string    `json:"item_id"`
	Name            string    `json:"name,omitempty"`
	Required        bool      `json:"required"`
	Status          string    `json:"status"`
	ProgressPercent int       `json:"progress_percent,omitempty"`
	Error           string    `json:"error,omitempty"`
	UpdatedAt       time.Time `json:"updated_at,omitempty"`
}

type RuntimeInstallEvent struct {
	ID              int64          `json:"id,omitempty"`
	JobID           string         `json:"job_id"`
	Sequence        int64          `json:"sequence"`
	ItemType        string         `json:"item_type,omitempty"`
	ItemID          string         `json:"item_id,omitempty"`
	Phase           string         `json:"phase,omitempty"`
	Status          string         `json:"status,omitempty"`
	Message         string         `json:"message,omitempty"`
	ReceivedBytes   int64          `json:"received_bytes,omitempty"`
	TotalBytes      int64          `json:"total_bytes,omitempty"`
	ProgressPercent int            `json:"progress_percent,omitempty"`
	Details         map[string]any `json:"details,omitempty"`
	CreatedAt       time.Time      `json:"created_at,omitempty"`
}

type RuntimePreflightRequest struct {
	SemanticAgentID     string `json:"semantic_agent_id"`
	ApplyRecommendedMCP bool   `json:"apply_recommended_mcp,omitempty"`
	DisableVerifyMCP    bool   `json:"disable_verify_mcp,omitempty"`
	AutoRepair          bool   `json:"auto_repair,omitempty"`
	RestartAfterApply   bool   `json:"restart_after_apply,omitempty"`
}

type RuntimePreflightStartPayload struct {
	JobID               string                `json:"job_id"`
	MachineID           string                `json:"machine_id"`
	LauncherAgentID     string                `json:"launcher_agent_id"`
	SemanticAgentID     string                `json:"semantic_agent_id"`
	ApplyRecommendedMCP bool                  `json:"apply_recommended_mcp,omitempty"`
	DisableVerifyMCP    bool                  `json:"disable_verify_mcp,omitempty"`
	AutoRepair          bool                  `json:"auto_repair,omitempty"`
	RestartAfterApply   bool                  `json:"restart_after_apply,omitempty"`
	SemanticAgent       *SemanticAgentProfile `json:"semantic_agent,omitempty"`
	Runtimes            []RuntimeCatalogItem  `json:"runtimes,omitempty"`
}

type RuntimePreflightStatusPayload struct {
	JobID              string                        `json:"job_id"`
	MachineID          string                        `json:"machine_id,omitempty"`
	LauncherAgentID    string                        `json:"launcher_agent_id,omitempty"`
	SemanticAgentID    string                        `json:"semantic_agent_id,omitempty"`
	Status             string                        `json:"status"`
	CurrentStep        string                        `json:"current_step,omitempty"`
	ProgressPercent    int                           `json:"progress_percent,omitempty"`
	RequiresUserAction bool                          `json:"requires_user_action,omitempty"`
	UserAction         *RuntimeUserAction            `json:"user_action,omitempty"`
	Items              []RuntimeInstallJobItem       `json:"items,omitempty"`
	Events             []RuntimeInstallEvent         `json:"events,omitempty"`
	Error              string                        `json:"error,omitempty"`
	SemanticSelection  *DeviceAgentSemanticSelection `json:"semantic_selection,omitempty"`
}

type DeviceMCPConfigResultPayload struct {
	AgentID   string                   `json:"agent_id,omitempty"`
	MachineID string                   `json:"machine_id,omitempty"`
	Action    string                   `json:"action"`
	Config    *DeviceMCPConfigPreview  `json:"config,omitempty"`
	Selection *DeviceAgentMCPSelection `json:"selection,omitempty"`
	Success   bool                     `json:"success"`
	Error     string                   `json:"error,omitempty"`
}

type DeviceAgentEnvInfo struct {
	AgentID     string            `json:"agent_id"`
	Name        string            `json:"name,omitempty"`
	ProjectDir  string            `json:"project_dir,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
}

type DeviceEnvConfigPreview struct {
	GlobalEnvironment map[string]string    `json:"global_environment,omitempty"`
	Agents            []DeviceAgentEnvInfo `json:"agents,omitempty"`
	Revision          string               `json:"revision,omitempty"`
}

type DeviceEnvConfigGetPayload struct {
	MachineID string `json:"machine_id,omitempty"`
}

type DeviceEnvConfigSavePayload struct {
	MachineID         string            `json:"machine_id,omitempty"`
	GlobalEnvironment map[string]string `json:"global_environment,omitempty"`
	AgentID           string            `json:"agent_id,omitempty"`
	AgentEnvironment  map[string]string `json:"agent_environment,omitempty"`
	ExpectedRevision  string            `json:"expected_revision,omitempty"`
}

type DeviceEnvConfigResultPayload struct {
	MachineID string                  `json:"machine_id,omitempty"`
	Action    string                  `json:"action"`
	Config    *DeviceEnvConfigPreview `json:"config,omitempty"`
	Success   bool                    `json:"success"`
	Error     string                  `json:"error,omitempty"`
}

type DeviceAgentCompactionConfig struct {
	AgentID                   string `json:"agent_id"`
	ThresholdPercent          int    `json:"threshold_percent"`
	DefaultThresholdPercent   int    `json:"default_threshold_percent"`
	EffectiveThresholdPercent int    `json:"effective_threshold_percent"`
}

type MCPToolInfo struct {
	ID          string `json:"id"`
	Description string `json:"description,omitempty"`
}

type MCPServerStatusInfo struct {
	Name   string        `json:"name"`
	Status string        `json:"status"`
	Error  string        `json:"error,omitempty"`
	Tools  []MCPToolInfo `json:"tools,omitempty"`
}

type MCPStatusInfo struct {
	AgentID string                `json:"agent_id,omitempty"`
	Port    int                   `json:"port,omitempty"`
	Servers []MCPServerStatusInfo `json:"servers,omitempty"`
}

type DeviceLauncherUpgradePayload struct {
	MachineID     string `json:"machine_id,omitempty"`
	TargetVersion string `json:"target_version"`
}

type DeviceLauncherAutostartPayload struct {
	MachineID string `json:"machine_id,omitempty"`
	DryRun    bool   `json:"dry_run,omitempty"`
}

type DeviceLauncherSelfUpdatePayload struct {
	MachineID     string `json:"machine_id,omitempty"`
	TargetVersion string `json:"target_version,omitempty"`
	BaseURL       string `json:"base_url,omitempty"`
	Apply         bool   `json:"apply,omitempty"`
	Restart       bool   `json:"restart,omitempty"`
	DryRun        bool   `json:"dry_run,omitempty"`
}

type DeviceLauncherDirectoryPermissionPayload struct {
	MachineID string `json:"machine_id,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`
	Path      string `json:"path,omitempty"`
	Request   bool   `json:"request,omitempty"`
}

type DeviceDirectoryPermissionResult struct {
	Platform           string `json:"platform,omitempty"`
	Path               string `json:"path,omitempty"`
	Accessible         bool   `json:"accessible"`
	RequiresUserAction bool   `json:"requires_user_action,omitempty"`
	Action             string `json:"action,omitempty"`
	Message            string `json:"message,omitempty"`
	Error              string `json:"error,omitempty"`
}

type DeviceLauncherState struct {
	Status              string                     `json:"status,omitempty"`
	CurrentVersion      string                     `json:"current_version,omitempty"`
	TargetVersion       string                     `json:"target_version,omitempty"`
	PreviousVersion     string                     `json:"previous_version,omitempty"`
	LastError           string                     `json:"last_error,omitempty"`
	AgentCount          int                        `json:"agent_count,omitempty"`
	AllowAllDirectories bool                       `json:"allow_all_directories,omitempty"`
	UpgradeLocked       bool                       `json:"upgrade_locked,omitempty"`
	UpgradeStage        string                     `json:"upgrade_stage,omitempty"`
	UpgradeProgress     int                        `json:"upgrade_progress,omitempty"`
	UpgradeMessage      string                     `json:"upgrade_message,omitempty"`
	UpgradeStartedAt    time.Time                  `json:"upgrade_started_at,omitempty"`
	UpgradeUpdatedAt    time.Time                  `json:"upgrade_updated_at,omitempty"`
	UpgradeLogs         []DeviceLauncherUpgradeLog `json:"upgrade_logs,omitempty"`
	Agents              []Agent                    `json:"agents,omitempty"`
	SSH                 *SSHStatus                 `json:"ssh,omitempty"`
}

type SSHStatus struct {
	Platform           string `json:"platform"`
	Installed          bool   `json:"installed"`
	Running            bool   `json:"running"`
	Listening          bool   `json:"listening"`
	ListenAddress      string `json:"listen_address,omitempty"`
	HostKeyFingerprint string `json:"host_key_fingerprint,omitempty"`
	Message            string `json:"message,omitempty"`
	Error              string `json:"error,omitempty"`
}

type DeviceLauncherTunnelOpenPayload struct {
	MachineID string `json:"machine_id"`
	TunnelID  string `json:"tunnel_id"`
	Token     string `json:"token"`
	TunnelURL string `json:"tunnel_url"`
}

type DeviceLauncherSSHSetupPayload struct {
	MachineID string `json:"machine_id"`
}

type DeviceLauncherSSHAuthorizedKeyPayload struct {
	MachineID string    `json:"machine_id"`
	PublicKey string    `json:"public_key"`
	ExpiresAt time.Time `json:"expires_at"`
}

type DeviceLauncherUpgradeLog struct {
	Time    time.Time `json:"time"`
	Stage   string    `json:"stage,omitempty"`
	Message string    `json:"message"`
}

type DeviceLauncherResultPayload struct {
	MachineID         string                           `json:"machine_id,omitempty"`
	Action            string                           `json:"action"`
	State             *DeviceLauncherState             `json:"state,omitempty"`
	MCPStatus         *MCPStatusInfo                   `json:"mcp_status,omitempty"`
	SemanticSelection *DeviceAgentSemanticSelection    `json:"semantic_selection,omitempty"`
	SkillSelection    *DeviceAgentSkillSelection       `json:"skill_selection,omitempty"`
	SkillConfig       *DeviceSkillConfigInfo           `json:"skill_config,omitempty"`
	Autostart         any                              `json:"autostart,omitempty"`
	SelfUpdate        any                              `json:"self_update,omitempty"`
	Permission        *DeviceDirectoryPermissionResult `json:"permission,omitempty"`
	Identity          *ProjectIdentityCorrectionResult `json:"identity,omitempty"`
	Diagnostics       any                              `json:"diagnostics,omitempty"`
	SSH               *SSHStatus                       `json:"ssh,omitempty"`
	SSHSetup          any                              `json:"setup,omitempty"`
	SSHAuthorizedKey  any                              `json:"authorized_key,omitempty"`
	CompactionConfig  *DeviceAgentCompactionConfig     `json:"compaction_config,omitempty"`
	Success           bool                             `json:"success"`
	Error             string                           `json:"error,omitempty"`
}

type ProjectIdentityCorrectionInput struct {
	AgentID     string `json:"agent_id"`
	Action      string `json:"action"`
	KeepScopeID string `json:"keep_scope_id,omitempty"`
}

type ProjectIdentityCorrectionResult struct {
	ProjectScopeID        string `json:"project_scope_id"`
	InstanceNonce         string `json:"instance_nonce"`
	LineageProjectScopeID string `json:"lineage_project_scope_id,omitempty"`
	Root                  string `json:"root"`
	MarkerWritable        bool   `json:"marker_writable"`
}

type DeviceDirectoriesResultPayload struct {
	MachineID   string            `json:"machine_id,omitempty"`
	Action      string            `json:"action"`
	CurrentPath string            `json:"current_path,omitempty"`
	ParentPath  string            `json:"parent_path,omitempty"`
	Entries     []DeviceDirectory `json:"entries,omitempty"`
	File        *DeviceFile       `json:"file,omitempty"`
	Status      *UploadStatus     `json:"status,omitempty"`
	Success     bool              `json:"success"`
	Error       string            `json:"error,omitempty"`
}

// UploadStatus 描述一次分块上传在设备侧的当前进度，用于断点续传。
type UploadStatus struct {
	Path           string `json:"path,omitempty"`
	UploadID       string `json:"upload_id,omitempty"`
	Size           int64  `json:"size,omitempty"`
	TotalChunks    int    `json:"total_chunks,omitempty"`
	ReceivedChunks []int  `json:"received_chunks,omitempty"`
	ReceivedBytes  int64  `json:"received_bytes,omitempty"`
	Completed      bool   `json:"completed,omitempty"`
	Resumable      bool   `json:"resumable,omitempty"`
}

type DeviceDirectoriesGetPayload struct {
	MachineID string `json:"machine_id,omitempty"`
	Path      string `json:"path,omitempty"`
	AllowAll  bool   `json:"allow_all,omitempty"`
}

type DeviceDirectoryFilesPayload struct {
	MachineID   string `json:"machine_id,omitempty"`
	Path        string `json:"path,omitempty"`
	IsDir       bool   `json:"is_dir,omitempty"`
	AllowAll    bool   `json:"allow_all,omitempty"`
	Content     string `json:"content,omitempty"`
	Encoding    string `json:"encoding,omitempty"`
	UploadID    string `json:"upload_id,omitempty"`
	Size        int64  `json:"size,omitempty"`
	TotalChunks int    `json:"total_chunks,omitempty"`
	ChunkIndex  int    `json:"chunk_index,omitempty"`
	Offset      int64  `json:"offset,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	Resume      bool   `json:"resume,omitempty"`
}

type DeviceProjectFilesPayload struct {
	MachineID   string `json:"machine_id,omitempty"`
	AgentID     string `json:"agent_id"`
	Path        string `json:"path,omitempty"`
	Name        string `json:"name,omitempty"`
	IsDir       bool   `json:"is_dir,omitempty"`
	Content     string `json:"content,omitempty"`
	Encoding    string `json:"encoding,omitempty"`
	UploadID    string `json:"upload_id,omitempty"`
	Size        int64  `json:"size,omitempty"`
	TotalChunks int    `json:"total_chunks,omitempty"`
	ChunkIndex  int    `json:"chunk_index,omitempty"`
	Offset      int64  `json:"offset,omitempty"`
	Length      int64  `json:"length,omitempty"`
	SHA256      string `json:"sha256,omitempty"`
	Resume      bool   `json:"resume,omitempty"`
}

type DeviceDirectory struct {
	Path  string `json:"path"`
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size,omitempty"`
}

type DeviceFile struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Kind     string `json:"kind,omitempty"`
	IsDir    bool   `json:"is_dir"`
	Size     int64  `json:"size,omitempty"`
	Content  string `json:"content,omitempty"`
	Encoding string `json:"encoding,omitempty"`
	SHA256   string `json:"sha256,omitempty"`
}
