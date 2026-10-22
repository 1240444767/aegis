// Package devicelimit 登录设备数控制：一个账号同时能在几台设备上保持登录。
//
// 规则见 docs/device-limit.md。这里只放纯数据与纯函数（策略的规整、生效上限的推导、
// 超限时的处置方案），读写会话与数据库在 service 层的 DeviceLimitService。
//
// 「设备」不是「会话」：同一台设备上反复登录只算一台。设备按 Key 区分 ——
// 客户端上报了设备标识就用它；没有时用 UA 摘要（同一浏览器算同一台）；
// 连 UA 都没有才退回到这一次登录的刷新令牌族（每次登录各算一台）。
package devicelimit

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

// SettingsKey 应用设置（apps.settings）里存放策略的键。
const SettingsKey = "deviceLimit"

// 超限处置方式。
const (
	// OverflowKickOldest 自动下线最早登录的设备
	OverflowKickOldest = "kick_oldest"
	// OverflowKickIdle 自动下线最久没有活动（刷新令牌）的设备
	OverflowKickIdle = "kick_idle"
	// OverflowReject 拒绝这次登录，已登录的设备不受影响
	OverflowReject = "reject"
	// OverflowAsk 让用户自己选下线哪台；客户端没有声明支持时按 AskFallback 处理
	OverflowAsk = "ask"
)

// 同一台设备再次登录时旧登录的去留。
const (
	// SameDeviceReplace 新登录顶掉这台设备上原有的登录（一台设备只保留一份登录）
	SameDeviceReplace = "replace"
	// SameDeviceCoexist 保留旧登录，同一台设备上可以有多份登录，仍只算一台
	SameDeviceCoexist = "coexist"
)

// 终端类别。上限按类别配置：按具体平台配会让「iPad 算手机还是平板」这类问题变成配置项，
// 而客户端上报的平台本来就分不出平板。
const (
	ClassMobile  = "mobile"  // Android / iOS / HarmonyOS
	ClassDesktop = "desktop" // Windows / macOS / Linux
	ClassWeb     = "web"     // 浏览器
	ClassOther   = "other"   // 平台未知
)

// Classes 全部终端类别，顺序即展示顺序。
var Classes = []string{ClassMobile, ClassDesktop, ClassWeb, ClassOther}

// ClassLabel 终端类别的中文名。
func ClassLabel(class string) string {
	switch class {
	case ClassMobile:
		return "手机"
	case ClassDesktop:
		return "电脑"
	case ClassWeb:
		return "网页"
	default:
		return "其他设备"
	}
}

// ClassOf 把规整后的平台（devicedomain.NormalizePlatform 的取值）归到终端类别。
func ClassOf(platform string) string {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case "android", "ios", "harmonyos":
		return ClassMobile
	case "windows", "macos", "linux":
		return ClassDesktop
	case "web":
		return ClassWeb
	}
	return ClassOther
}

// 会员档位规则的适用对象。
const (
	AudienceTrial    = "trial"    // 试用会员
	AudienceVIP      = "vip"      // 有效会员（含试用与永久）
	AudienceLifetime = "lifetime" // 永久会员
	AudienceFeature  = "feature"  // 持有指定功能（会员套餐所含或单独解锁）
)

// 取值范围。
const (
	MaxDevicesCeiling = 100
	MaxTiers          = 10
	MaxWindowDays     = 365
	MaxNewDevices     = 100
	MaxMessageLength  = 200
)

// Policy 应用的设备数策略，存在 apps.settings.deviceLimit。
type Policy struct {
	Enabled bool `json:"enabled"`
	// MaxDevices 默认的同时登录设备上限，0 表示不限总数（仍可按类别限制）
	MaxDevices int `json:"maxDevices"`
	// ClassLimits 各终端类别的上限，缺省或 0 表示该类别不单独限制
	ClassLimits map[string]int `json:"classLimits"`
	// Overflow 超限时的处置方式
	Overflow string `json:"overflow"`
	// AskFallback 处置方式为 ask、但客户端没有声明能让用户选择时改用的方式（不能再是 ask）
	AskFallback string `json:"askFallback"`
	// SameDevice 同一台设备再次登录时旧登录的去留
	SameDevice string `json:"sameDevice"`
	// Tiers 会员档位规则，按顺序第一条命中的生效；单人例外优先于它们
	Tiers []Tier `json:"tiers"`
	// NewDevice 新设备频率限制：滚动窗口内首次出现的设备数
	NewDevice NewDeviceRule `json:"newDevice"`
	// NotifyKicked 设备被自动下线时给用户发一条站内信（账号安全分类）
	NotifyKicked bool `json:"notifyKicked"`
	// RejectMessage 拒绝登录时的提示，留空用默认文案。可用 {limit} 占位
	RejectMessage string `json:"rejectMessage"`
}

