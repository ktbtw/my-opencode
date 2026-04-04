package model

import "time"

type TaskStatus string

const (
	TaskPending    TaskStatus = "pending"
	TaskDispatched TaskStatus = "dispatched"
	TaskRunning    TaskStatus = "running"
	TaskCompleted  TaskStatus = "completed"
	TaskFailed     TaskStatus = "failed"
	TaskCancelled  TaskStatus = "cancelled"
)

type Task struct {
	ID        string     `json:"task_id"`
	DeviceID  string     `json:"device_id"`
	ProjectID string     `json:"project_id"`
	SessionID string     `json:"session_id,omitempty"`
	Parts     []Part     `json:"parts,omitempty"`
	Status    TaskStatus `json:"status"`
	Result    string     `json:"result,omitempty"`
	Error     string     `json:"error,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
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
	TaskID    string    `json:"task_id"`
	Type      string    `json:"type"`
	Content   string    `json:"content,omitempty"`
	SessionID string    `json:"session_id,omitempty"`
	Error     string    `json:"error,omitempty"`
	SentAt    time.Time `json:"sent_at"`
}

type Envelope struct {
	Type      string `json:"type"`
	RequestID string `json:"request_id"`
	SentAt    string `json:"sent_at"`
	Payload   any    `json:"payload"`
}

type HelloProject struct {
	ProjectID string `json:"project_id"`
	Root      string `json:"root"`
}

type HelloPayload struct {
	DeviceID string         `json:"device_id"`
	Hostname string         `json:"hostname"`
	Version  string         `json:"version"`
	Projects []HelloProject `json:"projects"`
}

type WelcomePayload struct {
	DeviceID             string `json:"device_id"`
	HeartbeatIntervalSec int    `json:"heartbeat_interval_sec"`
}

type HeartbeatPayload struct {
	DeviceID      string `json:"device_id"`
	RunningTaskID string `json:"running_task_id,omitempty"`
}

type RunPayload struct {
	TaskID    string            `json:"task_id"`
	DeviceID  string            `json:"device_id"`
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

type CancelPayload struct {
	TaskID string `json:"task_id"`
}
