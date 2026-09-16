package store

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"relay-server/internal/model"
)

type archivedEventArchive struct {
	noopArchive
	task                    *model.Task
	events                  []model.Event
	listEventsCalls         int
	projectMemoryEventCalls int
	latestSequenceCalls     int
	latestTerminalCalls     int
}

func (a *archivedEventArchive) LatestEventSequence(string) (int64, error) {
	a.latestSequenceCalls++
	return 41, nil
}

func (a *archivedEventArchive) LatestTerminalEvent(string) (model.Event, bool, error) {
	a.latestTerminalCalls++
	return model.Event{Type: "completed", Content: "done"}, true, nil
}

func (a *archivedEventArchive) GetTask(taskID string) (*model.Task, error) {
	if a.task == nil || a.task.ID != taskID {
		return nil, nil
	}
	task := *a.task
	return &task, nil
}

func (a *archivedEventArchive) ListEvents(taskID string) ([]model.Event, error) {
	a.listEventsCalls++
	if a.task == nil || a.task.ID != taskID {
		return nil, nil
	}
	out := make([]model.Event, len(a.events))
	copy(out, a.events)
	return out, nil
}

func (a *archivedEventArchive) ListProjectMemoryEvents(taskID string, limit int) ([]model.Event, error) {
	a.projectMemoryEventCalls++
	if a.task == nil || a.task.ID != taskID {
		return nil, nil
	}
	return selectProjectMemoryEvents(a.events, limit), nil
}

func TestEventsLoadsArchivedTaskEventsWithReasoningField(t *testing.T) {
	now := time.Now().UTC()
	archive := &archivedEventArchive{
		task: &model.Task{
			ID:         "task_reasoning",
			AgentID:    "agent_chat_codex",
			MachineID:  "machine_1",
			ProjectID:  "chat-codex",
			Status:     model.TaskCompleted,
			CreatedAt:  now,
			UpdatedAt:  now,
			OperatorID: 1,
		},
		events: []model.Event{
			{
				TaskID:  "task_reasoning",
				Type:    "delta",
				Field:   "reasoning",
				Content: "先分析",
				SentAt:  now,
			},
			{
				TaskID:  "task_reasoning",
				Type:    "delta",
				Field:   "text",
				Content: "最终回答",
				SentAt:  now.Add(time.Millisecond),
			},
			{
				TaskID:  "task_reasoning",
				Type:    "completed",
				Content: "最终回答",
				SentAt:  now.Add(2 * time.Millisecond),
			},
		},
	}
	mem := NewMemory(archive)

	events := mem.Events("task_reasoning")
	if len(events) != 3 {
		t.Fatalf("expected 3 archived events, got %d: %+v", len(events), events)
	}
	if events[0].Type != "delta" || events[0].Field != "reasoning" || events[0].Content != "先分析" {
		t.Fatalf("expected reasoning event to be restored, got %+v", events[0])
	}
	if events[1].Type != "delta" || events[1].Field != "text" || events[1].Content != "最终回答" {
		t.Fatalf("expected text event to be restored, got %+v", events[1])
	}

	events[0].Field = "mutated"
	cached := mem.Events("task_reasoning")
	if cached[0].Field != "reasoning" {
		t.Fatalf("expected Events to return a copy, got %+v", cached[0])
	}
	mem.mu.RLock()
	_, cachedHistory := mem.events["task_reasoning"]
	mem.mu.RUnlock()
	if cachedHistory {
		t.Fatal("historical archive read populated the in-process event cache")
	}
}

func TestTerminalEventReleasesLiveEventWindow(t *testing.T) {
	memory := NewMemory(nil)
	task := memory.CreateTask("agent", "machine", "project", "/tmp/project", "session", nil, nil, 1)
	memory.AddEvent(task.ID, model.Event{TaskID: task.ID, Type: "delta", Content: "working", SentAt: time.Now().UTC()})
	memory.AddEvent(task.ID, model.Event{TaskID: task.ID, Type: "completed", Content: "done", SentAt: time.Now().UTC()})

	memory.mu.RLock()
	_, cached := memory.events[task.ID]
	memory.mu.RUnlock()
	if cached {
		t.Fatal("terminal task retained its live event window")
	}
}

