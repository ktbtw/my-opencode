package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"relay-server/internal/auth"
	"relay-server/internal/broker"
	"relay-server/internal/model"
	"relay-server/internal/store"
)

func TestMatchModelPromptPresetRules(t *testing.T) {
	presets := []model.SemanticAgentModelPromptPreset{
		{ID: "disabled", Provider: "deepseek", Prompt: "disabled prompt", Enabled: false},
		{ID: "empty", Provider: "deepseek", Prompt: "   ", Enabled: true},
		{ID: "other-provider", Provider: "openai", Prompt: "openai prompt", Enabled: true},
		{ID: "deepseek-pattern", ModelPattern: "deepseek", Prompt: "ds破甲", Enabled: true},
		{ID: "later", ModelPattern: "deepseek", Prompt: "later prompt", Enabled: true},
	}
	cases := []struct {
		name     string
		modelRef string
		want     string
		wantOn   bool
	}{
		{"pattern only match", "custom/deepseek-reasoner", "ds破甲", true},
		{"provider case insensitive", "DeepSeek/DeepSeek-Chat", "ds破甲", true},
		{"bare model id", "deepseek-chat", "ds破甲", true},
		{"non deepseek", "anthropic/claude-3", "", false},
		{"empty model", "", "", false},
	}
	for _, tc := range cases {
		got, ok := matchModelPromptPreset(presets, tc.modelRef)
		if ok != tc.wantOn || got != tc.want {
			t.Fatalf("%s: got (%q,%v) want (%q,%v)", tc.name, got, ok, tc.want, tc.wantOn)
		}
	}

	providerOnly := []model.SemanticAgentModelPromptPreset{
		{ID: "provider", Provider: "deepseek", Prompt: "provider prompt", Enabled: true},
	}
	if _, ok := matchModelPromptPreset(providerOnly, "openai/deepseek-chat"); ok {
		t.Fatal("provider filter should reject mismatched provider segment")
	}
	if got, ok := matchModelPromptPreset(providerOnly, "deepseek/deepseek-chat"); !ok || got != "provider prompt" {
		t.Fatalf("provider filter should match provider segment, got (%q,%v)", got, ok)
	}

	if _, ok := matchModelPromptPreset(nil, "deepseek/deepseek-chat"); ok {
		t.Fatal("empty presets should not match")
	}
}

func TestSplitPresetModelRef(t *testing.T) {
	cases := []struct {
		ref      string
		provider string
		model    string
	}{
		{"DeepSeek/DeepSeek-Chat", "deepseek", "deepseek-chat"},
		{"deepseek-chat", "", "deepseek-chat"},
		{"", "", ""},
		{" custom/model ", "custom", "model"},
	}
	for _, tc := range cases {
		provider, modelID := splitPresetModelRef(tc.ref)
		if provider != tc.provider || modelID != tc.model {
			t.Fatalf("splitPresetModelRef(%q) = (%q,%q) want (%q,%q)", tc.ref, provider, modelID, tc.provider, tc.model)
		}
	}
}

func TestApplyModelPromptWrapsAndPrepends(t *testing.T) {
	got, ok := applyModelPrompt("【全局提示词】\nrule", "ds破甲")
	if !ok {
		t.Fatal("expected model prompt applied")
	}
	if !strings.HasPrefix(got, modelPromptWrapperStart+"\n") {
		t.Fatalf("wrapper start missing: %q", got)
	}
	if !strings.HasSuffix(got, modelPromptWrapperEnd+"\n\n【全局提示词】\nrule") {
		t.Fatalf("base prompt lost or ordering wrong: %q", got)
	}
	if !strings.Contains(got, "\nds破甲\n") {
		t.Fatalf("prompt body missing: %q", got)
	}

	if got, ok := applyModelPrompt("", "only prompt"); !ok || got != modelPromptWrapperStart+"\nonly prompt\n"+modelPromptWrapperEnd {
		t.Fatalf("empty base should yield wrapped prompt only: (%q,%v)", got, ok)
	}

	base := "【全局提示词】\nrule"
	if got, ok := applyModelPrompt(base, "   "); ok || got != base {
		t.Fatalf("blank prompt should keep base untouched: (%q,%v)", got, ok)
	}
}

