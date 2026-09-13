package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"relay-server/internal/broker"
	"relay-server/internal/model"
	"relay-server/internal/store"
)

func TestRegisterHelloProjectScopePersistsForkLineage(t *testing.T) {
	memory := store.NewMemory(nil)
	application := &App{store: memory}
	hello := &model.HelloPayload{
		MachineID: "machine-a",
		Projects: []model.HelloProject{{
			ProjectID: "copy", Root: "/project/copy", ScopeID: "scope-copy",
			InstanceNonce: "nonce-copy", LineageScopeID: "scope-original",
		}},
	}
	application.registerHelloProjectScopes(context.Background(), 1, "agent-a", hello)
	scope, err := memory.GetProjectScope(context.Background(), 1, "machine-a", "scope-copy")
	if err != nil || scope == nil {
		t.Fatalf("scope lookup failed: scope=%+v err=%v", scope, err)
	}
	if scope.LineageScopeID != "scope-original" || hello.Projects[0].BindingEpoch != 1 {
		t.Fatalf("fork lineage not persisted: scope=%+v hello=%+v", scope, hello.Projects[0])
	}
}

func TestProjectMemoryCheckpointDoesNotAdvanceCommittedCursor(t *testing.T) {
	memory := store.NewMemory(nil)
	if err := memory.UpsertProjectScope(context.Background(), model.ProjectScope{
		ID: "scope", OperatorID: 1, MachineID: "machine", BindingEpoch: 3,
		Status: model.ProjectScopeActive, Revision: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := memory.CreateProjectMemoryJob(context.Background(), model.ProjectMemoryJob{
		ID: "job", OperatorID: 1, MachineID: "machine", ScopeID: "scope",
		Status: model.ProjectMemoryJobClaimed, LeaseOwner: "worker", LeaseExpiresAt: time.Now().Add(time.Minute),
		FencingToken: 4, CursorEnd: "task-end",
	}); err != nil {
		t.Fatal(err)
	}
	application := &App{store: memory, projectMemoryEnabled: true}
	err := application.handleProjectMemoryWorker(&broker.Device{
		ID: "worker", OperatorID: 1, MachineID: "machine",
	}, "project.memory.checkpoint", model.ProjectMemoryWorkerPayload{
		JobID: "job", ProjectScopeID: "scope", BindingEpoch: 3, FencingToken: 4,
		Cursor: "checkpoint-1", Progress: 62,
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err := memory.GetProjectMemoryJob(context.Background(), 1, "job")
	if err != nil || job == nil {
		t.Fatalf("get checkpointed job: job=%+v err=%v", job, err)
	}
	if job.CursorCommitted != "" || job.CheckpointCursor != "checkpoint-1" || job.CheckpointProgress != 62 || job.CheckpointAt.IsZero() {
		t.Fatalf("checkpoint advanced committed cursor or was not persisted: %+v", job)
	}
}

func TestProjectMemoryCompletionWithoutCommittedCandidatesRetries(t *testing.T) {
	memory := store.NewMemory(nil)
	if err := memory.UpsertProjectScope(context.Background(), model.ProjectScope{
		ID: "scope", OperatorID: 1, MachineID: "machine", BindingEpoch: 3,
		Status: model.ProjectScopeActive, Revision: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := memory.CreateProjectMemoryJob(context.Background(), model.ProjectMemoryJob{
		ID: "job", OperatorID: 1, MachineID: "machine", ScopeID: "scope",
		Trigger: model.ProjectMemoryTriggerManual, Status: model.ProjectMemoryJobRunning,
		LeaseOwner: "worker", LeaseExpiresAt: time.Now().Add(time.Minute),
		FencingToken: 4, Attempt: 1,
	}); err != nil {
		t.Fatal(err)
	}
	application := &App{store: memory, projectMemoryEnabled: true}
	err := application.handleProjectMemoryWorker(&broker.Device{
		ID: "worker", OperatorID: 1, MachineID: "machine",
	}, "project.memory.completed", model.ProjectMemoryWorkerPayload{
		JobID: "job", ProjectScopeID: "scope", BindingEpoch: 3, FencingToken: 4, Progress: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err := memory.GetProjectMemoryJob(context.Background(), 1, "job")
	if err != nil || job == nil {
		t.Fatalf("get retried job: job=%+v err=%v", job, err)
	}
	if job.Status != model.ProjectMemoryJobQueued || job.Error == "" || job.LeaseOwner != "" {
		t.Fatalf("completion without committed candidates was accepted: %+v", job)
	}
	events, err := memory.ListProjectMemoryJobEvents(context.Background(), 1, "job", 0, false)
	if err != nil || len(events) != 1 || events[0].Type != "retrying" {
		t.Fatalf("missing retrying event: events=%+v err=%v", events, err)
	}
}

func TestVisibleProjectMemoryJobContinuesPendingBatchWithSameID(t *testing.T) {
	memory := store.NewMemory(nil)
	if err := memory.UpsertProjectScope(context.Background(), model.ProjectScope{
		ID: "scope", OperatorID: 1, MachineID: "machine", BindingEpoch: 3,
		Status: model.ProjectScopeActive, Revision: 2,
	}); err != nil {
		t.Fatal(err)
	}
	if err := memory.CreateProjectMemoryJob(context.Background(), model.ProjectMemoryJob{
		ID: "job", OperatorID: 1, MachineID: "machine", ScopeID: "scope",
		Trigger: model.ProjectMemoryTriggerManual, Status: model.ProjectMemoryJobRunning,
		CursorEnd: "task-batch", CursorCommitted: "task-batch", PendingCursorEnd: "task-final",
		InputRevision: 1, ResultRevision: 2, ManualVisible: true, Attempt: 1,
		LeaseOwner: "worker", LeaseExpiresAt: time.Now().Add(time.Minute), FencingToken: 4,
	}); err != nil {
		t.Fatal(err)
	}
	application := &App{store: memory, projectMemoryEnabled: true}
	err := application.handleProjectMemoryWorker(&broker.Device{
		ID: "worker", OperatorID: 1, MachineID: "machine",
	}, "project.memory.completed", model.ProjectMemoryWorkerPayload{
		JobID: "job", ProjectScopeID: "scope", BindingEpoch: 3, FencingToken: 4, Progress: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	job, err := memory.GetProjectMemoryJob(context.Background(), 1, "job")
	if err != nil || job == nil {
		t.Fatalf("get continued job: job=%+v err=%v", job, err)
	}
	if job.Status != model.ProjectMemoryJobQueued || job.ID != "job" || job.CursorStart != "task-batch" ||
		job.CursorEnd != "task-final" || job.PendingCursorEnd != "" || job.InputRevision != 2 ||
		job.ResultRevision != 0 || job.Attempt != 0 || job.LeaseOwner != "" {
		t.Fatalf("visible job did not continue in place: %+v", job)
	}
	events, err := memory.ListProjectMemoryJobEvents(context.Background(), 1, "job", 0, false)
	if err != nil || len(events) != 1 || events[0].Type != "queued" || !strings.Contains(events[0].Message, "继续整理") {
		t.Fatalf("missing continuation event: events=%+v err=%v", events, err)
	}
}

func TestProjectMemorySourceRecheckMayCompleteWithoutCandidateBatch(t *testing.T) {
	memory := store.NewMemory(nil)
	if err := memory.UpsertProjectScope(context.Background(), model.ProjectScope{
		ID: "scope", OperatorID: 1, MachineID: "machine", BindingEpoch: 3,
		Status: model.ProjectScopeActive, Revision: 1,
	}); err != nil {
		t.Fatal(err)
	}
	if err := memory.CreateProjectMemoryJob(context.Background(), model.ProjectMemoryJob{
		ID: "job", OperatorID: 1, MachineID: "machine", ScopeID: "scope",
		Trigger: model.ProjectMemoryTriggerSourceRecheck, Status: model.ProjectMemoryJobRunning,
		LeaseOwner: "worker", LeaseExpiresAt: time.Now().Add(time.Minute), FencingToken: 4, Attempt: 1,
	}); err != nil {
		t.Fatal(err)
	}
	application := &App{store: memory, projectMemoryEnabled: true}
	err := application.handleProjectMemoryWorker(&broker.Device{
		ID: "worker", OperatorID: 1, MachineID: "machine",
	}, "project.memory.completed", model.ProjectMemoryWorkerPayload{
		JobID: "job", ProjectScopeID: "scope", BindingEpoch: 3, FencingToken: 4, Progress: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	job, _ := memory.GetProjectMemoryJob(context.Background(), 1, "job")
	if job == nil || job.Status != model.ProjectMemoryJobCompleted {
		t.Fatalf("source recheck did not complete: %+v", job)
	}
}

func TestProjectMemoryAgentSelectionRequiresCapability(t *testing.T) {
	job := model.ProjectMemoryJob{OperatorID: 1, MachineID: "machine", ScopeID: "scope"}
	base := model.Agent{ID: "agent", OperatorID: 1, MachineID: "machine", Projects: []model.HelloProject{{ScopeID: "scope"}}}
	if _, _, ok := projectMemoryAgentForJob(job, []model.Agent{base}); ok {
		t.Fatal("legacy agent without project memory capability was selected")
	}
	base.Capabilities = []string{"project_memory_v1"}
	if _, _, ok := projectMemoryAgentForJob(job, []model.Agent{base}); !ok {
		t.Fatal("capable project memory agent was not selected")
	}
}

func TestProjectMemoryFeatureFlagParsing(t *testing.T) {
	t.Setenv("CHAT_CODEX_PROJECT_MEMORY_ENABLED", "off")
	if envEnabled("CHAT_CODEX_PROJECT_MEMORY_ENABLED", true) {
		t.Fatal("explicit feature disable was ignored")
	}
	t.Setenv("CHAT_CODEX_PROJECT_MEMORY_ENABLED", "true")
	if !envEnabled("CHAT_CODEX_PROJECT_MEMORY_ENABLED", false) {
		t.Fatal("explicit feature enable was ignored")
	}
}

func verifiedHookCandidate() model.ProjectMemoryCandidate {
	return model.ProjectMemoryCandidate{
		Kind: model.ProjectMemoryVerifiedFact, SubjectKey: "native_hook:libgame.so:tick",
		Statement: "tick is hooked", Confidence: 1,
		Verification: model.ProjectMemoryVerification{Status: model.ProjectMemoryVerified, Method: "runtime_log", EvidenceRefs: []string{"log-1"}},
		Sources:      []model.ProjectMemorySource{{ToolCallIDs: []string{"log-1"}}},
		Artifacts: []model.ProjectMemoryArtifact{{
			Path: "lib/arm64-v8a/libgame.so", SHA256: strings.Repeat("a", 64),
			PackageName: "com.example.game", APKSHA256: strings.Repeat("b", 64), BuildID: "build",
			ABI: "arm64-v8a", AppVersion: "1.0", ModuleName: "libgame.so", Symbol: "tick",
			RelativeOffset: "0x100", FunctionStart: "0x100", InstructionSet: "aarch64",
			IDAStatus: "complete", IDADatabaseID: "ida-db-1", HotUpdateStatus: "not_detected", ShadowHookStatus: "verified",
			UICallStatus: "verified", NativeCallStatus: "verified",
		}},
	}
}

func TestValidateProjectMemoryBatchAcceptsCurrentAndPreviousSchema(t *testing.T) {
	job := &model.ProjectMemoryJob{ID: "job", ScopeID: "scope"}
	for _, version := range []int{projectMemorySchemaPrevious, projectMemorySchemaCurrent} {
		batch := &model.ProjectMemoryCandidateBatch{
			SchemaVersion: version, ProjectScopeID: "scope", JobID: "job",
			Candidates: []model.ProjectMemoryCandidate{verifiedHookCandidate()},
		}
		if err := validateProjectMemoryBatch(batch, job); err != nil {
			t.Fatalf("schema version %d rejected: %v", version, err)
		}
	}
	batch := &model.ProjectMemoryCandidateBatch{SchemaVersion: projectMemorySchemaCurrent + 1, ProjectScopeID: "scope", JobID: "job"}
	if err := validateProjectMemoryBatch(batch, job); err == nil {
		t.Fatal("future candidate schema was accepted")
	}
}

func TestValidateProjectMemoryBatchRequiresCompleteNativeHookProof(t *testing.T) {
	job := &model.ProjectMemoryJob{ID: "job", ScopeID: "scope"}
	candidate := verifiedHookCandidate()
	batch := &model.ProjectMemoryCandidateBatch{SchemaVersion: 1, ProjectScopeID: "scope", JobID: "job", Candidates: []model.ProjectMemoryCandidate{candidate}}
	if err := validateProjectMemoryBatch(batch, job); err != nil {
		t.Fatalf("complete hook proof rejected: %v", err)
	}
	checks := map[string]func(*model.ProjectMemoryCandidate){
		"hot update":        func(value *model.ProjectMemoryCandidate) { value.Artifacts[0].HotUpdateStatus = "" },
		"ui callback":       func(value *model.ProjectMemoryCandidate) { value.Artifacts[0].UICallStatus = "unverified" },
		"native callback":   func(value *model.ProjectMemoryCandidate) { value.Artifacts[0].NativeCallStatus = "unverified" },
		"function boundary": func(value *model.ProjectMemoryCandidate) { value.Artifacts[0].FunctionStart = "" },
		"apk fingerprint":   func(value *model.ProjectMemoryCandidate) { value.Artifacts[0].APKSHA256 = "" },
		"ida database":      func(value *model.ProjectMemoryCandidate) { value.Artifacts[0].IDADatabaseID = "" },
		"instruction align": func(value *model.ProjectMemoryCandidate) { value.Artifacts[0].RelativeOffset = "0x101" },
		"runtime evidence":  func(value *model.ProjectMemoryCandidate) { value.Verification.EvidenceRefs = nil },
		"source provenance": func(value *model.ProjectMemoryCandidate) { value.Sources = nil },
	}
	for name, mutate := range checks {
		t.Run(name, func(t *testing.T) {
			copy := verifiedHookCandidate()
			mutate(&copy)
			invalid := &model.ProjectMemoryCandidateBatch{SchemaVersion: 1, ProjectScopeID: "scope", JobID: "job", Candidates: []model.ProjectMemoryCandidate{copy}}
			if err := validateProjectMemoryBatch(invalid, job); err == nil {
				t.Fatal("incomplete verified hook proof was accepted")
			}
		})
	}
}

func TestValidateProjectMemoryBatchAcceptsExcerptEvidence(t *testing.T) {
	job := &model.ProjectMemoryJob{ID: "job", ScopeID: "scope"}
	batch := &model.ProjectMemoryCandidateBatch{
		SchemaVersion: 1, ProjectScopeID: "scope", JobID: "job",
		Candidates: []model.ProjectMemoryCandidate{{
			Kind: model.ProjectMemoryEpisode, SubjectKey: "workflow:summary",
			Statement: "The project uses the established workflow.", Confidence: 0.8,
			Sources: []model.ProjectMemorySource{{Excerpt: "established workflow"}},
		}},
	}
	if err := validateProjectMemoryBatch(batch, job); err != nil {
		t.Fatalf("excerpt evidence was rejected: %v", err)
	}
}

func TestProjectMemorySourcePacketAlwaysValidJSON(t *testing.T) {
	task := &model.Task{ID: "task", SessionID: "session", ProjectID: "project", Result: strings.Repeat("x", projectMemorySourceLimit*2)}
	packet := projectMemorySourcePacket(task, nil)
	if len(packet) > projectMemorySourceLimit {
		t.Fatalf("packet exceeds limit: %d", len(packet))
	}
	var value map[string]any
	if err := json.Unmarshal([]byte(packet), &value); err != nil {
		t.Fatalf("packet is invalid JSON: %v", err)
	}
}

func TestProjectMemoryTaskSourcePacketBoundsLargeEventHistoryAndKeepsNewestEvents(t *testing.T) {
	large := strings.Repeat("内容\n", 5000)
	task := &model.Task{
		ID: "task-large", SessionID: "session", ProjectID: "project",
		Metadata: map[string]string{"large": large},
		Parts:    []model.Part{{Type: "text", Text: large, Source: map[string]any{"large": large}}},
		Result:   large, Error: large,
	}
	sharedTool := &model.ToolCall{
		Tool: "exec", Title: large, Output: large, Error: large,
		Input: map[string]any{"large": large}, Metadata: map[string]any{"large": large},
		Attachments: []model.Part{{Type: "text", Text: large}},
	}
	events := make([]model.Event, 20_000)
	for index := range events {
		events[index] = model.Event{
			TaskID: task.ID, Type: "tool_updated", Attempt: index, Content: large,
			Tool: sharedTool, Metadata: map[string]any{"large": large}, Files: task.Parts,
		}
	}

	packet := projectMemoryTaskSourcePacket(task, events, projectMemoryTaskSourceLimit)
	if len(packet) > projectMemoryTaskSourceLimit {
		t.Fatalf("packet exceeds limit: %d", len(packet))
	}
	var decoded struct {
		SourceTruncated bool          `json:"source_truncated"`
		Events          []model.Event `json:"events"`
	}
	if err := json.Unmarshal(packet, &decoded); err != nil {
		t.Fatalf("packet is invalid JSON: %v", err)
	}
	if !decoded.SourceTruncated || len(decoded.Events) == 0 || len(decoded.Events) > projectMemoryTaskEventLimit {
		t.Fatalf("unexpected bounded packet: truncated=%v events=%d", decoded.SourceTruncated, len(decoded.Events))
	}
	newest := decoded.Events[len(decoded.Events)-1]
	if newest.Attempt != len(events)-1 {
		t.Fatalf("newest event was not retained: got=%d want=%d", newest.Attempt, len(events)-1)
	}
	if newest.Tool == nil || newest.Tool.Input != nil || newest.Tool.Metadata != nil || len(newest.Tool.Attachments) != 0 || !newest.Tool.OutputTruncated {
		t.Fatalf("tool payload was not bounded: %+v", newest.Tool)
	}
}

func seedProjectMemoryTask(t *testing.T, memory *store.Memory, operatorID int64, machineID, projectID string, createdAt time.Time, result string) *model.Task {
	t.Helper()
	task := memory.CreateTask("agent", machineID, projectID, "/project", "session", nil, nil, operatorID)
	task.Status = model.TaskCompleted
	task.Result = result
	task.CreatedAt = createdAt
	task.UpdatedAt = createdAt
	updated, ok := memory.UpdateTask(task)
	if !ok {
		t.Fatal("seed task update failed")
	}
	return updated
}

func TestProjectMemorySourceBatchCollectsHistoricalTasksInProjectOrder(t *testing.T) {
	memory := store.NewMemory(nil)
	base := time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC)
	first := seedProjectMemoryTask(t, memory, 1, "machine", "project", base, "first result")
	second := seedProjectMemoryTask(t, memory, 1, "machine", "project", base.Add(time.Minute), "second result")
	third := seedProjectMemoryTask(t, memory, 1, "machine", "project", base.Add(2*time.Minute), "third result")
	seedProjectMemoryTask(t, memory, 1, "machine", "other-project", base.Add(3*time.Minute), "other project")
	seedProjectMemoryTask(t, memory, 1, "other-machine", "project", base.Add(4*time.Minute), "other machine")

	application := &App{store: memory}
	batch, err := application.projectMemorySourceBatch(&model.ProjectMemoryJob{
		OperatorID: 1, MachineID: "machine", Trigger: model.ProjectMemoryTriggerManual,
	}, "project")
	if err != nil {
		t.Fatal(err)
	}
	var packet struct {
		TaskCount int `json:"task_count"`
		Tasks     []struct {
			TaskID string `json:"task_id"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(batch.Source), &packet); err != nil {
		t.Fatalf("decode source batch: %v", err)
	}
	want := []string{first.ID, second.ID, third.ID}
	if packet.TaskCount != len(want) || len(packet.Tasks) != len(want) || batch.TargetCursor != third.ID || batch.HasMore {
		t.Fatalf("unexpected source batch: packet=%+v batch=%+v", packet, batch)
	}
	for index, taskID := range want {
		if packet.Tasks[index].TaskID != taskID {
			t.Fatalf("task order mismatch at %d: got=%s want=%s", index, packet.Tasks[index].TaskID, taskID)
		}
	}
}

func TestProjectMemorySourceBatchTreatsCommittedStartAsExclusive(t *testing.T) {
	memory := store.NewMemory(nil)
	base := time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC)
	first := seedProjectMemoryTask(t, memory, 1, "machine", "project", base, "first result")
	second := seedProjectMemoryTask(t, memory, 1, "machine", "project", base.Add(time.Minute), "second result")
	third := seedProjectMemoryTask(t, memory, 1, "machine", "project", base.Add(2*time.Minute), "third result")
	application := &App{store: memory}

	batch, err := application.projectMemorySourceBatch(&model.ProjectMemoryJob{
		OperatorID: 1, MachineID: "machine", CursorStart: first.ID, CursorEnd: third.ID,
	}, "project")
	if err != nil {
		t.Fatal(err)
	}
	var packet struct {
		Tasks []struct {
			TaskID string `json:"task_id"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal([]byte(batch.Source), &packet); err != nil {
		t.Fatal(err)
	}
	if len(packet.Tasks) != 2 || packet.Tasks[0].TaskID != second.ID || packet.Tasks[1].TaskID != third.ID {
		t.Fatalf("committed cursor was included again: %+v", packet.Tasks)
	}

	single, err := application.projectMemorySourceBatch(&model.ProjectMemoryJob{
		OperatorID: 1, MachineID: "machine", CursorStart: first.ID, CursorEnd: first.ID,
	}, "project")
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(single.Source), &packet); err != nil {
		t.Fatal(err)
	}
	if len(packet.Tasks) != 1 || packet.Tasks[0].TaskID != first.ID {
		t.Fatalf("legacy single-task cursor was not preserved: %+v", packet.Tasks)
	}
}

func TestProjectMemorySourceBatchIsBoundedAndKeepsTargetCursor(t *testing.T) {
	memory := store.NewMemory(nil)
	base := time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC)
	var last *model.Task
	for index := 0; index < 10; index++ {
		last = seedProjectMemoryTask(t, memory, 1, "machine", "project", base.Add(time.Duration(index)*time.Minute), strings.Repeat("x", projectMemoryTaskSourceLimit*2))
	}
	application := &App{store: memory}
	batch, err := application.projectMemorySourceBatch(&model.ProjectMemoryJob{
		OperatorID: 1, MachineID: "machine", Trigger: model.ProjectMemoryTriggerManual,
	}, "project")
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Source) > projectMemorySourceLimit || !batch.HasMore || batch.LastTask == nil || batch.LastTask.ID == last.ID || batch.TargetCursor != last.ID {
		lastID := ""
		if batch.LastTask != nil {
			lastID = batch.LastTask.ID
		}
		t.Fatalf("source batch did not preserve bounded continuation: size=%d has_more=%v last=%s target=%s want_target=%s",
			len(batch.Source), batch.HasMore, lastID, batch.TargetCursor, last.ID)
	}
}

func TestValidateProjectMemoryBatchRedactsCredentials(t *testing.T) {
	job := &model.ProjectMemoryJob{ID: "job", ScopeID: "scope"}
	batch := &model.ProjectMemoryCandidateBatch{
		SchemaVersion: projectMemorySchemaCurrent, ProjectScopeID: "scope", JobID: "job",
		Candidates: []model.ProjectMemoryCandidate{{
			Kind: model.ProjectMemoryIssue, SubjectKey: "configuration",
			Statement: "Set api_key=super-secret-value and Authorization Bearer abcdefghijklmnopqrstuvwxyz",
			Sources:   []model.ProjectMemorySource{{TaskID: "task-1", Excerpt: "password: hunter2-value"}},
		}},
		BriefCandidate: &model.ProjectMemoryBriefCandidate{Content: "token=token-value-123456"},
	}
	if err := validateProjectMemoryBatch(batch, job); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(batch)
	text := string(encoded)
	for _, secret := range []string{"super-secret-value", "abcdefghijklmnopqrstuvwxyz", "hunter2-value", "token-value-123456"} {
		if strings.Contains(text, secret) {
			t.Fatalf("credential remained in curator batch: %s", text)
		}
	}
	if !strings.Contains(text, "[REDACTED]") {
		t.Fatalf("redaction marker missing: %s", text)
	}
}
