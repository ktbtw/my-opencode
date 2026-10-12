package aiconfig

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"launcher/internal/model"
)

// catalogRefreshInterval 是两次联网刷新目录之间的最小间隔。
const catalogRefreshInterval = 5 * time.Minute

// OpenCode reads this same models.dev schema. Runtime metadata is preferred:
// it also contains ProviderTransform's current reasoning variants.
type catalogProvider struct {
	ID     string `json:"id"`
	Models map[string]struct {
		ID               string                    `json:"id"`
		Name             string                    `json:"name"`
		Reasoning        bool                      `json:"reasoning"`
		ReasoningOptions []catalogReasoningOption  `json:"reasoning_options"`
		Limit            RuntimeModelLimit         `json:"limit"`
		Modalities       *model.DeviceAIModalities `json:"modalities"`
	} `json:"models"`
}

type catalogReasoningOption struct {
	Type   string   `json:"type"`
	Values []string `json:"values"`
}

type catalogSnapshot struct {
	path    string
	updated time.Time
}

var catalogCache struct {
	sync.Mutex
	key         string
	checked     time.Time
	filePath    string
	fileUpdated time.Time
	providers   []RuntimeProviderMetadataItem
}

// SyncOpenCodeModelCatalog refreshes the catalogue explicitly when users fetch
// a relay's models. A timeout or bad response keeps the last valid snapshot.
func SyncOpenCodeModelCatalog() { loadOpenCodeCatalog(true) }

// EnsureOpenCodeModelCatalog 立即返回当前目录，并在快照过期时后台刷新。
//
// 对话页拉取模型列表与 AI 配置页读取配置都走设备 RPC，它们只需要模型 ID
// 与名称；目录只提供可选的预设元数据（上下文窗口、模态、思考档位）。因此
// 联网刷新绝不能挡在请求路径上：models.dev 慢或不可达时，请求必须立刻拿到
// 上一次的有效快照，而不是等满 10 秒超时。
func EnsureOpenCodeModelCatalog() []RuntimeProviderMetadataItem {
	paths := resolveCatalogPaths()
	providers, claimed := catalogSnapshotAndClaim(paths, true)
	if claimed {
		go fetchAndApplyCatalog(paths, providers)
	}
	return providers
}

type catalogPaths struct {
	// source 是目录来源地址，可用 OPENCODE_MODELS_URL 覆盖。
	source string
	// openCodePath 是 OpenCode 自身的缓存文件，也是缓存作用域的一部分。
	openCodePath string
	// launcherPath 是本 launcher 写回的目录快照，避免与 OpenCode 争用跨进程锁。
	launcherPath string
	// readPaths 是按优先级读取的候选路径。
	readPaths []string
}

func resolveCatalogPaths() catalogPaths {
	source := strings.TrimRight(firstNonEmpty(os.Getenv("OPENCODE_MODELS_URL"), "https://models.dev"), "/")
	home, _ := os.UserHomeDir()
	cacheRoot := firstNonEmpty(os.Getenv("XDG_CACHE_HOME"), filepath.Join(home, ".cache"))
	openCodePath := firstNonEmpty(os.Getenv("OPENCODE_MODELS_PATH"), filepath.Join(cacheRoot, "opencode", "models.json"))
	launcherPath := filepath.Join(cacheRoot, "chat-codex", "model-catalog.json")
	readPaths := []string{openCodePath}
	// 显式指定 OPENCODE_MODELS_PATH 时只认它，与 OpenCode 的语义保持一致。
	if os.Getenv("OPENCODE_MODELS_PATH") == "" {
		readPaths = append(readPaths, launcherPath)
	}
	return catalogPaths{
		source:       source,
		openCodePath: openCodePath,
		launcherPath: launcherPath,
		readPaths:    readPaths,
	}
}

