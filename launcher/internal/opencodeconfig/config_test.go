package opencodeconfig

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestPathUsesCanonicalOpencodeJSON(t *testing.T) {
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

func TestReadTextRestrictsExistingConfigPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not expose Unix permission bits")
	}
	t.Setenv("HOME", t.TempDir())
	path, err := Path()
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"mcp":{"verify":{"environment":{"VERIFY_API_TOKEN":"secret"}}}}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	if _, _, err := ReadText(); err != nil {
		t.Fatalf("read config: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected mode 0600, got %o", info.Mode().Perm())
	}
}

func TestReadMapMigratesLegacyGlobalFiles(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := filepath.Join(os.Getenv("HOME"), ".config", "opencode")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "config.json"), []byte(`{"username":"base"}`), 0o644); err != nil {
		t.Fatalf("write config.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "opencode.json"), []byte(`{"model":"demo/gpt-4.1"}`), 0o644); err != nil {
		t.Fatalf("write opencode.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "opencode.jsonc"), []byte("{\n// comment\n\"username\":\"override\",\n\"mcp\":{\"jira\":{\"type\":\"remote\",\"url\":\"https://jira.example.com/mcp\"}}\n}\n"), 0o644); err != nil {
		t.Fatalf("write opencode.jsonc: %v", err)
	}

	path, _, parsed, err := ReadMap()
	if err != nil {
		t.Fatalf("read map: %v", err)
	}
	if path != filepath.Join(root, "opencode.json") {
		t.Fatalf("expected canonical path, got %s", path)
	}
	if parsed["username"] != "override" {
		t.Fatalf("expected override username, got %#v", parsed["username"])
	}
	if parsed["model"] != "demo/gpt-4.1" {
		t.Fatalf("expected model to survive merge, got %#v", parsed["model"])
	}
	if _, err := os.Stat(filepath.Join(root, "config.json")); !os.IsNotExist(err) {
		t.Fatalf("expected config.json to be removed, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "opencode.jsonc")); !os.IsNotExist(err) {
		t.Fatalf("expected opencode.jsonc to be removed, stat err=%v", err)
	}
}

func TestWriteMapKeepsCanonicalFileOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := filepath.Join(os.Getenv("HOME"), ".config", "opencode")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "opencode.jsonc"), []byte(`{"username":"legacy"}`), 0o644); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}

	path, data, err := WriteMap(map[string]any{
		"username": "writer",
	})
	if err != nil {
		t.Fatalf("write map: %v", err)
	}
	if path != filepath.Join(root, "opencode.json") {
		t.Fatalf("expected canonical path, got %s", path)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("decode data: %v", err)
	}
	if parsed["username"] != "writer" {
		t.Fatalf("expected writer username, got %#v", parsed["username"])
	}
	if parsed["$schema"] != schemaURL {
		t.Fatalf("expected schema, got %#v", parsed["$schema"])
	}
	if _, err := os.Stat(filepath.Join(root, "opencode.jsonc")); !os.IsNotExist(err) {
		t.Fatalf("expected legacy file to be removed, stat err=%v", err)
	}
}

func TestRemoveProviderWhitelistDeletesEnabledProviders(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	root := filepath.Join(os.Getenv("HOME"), ".config", "opencode")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(root, "opencode.json")
	if err := os.WriteFile(path, []byte(`{
	  "enabled_providers": ["old-only"],
	  "provider": {
	    "old-only": {"models": {"gpt-old": {"name": "gpt-old"}}},
	    "demo": {"models": {"gpt-4.1": {"name": "gpt-4.1"}}}
	  },
	  "model": "demo/gpt-4.1"
	}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	changed, err := RemoveProviderWhitelist()
	if err != nil {
		t.Fatalf("remove whitelist: %v", err)
	}
	if !changed {
		t.Fatal("expected whitelist removal to report changed")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if _, ok := parsed["enabled_providers"]; ok {
		t.Fatalf("expected enabled_providers to be removed, got %s", string(data))
	}
	providers, ok := parsed["provider"].(map[string]any)
	if !ok || len(providers) != 2 {
		t.Fatalf("expected providers to remain intact, got %#v", parsed["provider"])
	}

	changed, err = RemoveProviderWhitelist()
	if err != nil {
		t.Fatalf("second remove whitelist: %v", err)
	}
	if changed {
		t.Fatal("expected second removal to report unchanged")
	}
}
