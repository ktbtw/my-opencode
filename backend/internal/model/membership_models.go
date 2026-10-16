package model

import (
	"sort"
	"strings"
)

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
	// MaxTokens 是会员套餐的总量 token 上限（输入+输出累计）；0 表示不限。
	MaxTokens      int64 `json:"max_tokens,omitempty"`
	DailyRequests  int64 `json:"daily_requests,omitempty"`
	DailyTokens    int64 `json:"daily_tokens,omitempty"`
	DailyCostMicro int64 `json:"daily_cost_micro,omitempty"`
	MaxCostMicro   int64 `json:"max_cost_micro,omitempty"`
}

// MembershipModelPolicy 描述某个会员等级在网关侧的模型包与额度包。
type MembershipModelPolicy struct {
	Tier        string   `json:"tier"`
	IsMember    bool     `json:"is_member"`
	ProviderID  string   `json:"provider_id,omitempty"`
	DisplayName string   `json:"display_name,omitempty"`
	Models      []string `json:"models,omitempty"`
	// RealModels 是「对外 id（别名）→ 真实模型」：用于把别名同步到网关。
	RealModels map[string]string `json:"real_models,omitempty"`
	// ModelNames 是「模型 id → 展示名」：id 必须是网关真实模型（用于请求与鉴权），
	// 展示名随便起，App 的模型列表显示的就是它。
	ModelNames map[string]string `json:"model_names,omitempty"`
	Limits     GatewayLimits     `json:"limits,omitempty"`
	// Presets 按对外模型名（别名）索引：随下发把上下文/最大输出/多模态/思考强度写进设备配置。
	Presets        map[string]ModelPreset `json:"presets,omitempty"`
	DailyCostMicro int64                  `json:"daily_cost_micro,omitempty"` // 兼容写法，与 Limits.DailyCostMicro 取较大者
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

// DisplayNameFor 返回某模型的展示名（没配就用 id）。
func (p MembershipModelPolicy) DisplayNameFor(modelID string) string {
	if name, ok := p.ModelNames[modelID]; ok && strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name)
	}
	return modelID
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
	names := make(map[string]string, len(p.ModelNames))
	for id, name := range p.ModelNames {
		id = strings.TrimSpace(id)
		name = strings.TrimSpace(name)
		if id == "" || name == "" {
			continue
		}
		names[id] = name
	}
	p.ModelNames = names
	return p
}

// OperatorGatewayKey 是平台侧保存的「期望状态 + 网关钥匙」，每个用户一行。
type OperatorGatewayKey struct {
	OperatorID  int64         `json:"operator_id"`
	DesiredTier string        `json:"desired_tier"`
	State       string        `json:"state"` // provisioning|active|suspended|failed
	KeyID       string        `json:"key_id,omitempty"`
	KeyLast4    string        `json:"key_last4,omitempty"`
	KeyCipher   string        `json:"-"` // 明文钥匙密文，只在服务端内部流转
	Models      []string      `json:"models,omitempty"`
	Limits      GatewayLimits `json:"limits,omitempty"`
	Provider    string        `json:"provider,omitempty"`
	// DeviceBaseURL 是上次下发到设备的推理地址；变化时需要先清 provider 再下发，
	// 否则 launcher 只在 api 字段为空时才写，会留下旧地址（真机实测踩过）。
	DeviceBaseURL string            `json:"device_base_url,omitempty"`
	Machines      map[string]string `json:"machines,omitempty"` // machine_id → 下发结果（ok/错误摘要）
	LastError     string            `json:"last_error,omitempty"`
	UpdatedAt     string            `json:"updated_at,omitempty"`
}

// 网关钥匙状态取值。
const (
	GatewayKeyStateProvisioning = "provisioning"
	GatewayKeyStateActive       = "active"
	GatewayKeyStateSuspended    = "suspended"
	GatewayKeyStateFailed       = "failed"
)

