package api

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"relay-server/internal/model"
)

// 会员分级的自动收敛：等级（期望状态）→ 网关虚拟钥匙 → 用户设备的 AI 配置。
//
// 设计见 docs/反代会员分级联动方案.md：网关是账本与门禁（一把钥匙 = 模型包 + 额度包），
// 平台负责自动建/改/删钥匙并把专属 provider 下发到设备，
// 于是会员在 App 的模型列表里直接看到专属模型（/v1/models 已按钥匙过滤）。
//
// 配置（环境变量）：
//
//	CC_GATEWAY_BASE_URL   网关地址，例 https://cc.example.com（不带 /v1）
//	CC_GATEWAY_ADMIN_KEY  网关管理密钥（X-Admin-Key）
//	CC_GATEWAY_KEY_SECRET 加密虚拟钥匙用的密钥（不配则钥匙不落库，只能即时下发）
//	CC_GATEWAY_POLICIES   等级策略 JSON 覆盖，例
//	                      {"plus":{"models":["m1","m2"],"limits":{"daily_cost_micro":5000000}},
//	                       "pro":{"models":["m1","m2","m3"],"limits":{"max_cost_micro":500000000}}}
const (
	envGatewayBaseURL  = "CC_GATEWAY_BASE_URL"  // 平台 → 网关管理口（如经隧道的 127.0.0.1:13051）
	envGatewayProxyURL = "CC_GATEWAY_PROXY_URL" // 设备 → 网关推理口（必须是设备能访问到的地址）
	envGatewayAdminKey = "CC_GATEWAY_ADMIN_KEY"
	envGatewaySecret   = "CC_GATEWAY_KEY_SECRET"
	envGatewayPolicies = "CC_GATEWAY_POLICIES"

	gatewayRequestTimeout = 20 * time.Second
	gatewayConvergeBudget = 3 * time.Minute
)

type gatewayConfig struct {
	BaseURL  string // 管理口（平台侧使用）
	ProxyURL string // 推理口（下发到设备；为空时回落到 BaseURL）
	AdminKey string
	Secret   string
	Policies map[string]model.MembershipModelPolicy
}

// deviceBaseURL 返回设备侧要填的 base_url（带 /v1）。
//
// 管理地址与推理地址常常不同：平台经隧道访问管理口（127.0.0.1:13051），
// 而设备必须走网关的公网推理口（如 http://host:3050/v1）。混用会让设备拿到
// 一个只在本机有效的地址——真机实测踩过。
func (c gatewayConfig) deviceBaseURL() string {
	base := strings.TrimSpace(c.ProxyURL)
	if base == "" {
		base = c.BaseURL
	}
	return strings.TrimRight(base, "/") + "/v1"
}

// loadGatewayConfig 每次读取环境变量：调用频率很低（改等级/心跳/管理口），换取零缓存失效问题。
func loadGatewayConfig() gatewayConfig {
	cfg := gatewayConfig{
		BaseURL:  strings.TrimRight(strings.TrimSpace(os.Getenv(envGatewayBaseURL)), "/"),
		ProxyURL: strings.TrimRight(strings.TrimSpace(os.Getenv(envGatewayProxyURL)), "/"),
		AdminKey: strings.TrimSpace(os.Getenv(envGatewayAdminKey)),
		Secret:   strings.TrimSpace(os.Getenv(envGatewaySecret)),
	}
	if raw := strings.TrimSpace(os.Getenv(envGatewayPolicies)); raw != "" {
		var parsed map[string]model.MembershipModelPolicy
		if err := json.Unmarshal([]byte(raw), &parsed); err == nil {
			cfg.Policies = parsed
		} else {
			log.Printf("[gateway] %s 解析失败，已忽略：%v", envGatewayPolicies, err)
		}
	}
	return cfg
}

func (c gatewayConfig) enabled() bool { return c.BaseURL != "" && c.AdminKey != "" }

// policyFor 返回等级策略：环境变量覆盖优先，其次内置默认（默认不开通专属模型）。
func (c gatewayConfig) policyFor(tier string) model.MembershipModelPolicy {
	normalized := model.NormalizeMembershipTier(tier)
	if policy, ok := c.Policies[normalized]; ok {
		policy.Tier = normalized
		return policy.Normalize()
	}
	return model.ModelPolicyForTier(normalized).Normalize()
}

// ── 网关管理 API（/admin/keys） ───────────────────────────────────────────

