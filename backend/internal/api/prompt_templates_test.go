package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"relay-server/internal/auth"
	"relay-server/internal/broker"
	"relay-server/internal/model"
	"relay-server/internal/promptregistry"
	"relay-server/internal/store"
)

func TestPromptTemplatesRevisionStableAndSensitive(t *testing.T) {
	base := map[string]string{"chat.base_prompt": "A", "chat.question_policy": ""}
	first := promptTemplatesRevision(base)
	if first != promptTemplatesRevision(map[string]string{"chat.question_policy": "", "chat.base_prompt": "A"}) {
		t.Fatal("revision 应与会话无关地稳定")
	}
	if first == promptTemplatesRevision(map[string]string{"chat.base_prompt": "B", "chat.question_policy": ""}) {
		t.Fatal("内容变化时 revision 必须变化")
	}
	if len(first) != 16 {
		t.Fatalf("revision 长度应为 16，实际 %d", len(first))
	}
}

// promptTestRig 组装一个带在线 launcher + 一个在线设备 Agent 的管理端测试环境。
type promptTestRig struct {
	api      *API
	store    *store.Memory
	operator model.Operator
	token    string
	pushes   []promptTemplatesPayload
	calls    []string
}

func newPromptTestRig(t *testing.T, agentSemanticID string) *promptTestRig {
	t.Helper()
	mem := store.NewMemory(nil)
	authManager := auth.NewManager(time.Hour)
	b := broker.New()
	rig := &promptTestRig{store: mem, operator: model.Operator{ID: 7, Username: "admin"}}

	rig.api = newTestAPI(mem, b, authManager, testAPIOptions{
		launcherRequest: func(_ context.Context, _ int64, _, _, envType string, payload any) (model.DeviceLauncherResultPayload, error) {
			rig.calls = append(rig.calls, envType)
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
						AgentID:         info.AgentID,
						SemanticAgentID: agentSemanticID,
					},
				}, nil
			case "device.launcher.prompt_templates_save":
				info, ok := payload.(promptTemplatesPayload)
				if !ok {
					t.Fatalf("unexpected payload type %#v", payload)
				}
				rig.pushes = append(rig.pushes, info)
				return model.DeviceLauncherResultPayload{Action: "prompt_templates_save", Success: true}, nil
			default:
				t.Fatalf("unexpected env type %s", envType)
				return model.DeviceLauncherResultPayload{}, nil
			}
		},
	})

	token, err := authManager.Issue(rig.operator)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	rig.token = token
	b.Add(nil, model.HelloPayload{AgentID: "launcher:m_prompt", MachineID: "m_prompt", Kind: "launcher"}, rig.operator.ID)
	b.Add(nil, model.HelloPayload{AgentID: "agent_prompt", MachineID: "m_prompt", Kind: "agent", SemanticAgentID: agentSemanticID}, rig.operator.ID)
	return rig
}

func (r *promptTestRig) do(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+r.token)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	switch {
	case method == http.MethodGet && strings.Contains(path, "device-overrides"):
		r.api.GetPromptDeviceOverrides(rec, req)
	case method == http.MethodPost && strings.Contains(path, "device-overrides"):
		r.api.SavePromptDeviceOverrides(rec, req)
	case method == http.MethodGet && strings.Contains(path, "device-agents"):
		r.api.ListPromptDeviceAgents(rec, req)
	default:
		t.Fatalf("unsupported test route %s %s", method, path)
	}
	return rec
}

// TestSavePromptDeviceOverridesWritesScopedSettingsAndPushes 覆盖设备级写入的三个要点：
// 写到设备归属账号（operator 7，而不是发起请求的账号）、只写设备级 key、保存后立刻下发。
func TestSavePromptDeviceOverridesWritesScopedSettingsAndPushes(t *testing.T) {
	rig := newPromptTestRig(t, "reverse-android")
	body := `{"machine_id":"m_prompt","agent_id":"agent_prompt","semantic_agent_id":"reverse-android","overrides":{"chat.base_prompt":"设备级人设：小王"}}`
	rec := rig.do(t, http.MethodPost, "/api/admin/prompt-registry/device-overrides", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d body=%s", rec.Code, rec.Body.String())
	}
	settings, err := rig.store.GetDeviceSettings(rig.operator.ID, "agent_prompt")
	if err != nil {
		t.Fatalf("read device settings: %v", err)
	}
	if settings["prompt_template.chat.base_prompt"] != "设备级人设：小王" {
		t.Fatalf("设备级覆盖未按 operator+agent 落库: %+v", settings)
	}
	if len(rig.pushes) != 1 {
		t.Fatalf("保存后应立刻下发一次，实际 %d 次", len(rig.pushes))
	}
	if rig.pushes[0].Templates["chat.base_prompt"] != "设备级人设：小王" {
		t.Fatalf("下发内容未取设备级覆盖: %+v", rig.pushes[0].Templates)
	}
	// 未覆盖的槽位也要出现在下发清单里，设备端据此声明完整 prompt_slots。
	if len(rig.pushes[0].Templates) != len(promptregistry.All()) {
		t.Fatalf("下发槽位清单不完整: %d", len(rig.pushes[0].Templates))
	}
	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["pushed"] != true {
		t.Fatalf("响应应标记已下发: %s", rec.Body.String())
	}
}