// Tier 会员档位规则：命中的用户改用这一档的上限。
type Tier struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Audience string `json:"audience"`
	// FeatureTag Audience 为 feature 时必填
	FeatureTag string `json:"featureTag,omitempty"`
	MaxDevices int    `json:"maxDevices"`
	// ClassLimits 为空时沿用默认的类别上限
	ClassLimits map[string]int `json:"classLimits,omitempty"`
}

// NewDeviceRule 滚动窗口内最多允许多少台从没登录过的设备。
type NewDeviceRule struct {
	Enabled    bool `json:"enabled"`
	WindowDays int  `json:"windowDays"`
	MaxNew     int  `json:"maxNew"`
}

// DefaultPolicy 未配置时的策略：不限制。
func DefaultPolicy() Policy {
	return Policy{
		Enabled:      false,
		MaxDevices:   3,
		ClassLimits:  map[string]int{},
		Overflow:     OverflowKickOldest,
		AskFallback:  OverflowKickOldest,
		SameDevice:   SameDeviceReplace,
		Tiers:        []Tier{},
		NewDevice:    NewDeviceRule{WindowDays: 30, MaxNew: 5},
		NotifyKicked: true,
	}
}

// Normalize 规整并校验。返回的错误文案直接给管理员看。
func (p Policy) Normalize() (Policy, error) {
	out := p
	if out.MaxDevices < 0 || out.MaxDevices > MaxDevicesCeiling {
		return out, fmt.Errorf("同时登录设备上限需要在 0–%d 之间", MaxDevicesCeiling)
	}
	limits, err := normalizeClassLimits(out.ClassLimits)
	if err != nil {
		return out, err
	}
	out.ClassLimits = limits
	switch out.Overflow {
	case "":
		out.Overflow = OverflowKickOldest
	case OverflowKickOldest, OverflowKickIdle, OverflowReject, OverflowAsk:
	default:
		return out, fmt.Errorf("不认识的超限处置方式 %q", out.Overflow)
	}
	switch out.AskFallback {
	case "":
		out.AskFallback = OverflowKickOldest
	case OverflowKickOldest, OverflowKickIdle, OverflowReject:
	default:
		return out, fmt.Errorf("客户端不支持选择设备时的处置方式只能是自动下线或拒绝登录")
	}
	switch out.SameDevice {
	case "":
		out.SameDevice = SameDeviceReplace
	case SameDeviceReplace, SameDeviceCoexist:
	default:
		return out, fmt.Errorf("不认识的同设备登录方式 %q", out.SameDevice)
	}
	if len(out.Tiers) > MaxTiers {
		return out, fmt.Errorf("会员档位规则最多 %d 条", MaxTiers)
	}
	tiers := make([]Tier, 0, len(out.Tiers))
	seen := map[string]bool{}
	for i, tier := range out.Tiers {
		normalized, err := tier.normalize(i)
		if err != nil {
			return out, err
		}
		if seen[normalized.ID] {
			normalized.ID = fmt.Sprintf("%s-%d", normalized.ID, i+1)
		}
		seen[normalized.ID] = true
		tiers = append(tiers, normalized)
	}
	out.Tiers = tiers
	if out.NewDevice.WindowDays <= 0 {
		out.NewDevice.WindowDays = 30
	}
	if out.NewDevice.WindowDays > MaxWindowDays {
		return out, fmt.Errorf("新设备统计窗口最长 %d 天", MaxWindowDays)
	}
	if out.NewDevice.MaxNew < 0 || out.NewDevice.MaxNew > MaxNewDevices {
		return out, fmt.Errorf("窗口内新设备数需要在 0–%d 之间", MaxNewDevices)
	}
	if out.NewDevice.Enabled && out.NewDevice.MaxNew == 0 {
		return out, fmt.Errorf("开启新设备限制时，窗口内允许的新设备数至少为 1")
	}
	out.RejectMessage = strings.TrimSpace(out.RejectMessage)
	if len([]rune(out.RejectMessage)) > MaxMessageLength {
		return out, fmt.Errorf("拒绝提示最长 %d 个字", MaxMessageLength)
	}
	return out, nil
}