type gatewayKeyView struct {
	ID       string              `json:"id"`
	Name     string              `json:"name"`
	Disabled bool                `json:"disabled,omitempty"`
	Models   []string            `json:"models,omitempty"`
	Limits   model.GatewayLimits `json:"limits,omitempty"`
}

func (a *API) gatewayRequest(ctx context.Context, cfg gatewayConfig, method, path string, body any) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, cfg.BaseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Admin-Key", cfg.AdminKey)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: gatewayRequestTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("网关请求失败：%w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return raw, fmt.Errorf("网关 %s %s 返回 %d：%s", method, path, resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return raw, nil
}

func gatewayKeyBody(name string, policy model.MembershipModelPolicy) map[string]any {
	return map[string]any{"name": name, "models": policy.Models, "limits": policy.Limits}
}

// gatewayCreateKey 建钥匙；明文只返回一次，必须立刻落库（或加密后落库）。
func (a *API) gatewayCreateKey(ctx context.Context, cfg gatewayConfig, name string, policy model.MembershipModelPolicy) (string, string, error) {
	raw, err := a.gatewayRequest(ctx, cfg, http.MethodPost, "/admin/keys", gatewayKeyBody(name, policy))
	if err != nil {
		return "", "", err
	}
	var out struct {
		ID  string `json:"id"`
		Key string `json:"key"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", "", fmt.Errorf("解析建钥匙响应失败：%w", err)
	}
	if out.ID == "" || out.Key == "" {
		return "", "", errors.New("网关未返回钥匙明文")
	}
	return out.ID, out.Key, nil
}

func (a *API) gatewayUpdateKey(ctx context.Context, cfg gatewayConfig, id, name string, policy model.MembershipModelPolicy) error {
	_, err := a.gatewayRequest(ctx, cfg, http.MethodPut, "/admin/keys/"+id, gatewayKeyBody(name, policy))
	return err
}

func (a *API) gatewayDeleteKey(ctx context.Context, cfg gatewayConfig, id string) error {
	_, err := a.gatewayRequest(ctx, cfg, http.MethodDelete, "/admin/keys/"+id, nil)
	return err
}

// gatewayFindKeyByName 用于「创建成功但平台没落库」时的认领，避免重复建钥匙。
func (a *API) gatewayFindKeyByName(ctx context.Context, cfg gatewayConfig, name string) (gatewayKeyView, bool, error) {
	raw, err := a.gatewayRequest(ctx, cfg, http.MethodGet, "/admin/keys", nil)
	if err != nil {
		return gatewayKeyView{}, false, err
	}
	var out struct {
		Keys []gatewayKeyView `json:"keys"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return gatewayKeyView{}, false, fmt.Errorf("解析钥匙列表失败：%w", err)
	}
	for _, key := range out.Keys {
		if key.Name == name {
			return key, true, nil
		}
	}
	return gatewayKeyView{}, false, nil
}

// ── 钥匙加密（AES-GCM，密钥来自 CC_GATEWAY_KEY_SECRET） ──────────────────

func gatewayCipherKey(secret string) []byte {
	sum := sha256.Sum256([]byte(secret))
	return sum[:]
}

