package rewardedad

import (
	"fmt"
	"slices"
	"strings"

	cardkeydomain "aegis/internal/domain/cardkey"
	vipdomain "aegis/internal/domain/vip"
)

// 已拥有的权益怎么办。
//
// 永久会员看一次「领 1 天会员」的广告，照常记一段限时会员什么也没给他：功能取两条线的并集，
// 永久那条线已经全包了。同样落空的还有：永久会员领他已包含的套餐、「功能时长」（会员使用时免扣，
// 见 featuretime.Exempt，永久会员的余额永远用不上）、已永久解锁某功能的人再领这个功能的时长。
//
// 场景的 Lifetime 决定怎么处理（在结算事务里锁住用户行之后判定，客户端只看结果）：
//
//   - convert 按价值折算（默认）：会员每天 PerVipDay、功能时长每小时 PerFeatureHour，
//     折成 Target 那一档（积分 / AI 额度 / 经验 / 抽奖次数）；套餐附赠的积分照发
//   - replace 改发一组固定权益（Rewards），与折了几项无关
//   - hide    这个场景不出现在他的列表里；万一仍有观看到达（列表是旧的），按 convert 折算
//   - grant   照常发放（旧行为）
//
// 积分、经验、抽奖次数、AI 额度从来不算已拥有；不是已拥有的那几项照常发放，只折已拥有的部分。
const (
	LifetimeConvert = "convert"
	LifetimeReplace = "replace"
	LifetimeHide    = "hide"
	LifetimeGrant   = "grant"
)

// 已拥有的原因。
const (
	OwnedLifetime = vipdomain.OwnedLifetime // 永久会员已包含
	OwnedFeature  = vipdomain.OwnedFeature  // 已永久解锁这个功能
)

// 默认折算与上界。
const (
	DefaultPerVipDay      = 10
	DefaultPerFeatureHour = 10
	MaxPerVipDay          = 100_000
	MaxPerFeatureHour     = 100_000
)

// convertTargets 折算能选的去处：都是不会再「已拥有」的东西。顺序即控制台的展示顺序。
var convertTargets = []string{
	cardkeydomain.RewardIntegral,
	RewardAICredit,
	cardkeydomain.RewardExperience,
	cardkeydomain.RewardLotteryDraws,
}

// ConvertTargets 折算能选的去处。
func ConvertTargets() []string { return slices.Clone(convertTargets) }

// LifetimePolicies 处理方式，顺序即控制台的展示顺序。
func LifetimePolicies() []string {
	return []string{LifetimeConvert, LifetimeReplace, LifetimeHide, LifetimeGrant}
}

// SceneLifetime 永久会员（或已永久拥有某功能的人）看这个场景时，已拥有的权益怎么办。
type SceneLifetime struct {
	Policy string `json:"policy"`
	// Target convert 折成哪一档，见 ConvertTargets
	Target string `json:"target,omitempty"`
	// PerVipDay 会员每 1 天折多少（会员天数按天数，会员套餐按套餐天数）
	PerVipDay int64 `json:"perVipDay,omitempty"`
	// PerFeatureHour 功能时长每 60 分钟折多少，不足一小时按比例、向上取整
	PerFeatureHour int64 `json:"perFeatureHour,omitempty"`
	// Rewards replace 改发的权益
	Rewards []Reward `json:"rewards,omitempty"`
}

// LifetimePolicySpec 控制台上一种处理方式的自述。
type LifetimePolicySpec struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Hint  string `json:"hint"`
}

// LifetimePolicyCatalog 处理方式目录。
func LifetimePolicyCatalog() []LifetimePolicySpec {
	return []LifetimePolicySpec{
		{LifetimeConvert, "按价值折算", "会员按天、功能时长按小时折成积分、AI 额度、经验或抽奖次数，其余权益照发"},
		{LifetimeReplace, "改发固定权益", "已拥有的权益不再发放，改发一组固定的积分、经验、抽奖次数或 AI 额度"},
		{LifetimeHide, "不展示该场景", "对已拥有的用户隐藏这个场景；仍有观看到达时按价值折算"},
		{LifetimeGrant, "照常发放", "仍记一段会员或功能时长（永久会员实际拿不到新东西）"},
	}
}

