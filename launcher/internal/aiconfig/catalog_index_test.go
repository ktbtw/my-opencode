package aiconfig

import (
	"strings"
	"testing"

	"launcher/internal/model"
)

// 索引化后必须与逐候选暴力遍历选出完全相同的候选。
// 这是本次性能优化的正确性底线：分数与「同分取先出现者」的语义都不能变。
func TestCatalogIndexMatchesBruteForce(t *testing.T) {
	candidates := []catalogCandidate{
		{provider: "openai", id: "gpt-5.4", meta: RuntimeModelMetadata{ID: "gpt-5.4", Name: "GPT-5.4"}},
		{provider: "openai", id: "gpt-5.4-codex", meta: RuntimeModelMetadata{ID: "gpt-5.4-codex", Name: "GPT-5.4 Codex"}},
		{provider: "xai", id: "grok-4", meta: RuntimeModelMetadata{ID: "grok-4", Name: "Grok 4"}},
		{provider: "deepseek", id: "deepseek-v3.2", meta: RuntimeModelMetadata{ID: "deepseek-v3.2", Name: "DeepSeek V3.2"}},
		{provider: "relay-a", id: "vendor/gpt-5.4", meta: RuntimeModelMetadata{ID: "vendor/gpt-5.4", Name: "AI"}},
		{provider: "relay-b", id: "claude-sonnet-4-6", meta: RuntimeModelMetadata{ID: "claude-sonnet-4-6", Name: "Claude Sonnet 4.6"}},
		{provider: "relay-c", id: "kimi-k2.5", meta: RuntimeModelMetadata{ID: "kimi-k2.5", Name: "kimi2.5"}},
		{provider: "relay-d", id: "qwen3-max", meta: RuntimeModelMetadata{ID: "qwen3-max", Name: "Qwen3 Max"}},
		{provider: "relay-e", id: "glm-4.6", meta: RuntimeModelMetadata{ID: "glm-4.6", Name: "GLM-4.6"}},
		{provider: "relay-f", id: "gpt", meta: RuntimeModelMetadata{ID: "gpt", Name: "GPT"}},
		{provider: "relay-g", id: "ai", meta: RuntimeModelMetadata{ID: "ai", Name: "AI"}},
		{provider: "relay-h", id: "minimax-m2", meta: RuntimeModelMetadata{ID: "minimax-m2", Name: "MiniMax M2"}},
		{provider: "relay-i", id: "llama-4", meta: RuntimeModelMetadata{ID: "llama-4", Name: "Llama 4"}},
		{provider: "relay-j", id: "gemini-3-pro", meta: RuntimeModelMetadata{ID: "gemini-3-pro", Name: "Gemini 3 Pro"}},
		{provider: "relay-k", id: "command-r", meta: RuntimeModelMetadata{ID: "command-r", Name: "Command R"}},
		{provider: "relay-l", id: "yi-large", meta: RuntimeModelMetadata{ID: "yi-large", Name: "Yi Large"}},
		{provider: "relay-m", id: "doubao-pro", meta: RuntimeModelMetadata{ID: "doubao-pro", Name: "Doubao Pro"}},
		// 同一家族多个候选，用于验证「同分取先出现者」的顺序语义。
		{provider: "relay-n", id: "gpt-5.4", meta: RuntimeModelMetadata{ID: "gpt-5.4", Name: "GPT-5.4"}},
		{provider: "relay-o", id: "gpt-5.4", meta: RuntimeModelMetadata{ID: "gpt-5.4", Name: "GPT-5.4"}},
	}
	index := newCatalogIndex(candidates)

	targets := []model.DeviceAIModelInfo{
		{ID: "gpt-5.4", Name: "gpt-5.4"},
		{ID: "gpt-5.4-codex", Name: "gpt-5.4-codex"},
		{ID: "gpt-5.4-pro", Name: "gpt-5.4-pro"},
		{ID: "GPT", Name: "GPT"},
		{ID: "ai", Name: "AI"},
		{ID: "grok-4", Name: "grok-4"},
		{ID: "grok", Name: "grok"},
		{ID: "deepseek-v3.2", Name: "deepseek-v3.2"},
		{ID: "deepseek", Name: "deepseek"},
		{ID: "claude-sonnet-4-6", Name: "claude-sonnet-4-6"},
		{ID: "claude", Name: "claude"},
		{ID: "kimi2.5", Name: "kimi2.5"},
		{ID: "kimi3", Name: "kimi3"},
		{ID: "qwen3-max", Name: "qwen3-max"},
		{ID: "glm-4.6", Name: "glm-4.6"},
		{ID: "minimax-m2", Name: "minimax-m2"},
		{ID: "llama-4", Name: "llama-4"},
		{ID: "gemini-3-pro", Name: "gemini-3-pro"},
		{ID: "command-r", Name: "command-r"},
		{ID: "yi-large", Name: "yi-large"},
		{ID: "doubao-pro", Name: "doubao-pro"},
		{ID: "unknown-model-xyz", Name: "unknown-model-xyz"},
		{ID: "", Name: ""},
		{ID: "vendor/gpt-5.4", Name: ""},
	}
	providers := []string{"relay", "openai", "xai", "vendor", "自己用"}

	for _, provider := range providers {
		for _, target := range targets {
			want := bruteForceMatchCatalogModel(provider, target, candidates)
			got := index.match(provider, target)
			if (want == nil) != (got == nil) {
				t.Fatalf("provider=%q model=%q: 索引与暴力遍历不一致（一方为空）: want=%v got=%v",
					provider, target.ID, want, got)
			}
			if want != nil && (want.provider != got.provider || want.id != got.id) {
				t.Fatalf("provider=%q model=%q: 索引选中 %s/%s，暴力遍历选中 %s/%s",
					provider, target.ID, got.provider, got.id, want.provider, want.id)
			}
		}
	}
}