func (a *API) encryptGatewayKey(secret, plain string) string {
	if strings.TrimSpace(plain) == "" || strings.TrimSpace(secret) == "" {
		return ""
	}
	block, err := aes.NewCipher(gatewayCipherKey(secret))
	if err != nil {
		return ""
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return ""
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return ""
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(sealed)
}

func (a *API) decryptGatewayKey(secret, encoded string) string {
	if strings.TrimSpace(encoded) == "" || strings.TrimSpace(secret) == "" {
		return ""
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return ""
	}
	block, err := aes.NewCipher(gatewayCipherKey(secret))
	if err != nil {
		return ""
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil || len(raw) < gcm.NonceSize() {
		return ""
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return ""
	}
	return string(plain)
}

// ── 收敛：期望状态 → 钥匙 → 设备 ────────────────────────────────────────

func gatewayKeyName(operatorID int64) string { return fmt.Sprintf("op_%d", operatorID) }

func gatewayKeyLast4(plain string) string {
	if len(plain) <= 4 {
		return plain
	}
	return plain[len(plain)-4:]
}

// ConvergeOperatorMembership 把某个用户的会员等级收敛到网关钥匙与设备配置。
// 幂等：可重复调用；失败会记录 last_error 并返回错误，调用方负责重试。
func (a *API) ConvergeOperatorMembership(ctx context.Context, operatorID int64) (*model.OperatorGatewayKey, error) {
	if operatorID <= 0 {
		return nil, errors.New("用户 ID 不合法")
	}
	cfg := loadGatewayConfig()
	if !cfg.enabled() {
		return nil, errors.New("未配置网关管理地址/密钥（CC_GATEWAY_BASE_URL、CC_GATEWAY_ADMIN_KEY）")
	}

	tier := model.MembershipTierFree
	if resolved, err := a.store.GetOperatorMembershipTier(operatorID); err == nil && strings.TrimSpace(resolved) != "" {
		tier = resolved
	}
	tier = model.NormalizeMembershipTier(tier)
	// 控制台配置优先（DB），其次环境变量，最后内置默认
	policy := a.tierPolicy(ctx, tier)

	record, _ := a.store.GetOperatorGatewayKey(ctx, operatorID)
	if record == nil {
		record = &model.OperatorGatewayKey{OperatorID: operatorID}
	}

	if !policy.GatewayEnabled() {
		return a.deprovisionOperator(ctx, cfg, *record, policy, "该等级未开通专属模型")
	}

	record.DesiredTier = tier
	record.Provider = policy.ProviderID
	record.State = model.GatewayKeyStateProvisioning

	plain := a.decryptGatewayKey(cfg.Secret, record.KeyCipher)
	name := gatewayKeyName(operatorID)

	// 1) 钥匙：没有就建；建失败先按名字认领（上次成功但没落库的孤儿钥匙）。
	if record.KeyID == "" {
		id, secret, err := a.gatewayCreateKey(ctx, cfg, name, policy)
		if err != nil {
			view, found, findErr := a.gatewayFindKeyByName(ctx, cfg, name)
			if findErr != nil || !found {
				record.State = model.GatewayKeyStateFailed
				record.LastError = err.Error()
				_ = a.store.UpsertOperatorGatewayKey(ctx, *record)
				return record, err
			}
			record.KeyID = view.ID
		} else {
			record.KeyID = id
			plain = secret
			record.KeyLast4 = gatewayKeyLast4(secret)
		}
	}

	// 2) 模型包/额度：与快照不同就更新（等级切换、策略调整都走这里）。
	if !sameStringSet(record.Models, policy.Models) || record.Limits != policy.Limits {
		if err := a.gatewayUpdateKey(ctx, cfg, record.KeyID, name, policy); err != nil {
			// 钥匙已被删除时重建一次，避免旧状态卡死。
			if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "not found") {
				id, secret, createErr := a.gatewayCreateKey(ctx, cfg, name, policy)
				if createErr != nil {
					record.State = model.GatewayKeyStateFailed
					record.LastError = createErr.Error()
					_ = a.store.UpsertOperatorGatewayKey(ctx, *record)
					return record, createErr
				}
				record.KeyID, plain = id, secret
				record.KeyLast4 = gatewayKeyLast4(secret)
			} else {
				record.State = model.GatewayKeyStateFailed
				record.LastError = err.Error()
				_ = a.store.UpsertOperatorGatewayKey(ctx, *record)
				return record, err
			}
		}
	}

	// 3) 明文钥匙不可用（没配 CC_GATEWAY_KEY_SECRET 或已丢失）→ 轮换一把，保证设备能拿到可用钥匙。
	if plain == "" {
		oldID := record.KeyID
		id, secret, err := a.gatewayCreateKey(ctx, cfg, name, policy)
		if err != nil {
			record.State = model.GatewayKeyStateFailed
			record.LastError = "轮换钥匙失败：" + err.Error()
			_ = a.store.UpsertOperatorGatewayKey(ctx, *record)
			return record, err
		}
		record.KeyID, plain = id, secret
		record.KeyLast4 = gatewayKeyLast4(secret)
		if oldID != "" && oldID != id {
			_ = a.gatewayDeleteKey(ctx, cfg, oldID)
		}
	}

	// 4) 下发到用户的在线设备（每台设备的每个 agent 一份专属 provider）。
	// 推理地址变化时先清掉设备上的旧 provider 再下发：launcher 只在 api 字段为空时写入，
	// 否则会留下旧地址（管理地址/推理地址分离后真机实测踩过）。
	reset := record.DeviceBaseURL != "" && record.DeviceBaseURL != cfg.deviceBaseURL()
	record.Machines = a.deliverMemberProvider(ctx, cfg, operatorID, policy, plain, reset)
	record.DeviceBaseURL = cfg.deviceBaseURL()
	record.Models = policy.Models
	record.Limits = policy.Limits
	if encrypted := a.encryptGatewayKey(cfg.Secret, plain); encrypted != "" {
		record.KeyCipher = encrypted
	} else {
		record.KeyCipher = ""
		log.Printf("[gateway] %s 未配置，虚拟钥匙无法落库：用户 %d 的钥匙仅在本次下发有效", envGatewaySecret, operatorID)
	}
	record.State = model.GatewayKeyStateActive
	record.LastError = ""
	record.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := a.store.UpsertOperatorGatewayKey(ctx, *record); err != nil {
		return record, err
	}
	return record, nil
}

// deprovisionOperator 非会员（或策略关闭）时：撤掉设备上的专属 provider 并删除网关钥匙。
func (a *API) deprovisionOperator(ctx context.Context, cfg gatewayConfig, record model.OperatorGatewayKey, policy model.MembershipModelPolicy, reason string) (*model.OperatorGatewayKey, error) {
	provider := record.Provider
	if provider == "" {
		provider = policy.ProviderID
	}
	if provider != "" {
		record.Machines = a.clearMemberProvider(ctx, operatorIDOf(record), provider)
	}
	if record.KeyID != "" {
		if err := a.gatewayDeleteKey(ctx, cfg, record.KeyID); err != nil {
			log.Printf("[gateway] 删除钥匙失败 operator=%d key=%s：%v", record.OperatorID, record.KeyID, err)
		}
	}
	record.KeyID = ""
	record.KeyCipher = ""
	record.KeyLast4 = ""
	record.Models = nil
	record.Limits = model.GatewayLimits{}
	record.DeviceBaseURL = ""
	record.State = model.GatewayKeyStateSuspended
	record.LastError = reason
	record.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := a.store.UpsertOperatorGatewayKey(ctx, record); err != nil {
		return &record, err
	}
	return &record, nil
}

func operatorIDOf(record model.OperatorGatewayKey) int64 { return record.OperatorID }

// deliverMemberProvider 把专属 provider 写进该用户所有在线设备。
//
// 语义与既有 SaveDeviceAIConfig 完全一致：请求发给该设备的 launcher agent，
// 载荷里的 AgentID 填 machineID（设备级配置），launcher 侧按 provider id upsert，
// 因此不会动用户自己的其它 provider。写入后刷新平台的模型缓存，App 立刻能看到专属模型。
func (a *API) deliverMemberProvider(ctx context.Context, cfg gatewayConfig, operatorID int64, policy model.MembershipModelPolicy, plainKey string, reset bool) map[string]string {
	results := map[string]string{}
	baseURL := cfg.deviceBaseURL()
	apiMode := normalizeDeviceAIAPIMode("")
	models := normalizeDeviceAIModels(policy.ProviderID, gatewayDeviceModels(policy))

	for _, machine := range a.broker.ListMachines(operatorID) {
		launcherID, err := a.deviceLauncherAgent(operatorID, machine.MachineID)
		if err != nil {
			results[machine.MachineID] = "launcher 离线，待下次心跳自愈"
			continue
		}
		if reset {
			cleanup := model.DeviceAIConfigProviderCleanupPayload{AgentID: machine.MachineID, Provider: policy.ProviderID}
			cleanupID := fmt.Sprintf("gwrst-%d-%s-%d", operatorID, machine.MachineID, time.Now().UnixNano())
			_, _ = a.requestDeviceAIConfig(ctx, operatorID, launcherID, cleanupID, "device.ai_config.clear_provider", cleanup)
		}
		payload := model.DeviceAIConfigSavePayload{
			// 会员 provider 由平台托管：启动器侧会对用户隐藏（AI 配置页 + 全局 JSON），
			// 保存 JSON 时也会被保留，避免被用户编辑冲掉。
			Managed:  true,
			AgentID:  machine.MachineID,
			Provider: policy.ProviderID,
			BaseURL:  baseURL,
			APIKey:   plainKey,
			APIMode:  apiMode,
			Config: model.DeviceAIConfig{
				Provider: policy.ProviderID,
				BaseURL:  baseURL,
				APIKey:   plainKey,
				APIMode:  apiMode,
				Models:   models,
			},
		}
		requestID := fmt.Sprintf("gwprov-%d-%s-%d", operatorID, machine.MachineID, time.Now().UnixNano())
		result, err := a.requestDeviceAIConfig(ctx, operatorID, launcherID, requestID, "device.ai_config.save", payload)
		if err != nil {
			results[machine.MachineID] = "下发失败：" + err.Error()
			log.Printf("[gateway] 下发会员 provider 失败 operator=%d machine=%s：%v", operatorID, machine.MachineID, err)
			continue
		}
		enrichDeviceAIConfigResult(&result)
		a.updateLauncherModelCache(operatorID, machine.MachineID, result)
		results[machine.MachineID] = "ok"
	}
	return results
}

// clearMemberProvider 降级/退订时移除专属 provider（保留用户自己的其它 provider）。
func (a *API) clearMemberProvider(ctx context.Context, operatorID int64, provider string) map[string]string {
	results := map[string]string{}
	for _, machine := range a.broker.ListMachines(operatorID) {
		launcherID, err := a.deviceLauncherAgent(operatorID, machine.MachineID)
		if err != nil {
			results[machine.MachineID] = "launcher 离线"
			continue
		}
		payload := model.DeviceAIConfigProviderCleanupPayload{AgentID: machine.MachineID, Provider: provider}
		requestID := fmt.Sprintf("gwcls-%d-%s-%d", operatorID, machine.MachineID, time.Now().UnixNano())
		result, err := a.requestDeviceAIConfig(ctx, operatorID, launcherID, requestID, "device.ai_config.clear_provider", payload)
		if err != nil {
			results[machine.MachineID] = "清理失败：" + err.Error()
			continue
		}
		enrichDeviceAIConfigResult(&result)
		a.updateLauncherModelCache(operatorID, machine.MachineID, result)
		results[machine.MachineID] = "已清理"
	}
	return results
}

func gatewayDeviceModels(policy model.MembershipModelPolicy) []model.DeviceAIModel {
	out := make([]model.DeviceAIModel, 0, len(policy.Models))
	for _, id := range policy.Models {
		// 下发给设备的是"对外模型名（别名）"：设备用它发请求，网关再映射到真实模型，
		// 所以真实模型名永远不会落到用户设备上；name 是展示名，可自定义。
		item := model.DeviceAIModel{ID: id, Name: policy.DisplayNameFor(id), Owned: policy.ProviderID}
		if preset, ok := policy.Presets[id]; ok {
			item = applyModelPreset(item, preset)
		}
		out = append(out, item)
	}
	return out
}

// applyModelPreset 把控制台预设写进下发的模型条目：
// 上下文/最大输出按"手填值"下发（不会被供应商返回值覆盖），多模态与思考强度同理。
func applyModelPreset(item model.DeviceAIModel, preset model.ModelPreset) model.DeviceAIModel {
	if len(preset.InputModalities) > 0 || len(preset.OutputModalities) > 0 {
		item.Modalities = &model.DeviceAIModalities{
			Input:  preset.InputModalities,
			Output: preset.OutputModalities,
		}
		item.ModalitiesSource = "manual"
	}
	if preset.ContextLimit > 0 {
		item.Context = preset.ContextLimit
		item.ManualContext = preset.ContextLimit
	}
	if preset.OutputLimit > 0 {
		item.Output = preset.OutputLimit
		item.ManualOutput = preset.OutputLimit
	}
	// 思考强度：按勾选的档位生成 variants（与启动器/App 的既有约定一致），
	// 勾一档 = 固定强度，勾多档 = 用户可在 App 里切换。
	if len(preset.ReasoningLevels) > 0 {
		variants := map[string]any{}
		for _, level := range preset.ReasoningLevels {
			variants[level] = map[string]any{"reasoningEffort": level}
		}
		item.Variants = variants
		item.Thinking = &model.DeviceAIThinking{
			Supported:        true,
			Source:           "manual",
			OverrideEnabled:  true,
			OverrideVariants: variants,
			Variants:         variants,
		}
	} else {
		item.Thinking = &model.DeviceAIThinking{Supported: false}
	}
	return item
}

// ConvergeOperatorMembershipAsync 供管理操作（改等级/兑换）调用，不阻塞请求。
func (a *API) ConvergeOperatorMembershipAsync(operatorID int64) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), gatewayConvergeBudget)
		defer cancel()
		record, err := a.ConvergeOperatorMembership(ctx, operatorID)
		if err != nil {
			log.Printf("[gateway] 会员收敛失败 operator=%d：%v", operatorID, err)
			return
		}
		log.Printf("[gateway] 会员收敛完成 operator=%d tier=%s state=%s provider=%s 设备=%v",
			operatorID, record.DesiredTier, record.State, record.Provider, record.Machines)
	}()
}

