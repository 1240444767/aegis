package app

import (
	"fmt"
	"slices"
	"strings"
)

// 自动签到的开放策略。
//
// 用户自己的「开不开、几点签」记在个人设置（user_settings.autoSign）里；
// 「这个应用里谁有资格用」是应用的事，由管理员配置，存在 apps.settings.autoSign。
// 两层分开放：管理员收紧范围时不去改任何人的个人设置，资格恢复后原来的时刻接着生效。

// AutoSignSettingsKey 应用设置里存放策略的键。
const AutoSignSettingsKey = "autoSign"

// 开放对象。
const (
	AutoSignAudienceAll      = "all"      // 所有用户
	AutoSignAudienceVip      = "vip"      // 有效会员（含永久会员）
	AutoSignAudienceLifetime = "lifetime" // 仅永久会员
	AutoSignAudienceFeature  = "feature"  // 持有指定功能（会员套餐所含或功能解锁）
)

// 不可用的原因。空串表示可用。
const (
	AutoSignReasonDisabled         = "disabled"
	AutoSignReasonVipRequired      = "vip_required"
	AutoSignReasonTrialExcluded    = "trial_excluded"
	AutoSignReasonLifetimeRequired = "lifetime_required"
	AutoSignReasonFeatureRequired  = "feature_required"
)

// AutoSignAudiences 开放对象，顺序即控制台的展示顺序。
func AutoSignAudiences() []string {
	return []string{AutoSignAudienceAll, AutoSignAudienceVip, AutoSignAudienceLifetime, AutoSignAudienceFeature}
}

// AutoSignPolicy 应用的自动签到开放策略。
type AutoSignPolicy struct {
	// Enabled 应用是否开放自动签到。关闭时所有人的自动签到暂停，个人设置保留。
	Enabled bool `json:"enabled"`
	// Audience 开放对象：all / vip / lifetime / feature
	Audience string `json:"audience"`
	// IncludeTrial 开放对象为 vip 时，试用会员是否算在内
	IncludeTrial bool `json:"includeTrial"`
	// FeatureTag 开放对象为 feature 时要求持有的功能标识
	FeatureTag string `json:"featureTag,omitempty"`
	// IsDefault 应用从未配置过，返回的是默认策略
	IsDefault bool `json:"isDefault"`
}

// DefaultAutoSignPolicy 未配置时的策略：不开放。自动签到是管理员决定给谁的权益。
func DefaultAutoSignPolicy() AutoSignPolicy {
	return AutoSignPolicy{Enabled: false, Audience: AutoSignAudienceAll, IncludeTrial: true, IsDefault: true}
}

// AutoSignMember 判定所需的会员事实（取自会员权益判定的结论）。
type AutoSignMember struct {
	IsVIP      bool
	IsLifetime bool
	IsTrial    bool
	Features   []string
}

// EvaluateAutoSign 判定一个用户能否使用自动签到。纯函数，返回空串即可用。
//
// 设置接口拒绝开启、调度器跳过执行、状态接口给出的原因，都走这一个判据。
func EvaluateAutoSign(policy AutoSignPolicy, member AutoSignMember) string {
	if !policy.Enabled {
		return AutoSignReasonDisabled
	}
	switch policy.Audience {
	case AutoSignAudienceVip:
		if !member.IsVIP {
			return AutoSignReasonVipRequired
		}
		if member.IsTrial && !policy.IncludeTrial {
			return AutoSignReasonTrialExcluded
		}
	case AutoSignAudienceLifetime:
		if !member.IsLifetime {
			return AutoSignReasonLifetimeRequired
		}
	case AutoSignAudienceFeature:
		if policy.FeatureTag == "" || !slices.Contains(member.Features, policy.FeatureTag) {
			return AutoSignReasonFeatureRequired
		}
	}
	return ""
}

// NeedsMembership 判定是否要查会员事实。开放给所有人时不必多打一次库。
func (p AutoSignPolicy) NeedsMembership() bool {
	return p.Enabled && p.Audience != AutoSignAudienceAll
}

// AutoSignReasonMessage 原因的用户可读说法。featureName 为空时退回功能标识。
func AutoSignReasonMessage(reason string, policy AutoSignPolicy, featureName string) string {
	switch reason {
	case "":
		return ""
	case AutoSignReasonDisabled:
		return "自动签到暂未开放"
	case AutoSignReasonVipRequired:
		return "自动签到仅限会员使用"
	case AutoSignReasonTrialExcluded:
		return "试用会员暂不可使用自动签到"
	case AutoSignReasonLifetimeRequired:
		return "自动签到仅限永久会员使用"
	case AutoSignReasonFeatureRequired:
		name := strings.TrimSpace(featureName)
		if name == "" {
			name = policy.FeatureTag
		}
		return fmt.Sprintf("自动签到需要开通「%s」", name)
	default:
		return "自动签到暂不可用"
	}
}

// NormalizeAutoSignPolicy 清洗并校验策略。功能标识是否已登记由服务层核对。
func NormalizeAutoSignPolicy(policy AutoSignPolicy) (AutoSignPolicy, error) {
	policy.Audience = strings.TrimSpace(policy.Audience)
	if policy.Audience == "" {
		policy.Audience = AutoSignAudienceAll
	}
	if !slices.Contains(AutoSignAudiences(), policy.Audience) {
		return policy, fmt.Errorf("未知的开放对象：%s", policy.Audience)
	}
	policy.FeatureTag = strings.ToLower(strings.TrimSpace(policy.FeatureTag))
	if policy.Audience == AutoSignAudienceFeature {
		if policy.FeatureTag == "" {
			return policy, fmt.Errorf("开放对象为指定功能时需要选择功能")
		}
	} else {
		policy.FeatureTag = ""
	}
	policy.IsDefault = false
	return policy, nil
}

// AutoSignPolicyFromSettings 从应用设置里读出策略；没有配置或结构不对时返回默认策略。
func AutoSignPolicyFromSettings(settings map[string]any) AutoSignPolicy {
	policy := DefaultAutoSignPolicy()
	raw, ok := settings[AutoSignSettingsKey].(map[string]any)
	if !ok {
		return policy
	}
	policy.IsDefault = false
	if value, ok := raw["enabled"].(bool); ok {
		policy.Enabled = value
	}
	if value, ok := raw["audience"].(string); ok {
		policy.Audience = value
	}
	if value, ok := raw["includeTrial"].(bool); ok {
		policy.IncludeTrial = value
	}
	if value, ok := raw["featureTag"].(string); ok {
		policy.FeatureTag = value
	}
	normalized, err := NormalizeAutoSignPolicy(policy)
	if err != nil {
		// 存进去的策略都校验过；读出来不对只可能是被手工改坏了，按不开放处理最稳妥
		return DefaultAutoSignPolicy()
	}
	return normalized
}

// ToMap 写回应用设置的形状。
func (p AutoSignPolicy) ToMap() map[string]any {
	out := map[string]any{
		"enabled":      p.Enabled,
		"audience":     p.Audience,
		"includeTrial": p.IncludeTrial,
	}
	if p.FeatureTag != "" {
		out["featureTag"] = p.FeatureTag
	}
	return out
}
