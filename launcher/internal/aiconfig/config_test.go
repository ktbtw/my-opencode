package aiconfig

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"launcher/internal/model"
)

func TestPathUsesCanonicalOpencodeGlobalFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	got, err := Path()
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	want := filepath.Join(os.Getenv("HOME"), ".config", "opencode", "opencode.json")
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestDirUsesAbsoluteXDGConfigHome(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(os.Getenv("HOME"), "xdg-config"))

	got, err := Dir()
	if err != nil {
		t.Fatalf("dir: %v", err)
	}
	want := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "opencode")
	if got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestDirIgnoresRelativeXDGConfigHome(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "relative-config")

	got, err := Dir()
	if err != nil {
		t.Fatalf("dir: %v", err)
	}
	want := filepath.Join(os.Getenv("HOME"), ".config", "opencode")
	if got != want {
		t.Fatalf("expected fallback dir %s, got %s", want, got)
	}
}

func TestLoadSupportsJsoncGlobalConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := filepath.Join(os.Getenv("HOME"), ".config", "opencode")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	legacy := filepath.Join(root, "opencode.jsonc")
	content := `{
	  // provider config
	  "provider": {
	    "demo": {
	      "options": {
	        "baseURL": "https://api.example.com/v1",
	        "apiKey": "sk-demo-secret"
	      },
	      "models": {
	        "gpt-4.1": {
	          "name": "gpt-4.1",
	          "modalities": {
	            "input": ["text"],
	            "output": ["text", "image"]
	          }
	        },
	      },
	    },
	  },
	  "model": "demo/gpt-4.1",
	}
`
	if err := os.WriteFile(legacy, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	info, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	path := filepath.Join(root, "opencode.json")
	if info.ConfigPath != path {
		t.Fatalf("expected config path %s, got %s", path, info.ConfigPath)
	}
	if info.Provider != "demo" {
		t.Fatalf("expected provider demo, got %s", info.Provider)
	}
	if info.Model != "gpt-4.1" {
		t.Fatalf("expected bare model id, got %s", info.Model)
	}
	if info.BaseURL != "https://api.example.com/v1" {
		t.Fatalf("expected baseURL to be loaded, got %s", info.BaseURL)
	}
	if info.APIKeyMasked == "" || info.APIKeyMasked == "sk-demo-secret" {
		t.Fatalf("expected masked api key, got %s", info.APIKeyMasked)
	}
	if len(info.Models) != 1 || info.Models[0].Modalities == nil {
		t.Fatalf("expected model modalities, got %+v", info.Models)
	}
	if got := info.Models[0].Modalities.Output; len(got) != 2 || got[1] != "image" {
		t.Fatalf("expected output modalities to include image, got %+v", got)
	}
}