func TestProjectMemoryEventsUsesBoundedArchiveReadWithoutPopulatingEventCache(t *testing.T) {
	now := time.Now().UTC()
	archive := &archivedEventArchive{
		task: &model.Task{ID: "task_memory", Status: model.TaskCompleted, CreatedAt: now, UpdatedAt: now},
		events: []model.Event{
			{TaskID: "task_memory", Type: "delta", Content: "ignored", SentAt: now},
			{TaskID: "task_memory", Type: "completed", Content: "old", SentAt: now.Add(time.Second)},
			{TaskID: "task_memory", Type: "tool_updated", Content: "middle", SentAt: now.Add(2 * time.Second)},
			{TaskID: "task_memory", Type: "goal_completed", Content: "new", SentAt: now.Add(3 * time.Second)},
		},
	}
	memory := NewMemory(archive)

	events := memory.ProjectMemoryEvents("task_memory", 2)
	if len(events) != 2 || events[0].Content != "middle" || events[1].Content != "new" {
		t.Fatalf("unexpected bounded project memory events: %+v", events)
	}
	if archive.projectMemoryEventCalls != 1 || archive.listEventsCalls != 0 {
		t.Fatalf("unexpected archive calls: project_memory=%d full=%d", archive.projectMemoryEventCalls, archive.listEventsCalls)
	}
	memory.mu.RLock()
	cachedCount := len(memory.events["task_memory"])
	_, taskCached := memory.tasks["task_memory"]
	memory.mu.RUnlock()
	if cachedCount != 0 {
		t.Fatalf("project memory read populated the general event cache with %d events", cachedCount)
	}
	if taskCached {
		t.Fatal("project memory event read populated the general task cache")
	}
}

func TestEventCacheKeepsBoundedRecentWindowAndSequences(t *testing.T) {
	memory := NewMemory(nil)
	task := memory.CreateTask("agent", "machine", "project", "/tmp/project", "session", nil, nil, 1)
	for index := 0; index < taskEventHistoryLimit+5; index++ {
		memory.AddEvent(task.ID, model.Event{TaskID: task.ID, Type: "delta", Content: "event", SentAt: time.Now().UTC()})
	}

	events := memory.Events(task.ID)
	if len(events) != taskEventHistoryLimit {
		t.Fatalf("expected bounded event cache of %d, got %d", taskEventHistoryLimit, len(events))
	}
	if events[0].Sequence != 6 || events[len(events)-1].Sequence != int64(taskEventHistoryLimit+5) {
		t.Fatalf("unexpected retained sequence window: first=%d last=%d", events[0].Sequence, events[len(events)-1].Sequence)
	}
	window := memory.EventsAfter(task.ID, int64(taskEventHistoryLimit+2))
	if len(window) != 3 || window[0].Sequence != int64(taskEventHistoryLimit+3) || window[2].Sequence != int64(taskEventHistoryLimit+5) {
		t.Fatalf("unexpected incremental event window: %+v", window)
	}
}

func TestAddEventUsesLatestSequenceWithoutCountingOrLoadingHistory(t *testing.T) {
	now := time.Now().UTC()
	archive := &archivedEventArchive{
		task: &model.Task{ID: "task-sequence", Status: model.TaskRunning, CreatedAt: now, UpdatedAt: now},
	}
	memory := NewMemory(archive)
	// Simulate an active task restored from durable storage. It has no local
	// sequence entry, so the next event must seed from the dedicated query.
	memory.UpdateTask(archive.task)
	memory.AddEvent(archive.task.ID, model.Event{TaskID: archive.task.ID, Type: "delta", Content: "next", SentAt: now})
	events := memory.Events(archive.task.ID)
	if len(events) != 1 || events[0].Sequence != 42 {
		t.Fatalf("unexpected seeded sequence: %+v", events)
	}
	if archive.latestSequenceCalls != 1 {
		t.Fatalf("expected one latest sequence query, got %d", archive.latestSequenceCalls)
	}
	if archive.listEventsCalls != 0 {
		t.Fatalf("expected no full history read, got %d", archive.listEventsCalls)
	}
}