// TestSavePromptDeviceOverridesClearsAndRejects 校验清空覆盖与非法输入。
func TestSavePromptDeviceOverridesClearsAndRejects(t *testing.T) {
	rig := newPromptTestRig(t, "reverse-android")
	if err := rig.store.SetDeviceSetting(rig.operator.ID, "agent_prompt", "prompt_template.chat.base_prompt", "旧覆盖"); err != nil {
		t.Fatalf("seed override: %v", err)
	}
	rec := rig.do(t, http.MethodPost, "/api/admin/prompt-registry/device-overrides",
		`{"machine_id":"m_prompt","agent_id":"agent_prompt","overrides":{"chat.base_prompt":"   "}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("清空覆盖应成功: %d %s", rec.Code, rec.Body.String())
	}
	settings, _ := rig.store.GetDeviceSettings(rig.operator.ID, "agent_prompt")
	if _, ok := settings["prompt_template.chat.base_prompt"]; ok {
		t.Fatalf("空内容应删除覆盖行: %+v", settings)
	}

	rec = rig.do(t, http.MethodPost, "/api/admin/prompt-registry/device-overrides",
		`{"machine_id":"m_prompt","agent_id":"agent_prompt","overrides":{"unknown.slot":"x"}}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("未知槽位应被拒绝: %d", rec.Code)
	}
}

// TestGetPromptDeviceOverridesShowsThreeLevels 校验三层取值明细（设备 > 语义 > 平台 > 内置）。
func TestGetPromptDeviceOverridesShowsThreeLevels(t *testing.T) {
	rig := newPromptTestRig(t, "reverse-android")
	if _, err := rig.store.UpsertPromptTemplate("chat.base_prompt", "平台默认"); err != nil {
		t.Fatalf("seed platform: %v", err)
	}
	if _, err := rig.store.UpsertSemanticAgent(model.SemanticAgentProfile{
		ID:              "reverse-android",
		Name:            "逆向专家-安卓",
		Enabled:         true,
		PromptTemplates: map[string]string{"chat.question_policy": "语义级提问策略"},
	}); err != nil {
		t.Fatalf("seed semantic agent: %v", err)
	}
	if err := rig.store.SetDeviceSetting(rig.operator.ID, "agent_prompt", "prompt_template.chat.orchestration_prompt", "设备级编排"); err != nil {
		t.Fatalf("seed device override: %v", err)
	}
	rec := rig.do(t, http.MethodGet, "/api/admin/prompt-registry/device-overrides?machine_id=m_prompt&agent_id=agent_prompt", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status %d body=%s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Items []promptSlotResolution `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	byID := map[string]promptSlotResolution{}
	for _, item := range payload.Items {
		byID[item.SlotID] = item
	}
	if got := byID["chat.base_prompt"]; got.Source != "platform" || got.Effective != "平台默认" {
		t.Fatalf("平台默认未生效: %+v", got)
	}
	if got := byID["chat.question_policy"]; got.Source != "semantic" || got.Effective != "语义级提问策略" {
		t.Fatalf("语义级覆盖未生效: %+v", got)
	}
	if got := byID["chat.orchestration_prompt"]; got.Source != "device" || got.Effective != "设备级编排" {
		t.Fatalf("设备级覆盖未生效: %+v", got)
	}
}

// TestSyncDeviceAgentPromptTemplatesSkipsUnchangedRevision 校验增量下发：
// revision 未变时不再重复发 RPC，内容变化后才重新下发。
func TestSyncDeviceAgentPromptTemplatesSkipsUnchangedRevision(t *testing.T) {
	rig := newPromptTestRig(t, "reverse-android")
	ctx := context.Background()
	if _, pushed := rig.api.syncDeviceAgentPromptTemplatesWithSemantic(ctx, rig.operator.ID, "launcher:m_prompt", "agent_prompt", "reverse-android", false); !pushed {
		t.Fatal("首次应下发")
	}
	if _, pushed := rig.api.syncDeviceAgentPromptTemplatesWithSemantic(ctx, rig.operator.ID, "launcher:m_prompt", "agent_prompt", "reverse-android", false); pushed {
		t.Fatal("revision 未变时不应重复下发")
	}
	if len(rig.pushes) != 1 {
		t.Fatalf("下发次数应为 1，实际 %d", len(rig.pushes))
	}
	if _, err := rig.store.UpsertPromptTemplate("chat.base_prompt", "新平台默认"); err != nil {
		t.Fatalf("update platform: %v", err)
	}
	if _, pushed := rig.api.syncDeviceAgentPromptTemplatesWithSemantic(ctx, rig.operator.ID, "launcher:m_prompt", "agent_prompt", "reverse-android", false); !pushed {
		t.Fatal("内容变化后应重新下发")
	}
	if len(rig.pushes) != 2 {
		t.Fatalf("下发次数应为 2，实际 %d", len(rig.pushes))
	}
	// 重启前补推走 force=true：即使 revision 未变也要重新下发，保证槽位清单一定在设备上。
	if _, pushed := rig.api.syncDeviceAgentPromptTemplatesWithSemantic(ctx, rig.operator.ID, "launcher:m_prompt", "agent_prompt", "reverse-android", true); !pushed {
		t.Fatal("force=true 时应强制下发")
	}
	if len(rig.pushes) != 3 {
		t.Fatalf("force 下发后次数应为 3，实际 %d", len(rig.pushes))
	}
}