func TestModelPromptForTaskHonoursModeAndSettingsFallback(t *testing.T) {
	mem := store.NewMemory(nil)
	api := newTestAPI(mem, nil, nil, testAPIOptions{})

	if _, err := mem.UpsertSemanticAgent(model.SemanticAgentProfile{
		ID:         "reverse-android",
		Name:       "逆向专家-安卓",
		Enabled:    true,
		PromptMode: "both",
		ModelPromptPresets: []model.SemanticAgentModelPromptPreset{{
			ID: "model-prompt-1", Name: "ds破甲", ModelPattern: "deepseek", Prompt: "ds破甲协议", Enabled: true,
		}},
	}); err != nil {
		t.Fatalf("upsert semantic agent: %v", err)
	}

	if got, ok := api.modelPromptForTask(1, "agent-1", "reverse-android", "custom/deepseek-reasoner"); !ok || got != "ds破甲协议" {
		t.Fatalf("expected preset matched by task model, got (%q,%v)", got, ok)
	}
	if _, ok := api.modelPromptForTask(1, "agent-1", "reverse-android", "openai/gpt-4o"); ok {
		t.Fatal("non matching model should not inject prompt")
	}
	if _, ok := api.modelPromptForTask(1, "agent-1", "reverse-android", ""); ok {
		t.Fatal("missing model without fallback should not inject prompt")
	}

	if err := mem.SetDeviceSetting(1, "agent-1", selectedModelSettingKey, "deepseek/deepseek-chat"); err != nil {
		t.Fatalf("set selected model: %v", err)
	}
	if got, ok := api.modelPromptForTask(1, "agent-1", "reverse-android", ""); !ok || got != "ds破甲协议" {
		t.Fatalf("expected fallback to selected_model setting, got (%q,%v)", got, ok)
	}

	profile, ok := api.modelPromptProfile("reverse-android")
	if !ok {
		t.Fatal("expected profile loaded")
	}
	profile.PromptMode = "agent"
	if _, err := mem.UpsertSemanticAgent(profile); err != nil {
		t.Fatalf("update semantic agent: %v", err)
	}
	if _, ok := api.modelPromptForTask(1, "agent-1", "reverse-android", "deepseek/deepseek-chat"); ok {
		t.Fatal("agent-only prompt mode must not inject model prompt")
	}

	profile.PromptMode = "model"
	profile.ModelPromptPresets = nil
	if _, err := mem.UpsertSemanticAgent(profile); err != nil {
		t.Fatalf("update semantic agent: %v", err)
	}
	if _, err := mem.UpsertSemanticAgentModelPromptPreset(model.SemanticAgentModelPromptPreset{
		ID: "global", ModelPattern: "deepseek", Prompt: "全局预设", Enabled: true,
	}); err != nil {
		t.Fatalf("upsert global preset: %v", err)
	}
	if got, ok := api.modelPromptForTask(1, "agent-1", "reverse-android", "deepseek/deepseek-chat"); !ok || got != "全局预设" {
		t.Fatalf("expected global preset applied, got (%q,%v)", got, ok)
	}

	if _, ok := api.modelPromptForTask(1, "agent-1", "unknown-agent", "deepseek/deepseek-chat"); ok {
		t.Fatal("unknown semantic agent should not inject prompt")
	}
	if _, ok := api.modelPromptForTask(1, "agent-1", "", "deepseek/deepseek-chat"); ok {
		t.Fatal("missing semantic agent id should not inject prompt")
	}
}

// 覆盖 hello 上报语义 Agent → broker → 建任务 → 派发内容的完整链路。
func TestCreateTaskInjectsModelPromptPresetFromHelloBinding(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()
	api := New(mem, b, authManager, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)

	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	if err := mem.SetDeviceSetting(operator.ID, userSettingsAgentID, globalPromptSettingKey, "global rule"); err != nil {
		t.Fatalf("set global prompt: %v", err)
	}
	if _, err := mem.UpsertSemanticAgent(model.SemanticAgentProfile{
		ID: "reverse-android", Name: "逆向专家-安卓", Enabled: true, PromptMode: "both",
		ModelPromptPresets: []model.SemanticAgentModelPromptPreset{{
			ID: "model-prompt-1", Name: "ds破甲", ModelPattern: "deepseek", Prompt: "ds破甲协议", Enabled: true,
		}},
	}); err != nil {
		t.Fatalf("upsert semantic agent: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("accept websocket: %v", err)
			return
		}
		b.Add(conn, model.HelloPayload{
			AgentID:         "agent_reverse",
			MachineID:       "m_online",
			Hostname:        "WinBox",
			Version:         "opencode-test",
			SemanticAgentID: "reverse-android",
			Projects:        []model.HelloProject{{ProjectID: "reverse-app", Root: "/tmp/reverse-app"}},
		}, operator.ID)
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	body := `{
		"agent_id":"agent_reverse",
		"project_id":"reverse-app",
		"metadata":{"model":"custom/deepseek-reasoner"},
		"parts":[{"type":"text","text":"decompile"}]
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	api.CreateTask(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("unexpected status: %d body=%s", rr.Code, rr.Body.String())
	}

	var system string
	for attempt := 0; attempt < 5 && system == ""; attempt++ {
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("read dispatched envelope: %v", err)
		}
		var env model.Envelope
		if err := json.Unmarshal(data, &env); err != nil {
			t.Fatalf("decode envelope: %v", err)
		}
		if env.Type != "task.run" {
			continue
		}
		raw, err := json.Marshal(env.Payload)
		if err != nil {
			t.Fatalf("marshal payload: %v", err)
		}
		var payload model.RunPayload
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		system = payload.System
	}

	if system == "" {
		t.Fatal("expected task.run carrying a system prompt")
	}
	if !strings.HasPrefix(system, modelPromptWrapperStart) {
		t.Fatalf("model prompt should be injected first: %q", system)
	}
	if !strings.Contains(system, "ds破甲协议") {
		t.Fatalf("preset prompt missing: %q", system)
	}
	if !strings.Contains(system, "global rule") {
		t.Fatalf("base system prompt lost: %q", system)
	}
}
