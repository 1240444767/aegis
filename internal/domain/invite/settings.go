package invite

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"
)

// DefaultConfig 没保存过配置时的默认值；控制台第一次打开时展示的也是它。
func DefaultConfig() Config {
	return Config{
		RegisterField:     FieldOptional,
		FieldLabel:        "邀请码",
		FieldPlaceholder:  "选填",
		BindWindowHours:   DefaultBindWindowHours,
		CodeLength:        DefaultCodeLength,
		CodeCharset:       CharsetAlnum,
		InviterAudience:   AudienceAll,
		InviteeRewards:    []Reward{},
		InviterRewards:    []Reward{},
		Milestones:        []Milestone{},
		InviterTrigger:    TriggerBind,
		TriggerSignInDays: 1,
		NotifyInviter:     true,
		DeviceLimit:       1,
		ShareTemplate:     "邀请你使用 {app}，注册时填写邀请码 {code} 即可领取奖励。{link}",
	}
}

// RegisterFields / Charsets / Audiences / Triggers 枚举，顺序即控制台的展示顺序。
func RegisterFields() []string { return []string{FieldHidden, FieldOptional, FieldRequired} }
func Charsets() []string       { return []string{CharsetAlnum, CharsetDigits, CharsetLetters} }
func Audiences() []string      { return []string{AudienceAll, AudienceVip} }
func Triggers() []string       { return []string{TriggerBind, TriggerSignIn} }

// NormalizeConfig 清洗：去空白、补默认值、按目录重排奖励、阶梯按人数升序。
func NormalizeConfig(cfg Config) Config {
	defaults := DefaultConfig()
	cfg.RegisterField = strings.TrimSpace(cfg.RegisterField)
	if cfg.RegisterField == "" {
		cfg.RegisterField = defaults.RegisterField
	}
	cfg.FieldLabel = strings.TrimSpace(cfg.FieldLabel)
	if cfg.FieldLabel == "" {
		cfg.FieldLabel = defaults.FieldLabel
	}
	cfg.FieldPlaceholder = strings.TrimSpace(cfg.FieldPlaceholder)
	if cfg.CodeLength == 0 {
		cfg.CodeLength = defaults.CodeLength
	}
	cfg.CodeCharset = strings.TrimSpace(cfg.CodeCharset)
	if cfg.CodeCharset == "" {
		cfg.CodeCharset = defaults.CodeCharset
	}
	cfg.CodePrefix = NormalizeCode(cfg.CodePrefix)
	cfg.InviterAudience = strings.TrimSpace(cfg.InviterAudience)
	if cfg.InviterAudience == "" {
		cfg.InviterAudience = defaults.InviterAudience
	}
	cfg.InviterTrigger = strings.TrimSpace(cfg.InviterTrigger)
	if cfg.InviterTrigger == "" {
		cfg.InviterTrigger = defaults.InviterTrigger
	}
	if cfg.InviterTrigger == TriggerSignIn && cfg.TriggerSignInDays == 0 {
		cfg.TriggerSignInDays = 1
	}
	if cfg.InviterTrigger == TriggerBind {
		cfg.TriggerSignInDays = 0
	}
	cfg.InviteeRewards = NormalizeRewards(cfg.InviteeRewards)
	cfg.InviterRewards = NormalizeRewards(cfg.InviterRewards)
	milestones := make([]Milestone, 0, len(cfg.Milestones))
	for _, milestone := range cfg.Milestones {
		milestones = append(milestones, Milestone{
			Count:   milestone.Count,
			Title:   strings.TrimSpace(milestone.Title),
			Rewards: NormalizeRewards(milestone.Rewards),
		})
	}
	slices.SortStableFunc(milestones, func(a, b Milestone) int { return a.Count - b.Count })
	cfg.Milestones = milestones
	cfg.RulesText = strings.TrimSpace(cfg.RulesText)
	cfg.ShareTemplate = strings.TrimSpace(cfg.ShareTemplate)
	cfg.LandingURL = strings.TrimSpace(cfg.LandingURL)
	return cfg
}

