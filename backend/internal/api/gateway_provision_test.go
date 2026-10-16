package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"relay-server/internal/broker"
	"relay-server/internal/model"
	"relay-server/internal/store"
)

// 会员分级自动收敛的测试：等级（期望状态）→ 网关钥匙 → 设备配置。
// 见 docs/反代会员分级联动方案.md（v2：零人工）。

// fakeGateway 是一个最小的 cc-gateway 管理面：/admin/keys 的增删改查。
type fakeGateway struct {
	server   *httptest.Server
	adminKey string

	created []struct {
		Name   string
		Models []string
		Limits model.GatewayLimits
	}
	updated []string
	deleted []string
	nextID  int
}

func newFakeGateway(t *testing.T) *fakeGateway {
	t.Helper()
	f := &fakeGateway{adminKey: "test-admin-key"}
	keys := map[string]map[string]any{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Admin-Key") != f.adminKey {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"invalid admin key"}}`))
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/admin/keys") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/admin/keys"), "/")
		switch {
		case r.Method == http.MethodGet && id == "":
			list := make([]map[string]any, 0, len(keys))
			for _, v := range keys {
				list = append(list, v)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"keys": list})
		case r.Method == http.MethodPost && id == "":
			var req struct {
				Name   string              `json:"name"`
				Models []string            `json:"models"`
				Limits model.GatewayLimits `json:"limits"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			f.nextID++
			keyID := "key-" + string(rune('a'+f.nextID-1))
			plain := "sk-cc-test-" + keyID
			keys[keyID] = map[string]any{
				"id": keyID, "name": req.Name, "models": req.Models, "limits": req.Limits,
			}
			f.created = append(f.created, struct {
				Name   string
				Models []string
				Limits model.GatewayLimits
			}{req.Name, req.Models, req.Limits})
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": keyID, "name": req.Name, "key": plain})
		case r.Method == http.MethodPut && id != "":
			if _, ok := keys[id]; !ok {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(`{"error":{"message":"No such key."}}`))
				return
			}
			f.updated = append(f.updated, id)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id})
		case r.Method == http.MethodDelete && id != "":
			delete(keys, id)
			f.deleted = append(f.deleted, id)
			_ = json.NewEncoder(w).Encode(map[string]any{"deleted": id})
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

func TestGatewayPolicyForTierUsesEnvOverride(t *testing.T) {
	t.Setenv(envGatewayPolicies, `{"plus":{"models":["m-plus"],"limits":{"daily_cost_micro":5000000,"max_cost_micro":900000000}}}`)
	cfg := loadGatewayConfig()

	plus := cfg.policyFor("plus")
	if !plus.GatewayEnabled() {
		t.Fatalf("plus 策略应开通专属模型：%+v", plus)
	}
	if len(plus.Models) != 1 || plus.Models[0] != "m-plus" || plus.Limits.DailyCostMicro != 5000000 {
		t.Fatalf("策略覆盖没生效：%+v", plus)
	}
	if plus.ProviderID != model.GatewayProviderPlus || plus.DisplayName == "" {
		t.Fatalf("provider/显示名应自动补全：%+v", plus)
	}

	// 没有配置模型包的等级：不开通（避免把不存在的模型写进设备）
	if free := cfg.policyFor("free"); free.GatewayEnabled() {
		t.Fatalf("free 未配置模型包时不应开通：%+v", free)
	}
	// pro 默认走 pro 的 provider id，但没模型包仍不开通
	if pro := cfg.policyFor("pro"); pro.ProviderID != model.GatewayProviderPro || pro.GatewayEnabled() {
		t.Fatalf("pro 默认策略不符合预期：%+v", pro)
	}
}

func TestGatewayKeyEncryptionRoundTrip(t *testing.T) {
	api := &API{}
	secret := "unit-test-secret"
	plain := "sk-cc-abcdefghijklmnop"
	encoded := api.encryptGatewayKey(secret, plain)
	if encoded == "" || strings.Contains(encoded, plain) {
		t.Fatalf("密文不应为空且不得包含明文：%q", encoded)
	}
	if got := api.decryptGatewayKey(secret, encoded); got != plain {
		t.Fatalf("解密不一致：want %q got %q", plain, got)
	}
	if got := api.decryptGatewayKey("wrong-secret", encoded); got != "" {
		t.Fatalf("错误密钥应解不出来，got %q", got)
	}
	// 没配密钥时不落库（返回空），避免把明文当密文存下去
	if got := api.encryptGatewayKey("", plain); got != "" {
		t.Fatalf("缺少密钥时不应产出密文，got %q", got)
	}
}

func TestGatewayClientCRUD(t *testing.T) {
	f := newFakeGateway(t)
	cfg := gatewayConfig{BaseURL: f.server.URL, AdminKey: f.adminKey}
	api := &API{}
	ctx := context.Background()
	policy := model.MembershipModelPolicy{ProviderID: model.GatewayProviderPlus, Models: []string{"m1", "m2"}, Limits: model.GatewayLimits{MaxCostMicro: 123456}}

	id, plain, err := api.gatewayCreateKey(ctx, cfg, "op_42", policy)
	if err != nil {
		t.Fatalf("建钥匙失败：%v", err)
	}
	if id == "" || plain == "" {
		t.Fatalf("建钥匙应返回 id 与一次性明文：id=%q plain=%q", id, plain)
	}
	if len(f.created) != 1 || f.created[0].Name != "op_42" || len(f.created[0].Models) != 2 || f.created[0].Limits.MaxCostMicro != 123456 {
		t.Fatalf("网关收到的模型包/额度不对：%+v", f.created)
	}

	// 按名字认领（幂等兜底：上次创建成功但平台没落库时用）
	view, found, err := api.gatewayFindKeyByName(ctx, cfg, "op_42")
	if err != nil || !found || view.ID != id {
		t.Fatalf("按名字认领失败：view=%+v found=%v err=%v", view, found, err)
	}

	if err := api.gatewayUpdateKey(ctx, cfg, id, "op_42", policy); err != nil {
		t.Fatalf("改钥匙失败：%v", err)
	}
	if len(f.updated) != 1 || f.updated[0] != id {
		t.Fatalf("网关没收到更新：%+v", f.updated)
	}

	if err := api.gatewayDeleteKey(ctx, cfg, id); err != nil {
		t.Fatalf("删钥匙失败：%v", err)
	}
	if len(f.deleted) != 1 || f.deleted[0] != id {
		t.Fatalf("网关没收到删除：%+v", f.deleted)
	}

	// 管理密钥错误要被当成可重试的错误抛出，而不是静默成功
	if _, _, err := api.gatewayCreateKey(ctx, gatewayConfig{BaseURL: f.server.URL, AdminKey: "wrong"}, "op_43", policy); err == nil {
		t.Fatal("管理密钥错误时应返回错误")
	}
}

// 核心路径：等级策略 → 自动建钥匙（含模型包与额度），全程无人工。
func TestConvergeCreatesGatewayKeyFromPolicy(t *testing.T) {
	f := newFakeGateway(t)
	t.Setenv(envGatewayBaseURL, f.server.URL)
	t.Setenv(envGatewayAdminKey, f.adminKey)
	t.Setenv(envGatewaySecret, "unit-test-secret")
	// 让 free 等级也带模型包：测试不依赖数据库里的真实等级。
	t.Setenv(envGatewayPolicies, `{"free":{"models":["m-a","m-b"],"limits":{"daily_tokens":1000,"max_cost_micro":777}}}`)

	api := &API{store: store.NewMemory(nil), broker: broker.New()}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	record, err := api.ConvergeOperatorMembership(ctx, 42)
	if err != nil {
		t.Fatalf("收敛失败：%v", err)
	}
	if record.State != model.GatewayKeyStateActive {
		t.Fatalf("收敛后状态应为 active，got %s（last_error=%s）", record.State, record.LastError)
	}
	if len(f.created) != 1 || f.created[0].Name != gatewayKeyName(42) {
		t.Fatalf("应以 op_<uid> 命名建钥匙：%+v", f.created)
	}
	if len(f.created[0].Models) != 2 || f.created[0].Limits.MaxCostMicro != 777 || f.created[0].Limits.DailyTokens != 1000 {
		t.Fatalf("钥匙的模型包/额度应来自策略：%+v", f.created[0])
	}
	if record.KeyID == "" || record.KeyLast4 == "" || record.Provider != model.GatewayProviderPlus {
		t.Fatalf("收敛记录不完整：%+v", record)
	}
	// 已配密钥时应能把明文加密落库（不落库就无法在重启后继续下发）
	if record.KeyCipher == "" {
		t.Fatal("配置了 CC_GATEWAY_KEY_SECRET 时应加密保存钥匙")
	}
}

// 未配置网关时应直接报错，而不是假装成功。
func TestConvergeReportsMissingGatewayConfig(t *testing.T) {
	t.Setenv(envGatewayBaseURL, "")
	t.Setenv(envGatewayAdminKey, "")
	api := &API{store: store.NewMemory(nil), broker: broker.New()}
	if _, err := api.ConvergeOperatorMembership(context.Background(), 42); err == nil {
		t.Fatal("未配置网关时应返回错误")
	}
}

// 策略里没有模型包 → 走撤销路径：不建钥匙，状态置为 suspended。
func TestConvergeWithoutModelsSuspends(t *testing.T) {
	f := newFakeGateway(t)
	t.Setenv(envGatewayBaseURL, f.server.URL)
	t.Setenv(envGatewayAdminKey, f.adminKey)
	t.Setenv(envGatewayPolicies, `{}`)

	api := &API{store: store.NewMemory(nil), broker: broker.New()}
	record, err := api.ConvergeOperatorMembership(context.Background(), 42)
	if err != nil {
		t.Fatalf("无模型包时不应报错：%v", err)
	}
	if record.State != model.GatewayKeyStateSuspended {
		t.Fatalf("无模型包时应挂起，got %s", record.State)
	}
	if len(f.created) != 0 {
		t.Fatalf("无模型包时不应建钥匙：%+v", f.created)
	}
}

var _ io.Reader // 保持导入稳定（httptest 请求体在别处使用）

// 打真实网关的端到端用例：验证「平台契约」与线上网关完全对得上。
//
// 跑法（需先打通到网关管理口的通道，例如 SSH -L 127.0.0.1:13051:127.0.0.1:3051）：
//
//	CC_GATEWAY_E2E=1 CC_GATEWAY_BASE_URL=http://127.0.0.1:13051 \
//	CC_GATEWAY_ADMIN_KEY=<admin.key> CC_GATEWAY_KEY_SECRET=e2e \
//	CC_GATEWAY_POLICIES='{"free":{"models":["z-ai/glm-5.3-flash"],"limits":{"daily_cost_micro":250000}}}' \
//	go test ./internal/api -run TestConvergeAgainstRealGateway -v
func TestConvergeAgainstRealGateway(t *testing.T) {
	if os.Getenv("CC_GATEWAY_E2E") != "1" {
		t.Skip("未开启：需要 CC_GATEWAY_E2E=1 且能访问网关管理口")
	}
	const operatorID int64 = 424242
	cfg := loadGatewayConfig()
	if !cfg.enabled() {
		t.Fatal("需要 CC_GATEWAY_BASE_URL 与 CC_GATEWAY_ADMIN_KEY")
	}
	policy := cfg.policyFor("free")
	if !policy.GatewayEnabled() {
		t.Fatal("需要为 free 等级配置模型包（CC_GATEWAY_POLICIES）")
	}

	api := &API{store: store.NewMemory(nil), broker: broker.New()}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 清场：上一次失败留下的同名钥匙
	if view, found, err := api.gatewayFindKeyByName(ctx, cfg, gatewayKeyName(operatorID)); err == nil && found {
		_ = api.gatewayDeleteKey(ctx, cfg, view.ID)
	}

	record, err := api.ConvergeOperatorMembership(ctx, operatorID)
	if err != nil {
		t.Fatalf("对真实网关收敛失败：%v", err)
	}
	if record.State != model.GatewayKeyStateActive || record.KeyID == "" {
		t.Fatalf("收敛结果异常：state=%s key_id=%q last_error=%s", record.State, record.KeyID, record.LastError)
	}

	view, found, err := api.gatewayFindKeyByName(ctx, cfg, gatewayKeyName(operatorID))
	if err != nil || !found {
		t.Fatalf("真实网关上应能按名字查到钥匙：found=%v err=%v", found, err)
	}
	if !sameStringSet(view.Models, policy.Models) {
		t.Fatalf("网关上的模型包与策略不一致：gateway=%v policy=%v", view.Models, policy.Models)
	}
	if view.Limits != policy.Limits {
		t.Fatalf("网关上的额度与策略不一致：gateway=%+v policy=%+v", view.Limits, policy.Limits)
	}
	t.Logf("✅ 真实网关钥匙已建：id=%s models=%v limits=%+v", view.ID, view.Models, view.Limits)

	// 撤销路径：非会员时应当把设备 provider 撤掉并删除钥匙
	if _, err := api.deprovisionOperator(ctx, cfg, *record, policy, "端到端用例收尾"); err != nil {
		t.Fatalf("撤销失败：%v", err)
	}
	if _, found, _ := api.gatewayFindKeyByName(ctx, cfg, gatewayKeyName(operatorID)); found {
		t.Fatal("撤销后钥匙应已从网关删除")
	}
	t.Log("✅ 撤销完成：网关钥匙已删除")
}

// 管理地址与推理地址必须分开：设备拿到的是公网推理口。
func TestGatewayDeviceBaseURLUsesProxyURL(t *testing.T) {
	cfg := gatewayConfig{BaseURL: "http://127.0.0.1:13051", ProxyURL: "http://114.66.31.184:3050"}
	if got := cfg.deviceBaseURL(); got != "http://114.66.31.184:3050/v1" {
		t.Fatalf("设备侧 base_url 应为推理口：got %q", got)
	}
	// 没配推理口时回落到管理口（单机部署场景）
	only := gatewayConfig{BaseURL: "http://gw.local:3051"}
	if got := only.deviceBaseURL(); got != "http://gw.local:3051/v1" {
		t.Fatalf("回落到管理口：got %q", got)
	}
}

// 展示名可自定义，但模型 id 必须保持真实（网关按 id 鉴权与转发）。
func TestGatewayDeviceModelsKeepsRealIDWithCustomName(t *testing.T) {
	policy := model.MembershipModelPolicy{
		ProviderID: model.GatewayProviderPro,
		Models:     []string{"deepseek-v4.1-flash-free"},
		ModelNames: map[string]string{"deepseek-v4.1-flash-free": "高级专线 · DeepSeek 4.1"},
	}.Normalize()
	items := gatewayDeviceModels(policy)
	if len(items) != 1 {
		t.Fatalf("want 1 model, got %d", len(items))
	}
	if items[0].ID != "deepseek-v4.1-flash-free" {
		t.Fatalf("id 必须保持真实模型：got %q", items[0].ID)
	}
	if items[0].Name != "高级专线 · DeepSeek 4.1" {
		t.Fatalf("展示名应可自定义：got %q", items[0].Name)
	}
	if items[0].Owned != model.GatewayProviderPro {
		t.Fatalf("owned_by 应为 provider id：got %q", items[0].Owned)
	}
}

// 参数预设要真的进到下发的模型条目：多模态 / 上下文 / 最大输出 / 思考强度。
func TestGatewayDeviceModelsCarryPreset(t *testing.T) {
	policy := model.MembershipModelPolicy{
		Tier:       "pro",
		ProviderID: "cc-pro",
		Models:     []string{"cc-pro-flash"},
		ModelNames: map[string]string{"cc-pro-flash": "高级专线 · DeepSeek Flash"},
		RealModels: map[string]string{"cc-pro-flash": "deepseek-v4.1-flash-free"},
		Presets: map[string]model.ModelPreset{
			"cc-pro-flash": {
				InputModalities:  []string{"text", "image"},
				OutputModalities: []string{"text"},
				ContextLimit:     128000,
				OutputLimit:      8192,
				// 思考强度是多选：勾了三档 → 三档都可切换
				ReasoningLevels: []string{"low", "medium", "high"},
			},
		},
	}
	models := gatewayDeviceModels(policy)
	if len(models) != 1 {
		t.Fatalf("应下发 1 个模型：%+v", models)
	}
	item := models[0]
	// 设备侧只应看到别名，真实模型名不下发
	if item.ID != "cc-pro-flash" || item.Name != "高级专线 · DeepSeek Flash" {
		t.Fatalf("id/name 不对：%+v", item)
	}
	if strings.Contains(item.Name, "deepseek") || strings.Contains(item.ID, "deepseek") {
		t.Fatalf("真实模型名不应出现在设备侧：%+v", item)
	}
	if item.Modalities == nil || len(item.Modalities.Input) != 2 || item.ModalitiesSource != "manual" {
		t.Fatalf("多模态未下发：%+v", item.Modalities)
	}
	if item.Context != 128000 || item.ManualContext != 128000 || item.Output != 8192 || item.ManualOutput != 8192 {
		t.Fatalf("上下文/最大输出未下发：context=%d output=%d", item.Context, item.Output)
	}
	if item.Thinking == nil || !item.Thinking.Supported || len(item.Thinking.OverrideVariants) != 3 {
		t.Fatalf("思考强度未下发：%+v", item.Thinking)
	}
	if len(item.Modalities.Output) != 1 || item.Modalities.Output[0] != "text" {
		t.Fatalf("输出方式未下发：%+v", item.Modalities.Output)
	}
	if got := item.Variants["high"].(map[string]any)["reasoningEffort"]; got != "high" {
		t.Fatalf("档位映射不对：%+v", item.Variants)
	}
}

// 只勾一档 = 固定强度（variants 里只有一个）。
func TestGatewayDeviceModelsSingleReasoningLevel(t *testing.T) {
	policy := model.MembershipModelPolicy{
		Tier:       "plus",
		ProviderID: "cc-plus",
		Models:     []string{"cc-plus-flash"},
		Presets: map[string]model.ModelPreset{
			"cc-plus-flash": {ReasoningLevels: []string{"medium"}},
		},
	}
	models := gatewayDeviceModels(policy)
	if len(models) != 1 || models[0].Thinking == nil {
		t.Fatalf("应下发思考能力：%+v", models)
	}
	if got := len(models[0].Thinking.OverrideVariants); got != 1 {
		t.Fatalf("单档位只应有一个 variant，实际 %d", got)
	}
	if _, ok := models[0].Variants["medium"]; !ok {
		t.Fatalf("档位不对：%+v", models[0].Variants)
	}
}

// 会员模型要出现在给 App 的模型列表里（供应商名与展示名、参数预设都要带上）。
func TestMergeMemberProviderIntoModels(t *testing.T) {
	policy := model.MembershipModelPolicy{
		Tier:        "pro",
		ProviderID:  "cc-pro",
		DisplayName: "会员专属",
		Models:      []string{"cc-pro-flash"},
		ModelNames:  map[string]string{"cc-pro-flash": "肥美大鲸鱼"},
		RealModels:  map[string]string{"cc-pro-flash": "deepseek-v4.1-flash-free"},
		Presets: map[string]model.ModelPreset{
			"cc-pro-flash": {ContextLimit: 1000000, OutputLimit: 384000, ReasoningLevels: []string{"low", "medium", "high", "max"}},
		},
	}
	payload := map[string]any{
		"connected": []string{"ds22"},
		"all":       []map[string]any{{"id": "ds22", "name": "ds22"}},
		"default":   map[string]any{},
	}
	out := mergeMemberProviderIntoModels(payload, policy, "http://114.66.31.184:3050/v1")

	connected := stringSlice(out["connected"])
	if len(connected) != 2 || connected[1] != "cc-pro" {
		t.Fatalf("会员 provider 应进 connected：%v", connected)
	}
	var member map[string]any
	for _, item := range mapSlice(out["all"]) {
		if id, _ := item["id"].(string); id == "cc-pro" {
			member = item
		}
	}
	if member == nil {
		t.Fatalf("会员 provider 没进模型列表：%+v", out["all"])
	}
	if member["name"] != "会员专属" {
		t.Fatalf("供应商名应为 会员专属：%v", member["name"])
	}
	entries, _ := member["models"].(map[string]any)
	entry, _ := entries["cc-pro-flash"].(map[string]any)
	if entry == nil {
		t.Fatalf("模型条目缺失：%+v", member["models"])
	}
	if entry["name"] != "肥美大鲸鱼" {
		t.Fatalf("展示名不对：%v", entry["name"])
	}
	if toInt64(entry["context_limit"]) != 1000000 || toInt64(entry["output_limit"]) != 384000 {
		t.Fatalf("上下文/输出不对：%v / %v", entry["context_limit"], entry["output_limit"])
	}
	if variants, _ := entry["variants"].(map[string]any); len(variants) != 4 {
		t.Fatalf("思考档位应带上：%+v", entry["variants"])
	}
	// 真实模型名不能出现在给 App 的列表里
	if raw := fmt.Sprintf("%+v", out); strings.Contains(raw, "deepseek-v4.1-flash-free") {
		t.Fatalf("真实模型名泄露：%s", raw[:200])
	}

	// 启动器已经报过就不重复补
	out2 := mergeMemberProviderIntoModels(map[string]any{
		"connected": []string{"cc-pro"},
		"all":       []map[string]any{{"id": "cc-pro"}},
		"default":   map[string]any{},
	}, policy, "")
	if len(mapSlice(out2["all"])) != 1 {
		t.Fatalf("重复注入：%+v", out2["all"])
	}
}

// 只勾一档 = 固定强度（variants 里只有一个）。
func toInt64(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	default:
		return 0
	}
}