// catalogSnapshotAndClaim 在锁内载入磁盘快照，并在需要时认领一次联网刷新。
//
// 返回当前可用目录，以及本次调用是否获得了刷新资格。认领即把 checked 提前
// 置位，因此并发的调用方不会重复抓取；真正的网络请求由调用方在锁外执行。
func catalogSnapshotAndClaim(paths catalogPaths, refresh bool) ([]RuntimeProviderMetadataItem, bool) {
	key := paths.source + "|" + paths.openCodePath
	catalogCache.Lock()
	defer catalogCache.Unlock()
	if catalogCache.key != key {
		catalogCache.key = key
		catalogCache.checked = time.Time{}
		catalogCache.filePath = ""
		catalogCache.fileUpdated = time.Time{}
		catalogCache.providers = nil
	}
	var snapshots []catalogSnapshot
	for _, readPath := range paths.readPaths {
		if stat, err := os.Stat(readPath); err == nil {
			snapshots = append(snapshots, catalogSnapshot{readPath, stat.ModTime()})
		}
	}
	// A successful relay refresh can be newer than OpenCode's own disk cache.
	// Restarting offline must keep that snapshot instead of an older catalogue.
	sort.SliceStable(snapshots, func(i, j int) bool { return snapshots[i].updated.After(snapshots[j].updated) })
	for _, snapshot := range snapshots {
		if snapshot.path == catalogCache.filePath && snapshot.updated.Equal(catalogCache.fileUpdated) {
			break
		}
		if data, err := os.ReadFile(snapshot.path); err == nil {
			if providers := decodeOpenCodeCatalog(data); len(providers) > 0 {
				catalogCache.providers = providers
				catalogCache.filePath = snapshot.path
				catalogCache.fileUpdated = snapshot.updated
				break
			}
		}
	}
	if !refresh ||
		os.Getenv("OPENCODE_DISABLE_MODELS_FETCH") != "" ||
		os.Getenv("OPENCODE_MODELS_PATH") != "" ||
		time.Since(catalogCache.checked) < catalogRefreshInterval {
		return catalogCache.providers, false
	}
	catalogCache.checked = time.Now()
	return catalogCache.providers, true
}

// fetchAndApplyCatalog 在锁外抓取目录；成功后写回内存与磁盘，失败则沿用快照。
func fetchAndApplyCatalog(paths catalogPaths, fallback []RuntimeProviderMetadataItem) []RuntimeProviderMetadataItem {
	client := &http.Client{Timeout: 10 * time.Second}
	response, err := client.Get(paths.source + "/api.json")
	if err != nil {
		return fallback
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fallback
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return fallback
	}
	providers := decodeOpenCodeCatalog(data)
	if len(providers) == 0 {
		return fallback
	}
	catalogCache.Lock()
	defer catalogCache.Unlock()
	catalogCache.providers = providers
	// Use our own file; never race OpenCode's cross-process cache lock.
	if os.MkdirAll(filepath.Dir(paths.launcherPath), 0700) == nil {
		if tmp, err := os.CreateTemp(filepath.Dir(paths.launcherPath), "catalog-*"); err == nil {
			name := tmp.Name()
			_, err = tmp.Write(data)
			closeErr := tmp.Close()
			if err == nil && closeErr == nil {
				if os.Rename(name, paths.launcherPath) == nil {
					if stat, err := os.Stat(paths.launcherPath); err == nil {
						catalogCache.filePath = paths.launcherPath
						catalogCache.fileUpdated = stat.ModTime()
					}
				}
			}
			_ = os.Remove(name)
		}
	}
	return providers
}

func loadOpenCodeCatalog(refresh bool) []RuntimeProviderMetadataItem {
	paths := resolveCatalogPaths()
	providers, claimed := catalogSnapshotAndClaim(paths, refresh)
	if !claimed {
		return providers
	}
	return fetchAndApplyCatalog(paths, providers)
}