func TestLatestTerminalEventReadsHistoricalTaskWithoutCachingIt(t *testing.T) {
	now := time.Now().UTC()
	archive := &archivedEventArchive{
		task: &model.Task{ID: "task-terminal", Status: model.TaskCompleted, CreatedAt: now, UpdatedAt: now},
	}
	memory := NewMemory(archive)
	event, ok := memory.LatestTerminalEvent(archive.task.ID)
	if !ok || event.Type != "completed" || archive.latestTerminalCalls != 1 {
		t.Fatalf("unexpected historical terminal event=%+v ok=%v calls=%d", event, ok, archive.latestTerminalCalls)
	}
	memory.mu.RLock()
	_, taskCached := memory.tasks[archive.task.ID]
	_, eventsCached := memory.events[archive.task.ID]
	memory.mu.RUnlock()
	if taskCached || eventsCached {
		t.Fatalf("historical terminal lookup populated memory: task=%v events=%v", taskCached, eventsCached)
	}
}

func TestAddEventCanSkipHighFrequencyArchiveWrites(t *testing.T) {
	archive := &blockingEventArchive{
		firstStarted:  make(chan struct{}),
		secondStarted: make(chan struct{}),
		releaseFirst:  make(chan struct{}),
	}
	close(archive.releaseFirst)
	memory := NewMemory(archive)
	task := memory.CreateTask("agent", "machine", "project", "/tmp", "session", nil, nil, 1)
	memory.AddEvent(task.ID, model.Event{TaskID: task.ID, Type: "delta", Content: "skip-me", SentAt: time.Now().UTC()})
	memory.AddEvent(task.ID, model.Event{TaskID: task.ID, Type: "progress", Content: "skip-progress", SentAt: time.Now().UTC()})
	memory.AddEvent(task.ID, model.Event{TaskID: task.ID, Type: "completed", Content: "keep-me", SentAt: time.Now().UTC()})
	archive.mu.Lock()
	order := append([]string(nil), archive.order...)
	archive.mu.Unlock()
	if len(order) != 1 || order[0] != "keep-me" {
		t.Fatalf("high-frequency events were archived: %v", order)
	}
	live := memory.Events(task.ID)
	if len(live) != 3 || live[0].Type != "delta" || live[1].Type != "progress" || live[2].Type != "completed" {
		t.Fatalf("live window should keep skipped high-frequency events: %+v", live)
	}
}

func TestAddEventArchivesHighFrequencyWhenEnabled(t *testing.T) {
	t.Setenv("ARCHIVE_HIGH_FREQ_EVENTS", "1")
	archive := &blockingEventArchive{
		firstStarted:  make(chan struct{}),
		secondStarted: make(chan struct{}),
		releaseFirst:  make(chan struct{}),
	}
	close(archive.releaseFirst)
	memory := NewMemory(archive)
	task := memory.CreateTask("agent", "machine", "project", "/tmp", "session", nil, nil, 1)
	memory.AddEvent(task.ID, model.Event{TaskID: task.ID, Type: "delta", Content: "keep-delta", SentAt: time.Now().UTC()})
	memory.AddEvent(task.ID, model.Event{TaskID: task.ID, Type: "progress", Content: "keep-progress", SentAt: time.Now().UTC()})
	archive.mu.Lock()
	order := append([]string(nil), archive.order...)
	archive.mu.Unlock()
	if len(order) != 2 || order[0] != "keep-delta" || order[1] != "keep-progress" {
		t.Fatalf("enabled high-frequency events were not archived: %v", order)
	}
}

