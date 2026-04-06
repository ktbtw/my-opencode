package model

import "time"

type TaskStatus string

const (
	TaskPending         TaskStatus = "pending"
	TaskDispatched      TaskStatus = "dispatched"
	TaskRunning         TaskStatus = "running"
	TaskWaitingApproval TaskStatus = "waiting_approval"
	TaskCompleted       TaskStatus = "completed"
	TaskFailed          TaskStatus = "failed"
	TaskCancelled       TaskStatus = "cancelled"
)

type Task struct {
	ID          string     `json:"task_id"`
	AgentID     string     `json:"agent_id"`
	MachineID   string     `json:"machine_id,omitempty"`
	ProjectID   string     `json:"project_id"`
	ProjectRoot string     `json:"project_root,omitempty"`
	SessionID   string     `json:"session_id,omitempty"`
	Approval    *Approval  `json:"approval,omitempty"`
	Parts       []Part     `json:"parts,omitempty"`
	Status      TaskStatus `json:"status"`
	Result      string     `json:"result,omitempty"`
	Error       string     `json:"error,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type TaskFilter struct {
	AgentID   string
	MachineID string
	ProjectID string
	SessionID string
	Status    TaskStatus
	Limit     int
}

type Session struct {
	ID         string    `json:"session_id"`
	AgentID    string    `json:"agent_id"`
	MachineID  string    `json:"machine_id"`
	ProjectID  string    `json:"project_id"`
	Status     string    `json:"status"`
	LastTaskID string    `json:"last_task_id,omitempty"`
	Summary    string    `json:"summary,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type SessionFilter struct {
	AgentID   string
	MachineID string
	ProjectID string
	Status    string
	Limit     int
}

type Approval struct {
	PermissionID string   `json:"permission_id"`
	Permission   string   `json:"permission"`
	Patterns     []string `json:"patterns,omitempty"`
	Metadata     any      `json:"metadata,omitempty"`
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

type Event struct {
	TaskID       string    `json:"task_id"`
	Type         string    `json:"type"`
	Content      string    `json:"content,omitempty"`
	Field        string    `json:"field,omitempty"`
	SessionID    string    `json:"session_id,omitempty"`
	PermissionID string    `json:"permission_id,omitempty"`
	Permission   string    `json:"permission,omitempty"`
	Patterns     []string  `json:"patterns,omitempty"`
	Reply        string    `json:"reply,omitempty"`
	Metadata     any       `json:"metadata,omitempty"`
	Error        string    `json:"error,omitempty"`
	SentAt       time.Time `json:"sent_at"`
}

type Operator struct {
	ID          int64  `json:"id"`
	OperatorUID string `json:"operator_uid"`
	Username    string `json:"username"`
	Name        string `json:"name"`
	OperatorKey string `json:"operator_key,omitempty"`
}

type Envelope struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	SentAt    string `json:"sent_at"`
	Payload   any    `json:"payload"`
}

type Agent struct {
	ID          string         `json:"agent_id"`
	OperatorID  int64          `json:"operator_id,omitempty"`
	MachineID   string         `json:"machine_id"`
	Hostname    string         `json:"hostname"`
	Version     string         `json:"version"`
	Projects    []HelloProject `json:"projects,omitempty"`
	SeenAt      time.Time      `json:"seen_at"`
	CurrentTask string         `json:"current_task_id,omitempty"`
}

type Machine struct {
	MachineID string    `json:"machine_id"`
	Hostname  string    `json:"hostname"`
	Status    string    `json:"status"`
	SeenAt    time.Time `json:"seen_at"`
	Agents    []Agent   `json:"agents,omitempty"`
}

type HelloProject struct {
	ProjectID string `json:"project_id"`
	Root      string `json:"root"`
}

type HelloPayload struct {
	AgentID     string         `json:"agent_id,omitempty"`
	MachineID   string         `json:"machine_id,omitempty"`
	DeviceID    string         `json:"device_id,omitempty"`
	OperatorKey string         `json:"operator_key,omitempty"`
	Hostname    string         `json:"hostname"`
	Version     string         `json:"version"`
	Projects    []HelloProject `json:"projects"`
}

type WelcomePayload struct {
	AgentID              string `json:"agent_id"`
	HeartbeatIntervalSec int    `json:"heartbeat_interval_sec"`
}

type HeartbeatPayload struct {
	AgentID       string `json:"agent_id,omitempty"`
	RunningTaskID string `json:"running_task_id,omitempty"`
}

type RunPayload struct {
	TaskID    string            `json:"task_id"`
	AgentID   string            `json:"agent_id"`
	MachineID string            `json:"machine_id,omitempty"`
	ProjectID string            `json:"project_id"`
	SessionID string            `json:"session_id,omitempty"`
	Parts     []Part            `json:"parts,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
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

type CompletedPayload struct {
	TaskID    string `json:"task_id"`
	SessionID string `json:"session_id,omitempty"`
	Result    string `json:"result"`
}

type FailedPayload struct {
	TaskID    string `json:"task_id"`
	SessionID string `json:"session_id,omitempty"`
	Error     string `json:"error"`
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

type CancelPayload struct {
	TaskID string `json:"task_id"`
}
