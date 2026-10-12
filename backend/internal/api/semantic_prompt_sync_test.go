package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"relay-server/internal/auth"
	"relay-server/internal/broker"
	"relay-server/internal/model"
	"relay-server/internal/store"
)

// legacyCodexPrompt 是线上首次入库时写入的那份旧角色提示词，用于复现"库里停留在旧文案"的缺陷。
const legacyCodexPrompt = "You are Codex, based on GPT-5.5, running as a coding agent in the Codex CLI on a user's computer."

// builtinProfileWithPrompt 取内置 Agent 档案并替换提示词与来源，构造"库里已存在"的历史状态。
func builtinProfileWithPrompt(t *testing.T, id, prompt, origin string) model.SemanticAgentProfile {
	t.Helper()
	for _, item := range builtinSemanticAgentProfiles() {
		if item.ID == id {
			item.Prompt = prompt
			item.PromptOrigin = origin
			item.BuiltIn = true
			return item
		}
	}
	t.Fatalf("missing builtin semantic agent %q", id)
	return model.SemanticAgentProfile{}
}

func storedSemanticAgent(t *testing.T, mem *store.Memory, id string) model.SemanticAgentProfile {
	t.Helper()
	items, err := mem.ListSemanticAgents(true)
	if err != nil {
		t.Fatalf("list semantic agents: %v", err)
	}
	for _, item := range items {
		if item.ID == id {
			return item
		}
	}
	t.Fatalf("semantic agent %q not found", id)
	return model.SemanticAgentProfile{}
}

func TestBuiltinSemanticAgentPromptFollowsCatalogUpdate(t *testing.T) {
	mem := store.NewMemory(nil)
	api := newTestAPI(mem, broker.New(), auth.NewManager(time.Hour), testAPIOptions{})

	if _, err := mem.UpsertSemanticAgent(builtinProfileWithPrompt(t, "reverse-android", legacyCodexPrompt, model.SemanticPromptOriginBuiltin)); err != nil {
		t.Fatalf("seed legacy semantic agent: %v", err)
	}
	if _, err := api.semanticAgentProfiles(true); err != nil {
		t.Fatalf("load semantic agents: %v", err)
	}

	stored := storedSemanticAgent(t, mem, "reverse-android")
	want, ok := builtinSemanticAgentPrompt("reverse-android")
	if !ok {
		t.Fatal("missing builtin prompt for reverse-android")
	}
	if stored.Prompt != want {
		t.Fatalf("内置提示词未跟随目录更新:\n got=%q\nwant=%q", stored.Prompt, want)
	}
	if strings.Contains(stored.Prompt, "Codex") {
		t.Fatalf("内置提示词仍自称 Codex: %q", stored.Prompt)
	}
	if stored.PromptOrigin != model.SemanticPromptOriginBuiltin {
		t.Fatalf("unexpected prompt origin %q", stored.PromptOrigin)
	}
}

func TestBuiltinSemanticAgentPromptKeepsCustomOverride(t *testing.T) {
	mem := store.NewMemory(nil)
	api := newTestAPI(mem, broker.New(), auth.NewManager(time.Hour), testAPIOptions{})

	const customPrompt = "我是运维自定义的执行层提示词。"
	if _, err := mem.UpsertSemanticAgent(builtinProfileWithPrompt(t, "reverse-android", customPrompt, model.SemanticPromptOriginCustom)); err != nil {
		t.Fatalf("seed custom semantic agent: %v", err)
	}
	if _, err := api.semanticAgentProfiles(true); err != nil {
		t.Fatalf("load semantic agents: %v", err)
	}

	stored := storedSemanticAgent(t, mem, "reverse-android")
	if stored.Prompt != customPrompt {
		t.Fatalf("运维自定义提示词被内置更新覆盖: %q", stored.Prompt)
	}
	if stored.PromptOrigin != model.SemanticPromptOriginCustom {
		t.Fatalf("unexpected prompt origin %q", stored.PromptOrigin)
	}
}

func TestBuiltinSemanticAgentPromptKeepsUnknownOriginRow(t *testing.T) {
	mem := store.NewMemory(nil)
	api := newTestAPI(mem, broker.New(), auth.NewManager(time.Hour), testAPIOptions{})

	// 升级前入库的历史行没有来源标记，属于未知来源，内置更新不得覆盖它。
	if _, err := mem.UpsertSemanticAgent(builtinProfileWithPrompt(t, "reverse-android", legacyCodexPrompt, "")); err != nil {
		t.Fatalf("seed legacy semantic agent: %v", err)
	}
	if _, err := api.semanticAgentProfiles(true); err != nil {
		t.Fatalf("load semantic agents: %v", err)
	}

	stored := storedSemanticAgent(t, mem, "reverse-android")
	if stored.Prompt != legacyCodexPrompt {
		t.Fatalf("来源未知的历史提示词被覆盖: %q", stored.Prompt)
	}
}

