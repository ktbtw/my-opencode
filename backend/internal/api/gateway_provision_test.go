package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
