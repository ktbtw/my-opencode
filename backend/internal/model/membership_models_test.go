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