// ── 管理口 ──────────────────────────────────────────────────────────────

// GetOperatorGatewayKey 查看某用户的钥匙状态（不返回密文）。
func (a *API) GetOperatorGatewayKey(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	operatorID, ok := parseOperatorIDParam(w, r)
	if !ok {
		return
	}
	cfg := loadGatewayConfig()
	tier := model.MembershipTierFree
	if resolved, err := a.store.GetOperatorMembershipTier(operatorID); err == nil && strings.TrimSpace(resolved) != "" {
		tier = model.NormalizeMembershipTier(resolved)
	}
	record, err := a.store.GetOperatorGatewayKey(r.Context(), operatorID)
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	var safe any
	if record != nil {
		record.KeyCipher = ""
		safe = record
	}
	write(w, http.StatusOK, map[string]any{
		"operator_id":     operatorID,
		"membership_tier": tier,
		"policy":          a.tierPolicy(r.Context(), tier),
		"gateway": map[string]any{
			"configured": cfg.enabled(), "base_url": cfg.BaseURL, "proxy_url": cfg.ProxyURL,
			"device_base_url": cfg.deviceBaseURL(), "secret_configured": cfg.Secret != "",
		},
		"record": safe,
	})
}

// EnsureOperatorGatewayKey 立即收敛（控制台应急按钮 / 自动化重试入口）。
func (a *API) EnsureOperatorGatewayKey(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	operatorID, ok := parseOperatorIDParam(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), gatewayConvergeBudget)
	defer cancel()
	record, err := a.ConvergeOperatorMembership(ctx, operatorID)
	if record != nil {
		record.KeyCipher = ""
	}
	if err != nil {
		write(w, http.StatusBadGateway, map[string]any{"error": err.Error(), "record": record})
		return
	}
	write(w, http.StatusOK, map[string]any{"record": record})
}