// TierModelEntry 是控制台里编辑的一行：真实模型 → 对外别名 / 展示名。
// ModelPreset 是会员模型的参数预设：控制台可调，随下发写入设备配置。
type ModelPreset struct {
	InputModalities  []string `json:"input_modalities,omitempty"`
	OutputModalities []string `json:"output_modalities,omitempty"`
	ContextLimit     int64    `json:"context_limit,omitempty"`
	OutputLimit      int64    `json:"output_limit,omitempty"`
	// ReasoningLevels 是"思考强度"可选档位（多选）：low / medium / high。
	// 空数组 = 不开启思考；单档位 = 固定强度；多档 = 用户可在 App 里切换。
	ReasoningLevels []string `json:"reasoning_levels,omitempty"`
	// Reasoning 兼容旧的单档位写法，归一化时并入 ReasoningLevels。
	Reasoning string `json:"reasoning,omitempty"`
}

// Empty 表示这个预设什么都没配，可整条丢掉。
func (p ModelPreset) Empty() bool {
	return len(p.InputModalities) == 0 && len(p.OutputModalities) == 0 &&
		p.ContextLimit == 0 && p.OutputLimit == 0 &&
		len(p.ReasoningLevels) == 0 && p.Reasoning == ""
}

// Normalize 收敛预设：去掉空项、未知档位按 off 处理。
func (p ModelPreset) Normalize() ModelPreset {
	p.InputModalities = normalizeModalities(p.InputModalities)
	p.OutputModalities = normalizeModalities(p.OutputModalities)
	if p.ContextLimit < 0 {
		p.ContextLimit = 0
	}
	if p.OutputLimit < 0 {
		p.OutputLimit = 0
	}
	// 思考强度：多档位，合并旧写法后去重、按低→高排序
	levels := append([]string{}, p.ReasoningLevels...)
	if single := strings.ToLower(strings.TrimSpace(p.Reasoning)); single != "" && single != "off" {
		levels = append(levels, single)
	}
	seen := map[string]bool{}
	p.ReasoningLevels = nil
	for _, level := range []string{"low", "medium", "high"} {
		for _, item := range levels {
			if strings.ToLower(strings.TrimSpace(item)) == level {
				if !seen[level] {
					seen[level] = true
					p.ReasoningLevels = append(p.ReasoningLevels, level)
				}
			}
		}
	}
	p.Reasoning = ""
	return p
}

// ReasoningLevelNames 把档位翻译成控制台/日志用的中文名。
func ReasoningLevelNames(levels []string) []string {
	names := make([]string, 0, len(levels))
	for _, level := range levels {
		switch level {
		case "low":
			names = append(names, "低")
		case "medium":
			names = append(names, "中")
		case "high":
			names = append(names, "高")
		}
	}
	return names
}

func normalizeModalities(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		switch value {
		case "text", "image", "audio", "video":
		default:
			continue
		}
		if seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

type TierModelEntry struct {
	RealModel   string       `json:"real_model"`
	Alias       string       `json:"alias,omitempty"`
	DisplayName string       `json:"display_name,omitempty"`
	Preset      *ModelPreset `json:"preset,omitempty"`
}

// 额度口径：控制台可自由选择按 Token、按金额，或两者都启用（谁先到谁生效）。
const (
	QuotaModeToken = "token"
	QuotaModeCost  = "cost"
	QuotaModeBoth  = "both"
)

// NormalizeQuotaMode 收敛额度口径；留空时按已填的字段推断，避免老配置失效。
func NormalizeQuotaMode(raw string, limits GatewayLimits) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case QuotaModeToken:
		return QuotaModeToken
	case QuotaModeCost:
		return QuotaModeCost
	case QuotaModeBoth:
		return QuotaModeBoth
	}
	hasToken := limits.DailyTokens > 0 || limits.MaxTokens > 0
	hasCost := limits.DailyCostMicro > 0 || limits.MaxCostMicro > 0
	switch {
	case hasToken && hasCost:
		return QuotaModeBoth
	case hasCost:
		return QuotaModeCost
	default:
		return QuotaModeToken
	}
}

