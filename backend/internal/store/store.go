package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"relay-server/internal/model"
)

type healthChecker interface {
	PingContext(context.Context) error
}

// Keep only a small live-task window in process memory. Historical events are
// read from the durable archive on demand and are never retained here.
const taskEventHistoryLimit = 500
const taskEventWriteShardCount = 64
const taskEventCatchUpLimit = 2000

// Per-subscriber mailbox cap. A stuck SSE client is disconnected instead of
// dropping events from the middle of a live stream.
const taskEventSubscriberQueueLimit = 16384

func archiveHighFreqEvent(eventType string) bool {
	switch eventType {
	case "delta", "progress":
		value := strings.TrimSpace(os.Getenv("ARCHIVE_HIGH_FREQ_EVENTS"))
		switch {
		case value == "1", strings.EqualFold(value, "true"), strings.EqualFold(value, "on"):
			return true
		default:
			// Phase 4 default: keep delta/progress in the live Memory+SSE
			// window only. Set ARCHIVE_HIGH_FREQ_EVENTS=1 to restore MySQL writes.
			return false
		}
	}
	return true
}

type Memory struct {
	mu                        sync.RWMutex
	tasks                     map[string]*model.Task
	events                    map[string][]model.Event
	eventSequences            map[string]int64
	sessions                  map[string]*model.Session
	chatQueue                 map[string]model.ChatQueueItem
	chatQueueLoaded           map[string]bool
	chatQueueVersions         map[string]int64
	chatQueueDispatching      map[string]bool
	deviceSettings            map[int64]map[string]map[string]string
	devicePreferences         map[int64]map[string]model.DevicePreference
	agentPreferences          map[int64]map[string]model.AgentPreference
	pushDevices               map[int64]map[int64]*model.PushDevice
	skills                    map[string]model.Skill
	mcpCatalog                map[string]model.MCPCatalogItem
	envPresets                map[string]model.EnvironmentPreset
	runtimeCatalog            map[string]model.RuntimeCatalogItem
	runtimeVersions           map[string]model.RuntimeVersion
	runtimeArtifacts          map[string]model.RuntimeArtifact
	runtimeMirrors            map[string]model.RuntimeMirror
	toolCatalog               map[string]model.ToolCatalogItem
	semanticAgents            map[string]model.SemanticAgentProfile
	runtimeJobs               map[string]model.RuntimeInstallJob
	runtimeJobItems           map[string][]model.RuntimeInstallJobItem
	runtimeEvents             map[string][]model.RuntimeInstallEvent
	runtimeSeq                map[string]int64
	runtimeActionNotices      map[string]bool
	projectScopes             map[string]model.ProjectScope
	projectLocations          map[string]model.ProjectScopeLocation
	projectBindings           map[string]model.ProjectAgentBinding
	projectMemories           map[string]model.ProjectMemory
	projectMemoryJobs         map[string]model.ProjectMemoryJob
	projectMemoryEvents       map[string][]model.ProjectMemoryJobEvent
	projectMemoryBriefs       map[string]model.ProjectMemoryBrief
	projectMemoryUsages       []model.ProjectMemoryUsage
	projectMemoryAudits       []model.ProjectMemoryAudit
	projectMemoryEventID      int64
	appNotifications          map[string]model.AppNotificationChange
	appNotificationChanges    map[int64][]model.AppNotificationChange
	appNotificationVersions   map[int64]int64
	archive                   TaskArchive
	eventWriteLocks           [taskEventWriteShardCount]sync.Mutex
	subMu                     sync.Mutex
	subscribers               map[string][]*taskEventSubscriber
	queueSubMu                sync.Mutex
	queueSubscribers          map[string][]chan model.ChatQueueSnapshot
	taskObserverMu            sync.RWMutex
	taskObserver              func(*model.Task)
	appNotificationWriteMu    sync.Mutex
	appNotificationObserverMu sync.RWMutex
	appNotificationObserver   func(int64, model.AppNotificationChange)
}

var lastTaskIDTimestamp atomic.Int64

func nextTaskID() string {
	candidate := time.Now().UnixNano()
	for {
		previous := lastTaskIDTimestamp.Load()
		if candidate <= previous {
			candidate = previous + 1
		}
		if lastTaskIDTimestamp.CompareAndSwap(previous, candidate) {
			return fmt.Sprintf("task_%d", candidate)
		}
	}
}

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	out := make(map[string]string, len(input))
	for k, v := range input {
		out[k] = v
	}
	return out
}

func NewMemory(archive TaskArchive) *Memory {
	if archive == nil {
		archive = noopArchive{}
	}
	return &Memory{
		tasks:                   map[string]*model.Task{},
		events:                  map[string][]model.Event{},
		eventSequences:          map[string]int64{},
		sessions:                map[string]*model.Session{},
		chatQueue:               map[string]model.ChatQueueItem{},
		chatQueueLoaded:         map[string]bool{},
		chatQueueVersions:       map[string]int64{},
		chatQueueDispatching:    map[string]bool{},
		deviceSettings:          map[int64]map[string]map[string]string{},
		devicePreferences:       map[int64]map[string]model.DevicePreference{},
		agentPreferences:        map[int64]map[string]model.AgentPreference{},
		pushDevices:             map[int64]map[int64]*model.PushDevice{},
		skills:                  map[string]model.Skill{},
		mcpCatalog:              map[string]model.MCPCatalogItem{},
		envPresets:              map[string]model.EnvironmentPreset{},
		runtimeCatalog:          map[string]model.RuntimeCatalogItem{},
		runtimeVersions:         map[string]model.RuntimeVersion{},
		runtimeArtifacts:        map[string]model.RuntimeArtifact{},
		runtimeMirrors:          map[string]model.RuntimeMirror{},
		toolCatalog:             map[string]model.ToolCatalogItem{},
		semanticAgents:          map[string]model.SemanticAgentProfile{},
		runtimeJobs:             map[string]model.RuntimeInstallJob{},
		runtimeJobItems:         map[string][]model.RuntimeInstallJobItem{},
		runtimeEvents:           map[string][]model.RuntimeInstallEvent{},
		runtimeSeq:              map[string]int64{},
		runtimeActionNotices:    map[string]bool{},
		projectScopes:           map[string]model.ProjectScope{},
		projectLocations:        map[string]model.ProjectScopeLocation{},
		projectBindings:         map[string]model.ProjectAgentBinding{},
		projectMemories:         map[string]model.ProjectMemory{},
		projectMemoryJobs:       map[string]model.ProjectMemoryJob{},
		projectMemoryEvents:     map[string][]model.ProjectMemoryJobEvent{},
		projectMemoryBriefs:     map[string]model.ProjectMemoryBrief{},
		projectMemoryUsages:     []model.ProjectMemoryUsage{},
		projectMemoryAudits:     []model.ProjectMemoryAudit{},
		appNotifications:        map[string]model.AppNotificationChange{},
		appNotificationChanges:  map[int64][]model.AppNotificationChange{},
		appNotificationVersions: map[int64]int64{},
		subscribers:             map[string][]*taskEventSubscriber{},
		queueSubscribers:        map[string][]chan model.ChatQueueSnapshot{},
		archive:                 archive,
	}
}

func (m *Memory) Close() error {
	if m == nil || m.archive == nil {
		return nil
	}
	return m.archive.Close()
}

