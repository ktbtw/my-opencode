package aiconfig

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"launcher/internal/model"
)

func catalogFixture(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("OPENCODE_DISABLE_MODELS_FETCH", "1")
	t.Setenv("OPENCODE_MODELS_PATH", filepath.Join(t.TempDir(), "absent.json"))
}

func catalogMetadata() RuntimeProviderMetadata {
	return RuntimeProviderMetadata{Connected: []string{"relay"}, All: []RuntimeProviderMetadataItem{
		{ID: "relay", Models: map[string]RuntimeModelMetadata{"gpt-5.4": {ID: "gpt-5.4", Name: "gpt-5.4"}}},
		{ID: "openai", Models: map[string]RuntimeModelMetadata{"gpt-5.4": {
			ID: "gpt-5.4", Name: "GPT 5.4", Limit: RuntimeModelLimit{Context: 1000000, Output: 128000},
			Modalities:   &model.DeviceAIModalities{Input: []string{"text", "image"}, Output: []string{"text"}},
			Capabilities: RuntimeModelCapabilities{Reasoning: true},
		}}},
		{ID: "xai", Models: map[string]RuntimeModelMetadata{"grok-4": {
			ID: "grok-4", Name: "Grok 4", Limit: RuntimeModelLimit{Context: 256000, Output: 64000},
			Modalities:   &model.DeviceAIModalities{Input: []string{"text", "image"}, Output: []string{"text"}},
			Capabilities: RuntimeModelCapabilities{Reasoning: true},
		}}},
		{ID: "moonshotai", Models: map[string]RuntimeModelMetadata{"kimi-k3": {
			ID: "kimi-k3", Name: "Kimi K3", Limit: RuntimeModelLimit{Context: 262144, Output: 32768},
			Modalities:   &model.DeviceAIModalities{Input: []string{"text", "image", "video"}, Output: []string{"text"}},
			Capabilities: RuntimeModelCapabilities{Reasoning: true},
		}}},
	}}
}

func TestCatalogPrefillsRelayFromUnconnectedNativeProvider(t *testing.T) {
	catalogFixture(t)
	cfg := &model.DeviceAIConfigInfo{Provider: "relay", Models: []model.DeviceAIModelInfo{{ID: "gpt-5.4", Name: "GPT 5.4", OwnedBy: "relay"}}}
	EnrichWithOpenCodeCatalog(cfg, catalogMetadata())
	item := cfg.Models[0]
	if item.PresetModel != "openai/gpt-5.4" || item.Context != 1000000 || item.Output != 128000 {
		t.Fatalf("missing native presets: %+v", item)
	}
	if item.Thinking == nil || item.Thinking.Protocol != "openai-compatible" || len(item.Variants) != 5 {
		t.Fatalf("missing effort/protocol presets: %+v", item)
	}
	if !reflect.DeepEqual(item.Modalities.Input, []string{"text", "image"}) {
		t.Fatalf("missing modalities: %+v", item)
	}
}

func TestCatalogReplacesRelayRuntimeDefaultsButKeepsSavedModalities(t *testing.T) {
	catalogFixture(t)
	metadata := catalogMetadata()
	metadata.All[0].Models["gpt-5.4"] = RuntimeModelMetadata{
		ID: "gpt-5.4", Limit: RuntimeModelLimit{Context: 4096, Output: 1024},
		Capabilities: RuntimeModelCapabilities{Input: map[string]bool{"text": true}, Output: map[string]bool{"text": true}},
	}
	for _, tc := range []struct {
		name       string
		modalities *model.DeviceAIModalities
		wantInput  []string
	}{
		{"runtime-default", nil, []string{"text", "image"}},
		{"explicit-config", &model.DeviceAIModalities{Input: []string{"text"}, Output: []string{"text"}}, []string{"text"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &model.DeviceAIConfigInfo{Provider: "relay", Providers: []model.DeviceAIProviderInfo{{ID: "relay", Models: []model.DeviceAIModelInfo{{ID: "gpt-5.4", Modalities: tc.modalities}}}}}
			EnrichWithRuntimeProviderMetadata(cfg, metadata)
			EnrichWithOpenCodeCatalog(cfg, metadata)
			item := cfg.Providers[0].Models[0]
			if !reflect.DeepEqual(item.Modalities.Input, tc.wantInput) || item.Context != 1000000 || item.Output != 128000 || len(item.Variants) != 5 {
				t.Fatalf("relay defaults hid native catalogue capabilities: %+v", item)
			}
		})
	}
}