func decodeOpenCodeCatalog(data []byte) []RuntimeProviderMetadataItem {
	var raw map[string]catalogProvider
	if json.Unmarshal(data, &raw) != nil {
		return nil
	}
	ids := make([]string, 0, len(raw))
	for id := range raw {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var result []RuntimeProviderMetadataItem
	for _, id := range ids {
		provider := raw[id]
		models := map[string]RuntimeModelMetadata{}
		for modelID, item := range provider.Models {
			variants := catalogReasoningVariantsFromOptions(item.ReasoningOptions)
			models[modelID] = RuntimeModelMetadata{
				ID: firstNonEmpty(item.ID, modelID), ProviderID: id, Name: item.Name,
				Limit: item.Limit, Modalities: item.Modalities,
				Capabilities: RuntimeModelCapabilities{Reasoning: item.Reasoning},
				Variants:     variants,
			}
		}
		if len(models) > 0 {
			result = append(result, RuntimeProviderMetadataItem{ID: id, Models: models})
		}
	}
	return result
}

func catalogReasoningVariantsFromOptions(options []catalogReasoningOption) map[string]any {
	variants := map[string]any{}
	for _, option := range options {
		kind := strings.ToLower(strings.TrimSpace(option.Type))
		for _, value := range option.Values {
			value = strings.TrimSpace(value)
			if value == "" {
				continue
			}
			if kind == "budget" || kind == "token_budget" {
				variants[value] = map[string]any{"reasoningBudget": value}
			} else {
				variants[value] = map[string]any{"reasoningEffort": value}
			}
		}
	}
	if len(variants) == 0 {
		return nil
	}
	return variants
}

type catalogCandidate struct {
	provider string
	id       string
	meta     RuntimeModelMetadata
	runtime  bool
}

var catalogSeparators = regexp.MustCompile(`[\s_]+`)
var catalogGPT5Family = regexp.MustCompile(`^gpt-5([.-]|$)`)
var catalogGPT5Version = regexp.MustCompile(`^gpt-5[.-](\d+)(?:[.-]|$)`)
var catalogGenericVersionPattern = regexp.MustCompile(`^(?:gpt|grok)[-_.]?(\d+)(?:[-_.](\d+))?`)

func catalogAlias(value string) string {
	value = catalogSeparators.ReplaceAllString(strings.ToLower(strings.TrimSpace(value)), "-")
	switch value {
	case "kimi3", "kimi-3", "kimi-k-3":
		return "kimi-k3"
	case "kimi2.5", "kimi-2.5", "kimi-k2p5":
		return "kimi-k2.5"
	}
	return value
}

func canonicalProvider(id string) bool {
	switch strings.ToLower(id) {
	case "openai", "anthropic", "google", "xai", "x-ai", "moonshotai", "moonshotai-cn", "deepseek", "minimax", "zhipuai", "zai":
		return true
	}
	return false
}

func catalogFamily(value string) string {
	value = strings.ReplaceAll(catalogAlias(value), "-", "")
	switch {
	case value == "gpt" || value == "chatgpt" || value == "openai":
		return "gpt"
	case value == "grok":
		return "grok"
	case value == "kimi3" || value == "kimik3":
		return "kimi3"
	case value == "kimi" || value == "moonshot":
		return "kimi"
	case value == "claude" || value == "anthropic":
		return "claude"
	case value == "gemini" || value == "google":
		return "gemini"
	case value == "deepseek":
		return "deepseek"
	case value == "qwen" || value == "tongyi":
		return "qwen"
	case value == "llama" || value == "meta":
		return "llama"
	case value == "mistral" || value == "mixtral":
		return "mistral"
	case value == "minimax":
		return "minimax"
	case value == "glm" || value == "chatglm" || value == "zhipu":
		return "glm"
	case value == "command" || value == "cohere":
		return "command"
	case value == "yi" || value == "01ai":
		return "yi"
	case value == "doubao" || value == "ark":
		return "doubao"
	case value == "ai":
		return "ai"
	}
	return ""
}

func catalogCandidateFamily(candidate catalogCandidate) string {
	for _, value := range []string{candidate.id, lastPathSegment(candidate.id), candidate.meta.Name} {
		compact := strings.ReplaceAll(catalogAlias(value), "-", "")
		switch {
		case strings.HasPrefix(compact, "gpt"):
			return "gpt"
		case strings.HasPrefix(compact, "grok"):
			return "grok"
		case strings.HasPrefix(compact, "kimik3") || strings.HasPrefix(compact, "kimi3"):
			return "kimi3"
		case strings.HasPrefix(compact, "kimi") || strings.HasPrefix(compact, "moonshot"):
			return "kimi"
		case strings.HasPrefix(compact, "claude") || strings.HasPrefix(compact, "anthropic"):
			return "claude"
		case strings.HasPrefix(compact, "gemini") || strings.HasPrefix(compact, "google"):
			return "gemini"
		case strings.HasPrefix(compact, "deepseek"):
			return "deepseek"
		case strings.HasPrefix(compact, "qwen") || strings.HasPrefix(compact, "tongyi"):
			return "qwen"
		case strings.HasPrefix(compact, "llama") || strings.HasPrefix(compact, "meta"):
			return "llama"
		case strings.HasPrefix(compact, "mistral") || strings.HasPrefix(compact, "mixtral"):
			return "mistral"
		case strings.HasPrefix(compact, "minimax"):
			return "minimax"
		case strings.HasPrefix(compact, "glm") || strings.HasPrefix(compact, "chatglm") || strings.HasPrefix(compact, "zhipu"):
			return "glm"
		case strings.HasPrefix(compact, "command") || strings.HasPrefix(compact, "cohere"):
			return "command"
		case strings.HasPrefix(compact, "yi") || strings.HasPrefix(compact, "01ai"):
			return "yi"
		case strings.HasPrefix(compact, "doubao") || strings.HasPrefix(compact, "ark"):
			return "doubao"
		}
	}
	return ""
}

func catalogGenericVersionScore(value string) int {
	match := catalogGenericVersionPattern.FindStringSubmatch(strings.ToLower(lastPathSegment(value)))
	if len(match) == 0 {
		return 0
	}
	major, _ := strconv.Atoi(match[1])
	minor := 0
	if len(match) > 2 {
		minor, _ = strconv.Atoi(match[2])
	}
	return major*100 + minor
}

// catalogCandidateDerived 是候选的匹配属性缓存。
//
// 这些值只依赖候选自身，与待匹配的模型无关；此前它们被写在 (alias, key)
// 双层循环内部，同一个候选要重复计算 4×3=12 次。目录规模约 226 个供应商、
// 8000+ 个模型时，单次匹配因此要几百毫秒。
type catalogCandidateDerived struct {
	family          string
	genericVersion  int
	canonical       bool
	runtimeVariants bool
}

// catalogIndex 预计算候选的匹配键与属性，把「遍历全部候选」收敛成
// 「只评估可能命中的候选」。
//
// 命中条件只有两种：某个匹配键与模型别名完全相同，或候选家族与别名家族
// 相同。因此按这两种关系各建一张倒排表，未出现在表中的候选不可能命中，
// 可直接跳过。候选按原下标升序评估，保持与暴力遍历一致的「同分取先出现者」。
type catalogIndex struct {
	candidates []catalogCandidate
	derived    []catalogCandidateDerived
	keys       [][3]string
	exact      map[string][]int
	family     map[string][]int
}

func newCatalogIndex(candidates []catalogCandidate) *catalogIndex {
	index := &catalogIndex{
		candidates: candidates,
		derived:    make([]catalogCandidateDerived, len(candidates)),
		keys:       make([][3]string, len(candidates)),
		exact:      make(map[string][]int, len(candidates)),
		family:     make(map[string][]int),
	}
	for i := range candidates {
		candidate := &candidates[i]
		keys := [3]string{
			catalogAlias(candidate.id),
			catalogAlias(lastPathSegment(candidate.id)),
			catalogAlias(candidate.meta.Name),
		}
		index.keys[i] = keys
		family := catalogCandidateFamily(*candidate)
		index.derived[i] = catalogCandidateDerived{
			family:          family,
			genericVersion:  catalogGenericVersionScore(candidate.id),
			canonical:       canonicalProvider(candidate.provider),
			runtimeVariants: candidate.runtime && len(candidate.meta.Variants) > 0,
		}
		// 同一个键可能同时来自 id 与 name，去重避免重复登记同一候选。
		seen := map[string]bool{}
		for _, key := range keys {
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			index.exact[key] = append(index.exact[key], i)
		}
		if family != "" {
			index.family[family] = append(index.family[family], i)
		}
	}
	return index
}

// Exact model IDs are preferred. Only deliberately generic aliases (GPT,
// Grok, AI, Kimi3) use a family match, so GPT-5.4 still cannot inherit
// GPT-5.4-Codex or GPT-50. Relay/vendor path prefixes and display-name
// spacing are normalized without changing the advertised ID saved to the
// provider.
func (index *catalogIndex) match(provider string, item model.DeviceAIModelInfo) *catalogCandidate {
	aliases := [4]string{
		catalogAlias(item.ID),
		catalogAlias(lastPathSegment(item.ID)),
		catalogAlias(item.Name),
		catalogAlias(lastPathSegment(item.Name)),
	}
	var aliasFamilies [4]string
	for i, alias := range aliases {
		aliasFamilies[i] = catalogFamily(alias)
	}

	hits := make([]int, 0, 32)
	for i, alias := range aliases {
		if alias == "" {
			continue
		}
		hits = append(hits, index.exact[alias]...)
		family := aliasFamilies[i]
		if family == "" {
			continue
		}
		hits = append(hits, index.family[family]...)
		// "AI" 是兼容 OpenAI 的中转站常用展示名，家族匹配时按 GPT 处理；
		// 仍只对显式使用该泛化别名的模型生效。
		if family == "ai" {
			hits = append(hits, index.family["gpt"]...)
		}
	}
	sort.Ints(hits)

	bestScore := 10000
	var best *catalogCandidate
	previous := -1
	for _, ci := range hits {
		if ci == previous {
			continue
		}
		previous = ci
		candidate := &index.candidates[ci]
		derived := &index.derived[ci]
		keys := index.keys[ci]
		ownedByMatch := strings.EqualFold(candidate.provider, item.OwnedBy)
		providerMatch := strings.EqualFold(candidate.provider, provider)
		for ai, alias := range aliases {
			if alias == "" {
				continue
			}
			for ki, key := range keys {
				exactMatch := alias == key
				family := aliasFamilies[ai]
				familyMatch := !exactMatch && family != "" && family == derived.family
				if !exactMatch && family == "ai" && derived.family == "gpt" {
					familyMatch = true
				}
				if !exactMatch && !familyMatch {
					continue
				}
				score := ai*20 + ki*5 + 10
				if familyMatch {
					score += 300
					score -= derived.genericVersion / 100
				}
				if derived.canonical {
					score -= 12
				}
				if ownedByMatch {
					score -= 3
				}
				if providerMatch {
					score -= 4
				}
				// A connected runtime may expose relay-specific variants that are
				// more accurate than the public catalogue. Prefer those variants
				// for the same configured provider, while still allowing a native
				// OpenCode provider to supply defaults for a bare relay model.
				if derived.runtimeVariants && (providerMatch || ownedByMatch) {
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

// matchCatalogModel 保留按候选切片直接匹配的入口，供单次调用与测试使用。
func matchCatalogModel(provider string, item model.DeviceAIModelInfo, candidates []catalogCandidate) *catalogCandidate {
	return newCatalogIndex(candidates).match(provider, item)
}

func EnrichWithOpenCodeCatalog(info *model.DeviceAIConfigInfo, metadata RuntimeProviderMetadata) *model.DeviceAIConfigInfo {
	if info == nil {
		return nil
	}
	providers := EnsureOpenCodeModelCatalog()
	// Runtime versions win for identical provider/model pairs. All native
	// providers are considered, including ones without a configured API key.
	byKey := map[string]catalogCandidate{}
	for _, group := range providers {
		for id, meta := range group.Models {
			if meta.ID == "" {
				meta.ID = id
			}
			byKey[group.ID+"/"+id] = catalogCandidate{provider: group.ID, id: id, meta: meta, runtime: false}
		}
	}
	for _, group := range metadata.All {
		for id, meta := range group.Models {
			if meta.ID == "" {
				meta.ID = id
			}
			byKey[group.ID+"/"+id] = catalogCandidate{provider: group.ID, id: id, meta: meta, runtime: true}
		}
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	candidates := make([]catalogCandidate, 0, len(keys))
	for _, key := range keys {
		candidates = append(candidates, byKey[key])
	}
	// 索引只依赖候选集合，整轮 enrich 复用一次，避免每个模型重建。
	index := newCatalogIndex(candidates)
	enrich := func(provider string, items []model.DeviceAIModelInfo) {
		for i := range items {
			item := &items[i]
			candidate := index.match(provider, *item)
			if candidate == nil {
				continue
			}
			meta := candidate.meta
			item.PresetSource, item.PresetModel = "opencode", candidate.provider+"/"+candidate.id
			item.PresetContext, item.PresetOutput = meta.Limit.Context, meta.Limit.Output
			item.Context = firstPositiveInt64(item.UpstreamContext, item.ManualContext, item.PresetContext, item.Context)
			item.Output = firstPositiveInt64(item.UpstreamOutput, item.ManualOutput, item.PresetOutput, item.Output)
			if item.Modalities == nil || automaticModalitiesSource(item.ModalitiesSource) {
				if modalities := runtimeModelModalities(meta); modalities != nil {
					item.Modalities = modalities
					item.ModalitiesSource = "opencode"
				}
			}
			if item.Thinking != nil && (item.Thinking.OverrideEnabled || item.Thinking.Source == "provider") {
				continue
			}
			variants := cloneMap(meta.Variants)
			if len(variants) == 0 {
				variants = catalogReasoningVariants(meta.ID, meta.Capabilities.Reasoning)
			}
			// Native OpenAI/Grok reasoning levels are valid on compatible relays too.
			// Keep vendor-specific Anthropic/Google option objects intact, rather than
			// inventing unsupported low/medium/high tiers for a toggle-only model.
			thinking := thinkingFromVariants("opencode", variants)
			thinking.Supported = meta.Capabilities.Reasoning || len(variants) > 0
			item.Thinking, item.Variants = thinking, variants
		}
	}
	for i := range info.Providers {
		enrich(info.Providers[i].ID, info.Providers[i].Models)
	}
	enrich(info.Provider, info.Models)
	return info
}

func automaticModalitiesSource(source string) bool {
	switch source {
	case "inferred", "runtime", "opencode":
		return true
	}
	return false
}

// Fallback for the raw models.dev cache; live runtime variants take priority.
// Mirrors OpenCode's GPT-5 version/chat/pro/codex effort subsets and Grok's
// two effort levels. Toggle-only families deliberately retain empty variants.
func catalogReasoningVariants(modelID string, reasoning bool) map[string]any {
	id := strings.ToLower(firstNonEmpty(lastPathSegment(modelID), modelID))
	var efforts []string
	switch {
	case strings.HasPrefix(id, "grok") && !strings.Contains(id, "imagine"):
		efforts = []string{"low", "high"}
	case reasoning && (catalogGPT5Family.MatchString(id) || strings.HasPrefix(id, "o1") || strings.HasPrefix(id, "o3") || strings.HasPrefix(id, "o4")):
		efforts = []string{"low", "medium", "high"}
		if catalogGPT5Family.MatchString(id) {
			version := 0
			if match := catalogGPT5Version.FindStringSubmatch(id); len(match) > 1 {
				version, _ = strconv.Atoi(match[1])
			}
			switch {
			case strings.Contains(id, "-chat"):
				if version == 0 {
					return nil
				}
				efforts = []string{"medium"}
			case strings.HasPrefix(id, "gpt-5-pro"):
				efforts = []string{"high"}
			case strings.Contains(id, "-pro"):
				efforts = []string{"medium", "high", "xhigh"}
			case strings.Contains(id, "codex"):
				if version >= 3 {
					efforts = append([]string{"none"}, efforts...)
				}
				if version >= 2 || strings.Contains(id, "codex-max") {
					efforts = append(efforts, "xhigh")
				}
			case version >= 2:
				efforts = []string{"none", "low", "medium", "high", "xhigh"}
			case version == 1:
				efforts = []string{"none", "low", "medium", "high"}
			default:
				efforts = append([]string{"minimal"}, efforts...)
			}
		}
	}
	variants := map[string]any{}
	if reasoning && strings.HasPrefix(id, "claude") {
		adaptive := strings.Contains(id, "opus-4-7") || strings.Contains(id, "opus-4.7") || strings.Contains(id, "opus-4-6") || strings.Contains(id, "opus-4.6") || strings.Contains(id, "sonnet-4-6") || strings.Contains(id, "sonnet-4.6")
		if adaptive {
			levels := []string{"low", "medium", "high", "max"}
			if strings.Contains(id, "opus-4-7") || strings.Contains(id, "opus-4.7") {
				levels = []string{"low", "medium", "high", "xhigh", "max"}
			}
			for _, level := range levels {
				variants[level] = map[string]any{"thinking": map[string]any{"type": "adaptive"}, "effort": level}
			}
		} else {
			variants["high"] = map[string]any{"thinking": map[string]any{"type": "enabled", "budgetTokens": 16000}}
			variants["max"] = map[string]any{"thinking": map[string]any{"type": "enabled", "budgetTokens": 31999}}
		}
		return variants
	}
	if reasoning && strings.HasPrefix(id, "gemini") {
		if strings.Contains(id, "2.5") {
			variants["high"] = map[string]any{"thinkingConfig": map[string]any{"includeThoughts": true, "thinkingBudget": 16000}}
			variants["max"] = map[string]any{"thinkingConfig": map[string]any{"includeThoughts": true, "thinkingBudget": 24576}}
		} else {
			levels := []string{"low", "high"}
			if strings.Contains(id, "gemini-3") {
				levels = []string{"low", "medium", "high"}
				if strings.Contains(id, "flash") {
					levels = []string{"minimal", "low", "medium", "high"}
				}
			}
			for _, level := range levels {
				variants[level] = map[string]any{"thinkingConfig": map[string]any{"includeThoughts": true, "thinkingLevel": level}}
			}
		}
		return variants
	}
	for _, effort := range efforts {
		variants[effort] = map[string]any{"reasoningEffort": effort}
	}
	return variants
}