func (m *Memory) CreateTask(agentID, machineID, projectID, projectRoot, sessionID string, parts []model.Part, metadata map[string]string, operatorID int64) *model.Task {
	m.mu.Lock()
	defer m.mu.Unlock()

	id := nextTaskID()
	now := time.Now().UTC()
	task := &model.Task{
		ID:          id,
		AgentID:     agentID,
		MachineID:   machineID,
		ProjectID:   projectID,
		ProjectRoot: projectRoot,
		SessionID:   sessionID,
		OperatorID:  operatorID,
		Parts:       cloneParts(parts),
		Metadata:    cloneStringMap(metadata),
		Status:      model.TaskPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	m.tasks[id] = task
	m.events[id] = []model.Event{}
	// A newly-created task has no archived events. Mark its sequence as known
	// so the first event does not issue a database count/scan query.
	m.eventSequences[id] = 0
	out := clone(task)
	m.persistTask(out)
	return out
}

func (m *Memory) GetTask(id string) (*model.Task, bool) {
	m.mu.RLock()
	task := m.tasks[id]
	if task != nil {
		out := clone(task)
		m.mu.RUnlock()
		return out, true
	}
	m.mu.RUnlock()
	// Completed history is authoritative in the archive and is intentionally
	// not inserted into the in-process task map.
	archived, err := m.archive.GetTask(id)
	if err != nil || archived == nil {
		return nil, false
	}
	return clone(archived), true
}

func (m *Memory) UpdateTask(task *model.Task) (*model.Task, bool) {
	if task == nil || strings.TrimSpace(task.ID) == "" {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	next := clone(task)
	if next.Metadata == nil {
		next.Metadata = map[string]string{}
	}
	m.tasks[next.ID] = next
	if _, ok := m.events[next.ID]; !ok {
		m.events[next.ID] = []model.Event{}
	}
	out := clone(next)
	m.persistTask(out)
	return out, true
}

func (m *Memory) loadTaskIntoMemory(id string) bool {
	m.mu.RLock()
	_, ok := m.tasks[id]
	m.mu.RUnlock()
	if ok {
		return true
	}

	task, err := m.archive.GetTask(id)
	if err != nil || task == nil {
		return false
	}
	if terminalTaskStatus(task.Status) {
		return false
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.tasks[id]; ok {
		return true
	}
	m.tasks[id] = clone(task)
	return true
}

func (m *Memory) SetStatus(id string, status model.TaskStatus) (*model.Task, bool) {
	if !m.loadTaskIntoMemory(id) {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	task := m.tasks[id]
	task.Status = status
	task.UpdatedAt = time.Now().UTC()
	out := clone(task)
	m.persistTask(out)
	return out, true
}

func (m *Memory) TouchTask(id string) (*model.Task, bool) {
	if strings.TrimSpace(id) == "" || !m.loadTaskIntoMemory(id) {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	task := m.tasks[id]
	switch task.Status {
	case model.TaskCompleted, model.TaskFailed, model.TaskCancelled:
		return clone(task), true
	}
	task.UpdatedAt = time.Now().UTC()
	out := clone(task)
	m.persistTask(out)
	return out, true
}

func (m *Memory) UpdateTaskMetadata(id string, updates map[string]string) (*model.Task, bool) {
	if strings.TrimSpace(id) == "" || !m.loadTaskIntoMemory(id) {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	task := m.tasks[id]
	if task.Metadata == nil {
		task.Metadata = map[string]string{}
	}
	for key, value := range updates {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if value == "" {
			delete(task.Metadata, key)
			continue
		}
		task.Metadata[key] = value
	}
	task.UpdatedAt = time.Now().UTC()
	out := clone(task)
	m.persistTask(out)
	return out, true
}

func (m *Memory) WaitApproval(id, sessionID string, approval *model.Approval) (*model.Task, bool) {
	if !m.loadTaskIntoMemory(id) {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	task := m.tasks[id]
	task.Status = model.TaskWaitingApproval
	task.SessionID = sessionID
	task.Approval = cloneApproval(approval)
	task.UpdatedAt = time.Now().UTC()
	out := clone(task)
	m.persistTask(out)
	return out, true
}

func (m *Memory) WaitQuestion(id, sessionID string, question *model.QuestionRequest) (*model.Task, bool) {
	if !m.loadTaskIntoMemory(id) {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	task := m.tasks[id]
	task.SessionID = sessionID
	task.Question = cloneQuestion(question)
	task.UpdatedAt = time.Now().UTC()
	out := clone(task)
	m.persistTask(out)
	return out, true
}

func (m *Memory) UpdatePlan(id, sessionID string, plan *model.Plan) (*model.Task, bool) {
	if !m.loadTaskIntoMemory(id) {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	task := m.tasks[id]
	task.SessionID = sessionID
	task.Plan = clonePlan(plan)
	task.UpdatedAt = time.Now().UTC()
	out := clone(task)
	m.persistTask(out)
	return out, true
}

func (m *Memory) Resume(id, sessionID string) (*model.Task, bool) {
	if !m.loadTaskIntoMemory(id) {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	task := m.tasks[id]
	task.Status = model.TaskRunning
	task.SessionID = sessionID
	task.Approval = nil
	task.Question = nil
	task.UpdatedAt = time.Now().UTC()
	out := clone(task)
	m.persistTask(out)
	return out, true
}

func (m *Memory) ResolveQuestion(id, sessionID string) (*model.Task, bool) {
	if !m.loadTaskIntoMemory(id) {
		return nil, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	task := m.tasks[id]
	task.SessionID = sessionID
	task.Question = nil
	task.UpdatedAt = time.Now().UTC()
	out := clone(task)
	m.persistTask(out)
	return out, true
}

func (m *Memory) MarkCancelling(id, sessionID, msg string) (*model.Task, bool) {
	if !m.loadTaskIntoMemory(id) {
		return nil, false
	}
	m.mu.Lock()
	task := m.tasks[id]
	switch task.Status {
	case model.TaskCompleted, model.TaskFailed, model.TaskCancelled:
		out := clone(task)
		m.mu.Unlock()
		return out, false
	}
	task.Status = model.TaskCancelling
	task.Error = msg
	task.SessionID = sessionID
	task.Approval = nil
	task.Question = nil
	task.UpdatedAt = time.Now().UTC()
	out := clone(task)
	m.mu.Unlock()
	m.persistTask(out)
	m.touchSession(out, "cancelling", msg)
	return out, true
}

func (m *Memory) Complete(id, sessionID, result string, artifacts []model.Artifact) (*model.Task, bool) {
	if !m.loadTaskIntoMemory(id) {
		return nil, false
	}
	m.mu.Lock()
	task := m.tasks[id]
	task.Status = model.TaskCompleted
	task.Result = result
	task.SessionID = sessionID
	task.Artifacts = cloneArtifacts(artifacts)
	task.Approval = nil
	task.Question = nil
	task.UpdatedAt = time.Now().UTC()
	out := clone(task)
	m.mu.Unlock()
	m.persistTask(out)
	m.touchSession(out, "active", result)
	return out, true
}

func (m *Memory) Fail(id, sessionID, msg string) (*model.Task, bool) {
	if !m.loadTaskIntoMemory(id) {
		return nil, false
	}
	m.mu.Lock()
	task := m.tasks[id]
	task.Status = model.TaskFailed
	task.Error = msg
	task.SessionID = sessionID
	task.Approval = nil
	task.Question = nil
	task.UpdatedAt = time.Now().UTC()
	out := clone(task)
	m.mu.Unlock()
	m.persistTask(out)
	m.touchSession(out, "error", msg)
	return out, true
}

func (m *Memory) Cancel(id string) (*model.Task, bool) {
	if !m.loadTaskIntoMemory(id) {
		return nil, false
	}
	m.mu.Lock()
	task := m.tasks[id]
	switch task.Status {
	case model.TaskCompleted, model.TaskFailed, model.TaskCancelled:
		out := clone(task)
		m.mu.Unlock()
		return out, false
	}
	task.Status = model.TaskCancelled
	task.Approval = nil
	task.Question = nil
	task.UpdatedAt = time.Now().UTC()
	out := clone(task)
	m.mu.Unlock()
	m.persistTask(out)
	m.touchSession(out, "cancelled", task.Error)
	return out, true
}

func (m *Memory) AddEvent(id string, evt model.Event) {
	writeLock := m.taskEventWriteLock(id)
	writeLock.Lock()

	m.mu.Lock()
	if evt.Sequence <= 0 {
		nextSequence := m.eventSequences[id]
		_, sequenceKnown := m.eventSequences[id]
		if nextSequence == 0 && len(m.events[id]) > 0 {
			nextSequence = m.events[id][len(m.events[id])-1].Sequence
		}
		if nextSequence == 0 && !sequenceKnown {
			if counter, ok := m.archive.(TaskEventSequenceReader); ok {
				if latest, err := counter.LatestEventSequence(id); err == nil {
					nextSequence = latest
				}
			}
		}
		evt.Sequence = nextSequence + 1
	}
	if evt.Sequence > m.eventSequences[id] {
		m.eventSequences[id] = evt.Sequence
	}
	m.events[id] = append(m.events[id], evt)
	if len(m.events[id]) > taskEventHistoryLimit {
		m.events[id] = m.events[id][len(m.events[id])-taskEventHistoryLimit:]
	}
	// 更新任务的 updated_at，用于超时检测
	if task, ok := m.tasks[id]; ok {
		task.UpdatedAt = time.Now().UTC()
	}
	m.mu.Unlock()
	if !archiveHighFreqEvent(evt.Type) {
		if evt.ID <= 0 {
			evt.ID = evt.Sequence
		}
	} else if appender, ok := m.archive.(TaskEventAppenderWithID); ok {
		if err := appender.AppendEventWithID(&evt); err != nil {
			log.Printf("append event archive failed: %v", err)
		}
	} else if err := m.archive.AppendEvent(evt); err != nil {
		log.Printf("append event archive failed: %v", err)
	}
	if evt.ID <= 0 {
		// Ephemeral test archives have no durable row id; sequence remains a
		// stable local cursor until the task is persisted by a real archive.
		evt.ID = evt.Sequence
	}
	// Persisting may have assigned the durable row id and normalized sent_at to
	// database precision. Keep the live copy and SSE payload aligned with it.
	m.mu.Lock()
	if events := m.events[id]; len(events) > 0 {
		for index := len(events) - 1; index >= 0; index-- {
			if events[index].Sequence == evt.Sequence {
				events[index].ID = evt.ID
				events[index].SentAt = evt.SentAt
				break
			}
		}
	}
	m.mu.Unlock()
	m.subMu.Lock()
	subscribers := append([]*taskEventSubscriber(nil), m.subscribers[id]...)
	m.subMu.Unlock()
	terminal := terminalTaskEvent(evt.Type)
	// A terminal task no longer needs an in-process history window. Keeping it
	// here made memory grow with the number of completed tasks; later history
	// reads use the archive directly.
	if terminal {
		_, durable := m.archive.(TaskEventPageReader)
		_, ephemeral := m.archive.(noopArchive)
		if durable || ephemeral {
			m.mu.Lock()
			delete(m.events, id)
			if durable {
				delete(m.tasks, id)
				delete(m.eventSequences, id)
			}
			m.mu.Unlock()
		}
	}
	writeLock.Unlock()
	for _, sub := range subscribers {
		sub.enqueue(evt)
	}
	if overlayTaskEvent(evt.Type) {
		if task, ok := m.GetTask(id); ok {
			m.taskObserverMu.RLock()
			observer := m.taskObserver
			m.taskObserverMu.RUnlock()
			if observer != nil {
				observer(task)
			}
		}
	}
}

func (m *Memory) taskEventWriteLock(taskID string) *sync.Mutex {
	var hash uint32 = 2166136261
	for index := 0; index < len(taskID); index++ {
		hash ^= uint32(taskID[index])
		hash *= 16777619
	}
	return &m.eventWriteLocks[hash%taskEventWriteShardCount]
}

func (m *Memory) SetTaskObserver(observer func(*model.Task)) {
	m.taskObserverMu.Lock()
	m.taskObserver = observer
	m.taskObserverMu.Unlock()
}

func overlayTaskEvent(eventType string) bool {
	switch eventType {
	case "dispatched", "started", "cancelling", "waiting_approval", "question_asked", "completed", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func terminalTaskEvent(eventType string) bool {
	switch eventType {
	case "completed", "failed", "cancelled":
		return true
	default:
		return false
	}
}

func terminalTaskStatus(status model.TaskStatus) bool {
	switch status {
	case model.TaskCompleted, model.TaskFailed, model.TaskCancelled:
		return true
	default:
		return false
	}
}

type taskEventSubscriber struct {
	mu        sync.Mutex
	cond      *sync.Cond
	queue     []model.Event
	closed    bool
	closeOnce sync.Once
	out       chan model.Event
	done      chan struct{}
}

func newTaskEventSubscriber() *taskEventSubscriber {
	sub := &taskEventSubscriber{
		out:  make(chan model.Event),
		done: make(chan struct{}),
	}
	sub.cond = sync.NewCond(&sub.mu)
	go sub.pump()
	return sub
}

func (s *taskEventSubscriber) enqueue(evt model.Event) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	if len(s.queue) >= taskEventSubscriberQueueLimit {
		s.mu.Unlock()
		log.Printf("disconnect slow task event subscriber task=%s type=%s queued=%d", evt.TaskID, evt.Type, taskEventSubscriberQueueLimit)
		s.close()
		return
	}
	s.queue = append(s.queue, evt)
	s.cond.Signal()
	s.mu.Unlock()
}

func (s *taskEventSubscriber) close() {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.queue = nil
		s.cond.Broadcast()
		s.mu.Unlock()
		close(s.done)
	})
}

func (s *taskEventSubscriber) pump() {
	defer close(s.out)
	for {
		s.mu.Lock()
		for len(s.queue) == 0 && !s.closed {
			s.cond.Wait()
		}
		if s.closed {
			s.queue = nil
			s.mu.Unlock()
			return
		}
		evt := s.queue[0]
		s.queue[0] = model.Event{}
		s.queue = s.queue[1:]
		if len(s.queue) == 0 {
			s.queue = nil
		}
		s.mu.Unlock()
		select {
		case s.out <- evt:
		case <-s.done:
			return
		}
	}
}

// Subscribe 订阅某个任务的新事件，返回 channel 和取消函数。
// 每个订阅使用独立邮箱：写入不丢事件，也不阻塞任务落盘。
func (m *Memory) Subscribe(taskID string) (chan model.Event, func()) {
	sub := newTaskEventSubscriber()
	m.subMu.Lock()
	m.subscribers[taskID] = append(m.subscribers[taskID], sub)
	m.subMu.Unlock()
	var cancelOnce sync.Once
	cancel := func() {
		cancelOnce.Do(func() {
			m.subMu.Lock()
			subs := m.subscribers[taskID]
			for i, item := range subs {
				if item == sub {
					m.subscribers[taskID] = append(subs[:i], subs[i+1:]...)
					break
				}
			}
			m.subMu.Unlock()
			sub.close()
		})
	}
	return sub.out, cancel
}

func (m *Memory) Events(id string) []model.Event {
	m.mu.RLock()
	src := m.events[id]
	if len(src) > 0 {
		out := make([]model.Event, len(src))
		copy(out, src)
		m.mu.RUnlock()
		return out
	}
	m.mu.RUnlock()

	archived, err := m.archive.ListEvents(id)
	if err == nil && len(archived) > 0 {
		for index := range archived {
			if archived[index].Sequence <= 0 {
				archived[index].Sequence = int64(index + 1)
			}
		}
		// Do not populate m.events here. This is a historical read and the
		// returned slice must die with the request instead of becoming a
		// permanent cache entry for the task.
		return archived
	}
	if err != nil {
		log.Printf("list event archive failed: %v", err)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	src = m.events[id]
	out := make([]model.Event, len(src))
	copy(out, src)
	return out
}

func (m *Memory) EventsAfter(id string, afterSequence int64) []model.Event {
	if afterSequence < 0 {
		afterSequence = 0
	}
	m.mu.RLock()
	src := m.events[id]
	if len(src) > 0 {
		firstSequence := src[0].Sequence
		if afterSequence == 0 || firstSequence <= afterSequence+1 {
			out := make([]model.Event, 0, minInt(len(src), taskEventHistoryLimit))
			for _, event := range src {
				if event.Sequence > afterSequence {
					out = append(out, event)
				}
			}
			m.mu.RUnlock()
			if len(out) > taskEventHistoryLimit {
				return out[:taskEventHistoryLimit]
			}
			return out
		}
	}
	m.mu.RUnlock()
	if reader, ok := m.archive.(TaskEventWindowReader); ok {
		events, err := reader.ListEventsAfter(id, afterSequence, taskEventHistoryLimit)
		if err == nil {
			return events
		}
		log.Printf("list event window failed: %v", err)
	}
	archived, err := m.archive.ListEvents(id)
	if err != nil {
		log.Printf("list event archive failed: %v", err)
		return nil
	}
	out := make([]model.Event, 0, minInt(len(archived), taskEventHistoryLimit))
	for _, event := range archived {
		if event.Sequence > afterSequence {
			out = append(out, event)
			if len(out) == taskEventHistoryLimit {
				break
			}
		}
	}
	return out
}

func (m *Memory) ProjectMemoryEvents(id string, limit int) []model.Event {
	if limit <= 0 {
		limit = 64
	}
	if limit > 64 {
		limit = 64
	}
	m.mu.RLock()
	src := m.events[id]
	if len(src) > 0 {
		out := selectProjectMemoryEvents(src, limit)
		m.mu.RUnlock()
		return out
	}
	m.mu.RUnlock()
	if reader, ok := m.archive.(ProjectMemoryEventReader); ok {
		events, err := reader.ListProjectMemoryEvents(id, limit)
		if err == nil {
			return events
		}
		log.Printf("list project memory events failed: %v", err)
	}
	return nil
}

// ListEventPage exposes durable keyset pagination to API consumers. When a
// durable reader exists it is the sole source, so an in-flight live row can
// never leak a cursor that does not yet exist in the archive.
func (m *Memory) ListEventPage(id string, after *EventCursor, limit int, eventTypes []string, nodeID string) (EventPage, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	if reader, ok := m.archive.(TaskEventPageReader); ok {
		return reader.ListEventPage(id, after, limit, eventTypes, nodeID)
	}
	m.mu.RLock()
	live := append([]model.Event(nil), m.events[id]...)
	m.mu.RUnlock()
	if len(live) > 0 {
		items := make([]model.Event, 0, minInt(len(live), limit))
		for _, event := range live {
			if after != nil && (event.SentAt.Before(after.SentAt) || (event.SentAt.Equal(after.SentAt) && eventSequenceID(event) <= after.ID)) {
				continue
			}
			if len(eventTypes) > 0 && !slices.Contains(eventTypes, event.Type) {
				continue
			}
			if nodeID != "" && eventNodeID(event.Metadata) != nodeID {
				continue
			}
			if event.ID == 0 {
				event.ID = eventSequenceID(event)
			}
			items = append(items, event)
			if len(items) == limit {
				break
			}
		}
		page := EventPage{Items: items, HasMore: len(items) == limit}
		if len(items) > 0 {
			last := items[len(items)-1]
			page.NextCursor = &EventCursor{SentAt: last.SentAt, ID: eventSequenceID(last)}
		}
		return page, nil
	}
	// Lightweight test archives may only implement the legacy reader.
	filtered := live
	if len(filtered) == 0 {
		filtered, _ = m.archive.ListEvents(id)
	}
	items := make([]model.Event, 0, limit)
	for _, event := range filtered {
		if after != nil && (event.SentAt.Before(after.SentAt) || (event.SentAt.Equal(after.SentAt) && event.ID <= after.ID)) {
			continue
		}
		if len(eventTypes) > 0 && !slices.Contains(eventTypes, event.Type) {
			continue
		}
		if nodeID != "" && eventNodeID(event.Metadata) != nodeID {
			continue
		}
		items = append(items, event)
		if len(items) == limit {
			break
		}
	}
	page := EventPage{Items: items, HasMore: len(items) == limit}
	if len(items) > 0 {
		last := items[len(items)-1]
		page.NextCursor = &EventCursor{SentAt: last.SentAt, ID: last.ID}
	}
	return page, nil
}

func eventAfterCatchUpCursor(event model.Event, after *EventCursor, afterSequence int64) bool {
	// Live deltas use ID=sequence; archived tool_updated rows use MySQL
	// autoincrement IDs. Mixing those keys in the same truncated second
	// drops tool completions (or later body text) on SSE reconnect.
	if afterSequence > 0 && event.Sequence > 0 {
		return event.Sequence > afterSequence
	}
	if after != nil && !after.SentAt.IsZero() {
		if event.SentAt.Before(after.SentAt) ||
			(event.SentAt.Equal(after.SentAt) && eventSequenceID(event) <= after.ID) {
			return false
		}
	}
	return true
}

func (m *Memory) liveEventsAfter(id string, after *EventCursor, afterSequence int64) []model.Event {
	m.mu.RLock()
	src := m.events[id]
	out := make([]model.Event, 0, len(src))
	for _, event := range src {
		if eventAfterCatchUpCursor(event, after, afterSequence) {
			out = append(out, event)
		}
	}
	m.mu.RUnlock()
	return out
}

func mergeCatchUpEvents(archived, live []model.Event) []model.Event {
	seenSeq := map[int64]struct{}{}
	seenKey := map[string]struct{}{}
	out := make([]model.Event, 0, len(archived)+len(live))
	add := func(event model.Event) {
		if event.Sequence > 0 {
			if _, ok := seenSeq[event.Sequence]; ok {
				return
			}
		}
		key := event.SentAt.UTC().Format(time.RFC3339Nano) + "|" + strconv.FormatInt(eventSequenceID(event), 10)
		if _, ok := seenKey[key]; ok {
			return
		}
		if event.Sequence > 0 {
			seenSeq[event.Sequence] = struct{}{}
		}
		seenKey[key] = struct{}{}
		out = append(out, event)
	}
	for _, event := range archived {
		add(event)
	}
	for _, event := range live {
		add(event)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Sequence > 0 && out[j].Sequence > 0 && out[i].Sequence != out[j].Sequence {
			return out[i].Sequence < out[j].Sequence
		}
		if !out[i].SentAt.Equal(out[j].SentAt) {
			return out[i].SentAt.Before(out[j].SentAt)
		}
		return eventSequenceID(out[i]) < eventSequenceID(out[j])
	})
	return out
}

// CatchUpEvents merges durable archive events with the in-process live window
// so SSE reconnects still receive delta/progress that are not stored in MySQL.
func (m *Memory) CatchUpEvents(id string, after *EventCursor, afterSequence int64, limit int) []model.Event {
	if limit <= 0 || limit > taskEventCatchUpLimit {
		limit = taskEventCatchUpLimit
	}
	live := m.liveEventsAfter(id, after, afterSequence)
	archived := make([]model.Event, 0, minInt(limit, 256))
	cursor := after
	for len(archived) < limit {
		page, err := m.ListEventPage(id, cursor, minInt(200, limit-len(archived)), nil, "")
		if err != nil {
			log.Printf("catch-up event page failed: %v", err)
			break
		}
		archived = append(archived, page.Items...)
		if !page.HasMore || page.NextCursor == nil {
			break
		}
		if cursor != nil && page.NextCursor.SentAt.Equal(cursor.SentAt) && page.NextCursor.ID == cursor.ID {
			break
		}
		cursor = page.NextCursor
	}
	merged := mergeCatchUpEvents(archived, live)
	if len(merged) > limit {
		return merged[:limit]
	}
	return merged
}

func eventNodeID(raw any) string {
	metadata, ok := raw.(map[string]any)
	if !ok {
		return ""
	}
	value, _ := metadata["node_id"].(string)
	return strings.TrimSpace(value)
}

func eventSequenceID(event model.Event) int64 {
	if event.ID > 0 {
		return event.ID
	}
	return event.Sequence
}

func selectProjectMemoryEvents(events []model.Event, limit int) []model.Event {
	selected := make([]model.Event, 0, minInt(len(events), limit))
	for index := len(events) - 1; index >= 0 && len(selected) < limit; index-- {
		event := events[index]
		switch event.Type {
		case "completed", "failed", "tool_updated", "goal_checkpoint", "goal_completed", "plan_updated", "compaction_completed":
			selected = append(selected, event)
		}
	}
	for left, right := 0, len(selected)-1; left < right; left, right = left+1, right-1 {
		selected[left], selected[right] = selected[right], selected[left]
	}
	return selected
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func (m *Memory) LastEventAt(id string) (time.Time, bool) {
	m.mu.RLock()
	src := m.events[id]
	if len(src) > 0 {
		var latest time.Time
		for _, evt := range src {
			if evt.SentAt.After(latest) {
				latest = evt.SentAt
			}
		}
		m.mu.RUnlock()
		if !latest.IsZero() {
			return latest.UTC(), true
		}
	} else {
		m.mu.RUnlock()
	}

	latest, ok, err := m.archive.LastEventAt(id)
	if err != nil {
		log.Printf("last event archive failed: %v", err)
		return time.Time{}, false
	}
	if ok {
		return latest.UTC(), true
	}
	return time.Time{}, false
}

func (m *Memory) LatestTerminalEvent(id string) (model.Event, bool) {
	terminal := func(eventType string) bool {
		switch eventType {
		case "completed", "failed", "cancelled":
			return true
		default:
			return false
		}
	}

	m.mu.RLock()
	src := m.events[id]
	for i := len(src) - 1; i >= 0; i-- {
		if terminal(src[i].Type) {
			evt := src[i]
			m.mu.RUnlock()
			return evt, true
		}
	}
	m.mu.RUnlock()

	if reader, ok := m.archive.(TaskTerminalEventReader); ok {
		event, found, err := reader.LatestTerminalEvent(id)
		if err != nil {
			log.Printf("latest terminal event query failed: %v", err)
		}
		return event, found
	}
	archived, err := m.archive.ListEvents(id)
	if err != nil {
		log.Printf("list terminal event archive failed: %v", err)
		return model.Event{}, false
	}
	for i := len(archived) - 1; i >= 0; i-- {
		if terminal(archived[i].Type) {
			return archived[i], true
		}
	}
	return model.Event{}, false
}

func (m *Memory) SubagentEvents(id string, limit int) []model.Event {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	m.mu.RLock()
	live := append([]model.Event(nil), m.events[id]...)
	m.mu.RUnlock()
	if len(live) > 0 {
		filtered := make([]model.Event, 0, limit)
		for index := len(live) - 1; index >= 0 && len(filtered) < limit; index-- {
			switch live[index].Type {
			case "subagent_state", "subagent_started", "subagent_result", "subagent_control_requested", "subagent_control_applied":
				filtered = append(filtered, live[index])
			}
		}
		for left, right := 0, len(filtered)-1; left < right; left, right = left+1, right-1 {
			filtered[left], filtered[right] = filtered[right], filtered[left]
		}
		return filtered
	}
	if reader, ok := m.archive.(TaskSubagentStateReader); ok {
		events, err := reader.ListLatestSubagentEvents(id, limit)
		if err == nil {
			return events
		}
		log.Printf("latest subagent event query failed: %v", err)
		return nil
	}
	types := []string{"subagent_state", "subagent_started", "subagent_result", "subagent_control_requested", "subagent_control_applied"}
	page, err := m.ListEventPage(id, nil, limit, types, "")
	if err != nil {
		log.Printf("subagent event query failed: %v", err)
		return nil
	}
	return page.Items
}

func (m *Memory) ListTasks(filter model.TaskFilter) ([]*model.Task, error) {
	if tasks, err := m.archive.ListTasks(filter); err == nil && tasks != nil {
		return tasks, nil
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]*model.Task, 0, len(m.tasks))
	for _, task := range m.tasks {
		if filter.OperatorID > 0 && task.OperatorID != filter.OperatorID {
			continue
		}
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
		if !filter.AfterUpdatedAt.IsZero() && (task.UpdatedAt.Before(filter.AfterUpdatedAt) ||
			(task.UpdatedAt.Equal(filter.AfterUpdatedAt) && task.ID <= filter.AfterUpdatedTaskID)) {
			continue
		}
		if filter.Status != "" && task.Status != filter.Status {
			continue
		}
		if !filter.BeforeCreatedAt.IsZero() {
			if task.CreatedAt.After(filter.BeforeCreatedAt) ||
				task.CreatedAt.Equal(filter.BeforeCreatedAt) &&
					(filter.BeforeTaskID == "" || task.ID >= filter.BeforeTaskID) {
				continue
			}
		}
		if !filter.AfterCreatedAt.IsZero() {
			if task.CreatedAt.Before(filter.AfterCreatedAt) ||
				task.CreatedAt.Equal(filter.AfterCreatedAt) &&
					(filter.AfterTaskID == "" || task.ID <= filter.AfterTaskID) {
				continue
			}
		}
		out = append(out, clone(task))
	}

	if filter.CreatedAsc {
		slices.SortFunc(out, func(a, b *model.Task) int {
			switch {
			case a.CreatedAt.Before(b.CreatedAt):
				return -1
			case a.CreatedAt.After(b.CreatedAt):
				return 1
			case a.ID < b.ID:
				return -1
			case a.ID > b.ID:
				return 1
			default:
				return 0
			}
		})
	} else if filter.CreatedDesc {
		slices.SortFunc(out, func(a, b *model.Task) int {
			switch {
			case a.CreatedAt.After(b.CreatedAt):
				return -1
			case a.CreatedAt.Before(b.CreatedAt):
				return 1
			case a.ID > b.ID:
				return -1
			case a.ID < b.ID:
				return 1
			default:
				return 0
			}
		})
	} else {
		slices.SortFunc(out, func(a, b *model.Task) int {
			switch {
			case model.TaskStatusPriority(a.Status) < model.TaskStatusPriority(b.Status):
				return -1
			case model.TaskStatusPriority(a.Status) > model.TaskStatusPriority(b.Status):
				return 1
			case a.UpdatedAt.After(b.UpdatedAt):
				return -1
			case a.UpdatedAt.Before(b.UpdatedAt):
				return 1
			case a.CreatedAt.After(b.CreatedAt):
				return -1
			case a.CreatedAt.Before(b.CreatedAt):
				return 1
			default:
				return 0
			}
		})
	}
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
		if filter.OperatorID > 0 && session.OperatorID != filter.OperatorID {
			continue
		}
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

// Health checks the durable archive when it exposes a database ping. The
// in-memory test archive is considered ready by default.
func (m *Memory) Health(ctx context.Context) error {
	if checker, ok := m.archive.(healthChecker); ok {
		return checker.PingContext(ctx)
	}
	return nil
}

func (m *Memory) GetOperatorByKey(operatorKey string) (*model.Operator, error) {
	return m.archive.GetOperatorByKey(operatorKey)
}

func (m *Memory) GetOperatorEmail(operatorID int64) (string, error) {
	return m.archive.GetOperatorEmail(operatorID)
}

// ListOperators 返回全部用户，供管理后台展示与调整会员等级。
func (m *Memory) ListOperators() ([]model.Operator, error) {
	return m.archive.ListOperators()
}

// GetOperatorMembershipTier 读取会员等级，未知值按 free 处理。
func (m *Memory) GetOperatorMembershipTier(operatorID int64) (string, error) {
	tier, err := m.archive.GetOperatorMembershipTier(operatorID)
	if err != nil {
		return model.MembershipTierFree, err
	}
	return model.NormalizeMembershipTier(tier), nil
}

// SetOperatorMembershipTier 更新会员等级。
func (m *Memory) SetOperatorMembershipTier(operatorID int64, tier string) error {
	return m.archive.SetOperatorMembershipTier(operatorID, model.NormalizeMembershipTier(tier))
}

func (m *Memory) UsernameExists(username string) (bool, error) {
	return m.archive.UsernameExists(username)
}

func (m *Memory) EmailExists(email string) (bool, error) {
	return m.archive.EmailExists(email)
}

func (m *Memory) CreateOperator(username, password, email, name string) (*model.Operator, error) {
	return m.archive.CreateOperator(username, password, email, name)
}

func (m *Memory) ChangePassword(operatorID int64, newPassword string) error {
	return m.archive.ChangePassword(operatorID, newPassword)
}

func (m *Memory) DeleteOperator(operatorID int64) error {
	if err := m.archive.DeleteOperator(operatorID); err != nil {
		return err
	}
	m.mu.Lock()
	prefix := strconv.FormatInt(operatorID, 10) + "\x00"
	for key := range m.appNotifications {
		if strings.HasPrefix(key, prefix) {
			delete(m.appNotifications, key)
		}
	}
	delete(m.appNotificationChanges, operatorID)
	delete(m.appNotificationVersions, operatorID)
	delete(m.devicePreferences, operatorID)
	delete(m.agentPreferences, operatorID)
	m.mu.Unlock()
	return nil
}

func (m *Memory) EnsureDevicePreferences(operatorID int64, machineIDs []string) ([]model.DevicePreference, error) {
	ids := uniqueNonEmptyStrings(machineIDs)
	if len(ids) == 0 {
		return []model.DevicePreference{}, nil
	}
	if archive, ok := m.archive.(DevicePreferenceArchive); ok {
		preferences, err := archive.EnsureDevicePreferences(operatorID, ids)
		if err != nil {
			return nil, err
		}
		m.mu.Lock()
		if m.devicePreferences[operatorID] == nil {
			m.devicePreferences[operatorID] = map[string]model.DevicePreference{}
		}
		for _, preference := range preferences {
			m.devicePreferences[operatorID][preference.MachineID] = preference
		}
		m.mu.Unlock()
		return preferences, nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.devicePreferences[operatorID] == nil {
		m.devicePreferences[operatorID] = map[string]model.DevicePreference{}
	}
	nextOrder := int64(1)
	for _, preference := range m.devicePreferences[operatorID] {
		if preference.SortOrder >= nextOrder {
			nextOrder = preference.SortOrder + 1
		}
	}
	now := time.Now()
	result := make([]model.DevicePreference, 0, len(ids))
	for _, machineID := range ids {
		preference, exists := m.devicePreferences[operatorID][machineID]
		if !exists {
			preference = model.DevicePreference{
				OperatorID: operatorID,
				MachineID:  machineID,
				SortOrder:  nextOrder,
				CreatedAt:  now,
				UpdatedAt:  now,
			}
			nextOrder++
			m.devicePreferences[operatorID][machineID] = preference
		}
		result = append(result, preference)
	}
	return result, nil
}

func (m *Memory) UpdateDeviceDisplayName(operatorID int64, machineID, displayName string) (*model.DevicePreference, error) {
	machineID = strings.TrimSpace(machineID)
	if archive, ok := m.archive.(DevicePreferenceArchive); ok {
		preference, err := archive.UpdateDeviceDisplayName(operatorID, machineID, displayName)
		if err != nil {
			return nil, err
		}
		m.mu.Lock()
		if m.devicePreferences[operatorID] == nil {
			m.devicePreferences[operatorID] = map[string]model.DevicePreference{}
		}
		m.devicePreferences[operatorID][machineID] = *preference
		m.mu.Unlock()
		return preference, nil
	}

	if _, err := m.EnsureDevicePreferences(operatorID, []string{machineID}); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	preference := m.devicePreferences[operatorID][machineID]
	preference.DisplayName = displayName
	preference.UpdatedAt = time.Now()
	m.devicePreferences[operatorID][machineID] = preference
	return &preference, nil
}

func (m *Memory) ReorderDevicePreferences(operatorID int64, machineIDs []string) ([]model.DevicePreference, error) {
	ids := uniqueNonEmptyStrings(machineIDs)
	if archive, ok := m.archive.(DevicePreferenceArchive); ok {
		preferences, err := archive.ReorderDevicePreferences(operatorID, ids)
		if err != nil {
			return nil, err
		}
		m.mu.Lock()
		if m.devicePreferences[operatorID] == nil {
			m.devicePreferences[operatorID] = map[string]model.DevicePreference{}
		}
		for _, preference := range preferences {
			m.devicePreferences[operatorID][preference.MachineID] = preference
		}
		m.mu.Unlock()
		return preferences, nil
	}
	if _, err := m.EnsureDevicePreferences(operatorID, ids); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	byMachine := m.devicePreferences[operatorID]
	ordered := append([]string(nil), ids...)
	included := make(map[string]bool, len(ordered))
	for _, machineID := range ordered {
		included[machineID] = true
	}
	remainder := make([]model.DevicePreference, 0, len(byMachine))
	for machineID, preference := range byMachine {
		if !included[machineID] {
			remainder = append(remainder, preference)
		}
	}
	slices.SortFunc(remainder, func(a, b model.DevicePreference) int {
		if a.SortOrder < b.SortOrder {
			return -1
		}
		if a.SortOrder > b.SortOrder {
			return 1
		}
		return strings.Compare(a.MachineID, b.MachineID)
	})
	for _, preference := range remainder {
		ordered = append(ordered, preference.MachineID)
	}
	now := time.Now()
	result := make([]model.DevicePreference, 0, len(ordered))
	for index, machineID := range ordered {
		preference := byMachine[machineID]
		preference.SortOrder = int64(index + 1)
		preference.UpdatedAt = now
		byMachine[machineID] = preference
		result = append(result, preference)
	}
	return result, nil
}

func (m *Memory) ListAgentPreferences(operatorID int64, machineIDs []string) ([]model.AgentPreference, error) {
	ids := uniqueNonEmptyStrings(machineIDs)
	if archive, ok := m.archive.(AgentPreferenceArchive); ok {
		preferences, err := archive.ListAgentPreferences(operatorID, ids)
		if err != nil {
			return nil, err
		}
		m.mu.Lock()
		if m.agentPreferences[operatorID] == nil {
			m.agentPreferences[operatorID] = map[string]model.AgentPreference{}
		}
		for _, preference := range preferences {
			m.agentPreferences[operatorID][agentPreferenceKey(preference.MachineID, preference.AgentID)] = preference
		}
		m.mu.Unlock()
		return preferences, nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	machines := make(map[string]bool, len(ids))
	for _, machineID := range ids {
		machines[machineID] = true
	}
	result := make([]model.AgentPreference, 0)
	for _, preference := range m.agentPreferences[operatorID] {
		if machines[preference.MachineID] {
			result = append(result, preference)
		}
	}
	return result, nil
}

func (m *Memory) ReorderAgentPreferences(operatorID int64, machineID string, agentIDs []string) ([]model.AgentPreference, error) {
	ids := uniqueNonEmptyStrings(agentIDs)
	if archive, ok := m.archive.(AgentPreferenceArchive); ok {
		preferences, err := archive.ReorderAgentPreferences(operatorID, machineID, ids)
		if err != nil {
			return nil, err
		}
		m.mu.Lock()
		if m.agentPreferences[operatorID] == nil {
			m.agentPreferences[operatorID] = map[string]model.AgentPreference{}
		}
		for key, preference := range m.agentPreferences[operatorID] {
			if preference.MachineID == machineID {
				delete(m.agentPreferences[operatorID], key)
			}
		}
		for _, preference := range preferences {
			m.agentPreferences[operatorID][agentPreferenceKey(machineID, preference.AgentID)] = preference
		}
		m.mu.Unlock()
		return preferences, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.agentPreferences[operatorID] == nil {
		m.agentPreferences[operatorID] = map[string]model.AgentPreference{}
	}
	for key, preference := range m.agentPreferences[operatorID] {
		if preference.MachineID == machineID {
			delete(m.agentPreferences[operatorID], key)
		}
	}
	now := time.Now()
	result := make([]model.AgentPreference, 0, len(ids))
	for index, agentID := range ids {
		preference := model.AgentPreference{
			OperatorID: operatorID,
			MachineID:  machineID,
			AgentID:    agentID,
			SortOrder:  int64(index + 1),
			CreatedAt:  now,
			UpdatedAt:  now,
		}
		m.agentPreferences[operatorID][agentPreferenceKey(machineID, agentID)] = preference
		result = append(result, preference)
	}
	return result, nil
}

func agentPreferenceKey(machineID, agentID string) string {
	return machineID + "\x00" + agentID
}

func uniqueNonEmptyStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func (m *Memory) SetDeviceSetting(operatorID int64, agentID, key, value string) error {
	m.mu.Lock()
	if _, ok := m.deviceSettings[operatorID]; !ok {
		m.deviceSettings[operatorID] = map[string]map[string]string{}
	}
	if _, ok := m.deviceSettings[operatorID][agentID]; !ok {
		m.deviceSettings[operatorID][agentID] = map[string]string{}
	}
	m.deviceSettings[operatorID][agentID][key] = value
	m.mu.Unlock()
	return m.archive.SetDeviceSetting(operatorID, agentID, key, value)
}

func (m *Memory) GetDeviceSettings(operatorID int64, agentID string) (map[string]string, error) {
	if settings, err := m.archive.GetDeviceSettings(operatorID, agentID); err == nil && settings != nil {
		m.mu.Lock()
		if _, ok := m.deviceSettings[operatorID]; !ok {
			m.deviceSettings[operatorID] = map[string]map[string]string{}
		}
		m.deviceSettings[operatorID][agentID] = cloneStringMap(settings)
		m.mu.Unlock()
		return cloneStringMap(settings), nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if settings := m.deviceSettings[operatorID][agentID]; settings != nil {
		return cloneStringMap(settings), nil
	}
	return nil, nil
}

func (m *Memory) DeleteDeviceSettings(operatorID int64, agentID string) error {
	m.mu.Lock()
	if byAgent, ok := m.deviceSettings[operatorID]; ok {
		delete(byAgent, agentID)
		if len(byAgent) == 0 {
			delete(m.deviceSettings, operatorID)
		}
	}
	m.mu.Unlock()
	return m.archive.DeleteDeviceSettings(operatorID, agentID)
}

func (m *Memory) ListDeviceSettingAgentIDs(operatorID int64, key string) ([]string, error) {
	if ids, err := m.archive.ListDeviceSettingAgentIDs(operatorID, key); err == nil && len(ids) > 0 {
		return ids, nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	byAgent := m.deviceSettings[operatorID]
	if len(byAgent) == 0 {
		return nil, nil
	}
	result := make([]string, 0, len(byAgent))
	for agentID, settings := range byAgent {
		if strings.EqualFold(strings.TrimSpace(settings[key]), "true") {
			result = append(result, agentID)
		}
	}
	slices.Sort(result)
	return result, nil
}

func (m *Memory) UpsertPushDevice(device model.PushDevice) (*model.PushDevice, error) {
	if stored, err := m.archive.UpsertPushDevice(device); err == nil && stored != nil {
		m.mu.Lock()
		if _, ok := m.pushDevices[stored.OperatorID]; !ok {
			m.pushDevices[stored.OperatorID] = map[int64]*model.PushDevice{}
		}
		m.pushDevices[stored.OperatorID][stored.ID] = clonePushDevice(stored)
		m.mu.Unlock()
		return clonePushDevice(stored), nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	if device.CreatedAt.IsZero() {
		device.CreatedAt = now
	}
	device.UpdatedAt = now
	device.LastSeenAt = now
	device.Enabled = true
	if _, ok := m.pushDevices[device.OperatorID]; !ok {
		m.pushDevices[device.OperatorID] = map[int64]*model.PushDevice{}
	}
	for id, existing := range m.pushDevices[device.OperatorID] {
		if existing.DeviceID != "" && existing.DeviceID == device.DeviceID {
			device.ID = id
			break
		}
		if existing.RegistrationID == device.RegistrationID {
			device.ID = id
			break
		}
	}
	if device.ID == 0 {
		device.ID = now.UnixNano()
	}
	m.pushDevices[device.OperatorID][device.ID] = clonePushDevice(&device)
	return clonePushDevice(&device), nil
}

func (m *Memory) ListPushDevices(operatorID int64) ([]*model.PushDevice, error) {
	if devices, err := m.archive.ListPushDevices(operatorID); err == nil && devices != nil {
		return devices, nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	src := m.pushDevices[operatorID]
	out := make([]*model.PushDevice, 0, len(src))
	for _, device := range src {
		out = append(out, clonePushDevice(device))
	}
	slices.SortFunc(out, func(a, b *model.PushDevice) int {
		switch {
		case a.UpdatedAt.After(b.UpdatedAt):
			return -1
		case a.UpdatedAt.Before(b.UpdatedAt):
			return 1
		default:
			return 0
		}
	})
	return out, nil
}

func (m *Memory) ListSkills(includeDisabled bool) ([]model.Skill, error) {
	if items, err := m.archive.ListSkills(includeDisabled); err == nil && items != nil {
		m.mu.Lock()
		m.skills = map[string]model.Skill{}
		for _, item := range items {
			m.skills[item.ID] = cloneSkill(item)
		}
		m.mu.Unlock()
		return cloneSkills(items), nil
	} else if err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]model.Skill, 0, len(m.skills))
	for _, item := range m.skills {
		if !includeDisabled && !item.Enabled {
			continue
		}
		next := normalizeSkillForStore(cloneSkill(item))
		out = append(out, next)
	}
	slices.SortFunc(out, compareSkills)
	return out, nil
}

func (m *Memory) UpsertSkill(item model.Skill) (*model.Skill, error) {
	item = normalizeSkillForStore(item)
	if stored, err := m.archive.UpsertSkill(item); err == nil && stored != nil {
		m.mu.Lock()
		m.skills[stored.ID] = cloneSkill(*stored)
		m.mu.Unlock()
		out := cloneSkill(*stored)
		return &out, nil
	} else if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	current, ok := m.skills[item.ID]
	item = cloneSkill(item)
	if ok {
		item.CreatedAt = current.CreatedAt
	} else {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	m.skills[item.ID] = item
	out := cloneSkill(item)
	return &out, nil
}

func (m *Memory) DeleteSkill(id string) error {
	if err := m.archive.DeleteSkill(id); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.skills, id)
	for key, agent := range m.semanticAgents {
		agent.SkillIDs = removeString(agent.SkillIDs, id)
		agent.Skills = removeString(agent.Skills, id)
		agent.SkillDefinitions = removeSemanticAgentSkillDefinition(agent.SkillDefinitions, id)
		m.semanticAgents[key] = agent
	}
	return nil
}

func (m *Memory) ListMCPCatalog(includeDisabled bool) ([]model.MCPCatalogItem, error) {
	if items, err := m.archive.ListMCPCatalog(includeDisabled); err == nil && items != nil {
		m.mu.Lock()
		m.mcpCatalog = map[string]model.MCPCatalogItem{}
		for _, item := range items {
			m.mcpCatalog[item.ID] = cloneMCPCatalogItem(item)
		}
		m.mu.Unlock()
		return cloneMCPCatalogItems(items), nil
	} else if err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]model.MCPCatalogItem, 0, len(m.mcpCatalog))
	for _, item := range m.mcpCatalog {
		if !includeDisabled && !item.Enabled {
			continue
		}
		next := normalizeMCPCatalogItemForStore(cloneMCPCatalogItem(item))
		out = append(out, next)
	}
	slices.SortFunc(out, compareMCPCatalogItems)
	return out, nil
}

func (m *Memory) UpsertMCPCatalogItem(item model.MCPCatalogItem) (*model.MCPCatalogItem, error) {
	item = normalizeMCPCatalogItemForStore(item)
	if stored, err := m.archive.UpsertMCPCatalogItem(item); err == nil && stored != nil {
		m.mu.Lock()
		m.mcpCatalog[stored.ID] = cloneMCPCatalogItem(*stored)
		m.mu.Unlock()
		out := cloneMCPCatalogItem(*stored)
		return &out, nil
	} else if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	current, ok := m.mcpCatalog[item.ID]
	item = cloneMCPCatalogItem(item)
	if ok {
		item.CreatedAt = current.CreatedAt
	} else {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	m.mcpCatalog[item.ID] = item
	out := cloneMCPCatalogItem(item)
	return &out, nil
}

func (m *Memory) DeleteMCPCatalogItem(id string) error {
	var aliases []string
	m.mu.RLock()
	if item, ok := m.mcpCatalog[id]; ok {
		aliases = mcpCatalogAliases(item)
	}
	m.mu.RUnlock()
	if err := m.archive.DeleteMCPCatalogItem(id); err != nil {
		return err
	}
	persistentArchive := !isNoopArchive(m.archive)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.mcpCatalog[id]; !ok && !persistentArchive {
		return sql.ErrNoRows
	}
	if len(aliases) == 0 {
		aliases = []string{id}
	}
	delete(m.mcpCatalog, id)
	for key, agent := range m.semanticAgents {
		agent.MCPIDs = removeStrings(agent.MCPIDs, aliases)
		agent.RecommendedMCPServers = removeStrings(agent.RecommendedMCPServers, aliases)
		agent.RecommendedMCPConfigs = removeMCPConfigs(agent.RecommendedMCPConfigs, aliases)
		m.semanticAgents[key] = agent
	}
	return nil
}

func (m *Memory) ListEnvironmentPresets(includeDisabled bool) ([]model.EnvironmentPreset, error) {
	if items, err := m.archive.ListEnvironmentPresets(includeDisabled); err == nil && items != nil {
		m.mu.Lock()
		m.envPresets = map[string]model.EnvironmentPreset{}
		for _, item := range items {
			m.envPresets[item.ID] = cloneEnvironmentPreset(item)
		}
		m.mu.Unlock()
		return cloneEnvironmentPresets(items), nil
	} else if err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]model.EnvironmentPreset, 0, len(m.envPresets))
	for _, item := range m.envPresets {
		if !includeDisabled && !item.Enabled {
			continue
		}
		out = append(out, cloneEnvironmentPreset(item))
	}
	slices.SortFunc(out, compareEnvironmentPresets)
	return out, nil
}

func (m *Memory) UpsertEnvironmentPreset(item model.EnvironmentPreset) (*model.EnvironmentPreset, error) {
	if stored, err := m.archive.UpsertEnvironmentPreset(item); err == nil && stored != nil {
		m.mu.Lock()
		m.envPresets[stored.ID] = cloneEnvironmentPreset(*stored)
		m.mu.Unlock()
		out := cloneEnvironmentPreset(*stored)
		return &out, nil
	} else if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	current, ok := m.envPresets[item.ID]
	item = cloneEnvironmentPreset(item)
	if ok {
		item.CreatedAt = current.CreatedAt
	} else {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	m.envPresets[item.ID] = item
	out := cloneEnvironmentPreset(item)
	return &out, nil
}

func (m *Memory) DeleteEnvironmentPreset(id string) error {
	if err := m.archive.DeleteEnvironmentPreset(id); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.envPresets, id)
	return nil
}

func (m *Memory) ListRuntimeCatalog(includeDisabled bool) ([]model.RuntimeCatalogItem, error) {
	if items, err := m.archive.ListRuntimeCatalog(includeDisabled); err == nil && items != nil {
		m.mu.Lock()
		m.runtimeCatalog = map[string]model.RuntimeCatalogItem{}
		for _, item := range items {
			m.runtimeCatalog[item.ID] = cloneRuntimeCatalogItem(item)
		}
		m.mu.Unlock()
		return cloneRuntimeCatalogItems(items), nil
	} else if err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]model.RuntimeCatalogItem, 0, len(m.runtimeCatalog))
	for _, item := range m.runtimeCatalog {
		if !includeDisabled && !item.Enabled {
			continue
		}
		out = append(out, cloneRuntimeCatalogItem(item))
	}
	slices.SortFunc(out, compareRuntimeCatalogItems)
	return out, nil
}

func (m *Memory) UpsertRuntimeCatalogItem(item model.RuntimeCatalogItem) (*model.RuntimeCatalogItem, error) {
	if stored, err := m.archive.UpsertRuntimeCatalogItem(item); err == nil && stored != nil {
		m.mu.Lock()
		m.runtimeCatalog[stored.ID] = cloneRuntimeCatalogItem(*stored)
		m.mu.Unlock()
		out := cloneRuntimeCatalogItem(*stored)
		return &out, nil
	} else if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	current, ok := m.runtimeCatalog[item.ID]
	item = cloneRuntimeCatalogItem(item)
	if ok {
		item.CreatedAt = current.CreatedAt
	} else {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	m.runtimeCatalog[item.ID] = item
	out := cloneRuntimeCatalogItem(item)
	return &out, nil
}

func (m *Memory) DeleteRuntimeCatalogItem(id string) error {
	if err := m.archive.DeleteRuntimeCatalogItem(id); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.runtimeCatalog, id)
	for key, agent := range m.semanticAgents {
		agent.RuntimeRequirements = removeRuntimeRequirement(agent.RuntimeRequirements, id)
		m.semanticAgents[key] = agent
	}
	return nil
}

func (m *Memory) ListRuntimeVersions(includeDisabled bool) ([]model.RuntimeVersion, error) {
	if items, err := m.archive.ListRuntimeVersions(includeDisabled); err == nil && items != nil {
		m.mu.Lock()
		m.runtimeVersions = map[string]model.RuntimeVersion{}
		for _, item := range items {
			m.runtimeVersions[item.ID] = cloneRuntimeVersion(item)
		}
		m.mu.Unlock()
		return cloneRuntimeVersions(items), nil
	} else if err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]model.RuntimeVersion, 0, len(m.runtimeVersions))
	for _, item := range m.runtimeVersions {
		if !includeDisabled && !item.Enabled {
			continue
		}
		out = append(out, cloneRuntimeVersion(item))
	}
	slices.SortFunc(out, compareRuntimeVersions)
	return out, nil
}

func (m *Memory) UpsertRuntimeVersion(item model.RuntimeVersion) (*model.RuntimeVersion, error) {
	if stored, err := m.archive.UpsertRuntimeVersion(item); err == nil && stored != nil {
		m.mu.Lock()
		m.runtimeVersions[stored.ID] = cloneRuntimeVersion(*stored)
		m.mu.Unlock()
		out := cloneRuntimeVersion(*stored)
		return &out, nil
	} else if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	current, ok := m.runtimeVersions[item.ID]
	item = cloneRuntimeVersion(item)
	if ok {
		item.CreatedAt = current.CreatedAt
	} else {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	m.runtimeVersions[item.ID] = item
	out := cloneRuntimeVersion(item)
	return &out, nil
}

func (m *Memory) DeleteRuntimeVersion(id string) error {
	if err := m.archive.DeleteRuntimeVersion(id); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.runtimeVersions, id)
	for key, artifact := range m.runtimeArtifacts {
		if artifact.VersionID == id {
			delete(m.runtimeArtifacts, key)
		}
	}
	for key, mirror := range m.runtimeMirrors {
		if mirror.VersionID == id {
			delete(m.runtimeMirrors, key)
		}
	}
	return nil
}

func (m *Memory) ListRuntimeArtifacts(includeDisabled bool) ([]model.RuntimeArtifact, error) {
	if items, err := m.archive.ListRuntimeArtifacts(includeDisabled); err == nil && items != nil {
		m.mu.Lock()
		m.runtimeArtifacts = map[string]model.RuntimeArtifact{}
		for _, item := range items {
			m.runtimeArtifacts[item.ID] = cloneRuntimeArtifact(item)
		}
		m.mu.Unlock()
		return cloneRuntimeArtifacts(items), nil
	} else if err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]model.RuntimeArtifact, 0, len(m.runtimeArtifacts))
	for _, item := range m.runtimeArtifacts {
		if !includeDisabled && !item.Enabled {
			continue
		}
		out = append(out, cloneRuntimeArtifact(item))
	}
	slices.SortFunc(out, compareRuntimeArtifacts)
	return out, nil
}

func (m *Memory) UpsertRuntimeArtifact(item model.RuntimeArtifact) (*model.RuntimeArtifact, error) {
	if stored, err := m.archive.UpsertRuntimeArtifact(item); err == nil && stored != nil {
		m.mu.Lock()
		m.runtimeArtifacts[stored.ID] = cloneRuntimeArtifact(*stored)
		m.mu.Unlock()
		out := cloneRuntimeArtifact(*stored)
		return &out, nil
	} else if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	current, ok := m.runtimeArtifacts[item.ID]
	item = cloneRuntimeArtifact(item)
	if ok {
		item.CreatedAt = current.CreatedAt
	} else {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	m.runtimeArtifacts[item.ID] = item
	out := cloneRuntimeArtifact(item)
	return &out, nil
}

func (m *Memory) DeleteRuntimeArtifact(id string) error {
	if err := m.archive.DeleteRuntimeArtifact(id); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.runtimeArtifacts, id)
	return nil
}

func (m *Memory) ListRuntimeMirrors(includeDisabled bool) ([]model.RuntimeMirror, error) {
	if items, err := m.archive.ListRuntimeMirrors(includeDisabled); err == nil && items != nil {
		m.mu.Lock()
		m.runtimeMirrors = map[string]model.RuntimeMirror{}
		for _, item := range items {
			m.runtimeMirrors[item.ID] = cloneRuntimeMirror(item)
		}
		m.mu.Unlock()
		return cloneRuntimeMirrors(items), nil
	} else if err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]model.RuntimeMirror, 0, len(m.runtimeMirrors))
	for _, item := range m.runtimeMirrors {
		if !includeDisabled && !item.Enabled {
			continue
		}
		out = append(out, cloneRuntimeMirror(item))
	}
	slices.SortFunc(out, compareRuntimeMirrors)
	return out, nil
}

func (m *Memory) UpsertRuntimeMirror(item model.RuntimeMirror) (*model.RuntimeMirror, error) {
	if stored, err := m.archive.UpsertRuntimeMirror(item); err == nil && stored != nil {
		m.mu.Lock()
		m.runtimeMirrors[stored.ID] = cloneRuntimeMirror(*stored)
		m.mu.Unlock()
		out := cloneRuntimeMirror(*stored)
		return &out, nil
	} else if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	current, ok := m.runtimeMirrors[item.ID]
	item = cloneRuntimeMirror(item)
	if ok {
		item.CreatedAt = current.CreatedAt
	} else {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	m.runtimeMirrors[item.ID] = item
	out := cloneRuntimeMirror(item)
	return &out, nil
}

func (m *Memory) DeleteRuntimeMirror(id string) error {
	if err := m.archive.DeleteRuntimeMirror(id); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.runtimeMirrors, id)
	return nil
}

func (m *Memory) ListToolCatalog(includeDisabled bool) ([]model.ToolCatalogItem, error) {
	if items, err := m.archive.ListToolCatalog(includeDisabled); err == nil && items != nil {
		m.mu.Lock()
		m.toolCatalog = map[string]model.ToolCatalogItem{}
		for _, item := range items {
			m.toolCatalog[item.ID] = cloneToolCatalogItem(item)
		}
		m.mu.Unlock()
		return cloneToolCatalogItems(items), nil
	} else if err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]model.ToolCatalogItem, 0, len(m.toolCatalog))
	for _, item := range m.toolCatalog {
		if !includeDisabled && !item.Enabled {
			continue
		}
		out = append(out, cloneToolCatalogItem(item))
	}
	slices.SortFunc(out, compareToolCatalogItems)
	return out, nil
}

func (m *Memory) UpsertToolCatalogItem(item model.ToolCatalogItem) (*model.ToolCatalogItem, error) {
	if stored, err := m.archive.UpsertToolCatalogItem(item); err == nil && stored != nil {
		m.mu.Lock()
		m.toolCatalog[stored.ID] = cloneToolCatalogItem(*stored)
		m.mu.Unlock()
		out := cloneToolCatalogItem(*stored)
		return &out, nil
	} else if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	current, ok := m.toolCatalog[item.ID]
	item = cloneToolCatalogItem(item)
	if ok {
		item.CreatedAt = current.CreatedAt
	} else {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	m.toolCatalog[item.ID] = item
	out := cloneToolCatalogItem(item)
	return &out, nil
}

func (m *Memory) DeleteToolCatalogItem(id string) error {
	var tool model.ToolCatalogItem
	var hasTool bool
	m.mu.RLock()
	if item, ok := m.toolCatalog[id]; ok {
		tool = cloneToolCatalogItem(item)
		hasTool = true
	}
	m.mu.RUnlock()
	if err := m.archive.DeleteToolCatalogItem(id); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.toolCatalog, id)
	if hasTool && !hasEquivalentToolPermission(m.toolCatalog, tool) {
		for key, agent := range m.semanticAgents {
			permissions, changed := removeToolPermission(agent.ToolPermissions, tool)
			if !changed {
				continue
			}
			agent.ToolPermissions = permissions
			m.semanticAgents[key] = agent
		}
	}
	return nil
}

func (m *Memory) ListSemanticAgents(includeDisabled bool) ([]model.SemanticAgentProfile, error) {
	if items, err := m.archive.ListSemanticAgents(includeDisabled); err == nil && items != nil {
		m.mu.Lock()
		m.semanticAgents = map[string]model.SemanticAgentProfile{}
		for _, item := range items {
			m.semanticAgents[item.ID] = cloneSemanticAgentProfile(item)
		}
		m.mu.Unlock()
		return cloneSemanticAgentProfiles(items), nil
	} else if err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]model.SemanticAgentProfile, 0, len(m.semanticAgents))
	for _, item := range m.semanticAgents {
		if !includeDisabled && !item.Enabled {
			continue
		}
		out = append(out, cloneSemanticAgentProfile(item))
	}
	slices.SortFunc(out, compareSemanticAgents)
	return out, nil
}

func (m *Memory) UpsertSemanticAgent(item model.SemanticAgentProfile) (*model.SemanticAgentProfile, error) {
	if stored, err := m.archive.UpsertSemanticAgent(item); err == nil && stored != nil {
		m.mu.Lock()
		m.semanticAgents[stored.ID] = cloneSemanticAgentProfile(*stored)
		m.mu.Unlock()
		out := cloneSemanticAgentProfile(*stored)
		return &out, nil
	} else if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	current, ok := m.semanticAgents[item.ID]
	item = cloneSemanticAgentProfile(item)
	if ok {
		item.CreatedAt = current.CreatedAt
	} else {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	m.semanticAgents[item.ID] = item
	out := cloneSemanticAgentProfile(item)
	return &out, nil
}

func (m *Memory) DeleteSemanticAgent(id string) error {
	if err := m.archive.DeleteSemanticAgent(id); err != nil {
		return err
	}
	persistentArchive := !isNoopArchive(m.archive)
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.semanticAgents[id]; !ok && !persistentArchive {
		return sql.ErrNoRows
	}
	delete(m.semanticAgents, id)
	return nil
}

func isNoopArchive(archive TaskArchive) bool {
	_, ok := archive.(noopArchive)
	return ok
}

func (m *Memory) UpsertRuntimeInstallJob(job model.RuntimeInstallJob) (*model.RuntimeInstallJob, error) {
	if archive, ok := m.archive.(RuntimePreflightArchive); ok {
		stored, err := archive.UpsertRuntimeInstallJob(job)
		if err != nil {
			return nil, err
		}
		if stored != nil {
			m.mu.Lock()
			m.runtimeJobs[stored.ID] = cloneRuntimeInstallJob(*stored)
			m.mu.Unlock()
			out := cloneRuntimeInstallJob(*stored)
			return &out, nil
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	current, ok := m.runtimeJobs[job.ID]
	job = cloneRuntimeInstallJob(job)
	if ok {
		job.CreatedAt = current.CreatedAt
		if !current.StartedAt.IsZero() && job.StartedAt.IsZero() {
			job.StartedAt = current.StartedAt
		}
	} else {
		job.CreatedAt = now
	}
	job.UpdatedAt = now
	m.runtimeJobs[job.ID] = job
	out := cloneRuntimeInstallJob(job)
	out.Items = cloneRuntimeInstallJobItems(m.runtimeJobItems[job.ID])
	out.LatestEvents = cloneRuntimeInstallEvents(m.runtimeEvents[job.ID])
	return &out, nil
}

func (m *Memory) GetRuntimeInstallJob(operatorID int64, jobID string) (*model.RuntimeInstallJob, bool) {
	if archive, ok := m.archive.(RuntimePreflightArchive); ok {
		stored, err := archive.GetRuntimeInstallJob(operatorID, jobID)
		if err == nil && stored != nil {
			return stored, true
		}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	job, ok := m.runtimeJobs[jobID]
	if !ok || (operatorID > 0 && job.OperatorID != operatorID) {
		return nil, false
	}
	out := cloneRuntimeInstallJob(job)
	out.Items = cloneRuntimeInstallJobItems(m.runtimeJobItems[jobID])
	out.LatestEvents = cloneRuntimeInstallEvents(m.runtimeEvents[jobID])
	return &out, true
}

func (m *Memory) ListRuntimeInstallJobs(operatorID int64, machineID string, limit int) ([]model.RuntimeInstallJob, error) {
	if archive, ok := m.archive.(RuntimePreflightArchive); ok {
		items, err := archive.ListRuntimeInstallJobs(operatorID, machineID, limit)
		if err != nil {
			return nil, err
		}
		if items != nil {
			return cloneRuntimeInstallJobs(items), nil
		}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]model.RuntimeInstallJob, 0, len(m.runtimeJobs))
	for _, job := range m.runtimeJobs {
		if operatorID > 0 && job.OperatorID != operatorID {
			continue
		}
		if strings.TrimSpace(machineID) != "" && job.MachineID != machineID {
			continue
		}
		job = cloneRuntimeInstallJob(job)
		job.Items = cloneRuntimeInstallJobItems(m.runtimeJobItems[job.ID])
		out = append(out, job)
	}
	slices.SortFunc(out, func(left, right model.RuntimeInstallJob) int {
		return right.CreatedAt.Compare(left.CreatedAt)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *Memory) ReplaceRuntimeInstallJobItems(jobID string, items []model.RuntimeInstallJobItem) error {
	if archive, ok := m.archive.(RuntimePreflightArchive); ok {
		if err := archive.ReplaceRuntimeInstallJobItems(jobID, items); err != nil {
			return err
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now().UTC()
	cleaned := make([]model.RuntimeInstallJobItem, 0, len(items))
	for _, item := range items {
		item.JobID = jobID
		if item.Status == "" {
			item.Status = "pending"
		}
		item.UpdatedAt = now
		cleaned = append(cleaned, cloneRuntimeInstallJobItem(item))
	}
	m.runtimeJobItems[jobID] = cleaned
	return nil
}

func (m *Memory) AppendRuntimeInstallEvent(event model.RuntimeInstallEvent) (*model.RuntimeInstallEvent, error) {
	if archive, ok := m.archive.(RuntimePreflightArchive); ok {
		stored, err := archive.AppendRuntimeInstallEvent(event)
		if err != nil {
			return nil, err
		}
		if stored != nil {
			m.mu.Lock()
			m.runtimeEvents[stored.JobID] = append(m.runtimeEvents[stored.JobID], cloneRuntimeInstallEvent(*stored))
			if stored.Sequence > m.runtimeSeq[stored.JobID] {
				m.runtimeSeq[stored.JobID] = stored.Sequence
			}
			m.mu.Unlock()
			out := cloneRuntimeInstallEvent(*stored)
			return &out, nil
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	event = cloneRuntimeInstallEvent(event)
	if event.Sequence <= 0 {
		m.runtimeSeq[event.JobID]++
		event.Sequence = m.runtimeSeq[event.JobID]
	} else if event.Sequence > m.runtimeSeq[event.JobID] {
		m.runtimeSeq[event.JobID] = event.Sequence
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	m.runtimeEvents[event.JobID] = append(m.runtimeEvents[event.JobID], event)
	out := cloneRuntimeInstallEvent(event)
	return &out, nil
}

func (m *Memory) ListRuntimeInstallEvents(jobID string, afterSequence int64) ([]model.RuntimeInstallEvent, error) {
	if archive, ok := m.archive.(RuntimePreflightArchive); ok {
		items, err := archive.ListRuntimeInstallEvents(jobID, afterSequence)
		if err != nil {
			return nil, err
		}
		if items != nil {
			return cloneRuntimeInstallEvents(items), nil
		}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]model.RuntimeInstallEvent, 0, len(m.runtimeEvents[jobID]))
	for _, event := range m.runtimeEvents[jobID] {
		if event.Sequence <= afterSequence {
			continue
		}
		out = append(out, cloneRuntimeInstallEvent(event))
	}
	slices.SortFunc(out, func(left, right model.RuntimeInstallEvent) int {
		if left.Sequence < right.Sequence {
			return -1
		}
		if left.Sequence > right.Sequence {
			return 1
		}
		return 0
	})
	return out, nil
}

func (m *Memory) ClaimRuntimeUserActionNotification(operatorID int64, jobID, actionID, channel string) (bool, error) {
	if archive, ok := m.archive.(RuntimePreflightArchive); ok {
		return archive.ClaimRuntimeUserActionNotification(operatorID, jobID, actionID, channel)
	}
	jobID = strings.TrimSpace(jobID)
	actionID = strings.TrimSpace(actionID)
	channel = strings.TrimSpace(channel)
	if operatorID <= 0 || jobID == "" || actionID == "" || channel == "" {
		return false, errors.New("用户操作通知缺少幂等键")
	}
	key := fmt.Sprintf("%d\x00%s\x00%s\x00%s", operatorID, jobID, actionID, channel)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.runtimeActionNotices[key] {
		return false, nil
	}
	m.runtimeActionNotices[key] = true
	return true, nil
}

func compareSemanticAgents(left model.SemanticAgentProfile, right model.SemanticAgentProfile) int {
	if left.SortOrder != right.SortOrder {
		return left.SortOrder - right.SortOrder
	}
	return strings.Compare(left.Name, right.Name)
}

func compareSkills(left model.Skill, right model.Skill) int {
	if left.SortOrder != right.SortOrder {
		return left.SortOrder - right.SortOrder
	}
	return strings.Compare(left.Name, right.Name)
}

func compareMCPCatalogItems(left model.MCPCatalogItem, right model.MCPCatalogItem) int {
	if left.SortOrder != right.SortOrder {
		return left.SortOrder - right.SortOrder
	}
	if left.Recommended != right.Recommended {
		if left.Recommended {
			return -1
		}
		return 1
	}
	return strings.Compare(left.Title, right.Title)
}

func compareEnvironmentPresets(left model.EnvironmentPreset, right model.EnvironmentPreset) int {
	if left.SortOrder != right.SortOrder {
		return left.SortOrder - right.SortOrder
	}
	if left.Category != right.Category {
		return strings.Compare(left.Category, right.Category)
	}
	return strings.Compare(left.Name, right.Name)
}

func compareRuntimeCatalogItems(left model.RuntimeCatalogItem, right model.RuntimeCatalogItem) int {
	if left.SortOrder != right.SortOrder {
		return left.SortOrder - right.SortOrder
	}
	return strings.Compare(left.Name, right.Name)
}

func compareRuntimeVersions(left model.RuntimeVersion, right model.RuntimeVersion) int {
	if left.RuntimeID != right.RuntimeID {
		return strings.Compare(left.RuntimeID, right.RuntimeID)
	}
	if left.DefaultSelected != right.DefaultSelected {
		if left.DefaultSelected {
			return -1
		}
		return 1
	}
	if left.VersionOrder != right.VersionOrder {
		return left.VersionOrder - right.VersionOrder
	}
	if left.SortOrder != right.SortOrder {
		return left.SortOrder - right.SortOrder
	}
	return strings.Compare(left.Version, right.Version)
}

func compareRuntimeArtifacts(left model.RuntimeArtifact, right model.RuntimeArtifact) int {
	if left.RuntimeID != right.RuntimeID {
		return strings.Compare(left.RuntimeID, right.RuntimeID)
	}
	if left.VersionID != right.VersionID {
		return strings.Compare(left.VersionID, right.VersionID)
	}
	if left.Priority != right.Priority {
		return left.Priority - right.Priority
	}
	if left.Platform != right.Platform {
		return strings.Compare(left.Platform, right.Platform)
	}
	if left.Arch != right.Arch {
		return strings.Compare(left.Arch, right.Arch)
	}
	return strings.Compare(left.Filename, right.Filename)
}

func compareRuntimeMirrors(left model.RuntimeMirror, right model.RuntimeMirror) int {
	if left.RuntimeID != right.RuntimeID {
		return strings.Compare(left.RuntimeID, right.RuntimeID)
	}
	if left.VersionID != right.VersionID {
		return strings.Compare(left.VersionID, right.VersionID)
	}
	if left.Priority != right.Priority {
		return left.Priority - right.Priority
	}
	return strings.Compare(left.Name, right.Name)
}

func compareToolCatalogItems(left model.ToolCatalogItem, right model.ToolCatalogItem) int {
	if left.SortOrder != right.SortOrder {
		return left.SortOrder - right.SortOrder
	}
	if left.Category != right.Category {
		return strings.Compare(left.Category, right.Category)
	}
	return strings.Compare(left.Name, right.Name)
}

func cloneSemanticAgentProfiles(input []model.SemanticAgentProfile) []model.SemanticAgentProfile {
	if input == nil {
		return nil
	}
	out := make([]model.SemanticAgentProfile, 0, len(input))
	for _, item := range input {
		out = append(out, cloneSemanticAgentProfile(item))
	}
	return out
}

func cloneSemanticAgentProfile(item model.SemanticAgentProfile) model.SemanticAgentProfile {
	item.SkillIDs = append([]string(nil), item.SkillIDs...)
	item.MCPIDs = append([]string(nil), item.MCPIDs...)
	item.Skills = append([]string(nil), item.Skills...)
	item.RecommendedMCPServers = append([]string(nil), item.RecommendedMCPServers...)
	item.RecommendedMCPConfigs = append([]model.DeviceMCPServer(nil), item.RecommendedMCPConfigs...)
	item.MCPDependencies = cloneSemanticAgentMCPDependencies(item.MCPDependencies)
	item.ToolPermissions = cloneAnyMapStore(item.ToolPermissions)
	item.SkillDefinitions = cloneSemanticAgentSkills(item.SkillDefinitions)
	item.RuntimeRequirements = cloneSemanticAgentRuntimes(item.RuntimeRequirements)
	return item
}

func cloneSemanticAgentMCPDependencies(input []model.SemanticAgentMCPDependency) []model.SemanticAgentMCPDependency {
	if input == nil {
		return nil
	}
	out := make([]model.SemanticAgentMCPDependency, 0, len(input))
	for _, item := range input {
		item.Config.Headers = cloneStringMap(item.Config.Headers)
		item.Config.Command = append([]string(nil), item.Config.Command...)
		item.Config.Environment = cloneStringMap(item.Config.Environment)
		item.Install.Assets = cloneMCPInstallAssets(item.Install.Assets)
		item.Install.Files = cloneMCPInstallFiles(item.Install.Files)
		item.Install.InstallCommands = append([]string(nil), item.Install.InstallCommands...)
		item.Install.BuildCommands = append([]string(nil), item.Install.BuildCommands...)
		item.Install.RunCommandTemplate = append([]string(nil), item.Install.RunCommandTemplate...)
		item.Install.ExecutableNames = append([]string(nil), item.Install.ExecutableNames...)
		item.Install.Environment = cloneStringMap(item.Install.Environment)
		out = append(out, item)
	}
	return out
}

func cloneSkills(input []model.Skill) []model.Skill {
	if input == nil {
		return nil
	}
	out := make([]model.Skill, 0, len(input))
	for _, item := range input {
		out = append(out, cloneSkill(item))
	}
	return out
}

func cloneSkill(item model.Skill) model.Skill {
	item.Tags = append([]string(nil), item.Tags...)
	item.PackageFiles = cloneSkillPackageFiles(item.PackageFiles)
	return item
}

func cloneSemanticAgentSkills(input []model.SemanticAgentSkill) []model.SemanticAgentSkill {
	if input == nil {
		return nil
	}
	out := make([]model.SemanticAgentSkill, 0, len(input))
	for _, item := range input {
		item.PackageFiles = cloneSkillPackageFiles(item.PackageFiles)
		out = append(out, item)
	}
	return out
}

func cloneMCPCatalogItems(input []model.MCPCatalogItem) []model.MCPCatalogItem {
	if input == nil {
		return nil
	}
	out := make([]model.MCPCatalogItem, 0, len(input))
	for _, item := range input {
		out = append(out, cloneMCPCatalogItem(item))
	}
	return out
}

func cloneMCPCatalogItem(item model.MCPCatalogItem) model.MCPCatalogItem {
	item.Tags = append([]string(nil), item.Tags...)
	item.Config.Headers = cloneStringMap(item.Config.Headers)
	item.Config.Command = append([]string(nil), item.Config.Command...)
	item.Config.Environment = cloneStringMap(item.Config.Environment)
	item.Install.Assets = cloneMCPInstallAssets(item.Install.Assets)
	item.Install.Files = cloneMCPInstallFiles(item.Install.Files)
	item.Install.InstallCommands = append([]string(nil), item.Install.InstallCommands...)
	item.Install.BuildCommands = append([]string(nil), item.Install.BuildCommands...)
	item.Install.RunCommandTemplate = append([]string(nil), item.Install.RunCommandTemplate...)
	item.Install.ExecutableNames = append([]string(nil), item.Install.ExecutableNames...)
	item.Install.Environment = cloneStringMap(item.Install.Environment)
	return item
}

func cloneMCPInstallAssets(input []model.MCPInstallAsset) []model.MCPInstallAsset {
	if input == nil {
		return nil
	}
	out := make([]model.MCPInstallAsset, 0, len(input))
	for _, item := range input {
		item.Headers = cloneStringMap(item.Headers)
		out = append(out, item)
	}
	return out
}

func cloneMCPInstallFiles(input []model.MCPInstallFile) []model.MCPInstallFile {
	if input == nil {
		return nil
	}
	return append([]model.MCPInstallFile(nil), input...)
}

func cloneEnvironmentPresets(input []model.EnvironmentPreset) []model.EnvironmentPreset {
	if input == nil {
		return nil
	}
	out := make([]model.EnvironmentPreset, 0, len(input))
	for _, item := range input {
		out = append(out, cloneEnvironmentPreset(item))
	}
	return out
}

func cloneEnvironmentPreset(item model.EnvironmentPreset) model.EnvironmentPreset {
	item.Tags = append([]string(nil), item.Tags...)
	item.Variables = cloneStringMap(item.Variables)
	return item
}

func cloneRuntimeCatalogItems(input []model.RuntimeCatalogItem) []model.RuntimeCatalogItem {
	if input == nil {
		return nil
	}
	out := make([]model.RuntimeCatalogItem, 0, len(input))
	for _, item := range input {
		out = append(out, cloneRuntimeCatalogItem(item))
	}
	return out
}

func cloneRuntimeCatalogItem(item model.RuntimeCatalogItem) model.RuntimeCatalogItem {
	item.ExecutableNames = append([]string(nil), item.ExecutableNames...)
	item.EnvTemplate = cloneStringMap(item.EnvTemplate)
	item.Tags = append([]string(nil), item.Tags...)
	item.Versions = cloneRuntimeVersions(item.Versions)
	item.Artifacts = cloneRuntimeArtifacts(item.Artifacts)
	item.Mirrors = cloneRuntimeMirrors(item.Mirrors)
	return item
}

func cloneRuntimeVersions(input []model.RuntimeVersion) []model.RuntimeVersion {
	if input == nil {
		return nil
	}
	out := make([]model.RuntimeVersion, 0, len(input))
	for _, item := range input {
		out = append(out, cloneRuntimeVersion(item))
	}
	return out
}

func cloneRuntimeVersion(item model.RuntimeVersion) model.RuntimeVersion {
	item.MinOSVersion = cloneStringMap(item.MinOSVersion)
	item.InstallManifest = cloneAnyMapStore(item.InstallManifest)
	return item
}

func cloneRuntimeArtifacts(input []model.RuntimeArtifact) []model.RuntimeArtifact {
	if input == nil {
		return nil
	}
	out := make([]model.RuntimeArtifact, 0, len(input))
	for _, item := range input {
		out = append(out, cloneRuntimeArtifact(item))
	}
	return out
}

func cloneRuntimeArtifact(item model.RuntimeArtifact) model.RuntimeArtifact {
	item.BinPaths = append([]string(nil), item.BinPaths...)
	item.EnvPatch = cloneStringMap(item.EnvPatch)
	return item
}

func cloneRuntimeMirrors(input []model.RuntimeMirror) []model.RuntimeMirror {
	if input == nil {
		return nil
	}
	out := make([]model.RuntimeMirror, 0, len(input))
	for _, item := range input {
		out = append(out, cloneRuntimeMirror(item))
	}
	return out
}

func cloneRuntimeMirror(item model.RuntimeMirror) model.RuntimeMirror {
	item.Headers = cloneStringMap(item.Headers)
	item.Tags = append([]string(nil), item.Tags...)
	return item
}

func cloneSemanticAgentRuntimes(input []model.SemanticAgentRuntime) []model.SemanticAgentRuntime {
	return append([]model.SemanticAgentRuntime(nil), input...)
}

func cloneRuntimeInstallJobs(input []model.RuntimeInstallJob) []model.RuntimeInstallJob {
	if input == nil {
		return nil
	}
	out := make([]model.RuntimeInstallJob, 0, len(input))
	for _, item := range input {
		out = append(out, cloneRuntimeInstallJob(item))
	}
	return out
}

func cloneRuntimeInstallJob(item model.RuntimeInstallJob) model.RuntimeInstallJob {
	if item.UserAction != nil {
		cloned := *item.UserAction
		cloned.Instructions = append([]string(nil), item.UserAction.Instructions...)
		item.UserAction = &cloned
	}
	item.Items = cloneRuntimeInstallJobItems(item.Items)
	item.LatestEvents = cloneRuntimeInstallEvents(item.LatestEvents)
	return item
}

func cloneRuntimeInstallJobItems(input []model.RuntimeInstallJobItem) []model.RuntimeInstallJobItem {
	if input == nil {
		return nil
	}
	return append([]model.RuntimeInstallJobItem(nil), input...)
}

func cloneRuntimeInstallJobItem(item model.RuntimeInstallJobItem) model.RuntimeInstallJobItem {
	return item
}

func cloneRuntimeInstallEvents(input []model.RuntimeInstallEvent) []model.RuntimeInstallEvent {
	if input == nil {
		return nil
	}
	out := make([]model.RuntimeInstallEvent, 0, len(input))
	for _, item := range input {
		out = append(out, cloneRuntimeInstallEvent(item))
	}
	return out
}

func cloneRuntimeInstallEvent(item model.RuntimeInstallEvent) model.RuntimeInstallEvent {
	item.Details = cloneAnyMapStore(item.Details)
	return item
}

func cloneToolCatalogItems(input []model.ToolCatalogItem) []model.ToolCatalogItem {
	if input == nil {
		return nil
	}
	out := make([]model.ToolCatalogItem, 0, len(input))
	for _, item := range input {
		out = append(out, cloneToolCatalogItem(item))
	}
	return out
}

func cloneToolCatalogItem(item model.ToolCatalogItem) model.ToolCatalogItem {
	item.Tags = append([]string(nil), item.Tags...)
	return item
}

func removeString(input []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" || len(input) == 0 {
		return input
	}
	out := make([]string, 0, len(input))
	for _, item := range input {
		if item == value {
			continue
		}
		out = append(out, item)
	}
	return out
}

func removeStrings(input []string, values []string) []string {
	if len(input) == 0 || len(values) == 0 {
		return input
	}
	remove := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			remove[value] = true
		}
	}
	if len(remove) == 0 {
		return input
	}
	out := make([]string, 0, len(input))
	for _, item := range input {
		if remove[item] {
			continue
		}
		out = append(out, item)
	}
	return out
}

func removeSemanticAgentSkillDefinition(input []model.SemanticAgentSkill, skillID string) []model.SemanticAgentSkill {
	skillID = strings.TrimSpace(skillID)
	if skillID == "" || len(input) == 0 {
		return input
	}
	out := make([]model.SemanticAgentSkill, 0, len(input))
	for _, item := range input {
		if item.Name == skillID {
			continue
		}
		out = append(out, item)
	}
	return out
}

func removeRuntimeRequirement(input []model.SemanticAgentRuntime, runtimeID string) []model.SemanticAgentRuntime {
	runtimeID = strings.TrimSpace(runtimeID)
	if runtimeID == "" || len(input) == 0 {
		return input
	}
	out := make([]model.SemanticAgentRuntime, 0, len(input))
	for _, item := range input {
		if item.RuntimeID == runtimeID {
			continue
		}
		out = append(out, item)
	}
	return out
}

func removeMCPConfigs(input []model.DeviceMCPServer, aliases []string) []model.DeviceMCPServer {
	if len(input) == 0 || len(aliases) == 0 {
		return input
	}
	remove := map[string]bool{}
	for _, value := range aliases {
		value = strings.TrimSpace(value)
		if value != "" {
			remove[value] = true
		}
	}
	if len(remove) == 0 {
		return input
	}
	out := make([]model.DeviceMCPServer, 0, len(input))
	for _, item := range input {
		if remove[item.Name] {
			continue
		}
		out = append(out, item)
	}
	return out
}

func removeToolPermission(input map[string]any, tool model.ToolCatalogItem) (map[string]any, bool) {
	key := strings.TrimSpace(tool.PermissionKey)
	pattern := strings.TrimSpace(tool.Pattern)
	if pattern == "" {
		pattern = "*"
	}
	if key == "" || input == nil {
		return input, false
	}
	value, ok := input[key]
	if !ok {
		return input, false
	}
	out := cloneAnyMapStore(input)
	switch nested := value.(type) {
	case string:
		if pattern != "*" {
			return input, false
		}
		delete(out, key)
		return out, true
	case map[string]any:
		nextNested := cloneAnyMapStore(nested)
		if _, ok := nextNested[pattern]; !ok {
			return input, false
		}
		delete(nextNested, pattern)
		if len(nextNested) == 0 {
			delete(out, key)
		} else {
			out[key] = nextNested
		}
		return out, true
	case map[string]string:
		nextNested := make(map[string]any, len(nested))
		for nestedKey, nestedValue := range nested {
			nextNested[nestedKey] = nestedValue
		}
		if _, ok := nextNested[pattern]; !ok {
			return input, false
		}
		delete(nextNested, pattern)
		if len(nextNested) == 0 {
			delete(out, key)
		} else {
			out[key] = nextNested
		}
		return out, true
	default:
		if pattern != "*" {
			return input, false
		}
		delete(out, key)
		return out, true
	}
}

func hasEquivalentToolPermission(items map[string]model.ToolCatalogItem, tool model.ToolCatalogItem) bool {
	key := strings.TrimSpace(tool.PermissionKey)
	pattern := strings.TrimSpace(tool.Pattern)
	if pattern == "" {
		pattern = "*"
	}
	if key == "" {
		return false
	}
	for _, item := range items {
		itemPattern := strings.TrimSpace(item.Pattern)
		if itemPattern == "" {
			itemPattern = "*"
		}
		if strings.TrimSpace(item.PermissionKey) == key && itemPattern == pattern {
			return true
		}
	}
	return false
}

func mcpCatalogAliases(item model.MCPCatalogItem) []string {
	return cleanAliasList([]string{
		item.ID,
		item.Name,
		item.Title,
		item.Config.Name,
	})
}

func cleanAliasList(input []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(input))
	for _, value := range input {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func cloneAnyMapStore(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	out := make(map[string]any, len(input))
	for key, value := range input {
		if nested, ok := value.(map[string]any); ok {
			out[key] = cloneAnyMapStore(nested)
			continue
		}
		out[key] = value
	}
	return out
}

func (m *Memory) TrackSession(taskID, sessionID, status, summary string) {
	if sessionID == "" || taskID == "" {
		return
	}
	if !m.loadTaskIntoMemory(taskID) {
		return
	}

	var persisted *model.Task
	m.mu.Lock()
	task := m.tasks[taskID]
	if task == nil {
		m.mu.Unlock()
		return
	}
	if task.SessionID != sessionID {
		task.SessionID = sessionID
		task.UpdatedAt = time.Now().UTC()
		persisted = clone(task)
	}
	snapshot := clone(task)
	m.mu.Unlock()

	if persisted != nil {
		m.persistTask(persisted)
	}
	m.touchSession(snapshot, status, summary)
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
			ID:         task.SessionID,
			AgentID:    task.AgentID,
			MachineID:  task.MachineID,
			ProjectID:  task.ProjectID,
			OperatorID: task.OperatorID,
			Status:     status,
			CreatedAt:  now,
			UpdatedAt:  now,
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
	cp.Question = cloneQuestion(task.Question)
	cp.Plan = clonePlan(task.Plan)
	cp.Parts = cloneParts(task.Parts)
	cp.Artifacts = cloneArtifacts(task.Artifacts)
	cp.Metadata = cloneStringMap(task.Metadata)
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

func cloneQuestion(question *model.QuestionRequest) *model.QuestionRequest {
	if question == nil {
		return nil
	}
	cp := *question
	if len(question.Questions) > 0 {
		cp.Questions = make([]model.QuestionItem, len(question.Questions))
		for i, item := range question.Questions {
			cp.Questions[i] = item
			if len(item.Options) > 0 {
				cp.Questions[i].Options = append([]model.QuestionOption(nil), item.Options...)
			}
		}
	}
	return &cp
}

func clonePlan(plan *model.Plan) *model.Plan {
	if plan == nil {
		return nil
	}
	cp := *plan
	if len(plan.Items) > 0 {
		cp.Items = append([]model.PlanItem(nil), plan.Items...)
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

func cloneArtifacts(artifacts []model.Artifact) []model.Artifact {
	if len(artifacts) == 0 {
		return nil
	}
	out := make([]model.Artifact, len(artifacts))
	copy(out, artifacts)
	return out
}

func cloneSession(session *model.Session) *model.Session {
	if session == nil {
		return nil
	}
	cp := *session
	return &cp
}

func clonePushDevice(device *model.PushDevice) *model.PushDevice {
	if device == nil {
		return nil
	}
	cp := *device
	return &cp
}