func TestLoadIncludesAllProviders(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := filepath.Join(os.Getenv("HOME"), ".config", "opencode")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(root, "opencode.json")
	content := `{
	  "provider": {
	    "longent": {
	      "options": {
	        "baseURL": "https://longent.tech/v1",
	        "apiKey": "sk-longent",
	        "apiMode": "responses"
	      },
	      "models": {
	        "gpt-5.4": { "name": "gpt-5.4" }
	      }
	    },
	    "longent-grok": {
	      "options": {
	        "baseURL": "https://longent.tech/v1",
	        "apiKey": "sk-grok"
	      },
	      "models": {
	        "grok-4": { "name": "grok-4" }
	      }
	    }
	  },
	  "model": "longent/gpt-5.4"
	}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	info, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(info.Providers) != 2 {
		t.Fatalf("expected 2 providers, got %+v", info.Providers)
	}
	if info.Providers[0].ID != "longent" || len(info.Providers[0].Models) != 1 || info.Providers[0].Models[0].ID != "gpt-5.4" {
		t.Fatalf("expected longent provider models, got %+v", info.Providers[0])
	}
	if info.Providers[1].ID != "longent-grok" || len(info.Providers[1].Models) != 1 || info.Providers[1].Models[0].ID != "grok-4" {
		t.Fatalf("expected longent-grok provider models, got %+v", info.Providers[1])
	}
	if info.Providers[0].APIKeyMasked == "" || info.Providers[1].APIKeyMasked == "" {
		t.Fatalf("expected provider api keys to be masked, got %+v", info.Providers)
	}
	if info.Providers[0].APIMode != "responses" {
		t.Fatalf("expected longent api mode responses, got %+v", info.Providers[0])
	}
	if info.Providers[1].APIMode != "chat" {
		t.Fatalf("expected missing non-gpt api mode to default to chat, got %+v", info.Providers[1])
	}
}

func TestLoadReadsProviderAPIWhenOptionsBaseURLIsMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := filepath.Join(os.Getenv("HOME"), ".config", "opencode")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := `{
  "provider": {
    "openrouter": {
      "api": "https://openrouter.ai/api/v1",
      "options": {"apiKey": "sk-test"},
      "models": {"stealth/ox-alpha": {"name": "stealth/ox-alpha"}}
    }
  },
  "model": "openrouter/stealth/ox-alpha"
}`
	if err := os.WriteFile(filepath.Join(root, "opencode.json"), []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	info, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if info.BaseURL != "https://openrouter.ai/api/v1" {
		t.Fatalf("expected provider api URL, got %q", info.BaseURL)
	}
	if len(info.Providers) != 1 || info.Providers[0].BaseURL != info.BaseURL {
		t.Fatalf("expected provider URL to be exposed, got %+v", info.Providers)
	}
}

func TestSaveWritesIntoOpencodeGlobalConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	info, err := Save(model.DeviceAIConfigInfo{
		Provider:     "demo",
		BaseURL:      "https://api.example.com/v1",
		APIKeyMasked: "sk-demo-secret",
		APIMode:      "responses",
		Model:        "gpt-4.1",
		Models: []model.DeviceAIModelInfo{{
			ID:      "gpt-4.1",
			Name:    "GPT-4.1",
			Context: 128000,
			Modalities: &model.DeviceAIModalities{
				Input:  []string{"text"},
				Output: []string{"text", "image"},
			},
		}},
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	path, err := Path()
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	want := filepath.Join(os.Getenv("HOME"), ".config", "opencode", "opencode.json")
	if path != want {
		t.Fatalf("expected target path %s, got %s", want, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved config: %v", err)
	}
	text := string(data)
	for _, expected := range []string{
		`"$schema": "https://opencode.ai/config.json"`,
		`"name": "demo"`,
		`"npm": "@ai-sdk/openai-compatible"`,
		`"baseURL": "https://api.example.com/v1"`,
		`"apiKey": "sk-demo-secret"`,
		`"apiMode": "responses"`,
		`"model": "demo/gpt-4.1"`,
		`"modalities": {`,
		`"output": [`,
		`"image"`,
		`"limit": {`,
		`"context": 128000`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("expected saved config to contain %s, got %s", expected, text)
		}
	}
	if strings.Contains(text, `"enabled_providers"`) {
		t.Fatalf("expected saved config not to contain provider whitelist, got %s", text)
	}
	if info.ConfigPath != path {
		t.Fatalf("expected returned config path %s, got %s", path, info.ConfigPath)
	}
	if info.Model != "gpt-4.1" {
		t.Fatalf("expected returned bare model id, got %s", info.Model)
	}
	if info.APIMode != "responses" {
		t.Fatalf("expected returned api mode responses, got %s", info.APIMode)
	}
}

func TestSaveAndLoadProviderConsoleURL(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	info, err := Save(model.DeviceAIConfigInfo{
		Provider:   "demo",
		BaseURL:    "https://api.example.com/v1",
		ConsoleURL: "https://console.example.com/account",
		Model:      "gpt-4.1",
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if info.ConsoleURL != "https://console.example.com/account" {
		t.Fatalf("expected selected console url, got %q", info.ConsoleURL)
	}
	if len(info.Providers) != 1 || info.Providers[0].ConsoleURL != info.ConsoleURL {
		t.Fatalf("expected provider console url, got %+v", info.Providers)
	}
	if !strings.Contains(info.RawJSON, `"x-operit-console-url": "https://console.example.com/account"`) {
		t.Fatalf("expected console metadata in config, got %s", info.RawJSON)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded.ConsoleURL != info.ConsoleURL {
		t.Fatalf("expected persisted console url, got %q", loaded.ConsoleURL)
	}
}

func TestSaveUsesXDGConfigHomeTarget(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(os.Getenv("HOME"), "xdg-config"))

	info, err := Save(model.DeviceAIConfigInfo{Provider: "demo"})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	want := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "opencode", "opencode.json")
	if info.ConfigPath != want {
		t.Fatalf("expected returned path %s, got %s", want, info.ConfigPath)
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("expected config file at %s: %v", want, err)
	}
}

func TestLoadWithModelsBuildsPreviewFromEmptyConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-demo-secret" {
			t.Fatalf("expected Authorization header, got %s", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-opus-4-6","owned_by":"demo","modalities":{"input":["text"],"output":["text"]},"limit":{"context":200000},"variants":{"low":{},"medium":{},"high":{}}}]}`))
	}))
	defer server.Close()

	info, err := LoadWithModels(model.DeviceAIConfigInput{
		Provider: "demo",
		BaseURL:  server.URL + "/v1",
		APIKey:   "sk-demo-secret",
		Model:    "claude-opus-4-6",
	})
	if err != nil {
		t.Fatalf("load with models: %v", err)
	}
	for _, expected := range []string{
		`"$schema": "https://opencode.ai/config.json"`,
		`"name": "demo"`,
		`"npm": "@ai-sdk/openai-compatible"`,
		`"baseURL": "` + server.URL + `/v1"`,
		`"apiKey": "sk-demo-secret"`,
		`"model": "demo/claude-opus-4-6"`,
		`"claude-opus-4-6": {`,
		`"context": 200000`,
		`"variants": {`,
		`"high": {}`,
	} {
		if !strings.Contains(info.RawJSON, expected) {
			t.Fatalf("expected generated preview to contain %s, got %s", expected, info.RawJSON)
		}
	}
	if strings.Contains(info.RawJSON, `"enabled_providers"`) {
		t.Fatalf("expected generated preview not to contain provider whitelist, got %s", info.RawJSON)
	}
	if info.Provider != "demo" {
		t.Fatalf("expected provider demo, got %s", info.Provider)
	}
	if info.Model != "claude-opus-4-6" {
		t.Fatalf("expected bare model id, got %s", info.Model)
	}
	if info.APIKeyMasked == "" || info.APIKeyMasked == "sk-demo-secret" {
		t.Fatalf("expected masked api key, got %s", info.APIKeyMasked)
	}
	if len(info.Models) != 1 || info.Models[0].ID != "claude-opus-4-6" {
		t.Fatalf("expected preview models, got %+v", info.Models)
	}
	if info.Models[0].Modalities == nil || len(info.Models[0].Modalities.Output) != 1 || info.Models[0].Modalities.Output[0] != "text" {
		t.Fatalf("expected preview modalities, got %+v", info.Models[0].Modalities)
	}
	if info.Models[0].Context != 200000 {
		t.Fatalf("expected preview context limit, got %+v", info.Models[0])
	}
	if len(info.Models[0].Variants) != 3 {
		t.Fatalf("expected preview variants, got %+v", info.Models[0].Variants)
	}
}