// ReconcileMembership 周期性兜底：把存量会员与新策略收敛一遍（零人工的关键补丁）。
//
// 触发场景：改等级只针对"被改的那个人"；存量会员、策略调整、上次失败的重试、
// 网关抖动恢复，都靠这个巡检自愈。幂等：只在快照不一致时才真的调用网关。
func (a *API) ReconcileMembership(ctx context.Context) (int, int) {
	cfg := loadGatewayConfig()
	if !cfg.enabled() {
		return 0, 0
	}
	operators, err := a.store.ListOperators()
	if err != nil {
		log.Printf("[gateway] 会员巡检：读取用户失败：%v", err)
		return 0, 0
	}
	converged, failed := 0, 0
	for _, operator := range operators {
		if ctx.Err() != nil {
			break
		}
		tier := model.NormalizeMembershipTier(operator.MembershipTier)
		if resolved, err := a.store.GetOperatorMembershipTier(operator.ID); err == nil && strings.TrimSpace(resolved) != "" {
			tier = model.NormalizeMembershipTier(resolved)
		}
		policy := a.tierPolicy(ctx, tier)
		record, _ := a.store.GetOperatorGatewayKey(ctx, operator.ID)
		if !policy.GatewayEnabled() {
			// 非会员（或该等级未开专属模型）：有残留钥匙才需要收尾
			if record != nil && (record.KeyID != "" || record.Provider != "") {
				if _, err := a.deprovisionOperator(ctx, cfg, *record, policy, "该等级未开通专属模型"); err != nil {
					failed++
					continue
				}
				converged++
			}
			continue
		}
		if record != nil && record.State == model.GatewayKeyStateActive &&
			sameStringSet(record.Models, policy.Models) && record.Limits == policy.Limits &&
			record.Provider == policy.ProviderID && record.DeviceBaseURL == cfg.deviceBaseURL() {
			continue // 已是最新，跳过
		}
		if _, err := a.ConvergeOperatorMembership(ctx, operator.ID); err != nil {
			failed++
			continue
		}
		converged++
	}
	return converged, failed
}

// StartMembershipReconciler 启动兜底巡检（默认 10 分钟一次，可用环境变量调整）。
func (a *API) StartMembershipReconciler(interval time.Duration) {
	if interval <= 0 {
		return
	}
	go func() {
		// 启动后先延迟一会儿，避开服务启动期的高峰
		time.Sleep(30 * time.Second)
		for {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			converged, failed := a.ReconcileMembership(ctx)
			cancel()
			if converged > 0 || failed > 0 {
				log.Printf("[gateway] 会员巡检完成：收敛 %d 个，失败 %d 个", converged, failed)
			}
			time.Sleep(interval)
		}
	}()
}
