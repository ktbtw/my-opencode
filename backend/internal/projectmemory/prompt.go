package projectmemory

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"relay-server/internal/model"
	"relay-server/internal/store"
)

const (
	promptTimeout       = time.Second
	normalTokenBudget   = 12_000
	absoluteTokenBudget = 16_000
	briefTokenBudget    = 3_000
	dynamicTokenBudget  = 7_000
)

type Snapshot struct {
	System    string
	ScopeID   string
	Revision  int64
	MemoryIDs []string
	Tokens    int
}

type cachedCore struct {
	ScopeID  string
	Revision int64
	Rules    []model.ProjectMemory
	Brief    string
	StoredAt time.Time
}

var coreCache = struct {
	sync.RWMutex
	items map[string]cachedCore
}{items: map[string]cachedCore{}}

func estimateTokens(value string) int {
	ascii := 0
	nonASCII := 0
	for _, r := range value {
		if r > unicode.MaxASCII {
			nonASCII++
		} else {
			ascii++
		}
	}
	return nonASCII + (ascii+3)/4
}

func truncateTokens(value string, limit int) string {
	if limit <= 0 || estimateTokens(value) <= limit {
		return value
	}
	runes := []rune(value)
	low, high := 0, len(runes)
	for low < high {
		mid := (low + high + 1) / 2
		if estimateTokens(string(runes[:mid])) <= limit {
			low = mid
		} else {
			high = mid - 1
		}
	}
	return strings.TrimSpace(string(runes[:low]))
}

func QueryFromParts(parts []model.Part) string {
	var values []string
	for _, part := range parts {
		for _, value := range []string{part.Text, part.Prompt, part.Filename, part.Name, part.Command} {
			if value = strings.TrimSpace(value); value != "" {
				values = append(values, value)
			}
		}
	}
	return truncateTokens(strings.Join(values, "\n"), 1_000)
}

func validForPrompt(memory model.ProjectMemory) bool {
	if memory.Sensitive || memory.Status != model.ProjectMemoryActive {
		return false
	}
	switch memory.Verification.Status {
	case model.ProjectMemoryFailed, model.ProjectMemoryStaleCheck:
		return false
	}
	hasFileSource := false
	for _, source := range memory.Sources {
		if strings.TrimSpace(source.RelativePath) == "" {
			if strings.TrimSpace(source.TaskID) != "" || strings.TrimSpace(source.SessionID) != "" ||
				len(source.MessageIDs) > 0 || len(source.ToolCallIDs) > 0 || strings.TrimSpace(source.Excerpt) != "" {
				return true
			}
			continue
		}
		hasFileSource = true
		switch strings.ToLower(strings.TrimSpace(source.Status)) {
		case "", "available":
			return true
		}
	}
	return !hasFileSource
}

func memoryScore(memory model.ProjectMemory, query string) float64 {
	score := memory.Confidence * 100
	if memory.Locked || memory.Kind == model.ProjectMemoryLockedRule {
		score += 10_000
	}
	if memory.Verification.Status == model.ProjectMemoryVerified {
		score += 1_000
	}
	switch memory.Kind {
	case model.ProjectMemoryVerifiedFact, model.ProjectMemoryProcedure, model.ProjectMemoryDecision:
		score += 400
	case model.ProjectMemoryInferredFact, model.ProjectMemoryIssue:
		score += 150
	case model.ProjectMemoryEpisode:
		score -= 100
	}
	searchValues := []string{memory.SubjectKey, memory.Statement}
	searchValues = append(searchValues, memory.ScopePaths...)
	for _, artifact := range memory.Artifacts {
		searchValues = append(searchValues, artifact.Path, artifact.SHA256, artifact.APKSHA256,
			artifact.BuildID, artifact.ABI, artifact.ModuleName, artifact.Symbol, artifact.PackageName)
	}
	queryLower := strings.ToLower(query)
	for _, value := range searchValues[2:] {
		value = strings.TrimSpace(strings.ToLower(value))
		if len(value) >= 3 && strings.Contains(queryLower, value) {
			score += 5_000
		}
	}
	haystack := strings.ToLower(strings.Join(searchValues, " "))
	for _, token := range strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '.' || r == '/' || r == '-')
	}) {
		if len([]rune(token)) >= 2 && strings.Contains(haystack, token) {
			score += 250
		}
	}
	score += float64(memory.UpdatedAt.Unix()) / 1e9
	return score
}