// Coverage 一个人已经永久拥有什么。
type Coverage struct {
	Lifetime         bool
	LifetimeFeatures []string
	// PermanentUnlocks 已永久解锁的功能（功能解锁那条线，与会员无关）
	PermanentUnlocks []string
}

// Owned 这项权益对这个人是不是空的；是的话返回原因。
//
//   - 会员天数：带场景会员权益里的功能，这些功能都已在永久开通里才算（多出来的功能是实打实的升级）
//   - 会员套餐：与购买入口同一个判据 vip.LifetimeIncludes
//   - 功能时长：会员使用时免扣，永久会员的余额永远用不上；已永久解锁该功能的人同理
func (c Coverage) Owned(scene Scene, reward Reward, plans map[int64]vipdomain.Plan) (string, bool) {
	holdings := vipdomain.Holdings{Lifetime: c.Lifetime, LifetimeFeatures: c.LifetimeFeatures, PermanentUnlocks: c.PermanentUnlocks}
	switch reward.Type {
	case cardkeydomain.RewardVipDays:
		if holdings.CoversMembership(scene.Membership.Features) {
			return OwnedLifetime, true
		}
	case cardkeydomain.RewardVipPlan:
		plan, ok := plans[reward.RefID]
		if ok && holdings.CoversPlan(plan) {
			return OwnedLifetime, true
		}
	case RewardFeatureMinutes:
		return holdings.CoversFeatureTime(strings.ToLower(strings.TrimSpace(reward.Feature)))
	}
	return "", false
}

// Conversion 一次观看里「场景配的是 A，发的是 B」的说明，存进观看记录。
type Conversion struct {
	Policy string `json:"policy"`
	// Reason lifetime（永久会员已包含）/ feature（已永久解锁这个功能）
	Reason string `json:"reason"`
	// Owned 已拥有、因而没有发放的权益
	Owned      []Reward `json:"owned"`
	OwnedLabel string   `json:"ownedLabel"`
	// Rewards 改发的权益（折算结果为 0 时为空）
	Rewards []Reward `json:"rewards"`
	Label   string   `json:"label"`
	// Message 给用户看的一句话
	Message string `json:"message"`
}

// Grant 实际要发的一项。Converted 标出它是折算来的（流水的说明不同）。
type Grant struct {
	Reward    Reward
	Converted bool
}

// Outcome 一个场景对一个人的结论。
type Outcome struct {
	Grants     []Grant
	Conversion *Conversion
	// Hidden 场景对他隐藏（hide 且有已拥有的权益）
	Hidden bool
}

// Labels 摘要里要用到的展示名。
type Labels struct {
	Plans    map[int64]string
	Features map[string]string
}

// LifetimeOrDefault 处理方式与折算参数，未设置时按默认值补齐。
func (s Scene) LifetimeOrDefault() SceneLifetime {
	rule := s.Lifetime
	if rule.Policy == "" {
		rule.Policy = LifetimeConvert
	}
	if rule.Target == "" {
		rule.Target = cardkeydomain.RewardIntegral
	}
	if rule.PerVipDay == 0 && rule.PerFeatureHour == 0 {
		rule.PerVipDay = DefaultPerVipDay
		rule.PerFeatureHour = DefaultPerFeatureHour
	}
	return rule
}