func TestCatchUpEventsMergesLiveDeltasWithArchivedTools(t *testing.T) {
	stamp := time.Now().UTC().Truncate(time.Second)
	archive := &catchUpPageArchive{events: []model.Event{
		{TaskID: "task-catch", Type: "tool_updated", ID: 1, Sequence: 1, SentAt: stamp, Tool: &model.ToolCall{ID: "prt_1", CallID: "c1", Tool: "bash", Status: "running"}},
		{TaskID: "task-catch", Type: "tool_updated", ID: 3, Sequence: 3, SentAt: stamp.Add(2 * time.Second), Tool: &model.ToolCall{ID: "prt_1", CallID: "c1", Tool: "bash", Status: "completed"}},
	}}
	memory := NewMemory(archive)
	task := memory.CreateTask("agent", "machine", "project", "/tmp", "session", nil, nil, 1)
	archive.taskID = task.ID
	for i := range archive.events {
		archive.events[i].TaskID = task.ID
	}
	memory.mu.Lock()
	memory.events[task.ID] = []model.Event{{
		TaskID: task.ID, Type: "delta", Field: "text", Content: "中间正文", ID: 2, Sequence: 2, SentAt: stamp.Add(time.Second),
	}}
	memory.mu.Unlock()

	events := memory.CatchUpEvents(task.ID, &EventCursor{SentAt: stamp, ID: 1}, 1, 100)
	if len(events) != 2 {
		t.Fatalf("expected delta then completed tool, got %d events: %+v", len(events), events)
	}
	if events[0].Type != "delta" || events[0].Content != "中间正文" {
		t.Fatalf("expected live delta first, got %+v", events[0])
	}
	if events[1].Type != "tool_updated" || events[1].Tool == nil || events[1].Tool.Status != "completed" {
		t.Fatalf("expected archived completed tool second, got %+v", events[1])
	}
}

func TestCatchUpEventsKeepsSameSecondToolCompleteAfterDeltaCursor(t *testing.T) {
	stamp := time.Unix(400, 0).UTC()
	archive := &catchUpPageArchive{events: []model.Event{
		{Type: "tool_updated", ID: 1000002, Sequence: 2, SentAt: stamp, Tool: &model.ToolCall{ID: "prt_1", CallID: "c1", Tool: "mcp_call", Status: "running"}},
		{Type: "tool_updated", ID: 1000004, Sequence: 4, SentAt: stamp, Tool: &model.ToolCall{ID: "prt_1", CallID: "c1", Tool: "mcp_call", Status: "completed"}},
	}}
	memory := NewMemory(archive)
	task := memory.CreateTask("agent", "machine", "project", "/tmp", "session", nil, nil, 1)
	archive.taskID = task.ID
	for i := range archive.events {
		archive.events[i].TaskID = task.ID
	}
	memory.mu.Lock()
	memory.events[task.ID] = []model.Event{
		{TaskID: task.ID, Type: "delta", Field: "text", Content: "before", ID: 1, Sequence: 1, SentAt: stamp},
		{TaskID: task.ID, Type: "delta", Field: "text", Content: "after", ID: 3, Sequence: 3, SentAt: stamp},
	}
	memory.mu.Unlock()

	events := memory.CatchUpEvents(task.ID, &EventCursor{SentAt: stamp, ID: 1}, 1, 100)
	if len(events) != 3 {
		t.Fatalf("expected running tool, later delta, completed tool, got %d events: %+v", len(events), events)
	}
	if events[0].Type != "tool_updated" || events[0].Tool == nil || events[0].Tool.Status != "running" {
		t.Fatalf("expected running tool first, got %+v", events[0])
	}
	if events[1].Type != "delta" || events[1].Content != "after" {
		t.Fatalf("expected later delta second, got %+v", events[1])
	}
	if events[2].Type != "tool_updated" || events[2].Tool == nil || events[2].Tool.Status != "completed" {
		t.Fatalf("expected completed tool last, got %+v", events[2])
	}
}