func cacheKey(operatorID int64, machineID, scopeID string, revision int64) string {
	return fmt.Sprintf("%d\x00%s\x00%s\x00%d", operatorID, machineID, scopeID, revision)
}

func readCached(operatorID int64, machineID, scopeID string, revision int64) (cachedCore, bool) {
	coreCache.RLock()
	item, ok := coreCache.items[cacheKey(operatorID, machineID, scopeID, revision)]
	coreCache.RUnlock()
	return item, ok
}

func writeCached(operatorID int64, machineID string, item cachedCore) {
	coreCache.Lock()
	coreCache.items[cacheKey(operatorID, machineID, item.ScopeID, item.Revision)] = item
	if len(coreCache.items) > 256 {
		cutoff := time.Now().Add(-30 * time.Minute)
		for key, value := range coreCache.items {
			if value.StoredAt.Before(cutoff) {
				delete(coreCache.items, key)
			}
		}
	}
	coreCache.Unlock()
}

func CleanupCache(cutoff time.Time) int {
	coreCache.Lock()
	defer coreCache.Unlock()
	removed := 0
	for key, value := range coreCache.items {
		if value.StoredAt.Before(cutoff) {
			delete(coreCache.items, key)
			removed++
		}
	}
	return removed
}

func assemble(base string, core cachedCore, dynamic []model.ProjectMemory) Snapshot {
	var rules strings.Builder
	ids := make([]string, 0, len(core.Rules)+len(dynamic))
	for _, memory := range core.Rules {
		if !validForPrompt(memory) {
			continue
		}
		fmt.Fprintf(&rules, "- [%s] %s\n", memory.SubjectKey, memory.Statement)
		ids = append(ids, memory.ID)
	}

	rulesText := rules.String()
	rulesWrapperTokens := 0
	if rulesText != "" {
		rulesText = truncateTokens(rulesText, absoluteTokenBudget-100)
		rulesWrapperTokens = estimateTokens("<project_locked_rules>\n" + rulesText + "</project_locked_rules>\n\n")
	}
	contextBudget := normalTokenBudget - rulesWrapperTokens - 20
	if contextBudget < 0 {
		contextBudget = 0
	}
	briefBudget := min(briefTokenBudget, contextBudget)
	brief := truncateTokens(strings.TrimSpace(core.Brief), briefBudget)
	var contextBuilder strings.Builder
	if brief != "" {
		contextBuilder.WriteString("Project brief:\n")
		contextBuilder.WriteString(brief)
		contextBuilder.WriteString("\n")
	}
	usedDynamicTokens := 0
	dynamicBudget := min(dynamicTokenBudget, max(0, contextBudget-estimateTokens(contextBuilder.String())))
	for _, memory := range dynamic {
		line := fmt.Sprintf("- [%s; %s; %s] %s\n", memory.Kind, memory.Verification.Status, memory.SubjectKey, memory.Statement)
		lineTokens := estimateTokens(line)
		if usedDynamicTokens+lineTokens > dynamicBudget {
			continue
		}
		contextBuilder.WriteString(line)
		usedDynamicTokens += lineTokens
		ids = append(ids, memory.ID)
	}

	var memoryPrompt strings.Builder
	if rulesText != "" {
		memoryPrompt.WriteString("<project_locked_rules>\n")
		memoryPrompt.WriteString(rulesText)
		if !strings.HasSuffix(rulesText, "\n") {
			memoryPrompt.WriteString("\n")
		}
		memoryPrompt.WriteString("</project_locked_rules>\n\n")
	}
	if contextBuilder.Len() > 0 {
		memoryPrompt.WriteString("<project_memory>\n")
		memoryPrompt.WriteString(contextBuilder.String())
		memoryPrompt.WriteString("</project_memory>")
	}
	memoryText := memoryPrompt.String()
	system := strings.TrimSpace(base)
	if memoryText != "" {
		if system != "" {
			system += "\n\n"
		}
		system += memoryText
	}
	return Snapshot{System: system, ScopeID: core.ScopeID, Revision: core.Revision, MemoryIDs: ids, Tokens: estimateTokens(memoryText)}
}