// 索引化不能改变端到端的预设填充结果：用同一份候选分别走新旧匹配，
// 比较每个模型最终拿到的预设来源与上下文/输出上限。
func TestCatalogIndexPreservesEnrichmentOutcome(t *testing.T) {
	candidates := []catalogCandidate{
		{provider: "openai", id: "gpt-5.4", meta: RuntimeModelMetadata{
			ID: "gpt-5.4", Name: "GPT-5.4",
			Limit: RuntimeModelLimit{Context: 400000, Output: 128000},
		}},
		{provider: "relay-x", id: "deepseek-v3.2", meta: RuntimeModelMetadata{
			ID: "deepseek-v3.2", Name: "DeepSeek V3.2",
			Limit: RuntimeModelLimit{Context: 128000, Output: 32000},
		}},
		{provider: "relay-y", id: "vendor/claude-sonnet-4-6", meta: RuntimeModelMetadata{
			ID: "vendor/claude-sonnet-4-6", Name: "Claude Sonnet 4.6",
			Limit: RuntimeModelLimit{Context: 200000, Output: 64000},
		}},
	}
	index := newCatalogIndex(candidates)
	items := []model.DeviceAIModelInfo{
		{ID: "gpt-5.4", Name: "gpt-5.4"},
		{ID: "deepseek-v3.2", Name: "deepseek-v3.2"},
		{ID: "claude-sonnet-4-6", Name: "claude-sonnet-4-6"},
		{ID: "not-in-catalog", Name: "not-in-catalog"},
	}
	for _, provider := range []string{"relay", "openai"} {
		for _, item := range items {
			want := bruteForceMatchCatalogModel(provider, item, candidates)
			got := index.match(provider, item)
			wantPreset, gotPreset := "", ""
			if want != nil {
				wantPreset = want.provider + "/" + want.id
			}
			if got != nil {
				gotPreset = got.provider + "/" + got.id
			}
			if wantPreset != gotPreset {
				t.Fatalf("provider=%q model=%q: 预设来源不一致 want=%q got=%q",
					provider, item.ID, wantPreset, gotPreset)
			}
		}
	}
}

