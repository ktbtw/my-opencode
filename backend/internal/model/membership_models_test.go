package model

import "testing"

// 控制台策略 → 收敛策略：对外用别名，别名映射交给网关。
func TestTierPolicyToPolicyUsesAliasAndDisplayName(t *testing.T) {
	policy := MembershipTierPolicy{
		Tier:        "pro",
		ProviderID:  GatewayProviderPro,
		DisplayName: "高级专线",
		Enabled:     true,
		Entries: []TierModelEntry{
			{RealModel: "deepseek-v4.1-flash-free", Alias: "cc-pro-flash", DisplayName: "高级专线 · DeepSeek Flash"},
			{RealModel: "claude-sonnet-5"}, // 不填别名就用真实名
			{RealModel: ""},                // 空行忽略
		},
		Limits: GatewayLimits{DailyCostMicro: 8_000_000},
	}.Normalize()

	got := policy.ToPolicy()
	if !got.GatewayEnabled() {
		t.Fatalf("应视为开通：%+v", got)
	}
	if len(got.Models) != 2 || got.Models[0] != "cc-pro-flash" || got.Models[1] != "claude-sonnet-5" {
		t.Fatalf("模型包应对外用别名：%v", got.Models)
	}
	if got.DisplayNameFor("cc-pro-flash") != "高级专线 · DeepSeek Flash" {
		t.Fatalf("展示名没带上：%v", got.ModelNames)
	}
	if got.DisplayNameFor("claude-sonnet-5") != "claude-sonnet-5" {
		t.Fatalf("未配展示名应回落模型名：%v", got.ModelNames)
	}
	if got.RealModels["cc-pro-flash"] != "deepseek-v4.1-flash-free" {
		t.Fatalf("应记录别名映射：%v", got.RealModels)
	}
	if _, ok := got.RealModels["claude-sonnet-5"]; ok {
		t.Fatalf("真实名入包时不应产生别名映射：%v", got.RealModels)
	}
}

// 未启用的等级等于不发放。
func TestTierPolicyDisabledMeansNoModels(t *testing.T) {
	policy := MembershipTierPolicy{
		Tier:    "plus",
		Enabled: false,
		Entries: []TierModelEntry{{RealModel: "deepseek-v4.1-flash-free", Alias: "cc-plus-flash"}},
	}.Normalize().ToPolicy()
	if policy.GatewayEnabled() {
		t.Fatalf("未启用不该发放：%+v", policy)
	}
}

// 额度口径：只生效选中的那一套，未选中的置 0（网关侧 0 = 不限）。
func TestTierPolicyQuotaModeSelectsWhichLimitsApply(t *testing.T) {
	base := MembershipTierPolicy{
		Tier:    "pro",
		Enabled: true,
		Entries: []TierModelEntry{{RealModel: "deepseek-v4.1-flash-free", Alias: "cc-pro-flash"}},
		Limits: GatewayLimits{
			DailyTokens:    300_000_000,
			DailyCostMicro: 360_000_000,
			MaxCostMicro:   10_800_000_000,
		},
	}

	tokenOnly := base
	tokenOnly.QuotaMode = QuotaModeToken
	limits := tokenOnly.EffectiveLimits()
	if limits.DailyTokens != 300_000_000 || limits.DailyCostMicro != 0 || limits.MaxCostMicro != 0 {
		t.Fatalf("token 口径下只应保留 token 额度：%+v", limits)
	}

	costOnly := base
	costOnly.QuotaMode = QuotaModeCost
	limits = costOnly.EffectiveLimits()
	if limits.DailyTokens != 0 || limits.DailyCostMicro == 0 || limits.MaxCostMicro == 0 {
		t.Fatalf("金额口径下只应保留金额额度：%+v", limits)
	}

	both := base
	both.QuotaMode = QuotaModeBoth
	limits = both.EffectiveLimits()
	if limits.DailyTokens == 0 || limits.DailyCostMicro == 0 || limits.MaxCostMicro == 0 {
		t.Fatalf("两个都启用时都应保留：%+v", limits)
	}

	// 留空时按已填字段推断（老配置兼容）
	inferred := base.Normalize()
	if inferred.QuotaMode != QuotaModeBoth {
		t.Fatalf("两套都填应推断为 both：%s", inferred.QuotaMode)
	}
	onlyTokens := base
	onlyTokens.Limits.DailyCostMicro, onlyTokens.Limits.MaxCostMicro = 0, 0
	if got := onlyTokens.Normalize().QuotaMode; got != QuotaModeToken {
		t.Fatalf("只填 token 应推断为 token：%s", got)
	}
}
