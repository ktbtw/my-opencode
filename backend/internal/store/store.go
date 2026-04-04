package store

import (
	"fmt"
	"sync"
	"time"

	"relay-server/internal/model"
)

type Memory struct {
	mu       sync.RWMutex
	tasks    map[string]*model.Task
	events   map[string][]model.Event
	sessions map[string]string
}

func NewMemory() *Memory {
	return &Memory{
		tasks:    map[string]*model.Task{},
		events:   map[string][]model.Event{},
		sessions: map[string]string{},
	}
}

func (m *Memory) CreateTask(agentID, machineID, projectID, projectRoot, sessionID string, parts []model.Part) *model.Task {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := fmt.Sprintf("task_%d", time.Now().UnixNano())
	now := time.Now().UTC()
	task := &model.Task{
		ID:          id,
		AgentID:     agentID,
		MachineID:   machineID,
		ProjectID:   projectID,
		ProjectRoot: projectRoot,
		SessionID:   sessionID,
		Parts:       cloneParts(parts),
		Status:      model.TaskPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	m.tasks[id] = task
	m.events[id] = []model.Event{}
	return clone(task)
}

func (m *Memory) GetTask(id string) (*model.Task, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	task, ok := m.tasks[id]
	if !ok {
		return nil, false
	}
	return clone(task), true
}

func (m *Memory) SetStatus(id string, status model.TaskStatus) (*model.Task, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	task, ok := m.tasks[id]
	if !ok {
		return nil, false
	}
	task.Status = status
	task.UpdatedAt = time.Now().UTC()
	return clone(task), true
}

func (m *Memory) WaitApproval(id, sessionID string, approval *model.Approval) (*model.Task, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	task, ok := m.tasks[id]
	if !ok {
		return nil, false
	}
	task.Status = model.TaskWaitingApproval
	task.SessionID = sessionID
	task.Approval = cloneApproval(approval)
	task.UpdatedAt = time.Now().UTC()
	return clone(task), true
}

func (m *Memory) Resume(id, sessionID string) (*model.Task, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	task, ok := m.tasks[id]
	if !ok {
		return nil, false
	}
	task.Status = model.TaskRunning
	task.SessionID = sessionID
	task.Approval = nil
	task.UpdatedAt = time.Now().UTC()
	return clone(task), true
}

func (m *Memory) Complete(id, sessionID, result string) (*model.Task, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	task, ok := m.tasks[id]
	if !ok {
		return nil, false
	}
	task.Status = model.TaskCompleted
	task.Result = result
	task.SessionID = sessionID
	task.Approval = nil
	task.UpdatedAt = time.Now().UTC()
	if sessionID != "" {
		m.sessions[id] = sessionID
	}
	return clone(task), true
}

func (m *Memory) Fail(id, sessionID, msg string) (*model.Task, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	task, ok := m.tasks[id]
	if !ok {
		return nil, false
	}
	task.Status = model.TaskFailed
	task.Error = msg
	task.SessionID = sessionID
	task.Approval = nil
	task.UpdatedAt = time.Now().UTC()
	if sessionID != "" {
		m.sessions[id] = sessionID
	}
	return clone(task), true
}

func (m *Memory) Cancel(id string) (*model.Task, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	task, ok := m.tasks[id]
	if !ok {
		return nil, false
	}
	task.Status = model.TaskCancelled
	task.Approval = nil
	task.UpdatedAt = time.Now().UTC()
	return clone(task), true
}

func (m *Memory) AddEvent(id string, evt model.Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events[id] = append(m.events[id], evt)
}

func (m *Memory) Events(id string) []model.Event {
	m.mu.RLock()
	defer m.mu.RUnlock()
	src := m.events[id]
	out := make([]model.Event, len(src))
	copy(out, src)
	return out
}

func clone(task *model.Task) *model.Task {
	cp := *task
	cp.Approval = cloneApproval(task.Approval)
	cp.Parts = cloneParts(task.Parts)
	return &cp
}

func cloneApproval(approval *model.Approval) *model.Approval {
	if approval == nil {
		return nil
	}
	cp := *approval
	if len(approval.Patterns) > 0 {
		cp.Patterns = append([]string(nil), approval.Patterns...)
	}
	return &cp
}

func cloneParts(parts []model.Part) []model.Part {
	if len(parts) == 0 {
		return nil
	}
	out := make([]model.Part, len(parts))
	copy(out, parts)
	return out
}
