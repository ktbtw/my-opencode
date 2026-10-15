package model

import "strings"

// 会员等级 → 反代网关的「模型包 + 额度包」策略。
//
// 分工（见 docs/反代会员分级联动方案.md）：
//   - 网关（cmdproxy/gateway）是账本与门禁：一把虚拟钥匙 = 一个模型包 + 一个额度包，
//     请求时强校验模型、超限拒绝、逐条记账；
//   - 平台持有期望状态（等级），并自动建/改钥匙、把专属 provider 下发到设备。
//
// 默认策略刻意留空 Models：没配置就等于该等级不开通专属模型，
// 避免把不存在于网关的模型写进设备配置。真实模型包通过环境变量
// CC_GATEWAY_POLICIES（JSON 覆盖）在部署时给出。
const (
	// GatewayProviderPlus / GatewayProviderPro 是下发到设备的 provider id。
	GatewayProviderPlus = "cc-plus"
	GatewayProviderPro  = "cc-pro"
)

// GatewayLimits 对应网关 key 的 limits 字段（额度单位：微单位，1e-6 货币）。
type GatewayLimits struct {
	DailyRequests  int64 `json:"daily_requests,omitempty"`
	DailyTokens    int64 `json:"daily_tokens,omitempty"`
	DailyCostMicro int64 `json:"daily_cost_micro,omitempty"`
	MaxCostMicro   int64 `json:"max_cost_micro,omitempty"`
}

// MembershipModelPolicy 描述某个会员等级在网关侧的模型包与额度包。
type MembershipModelPolicy struct {
	Tier           string        `json:"tier"`
	IsMember       bool          `json:"is_member"`
	ProviderID     string        `json:"provider_id,omitempty"`
	DisplayName    string        `json:"display_name,omitempty"`
	Models         []string      `json:"models,omitempty"`
	Limits         GatewayLimits `json:"limits,omitempty"`
	DailyCostMicro int64         `json:"daily_cost_micro,omitempty"` // 兼容写法，与 Limits.DailyCostMicro 取较大者
}

// ModelPolicyForTier 返回等级对应的默认策略（未配置模型包时不开通专属模型）。
func ModelPolicyForTier(raw string) MembershipModelPolicy {
	switch NormalizeMembershipTier(raw) {
	case MembershipTierPro:
		return MembershipModelPolicy{
			Tier:        MembershipTierPro,
			IsMember:    true,
			ProviderID:  GatewayProviderPro,
			DisplayName: "会员专属 · Pro",
		}
	case MembershipTierPlus:
		return MembershipModelPolicy{
			Tier:        MembershipTierPlus,
			IsMember:    true,
			ProviderID:  GatewayProviderPlus,
			DisplayName: "会员专属 · Plus",
		}
	default:
		return MembershipModelPolicy{Tier: MembershipTierFree}
	}
}

// GatewayEnabled 表示该策略是否真的要走网关钥匙：有模型包才算开通。
// 刻意不要求 IsMember —— 等级只决定“给了什么模型包”，
// 于是同一个机制也支持给普通用户开公共模型包。
func (p MembershipModelPolicy) GatewayEnabled() bool {
	return len(p.Models) > 0 && strings.TrimSpace(p.ProviderID) != ""
}

// Normalize 收敛策略里的空字段，保证落库与下发一致。
func (p MembershipModelPolicy) Normalize() MembershipModelPolicy {
	p.Tier = NormalizeMembershipTier(p.Tier)
	// 等级是权威：环境变量里配置的策略不必再带 is_member 字段。
	p.IsMember = IsMemberTier(p.Tier)
	if strings.TrimSpace(p.ProviderID) == "" {
		if p.Tier == MembershipTierPro {
			p.ProviderID = GatewayProviderPro
		} else {
			p.ProviderID = GatewayProviderPlus
		}
	}
	if strings.TrimSpace(p.DisplayName) == "" {
		p.DisplayName = "会员专属 · " + strings.ToUpper(p.Tier)
	}
	if p.DailyCostMicro > p.Limits.DailyCostMicro {
		p.Limits.DailyCostMicro = p.DailyCostMicro
	}
	models := make([]string, 0, len(p.Models))
	seen := map[string]bool{}
	for _, m := range p.Models {
		m = strings.TrimSpace(m)
		if m == "" || seen[m] {
			continue
		}
		seen[m] = true
		models = append(models, m)
	}
	p.Models = models
	return p
}

// OperatorGatewayKey 是平台侧保存的「期望状态 + 网关钥匙」，每个用户一行。
type OperatorGatewayKey struct {
	OperatorID  int64             `json:"operator_id"`
	DesiredTier string            `json:"desired_tier"`
	State       string            `json:"state"` // provisioning|active|suspended|failed
	KeyID       string            `json:"key_id,omitempty"`
	KeyLast4    string            `json:"key_last4,omitempty"`
	KeyCipher   string            `json:"-"` // 明文钥匙密文，只在服务端内部流转
	Models      []string          `json:"models,omitempty"`
	Limits      GatewayLimits     `json:"limits,omitempty"`
	Provider    string            `json:"provider,omitempty"`
	Machines    map[string]string `json:"machines,omitempty"` // machine_id → 下发结果（ok/错误摘要）
	LastError   string            `json:"last_error,omitempty"`
	UpdatedAt   string            `json:"updated_at,omitempty"`
}

// 网关钥匙状态取值。
const (
	GatewayKeyStateProvisioning = "provisioning"
	GatewayKeyStateActive       = "active"
	GatewayKeyStateSuspended    = "suspended"
	GatewayKeyStateFailed       = "failed"
)