func TestUpsertSemanticAgentMarksPromptOrigin(t *testing.T) {
	mem := store.NewMemory(nil)
	api := newTestAPI(mem, broker.New(), auth.NewManager(time.Hour), testAPIOptions{})

	profile := builtinProfileWithPrompt(t, "reverse-android", "", model.SemanticPromptOriginBuiltin)
	profile.Prompt = "控制台里改成自定义文案。"
	updated, err := api.upsertSemanticAgent(profile)
	if err != nil {
		t.Fatalf("upsert custom prompt: %v", err)
	}
	if updated.PromptOrigin != model.SemanticPromptOriginCustom {
		t.Fatalf("自定义提示词应标记 custom, got %q", updated.PromptOrigin)
	}

	builtinPrompt, ok := builtinSemanticAgentPrompt("reverse-android")
	if !ok {
		t.Fatal("missing builtin prompt for reverse-android")
	}
	profile.Prompt = builtinPrompt
	updated, err = api.upsertSemanticAgent(profile)
	if err != nil {
		t.Fatalf("upsert builtin prompt: %v", err)
	}
	if updated.PromptOrigin != model.SemanticPromptOriginBuiltin {
		t.Fatalf("改回内置文案应标记 builtin, got %q", updated.PromptOrigin)
	}
}