func TestCatalogNormalizesRelayPathsDisplayNamesAndKimi3Alias(t *testing.T) {
	catalogFixture(t)
	for _, id := range []string{"vendor/openai/gpt-5.4", "GPT 5.4", "Kimi3", "Kimi-K3", "grok-4"} {
		cfg := &model.DeviceAIConfigInfo{Provider: "relay", Models: []model.DeviceAIModelInfo{{ID: id, Name: id}}}
		EnrichWithOpenCodeCatalog(cfg, catalogMetadata())
		item := cfg.Models[0]
		if item.ID != id || item.PresetModel == "" || item.Context == 0 || item.Modalities == nil {
			t.Fatalf("alias %q not matched: %+v", id, item)
		}
		if id == "Kimi3" && (len(item.Variants) != 0 || !item.Thinking.Supported || item.Thinking.Control != "toggle") {
			t.Fatalf("Kimi must not acquire unsupported effort levels: %+v", item)
		}
	}
}

func TestCatalogMatchesGenericRelayFamilyNames(t *testing.T) {
	catalogFixture(t)
	metadata := catalogMetadata()
	metadata.All = append(metadata.All,
		RuntimeProviderMetadataItem{ID: "anthropic", Models: map[string]RuntimeModelMetadata{
			"claude-opus-4-7": {ID: "claude-opus-4-7", Name: "Claude Opus 4.7", Limit: RuntimeModelLimit{Context: 200000, Output: 32000}, Capabilities: RuntimeModelCapabilities{Reasoning: true}},
		}},
		RuntimeProviderMetadataItem{ID: "google", Models: map[string]RuntimeModelMetadata{
			"gemini-3-pro": {ID: "gemini-3-pro", Name: "Gemini 3 Pro", Limit: RuntimeModelLimit{Context: 1000000, Output: 64000}, Capabilities: RuntimeModelCapabilities{Reasoning: true}},
		}},
		RuntimeProviderMetadataItem{ID: "deepseek", Models: map[string]RuntimeModelMetadata{
			"deepseek-v3": {ID: "deepseek-v3", Name: "DeepSeek V3", Limit: RuntimeModelLimit{Context: 128000, Output: 8192}},
		}},
	)
	for _, id := range []string{"GPT", "Grok", "AI", "Claude", "Gemini", "DeepSeek"} {
		cfg := &model.DeviceAIConfigInfo{Provider: "relay", Models: []model.DeviceAIModelInfo{{ID: id, Name: id}}}
		EnrichWithOpenCodeCatalog(cfg, metadata)
		item := cfg.Models[0]
		if item.PresetModel == "" || item.Context <= 0 || item.Output <= 0 {
			t.Fatalf("generic family %q was not matched: %+v", id, item)
		}
	}
}

func TestCatalogDoesNotFuzzyMatchDifferentModelVersion(t *testing.T) {
	catalogFixture(t)
	for _, id := range []string{"gpt-50", "gpt-5.4-codex", "unrelated-AI", "kimi-k30"} {
		cfg := &model.DeviceAIConfigInfo{Provider: "relay", Models: []model.DeviceAIModelInfo{{ID: id, Name: id}}}
		EnrichWithOpenCodeCatalog(cfg, catalogMetadata())
		if cfg.Models[0].PresetModel != "" {
			t.Fatalf("unexpected fuzzy match: %+v", cfg.Models[0])
		}
	}
}

func TestCatalogPreservesManualAndProviderValues(t *testing.T) {
	catalogFixture(t)
	manual := &model.DeviceAIThinkingInfo{Supported: false, Source: "manual", OverrideEnabled: true}
	cfg := &model.DeviceAIConfigInfo{Provider: "relay", Models: []model.DeviceAIModelInfo{{
		ID: "gpt-5.4", ManualContext: 12345, ManualOutput: 6789,
		Modalities: &model.DeviceAIModalities{Input: []string{"text"}, Output: []string{"image"}}, Thinking: manual,
	}}}
	EnrichWithOpenCodeCatalog(cfg, catalogMetadata())
	if cfg.Models[0].Context != 12345 || cfg.Models[0].Output != 6789 || cfg.Models[0].Thinking != manual || cfg.Models[0].Modalities.Output[0] != "image" {
		t.Fatalf("manual config replaced: %+v", cfg.Models[0])
	}
	cfg.Models[0].UpstreamContext = 999
	cfg.Models[0].UpstreamOutput = 888
	EnrichWithOpenCodeCatalog(cfg, catalogMetadata())
	if cfg.Models[0].Context != 999 || cfg.Models[0].Output != 888 {
		t.Fatalf("upstream values replaced: %+v", cfg.Models[0])
	}
}