func TestCatchUpEventsKeepsSameSecondDeltaAfterToolCursor(t *testing.T) {
	stamp := time.Unix(401, 0).UTC()
	archive := &catchUpPageArchive{events: []model.Event{
		{Type: "tool_updated", ID: 1000002, Sequence: 2, SentAt: stamp, Tool: &model.ToolCall{ID: "prt_1", CallID: "c1", Tool: "read", Status: "running"}},
		{Type: "tool_updated", ID: 1000004, Sequence: 4, SentAt: stamp, Tool: &model.ToolCall{ID: "prt_1", CallID: "c1", Tool: "read", Status: "completed"}},
	}}
	memory := NewMemory(archive)
	task := memory.CreateTask("agent", "machine", "project", "/tmp", "session", nil, nil, 1)
	archive.taskID = task.ID
	for i := range archive.events {
		archive.events[i].TaskID = task.ID
	}
	memory.mu.Lock()
	memory.events[task.ID] = []model.Event{
		{TaskID: task.ID, Type: "delta", Field: "text", Content: "after-tool", ID: 3, Sequence: 3, SentAt: stamp},
	}
	memory.mu.Unlock()

	events := memory.CatchUpEvents(task.ID, &EventCursor{SentAt: stamp, ID: 1000002}, 2, 100)
	if len(events) != 2 {
		t.Fatalf("expected later delta then completed tool, got %d events: %+v", len(events), events)
	}
	if events[0].Type != "delta" || events[0].Content != "after-tool" {
		t.Fatalf("expected live delta after tool cursor, got %+v", events[0])
	}
	if events[1].Type != "tool_updated" || events[1].Tool == nil || events[1].Tool.Status != "completed" {
		t.Fatalf("expected completed tool after delta, got %+v", events[1])
	}
}

func TestListEventPageUsesStableSentAtAndIDCursor(t *testing.T) {
	memory := NewMemory(nil)
	task := memory.CreateTask("agent", "machine", "project", "/tmp/project", "session", nil, nil, 1)
	stamp := time.Now().UTC().Truncate(time.Second)
	for index := int64(1); index <= 5; index++ {
		memory.AddEvent(task.ID, model.Event{TaskID: task.ID, Type: "delta", Content: string(rune('a' + index - 1)), SentAt: stamp, ID: index, Sequence: index})
	}
	seen := map[int64]int{}
	var ordered []int64
	var cursor *EventCursor
	for pageNum := 0; pageNum < 4; pageNum++ {
		page, err := memory.ListEventPage(task.ID, cursor, 2, nil, "")
		if err != nil {
			t.Fatalf("page %d: %v", pageNum, err)
		}
		for _, event := range page.Items {
			seen[event.ID]++
			ordered = append(ordered, event.ID)
		}
		if !page.HasMore {
			if page.NextCursor == nil {
				t.Fatalf("expected a cursor on the final page, got %+v", page)
			}
			break
		}
		if page.NextCursor == nil {
			t.Fatalf("page %d missing next cursor: %+v", pageNum, page)
		}
		cursor = page.NextCursor
	}
	if len(ordered) != 5 {
		t.Fatalf("expected 5 events across pages, got %v", ordered)
	}
	for index, id := range ordered {
		if id != int64(index+1) || seen[id] != 1 {
			t.Fatalf("cursor pages skipped or duplicated: ordered=%v seen=%v", ordered, seen)
		}
	}
}