func TestRestartDeviceAgentPushesLatestSemanticPrompt(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()

	var saved *model.DeviceAgentSemanticSelectionPayload
	var restarted bool
	api := newTestAPI(mem, b, authManager, testAPIOptions{
		launcherRequest: func(_ context.Context, _ int64, _, _, envType string, payload any) (model.DeviceLauncherResultPayload, error) {
			switch envType {
			case "device.launcher.semantic_agent_get":
				info, ok := payload.(model.DeviceAgentSemanticSelectionPayload)
				if !ok {
					t.Fatalf("unexpected payload type %#v", payload)
				}
				return model.DeviceLauncherResultPayload{
					Action:  "semantic_agent_get",
					Success: true,
					SemanticSelection: &model.DeviceAgentSemanticSelection{
						AgentID:          info.AgentID,
						SemanticAgentID:  "reverse-android",
						VerifyMCPEnabled: true,
						Profile:          &model.SemanticAgentProfile{ID: "reverse-android", Prompt: legacyCodexPrompt},
					},
				}, nil
			case "device.launcher.semantic_agent_save":
				info, ok := payload.(model.DeviceAgentSemanticSelectionPayload)
				if !ok {
					t.Fatalf("unexpected payload type %#v", payload)
				}
				copied := info
				saved = &copied
				return model.DeviceLauncherResultPayload{Action: "semantic_agent_save", Success: true}, nil
			case "device.launcher.restart_agent":
				restarted = true
				return model.DeviceLauncherResultPayload{Action: "restart_agent", Success: true}, nil
			default:
				t.Fatalf("unexpected env type %s", envType)
				return model.DeviceLauncherResultPayload{}, nil
			}
		},
	})

	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	b.Add(nil, model.HelloPayload{AgentID: "launcher:m_online", MachineID: "m_online", Kind: "launcher"}, operator.ID)

	req := httptest.NewRequest(http.MethodPost, "/api/devices/m_online/launcher/agents/agent_chat_codex/restart", strings.NewReader(`{"agent_id":"agent_chat_codex"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("machineID", "m_online")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
	rr := httptest.NewRecorder()

	api.RestartDeviceAgent(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected restart status: %d body=%s", rr.Code, rr.Body.String())
	}
	if !restarted {
		t.Fatal("expected launcher restart call")
	}
	if saved == nil {
		t.Fatal("重启前未把最新语义 Agent 提示词推给设备")
	}
	want, ok := builtinSemanticAgentPrompt("reverse-android")
	if !ok {
		t.Fatal("missing builtin prompt for reverse-android")
	}
	if saved.SemanticAgent == nil || strings.TrimSpace(saved.SemanticAgent.Prompt) != want {
		t.Fatalf("推送的提示词不是最新版本: %+v", saved.SemanticAgent)
	}
	// 设备端 Verify MCP 处于启用状态，补推提示词不应顺带把它关掉，也不应重放推荐 MCP 配置。
	if saved.DisableVerifyMCP {
		t.Fatalf("补推把设备端 Verify MCP 关掉了: %+v", saved)
	}
	if saved.ApplyRecommendedMCP {
		t.Fatalf("补推不应重放推荐 MCP 配置: %+v", saved)
	}
}

func TestRestartDeviceAgentSkipsSyncWhenPromptIsEmpty(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()

	// 库里角色提示词为空（线上确有这种数据），补推会写出没有 agent 条目的配置，
	// 导致 opencode 启动即报 default agent not found，因此必须跳过。
	if _, err := mem.UpsertSemanticAgent(builtinProfileWithPrompt(t, "coding-assistant", "", model.SemanticPromptOriginCustom)); err != nil {
		t.Fatalf("seed empty prompt semantic agent: %v", err)
	}

	var saved bool
	api := newTestAPI(mem, b, authManager, testAPIOptions{
		launcherRequest: func(_ context.Context, _ int64, _, _, envType string, payload any) (model.DeviceLauncherResultPayload, error) {
			switch envType {
			case "device.launcher.semantic_agent_get":
				info, ok := payload.(model.DeviceAgentSemanticSelectionPayload)
				if !ok {
					t.Fatalf("unexpected payload type %#v", payload)
				}
				return model.DeviceLauncherResultPayload{
					Action:  "semantic_agent_get",
					Success: true,
					SemanticSelection: &model.DeviceAgentSemanticSelection{
						AgentID:          info.AgentID,
						SemanticAgentID:  "coding-assistant",
						VerifyMCPEnabled: true,
						Profile:          &model.SemanticAgentProfile{ID: "coding-assistant", Prompt: "设备端现有提示词"},
					},
				}, nil
			case "device.launcher.semantic_agent_save":
				saved = true
				return model.DeviceLauncherResultPayload{Action: "semantic_agent_save", Success: true}, nil
			case "device.launcher.restart_agent":
				return model.DeviceLauncherResultPayload{Action: "restart_agent", Success: true}, nil
			default:
				t.Fatalf("unexpected env type %s", envType)
				return model.DeviceLauncherResultPayload{}, nil
			}
		},
	})

	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	b.Add(nil, model.HelloPayload{AgentID: "launcher:m_online", MachineID: "m_online", Kind: "launcher"}, operator.ID)

	req := httptest.NewRequest(http.MethodPost, "/api/devices/m_online/launcher/agents/agent_chat_codex/restart", strings.NewReader(`{"agent_id":"agent_chat_codex"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("machineID", "m_online")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
	rr := httptest.NewRecorder()

	api.RestartDeviceAgent(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected restart status: %d body=%s", rr.Code, rr.Body.String())
	}
	if saved {
		t.Fatal("角色提示词为空时不应下发，否则会写出无法启动的 Agent 配置")
	}
}

func TestRestartDeviceAgentSkipsSyncWhenPromptMatches(t *testing.T) {
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()

	want, ok := builtinSemanticAgentPrompt("reverse-android")
	if !ok {
		t.Fatal("missing builtin prompt for reverse-android")
	}
	var saved bool
	api := newTestAPI(mem, b, authManager, testAPIOptions{
		launcherRequest: func(_ context.Context, _ int64, _, _, envType string, payload any) (model.DeviceLauncherResultPayload, error) {
			switch envType {
			case "device.launcher.semantic_agent_get":
				info, ok := payload.(model.DeviceAgentSemanticSelectionPayload)
				if !ok {
					t.Fatalf("unexpected payload type %#v", payload)
				}
				return model.DeviceLauncherResultPayload{
					Action:  "semantic_agent_get",
					Success: true,
					SemanticSelection: &model.DeviceAgentSemanticSelection{
						AgentID:          info.AgentID,
						SemanticAgentID:  "reverse-android",
						VerifyMCPEnabled: true,
						Profile:          &model.SemanticAgentProfile{ID: "reverse-android", Prompt: want},
					},
				}, nil
			case "device.launcher.semantic_agent_save":
				saved = true
				return model.DeviceLauncherResultPayload{Action: "semantic_agent_save", Success: true}, nil
			case "device.launcher.restart_agent":
				return model.DeviceLauncherResultPayload{Action: "restart_agent", Success: true}, nil
			default:
				t.Fatalf("unexpected env type %s", envType)
				return model.DeviceLauncherResultPayload{}, nil
			}
		},
	})

	operator := model.Operator{ID: 1, Username: "tester"}
	token, err := authManager.Issue(operator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	b.Add(nil, model.HelloPayload{AgentID: "launcher:m_online", MachineID: "m_online", Kind: "launcher"}, operator.ID)

	req := httptest.NewRequest(http.MethodPost, "/api/devices/m_online/launcher/agents/agent_chat_codex/restart", strings.NewReader(`{"agent_id":"agent_chat_codex"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add("machineID", "m_online")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
	rr := httptest.NewRecorder()

	api.RestartDeviceAgent(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("unexpected restart status: %d body=%s", rr.Code, rr.Body.String())
	}
	if saved {
		t.Fatal("设备端提示词已是最新时不应重复下发")
	}
}
