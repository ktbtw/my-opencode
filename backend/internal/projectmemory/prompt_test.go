package projectmemory

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"relay-server/internal/model"
	"relay-server/internal/store"
)

func TestProjectMemoryMetricsSnapshot(t *testing.T) {
	var value metrics
	value.SetQueueDepth(4)
	value.LeaseExpired()
	value.Retried()
	value.Conflict()
	value.Candidates(5, 3)
	value.JobCompleted(125 * time.Millisecond)
	value.Retrieval(20 * time.Millisecond)
	value.Injected(700)
	value.CacheFallback()
	snapshot := value.Snapshot()
	if snapshot.QueueDepth != 4 || snapshot.CandidatesSubmitted != 5 || snapshot.CandidatesAccepted != 3 ||
		snapshot.JobDurationMaxMS != 125 || snapshot.RetrievalMaxMS != 20 || snapshot.InjectedTokens != 700 ||
		snapshot.LeaseExpiries != 1 || snapshot.Retries != 1 || snapshot.Conflicts != 1 || snapshot.CacheFallbacks != 1 {
		t.Fatalf("unexpected metrics snapshot: %+v", snapshot)
	}
}

func TestBuildInjectsValidMemoryAndExcludesInvalidRecords(t *testing.T) {
	memory := store.NewMemory(nil)
	ctx := context.Background()
	if err := memory.UpsertProjectScope(ctx, model.ProjectScope{
		ID: "scope", OperatorID: 1, MachineID: "machine", Revision: 7, Status: model.ProjectScopeActive,
	}); err != nil {
		t.Fatal(err)
	}
	if err := memory.UpsertProjectAgentBinding(ctx, model.ProjectAgentBinding{
		OperatorID: 1, MachineID: "machine", AgentID: "agent", ScopeID: "scope", Active: true,
	}); err != nil {
		t.Fatal(err)
	}
	items := []model.ProjectMemory{
		{ID: "rule", LogicalID: "rule", ScopeID: "scope", Kind: model.ProjectMemoryLockedRule, SubjectKey: "tests", Statement: "Run focused tests", Status: model.ProjectMemoryActive, Locked: true},
		{ID: "fact", LogicalID: "fact", ScopeID: "scope", Kind: model.ProjectMemoryVerifiedFact, SubjectKey: "libgame", Statement: "libgame uses arm64", Status: model.ProjectMemoryActive, Confidence: .99, Verification: model.ProjectMemoryVerification{Status: model.ProjectMemoryVerified}},
		{ID: "sensitive", LogicalID: "sensitive", ScopeID: "scope", Kind: model.ProjectMemoryVerifiedFact, SubjectKey: "token", Statement: "SECRET_VALUE", Status: model.ProjectMemoryActive, Sensitive: true},
		{ID: "stale", LogicalID: "stale", ScopeID: "scope", Kind: model.ProjectMemoryVerifiedFact, SubjectKey: "old", Statement: "OLD_OFFSET", Status: model.ProjectMemoryStale, Verification: model.ProjectMemoryVerification{Status: model.ProjectMemoryStaleCheck}},
		{ID: "failed", LogicalID: "failed", ScopeID: "scope", Kind: model.ProjectMemoryIssue, SubjectKey: "hook", Statement: "FAILED_HOOK", Status: model.ProjectMemoryActive, Verification: model.ProjectMemoryVerification{Status: model.ProjectMemoryFailed}},
		{ID: "missing-source", LogicalID: "missing-source", ScopeID: "scope", Kind: model.ProjectMemoryVerifiedFact, SubjectKey: "libgame", Statement: "MISSING_SOURCE_MEMORY", Status: model.ProjectMemoryActive, Verification: model.ProjectMemoryVerification{Status: model.ProjectMemoryVerified}, Sources: []model.ProjectMemorySource{{RelativePath: "src/missing.cc", Status: "source_missing"}}},
		{ID: "task-evidence", LogicalID: "task-evidence", ScopeID: "scope", Kind: model.ProjectMemoryVerifiedFact, SubjectKey: "libgame", Statement: "TASK_EVIDENCE_MEMORY", Status: model.ProjectMemoryActive, Verification: model.ProjectMemoryVerification{Status: model.ProjectMemoryVerified}, Sources: []model.ProjectMemorySource{{RelativePath: "src/missing-too.cc", Status: "changed"}, {TaskID: "task-evidence"}}},
	}
	for _, item := range items {
		if err := memory.UpsertProjectMemory(ctx, item); err != nil {
			t.Fatal(err)
		}
	}
	if err := memory.UpsertProjectMemoryBrief(ctx, model.ProjectMemoryBrief{ScopeID: "scope", Content: "Android native project", SourceRevision: 7}); err != nil {
		t.Fatal(err)
	}
	snapshot := Build(ctx, memory, "BASE_PROMPT", 1, "machine", "agent", "inspect libgame arm64")
	for _, expected := range []string{"BASE_PROMPT", "<project_locked_rules>", "Run focused tests", "<project_memory>", "Android native project", "libgame uses arm64", "TASK_EVIDENCE_MEMORY"} {
		if !strings.Contains(snapshot.System, expected) {
			t.Fatalf("missing %q in prompt:\n%s", expected, snapshot.System)
		}
	}
	for _, excluded := range []string{"SECRET_VALUE", "OLD_OFFSET", "FAILED_HOOK", "MISSING_SOURCE_MEMORY"} {
		if strings.Contains(snapshot.System, excluded) {
			t.Fatalf("invalid memory %q was injected", excluded)
		}
	}
	if snapshot.Revision != 7 || snapshot.ScopeID != "scope" || snapshot.Tokens > normalTokenBudget {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	RecordUsage(ctx, memory, "task-1", snapshot)
}

func TestAssembleKeepsXMLClosedWithinChineseTokenBudget(t *testing.T) {
	rule := model.ProjectMemory{
		ID: "rule", Kind: model.ProjectMemoryLockedRule, SubjectKey: "中文规则",
		Statement: strings.Repeat("必须验证", 6_000), Status: model.ProjectMemoryActive, Locked: true,
	}
	snapshot := assemble("", cachedCore{ScopeID: "scope", Revision: 1, Rules: []model.ProjectMemory{rule}}, nil)
	if snapshot.Tokens > absoluteTokenBudget {
		t.Fatalf("absolute budget exceeded: %d", snapshot.Tokens)
	}
	if !strings.Contains(snapshot.System, "</project_locked_rules>") {
		t.Fatalf("locked rules XML was truncated: %s", snapshot.System[len(snapshot.System)-80:])
	}
}

func TestCleanupCacheRemovesOnlyStaleEntries(t *testing.T) {
	coreCache.Lock()
	coreCache.items = map[string]cachedCore{
		"stale": {ScopeID: "stale", StoredAt: time.Now().Add(-time.Hour)},
		"fresh": {ScopeID: "fresh", StoredAt: time.Now()},
	}
	coreCache.Unlock()
	if removed := CleanupCache(time.Now().Add(-30 * time.Minute)); removed != 1 {
		t.Fatalf("removed=%d want=1", removed)
	}
	coreCache.RLock()
	_, stale := coreCache.items["stale"]
	_, fresh := coreCache.items["fresh"]
	coreCache.RUnlock()
	if stale || !fresh {
		t.Fatalf("unexpected cache state: stale=%v fresh=%v", stale, fresh)
	}
}

func TestMemoryScorePrioritizesExactNativeArtifactMetadata(t *testing.T) {
	exact := model.ProjectMemory{
		Kind: model.ProjectMemoryVerifiedFact, Confidence: .7,
		Artifacts: []model.ProjectMemoryArtifact{{
			ModuleName: "libgame.so", Symbol: "GameTick", ABI: "arm64-v8a", BuildID: "build-123",
		}},
	}
	weak := model.ProjectMemory{
		Kind: model.ProjectMemoryVerifiedFact, Confidence: 1,
		Statement: "Recent generic game information", UpdatedAt: time.Now().Add(time.Hour),
	}
	query := "Inspect GameTick in libgame.so for arm64-v8a build-123"
	if memoryScore(exact, query) <= memoryScore(weak, query) {
		t.Fatal("exact module/symbol/ABI/Build ID did not outrank weak text")
	}
}

func TestAcceptedMemoryIsInjectedIntoNextTaskAndTraceable(t *testing.T) {
	memory := store.NewMemory(nil)
	ctx := context.Background()
	if err := memory.UpsertProjectScope(ctx, model.ProjectScope{
		ID: "scope-e2e", OperatorID: 1, MachineID: "machine", Revision: 1, Status: model.ProjectScopeActive,
	}); err != nil {
		t.Fatal(err)
	}
	if err := memory.UpsertProjectAgentBinding(ctx, model.ProjectAgentBinding{
		OperatorID: 1, MachineID: "machine", AgentID: "agent", ScopeID: "scope-e2e", Active: true,
	}); err != nil {
		t.Fatal(err)
	}
	result, err := memory.ReconcileProjectMemories(ctx, 1, "scope-e2e", 1, []model.ProjectMemoryCandidate{{
		Kind: model.ProjectMemoryVerifiedFact, SubjectKey: "build:release",
		Statement: "Release builds require the verified signing task", Confidence: 1,
		Verification: model.ProjectMemoryVerification{Status: model.ProjectMemoryVerified},
		Sources: []model.ProjectMemorySource{{
			SessionID: "session-1", TaskID: "task-accepted", MessageIDs: []string{"message-1"},
			RelativePath: "build/release.sh", SHA256: strings.Repeat("a", 64), Excerpt: "SOURCE_EXCERPT_MUST_NOT_ENTER_PROMPT",
		}},
	}}, nil, nil)
	if err != nil || len(result.AcceptedIDs) != 1 {
		t.Fatalf("candidate was not accepted: result=%+v err=%v", result, err)
	}
	snapshot := Build(ctx, memory, "BASE_PROMPT", 1, "machine", "agent", "prepare release signing build")
	if !strings.Contains(snapshot.System, "Release builds require the verified signing task") {
		t.Fatalf("accepted memory was not available to the next task: %s", snapshot.System)
	}
	if strings.Contains(snapshot.System, "SOURCE_EXCERPT_MUST_NOT_ENTER_PROMPT") {
		t.Fatalf("untrusted source excerpt entered prompt: %s", snapshot.System)
	}
	for _, memoryID := range snapshot.MemoryIDs {
		item, err := memory.GetProjectMemory(ctx, 1, "scope-e2e", memoryID)
		if err != nil || item == nil || len(item.Sources) == 0 {
			t.Fatalf("injected memory %s has no traceable source: item=%+v err=%v", memoryID, item, err)
		}
	}
}

func TestProjectMemoryCacheIsolationAcrossOwnersMachinesAndScopes(t *testing.T) {
	CleanupCache(time.Now().Add(time.Hour))
	memory := store.NewMemory(nil)
	ctx := context.Background()
	fixtures := []struct {
		operator int64
		machine  string
		agent    string
		scope    string
	}{
		{operator: 1, machine: "machine-a", agent: "agent-a", scope: "scope-a"},
		{operator: 1, machine: "machine-b", agent: "agent-b", scope: "scope-b"},
		{operator: 2, machine: "machine-c", agent: "agent-c", scope: "scope-c"},
	}
	for _, fixture := range fixtures {
		if err := memory.UpsertProjectScope(ctx, model.ProjectScope{
			ID: fixture.scope, OperatorID: fixture.operator, MachineID: fixture.machine,
			Revision: 1, Status: model.ProjectScopeActive,
		}); err != nil {
			t.Fatal(err)
		}
		if err := memory.UpsertProjectAgentBinding(ctx, model.ProjectAgentBinding{
			OperatorID: fixture.operator, MachineID: fixture.machine, AgentID: fixture.agent,
			ScopeID: fixture.scope, Active: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := memory.UpsertProjectMemory(ctx, model.ProjectMemory{
		ID: "private-a", LogicalID: "private-a", ScopeID: "scope-a", Kind: model.ProjectMemoryLockedRule,
		SubjectKey: "private", Statement: "PRIVATE_SCOPE_A_RULE", Status: model.ProjectMemoryActive, Locked: true,
		Sources: []model.ProjectMemorySource{{TaskID: "task-a"}},
	}); err != nil {
		t.Fatal(err)
	}
	first := Build(ctx, memory, "BASE_A", 1, "machine-a", "agent-a", "private")
	if !strings.Contains(first.System, "PRIVATE_SCOPE_A_RULE") {
		t.Fatalf("source scope did not populate cache: %s", first.System)
	}
	for _, fixture := range fixtures[1:] {
		snapshot := Build(ctx, memory, "BASE_OTHER", fixture.operator, fixture.machine, fixture.agent, "private")
		if snapshot.System != "BASE_OTHER" || strings.Contains(snapshot.System, "PRIVATE_SCOPE_A_RULE") {
			t.Fatalf("cache leaked into operator=%d machine=%s scope=%s: %s",
				fixture.operator, fixture.machine, fixture.scope, snapshot.System)
		}
	}
}

func TestProjectMemoryTwentyThousandPromptAssemblyPerformance(t *testing.T) {
	if os.Getenv("PROJECT_MEMORY_PERFORMANCE") != "1" {
		t.Skip("set PROJECT_MEMORY_PERFORMANCE=1 to run the 20,000-memory prompt performance test")
	}
	CleanupCache(time.Now().Add(time.Hour))
	memory := store.NewMemory(nil)
	ctx := context.Background()
	if err := memory.UpsertProjectScope(ctx, model.ProjectScope{
		ID: "scope-performance", OperatorID: 1, MachineID: "machine", Revision: 20_001, Status: model.ProjectScopeActive,
	}); err != nil {
		t.Fatal(err)
	}
	if err := memory.UpsertProjectAgentBinding(ctx, model.ProjectAgentBinding{
		OperatorID: 1, MachineID: "machine", AgentID: "agent", ScopeID: "scope-performance", Active: true,
	}); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 20_000; index++ {
		statement := fmt.Sprintf("component %05d has a verified build procedure", index)
		if index%1000 == 0 {
			statement += " TARGET_PROMPT_MEMORY libgame.so arm64-v8a"
		}
		if err := memory.UpsertProjectMemory(ctx, model.ProjectMemory{
			ID: fmt.Sprintf("memory-%05d", index), LogicalID: fmt.Sprintf("logical-%05d", index),
			ScopeID: "scope-performance", Kind: model.ProjectMemoryVerifiedFact,
			SubjectKey: fmt.Sprintf("component:%05d", index), Statement: statement,
			Status: model.ProjectMemoryActive, Confidence: 1,
			Verification: model.ProjectMemoryVerification{Status: model.ProjectMemoryVerified},
			Sources:      []model.ProjectMemorySource{{TaskID: fmt.Sprintf("task-%05d", index)}},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := memory.UpsertProjectMemoryBrief(ctx, model.ProjectMemoryBrief{
		ScopeID: "scope-performance", Content: "Large project brief", SourceRevision: 20_001,
	}); err != nil {
		t.Fatal(err)
	}
	durations := make([]time.Duration, 25)
	for index := range durations {
		started := time.Now()
		snapshot := Build(ctx, memory, "BASE_PROMPT", 1, "machine", "agent", "TARGET_PROMPT_MEMORY libgame.so arm64-v8a")
		durations[index] = time.Since(started)
		if !strings.Contains(snapshot.System, "TARGET_PROMPT_MEMORY") || snapshot.Tokens > normalTokenBudget {
			t.Fatalf("invalid performance snapshot: tokens=%d system=%s", snapshot.Tokens, snapshot.System)
		}
		if durations[index] >= time.Second {
			t.Fatalf("complete prompt assembly exceeded one second: %s", durations[index])
		}
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p95 := durations[(len(durations)*95+99)/100-1]
	t.Logf("20,000-memory complete prompt assembly p95=%s samples=%v", p95, durations)
	if p95 >= 500*time.Millisecond {
		t.Fatalf("20,000-memory retrieval and prompt assembly p95 exceeded 500ms: %s", p95)
	}
}
