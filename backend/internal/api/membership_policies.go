package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"relay-server/internal/model"
)

// 会员模型分级：控制台配置（DB）→ 收敛器 → 网关钥匙 + 设备 provider + 别名同步。
//
// 配置优先级：控制台（数据库）> 环境变量 CC_GATEWAY_POLICIES > 内置默认。
// 之所以保留环境变量兜底，是为了没配控制台时也能直接跑（以及升级过渡）。

// tierPolicy 解析某会员等级当前生效的策略。
func (a *API) tierPolicy(ctx context.Context, tier string) model.MembershipModelPolicy {
	normalized := model.NormalizeMembershipTier(tier)
	if policies, err := a.store.ListMembershipTierPolicies(ctx); err == nil {
		for _, policy := range policies {
			if model.NormalizeMembershipTier(policy.Tier) == normalized {
				return policy.ToPolicy()
			}
		}
	}
	return loadGatewayConfig().policyFor(normalized)
}

// membershipGatewayOverview 汇总网关侧信息，供控制台展示（可用模型、现有别名）。
type membershipGatewayOverview struct {
	Configured       bool              `json:"configured"`
	BaseURL          string            `json:"base_url,omitempty"`
	ProxyURL         string            `json:"proxy_url,omitempty"`
	DeviceBaseURL    string            `json:"device_base_url,omitempty"`
	SecretConfigured bool              `json:"secret_configured"`
	AllowedModels    []string          `json:"allowed_models,omitempty"`
	Aliases          map[string]string `json:"aliases,omitempty"`
	Error            string            `json:"error,omitempty"`
}