func (t Tier) normalize(index int) (Tier, error) {
	out := t
	out.ID = strings.TrimSpace(out.ID)
	if out.ID == "" {
		out.ID = fmt.Sprintf("tier-%d", index+1)
	}
	out.Name = strings.TrimSpace(out.Name)
	out.FeatureTag = strings.TrimSpace(out.FeatureTag)
	label := out.Name
	if label == "" {
		label = fmt.Sprintf("第 %d 条档位规则", index+1)
	}
	switch out.Audience {
	case AudienceTrial, AudienceVIP, AudienceLifetime:
		out.FeatureTag = ""
	case AudienceFeature:
		if out.FeatureTag == "" {
			return out, fmt.Errorf("%s：按功能匹配时需要填写功能标识", label)
		}
	default:
		return out, fmt.Errorf("%s：不认识的适用对象 %q", label, out.Audience)
	}
	if out.Name == "" {
		out.Name = AudienceLabel(out.Audience, out.FeatureTag)
	}
	if out.MaxDevices < 0 || out.MaxDevices > MaxDevicesCeiling {
		return out, fmt.Errorf("%s：设备上限需要在 0–%d 之间", label, MaxDevicesCeiling)
	}
	limits, err := normalizeClassLimits(out.ClassLimits)
	if err != nil {
		return out, fmt.Errorf("%s：%w", label, err)
	}
	if len(limits) == 0 {
		limits = nil
	}
	out.ClassLimits = limits
	return out, nil
}

// AudienceLabel 档位适用对象的中文名。
func AudienceLabel(audience, featureTag string) string {
	switch audience {
	case AudienceTrial:
		return "试用会员"
	case AudienceVIP:
		return "会员"
	case AudienceLifetime:
		return "永久会员"
	case AudienceFeature:
		return "持有功能 " + featureTag
	}
	return audience
}

func normalizeClassLimits(in map[string]int) (map[string]int, error) {
	out := map[string]int{}
	for class, limit := range in {
		if !slices.Contains(Classes, class) {
			return nil, fmt.Errorf("不认识的终端类别 %q", class)
		}
		if limit < 0 || limit > MaxDevicesCeiling {
			return nil, fmt.Errorf("%s上限需要在 0–%d 之间", ClassLabel(class), MaxDevicesCeiling)
		}
		if limit > 0 {
			out[class] = limit
		}
	}
	return out, nil
}

// FromSettings 从应用设置读出策略。
//
// 没有 deviceLimit 键时按旧字段推导（multiDeviceLogin / multiDeviceLoginNum），
// 让升级前配过「同时在线上限」的应用行为不变。存进去的策略都校验过，读出来不对只可能被手工改坏，
// 那时按不限制处理 —— 拦错人比少拦一次代价大得多。
func FromSettings(settings map[string]any) (Policy, bool) {
	raw, ok := settings[SettingsKey]
	if !ok || raw == nil {
		return legacyPolicy(settings), false
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return DefaultPolicy(), true
	}
	policy := DefaultPolicy()
	if err := json.Unmarshal(data, &policy); err != nil {
		return DefaultPolicy(), true
	}
	normalized, err := policy.Normalize()
	if err != nil {
		return DefaultPolicy(), true
	}
	return normalized, true
}

func legacyPolicy(settings map[string]any) Policy {
	policy := DefaultPolicy()
	multi := true
	if value, ok := settings["multiDeviceLogin"].(bool); ok {
		multi = value
	}
	limit := 0
	switch value := settings["multiDeviceLoginNum"].(type) {
	case float64:
		limit = int(value)
	case int:
		limit = value
	case int64:
		limit = int(value)
	}
	switch {
	case !multi:
		policy.Enabled = true
		policy.MaxDevices = 1
	case limit > 0:
		policy.Enabled = true
		policy.MaxDevices = min(limit, MaxDevicesCeiling)
	}
	return policy
}

// ToSettings 写回应用设置的形状。
func (p Policy) ToSettings() map[string]any {
	data, _ := json.Marshal(p)
	out := map[string]any{}
	_ = json.Unmarshal(data, &out)
	return out
}

// LegacySettings 同步写回旧字段，让仍在读旧字段的地方（旧版控制台、导出）看到一致的值。
func (p Policy) LegacySettings() (multiDeviceLogin bool, multiDeviceLoginNum int) {
	if !p.Enabled {
		return true, 0
	}
	if p.MaxDevices == 1 {
		return false, 1
	}
	return true, p.MaxDevices
}