func TestSubagentEventsUsesBoundedWindowNotFullLog(t *testing.T) {
	stamp := time.Now().UTC().Truncate(time.Second)
	archive := &subagentWindowArchive{events: make([]model.Event, 0, 260)}
	for index := 1; index <= 200; index++ {
		archive.events = append(archive.events, model.Event{
			TaskID: "task-sub", Type: "delta", Content: "noise", ID: int64(index), SentAt: stamp,
		})
	}
	for index := 1; index <= 50; index++ {
		archive.events = append(archive.events, model.Event{
			TaskID: "task-sub", Type: "subagent_started", ID: int64(200 + index), SentAt: stamp.Add(time.Duration(index) * time.Second),
			Metadata: map[string]any{"node_id": "node-old"},
		})
	}
	for index := 1; index <= 10; index++ {
		archive.events = append(archive.events, model.Event{
			TaskID: "task-sub", Type: "subagent_started", ID: int64(250 + index), SentAt: stamp.Add(time.Duration(50+index) * time.Second),
			Metadata: map[string]any{"node_id": "node-keep"},
		})
	}
	memory := NewMemory(archive)
	events := memory.SubagentEvents("task-sub", 10)
	if archive.listEventsCalls != 0 {
		t.Fatalf("SubagentEvents scanned the full event log %d times", archive.listEventsCalls)
	}
	if archive.latestCalls != 1 {
		t.Fatalf("expected one bounded subagent read, got %d", archive.latestCalls)
	}
	if len(events) != 10 {
		t.Fatalf("expected bounded window of 10, got %d", len(events))
	}
	for _, event := range events {
		if event.Type == "delta" {
			t.Fatalf("bounded subagent window included full-log delta: %+v", event)
		}
		if node, _ := event.Metadata.(map[string]any)["node_id"].(string); node != "node-keep" {
			t.Fatalf("window did not keep the newest subagent events: %+v", event)
		}
	}
}

func TestSubagentEventsSurviveDeltaFlood(t *testing.T) {
	memory := NewMemory(noopArchive{})
	now := time.Now().UTC()
	memory.AddEvent("task-flood", model.Event{
		TaskID: "task-flood", Type: "subagent_started", SentAt: now,
		Metadata: map[string]any{"node_id": "node-live", "subagent_type": "explore"},
	})
	for index := 0; index < taskEventHistoryLimit*2; index++ {
		memory.AddEvent("task-flood", model.Event{
			TaskID: "task-flood", Type: "delta", Content: "token", SentAt: now,
		})
	}
	events := memory.SubagentEvents("task-flood", 200)
	if len(events) != 1 || events[0].Type != "subagent_started" {
		t.Fatalf("delta flood evicted the subagent window: %+v", events)
	}
}

func TestHistoricalEventPageDoesNotPopulateLiveCache(t *testing.T) {
	now := time.Now().UTC()
	archive := &archivedEventArchive{
		task:   &model.Task{ID: "task_page", Status: model.TaskCompleted, CreatedAt: now, UpdatedAt: now},
		events: []model.Event{{TaskID: "task_page", Type: "delta", ID: 1, Sequence: 1, SentAt: now}},
	}
	memory := NewMemory(archive)
	page, err := memory.ListEventPage("task_page", nil, 10, nil, "")
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("unexpected archived page: %+v err=%v", page, err)
	}
	memory.mu.RLock()
	defer memory.mu.RUnlock()
	if len(memory.events["task_page"]) != 0 {
		t.Fatal("historical event page populated live event cache")
	}
}

func TestTerminalTaskIsRemovedFromMemoryWhenDurableArchiveIsPresent(t *testing.T) {
	archive := &pageArchive{task: &model.Task{ID: "durable", Status: model.TaskRunning}}
	memory := NewMemory(archive)
	task := memory.CreateTask("agent", "machine", "project", "/tmp", "session", nil, nil, 1)
	archive.task = task
	memory.AddEvent(task.ID, model.Event{TaskID: task.ID, Type: "completed", SentAt: time.Now().UTC()})
	memory.mu.RLock()
	_, taskCached := memory.tasks[task.ID]
	_, eventCached := memory.events[task.ID]
	memory.mu.RUnlock()
	if taskCached || eventCached {
		t.Fatalf("terminal durable task retained in memory: task=%v events=%v", taskCached, eventCached)
	}
}

