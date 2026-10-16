package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"

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
		out = append(out, item)
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