// Resolve 这个场景对这个人发什么。plans 要包含场景里「会员套餐」引用的套餐（判已拥有、取天数与附赠积分）。
//
// 纯函数：结算事务与客户端列表用同一个，两边说的一定一样。
func Resolve(scene Scene, coverage Coverage, plans map[int64]vipdomain.Plan, labels Labels) Outcome {
	rewards := NormalizeRewards(scene.Rewards)
	rule := scene.LifetimeOrDefault()
	out := Outcome{Grants: make([]Grant, 0, len(rewards))}

	var owned []Reward
	reason := ""
	for _, reward := range rewards {
		why, ok := coverage.Owned(scene, reward, plans)
		if !ok || rule.Policy == LifetimeGrant {
			out.Grants = append(out.Grants, Grant{Reward: reward})
			continue
		}
		owned = append(owned, reward)
		// 两种原因都有时说「永久会员」：那是更大的那一个
		if reason == "" || why == OwnedLifetime {
			reason = why
		}
	}
	if len(owned) == 0 {
		return out
	}
	out.Hidden = rule.Policy == LifetimeHide

	var swapped []Reward
	if rule.Policy == LifetimeReplace {
		swapped = NormalizeRewards(rule.Rewards)
	} else {
		swapped = convertOwned(owned, rule, plans)
	}
	for _, reward := range swapped {
		out.Grants = append(out.Grants, Grant{Reward: reward, Converted: true})
	}

	conversion := &Conversion{
		Policy: rule.Policy, Reason: reason, Owned: owned, Rewards: swapped,
		OwnedLabel: DescribeRewards(owned, labels.Plans, labels.Features),
		Label:      DescribeRewards(swapped, labels.Plans, labels.Features),
	}
	if conversion.Rewards == nil {
		conversion.Rewards = []Reward{}
	}
	conversion.Message = ConversionMessage(reason, conversion.OwnedLabel, conversion.Label)
	out.Conversion = conversion
	return out
}

// convertOwned 按价值折算已拥有的权益：同一去处合成一项，各自不超过该档的上限。
// 套餐附赠的积分不是「已拥有」的东西，照发（并到积分里）。
func convertOwned(owned []Reward, rule SceneLifetime, plans map[int64]vipdomain.Plan) []Reward {
	var value, bonus int64
	for _, reward := range owned {
		switch reward.Type {
		case cardkeydomain.RewardVipDays:
			value += reward.Amount * rule.PerVipDay
		case cardkeydomain.RewardVipPlan:
			plan := plans[reward.RefID]
			value += int64(plan.DurationDays) * rule.PerVipDay
			bonus += plan.BonusIntegral
		case RewardFeatureMinutes:
			// 按分钟比例向上取整：看一次 30 分钟、每小时 10 → 5
			value += (reward.Amount*rule.PerFeatureHour + 59) / 60
		}
	}
	amounts := map[string]int64{}
	if value > 0 {
		amounts[rule.Target] += value
	}
	if bonus > 0 {
		amounts[cardkeydomain.RewardIntegral] += bonus
	}
	out := make([]Reward, 0, len(amounts))
	for _, target := range convertTargets {
		amount, ok := amounts[target]
		if !ok {
			continue
		}
		if spec, found := FindRewardSpec(target); found && spec.Max > 0 {
			amount = min(amount, spec.Max)
		}
		out = append(out, Reward{Type: target, Amount: amount})
	}
	return out
}

// ConversionMessage 给用户看的说明。
func ConversionMessage(reason, ownedLabel, label string) string {
	who := "你已是永久会员"
	if reason == OwnedFeature {
		who = "你已永久拥有该功能"
	}
	if label == "" {
		return fmt.Sprintf("%s，「%s」已包含在内，无需重复发放", who, ownedLabel)
	}
	return fmt.Sprintf("%s，「%s」已折算为%s", who, ownedLabel, label)
}

// ResultConversion 发放结果里的说明行（不是一项权益）：已拥有的权益折算或改发了什么。
//
// 放在结果里是为了不升级客户端：已发布的 App 只把 results 逐行展示，不认 conversion 字段。
const ResultConversion = "conversion"

// ConversionResultLabel 说明行的短名。
func ConversionResultLabel(c *Conversion) string {
	if c.Reason == OwnedFeature {
		return "已永久拥有"
	}
	return "永久会员"
}

// ClientLifetime 客户端场景上的提示：对你来说这个场景会怎样。
type ClientLifetime struct {
	Policy string `json:"policy"`
	Reason string `json:"reason"`
	// OwnedLabel 已拥有、不会再发的部分
	OwnedLabel string `json:"ownedLabel"`
	// Rewards 改发的权益
	Rewards []Reward `json:"rewards"`
	Label   string   `json:"label"`
	Message string   `json:"message"`
}