// Build creates a task-stable memory snapshot. A retrieval failure leaves the
// existing prompt intact; a timeout uses the cached rules and brief when present.
func Build(ctx context.Context, memoryStore *store.Memory, base string, operatorID int64, machineID, agentID, query string) Snapshot {
	startedAt := time.Now()
	defer func() { DefaultMetrics.Retrieval(time.Since(startedAt)) }()
	result := Snapshot{System: strings.TrimSpace(base)}
	if memoryStore == nil || operatorID == 0 || strings.TrimSpace(machineID) == "" || strings.TrimSpace(agentID) == "" {
		return result
	}
	retrievalCtx, cancel := context.WithTimeout(ctx, promptTimeout)
	defer cancel()
	scope, err := memoryStore.GetProjectScopeForAgent(retrievalCtx, operatorID, machineID, agentID)
	if err != nil || scope == nil {
		return result
	}
	result.ScopeID, result.Revision = scope.ID, scope.Revision
	locked := true
	rules, rulesErr := memoryStore.ListProjectMemories(retrievalCtx, model.ProjectMemoryFilter{
		OperatorID: operatorID, MachineID: machineID, ScopeID: scope.ID,
		Status: model.ProjectMemoryActive, Locked: &locked, Limit: 200,
	})
	brief, briefErr := memoryStore.GetProjectMemoryBrief(retrievalCtx, operatorID, scope.ID)
	if rulesErr != nil || briefErr != nil || retrievalCtx.Err() != nil {
		if cached, ok := readCached(operatorID, machineID, scope.ID, scope.Revision); ok {
			DefaultMetrics.CacheFallback()
			return assemble(base, cached, nil)
		}
		return result
	}
	core := cachedCore{ScopeID: scope.ID, Revision: scope.Revision, Rules: rules, StoredAt: time.Now()}
	if brief != nil && brief.SourceRevision <= scope.Revision {
		core.Brief = brief.Content
	}
	writeCached(operatorID, machineID, core)

	dynamic, err := memoryStore.ListProjectMemories(retrievalCtx, model.ProjectMemoryFilter{
		OperatorID: operatorID, MachineID: machineID, ScopeID: scope.ID,
		Status: model.ProjectMemoryActive, Query: strings.TrimSpace(query), Limit: 200,
	})
	if err != nil || retrievalCtx.Err() != nil {
		return assemble(base, core, nil)
	}
	ruleIDs := map[string]bool{}
	for _, rule := range rules {
		ruleIDs[rule.ID] = true
	}
	filtered := dynamic[:0]
	for _, memory := range dynamic {
		if validForPrompt(memory) && !ruleIDs[memory.ID] {
			filtered = append(filtered, memory)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return memoryScore(filtered[i], query) > memoryScore(filtered[j], query)
	})
	return assemble(base, core, filtered)
}

func RecordUsage(ctx context.Context, memoryStore *store.Memory, taskID string, snapshot Snapshot) {
	if memoryStore == nil || snapshot.ScopeID == "" || strings.TrimSpace(taskID) == "" {
		return
	}
	DefaultMetrics.Injected(snapshot.Tokens)
	_ = memoryStore.RecordProjectMemoryUsage(ctx, model.ProjectMemoryUsage{
		ScopeID: snapshot.ScopeID, TaskID: taskID, Revision: snapshot.Revision,
		MemoryIDs: append([]string(nil), snapshot.MemoryIDs...), TokenCount: snapshot.Tokens,
		CreatedAt: time.Now().UTC(),
	})
}