func TestCatalogDefaultsRoundTripWithoutBecomingManual(t *testing.T) {
	catalogFixture(t)
	cfg := &model.DeviceAIConfigInfo{Provider: "relay", Models: []model.DeviceAIModelInfo{{ID: "gpt-5.4"}}}
	EnrichWithOpenCodeCatalog(cfg, catalogMetadata())
	parsed := applyConfigInput(map[string]any{}, "relay", "https://relay.test", "", "fixture-key", "responses", "gpt-5.4", cfg.Models, true, false)
	models := asMap(asMap(asMap(parsed["provider"])["relay"])["models"])
	reloaded := parseModels(models, "relay")
	if len(reloaded) != 1 || reloaded[0].ManualContext != 0 || reloaded[0].ManualOutput != 0 || reloaded[0].PresetContext != 1000000 || reloaded[0].PresetOutput != 128000 {
		t.Fatalf("presets promoted to manual: %+v", reloaded)
	}
	if reloaded[0].Thinking.OverrideEnabled || reloaded[0].PresetModel != "openai/gpt-5.4" {
		t.Fatalf("preset source lost: %+v", reloaded)
	}
	if reloaded[0].ModalitiesSource != "opencode" {
		t.Fatalf("automatic modalities lost their provenance: %+v", reloaded)
	}
	metadata := catalogMetadata()
	meta := metadata.All[1].Models["gpt-5.4"]
	meta.Modalities = &model.DeviceAIModalities{Input: []string{"text", "image", "pdf"}, Output: []string{"text"}}
	metadata.All[1].Models["gpt-5.4"] = meta
	cfg.Models = reloaded
	EnrichWithOpenCodeCatalog(cfg, metadata)
	if !reflect.DeepEqual(cfg.Models[0].Modalities.Input, meta.Modalities.Input) {
		t.Fatalf("saved automatic modalities did not receive catalogue refresh: %+v", cfg.Models[0])
	}
}

func TestCatalogReplacesHeuristicModalitiesAfterRelayModelsFetch(t *testing.T) {
	catalogFixture(t)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-5.4"},{"id":"kimi-k2.5"}]}`))
	}))
	defer server.Close()
	metadata := catalogMetadata()
	metadata.All[2].Models["kimi-k2.5"] = RuntimeModelMetadata{
		ID: "kimi-k2.5", Modalities: &model.DeviceAIModalities{Input: []string{"text", "image", "video"}, Output: []string{"text"}},
	}
	cfg, err := LoadWithModels(model.DeviceAIConfigInput{Provider: "relay", BaseURL: server.URL, APIKey: "fixture-key"})
	if err != nil {
		t.Fatal(err)
	}
	EnrichWithRuntimeProviderMetadata(cfg, metadata)
	EnrichWithOpenCodeCatalog(cfg, metadata)
	for _, item := range cfg.Models {
		wantInput := []string{"text", "image"}
		if item.ID == "kimi-k2.5" {
			wantInput = append(wantInput, "video")
		}
		if item.ModalitiesSource != "opencode" || item.Modalities == nil || !reflect.DeepEqual(item.Modalities.Input, wantInput) {
			t.Fatalf("fetch/parse pipeline hid catalogue capabilities: %+v", item)
		}
	}
}