func TestAddEventSerializesWritesForTheSameTask(t *testing.T) {
	t.Setenv("ARCHIVE_HIGH_FREQ_EVENTS", "1")
	archive := &blockingEventArchive{
		firstStarted:  make(chan struct{}),
		secondStarted: make(chan struct{}),
		releaseFirst:  make(chan struct{}),
	}
	memory := NewMemory(archive)
	task := memory.CreateTask("agent", "machine", "project", "/tmp", "session", nil, nil, 1)
	var writes sync.WaitGroup
	writes.Add(2)
	go func() {
		defer writes.Done()
		memory.AddEvent(task.ID, model.Event{TaskID: task.ID, Type: "delta", Content: "first", SentAt: time.Now().UTC()})
	}()
	<-archive.firstStarted
	go func() {
		defer writes.Done()
		memory.AddEvent(task.ID, model.Event{TaskID: task.ID, Type: "delta", Content: "second", SentAt: time.Now().UTC()})
	}()
	select {
	case <-archive.secondStarted:
		close(archive.releaseFirst)
		writes.Wait()
		t.Fatal("second event reached the archive before the first event completed")
	case <-time.After(20 * time.Millisecond):
	}
	close(archive.releaseFirst)
	writes.Wait()

	archive.mu.Lock()
	order := append([]string(nil), archive.order...)
	archive.mu.Unlock()
	if len(order) != 2 || order[0] != "first" || order[1] != "second" {
		t.Fatalf("unexpected archive order: %v", order)
	}
	events := memory.Events(task.ID)
	if len(events) != 2 || events[0].ID != 1 || events[1].ID != 2 || events[0].SentAt.Nanosecond() != 0 {
		t.Fatalf("live event ids did not follow durable order: %+v", events)
	}
}

func TestSubscribeCancelIsIdempotent(t *testing.T) {
	memory := NewMemory(nil)
	_, cancel := memory.Subscribe("task")
	cancel()
	cancel()
}

func TestSubscribeDeliversTerminalWhenBufferFull(t *testing.T) {
	memory := NewMemory(nil)
	task := memory.CreateTask("agent", "machine", "project", "/tmp", "session", nil, nil, 1)
	ch, cancel := memory.Subscribe(task.ID)
	defer cancel()

	for index := 0; index < 32; index++ {
		memory.AddEvent(task.ID, model.Event{
			TaskID:  task.ID,
			Type:    "delta",
			Content: fmt.Sprintf("delta-%d", index),
			SentAt:  time.Now().UTC(),
		})
	}

	completedSent := make(chan struct{})
	go func() {
		memory.AddEvent(task.ID, model.Event{
			TaskID:  task.ID,
			Type:    "completed",
			Content: "done",
			SentAt:  time.Now().UTC(),
		})
		close(completedSent)
	}()

	foundCompleted := false
	deadline := time.After(2 * time.Second)
	for !foundCompleted {
		select {
		case evt := <-ch:
			if evt.Type == "completed" {
				foundCompleted = true
			}
		case <-deadline:
			t.Fatal("subscriber did not receive completed while the live buffer was full")
		}
	}
	select {
	case <-completedSent:
	case <-time.After(time.Second):
		t.Fatal("terminal AddEvent stayed blocked after the subscriber received completed")
	}
}

func TestSubscribeDoesNotDropEventsWhenConsumerIsSlow(t *testing.T) {
	memory := NewMemory(nil)
	task := memory.CreateTask("agent", "machine", "project", "/tmp", "session", nil, nil, 1)
	ch, cancel := memory.Subscribe(task.ID)
	defer cancel()

	const total = 200
	for index := 0; index < total; index++ {
		memory.AddEvent(task.ID, model.Event{
			TaskID:  task.ID,
			Type:    "delta",
			Content: fmt.Sprintf("delta-%d", index),
			SentAt:  time.Now().UTC(),
		})
	}
	memory.AddEvent(task.ID, model.Event{
		TaskID:  task.ID,
		Type:    "completed",
		Content: "done",
		SentAt:  time.Now().UTC(),
	})

	got := make([]string, 0, total+1)
	deadline := time.After(3 * time.Second)
	for len(got) < total+1 {
		select {
		case evt, ok := <-ch:
			if !ok {
				t.Fatalf("subscriber closed after %d events, want %d", len(got), total+1)
			}
			if evt.Type == "delta" || evt.Type == "completed" {
				if evt.Type == "completed" {
					got = append(got, "completed")
				} else {
					got = append(got, evt.Content)
				}
			}
		case <-deadline:
			t.Fatalf("timed out after %d/%d events", len(got), total+1)
		}
	}
	if got[0] != "delta-0" {
		t.Fatalf("first event = %q, want delta-0", got[0])
	}
	if got[total-1] != fmt.Sprintf("delta-%d", total-1) {
		t.Fatalf("last delta = %q", got[total-1])
	}
	if got[total] != "completed" {
		t.Fatalf("final event = %q, want completed", got[total])
	}
}