// Override 单个用户的例外，优先于会员档位与默认上限。
type Override struct {
	AppID  int64 `json:"-"`
	UserID int64 `json:"userId"`
	// Account 列表展示用，读取时联表带出
	Account string `json:"account,omitempty"`
	// Exempt 不受任何设备数限制（含新设备频率）
	Exempt      bool           `json:"exempt"`
	MaxDevices  int            `json:"maxDevices"`
	ClassLimits map[string]int `json:"classLimits"`
	Note        string         `json:"note"`
	ExpiresAt   *time.Time     `json:"expiresAt,omitempty"`
	UpdatedBy   string         `json:"updatedBy,omitempty"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
}

// OverrideInput 管理端写入例外。
type OverrideInput struct {
	Exempt      bool           `json:"exempt"`
	MaxDevices  int            `json:"maxDevices"`
	ClassLimits map[string]int `json:"classLimits"`
	Note        string         `json:"note"`
	ExpiresAt   *time.Time     `json:"expiresAt"`
}

// Normalize 校验例外。
func (in OverrideInput) Normalize(now time.Time) (OverrideInput, error) {
	out := in
	if out.MaxDevices < 0 || out.MaxDevices > MaxDevicesCeiling {
		return out, fmt.Errorf("设备上限需要在 0–%d 之间", MaxDevicesCeiling)
	}
	limits, err := normalizeClassLimits(out.ClassLimits)
	if err != nil {
		return out, err
	}
	out.ClassLimits = limits
	out.Note = strings.TrimSpace(out.Note)
	if len([]rune(out.Note)) > MaxMessageLength {
		return out, fmt.Errorf("备注最长 %d 个字", MaxMessageLength)
	}
	if out.ExpiresAt != nil && !out.ExpiresAt.After(now) {
		return out, fmt.Errorf("到期时间需要晚于现在")
	}
	return out, nil
}

// Active 例外此刻是否生效。
func (o *Override) Active(now time.Time) bool {
	return o != nil && (o.ExpiresAt == nil || o.ExpiresAt.After(now))
}

// Member 判定档位所需的会员事实。
type Member struct {
	IsVIP      bool     `json:"isVip"`
	IsLifetime bool     `json:"isLifetime"`
	IsTrial    bool     `json:"isTrial"`
	Features   []string `json:"features"`
}

// Matches 档位规则是否命中这位会员。
func (t Tier) Matches(member Member) bool {
	switch t.Audience {
	case AudienceTrial:
		return member.IsVIP && member.IsTrial
	case AudienceVIP:
		return member.IsVIP
	case AudienceLifetime:
		return member.IsLifetime
	case AudienceFeature:
		return t.FeatureTag != "" && slices.Contains(member.Features, t.FeatureTag)
	}
	return false
}

// 生效上限的来源。
const (
	SourceDisabled = "disabled"
	SourceDefault  = "default"
	SourceTier     = "tier"
	SourceOverride = "override"
	SourceExempt   = "exempt"
)

// Limits 某个用户此刻生效的上限。
type Limits struct {
	// Unlimited 不做任何限制（策略未开启或单人豁免）
	Unlimited   bool           `json:"unlimited"`
	MaxDevices  int            `json:"maxDevices"`
	ClassLimits map[string]int `json:"classLimits"`
	// Source 上限出自哪一层；SourceLabel 是给人看的说明（如「会员档位：永久会员」）
	Source      string `json:"source"`
	SourceLabel string `json:"sourceLabel"`
	TierID      string `json:"tierId,omitempty"`
	// NewDevice 是否套用新设备频率限制（豁免的用户不套用）
	NewDevice NewDeviceRule `json:"newDevice"`
}

// Resolve 推导用户此刻生效的上限：单人例外 > 会员档位（第一条命中）> 默认。
func Resolve(policy Policy, member Member, override *Override, now time.Time) Limits {
	if !policy.Enabled {
		return Limits{Unlimited: true, ClassLimits: map[string]int{}, Source: SourceDisabled, SourceLabel: "未开启设备数控制"}
	}
	if override.Active(now) {
		if override.Exempt {
			return Limits{Unlimited: true, ClassLimits: map[string]int{}, Source: SourceExempt, SourceLabel: "单人例外：不受限制"}
		}
		limits := Limits{
			MaxDevices:  override.MaxDevices,
			ClassLimits: copyLimits(override.ClassLimits),
			Source:      SourceOverride,
			SourceLabel: "单人例外",
			NewDevice:   policy.NewDevice,
		}
		if len(limits.ClassLimits) == 0 {
			limits.ClassLimits = copyLimits(policy.ClassLimits)
		}
		return limits
	}
	for _, tier := range policy.Tiers {
		if !tier.Matches(member) {
			continue
		}
		limits := Limits{
			MaxDevices:  tier.MaxDevices,
			ClassLimits: copyLimits(tier.ClassLimits),
			Source:      SourceTier,
			SourceLabel: "会员档位：" + tier.Name,
			TierID:      tier.ID,
			NewDevice:   policy.NewDevice,
		}
		if len(limits.ClassLimits) == 0 {
			limits.ClassLimits = copyLimits(policy.ClassLimits)
		}
		return limits
	}
	return Limits{
		MaxDevices:  policy.MaxDevices,
		ClassLimits: copyLimits(policy.ClassLimits),
		Source:      SourceDefault,
		SourceLabel: "默认上限",
		NewDevice:   policy.NewDevice,
	}
}

func copyLimits(in map[string]int) map[string]int {
	out := make(map[string]int, len(in))
	for k, v := range in {
		if v > 0 {
			out[k] = v
		}
	}
	return out
}

// NeedsMembership 有会员档位规则时才需要查会员事实，否则省掉一次查询。
func (p Policy) NeedsMembership() bool {
	return p.Enabled && len(p.Tiers) > 0
}