// ValidateConfig 校验清洗后的配置（引用的套餐与功能由服务层核对）。
func ValidateConfig(cfg Config) error {
	switch {
	case !slices.Contains(RegisterFields(), cfg.RegisterField):
		return fmt.Errorf("未知的注册表单形态：%s", cfg.RegisterField)
	case utf8.RuneCountInString(cfg.FieldLabel) > MaxFieldLabelRunes:
		return fmt.Errorf("字段名称不能超过 %d 字", MaxFieldLabelRunes)
	case utf8.RuneCountInString(cfg.FieldPlaceholder) > MaxPlaceholderRunes:
		return fmt.Errorf("输入提示不能超过 %d 字", MaxPlaceholderRunes)
	case cfg.BindWindowHours < 0 || cfg.BindWindowHours > MaxBindWindowHours:
		return fmt.Errorf("补填时限需要在 0–%d 小时之间", MaxBindWindowHours)
	case !slices.Contains(Charsets(), cfg.CodeCharset):
		return fmt.Errorf("未知的邀请码字符集：%s", cfg.CodeCharset)
	case cfg.CodeLength < minLengthFor(cfg.CodeCharset) || cfg.CodeLength > MaxCodeLength:
		return fmt.Errorf("邀请码长度需要在 %d–%d 位之间", minLengthFor(cfg.CodeCharset), MaxCodeLength)
	case len(cfg.CodePrefix) > MaxPrefixLength:
		return fmt.Errorf("邀请码前缀不能超过 %d 位", MaxPrefixLength)
	case cfg.CodePrefix != "" && !ValidCodeShape(cfg.CodePrefix):
		return fmt.Errorf("邀请码前缀只能使用字母与数字")
	case !slices.Contains(Audiences(), cfg.InviterAudience):
		return fmt.Errorf("未知的邀请人范围：%s", cfg.InviterAudience)
	case cfg.InviterMinLevel < 0 || cfg.InviterMinLevel > MaxMinLevel:
		return fmt.Errorf("邀请人等级门槛需要在 0–%d 之间", MaxMinLevel)
	case !slices.Contains(Triggers(), cfg.InviterTrigger):
		return fmt.Errorf("未知的发放时机：%s", cfg.InviterTrigger)
	case cfg.InviterTrigger == TriggerSignIn && (cfg.TriggerSignInDays < 1 || cfg.TriggerSignInDays > MaxTriggerSignInDays):
		return fmt.Errorf("签到天数需要在 1–%d 之间", MaxTriggerSignInDays)
	case cfg.InviterDailyLimit < 0 || cfg.InviterDailyLimit > MaxInviterLimit:
		return fmt.Errorf("每日奖励上限需要在 0–%d 之间", MaxInviterLimit)
	case cfg.InviterTotalLimit < 0 || cfg.InviterTotalLimit > MaxInviterLimit:
		return fmt.Errorf("累计奖励上限需要在 0–%d 之间", MaxInviterLimit)
	case cfg.DeviceLimit < 0 || cfg.DeviceLimit > MaxDeviceLimit:
		return fmt.Errorf("单台设备的填写次数需要在 0–%d 之间", MaxDeviceLimit)
	case cfg.IPDailyLimit < 0 || cfg.IPDailyLimit > MaxIPDailyLimit:
		return fmt.Errorf("单个 IP 每日填写次数需要在 0–%d 之间", MaxIPDailyLimit)
	case utf8.RuneCountInString(cfg.RulesText) > MaxRulesRunes:
		return fmt.Errorf("活动规则不能超过 %d 字", MaxRulesRunes)
	case utf8.RuneCountInString(cfg.ShareTemplate) > MaxShareRunes:
		return fmt.Errorf("分享文案不能超过 %d 字", MaxShareRunes)
	case len(cfg.LandingURL) > MaxLandingURLLength:
		return fmt.Errorf("落地页地址不能超过 %d 个字符", MaxLandingURLLength)
	}
	if cfg.LandingURL != "" {
		parsed, err := url.Parse(strings.ReplaceAll(cfg.LandingURL, "{code}", "CODE"))
		if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
			return fmt.Errorf("落地页需要是 http(s) 地址")
		}
	}
	if err := ValidateRewards("被邀请人奖励", cfg.InviteeRewards); err != nil {
		return err
	}
	if err := ValidateRewards("邀请人奖励", cfg.InviterRewards); err != nil {
		return err
	}
	if len(cfg.Milestones) > MaxMilestones {
		return fmt.Errorf("阶梯奖励最多 %d 档", MaxMilestones)
	}
	previous := 0
	for _, milestone := range cfg.Milestones {
		if milestone.Count < 1 || milestone.Count > MaxMilestoneCount {
			return fmt.Errorf("阶梯人数需要在 1–%d 之间", MaxMilestoneCount)
		}
		if milestone.Count == previous {
			return fmt.Errorf("阶梯人数 %d 重复配置了两次", milestone.Count)
		}
		previous = milestone.Count
		if utf8.RuneCountInString(milestone.Title) > MaxMilestoneTitle {
			return fmt.Errorf("阶梯名称不能超过 %d 字", MaxMilestoneTitle)
		}
		if len(milestone.Rewards) == 0 {
			return fmt.Errorf("邀请满 %d 人的阶梯没有配置奖励", milestone.Count)
		}
		if err := ValidateRewards(fmt.Sprintf("邀请满 %d 人的奖励", milestone.Count), milestone.Rewards); err != nil {
			return err
		}
	}
	return nil
}

// AllRewards 配置里出现过的全部奖励（核对引用用）。
func (cfg Config) AllRewards() []Reward {
	out := append([]Reward{}, cfg.InviteeRewards...)
	out = append(out, cfg.InviterRewards...)
	for _, milestone := range cfg.Milestones {
		out = append(out, milestone.Rewards...)
	}
	return out
}

// HasInviterRewards 邀请人这边有没有任何东西可发（每次的奖励或阶梯）。
func (cfg Config) HasInviterRewards() bool {
	return len(cfg.InviterRewards) > 0 || len(cfg.Milestones) > 0
}

// Capability /config 下发的邀请能力。关闭时字段一律隐藏。
func (s Settings) Capability() Capability {
	capability := Capability{
		Enabled:         s.Enabled,
		Field:           FieldHidden,
		Label:           s.FieldLabel,
		Placeholder:     s.FieldPlaceholder,
		MaxLength:       MaxStoredCodeLength,
		BindWindowHours: s.BindWindowHours,
	}
	if capability.Label == "" {
		capability.Label = DefaultConfig().FieldLabel
	}
	if s.Enabled {
		capability.Field = s.RegisterField
	}
	return capability
}

func minLengthFor(charset string) int {
	if charset == CharsetDigits {
		return MinDigitsCodeLength
	}
	return MinCodeLength
}