func TestLoadWithModelsDetectsOpenRouterReasoningParameters(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"stealth/ox-alpha","owned_by":"openrouter","supported_parameters":["include_reasoning","reasoning","reasoning_effort"]}]}`))
	}))
	defer server.Close()

	info, err := LoadWithModels(model.DeviceAIConfigInput{
		Provider: "openrouter",
		BaseURL:  server.URL,
		APIKey:   "sk-test",
		Model:    "stealth/ox-alpha",
	})
	if err != nil {
		t.Fatalf("load with models: %v", err)
	}
	if len(info.Models) != 1 || info.Models[0].Thinking == nil {
		t.Fatalf("expected thinking metadata, got %+v", info.Models)
	}
	thinking := info.Models[0].Thinking
	if !thinking.Supported || thinking.Source != "provider" || thinking.Protocol != "openrouter" {
		t.Fatalf("unexpected thinking metadata: %+v", thinking)
	}
	if len(thinking.Variants) != 2 || len(info.Models[0].Variants) != 2 {
		t.Fatalf("expected conservative low/high variants, got %+v", thinking.Variants)
	}
	for _, expected := range []string{`"reasoning": true`, `"variants_mode": "replace"`, `"x-operit-thinking":`, `"supported_parameters":`} {
		if !strings.Contains(info.RawJSON, expected) {
			t.Fatalf("expected preview to contain %s, got %s", expected, info.RawJSON)
		}
	}
}

func TestRefreshModelsPreservesManualThinkingOverride(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := filepath.Join(os.Getenv("HOME"), ".config", "opencode")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"stealth/ox-alpha","supported_parameters":["reasoning","reasoning_effort"]}]}`))
	}))
	defer server.Close()
	config := `{
	  "provider": {
	    "openrouter": {
	      "options": {"baseURL": "` + server.URL + `", "apiKey": "sk-test"},
	      "models": {
	        "stealth/ox-alpha": {
	          "reasoning": true,
	          "variants_mode": "replace",
	          "variants": {"focused": {"reasoning": {"effort": "high"}}},
	          "x-operit-thinking": {"supported": true, "source": "manual", "control": "effort", "protocol": "openrouter", "override_enabled": true}
	        }
	      }
	    }
	  },
	  "model": "openrouter/stealth/ox-alpha"
	}`
	if err := os.WriteFile(filepath.Join(root, "opencode.json"), []byte(config), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	info, err := LoadWithModels(model.DeviceAIConfigInput{Provider: "openrouter"})
	if err != nil {
		t.Fatalf("refresh models: %v", err)
	}
	thinking := info.Models[0].Thinking
	if thinking == nil || !thinking.OverrideEnabled || thinking.Source != "manual" {
		t.Fatalf("expected manual override to remain active, got %+v", thinking)
	}
	if _, ok := thinking.Variants["focused"]; !ok || len(thinking.Variants) != 1 {
		t.Fatalf("expected focused override to remain unchanged, got %+v", thinking.Variants)
	}
}

