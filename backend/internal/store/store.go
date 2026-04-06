package store

import (
	"fmt"
	"log"
	"slices"
	"sync"
	"time"

	"relay-server/internal/model"
)

type Memory struct {
	mu          sync.RWMutex
	tasks       map[string]*model.Task
	events      map[string][]model.Event
	sessions    map[string]*model.Session
	archive     TaskArchive
	subMu       sync.Mutex
	subscribers map[string][]chan model.Event
}

func NewMemory(archive TaskArchive) *Memory {
	if archive == nil {
		archive = noopArchive{}
	}
	return &Memory{
		tasks:       map[string]*model.Task{},
		events:      map[string][]model.Event{},
		sessions:    map[string]*model.Session{},
		subscribers: map[string][]chan model.Event{},
		archive:     archive,
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
	out := clone(task)
	m.persistTask(out)
	return out
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
	out := clone(task)
	m.persistTask(out)
	return out, true
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
	out := clone(task)
	m.persistTask(out)
	return out, true
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
	out := clone(task)
	m.persistTask(out)
	return out, true
}

func (m *Memory) Complete(id, sessionID, result string) (*model.Task, bool) {
	m.mu.Lock()
	task, ok := m.tasks[id]
	if !ok {
		m.mu.Unlock()
		return nil, false
	}
	task.Status = model.TaskCompleted
	task.Result = result
	task.SessionID = sessionID
	task.Approval = nil
	task.UpdatedAt = time.Now().UTC()
	out := clone(task)
	m.mu.Unlock()
	m.persistTask(out)
	m.touchSession(out, "active", result)
	return out, true
}

func (m *Memory) Fail(id, sessionID, msg string) (*model.Task, bool) {
	m.mu.Lock()
	task, ok := m.tasks[id]
	if !ok {
		m.mu.Unlock()
		return nil, false
	}
	task.Status = model.TaskFailed
	task.Error = msg
	task.SessionID = sessionID
	task.Approval = nil
	task.UpdatedAt = time.Now().UTC()
	out := clone(task)
	m.mu.Unlock()
	m.persistTask(out)
	m.touchSession(out, "error", msg)
	return out, true
}

func (m *Memory) Cancel(id string) (*model.Task, bool) {
	m.mu.Lock()
	task, ok := m.tasks[id]
	if !ok {
		m.mu.Unlock()
		return nil, false
	}
	task.Status = model.TaskCancelled
	task.Approval = nil
	task.UpdatedAt = time.Now().UTC()
	out := clone(task)
	m.mu.Unlock()
	m.persistTask(out)
	m.touchSession(out, "cancelled", task.Error)
	return out, true
}

func (m *Memory) AddEvent(id string, evt model.Event) {
	m.mu.Lock()
	m.events[id] = append(m.events[id], evt)
	m.mu.Unlock()
	if err := m.archive.AppendEvent(evt); err != nil {
		log.Printf("append event archive failed: %v", err)
	}
	// 广播给所有订阅者
	m.subMu.Lock()
	subs := m.subscribers[id]
	m.subMu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- evt:
		default:
		}
	}
}

// Subscribe 订阅某个任务的新事件，返回 channel 和取消函数
func (m *Memory) Subscribe(taskID string) (chan model.Event, func()) {
	ch := make(chan model.Event, 32)
	m.subMu.Lock()
	m.subscribers[taskID] = append(m.subscribers[taskID], ch)
	m.subMu.Unlock()
	cancel := func() {
		m.subMu.Lock()
		subs := m.subscribers[taskID]
		for i, s := range subs {
			if s == ch {
				m.subscribers[taskID] = append(subs[:i], subs[i+1:]...)
				break
			}
		}
		m.subMu.Unlock()
		close(ch)
	}
	return ch, cancel
}

func (m *Memory) Events(id string) []model.Event {
	m.mu.RLock()
	defer m.mu.RUnlock()
	src := m.events[id]
	out := make([]model.Event, len(src))
	copy(out, src)
	return out
}

