package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"relay-server/internal/model"
)

func seedProjectScope(t *testing.T, memory *Memory, operatorID int64, machineID, scopeID string) {
	t.Helper()
	err := memory.UpsertProjectScope(context.Background(), model.ProjectScope{
		ID: scopeID, OperatorID: operatorID, MachineID: machineID, DisplayName: scopeID,
		Status: model.ProjectScopeActive, Revision: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestProjectMemoryScopeIsolationAndDeleteRevision(t *testing.T) {
	memory := NewMemory(nil)
	seedProjectScope(t, memory, 1, "machine-a", "scope-shared")
	seedProjectScope(t, memory, 2, "machine-b", "scope-other")
	item := model.ProjectMemory{
		ID: "mem-a", LogicalID: "logical-a", ScopeID: "scope-shared",
		Kind: model.ProjectMemoryVerifiedFact, SubjectKey: "build", Statement: "verified build",
		Status: model.ProjectMemoryActive, Verification: model.ProjectMemoryVerification{Status: model.ProjectMemoryVerified},
	}
	if err := memory.UpsertProjectMemory(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	history := item
	history.ID = "mem-history"
	history.LogicalID = "logical-history"
	history.Status = model.ProjectMemorySuperseded
	if err := memory.UpsertProjectMemory(context.Background(), history); err != nil {
		t.Fatal(err)
	}
	items, err := memory.ListProjectMemories(context.Background(), model.ProjectMemoryFilter{
		OperatorID: 2, MachineID: "machine-b", ScopeID: "scope-shared",
	})
	if err != nil || len(items) != 0 {
		t.Fatalf("cross-owner list leaked memory: items=%v err=%v", items, err)
	}
	if err := memory.MarkProjectMemoryDeleted(context.Background(), 2, "scope-shared", "mem-a", 1); err == nil {
		t.Fatal("cross-owner delete succeeded")
	}
	if err := memory.MarkProjectMemoryDeleted(context.Background(), 1, "scope-shared", "mem-a", 9); !errors.Is(err, ErrProjectMemoryRevisionConflict) {
		t.Fatalf("stale delete returned %v", err)
	}
	if err := memory.MarkProjectMemoryDeleted(context.Background(), 1, "scope-shared", history.ID, 1); !errors.Is(err, ErrProjectMemoryRevisionConflict) {
		t.Fatalf("historical delete returned %v", err)
	}
	if err := memory.MarkProjectMemoryDeleted(context.Background(), 1, "scope-shared", "mem-a", 1); err != nil {
		t.Fatal(err)
	}
	scope, _ := memory.GetProjectScope(context.Background(), 1, "machine-a", "scope-shared")
	if scope == nil || scope.Revision != 2 {
		t.Fatalf("delete did not advance revision: %#v", scope)
	}
	if err := memory.MarkProjectMemoryDeleted(context.Background(), 1, "scope-shared", "mem-a", 1); err != nil {
		t.Fatal(err)
	}
	scope, _ = memory.GetProjectScope(context.Background(), 1, "machine-a", "scope-shared")
	if scope == nil || scope.Revision != 2 {
		t.Fatalf("idempotent delete advanced revision: %#v", scope)
	}
}

func TestProjectMemoryEditRejectsStaleVersion(t *testing.T) {
	memory := NewMemory(nil)
	seedProjectScope(t, memory, 1, "machine", "scope")
	original := model.ProjectMemory{
		ID: "mem-1", LogicalID: "logical-1", ScopeID: "scope", Version: 1,
		Kind: model.ProjectMemoryDecision, SubjectKey: "decision", Statement: "first", Status: model.ProjectMemoryActive,
	}
	if err := memory.UpsertProjectMemory(context.Background(), original); err != nil {
		t.Fatal(err)
	}
	next := original
	next.Statement = "second"
	if err := memory.EditProjectMemoryVersion(context.Background(), 1, "scope", original.ID, next); err != nil {
		t.Fatal(err)
	}
	if err := memory.EditProjectMemoryVersion(context.Background(), 1, "scope", original.ID, next); !errors.Is(err, ErrProjectMemoryRevisionConflict) {
		t.Fatalf("expected stale edit conflict, got %v", err)
	}
}

func TestProjectMemoryCompositeCursorAndFactUnion(t *testing.T) {
	memory := NewMemory(nil)
	seedProjectScope(t, memory, 1, "machine", "scope")
	base := time.Date(2026, 8, 5, 10, 0, 0, 0, time.UTC)
	items := []model.ProjectMemory{
		{ID: "z-old", Kind: model.ProjectMemoryDecision, UpdatedAt: base},
		{ID: "a-new", Kind: model.ProjectMemoryDecision, UpdatedAt: base.Add(3 * time.Minute)},
		{ID: "m-mid", Kind: model.ProjectMemoryInferredFact, UpdatedAt: base.Add(2 * time.Minute)},
		{ID: "b-tie", Kind: model.ProjectMemoryDecision, UpdatedAt: base.Add(2 * time.Minute)},
		{ID: "z-tie", Kind: model.ProjectMemoryVerifiedFact, UpdatedAt: base.Add(2 * time.Minute)},
	}
	for _, item := range items {
		item.LogicalID = "logical-" + item.ID
		item.ScopeID = "scope"
		item.SubjectKey = item.ID
		item.Statement = item.ID
		item.Status = model.ProjectMemoryActive
		item.CreatedAt = item.UpdatedAt
		if err := memory.UpsertProjectMemory(context.Background(), item); err != nil {
			t.Fatal(err)
		}
	}
	first, err := memory.ListProjectMemories(context.Background(), model.ProjectMemoryFilter{
		OperatorID: 1, MachineID: "machine", ScopeID: "scope", Limit: 2,
	})
	if err != nil || len(first) != 2 || first[0].ID != "a-new" || first[1].ID != "z-tie" {
		t.Fatalf("unexpected first page: items=%+v err=%v", first, err)
	}
	cursor := first[1]
	cursor.UpdatedAt = base.Add(4 * time.Minute)
	if err := memory.UpsertProjectMemory(context.Background(), cursor); err != nil {
		t.Fatal(err)
	}
	second, err := memory.ListProjectMemories(context.Background(), model.ProjectMemoryFilter{
		OperatorID: 1, MachineID: "machine", ScopeID: "scope", Limit: 2,
		BeforeID: first[1].ID, BeforeUpdatedAt: first[1].UpdatedAt,
	})
	if err != nil || len(second) != 2 || second[0].ID != "m-mid" || second[1].ID != "b-tie" {
		t.Fatalf("cursor update caused a pagination gap or duplicate: items=%+v err=%v", second, err)
	}
	third, err := memory.ListProjectMemories(context.Background(), model.ProjectMemoryFilter{
		OperatorID: 1, MachineID: "machine", ScopeID: "scope", Limit: 2,
		BeforeID: second[1].ID, BeforeUpdatedAt: second[1].UpdatedAt,
	})
	if err != nil || len(third) != 1 || third[0].ID != "z-old" {
		t.Fatalf("unexpected final page: items=%+v err=%v", third, err)
	}
	facts, err := memory.ListProjectMemories(context.Background(), model.ProjectMemoryFilter{
		OperatorID: 1, MachineID: "machine", ScopeID: "scope",
		Kinds: []model.ProjectMemoryKind{model.ProjectMemoryVerifiedFact, model.ProjectMemoryInferredFact},
	})
	if err != nil || len(facts) != 2 {
		t.Fatalf("fact union returned items=%+v err=%v", facts, err)
	}
}

func TestProjectMemoryClaimFencingExpiryAndDeviceLimit(t *testing.T) {
	memory := NewMemory(nil)
	now := time.Now().UTC()
	for index, scopeID := range []string{"scope-1", "scope-2", "scope-3"} {
		seedProjectScope(t, memory, 1, "machine", scopeID)
		job := model.ProjectMemoryJob{
			ID: "job-" + scopeID, OperatorID: 1, MachineID: "machine", ScopeID: scopeID,
			Status: model.ProjectMemoryJobQueued, CreatedAt: now.Add(time.Duration(index) * time.Millisecond),
		}
		if err := memory.CreateProjectMemoryJob(context.Background(), job); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	claimed := make(chan *model.ProjectMemoryJob, 3)
	for _, id := range []string{"job-scope-1", "job-scope-2", "job-scope-3"} {
		wg.Add(1)
		go func(jobID string) {
			defer wg.Done()
			job, _ := memory.ClaimProjectMemoryJob(context.Background(), jobID, "worker-"+jobID, now, now.Add(time.Minute), 2)
			claimed <- job
		}(id)
	}
	wg.Wait()
	close(claimed)
	count := 0
	var first *model.ProjectMemoryJob
	for job := range claimed {
		if job != nil {
			count++
			if first == nil {
				first = job
			}
		}
	}
	if count != 2 {
		t.Fatalf("expected two device claims, got %d", count)
	}
	if ok, err := memory.RenewProjectMemoryJob(context.Background(), first.ID, first.LeaseOwner, first.FencingToken-1, now.Add(2*time.Minute)); err != nil || ok {
		t.Fatalf("stale fencing token renewed lease: ok=%v err=%v", ok, err)
	}
	memory.mu.Lock()
	expired := memory.projectMemoryJobs[first.ID]
	expired.LeaseExpiresAt = now.Add(-time.Second)
	memory.projectMemoryJobs[first.ID] = expired
	memory.mu.Unlock()
	if ok, err := memory.RenewProjectMemoryJob(context.Background(), first.ID, first.LeaseOwner, first.FencingToken, now.Add(time.Minute)); err != nil || ok {
		t.Fatalf("expired lease renewed: ok=%v err=%v", ok, err)
	}
}

func TestProjectMemoryScheduleCoalescesAndDefersRunningCursor(t *testing.T) {
	memory := NewMemory(nil)
	seedProjectScope(t, memory, 1, "machine", "scope")
	now := time.Now().UTC()
	var wg sync.WaitGroup
	for index := 0; index < 32; index++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, _, err := memory.ScheduleProjectMemoryJob(context.Background(), model.ProjectMemoryJob{
				ID: fmt.Sprintf("job-%d", index), OperatorID: 1, MachineID: "machine", ScopeID: "scope",
				Status: model.ProjectMemoryJobQueued, CursorStart: "task-0", CursorEnd: fmt.Sprintf("task-%d", index+1),
				ManualVisible: index == 17, CreatedAt: now.Add(time.Duration(index) * time.Microsecond),
			})
			if err != nil {
				t.Errorf("schedule %d: %v", index, err)
			}
		}(index)
	}
	wg.Wait()
	jobs, err := memory.ListProjectMemoryJobs(context.Background(), 1, "scope", 100)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("expected one active job after concurrent schedule: jobs=%+v err=%v", jobs, err)
	}
	if !jobs[0].ManualVisible {
		t.Fatal("manual trigger did not attach to coalesced job")
	}
	claimed, err := memory.ClaimProjectMemoryJob(context.Background(), jobs[0].ID, "worker", now, now.Add(time.Minute), 2)
	if err != nil || claimed == nil {
		t.Fatalf("claim coalesced job: job=%+v err=%v", claimed, err)
	}
	workerSnapshot := *claimed
	workerSnapshot.ManualVisible = false
	attached, created, err := memory.ScheduleProjectMemoryJob(context.Background(), model.ProjectMemoryJob{
		ID: "later", OperatorID: 1, MachineID: "machine", ScopeID: "scope",
		Status: model.ProjectMemoryJobQueued, CursorEnd: "task-later",
	})
	if err != nil || created || attached == nil || attached.PendingCursorEnd != "task-later" {
		t.Fatalf("running cursor was not deferred: job=%+v created=%v err=%v", attached, created, err)
	}
	workerSnapshot.Status = model.ProjectMemoryJobCompleted
	workerSnapshot.CursorCommitted = workerSnapshot.CursorEnd
	workerSnapshot.ResultRevision = 2
	workerSnapshot.LeaseOwner = ""
	workerSnapshot.LeaseExpiresAt = time.Time{}
	successor, err := memory.FinalizeProjectMemoryJob(context.Background(), workerSnapshot, "successor")
	if err != nil || successor == nil || successor.CursorStart != workerSnapshot.CursorEnd || successor.CursorEnd != "task-later" {
		t.Fatalf("deferred successor mismatch: successor=%+v err=%v", successor, err)
	}
	finalized, _ := memory.GetProjectMemoryJob(context.Background(), 1, claimed.ID)
	if finalized == nil || !finalized.ManualVisible {
		t.Fatalf("finalize overwrote attached manual visibility: %+v", finalized)
	}
	jobs, _ = memory.ListProjectMemoryJobs(context.Background(), 1, "scope", 100)
	active := 0
	for _, job := range jobs {
		if job.Status == model.ProjectMemoryJobQueued || job.Status == model.ProjectMemoryJobClaimed || job.Status == model.ProjectMemoryJobRunning {
			active++
		}
	}
	if active != 1 {
		t.Fatalf("expected exactly one active successor, got %d: %+v", active, jobs)
	}
}

func TestProjectMemoryAutomaticJobsWaitForQuietWindow(t *testing.T) {
	memory := NewMemory(nil)
	seedProjectScope(t, memory, 1, "machine", "scope")
	base := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	automatic, created, err := memory.ScheduleProjectMemoryJob(context.Background(), model.ProjectMemoryJob{
		ID: "automatic", OperatorID: 1, MachineID: "machine", ScopeID: "scope",
		Trigger: model.ProjectMemoryTriggerTaskComplete, Status: model.ProjectMemoryJobQueued,
		CreatedAt: base, UpdatedAt: base,
	})
	if err != nil || !created || automatic == nil {
		t.Fatalf("schedule automatic job: job=%+v created=%v err=%v", automatic, created, err)
	}
	jobs, err := memory.ListClaimableProjectMemoryJobs(context.Background(), base.Add(projectMemoryAutoQuietWindow-time.Second), 10)
	if err != nil || len(jobs) != 0 {
		t.Fatalf("automatic job escaped quiet window: jobs=%+v err=%v", jobs, err)
	}
	jobs, err = memory.ListClaimableProjectMemoryJobs(context.Background(), base.Add(projectMemoryAutoQuietWindow), 10)
	if err != nil || len(jobs) != 1 || jobs[0].ID != automatic.ID {
		t.Fatalf("automatic job was not ready after quiet window: jobs=%+v err=%v", jobs, err)
	}

	manualMemory := NewMemory(nil)
	seedProjectScope(t, manualMemory, 1, "machine", "scope")
	manual, _, err := manualMemory.ScheduleProjectMemoryJob(context.Background(), model.ProjectMemoryJob{
		ID: "manual", OperatorID: 1, MachineID: "machine", ScopeID: "scope",
		Trigger: model.ProjectMemoryTriggerManual, Status: model.ProjectMemoryJobQueued,
		CreatedAt: base, UpdatedAt: base,
	})
	jobs, listErr := manualMemory.ListClaimableProjectMemoryJobs(context.Background(), base, 10)
	if err != nil || listErr != nil || manual == nil || len(jobs) != 1 || jobs[0].ID != manual.ID {
		t.Fatalf("manual job was delayed: job=%+v jobs=%+v err=%v list_err=%v", manual, jobs, err, listErr)
	}
}

func TestProjectMemoryJobOlderThanNinetyMinutesHasNoHardTimeout(t *testing.T) {
	memory := NewMemory(nil)
	seedProjectScope(t, memory, 1, "machine", "scope")
	now := time.Now().UTC()
	if err := memory.CreateProjectMemoryJob(context.Background(), model.ProjectMemoryJob{
		ID: "long-job", OperatorID: 1, MachineID: "machine", ScopeID: "scope",
		Status: model.ProjectMemoryJobQueued, CreatedAt: now.Add(-91 * time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	job, err := memory.ClaimProjectMemoryJob(context.Background(), "long-job", "worker", now, now.Add(5*time.Minute), 2)
	if err != nil || job == nil {
		t.Fatalf("claim old job: job=%+v err=%v", job, err)
	}
	for elapsed := 30 * time.Second; elapsed <= 91*time.Minute; elapsed += 30 * time.Second {
		ok, err := memory.RenewProjectMemoryJob(context.Background(), job.ID, "worker", job.FencingToken, now.Add(elapsed+5*time.Minute))
		if err != nil || !ok {
			t.Fatalf("heartbeat at %s rejected: ok=%v err=%v", elapsed, ok, err)
		}
	}
	latest, _ := memory.GetProjectMemoryJob(context.Background(), 1, job.ID)
	latest.Status = model.ProjectMemoryJobCompleted
	latest.LeaseOwner = ""
	latest.LeaseExpiresAt = time.Time{}
	if _, err := memory.FinalizeProjectMemoryJob(context.Background(), *latest, "unused"); err != nil {
		t.Fatalf("finalize 91-minute job: %v", err)
	}
}

func TestProjectMemoryArtifactChangeMarksOldHookStale(t *testing.T) {
	memory := NewMemory(nil)
	seedProjectScope(t, memory, 1, "machine", "scope")
	old := model.ProjectMemory{
		ID: "old", LogicalID: "logical", ScopeID: "scope", Kind: model.ProjectMemoryVerifiedFact,
		SubjectKey: "native_hook:libgame.so:tick", Statement: "old offset", Status: model.ProjectMemoryActive,
		Verification: model.ProjectMemoryVerification{Status: model.ProjectMemoryVerified},
		Artifacts:    []model.ProjectMemoryArtifact{{ModuleName: "libgame.so", SHA256: "old-hash"}},
	}
	if err := memory.UpsertProjectMemory(context.Background(), old); err != nil {
		t.Fatal(err)
	}
	result, err := memory.ReconcileProjectMemories(context.Background(), 1, "scope", 1, []model.ProjectMemoryCandidate{{
		SubjectKey: old.SubjectKey, Kind: model.ProjectMemoryVerifiedFact, Statement: "new offset", Confidence: 1,
		Verification: model.ProjectMemoryVerification{Status: model.ProjectMemoryVerified},
		Artifacts:    []model.ProjectMemoryArtifact{{ModuleName: "libgame.so", SHA256: "new-hash"}},
	}}, nil, nil)
	if err != nil || !result.Changed {
		t.Fatalf("reconcile failed: result=%+v err=%v", result, err)
	}
	stale, _ := memory.GetProjectMemory(context.Background(), 1, "scope", old.ID)
	if stale == nil || stale.Status != model.ProjectMemoryStale || stale.Verification.Status != model.ProjectMemoryStaleCheck {
		t.Fatalf("old hook was not stale: %#v", stale)
	}
}

func TestProjectMemoryEvidenceBackedInvalidationPreservesHumanPrecedence(t *testing.T) {
	memory := NewMemory(nil)
	seedProjectScope(t, memory, 1, "machine", "scope")
	for _, item := range []model.ProjectMemory{
		{ID: "curator", LogicalID: "curator", ScopeID: "scope", Kind: model.ProjectMemoryVerifiedFact, SubjectKey: "build", Statement: "Old build path", Status: model.ProjectMemoryActive, CreatedBy: "curator"},
		{ID: "human", LogicalID: "human", ScopeID: "scope", Kind: model.ProjectMemoryDecision, SubjectKey: "database", Statement: "Use PostgreSQL", Status: model.ProjectMemoryActive, CreatedBy: "user"},
		{ID: "locked-rule", LogicalID: "locked-rule", ScopeID: "scope", Kind: model.ProjectMemoryLockedRule, SubjectKey: "tests", Statement: "Run tests", Status: model.ProjectMemoryActive, Locked: true, CreatedBy: "user"},
	} {
		if err := memory.UpsertProjectMemory(context.Background(), item); err != nil {
			t.Fatal(err)
		}
	}
	curator, _ := memory.GetProjectMemory(context.Background(), 1, "scope", "curator")
	human, _ := memory.GetProjectMemory(context.Background(), 1, "scope", "human")
	locked, _ := memory.GetProjectMemory(context.Background(), 1, "scope", "locked-rule")
	result, err := memory.ReconcileProjectMemories(context.Background(), 1, "scope", 1, nil, []model.ProjectMemoryInvalidation{
		{MemoryID: curator.ID, ContentHash: curator.ContentHash, Status: model.ProjectMemoryStale, Reason: "New build evidence", EvidenceRefs: []string{"task:new-build"}},
		{MemoryID: human.ID, ContentHash: human.ContentHash, Status: model.ProjectMemoryStale, Reason: "Conflicting evidence", EvidenceRefs: []string{"task:db-check"}},
		{MemoryID: locked.ID, ContentHash: locked.ContentHash, Status: model.ProjectMemoryStale, Reason: "Must not alter locked rule", EvidenceRefs: []string{"task:test"}},
	}, nil)
	if err != nil || !result.Changed || len(result.InvalidatedIDs) != 2 {
		t.Fatalf("invalidation reconcile failed: result=%+v err=%v", result, err)
	}
	curator, _ = memory.GetProjectMemory(context.Background(), 1, "scope", curator.ID)
	human, _ = memory.GetProjectMemory(context.Background(), 1, "scope", human.ID)
	locked, _ = memory.GetProjectMemory(context.Background(), 1, "scope", locked.ID)
	if curator.Status != model.ProjectMemoryStale || curator.Verification.Status != model.ProjectMemoryStaleCheck {
		t.Fatalf("curator memory was not marked stale: %+v", curator)
	}
	if human.Status != model.ProjectMemoryDisputed {
		t.Fatalf("human memory did not retain dispute precedence: %+v", human)
	}
	if locked.Status != model.ProjectMemoryActive {
		t.Fatalf("locked rule changed: %+v", locked)
	}

	other := model.ProjectMemory{ID: "hash-guard", LogicalID: "hash-guard", ScopeID: "scope", Kind: model.ProjectMemoryVerifiedFact, SubjectKey: "guard", Statement: "Current", Status: model.ProjectMemoryActive}
	if err := memory.UpsertProjectMemory(context.Background(), other); err != nil {
		t.Fatal(err)
	}
	result, err = memory.ReconcileProjectMemories(context.Background(), 1, "scope", result.Revision, nil, []model.ProjectMemoryInvalidation{{
		MemoryID: other.ID, ContentHash: strings.Repeat("0", 64), Status: model.ProjectMemoryStale,
		Reason: "Outdated observation", EvidenceRefs: []string{"task:old"},
	}}, nil)
	if err != nil || result.Changed || len(result.InvalidatedIDs) != 0 {
		t.Fatalf("stale content hash changed memory: result=%+v err=%v", result, err)
	}
}

func TestProjectMemorySourceAvailabilityTracksMissingAndReappearance(t *testing.T) {
	memory := NewMemory(nil)
	seedProjectScope(t, memory, 1, "machine", "scope")
	item := model.ProjectMemory{
		ID: "memory-source", LogicalID: "logical-source", ScopeID: "scope",
		Kind: model.ProjectMemoryVerifiedFact, SubjectKey: "source", Statement: "source fact",
		Status:  model.ProjectMemoryActive,
		Sources: []model.ProjectMemorySource{{RelativePath: "src/main.go", SHA256: "abc", Status: "available"}},
	}
	if err := memory.UpsertProjectMemory(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	changed, err := memory.UpdateProjectMemorySourceAvailability(context.Background(), 1, "scope", []model.ProjectMemorySourceAvailability{{
		MemoryID: item.ID, RelativePath: "src/main.go", Status: "source_missing",
	}})
	if err != nil || changed != 1 {
		t.Fatalf("mark missing: changed=%d err=%v", changed, err)
	}
	changed, err = memory.UpdateProjectMemorySourceAvailability(context.Background(), 1, "scope", []model.ProjectMemorySourceAvailability{{
		MemoryID: item.ID, RelativePath: "src/main.go", SHA256: "abc", Status: "available",
	}})
	if err != nil || changed != 1 {
		t.Fatalf("restore source: changed=%d err=%v", changed, err)
	}
	got, _ := memory.GetProjectMemory(context.Background(), 1, "scope", item.ID)
	if got == nil || len(got.Sources) != 1 || got.Sources[0].Status != "available" || got.Sources[0].SHA256 != "abc" {
		t.Fatalf("unexpected restored source: %#v", got)
	}
	scope, _ := memory.GetProjectScope(context.Background(), 1, "machine", "scope")
	if scope == nil || scope.Revision != 3 {
		t.Fatalf("source overlays did not advance revision: %#v", scope)
	}
}

func TestProjectMemoryRetentionRollsUpActiveAndHistoryLimits(t *testing.T) {
	memory := NewMemory(nil)
	seedProjectScope(t, memory, 1, "machine", "scope")
	now := time.Now().UTC().Add(-time.Hour)
	statuses := []model.ProjectMemoryStatus{
		model.ProjectMemoryActive, model.ProjectMemoryActive, model.ProjectMemoryActive,
		model.ProjectMemoryArchived, model.ProjectMemoryStale, model.ProjectMemorySuperseded,
	}
	for index, status := range statuses {
		item := model.ProjectMemory{
			ID: fmt.Sprintf("memory-%d", index), LogicalID: fmt.Sprintf("logical-%d", index), ScopeID: "scope",
			Kind: model.ProjectMemoryVerifiedFact, SubjectKey: fmt.Sprintf("subject-%d", index),
			Statement: fmt.Sprintf("statement-%d", index), Status: status,
			CreatedAt: now.Add(time.Duration(index) * time.Minute), UpdatedAt: now.Add(time.Duration(index) * time.Minute),
		}
		if err := memory.UpsertProjectMemory(context.Background(), item); err != nil {
			t.Fatal(err)
		}
	}
	result, err := memory.MaintainProjectMemoryRetention(context.Background(), 2, 2, 10)
	if err != nil {
		t.Fatal(err)
	}
	// History rollup adds one active item, so three of four active items are
	// archived and replaced by one active Episode.
	if result.RolledUp != 2 || result.Archived != 3 || result.PurgedHistory != 1 {
		t.Fatalf("unexpected retention result: %+v", result)
	}
	items, err := memory.ListProjectMemories(context.Background(), model.ProjectMemoryFilter{
		OperatorID: 1, MachineID: "machine", ScopeID: "scope", Limit: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	active := 0
	episodes := 0
	for _, item := range items {
		if item.Status == model.ProjectMemoryActive || item.Status == model.ProjectMemoryDisputed {
			active++
		}
		if item.Kind == model.ProjectMemoryEpisode {
			episodes++
		}
	}
	if active > 2 || episodes != 2 {
		t.Fatalf("retention limits not enforced: active=%d episodes=%d items=%+v", active, episodes, items)
	}
}

func TestProjectMemoryFieldCipherRotationAndTamperDetection(t *testing.T) {
	t.Setenv("PROJECT_MEMORY_ENCRYPTION_KEY_ID", "new")
	t.Setenv("PROJECT_MEMORY_ENCRYPTION_KEYS", "old:MDEyMzQ1Njc4OWFiY2RlZg==,new:YWJjZGVmMDEyMzQ1Njc4OQ==")
	cipher, err := newProjectMemoryFieldCipher(MySQLConfig{})
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := cipher.encrypt("/private/project", "project_scope.current_root")
	if err != nil || encrypted == "/private/project" {
		t.Fatalf("field was not encrypted: %q err=%v", encrypted, err)
	}
	plain, err := cipher.decrypt(encrypted, "project_scope.current_root")
	if err != nil || plain != "/private/project" {
		t.Fatalf("decrypt mismatch: %q err=%v", plain, err)
	}
	if _, err := cipher.decrypt(encrypted+"x", "project_scope.current_root"); err == nil {
		t.Fatal("tampered ciphertext was accepted")
	}
}

func TestProjectMemoryBriefRejectsStaleCASWrite(t *testing.T) {
	memory := NewMemory(nil)
	seedProjectScope(t, memory, 1, "machine", "scope")
	if err := memory.UpsertProjectMemoryBrief(context.Background(), model.ProjectMemoryBrief{
		ScopeID: "scope", Content: "new", SourceRevision: 7, GeneratedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := memory.UpsertProjectMemoryBrief(context.Background(), model.ProjectMemoryBrief{
		ScopeID: "scope", Content: "stale", SourceRevision: 6, GeneratedAt: time.Now().UTC().Add(time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	brief, err := memory.GetProjectMemoryBrief(context.Background(), 1, "scope")
	if err != nil || brief == nil || brief.Content != "new" || brief.SourceRevision != 7 {
		t.Fatalf("stale brief overwrote newer revision: brief=%+v err=%v", brief, err)
	}
}

func TestProjectMemoryReconcileMergesSourcesAndHonorsTombstone(t *testing.T) {
	memory := NewMemory(nil)
	seedProjectScope(t, memory, 1, "machine", "scope")
	existing := model.ProjectMemory{
		ID: "memory", LogicalID: "logical", ScopeID: "scope", Kind: model.ProjectMemoryVerifiedFact,
		SubjectKey: "build", Statement: "Build passes", Status: model.ProjectMemoryActive,
		Verification: model.ProjectMemoryVerification{Status: model.ProjectMemoryVerified},
		Sources:      []model.ProjectMemorySource{{TaskID: "task-1"}},
	}
	if err := memory.UpsertProjectMemory(context.Background(), existing); err != nil {
		t.Fatal(err)
	}
	result, err := memory.ReconcileProjectMemories(context.Background(), 1, "scope", 1, []model.ProjectMemoryCandidate{{
		SubjectKey: existing.SubjectKey, Kind: existing.Kind, Statement: existing.Statement,
		Verification: existing.Verification, Sources: []model.ProjectMemorySource{{TaskID: "task-2"}},
	}}, nil, nil)
	if err != nil || !result.Changed {
		t.Fatalf("merge sources: result=%+v err=%v", result, err)
	}
	got, _ := memory.GetProjectMemory(context.Background(), 1, "scope", existing.ID)
	if got == nil || len(got.Sources) != 2 {
		t.Fatalf("candidate sources were not merged: %+v", got)
	}
	if err := memory.MarkProjectMemoryDeleted(context.Background(), 1, "scope", existing.ID, 0); err != nil {
		t.Fatal(err)
	}
	scope, _ := memory.GetProjectScope(context.Background(), 1, "machine", "scope")
	result, err = memory.ReconcileProjectMemories(context.Background(), 1, "scope", scope.Revision, []model.ProjectMemoryCandidate{{
		SubjectKey: existing.SubjectKey, Kind: existing.Kind, Statement: "resurrected",
		Verification: existing.Verification,
	}}, nil, nil)
	if err != nil || result.Changed || len(result.AcceptedIDs) != 0 {
		t.Fatalf("tombstone did not block curator resurrection: result=%+v err=%v", result, err)
	}
	items, _ := memory.ListProjectMemories(context.Background(), model.ProjectMemoryFilter{
		OperatorID: 1, MachineID: "machine", ScopeID: "scope", IncludeDeleted: true, Limit: 20,
	})
	if len(items) != 1 || items[0].Status != model.ProjectMemoryDeleted {
		t.Fatalf("unexpected records after tombstone reconciliation: %+v", items)
	}
}

func TestProjectMemoryReconcilePreservesNewerHumanEdit(t *testing.T) {
	memory := NewMemory(nil)
	seedProjectScope(t, memory, 1, "machine", "scope")
	human := model.ProjectMemory{
		ID: "human", LogicalID: "logical", ScopeID: "scope", Kind: model.ProjectMemoryDecision,
		SubjectKey: "database", Statement: "Use PostgreSQL", Status: model.ProjectMemoryActive,
		CreatedBy: "user", UpdatedAt: time.Now().UTC(),
	}
	if err := memory.UpsertProjectMemory(context.Background(), human); err != nil {
		t.Fatal(err)
	}
	result, err := memory.ReconcileProjectMemories(context.Background(), 1, "scope", 1, []model.ProjectMemoryCandidate{{
		SubjectKey: human.SubjectKey, Kind: human.Kind, Statement: "Use MySQL",
		Verification: model.ProjectMemoryVerification{Status: model.ProjectMemoryVerified},
	}}, nil, nil)
	if err != nil || !result.Changed {
		t.Fatalf("reconcile human conflict: result=%+v err=%v", result, err)
	}
	got, _ := memory.GetProjectMemory(context.Background(), 1, "scope", human.ID)
	if got == nil || got.Statement != human.Statement || got.Status != model.ProjectMemoryDisputed {
		t.Fatalf("human edit was overwritten: %+v", got)
	}
}