func TestSaveManualThinkingDisableReplacesGeneratedVariants(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	info, err := Save(model.DeviceAIConfigInfo{
		Provider: "openrouter",
		Model:    "grok-4.6",
		Models: []model.DeviceAIModelInfo{{
			ID:   "grok-4.6",
			Name: "Grok 4.6",
			Thinking: &model.DeviceAIThinkingInfo{
				Supported:       false,
				Source:          "manual",
				Control:         "effort",
				Protocol:        "openrouter",
				OverrideEnabled: true,
			},
		}},
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if !strings.Contains(info.RawJSON, `"variants_mode": "replace"`) || !strings.Contains(info.RawJSON, `"disabled": true`) {
		t.Fatalf("expected disabled replace marker, got %s", info.RawJSON)
	}
	if info.Models[0].Thinking == nil || !info.Models[0].Thinking.OverrideEnabled || info.Models[0].Thinking.Supported {
		t.Fatalf("expected disabled manual override metadata, got %+v", info.Models[0].Thinking)
	}
	if len(info.Models[0].Variants) != 0 {
		t.Fatalf("disabled marker must not be exposed as a selectable variant: %+v", info.Models[0].Variants)
	}
}

func TestLoadWithModelsInfersImageModalitiesFromProviderRule(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"grok-imagine-1.0","owned_by":"x-ai"}]}`))
	}))
	defer server.Close()

	info, err := LoadWithModels(model.DeviceAIConfigInput{
		Provider: "x-ai",
		BaseURL:  server.URL,
		APIKey:   "sk-demo-secret",
		Model:    "grok-imagine-1.0",
	})
	if err != nil {
		t.Fatalf("load with models: %v", err)
	}
	if len(info.Models) != 1 || info.Models[0].Modalities == nil {
		t.Fatalf("expected inferred modalities, got %+v", info.Models)
	}
	if got := info.Models[0].Modalities.Output; len(got) != 1 || got[0] != "image" {
		t.Fatalf("expected inferred image output, got %+v", got)
	}
	if !strings.Contains(info.RawJSON, `"output": [`) || !strings.Contains(info.RawJSON, `"image"`) {
		t.Fatalf("expected saved preview to include inferred modalities, got %s", info.RawJSON)
	}
	if !strings.Contains(info.RawJSON, `"apiMode": "chat"`) {
		t.Fatalf("expected non-gpt model preview to use chat api mode, got %s", info.RawJSON)
	}
}

func TestLoadWithModelsUsesSavedAPIKeyWhenInputBlank(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := filepath.Join(os.Getenv("HOME"), ".config", "opencode")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(root, "opencode.json")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer sk-saved-secret" {
			t.Fatalf("expected saved api key to be reused, got %s", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-5.4-mini","owned_by":"demo"}]}`))
	}))
	defer server.Close()
	content := `{
	  "provider": {
	    "demo": {
	      "options": {
	        "baseURL": "` + server.URL + `/v1",
	        "apiKey": "sk-saved-secret"
	      }
	    }
	  }
	}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	info, err := LoadWithModels(model.DeviceAIConfigInput{
		Provider: "demo",
		BaseURL:  server.URL + "/v1",
	})
	if err != nil {
		t.Fatalf("load with models: %v", err)
	}
	if len(info.Models) != 1 || info.Models[0].ID != "gpt-5.4-mini" {
		t.Fatalf("expected fetched models, got %+v", info.Models)
	}
}

func TestSaveRemovesExistingProviderWhitelist(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := filepath.Join(os.Getenv("HOME"), ".config", "opencode")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(root, "opencode.json")
	content := `{
	  "enabled_providers": ["old-only"],
	  "provider": {
	    "old-only": {
	      "options": {
	        "baseURL": "https://old.example.com/v1",
	        "apiKey": "sk-old"
	      },
	      "models": {
	        "gpt-old": { "name": "gpt-old" }
	      }
	    },
	    "demo": {
	      "options": {
	        "baseURL": "https://api.example.com/v1",
	        "apiKey": "sk-demo-secret"
	      },
	      "models": {
	        "gpt-4.1": { "name": "gpt-4.1" }
	      }
	    }
	  },
	  "model": "old-only/gpt-old"
	}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	info, err := Save(model.DeviceAIConfigInfo{
		Provider: "demo",
		BaseURL:  "https://api.example.com/v1",
		Model:    "gpt-4.1",
		Models: []model.DeviceAIModelInfo{{
			ID:   "gpt-4.1",
			Name: "gpt-4.1",
		}},
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if len(info.Providers) != 2 {
		t.Fatalf("expected both providers to remain visible, got %+v", info.Providers)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(data), `"enabled_providers"`) {
		t.Fatalf("expected provider whitelist to be removed, got %s", string(data))
	}
}

func TestSaveForceClearsProviderModels(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := filepath.Join(os.Getenv("HOME"), ".config", "opencode")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(root, "opencode.json")
	content := `{
	  "provider": {
	    "demo": {
	      "options": {
	        "baseURL": "https://api.example.com/v1",
	        "apiKey": "sk-demo-secret"
	      },
	      "models": {
	        "gpt-4.1": {
	          "name": "gpt-4.1"
	        }
	      }
	    }
	  }
	}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	info, err := Save(model.DeviceAIConfigInfo{
		Provider: "demo",
		BaseURL:  "https://api.example.com/v1",
		Force:    true,
		Models:   nil,
	})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if len(info.Providers) != 1 || len(info.Providers[0].Models) != 0 {
		t.Fatalf("expected provider models to be cleared, got %+v", info.Providers)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(data), `"gpt-4.1"`) {
		t.Fatalf("expected cleared config not to contain old model, got %s", string(data))
	}
}

func TestClearProviderPreservesMCPAndOtherConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := filepath.Join(os.Getenv("HOME"), ".config", "opencode")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(root, "opencode.json")
	content := `{
	  "provider": {
	    "demo": {
	      "options": {
	        "baseURL": "https://api.example.com/v1",
	        "apiKey": "sk-demo"
	      },
	      "models": {
	        "gpt-4.1": { "name": "gpt-4.1" }
	      }
	    },
	    "keep": {
	      "options": {
	        "baseURL": "https://keep.example.com/v1"
	      }
	    }
	  },
	  "model": "demo/gpt-4.1",
	  "mcp": {
	    "verify": {
	      "type": "local",
	      "command": ["npx", "-y", "@ktbtw/verify-mcp"]
	    }
	  },
	  "agent": {
	    "build": {
	      "description": "keep me"
	    }
	  }
	}`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	info, err := ClearProvider("demo")
	if err != nil {
		t.Fatalf("clear provider: %v", err)
	}
	if info.Provider != "keep" {
		t.Fatalf("expected remaining provider to become active, got %s", info.Provider)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	text := string(data)
	for _, expected := range []string{
		`"keep": {`,
		`"mcp": {`,
		`"verify": {`,
		`"agent": {`,
		`"description": "keep me"`,
	} {
		if !strings.Contains(text, expected) {
			t.Fatalf("expected config to preserve %s, got %s", expected, text)
		}
	}
	if strings.Contains(text, `"demo":`) || strings.Contains(text, `"model": "demo/gpt-4.1"`) {
		t.Fatalf("expected deleted provider and selected model to be removed, got %s", text)
	}
}

func TestMatchModalitiesUsesKeywordFallback(t *testing.T) {
	got := matchModalities("custom", "my-flux-ultra", "My Flux Ultra", nil)
	if got == nil {
		t.Fatal("expected keyword rule to infer modalities")
	}
	if len(got.Input) != 1 || got.Input[0] != "text" {
		t.Fatalf("expected text input, got %+v", got.Input)
	}
	if len(got.Output) != 1 || got.Output[0] != "image" {
		t.Fatalf("expected image output, got %+v", got.Output)
	}
}

func TestParseModelsOnlyUsesExplicitContextLimit(t *testing.T) {
	models := parseModels(map[string]any{
		"gpt-5.4": map[string]any{"name": "gpt-5.4"},
		"gpt-5.5": map[string]any{
			"name":     "gpt-5.5",
			"limit":    map[string]any{"context": 1050000},
			"variants": map[string]any{"high": map[string]any{}, "xhigh": map[string]any{}},
		},
	}, "cheap")
	if len(models) != 2 {
		t.Fatalf("expected two models, got %+v", models)
	}
	byID := map[string]model.DeviceAIModelInfo{}
	for _, item := range models {
		byID[item.ID] = item
	}
	if byID["gpt-5.5"].Context != 1050000 {
		t.Fatalf("expected explicit gpt-5.5 context, got %+v", byID["gpt-5.5"])
	}
	if len(byID["gpt-5.5"].Variants) != 2 {
		t.Fatalf("expected explicit gpt-5.5 variants, got %+v", byID["gpt-5.5"].Variants)
	}
	if byID["gpt-5.4"].Context != 0 {
		t.Fatalf("expected gpt-5.4 without explicit context to stay empty, got %+v", byID["gpt-5.4"])
	}
	if len(byID["gpt-5.4"].Variants) != 0 {
		t.Fatalf("expected gpt-5.4 without explicit variants to stay empty, got %+v", byID["gpt-5.4"].Variants)
	}
}

func TestMatchModalitiesInfersClaudeTestImageInput(t *testing.T) {
	got := matchModalities("claude-test", "claude-haiku-4-5-20251001", "claude-haiku-4-5-20251001", nil)
	if got == nil {
		t.Fatal("expected claude-test rule to infer modalities")
	}
	if len(got.Input) != 2 || got.Input[0] != "text" || got.Input[1] != "image" {
		t.Fatalf("expected text and image input, got %+v", got.Input)
	}
	if len(got.Output) != 1 || got.Output[0] != "text" {
		t.Fatalf("expected text output, got %+v", got.Output)
	}
}

func TestMatchModalitiesInfersKimiK3VisionAndVideo(t *testing.T) {
	got := matchModalities("custom", "kimi-k3", "kimi-k3", nil)
	if got == nil {
		t.Fatal("expected kimi-k3 rule to infer modalities")
	}
	if len(got.Input) != 3 || got.Input[0] != "text" || got.Input[1] != "image" || got.Input[2] != "video" {
		t.Fatalf("expected text, image and video input, got %+v", got.Input)
	}
	if len(got.Output) != 1 || got.Output[0] != "text" {
		t.Fatalf("expected text output, got %+v", got.Output)
	}
}

func TestMatchModalitiesInfersKimiK25WithoutVideo(t *testing.T) {
	got := matchModalities("custom", "kimi-k2.5", "kimi-k2.5", nil)
	if got == nil {
		t.Fatal("expected kimi-k2.5 rule to infer modalities")
	}
	if len(got.Input) != 2 || got.Input[0] != "text" || got.Input[1] != "image" {
		t.Fatalf("expected text and image input, got %+v", got.Input)
	}
	if len(got.Output) != 1 || got.Output[0] != "text" {
		t.Fatalf("expected text output, got %+v", got.Output)
	}
}

func TestMatchModalitiesInfersKimiK27CodeVisionAndVideo(t *testing.T) {
	got := matchModalities("custom", "kimi-k2.7-code", "kimi-k2.7-code", nil)
	if got == nil {
		t.Fatal("expected kimi-k2.7-code rule to infer modalities")
	}
	if len(got.Input) != 3 || got.Input[0] != "text" || got.Input[1] != "image" || got.Input[2] != "video" {
		t.Fatalf("expected text, image and video input, got %+v", got.Input)
	}
	if len(got.Output) != 1 || got.Output[0] != "text" {
		t.Fatalf("expected text output, got %+v", got.Output)
	}
}

func TestMatchModalitiesKeepsExistingValue(t *testing.T) {
	cur := &model.DeviceAIModalities{Input: []string{"text", "image"}, Output: []string{"image"}}
	got := matchModalities("google", "imagen-4-ultra", "Imagen 4 Ultra", cur)
	if got == nil {
		t.Fatal("expected existing modalities to be kept")
	}
	if len(got.Input) != 2 || got.Input[1] != "image" {
		t.Fatalf("expected existing input to remain unchanged, got %+v", got.Input)
	}
}

func TestEnrichWithRuntimeProviderMetadataUsesExactProviderAndModel(t *testing.T) {
	info := &model.DeviceAIConfigInfo{
		Provider: "性价比日卡",
		Models: []model.DeviceAIModelInfo{{
			ID:      "claude-opus-4-6",
			Name:    "claude-opus-4-6",
			OwnedBy: "性价比日卡",
		}},
		Providers: []model.DeviceAIProviderInfo{{
			ID: "性价比日卡",
			Models: []model.DeviceAIModelInfo{{
				ID:   "claude-opus-4-6",
				Name: "claude-opus-4-6",
			}},
		}},
	}
	got := EnrichWithRuntimeProviderMetadata(info, RuntimeProviderMetadata{
		Connected: []string{"性价比日卡"},
		All: []RuntimeProviderMetadataItem{{
			ID: "性价比日卡",
			Models: map[string]RuntimeModelMetadata{
				"claude-opus-4-6": {
					ID:         "claude-opus-4-6",
					ProviderID: "性价比日卡",
					Name:       "claude-opus-4-6",
					Limit:      RuntimeModelLimit{Context: 200000},
					Capabilities: RuntimeModelCapabilities{
						Input:  map[string]bool{"text": true, "image": true},
						Output: map[string]bool{"text": true},
					},
					Variants: map[string]any{
						"low":    map[string]any{"reasoningEffort": "low"},
						"medium": map[string]any{"reasoningEffort": "medium"},
						"high":   map[string]any{"reasoningEffort": "high"},
					},
				},
			},
		}},
	})
	if got == nil {
		t.Fatal("expected enriched config")
	}
	if len(got.Models) != 1 {
		t.Fatalf("expected one model, got %+v", got.Models)
	}
	if got.Models[0].Context != 200000 {
		t.Fatalf("expected runtime context limit, got %+v", got.Models[0])
	}
	if len(got.Models[0].Variants) != 3 {
		t.Fatalf("expected runtime variants, got %+v", got.Models[0].Variants)
	}
	if got.Models[0].Modalities == nil || len(got.Models[0].Modalities.Input) != 2 {
		t.Fatalf("expected runtime modalities, got %+v", got.Models[0].Modalities)
	}
}

func TestEnrichWithRuntimeProviderMetadataKeepsSavedModalities(t *testing.T) {
	info := &model.DeviceAIConfigInfo{
		Provider: "demo",
		Models: []model.DeviceAIModelInfo{{
			ID: "vision-model",
			Modalities: &model.DeviceAIModalities{
				Input:  []string{"text", "image", "pdf"},
				Output: []string{"text"},
			},
		}},
		Providers: []model.DeviceAIProviderInfo{{
			ID: "demo",
			Models: []model.DeviceAIModelInfo{{
				ID: "vision-model",
				Modalities: &model.DeviceAIModalities{
					Input:  []string{"text", "image", "pdf"},
					Output: []string{"text"},
				},
			}},
		}},
	}

	got := EnrichWithRuntimeProviderMetadata(info, RuntimeProviderMetadata{
		Connected: []string{"demo"},
		All: []RuntimeProviderMetadataItem{{
			ID: "demo",
			Models: map[string]RuntimeModelMetadata{
				"vision-model": {
					Capabilities: RuntimeModelCapabilities{
						Input:  map[string]bool{"text": true},
						Output: map[string]bool{"text": true},
					},
				},
			},
		}},
	})

	input := got.Models[0].Modalities.Input
	if len(input) != 3 || input[1] != "image" || input[2] != "pdf" {
		t.Fatalf("expected saved modalities to remain unchanged, got %+v", input)
	}
}

func TestEnrichWithRuntimeProviderMetadataKeepsManualThinkingOverride(t *testing.T) {
	info := &model.DeviceAIConfigInfo{
		Provider: "openrouter",
		Models: []model.DeviceAIModelInfo{{
			ID: "stealth/ox-alpha",
			Thinking: &model.DeviceAIThinkingInfo{
				Supported:       true,
				Source:          "manual",
				OverrideEnabled: true,
				Variants: map[string]any{
					"focused": map[string]any{"reasoning": map[string]any{"effort": "high"}},
				},
			},
			Variants: map[string]any{
				"focused": map[string]any{"reasoning": map[string]any{"effort": "high"}},
			},
		}},
		Providers: []model.DeviceAIProviderInfo{{
			ID: "openrouter",
			Models: []model.DeviceAIModelInfo{{
				ID: "stealth/ox-alpha",
				Thinking: &model.DeviceAIThinkingInfo{
					Supported:       true,
					Source:          "manual",
					OverrideEnabled: true,
					Variants: map[string]any{
						"focused": map[string]any{"reasoning": map[string]any{"effort": "high"}},
					},
				},
				Variants: map[string]any{
					"focused": map[string]any{"reasoning": map[string]any{"effort": "high"}},
				},
			}},
		}},
	}

	got := EnrichWithRuntimeProviderMetadata(info, RuntimeProviderMetadata{
		Connected: []string{"openrouter"},
		All: []RuntimeProviderMetadataItem{{
			ID: "openrouter",
			Models: map[string]RuntimeModelMetadata{
				"stealth/ox-alpha": {
					Capabilities: RuntimeModelCapabilities{Reasoning: true},
					Variants: map[string]any{
						"low":  map[string]any{"reasoning": map[string]any{"effort": "low"}},
						"high": map[string]any{"reasoning": map[string]any{"effort": "high"}},
					},
				},
			},
		}},
	})

	if got.Models[0].Thinking == nil || !got.Models[0].Thinking.OverrideEnabled {
		t.Fatalf("expected manual override to remain active, got %+v", got.Models[0].Thinking)
	}
	if len(got.Models[0].Variants) != 1 || got.Models[0].Variants["focused"] == nil {
		t.Fatalf("expected manual variants to remain unchanged, got %+v", got.Models[0].Variants)
	}
}

func TestEnrichWithRuntimeProviderMetadataAddsRuntimeModels(t *testing.T) {
	info := &model.DeviceAIConfigInfo{
		Provider: "runtime-provider",
		Providers: []model.DeviceAIProviderInfo{{
			ID: "runtime-provider",
		}},
	}
	got := EnrichWithRuntimeProviderMetadata(info, RuntimeProviderMetadata{
		Connected: []string{"runtime-provider"},
		All: []RuntimeProviderMetadataItem{{
			ID: "runtime-provider",
			Models: map[string]RuntimeModelMetadata{
				"grok-4.6": {
					Name:  "Grok 4.6",
					Limit: RuntimeModelLimit{Context: 256000},
					Variants: map[string]any{
						"low":  map[string]any{},
						"high": map[string]any{},
					},
				},
			},
		}},
	})
	if len(got.Providers) != 1 || len(got.Providers[0].Models) != 1 {
		t.Fatalf("expected runtime model to be added, got %+v", got.Providers)
	}
	added := got.Providers[0].Models[0]
	if added.ID != "grok-4.6" || added.Name != "Grok 4.6" || added.OwnedBy != "runtime-provider" {
		t.Fatalf("unexpected runtime model: %+v", added)
	}
	if added.Context != 256000 || len(added.Variants) != 2 {
		t.Fatalf("expected runtime metadata to be retained, got %+v", added)
	}
	if len(got.Models) != 1 || got.Models[0].ID != "grok-4.6" {
		t.Fatalf("expected selected provider models to be updated, got %+v", got.Models)
	}
}

func TestEnrichWithRuntimeProviderMetadataRejectsWrongProvider(t *testing.T) {
	info := &model.DeviceAIConfigInfo{
		Provider: "性价比日卡",
		Models: []model.DeviceAIModelInfo{{
			ID:      "claude-opus-4-6",
			Name:    "claude-opus-4-6",
			OwnedBy: "性价比日卡",
		}},
	}
	got := EnrichWithRuntimeProviderMetadata(info, RuntimeProviderMetadata{
		Connected: []string{"other"},
		All: []RuntimeProviderMetadataItem{{
			ID: "other",
			Models: map[string]RuntimeModelMetadata{
				"claude-opus-4-6": {
					Limit:    RuntimeModelLimit{Context: 900000},
					Variants: map[string]any{"low": map[string]any{}},
				},
			},
		}},
	})
	if got.Models[0].Context != 0 {
		t.Fatalf("expected wrong provider not to merge, got %+v", got.Models[0])
	}
	if len(got.Models[0].Variants) != 0 {
		t.Fatalf("expected wrong provider not to merge variants, got %+v", got.Models[0].Variants)
	}
}
