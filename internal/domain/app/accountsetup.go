package app

import (
	"fmt"
	"slices"
	"strings"
)

// 账号初始化策略（docs/account-setup.md）。
//
// 第三方、短信、卡密这类快速登录在首次登录时直接建号：账号名由系统生成（qq_<openid>、手机号、卡面），
// 也没有密码。策略决定哪些建号方式要引导用户补设用户名与密码，以及怎么提醒：
// 第一次弹窗，之后只出横幅；用户关掉横幅后隔一段时间再出。
//
// 存在 apps.settings.accountSetup。没配置过时按默认策略：开启，第三方与短信建的号都要引导。

// AccountSetupSettingsKey 应用设置里存放策略的键。
const AccountSetupSettingsKey = "accountSetup"

// 需要初始化的建号方式。
const (
	AccountSetupSourceOAuth   = "oauth"
	AccountSetupSourceSMS     = "sms"
	AccountSetupSourceCardKey = "cardkey"
)

// 第一次提醒的方式。
const (
	AccountSetupFirstPromptDialog = "dialog" // 第一次弹窗，之后横幅
	AccountSetupFirstPromptBanner = "banner" // 一律横幅
)

// 横幅关闭后再次出现的间隔上限（小时）。
const accountSetupMaxSnoozeHours = 24 * 90

// AccountSetupSources 可选的建号方式，顺序即控制台的展示顺序。
func AccountSetupSources() []string {
	return []string{AccountSetupSourceOAuth, AccountSetupSourceSMS, AccountSetupSourceCardKey}
}

// AccountSetupPolicy 应用的账号初始化策略。
type AccountSetupPolicy struct {
	// Enabled 是否引导快速登录建的号完成初始化。关闭后客户端不再提醒，已设的不受影响。
	Enabled bool `json:"enabled"`
	// Sources 哪些建号方式要引导：oauth / sms / cardkey
	Sources []string `json:"sources"`
	// RequireAccount 要求设置用户名
	RequireAccount bool `json:"requireAccount"`
	// RequirePassword 要求设置密码
	RequirePassword bool `json:"requirePassword"`
	// FirstPrompt 第一次提醒的方式：dialog（弹窗，之后横幅）/ banner（一律横幅）
	FirstPrompt string `json:"firstPrompt"`
	// SnoozeHours 用户关掉横幅后多久再出现；0 表示横幅不能关闭
	SnoozeHours int `json:"snoozeHours"`
	// IsDefault 应用从未配置过，返回的是默认策略
	IsDefault bool `json:"isDefault"`
}

// DefaultAccountSetupPolicy 未配置时的策略。
func DefaultAccountSetupPolicy() AccountSetupPolicy {
	return AccountSetupPolicy{
		Enabled:         true,
		Sources:         []string{AccountSetupSourceOAuth, AccountSetupSourceSMS},
		RequireAccount:  true,
		RequirePassword: true,
		FirstPrompt:     AccountSetupFirstPromptDialog,
		SnoozeHours:     72,
		IsDefault:       true,
	}
}

// Covers 该建号方式是否在引导范围内。
func (p AccountSetupPolicy) Covers(source string) bool {
	return p.Enabled && slices.Contains(p.Sources, source) && (p.RequireAccount || p.RequirePassword)
}

// NormalizeAccountSetupPolicy 清洗并校验策略。
func NormalizeAccountSetupPolicy(policy AccountSetupPolicy) (AccountSetupPolicy, error) {
	sources := make([]string, 0, len(policy.Sources))
	for _, source := range policy.Sources {
		source = strings.ToLower(strings.TrimSpace(source))
		if source == "" || slices.Contains(sources, source) {
			continue
		}
		if !slices.Contains(AccountSetupSources(), source) {
			return policy, fmt.Errorf("未知的建号方式：%s", source)
		}
		sources = append(sources, source)
	}
	// 按固定顺序存，控制台与接口读到的永远一致
	slices.SortFunc(sources, func(a, b string) int {
		return slices.Index(AccountSetupSources(), a) - slices.Index(AccountSetupSources(), b)
	})
	policy.Sources = sources
	if policy.Enabled && len(sources) == 0 {
		return policy, fmt.Errorf("开启账号初始化时至少选择一种建号方式")
	}
	if policy.Enabled && !policy.RequireAccount && !policy.RequirePassword {
		return policy, fmt.Errorf("开启账号初始化时至少要求设置用户名或密码之一")
	}
	policy.FirstPrompt = strings.ToLower(strings.TrimSpace(policy.FirstPrompt))
	switch policy.FirstPrompt {
	case "":
		policy.FirstPrompt = AccountSetupFirstPromptDialog
	case AccountSetupFirstPromptDialog, AccountSetupFirstPromptBanner:
	default:
		return policy, fmt.Errorf("未知的提醒方式：%s", policy.FirstPrompt)
	}
	if policy.SnoozeHours < 0 || policy.SnoozeHours > accountSetupMaxSnoozeHours {
		return policy, fmt.Errorf("横幅再次出现的间隔应在 0 到 %d 小时之间", accountSetupMaxSnoozeHours)
	}
	policy.IsDefault = false
	return policy, nil
}

// AccountSetupPolicyFromSettings 从应用设置里读出策略；没有配置或结构不对时返回默认策略。
func AccountSetupPolicyFromSettings(settings map[string]any) AccountSetupPolicy {
	policy := DefaultAccountSetupPolicy()
	raw, ok := settings[AccountSetupSettingsKey].(map[string]any)
	if !ok {
		return policy
	}
	policy.IsDefault = false
	if value, ok := raw["enabled"].(bool); ok {
		policy.Enabled = value
	}
	if values, ok := raw["sources"].([]any); ok {
		policy.Sources = policy.Sources[:0:0]
		for _, value := range values {
			if text, ok := value.(string); ok {
				policy.Sources = append(policy.Sources, text)
			}
		}
	}
	if value, ok := raw["requireAccount"].(bool); ok {
		policy.RequireAccount = value
	}
	if value, ok := raw["requirePassword"].(bool); ok {
		policy.RequirePassword = value
	}
	if value, ok := raw["firstPrompt"].(string); ok {
		policy.FirstPrompt = value
	}
	switch value := raw["snoozeHours"].(type) {
	case float64:
		policy.SnoozeHours = int(value)
	case int:
		policy.SnoozeHours = value
	case int64:
		policy.SnoozeHours = int(value)
	}
	normalized, err := NormalizeAccountSetupPolicy(policy)
	if err != nil {
		// 存进去的策略都校验过；读出来不对只可能是被手工改坏了，退回默认最稳妥
		return DefaultAccountSetupPolicy()
	}
	return normalized
}

// ToMap 写回应用设置的形状。
func (p AccountSetupPolicy) ToMap() map[string]any {
	sources := make([]any, 0, len(p.Sources))
	for _, source := range p.Sources {
		sources = append(sources, source)
	}
	return map[string]any{
		"enabled":         p.Enabled,
		"sources":         sources,
		"requireAccount":  p.RequireAccount,
		"requirePassword": p.RequirePassword,
		"firstPrompt":     p.FirstPrompt,
		"snoozeHours":     p.SnoozeHours,
	}
}