func (a *API) membershipGatewayOverview(ctx context.Context, cfg gatewayConfig) membershipGatewayOverview {
	overview := membershipGatewayOverview{
		Configured:       cfg.enabled(),
		BaseURL:          cfg.BaseURL,
		ProxyURL:         cfg.ProxyURL,
		DeviceBaseURL:    cfg.deviceBaseURL(),
		SecretConfigured: cfg.Secret != "",
	}
	if !cfg.enabled() {
		return overview
	}
	raw, err := a.gatewayRequest(ctx, cfg, http.MethodGet, "/admin/settings", nil)
	if err != nil {
		overview.Error = err.Error()
		return overview
	}
	var parsed struct {
		AllowedModels []string          `json:"allowed_models"`
		ModelAliases  map[string]string `json:"model_aliases"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		overview.Error = fmt.Sprintf("解析网关设置失败：%v", err)
		return overview
	}
	sort.Strings(parsed.AllowedModels)
	overview.AllowedModels = parsed.AllowedModels
	overview.Aliases = parsed.ModelAliases
	return overview
}

// GetMembershipPolicies 返回控制台要编辑的内容 + 网关概况。
func (a *API) GetMembershipPolicies(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	cfg := loadGatewayConfig()
	items, err := a.store.ListMembershipTierPolicies(r.Context())
	if err != nil {
		write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	overview := a.membershipGatewayOverview(r.Context(), cfg)
	if len(items) == 0 {
		items = a.defaultTierPolicies(overview)
	}
	write(w, http.StatusOK, map[string]any{
		"items":                   items,
		"gateway":                 a.membershipGatewayOverview(r.Context(), cfg),
		"env_fallback_configured": len(cfg.Policies) > 0,
	})
}

// defaultTierPolicies 在控制台还没配过时，用环境变量/默认值给出可编辑的初始内容。
func (a *API) defaultTierPolicies(overview membershipGatewayOverview) []model.MembershipTierPolicy {
	cfg := loadGatewayConfig()
	out := make([]model.MembershipTierPolicy, 0, 2)
	for _, tier := range []string{model.MembershipTierPlus, model.MembershipTierPro} {
		policy := cfg.policyFor(tier)
		item := model.MembershipTierPolicy{
			Tier:        tier,
			ProviderID:  policy.ProviderID,
			DisplayName: policy.DisplayName,
			Limits:      policy.Limits,
			Enabled:     len(policy.Models) > 0,
		}
		for _, id := range policy.Models {
			entry := model.TierModelEntry{RealModel: id}
			// 环境变量里配的可能已经是别名：以网关别名表为准反查真实模型
			if real, ok := overview.Aliases[id]; ok && real != "" {
				entry.Alias, entry.RealModel = id, real
			} else if real, ok := policy.RealModels[id]; ok && real != "" {
				entry.Alias, entry.RealModel = id, real
			}
			if name, ok := policy.ModelNames[id]; ok {
				entry.DisplayName = name
			}
			item.Entries = append(item.Entries, entry)
		}
		out = append(out, item.Normalize())
	}
	return out
}

// SaveMembershipPolicies 保存控制台配置：落库 → 同步网关别名 → 异步重新收敛。
func (a *API) SaveMembershipPolicies(w http.ResponseWriter, r *http.Request) {
	if !a.requireAdmin(w, r) {
		return
	}
	var req struct {
		Items []model.MembershipTierPolicy `json:"items"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		write(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if len(req.Items) == 0 {
		write(w, http.StatusBadRequest, map[string]string{"error": "items 不能为空"})
		return
	}
	cfg := loadGatewayConfig()
	overview := a.membershipGatewayOverview(r.Context(), cfg)
	allowed := map[string]bool{}
	for _, id := range overview.AllowedModels {
		allowed[id] = true
	}

	warnings := make([]string, 0, 4)
	normalized := make([]model.MembershipTierPolicy, 0, len(req.Items))
	aliases := map[string]string{}
	for _, item := range req.Items {
		item = item.Normalize()
		if item.Tier != model.MembershipTierPlus && item.Tier != model.MembershipTierPro && item.Tier != model.MembershipTierFree {
			write(w, http.StatusBadRequest, map[string]string{"error": "会员等级不合法：" + item.Tier})
			return
		}
		for _, entry := range item.Entries {
			if len(allowed) > 0 && !allowed[entry.RealModel] {
				warnings = append(warnings, fmt.Sprintf("%s：模型 %s 不在网关全局白名单里，会员用不了（需要先在网关加入白名单）", item.Tier, entry.RealModel))
			}
			if entry.Alias != "" && entry.Alias != entry.RealModel {
				if other, exists := aliases[entry.Alias]; exists && other != entry.RealModel {
					warnings = append(warnings, fmt.Sprintf("别名 %s 同时指向多个模型，已按最后一个处理", entry.Alias))
				}
				aliases[entry.Alias] = entry.RealModel
			}
		}
		normalized = append(normalized, item)
	}

	for _, item := range normalized {
		if err := a.store.UpsertMembershipTierPolicy(r.Context(), item); err != nil {
			write(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}

	aliasSynced := false
	if cfg.enabled() {
		if err := a.syncGatewayAliases(r.Context(), cfg, aliases); err != nil {
			warnings = append(warnings, "别名同步到网关失败："+err.Error())
		} else {
			aliasSynced = true
		}
	}

	// 保存后自动把该等级的会员重新收敛（异步，零人工）
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), gatewayConvergeBudget)
		defer cancel()
		converged, failed := a.ReconcileMembership(ctx)
		if converged > 0 || failed > 0 {
			log.Printf("[gateway] 策略更新后巡检：收敛 %d 个，失败 %d 个", converged, failed)
		}
	}()

	write(w, http.StatusOK, map[string]any{
		"items":        normalized,
		"alias_synced": aliasSynced,
		"aliases":      aliases,
		"warnings":     warnings,
	})
}

// syncGatewayAliases 把「别名 → 真实模型」写进网关（别名由平台统一维护，整表覆盖）。
func (a *API) syncGatewayAliases(ctx context.Context, cfg gatewayConfig, aliases map[string]string) error {
	payload := map[string]any{"model_aliases": aliases}
	_, err := a.gatewayRequest(ctx, cfg, http.MethodPut, "/admin/settings", payload)
	return err
}

// membershipUsageModel 是额度页要展示的一个模型。
//
// 只给"给人看的名字"：别名 id 与底层真实模型都不下发给用户端——
// 模型名已经白标，用户不需要、也不应该看到这两个内部标识。
type membershipUsageModel struct {
	Name string `json:"name"`
}

// membershipUsageDevice 是额度页展示的一台设备（只给用户看得懂的名字与状态）。
type membershipUsageDevice struct {
	Name  string `json:"name"`
	Ready bool   `json:"ready"`
}

// membershipUsageCounters 是额度页展示的用量数字（网关按钥匙记账的原始字段）。
type membershipUsageCounters struct {
	Requests         int64 `json:"requests"`
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	CostMicro        int64 `json:"cost_micro"`
	DayRequests      int64 `json:"day_requests"`
	DayTokens        int64 `json:"day_tokens"`
	DayCostMicro     int64 `json:"day_cost_micro"`
}

// GetMembershipUsage 返回当前用户的会员额度状态（网关侧实时用量 + 设备下发情况）。
//
// App 的「我的额度」页用它渲染：等级、已用/剩余、日额度、可用模型、重置时间。
func (a *API) GetMembershipUsage(w http.ResponseWriter, r *http.Request) {
	operator, ok := a.currentOperator(r)
	if !ok {
		write(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	ctx := r.Context()
	tier := model.MembershipTierFree
	if resolved, err := a.store.GetOperatorMembershipTier(operator.ID); err == nil && strings.TrimSpace(resolved) != "" {
		tier = resolved
	}
	tier = model.NormalizeMembershipTier(tier)
	policy := a.tierPolicy(ctx, tier)

	resp := map[string]any{
		"tier":        tier,
		"is_member":   model.IsMemberTier(tier),
		"provider_id": policy.ProviderID,
		"group_name":  policy.DisplayName,
		"active":      false,
		"limits":      policy.Limits,
		"used":        membershipUsageCounters{},
	}
	models := make([]membershipUsageModel, 0, len(policy.Models))
	for _, id := range policy.Models {
		// 没配展示名时用别名（也是我们自己起的名字），仍然不暴露真实模型
		models = append(models, membershipUsageModel{Name: policy.DisplayNameFor(id)})
	}
	resp["models"] = models

	record, err := a.store.GetOperatorGatewayKey(ctx, operator.ID)
	if err != nil || record == nil || record.KeyID == "" {
		write(w, http.StatusOK, resp)
		return
	}
	resp["active"] = record.State == model.GatewayKeyStateActive
	resp["state"] = record.State
	resp["key_last4"] = record.KeyLast4
	// 机器 ID 是内部标识，不下发给用户端：换成设备名（没有名字就用主机名）
	names := map[string]string{}
	// 用带用户自定义显示名的设备列表（"我的macmini" 这类），而不是原始主机名
	machines, err := a.listDevicesForOperator(ctx, operator.ID)
	if err != nil {
		machines = a.broker.ListMachines(operator.ID)
	}
	for _, machine := range machines {
		label := strings.TrimSpace(machine.DisplayName)
		if label == "" {
			label = strings.TrimSpace(machine.Hostname)
		}
		if label != "" {
			names[machine.MachineID] = label
		}
	}
	devices := make([]membershipUsageDevice, 0, len(record.Machines))
	for machineID, status := range record.Machines {
		label := names[machineID]
		if label == "" {
			continue // 认不出名字就不展示，避免把内部 ID 泄露给用户
		}
		devices = append(devices, membershipUsageDevice{Name: label, Ready: status == "ok"})
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].Name < devices[j].Name })
	resp["devices"] = devices

	// 实时用量：直接读网关的钥匙记录（网关按钥匙记账，含跨日字段）。
	cfg := loadGatewayConfig()
	if cfg.enabled() {
		if used, ok := a.gatewayKeyUsage(ctx, cfg, record.KeyID); ok {
			resp["used"] = used
			if policy.Limits.MaxCostMicro > 0 {
				remaining := policy.Limits.MaxCostMicro - used.CostMicro
				if remaining < 0 {
					remaining = 0
				}
				resp["remaining_cost_micro"] = remaining
			}
			if policy.Limits.DailyCostMicro > 0 {
				dailyRemaining := policy.Limits.DailyCostMicro - used.DayCostMicro
				if dailyRemaining < 0 {
					dailyRemaining = 0
				}
				resp["remaining_daily_cost_micro"] = dailyRemaining
			}
			if policy.Limits.MaxTokens > 0 {
				totalTokens := used.PromptTokens + used.CompletionTokens
				remainingTokens := policy.Limits.MaxTokens - totalTokens
				if remainingTokens < 0 {
					remainingTokens = 0
				}
				resp["remaining_tokens"] = remainingTokens
			}
			if policy.Limits.DailyTokens > 0 {
				dailyTokens := policy.Limits.DailyTokens - used.DayTokens
				if dailyTokens < 0 {
					dailyTokens = 0
				}
				resp["remaining_daily_tokens"] = dailyTokens
			}
		}
	}
	// 网关按 UTC 日翻滚额度
	now := time.Now().UTC()
	next := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC).Add(24 * time.Hour)
	resp["reset_at"] = next.Format(time.RFC3339)
	write(w, http.StatusOK, resp)
}

