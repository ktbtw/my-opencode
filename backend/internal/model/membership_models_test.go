package model

import (
	"strings"
	"testing"
)

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

// 参数预设：归一化 + 映射到 policy（按别名索引）。
func TestModelPresetNormalizeAndPolicyMapping(t *testing.T) {
	preset := ModelPreset{
		InputModalities:  []string{"IMAGE", "text", "text", "weird"},
		OutputModalities: []string{"text"},
		ContextLimit:     128000,
		OutputLimit:      8192,
		Reasoning:        "HIGH",
	}.Normalize()
	if len(preset.InputModalities) != 2 || preset.InputModalities[0] != "image" || preset.InputModalities[1] != "text" {
		t.Fatalf("多模态应去重排序并过滤未知项：%+v", preset.InputModalities)
	}
	// 档位现在统一进 ReasoningLevels（旧的单档写法会被并入）
	if len(preset.ReasoningLevels) != 1 || preset.ReasoningLevels[0] != "high" {
		t.Fatalf("思考强度应归一成 high 档：%+v", preset.ReasoningLevels)
	}
	if len((ModelPreset{Reasoning: "whatever"}).Normalize().ReasoningLevels) != 0 {
		t.Fatal("未知档位应被丢弃")
	}
	// 全套七档：顺序固定为 none→max
	all := ModelPreset{ReasoningLevels: []string{"max", "low", "none", "xhigh", "minimal", "high", "medium"}}.Normalize()
	want := []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}
	if strings.Join(all.ReasoningLevels, ",") != strings.Join(want, ",") {
		t.Fatalf("档位顺序/集合不对：%v", all.ReasoningLevels)
	}
	if labels := ReasoningLevelNames(all.ReasoningLevels); labels[0] != "关闭" || labels[6] != "最大" {
		t.Fatalf("中文名应与 App 一致：%v", labels)
	}

	tier := MembershipTierPolicy{
		Tier:    "pro",
		Enabled: true,
		Entries: []TierModelEntry{{
			RealModel:   "deepseek-v4.1-flash-free",
			Alias:       "cc-pro-flash",
			DisplayName: "高级专线",
			Preset:      &preset,
		}},
	}
	policy := tier.ToPolicy()
	got, ok := policy.Presets["cc-pro-flash"]
	if !ok {
		t.Fatalf("预设应按别名索引：%+v", policy.Presets)
	}
	if got.ContextLimit != 128000 || got.OutputLimit != 8192 ||
		len(got.ReasoningLevels) != 1 || got.ReasoningLevels[0] != "high" {
		t.Fatalf("预设内容不对：%+v", got)
	}
	// 空预设不该出现
	empty := MembershipTierPolicy{Tier: "plus", Enabled: true, Entries: []TierModelEntry{{RealModel: "m", Alias: "a", Preset: &ModelPreset{}}}}
	if len(empty.ToPolicy().Presets) != 0 {
		t.Fatal("空预设应被丢弃")
	}
}