func TestCatalogRefreshReadsModelsDevSchemaAndKeepsValidSnapshot(t *testing.T) {
	catalogFixture(t)
	t.Setenv("OPENCODE_MODELS_PATH", "")
	t.Setenv("OPENCODE_DISABLE_MODELS_FETCH", "")
	data := `{"openai":{"id":"openai","models":{"gpt-5.4":{"id":"gpt-5.4","name":"GPT 5.4","reasoning":true,"reasoning_options":[{"type":"effort","values":["low","high"]}],"limit":{"context":1000000,"output":128000},"modalities":{"input":["text","image"],"output":["text"]}}}}}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api.json" {
			t.Errorf("wrong path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(data))
	}))
	defer server.Close()
	t.Setenv("OPENCODE_MODELS_URL", server.URL)
	SyncOpenCodeModelCatalog()
	got := loadOpenCodeCatalog(false)
	if len(got) != 1 || got[0].Models["gpt-5.4"].Limit.Output != 128000 || len(got[0].Models["gpt-5.4"].Variants) != 2 {
		t.Fatalf("schema output dropped: %+v", got)
	}
	// Force another request and simulate a corrupt response.
	catalogCache.Lock()
	catalogCache.checked = catalogCache.checked.Add(-10 * 60 * 1e9)
	catalogCache.Unlock()
	data = `{"bad":`
	SyncOpenCodeModelCatalog()
	got = loadOpenCodeCatalog(false)
	if len(got) != 1 {
		t.Fatalf("invalid refresh discarded old snapshot: %+v", got)
	}
	cached := filepath.Join(os.Getenv("HOME"), ".cache", "chat-codex", "model-catalog.json")
	content, err := os.ReadFile(cached)
	if err != nil || !json.Valid(content) {
		t.Fatalf("valid snapshot not persisted: %v", err)
	}
}

func TestCatalogLoadsExplicitOpenCodeSnapshotOffline(t *testing.T) {
	catalogFixture(t)
	path := filepath.Join(t.TempDir(), "models.json")
	if err := os.WriteFile(path, []byte(`{"moonshotai":{"models":{"kimi-k3":{"id":"kimi-k3","name":"Kimi K3","reasoning":true,"limit":{"context":262144,"output":32768},"modalities":{"input":["text","image","video"],"output":["text"]}}}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENCODE_MODELS_PATH", path)
	cfg := &model.DeviceAIConfigInfo{Provider: "relay", Models: []model.DeviceAIModelInfo{{ID: "Kimi3"}}}
	EnrichWithOpenCodeCatalog(cfg, RuntimeProviderMetadata{})
	if cfg.Models[0].PresetModel != "moonshotai/kimi-k3" || cfg.Models[0].Output != 32768 {
		t.Fatalf("offline preset not loaded: %+v", cfg.Models[0])
	}
}

func TestCatalogOfflineRestartKeepsNewestValidSnapshot(t *testing.T) {
	catalogFixture(t)
	t.Setenv("OPENCODE_MODELS_PATH", "")
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	openCodePath := filepath.Join(os.Getenv("XDG_CACHE_HOME"), "opencode", "models.json")
	launcherPath := filepath.Join(os.Getenv("XDG_CACHE_HOME"), "chat-codex", "model-catalog.json")
	for i, path := range []string{openCodePath, launcherPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		data := `{"openai":{"models":{"gpt-5.4":{"limit":{"context":1000000,"output":128000}}}}}`
		if i == 1 {
			data = `{"openai":{"models":{"gpt-5.4":{"limit":{"context":1050000,"output":128000}}}}}`
		}
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		modified := time.Now().Add(time.Duration(i-2) * time.Hour)
		if err := os.Chtimes(path, modified, modified); err != nil {
			t.Fatal(err)
		}
	}
	if got := loadOpenCodeCatalog(false); len(got) != 1 || got[0].Models["gpt-5.4"].Limit.Context != 1050000 {
		t.Fatalf("offline reload reverted to stale OpenCode cache: %+v", got)
	}
	if err := os.WriteFile(openCodePath, []byte(`{"corrupt":`), 0600); err != nil {
		t.Fatal(err)
	}
	catalogCache.Lock()
	catalogCache.filePath = ""
	catalogCache.providers = nil
	catalogCache.Unlock()
	if got := loadOpenCodeCatalog(false); len(got) != 1 || got[0].Models["gpt-5.4"].Limit.Context != 1050000 {
		t.Fatalf("corrupt newer file hid valid persisted catalogue: %+v", got)
	}
}

func TestCatalogReasoningPresetSubsets(t *testing.T) {
	for _, tc := range []struct {
		id       string
		count    int
		protocol string
	}{
		{"gpt-5.1", 4, "openai-compatible"}, {"gpt-5.4", 5, "openai-compatible"},
		{"gpt-5.3-codex", 5, "openai-compatible"}, {"gpt-5.2-chat-latest", 1, "openai-compatible"},
		{"gpt-5-pro", 1, "openai-compatible"}, {"grok-4.6", 2, "openai-compatible"},
		{"kimi-k3", 0, "custom"}, {"claude-opus-4-7", 5, "anthropic"},
		{"claude-sonnet-4-6", 4, "anthropic"}, {"gemini-3-flash", 4, "google"},
	} {
		variants := catalogReasoningVariants(tc.id, true)
		thinking := thinkingFromVariants("opencode", variants)
		if len(variants) != tc.count || thinking.Protocol != tc.protocol {
			t.Fatalf("%s: %+v %+v", tc.id, variants, thinking)
		}
	}
}