func TestSubscribeCancelUnblocksSlowPump(t *testing.T) {
	memory := NewMemory(nil)
	task := memory.CreateTask("agent", "machine", "project", "/tmp", "session", nil, nil, 1)
	ch, cancel := memory.Subscribe(task.ID)
	for index := 0; index < 8; index++ {
		memory.AddEvent(task.ID, model.Event{
			TaskID:  task.ID,
			Type:    "delta",
			Content: fmt.Sprintf("delta-%d", index),
			SentAt:  time.Now().UTC(),
		})
	}
	cancel()
	deadline := time.After(time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("subscriber channel did not close after cancel")
		}
	}
}

type blockingEventArchive struct {
	noopArchive
	mu            sync.Mutex
	firstStarted  chan struct{}
	secondStarted chan struct{}
	releaseFirst  chan struct{}
	order         []string
}

func (a *blockingEventArchive) AppendEventWithID(event *model.Event) error {
	if event.Content == "first" {
		close(a.firstStarted)
		<-a.releaseFirst
	} else if event.Content == "second" {
		close(a.secondStarted)
	}
	a.mu.Lock()
	a.order = append(a.order, event.Content)
	event.ID = int64(len(a.order))
	event.SentAt = event.SentAt.UTC().Truncate(time.Second)
	a.mu.Unlock()
	return nil
}

type subagentWindowArchive struct {
	noopArchive
	events          []model.Event
	listEventsCalls int
	latestCalls     int
}

func (a *subagentWindowArchive) ListEvents(string) ([]model.Event, error) {
	a.listEventsCalls++
	out := make([]model.Event, len(a.events))
	copy(out, a.events)
	return out, nil
}

func (a *subagentWindowArchive) ListLatestSubagentEvents(_ string, limit int) ([]model.Event, error) {
	a.latestCalls++
	if limit <= 0 {
		limit = 200
	}
	filtered := make([]model.Event, 0, limit)
	for index := len(a.events) - 1; index >= 0 && len(filtered) < limit; index-- {
		switch a.events[index].Type {
		case "subagent_state", "subagent_started", "subagent_result", "subagent_control_requested", "subagent_control_applied":
			filtered = append(filtered, a.events[index])
		}
	}
	for left, right := 0, len(filtered)-1; left < right; left, right = left+1, right-1 {
		filtered[left], filtered[right] = filtered[right], filtered[left]
	}
	return filtered, nil
}

type catchUpPageArchive struct {
	noopArchive
	taskID string
	events []model.Event
}

func (a *catchUpPageArchive) ListEventPage(taskID string, after *EventCursor, limit int, _ []string, _ string) (EventPage, error) {
	if a.taskID != "" && taskID != a.taskID {
		return EventPage{}, nil
	}
	if limit <= 0 {
		limit = 100
	}
	items := make([]model.Event, 0, limit)
	for _, event := range a.events {
		if after != nil && !after.SentAt.IsZero() {
			if event.SentAt.Before(after.SentAt) ||
				(event.SentAt.Equal(after.SentAt) && eventSequenceID(event) <= after.ID) {
				continue
			}
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

type pageArchive struct {
	noopArchive
	task *model.Task
}

func (a *pageArchive) GetTask(id string) (*model.Task, error) {
	if a.task == nil || a.task.ID != id {
		return nil, nil
	}
	task := *a.task
	return &task, nil
}

func (a *pageArchive) ListEventPage(string, *EventCursor, int, []string, string) (EventPage, error) {
	return EventPage{}, nil
}

func (a *pageArchive) AppendEventWithID(event *model.Event) error {
	if event != nil && event.ID == 0 {
		event.ID = 99
	}
	return nil
}