// 索引必须覆盖所有候选，不能因为倒排表遗漏而漏掉本应命中的候选。
func TestCatalogIndexCoversAllCandidates(t *testing.T) {
	candidates := []catalogCandidate{
		{provider: "p1", id: "model-a", meta: RuntimeModelMetadata{Name: "Model A"}},
		{provider: "p2", id: "model-b", meta: RuntimeModelMetadata{Name: "Model B"}},
		{provider: "p3", id: "vendor/model-c", meta: RuntimeModelMetadata{}},
		{provider: "p4", id: "", meta: RuntimeModelMetadata{Name: "model-d"}},
	}
	index := newCatalogIndex(candidates)
	for i, candidate := range candidates {
		registered := false
		for _, ids := range index.exact {
			for _, id := range ids {
				if id == i {
					registered = true
				}
			}
		}
		if !registered {
			t.Fatalf("候选 %d (%s/%s) 未登记进倒排表，将永远无法命中",
				i, candidate.provider, candidate.id)
		}
	}
	// 名称兜底：id 为空时仍应能通过 meta.Name 命中。
	if got := index.match("p4", model.DeviceAIModelInfo{ID: "model-d"}); got == nil || got.provider != "p4" {
		t.Fatalf("仅靠名称的候选未被命中: %v", got)
	}
}

// 倒排表按候选下标升序去重，保证与暴力遍历的「先出现者优先」一致。
func TestCatalogIndexKeepsFirstOnTie(t *testing.T) {
	candidates := []catalogCandidate{
		{provider: "first", id: "same-model", meta: RuntimeModelMetadata{Name: "Same"}},
		{provider: "second", id: "same-model", meta: RuntimeModelMetadata{Name: "Same"}},
	}
	index := newCatalogIndex(candidates)
	got := index.match("relay", model.DeviceAIModelInfo{ID: "same-model"})
	if got == nil || got.provider != "first" {
		t.Fatalf("同分时应取先出现的候选，实际: %v", got)
	}
	want := bruteForceMatchCatalogModel("relay", model.DeviceAIModelInfo{ID: "same-model"}, candidates)
	if want == nil || want.provider != got.provider {
		t.Fatalf("与暴力遍历的 tie-break 不一致: want=%v got=%v", want, got)
	}
}

// bruteForceMatchCatalogModel 是索引化之前的原始实现，仅用作等价性基准。
func bruteForceMatchCatalogModel(provider string, item model.DeviceAIModelInfo, candidates []catalogCandidate) *catalogCandidate {
	aliases := []string{catalogAlias(item.ID), catalogAlias(lastPathSegment(item.ID)), catalogAlias(item.Name), catalogAlias(lastPathSegment(item.Name))}
	bestScore := 10000
	var best *catalogCandidate
	for i := range candidates {
		candidate := &candidates[i]
		keys := []string{catalogAlias(candidate.id), catalogAlias(lastPathSegment(candidate.id)), catalogAlias(candidate.meta.Name)}
		for ai, alias := range aliases {
			if alias == "" {
				continue
			}
			for ki, key := range keys {
				exactMatch := alias == key
				family := catalogFamily(alias)
				candidateFamily := catalogCandidateFamily(*candidate)
				familyMatch := !exactMatch && family != "" && family == candidateFamily
				if !exactMatch && family == "ai" && candidateFamily == "gpt" {
					familyMatch = true
				}
				if !exactMatch && !familyMatch {
					continue
				}
				score := ai*20 + ki*5 + 10
				if familyMatch {
					score += 300
					score -= catalogGenericVersionScore(candidate.id) / 100
				}
				if canonicalProvider(candidate.provider) {
					score -= 12
				}
				if strings.EqualFold(candidate.provider, item.OwnedBy) {
					score -= 3
				}
				if strings.EqualFold(candidate.provider, provider) {
					score -= 4
				}
				if candidate.runtime && len(candidate.meta.Variants) > 0 &&
					(strings.EqualFold(candidate.provider, provider) || strings.EqualFold(candidate.provider, item.OwnedBy)) {
					score -= 60
				}
				if score < bestScore {
					best, bestScore = candidate, score
				}
			}
		}
	}
	return best
}