func (m *Memory) ListTasks(filter model.TaskFilter) ([]*model.Task, error) {
	if tasks, err := m.archive.ListTasks(filter); err == nil && tasks != nil {
		return tasks, nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]*model.Task, 0, len(m.tasks))
	for _, task := range m.tasks {
		if filter.AgentID != "" && task.AgentID != filter.AgentID {
			continue
		}
		if filter.MachineID != "" && task.MachineID != filter.MachineID {
			continue
		}
		if filter.ProjectID != "" && task.ProjectID != filter.ProjectID {
			continue
		}
		if filter.SessionID != "" && task.SessionID != filter.SessionID {
			continue
		}
		if filter.Status != "" && task.Status != filter.Status {
			continue
		}
		out = append(out, clone(task))
	}

	slices.SortFunc(out, func(a, b *model.Task) int {
		switch {
		case a.CreatedAt.After(b.CreatedAt):
			return -1
		case a.CreatedAt.Before(b.CreatedAt):
			return 1
		default:
			return 0
		}
	})
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func (m *Memory) ListSessions(filter model.SessionFilter) ([]*model.Session, error) {
	if sessions, err := m.archive.ListSessions(filter); err == nil && sessions != nil {
		return sessions, nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]*model.Session, 0, len(m.sessions))
	for _, session := range m.sessions {
		if filter.AgentID != "" && session.AgentID != filter.AgentID {
			continue
		}
		if filter.MachineID != "" && session.MachineID != filter.MachineID {
			continue
		}
		if filter.ProjectID != "" && session.ProjectID != filter.ProjectID {
			continue
		}
		if filter.Status != "" && session.Status != filter.Status {
			continue
		}
		out = append(out, cloneSession(session))
	}

	slices.SortFunc(out, func(a, b *model.Session) int {
		switch {
		case a.UpdatedAt.After(b.UpdatedAt):
			return -1
		case a.UpdatedAt.Before(b.UpdatedAt):
			return 1
		default:
			return 0
		}
	})
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out, nil
}

func (m *Memory) AuthenticateOperator(username, password string) (*model.Operator, error) {
	return m.archive.AuthenticateOperator(username, password)
}

func (m *Memory) GetOperatorByKey(operatorKey string) (*model.Operator, error) {
	return m.archive.GetOperatorByKey(operatorKey)
}

func (m *Memory) TrackSession(taskID, sessionID, status, summary string) {
	if sessionID == "" || taskID == "" {
		return
	}
	task, ok := m.GetTask(taskID)
	if !ok {
		return
	}
	task.SessionID = sessionID
	m.touchSession(task, status, summary)
}

func (m *Memory) persistTask(task *model.Task) {
	if err := m.archive.UpsertTask(task); err != nil {
		log.Printf("upsert task archive failed: %v", err)
	}
}

func (m *Memory) touchSession(task *model.Task, status, summary string) {
	if task == nil || task.SessionID == "" {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	session, ok := m.sessions[task.SessionID]
	if !ok {
		session = &model.Session{
			ID:        task.SessionID,
			AgentID:   task.AgentID,
			MachineID: task.MachineID,
			ProjectID: task.ProjectID,
			Status:    status,
			CreatedAt: now,
			UpdatedAt: now,
		}
		m.sessions[task.SessionID] = session
	}
	session.AgentID = task.AgentID
	session.MachineID = task.MachineID
	session.ProjectID = task.ProjectID
	session.Status = status
	session.LastTaskID = task.ID
	if summary != "" {
		session.Summary = summary
	}
	if session.CreatedAt.IsZero() {
		session.CreatedAt = now
	}
	session.UpdatedAt = now

	if err := m.archive.UpsertSession(cloneSession(session)); err != nil {
		log.Printf("upsert session archive failed: %v", err)
	}
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

func cloneSession(session *model.Session) *model.Session {
	if session == nil {
		return nil
	}
	cp := *session
	return &cp
}
