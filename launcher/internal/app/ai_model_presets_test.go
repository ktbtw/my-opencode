package app

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"launcher/internal/config"
	"launcher/internal/model"
)

func TestAIConfigEnrichesRelayRuntimeDefaultsFromUnconnectedCatalog(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("OPENCODE_DISABLE_MODELS_FETCH", "1")
	t.Setenv("OPENCODE_MODELS_PATH", filepath.Join(home, "absent.json"))
	root := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "opencode.json"), []byte(`{"provider":{"relay":{"models":{"gpt-5.4":{"name":"GPT 5.4"}}}},"model":"relay/gpt-5.4"}`), 0600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/provider" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"connected":["relay"],"all":[
		  {"id":"relay","models":{"gpt-5.4":{"id":"gpt-5.4","limit":{"context":4096,"output":1024},"capabilities":{"input":{"text":true},"output":{"text":true}}}}},
		  {"id":"openai","models":{"gpt-5.4":{"id":"gpt-5.4","limit":{"context":1050000,"output":128000},"capabilities":{"reasoning":true,"input":{"text":true,"image":true,"pdf":true},"output":{"text":true}}}}}
		]}`))
	}))
	defer server.Close()
	port, err := strconv.Atoi(strings.TrimPrefix(server.URL, "http://127.0.0.1:"))
	if err != nil {
		t.Fatal(err)
	}
	svc := &service{
		cfg:    config.Config{RuntimeDir: t.TempDir()},
		agents: []model.Agent{{AgentID: "preset-fixture", Status: "running", Port: port, Enabled: true}},
	}
	info, err := svc.AIConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Models) != 1 {
		t.Fatalf("unexpected model list: %+v", info.Models)
	}
	item := info.Models[0]
	if item.PresetModel != "openai/gpt-5.4" || item.Context != 1050000 || item.Output != 128000 {
		t.Fatalf("relay limits hid native presets: %+v", item)
	}
	if item.Modalities == nil || len(item.Modalities.Input) != 3 || item.ModalitiesSource != "opencode" || len(item.Variants) != 5 {
		t.Fatalf("relay defaults hid native capabilities: %+v", item)
	}
}