// ClientHint 把结论转成客户端提示；没有已拥有的权益时为 nil。
func (o Outcome) ClientHint() *ClientLifetime {
	if o.Conversion == nil {
		return nil
	}
	c := o.Conversion
	message := "你已是永久会员"
	if c.Reason == OwnedFeature {
		message = "你已永久拥有该功能"
	}
	if c.Label == "" {
		message += "，「" + c.OwnedLabel + "」已包含在内"
	} else {
		message += "，「" + c.OwnedLabel + "」将折算为" + c.Label
	}
	return &ClientLifetime{
		Policy: c.Policy, Reason: c.Reason, OwnedLabel: c.OwnedLabel,
		Rewards: c.Rewards, Label: c.Label, Message: message,
	}
}

// GrantedRewards 实际要发的权益（客户端摘要用）。
func (o Outcome) GrantedRewards() []Reward {
	out := make([]Reward, 0, len(o.Grants))
	for _, grant := range o.Grants {
		out = append(out, grant.Reward)
	}
	return out
}

// normalizeLifetime 清洗处理方式：空的按默认值补齐，只留当前处理方式用得上的字段。
func normalizeLifetime(rule SceneLifetime) SceneLifetime {
	rule.Policy = strings.ToLower(strings.TrimSpace(rule.Policy))
	rule.Target = strings.ToLower(strings.TrimSpace(rule.Target))
	if rule.Policy == "" {
		rule.Policy = LifetimeConvert
	}
	switch rule.Policy {
	case LifetimeConvert, LifetimeHide:
		if rule.Target == "" {
			rule.Target = cardkeydomain.RewardIntegral
		}
		if rule.PerVipDay == 0 && rule.PerFeatureHour == 0 {
			rule.PerVipDay = DefaultPerVipDay
			rule.PerFeatureHour = DefaultPerFeatureHour
		}
		rule.Rewards = nil
	case LifetimeReplace:
		rule.Target, rule.PerVipDay, rule.PerFeatureHour = "", 0, 0
		rule.Rewards = NormalizeRewards(rule.Rewards)
	case LifetimeGrant:
		rule.Target, rule.PerVipDay, rule.PerFeatureHour, rule.Rewards = "", 0, 0, nil
	}
	return rule
}

// validateLifetime 校验处理方式与折算参数（按清洗后的形态校验：未设置的按默认值）。
func validateLifetime(rule SceneLifetime) error {
	rule = normalizeLifetime(rule)
	if !slices.Contains(LifetimePolicies(), rule.Policy) {
		return fmt.Errorf("未知的已拥有权益处理方式：%s", rule.Policy)
	}
	switch rule.Policy {
	case LifetimeConvert, LifetimeHide:
		if !slices.Contains(convertTargets, rule.Target) {
			return fmt.Errorf("折算只能折成积分、AI 额度、经验或抽奖次数")
		}
		if rule.PerVipDay < 0 || rule.PerVipDay > MaxPerVipDay {
			return fmt.Errorf("会员每天折算需要在 0–%d 之间", MaxPerVipDay)
		}
		if rule.PerFeatureHour < 0 || rule.PerFeatureHour > MaxPerFeatureHour {
			return fmt.Errorf("功能时长每小时折算需要在 0–%d 之间", MaxPerFeatureHour)
		}
	case LifetimeReplace:
		if len(rule.Rewards) == 0 {
			return fmt.Errorf("改发固定权益时至少配置一项")
		}
		for _, reward := range rule.Rewards {
			if !slices.Contains(convertTargets, reward.Type) {
				return fmt.Errorf("改发只能是积分、AI 额度、经验或抽奖次数")
			}
		}
		if err := ValidateRewards(rule.Rewards); err != nil {
			return fmt.Errorf("改发的权益：%w", err)
		}
	}
	return nil
}

// Ownable 这个场景有没有可能对某人「已拥有」的权益（控制台据此决定要不要展示处理方式）。
func (s Scene) Ownable() bool {
	for _, reward := range s.Rewards {
		switch reward.Type {
		case cardkeydomain.RewardVipDays, cardkeydomain.RewardVipPlan, RewardFeatureMinutes:
			return true
		}
	}
	return false
}