// MembershipTierPolicy 是控制台上可编辑的等级策略（一个等级一行配置）。
type MembershipTierPolicy struct {
	Tier        string           `json:"tier"`
	ProviderID  string           `json:"provider_id,omitempty"`
	DisplayName string           `json:"display_name,omitempty"`
	Entries     []TierModelEntry `json:"entries,omitempty"`
	Limits      GatewayLimits    `json:"limits,omitempty"`
	// QuotaMode 决定上面哪些额度真正生效：token / cost / both。
	// 两套数字都保留在 Limits 里，切换口径不用重填。
	QuotaMode string `json:"quota_mode,omitempty"`
	Enabled   bool   `json:"enabled"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// ToPolicy 把控制台配置转成收敛用的策略：
// 对外用别名（没填别名就用真实模型名），并记录别名→真实的映射供网关同步。
func (t MembershipTierPolicy) ToPolicy() MembershipModelPolicy {
	policy := MembershipModelPolicy{
		Tier:        t.Tier,
		ProviderID:  t.ProviderID,
		DisplayName: t.DisplayName,
		Limits:      t.EffectiveLimits(),
		RealModels:  map[string]string{},
		ModelNames:  map[string]string{},
	}
	if !t.Enabled {
		return policy.Normalize()
	}
	presets := map[string]ModelPreset{}
	for _, entry := range t.Entries {
		real := strings.TrimSpace(entry.RealModel)
		if real == "" {
			continue
		}
		id := strings.TrimSpace(entry.Alias)
		if id == "" {
			id = real
		}
		policy.Models = append(policy.Models, id)
		if entry.Preset != nil {
			if preset := entry.Preset.Normalize(); !preset.Empty() {
				presets[id] = preset
			}
		}
		if name := strings.TrimSpace(entry.DisplayName); name != "" {
			policy.ModelNames[id] = name
		}
		if id != real {
			policy.RealModels[id] = real
		}
	}
	if len(presets) > 0 {
		policy.Presets = presets
	}
	return policy.Normalize()
}

// EffectiveLimits 按额度口径裁掉不生效的那一套：未选中的维度置 0（0 = 网关侧不限）。
func (t MembershipTierPolicy) EffectiveLimits() GatewayLimits {
	limits := t.Limits
	switch NormalizeQuotaMode(t.QuotaMode, t.Limits) {
	case QuotaModeToken:
		limits.DailyCostMicro = 0
		limits.MaxCostMicro = 0
	case QuotaModeCost:
		limits.DailyTokens = 0
		limits.MaxTokens = 0
	}
	return limits
}

// NormalizeTierPolicy 收敛控制台输入。
func (t MembershipTierPolicy) Normalize() MembershipTierPolicy {
	t.Tier = NormalizeMembershipTier(t.Tier)
	t.ProviderID = strings.TrimSpace(t.ProviderID)
	t.DisplayName = strings.TrimSpace(t.DisplayName)
	entries := make([]TierModelEntry, 0, len(t.Entries))
	seen := map[string]bool{}
	for _, entry := range t.Entries {
		entry.RealModel = strings.TrimSpace(entry.RealModel)
		entry.Alias = strings.TrimSpace(entry.Alias)
		entry.DisplayName = strings.TrimSpace(entry.DisplayName)
		if entry.RealModel == "" {
			continue
		}
		key := entry.Alias + "\x00" + entry.RealModel
		if seen[key] {
			continue
		}
		seen[key] = true
		entries = append(entries, entry)
	}
	t.Entries = entries
	if t.ProviderID == "" {
		if t.Tier == MembershipTierPro {
			t.ProviderID = GatewayProviderPro
		} else {
			t.ProviderID = GatewayProviderPlus
		}
	}
	if t.DisplayName == "" {
		t.DisplayName = "会员专属 · " + strings.ToUpper(t.Tier)
	}
	t.QuotaMode = NormalizeQuotaMode(t.QuotaMode, t.Limits)
	return t
}