// gatewayKeyUsage 读某把钥匙在网关上的累计/当日用量。
func (a *API) gatewayKeyUsage(ctx context.Context, cfg gatewayConfig, keyID string) (membershipUsageCounters, bool) {
	raw, err := a.gatewayRequest(ctx, cfg, http.MethodGet, "/admin/keys", nil)
	if err != nil {
		return membershipUsageCounters{}, false
	}
	var parsed struct {
		Keys []struct {
			ID    string `json:"id"`
			Usage struct {
				Requests         int64 `json:"requests"`
				PromptTokens     int64 `json:"prompt_tokens"`
				CompletionTokens int64 `json:"completion_tokens"`
				CostMicro        int64 `json:"cost_micro"`
				DayRequests      int64 `json:"day_requests"`
				DayTokens        int64 `json:"day_tokens"`
				DayCostMicro     int64 `json:"day_cost_micro"`
			} `json:"usage"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return membershipUsageCounters{}, false
	}
	for _, key := range parsed.Keys {
		if key.ID != keyID {
			continue
		}
		return membershipUsageCounters{
			Requests:         key.Usage.Requests,
			PromptTokens:     key.Usage.PromptTokens,
			CompletionTokens: key.Usage.CompletionTokens,
			CostMicro:        key.Usage.CostMicro,
			DayRequests:      key.Usage.DayRequests,
			DayTokens:        key.Usage.DayTokens,
			DayCostMicro:     key.Usage.DayCostMicro,
		}, true
	}
	return membershipUsageCounters{}, false
}
