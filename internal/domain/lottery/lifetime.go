package lottery

import (
	"fmt"
	"math"
	"slices"

	"aegis/internal/domain/pointstore"
	vipdomain "aegis/internal/domain/vip"
)

// 已拥有的奖品怎么办。
//
// 永久会员抽中「会员 7 天」，照常记一段限时会员什么也没给他：功能取的是两条线的并集，
// 永久那条线已经全包了。同样的还有：永久会员抽中他已有功能的套餐、已永久解锁某功能的人
// 再抽中这个功能。这些奖品对这个人是「空奖」，却又算作抽中（清零保底进度）。
//
// 活动的 LifetimePolicy 决定怎么处理（在抽奖事务里判定、发放，客户端只看结果）：
//
//   - points  按应用的会员折算策略折成积分（会员与限时功能按天、功能时长按小时、永久权益给固定积分；
//     活动可以单独填每天 / 永久积分），并记一条会员折算记录
//   - draws   改发抽奖次数
//   - exclude 这个人的常规抽取里不出现这些奖品（公示概率随之重算）；保底奖恰好是这类时按积分折算
//   - grant   照常发放（旧行为）
//
// 奖品可以单独设「改发」（LifetimeReward），设了就不按公式折算。
// 「谢谢参与」、积分、经验、抽奖次数、人工发放从来不算已拥有。
const (
	LifetimePoints  = "points"
	LifetimeDraws   = "draws"
	LifetimeExclude = "exclude"
	LifetimeGrant   = "grant"
)

// 默认折算与上界。
const (
	DefaultLifetimePointsPerDay   = 10
	DefaultLifetimePointsLifetime = 3650
	DefaultLifetimeDraws          = 1
	MaxLifetimePointsPerDay       = 100_000
	MaxLifetimePointsLifetime     = 10_000_000
	MaxLifetimeDraws              = 100
	// MaxSwapPoints 一次折算最多给多少积分（天数很大的奖品 × 每天积分可能溢出余额上限）
	MaxSwapPoints = 100_000_000
)

// 已拥有的原因。
const (
	OwnedLifetime = vipdomain.OwnedLifetime
	OwnedFeature  = vipdomain.OwnedFeature
)

// LifetimePolicies 处理方式，顺序即控制台的展示顺序。
func LifetimePolicies() []string {
	return []string{LifetimePoints, LifetimeDraws, LifetimeExclude, LifetimeGrant}
}

// swapKinds 「改发」能选的奖品类型：都是不会再「已拥有」的东西。
var swapKinds = []string{KindPoints, KindExperience, KindLotteryDraws}

// SwapKinds 「改发」能选的奖品类型。
func SwapKinds() []string { return slices.Clone(swapKinds) }

// LifetimePolicySpec 控制台上一种处理方式的自述。
type LifetimePolicySpec struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Hint  string `json:"hint"`
}

// LifetimePolicyCatalog 处理方式目录。
func LifetimePolicyCatalog() []LifetimePolicySpec {
	return []LifetimePolicySpec{
		{LifetimePoints, "折算为积分", "会员天数按每天积分折算，永久套餐与永久功能给固定积分"},
		{LifetimeDraws, "改发抽奖次数", "抽中已拥有的奖品时改发若干次抽奖"},
		{LifetimeExclude, "不参与抽取", "对已拥有的用户剔除这些奖品，概率按剩余奖品重算；保底奖仍按积分折算"},
		{LifetimeGrant, "照常发放", "仍记一段会员或功能（永久会员实际拿不到新东西）"},
	}
}

// MemberCoverage 一个人已经拥有什么（统一判据 vip.Holdings），以及应用的会员折算策略（折算积分的单价）。
type MemberCoverage struct {
	vipdomain.Holdings
	Policy vipdomain.ConversionPolicy
}

// Owned 这个奖品对这个人是不是空奖；是的话返回原因（OwnedLifetime / OwnedFeature）。
//
// 会员类奖品问 vip.Holdings.Covers，与购买 40378、积分商城、激励广告、卡密同一个判据：
// 启用中的功能都已永久拥有（永久开通或永久解锁），且免广告也已经有了。
// 永久基础版抽中限时高级版不算已拥有 —— 那是实打实的升级。
func (c MemberCoverage) Owned(reward Reward, plans map[int64]pointstore.PlanRef) (string, bool) {
	switch reward.Type {
	case KindVipDays:
		// 抽奖发的会员一律免广告；不带功能
		if c.CoversMembership(nil) {
			return OwnedLifetime, true
		}
	case KindVipLifetime:
		if c.Covers(vipdomain.MembershipBenefit(nil, true, true)) {
			return OwnedLifetime, true
		}
	case KindAdFree:
		if c.CoversAdFree() {
			return OwnedLifetime, true
		}
	case KindFeatureTime:
		// 功能时长只在「永远免扣」时是空奖：永久会员（会员一律免扣），或已永久解锁这个功能
		return c.CoversFeatureTime(reward.Tag)
	case KindVipPlan:
		plan, ok := plans[reward.RefID]
		if ok && c.CoversPlan(vipdomain.Plan{Kind: vipdomain.KindPaid, Lifetime: plan.Lifetime, Features: plan.Features}) {
			return OwnedLifetime, true
		}
	case KindFeature:
		return c.CoversFeature(reward.Tag)
	}
	return "", false
}

// SwapRule 活动上的处理方式，与折算积分的单价。
type SwapRule struct {
	Policy string
	// Value 折算单价：应用的会员折算策略，活动填了每天 / 永久积分时以活动为准
	Value vipdomain.ConversionPolicy
	Draws int64
	// Scaled 时改发的积分、抽奖次数按原价的 Scale（0–1）发放（无套利，见 economy.go）；零值为原价
	Scaled bool
	Scale  float64
	// Price 用积分换一次抽奖的价格：改发的抽奖次数按它折成积分
	Price int64
}

// RuleOf 活动的处理方式。
//
// 折算积分与卡密、邀请、管理员发放同一套单价（应用的会员折算策略）：会员按天、限时功能与免广告按天、
// 功能时长按小时、永久权益按固定值。活动填了「每天积分」时，会员与限时功能、免广告的每天按活动的；
// 填了「永久积分」时永久奖品按活动的。两项都是 0 即完全跟随应用策略。
func RuleOf(activity Activity, policy vipdomain.ConversionPolicy) SwapRule {
	value := policy
	if activity.LifetimePointsPerDay > 0 {
		value.PerVipDay = activity.LifetimePointsPerDay
		value.PerFeatureDay = activity.LifetimePointsPerDay
	}
	if activity.LifetimePointsLifetime > 0 {
		value.PerLifetime = activity.LifetimePointsLifetime
	}
	return SwapRule{Policy: activity.LifetimePolicy, Value: value, Draws: activity.LifetimeDraws}
}

// OwnedItem 一个已拥有的奖品折成的那一项（与 vip 折算记录同一种结构，积分按 rule.Value 算）。
func OwnedItem(rule SwapRule, prize Prize, plans map[int64]pointstore.PlanRef) vipdomain.ConversionItem {
	reward := NormalizeReward(prize.Reward)
	value := rule.Value
	label := prize.Name
	if label == "" {
		label = DefaultPrizeName(reward, plans, nil)
	}
	switch reward.Type {
	case KindVipDays:
		return value.OwnedVipItem(label, int(reward.Amount), false, nil)
	case KindVipLifetime:
		return value.OwnedVipItem(label, 0, true, nil)
	case KindVipPlan:
		plan := plans[reward.RefID]
		return value.OwnedVipItem(label, plan.DurationDays, plan.Lifetime, plan.Features)
	case KindFeature:
		return value.OwnedUnlockItem(label, reward.Tag, int(reward.Amount))
	case KindFeatureTime:
		seconds := reward.Amount * 60
		return vipdomain.ConversionItem{Kind: vipdomain.ItemFeatureTime, Label: label, Feature: reward.Tag,
			Seconds: seconds, Points: value.FeatureTimePoints(seconds)}
	case KindAdFree:
		seconds := reward.Amount * 86400
		return vipdomain.ConversionItem{Kind: vipdomain.ItemAdFree, Label: label, Seconds: seconds,
			Points: value.FeatureDayPoints(seconds)}
	}
	return vipdomain.ConversionItem{Label: label}
}

// Swap 已拥有的奖品改发什么。ok=false 表示照常发放（grant，或折算出来是 0）。
//
// exclude 只影响常规抽取；走到这里（保底奖、兜底奖恰好是已拥有的）时按积分折算 ——
// 保底承诺的是「必中一个奖」，不能因为他已经是永久会员就落空。
//
// 正常的奖盘按原价折算。原价折算会让积分买次数稳赚时（rule.Scaled），改发的积分、抽奖次数按 rule.Scale
// 的比例发积分，不足 1 积分的改发经验 —— 不会落空成「谢谢参与」。
func Swap(rule SwapRule, prize Prize, plans map[int64]pointstore.PlanRef) (Reward, bool) {
	if prize.LifetimeReward != nil && prize.LifetimeReward.Type != "" {
		if rule.Policy == LifetimeGrant {
			return Reward{}, false
		}
		return scaleSwap(rule, NormalizeReward(*prize.LifetimeReward)), true
	}
	switch rule.Policy {
	case LifetimeGrant:
		return Reward{}, false
	case LifetimeDraws:
		if rule.Draws <= 0 {
			return Reward{}, false
		}
		return scaleSwap(rule, Reward{Type: KindLotteryDraws, Amount: rule.Draws}), true
	}
	amount := min(OwnedItem(rule, prize, plans).Points, MaxSwapPoints)
	if amount <= 0 {
		return Reward{}, false
	}
	return scaleSwap(rule, Reward{Type: KindPoints, Amount: amount}), true
}

// scaleSwap 需要缩减时，改发的积分、抽奖次数（次数按价格折成积分）按 rule.Scale 发积分（向下取整）；
// 不足 1 积分时改发同等原价的经验（最多 MaxScaledExperience 点）。其余改发（经验等）不受影响。
func scaleSwap(rule SwapRule, reward Reward) Reward {
	if !rule.Scaled || rule.Scale >= 1 {
		return reward
	}
	if reward.Type == KindLotteryDraws && rule.Price <= 0 {
		return reward
	}
	value := SwapValue(rule, reward)
	if value <= 0 {
		return reward
	}
	if points := int64(math.Floor(float64(value) * max(rule.Scale, 0))); points >= 1 {
		return Reward{Type: KindPoints, Amount: points}
	}
	return Reward{Type: KindExperience, Amount: min(value, MaxScaledExperience)}
}

// MaxScaledExperience 缩到不足 1 积分时改发的经验上限（经验换不成积分，只为不让这一格落空）。
const MaxScaledExperience int64 = 100

// SwapValue 改发的东西值多少积分：积分按数量，抽奖次数按 rule.Price 一次，其余为 0。
func SwapValue(rule SwapRule, reward Reward) int64 {
	switch reward.Type {
	case KindPoints:
		return reward.Amount
	case KindLotteryDraws:
		return reward.Amount * max(rule.Price, 0)
	}
	return 0
}

// Excluded 这个人的常规抽取里是否剔除这个奖品。
func Excluded(rule SwapRule, coverage MemberCoverage, prize Prize, plans map[int64]pointstore.PlanRef) bool {
	if rule.Policy != LifetimeExclude {
		return false
	}
	_, owned := coverage.Owned(prize.Reward, plans)
	return owned
}

// PersonalPrizes 这个人眼里的奖盘：exclude 时把已拥有的奖品权重置 0（不再参与常规抽取，仍可作保底 / 兜底）。
func PersonalPrizes(rule SwapRule, coverage MemberCoverage, prizes []Prize, plans map[int64]pointstore.PlanRef) []Prize {
	out := slices.Clone(prizes)
	for index := range out {
		if Excluded(rule, coverage, out[index], plans) {
			out[index].Weight = 0
		}
	}
	return out
}

// Conversion 一次抽奖里「抽中的是 A，发的是 B」的说明，存进记录。
type Conversion struct {
	// Reason lifetime（永久会员已包含）/ feature（已永久解锁这个功能）
	Reason string `json:"reason"`
	// Reward 实际发放的东西
	Reward Reward `json:"reward"`
	// Label 实际发放的一句话（「120 积分」）
	Label string `json:"label"`
	// Message 给用户看的说明（「你已是永久会员，会员 7 天已折算为 70 积分」）
	Message string `json:"message"`
	// ConversionNo 会员折算记录（/vip/conversions 里的那一条）
	ConversionNo string `json:"conversionNo,omitempty"`
}

// ConversionMessage 给用户看的说明；改发的是「谢谢参与」（旧版无套利上限为 0 时的记录）时说不再重复发放。
func ConversionMessage(reason, prizeName, label string) string {
	if label == noneSpec.Label {
		if reason == OwnedFeature {
			return fmt.Sprintf("你已永久拥有该功能，「%s」不再重复发放", prizeName)
		}
		return fmt.Sprintf("你已是永久会员，「%s」不再重复发放", prizeName)
	}
	if reason == OwnedFeature {
		return fmt.Sprintf("你已永久拥有该功能，「%s」已转为%s", prizeName, label)
	}
	return fmt.Sprintf("你已是永久会员，「%s」已转为%s", prizeName, label)
}

// SwapLabel 改发的东西的短名（「70 积分」「抽奖 1 次」「谢谢参与」）。
// 老客户端不认识 none 改发，按这个短名显示「已转为 谢谢参与」，说得通。
func SwapLabel(reward Reward) string {
	return DefaultPrizeName(reward, nil, nil)
}

// PrizeSwap 用户端奖品上的提示：对你来说这一格会怎样。
type PrizeSwap struct {
	// Action convert（改发 Reward）/ excluded（不会抽到）
	Action string  `json:"action"`
	Reason string  `json:"reason"`
	Reward *Reward `json:"reward,omitempty"`
	Label  string  `json:"label"`
}

// 用户端奖品提示的动作。
const (
	SwapActionConvert  = "convert"
	SwapActionExcluded = "excluded"
)

// DescribeSwap 用户端奖品提示；nil 表示照常发放。
func DescribeSwap(rule SwapRule, coverage MemberCoverage, prize Prize, plans map[int64]pointstore.PlanRef) *PrizeSwap {
	reason, owned := coverage.Owned(prize.Reward, plans)
	if !owned {
		return nil
	}
	if rule.Policy == LifetimeExclude && prize.Weight > 0 {
		return &PrizeSwap{Action: SwapActionExcluded, Reason: reason, Label: "已拥有，不会抽到"}
	}
	reward, ok := Swap(rule, prize, plans)
	if !ok {
		return nil
	}
	return &PrizeSwap{Action: SwapActionConvert, Reason: reason, Reward: &reward, Label: "转为" + SwapLabel(reward)}
}

// LifetimeNotice 用户端活动页顶部的提示（只在这个人有已拥有的奖品时出现）。
type LifetimeNotice struct {
	Lifetime bool   `json:"lifetime"`
	Policy   string `json:"policy"`
	Message  string `json:"message"`
	// OwnedPolicy 并入特权说明后，原来的已拥有处理方式（Policy 变成 perk）
	OwnedPolicy string `json:"ownedPolicy,omitempty"`
}

// NoticePerk 提示卡上并入了人群特权说明。老客户端遇到不认识的 policy 时原样显示 message。
const NoticePerk = "perk"

// MergePerkNotice 把特权说明并进活动页顶部的提示卡。
func MergePerkNotice(notice *LifetimeNotice, perks *PerkSummary, lifetime bool) *LifetimeNotice {
	if perks == nil {
		return notice
	}
	if notice == nil {
		return &LifetimeNotice{Lifetime: lifetime, Policy: NoticePerk, Message: perks.Message}
	}
	merged := *notice
	merged.OwnedPolicy = notice.Policy
	merged.Policy = NoticePerk
	merged.Message = notice.Message + "；" + perks.Message
	return &merged
}

// NoticeOf 活动页提示；没有已拥有的奖品时为 nil。
func NoticeOf(rule SwapRule, coverage MemberCoverage, prizes []Prize, plans map[int64]pointstore.PlanRef) *LifetimeNotice {
	owned := false
	for _, prize := range prizes {
		if _, ok := coverage.Owned(prize.Reward, plans); ok {
			owned = true
			break
		}
	}
	if !owned {
		return nil
	}
	who := "已拥有的奖品"
	if coverage.Lifetime {
		who = "你已是永久会员，已包含的会员奖品"
	}
	var message string
	switch rule.Policy {
	case LifetimePoints:
		message = who + "抽中时将自动折算为积分"
	case LifetimeDraws:
		message = who + "抽中时将改发抽奖次数"
	case LifetimeExclude:
		message = who + "不参与抽取，概率已按其余奖品重算"
	default:
		return nil
	}
	return &LifetimeNotice{Lifetime: coverage.Lifetime, Policy: rule.Policy, Message: message}
}

// NormalizeLifetime 清洗处理方式与折算参数：空策略按折算积分；每天 / 永久积分留 0 即跟随应用的会员折算策略。
func NormalizeLifetime(input *ActivityInput) {
	if input.LifetimePolicy == "" {
		input.LifetimePolicy = LifetimePoints
	}
	if input.LifetimeDraws == 0 {
		input.LifetimeDraws = DefaultLifetimeDraws
	}
	for index := range input.Prizes {
		swap := input.Prizes[index].LifetimeReward
		if swap == nil {
			continue
		}
		if swap.Type == "" {
			input.Prizes[index].LifetimeReward = nil
			continue
		}
		normalized := NormalizeReward(*swap)
		input.Prizes[index].LifetimeReward = &normalized
	}
}

// validateLifetime 校验处理方式、折算参数与奖品的改发。
func validateLifetime(input ActivityInput) error {
	switch {
	case !slices.Contains(LifetimePolicies(), input.LifetimePolicy):
		return fmt.Errorf("未知的已拥有奖品处理方式：%s", input.LifetimePolicy)
	case input.LifetimePointsPerDay < 0 || input.LifetimePointsPerDay > MaxLifetimePointsPerDay:
		return fmt.Errorf("每天折算积分需要在 0–%d 之间", MaxLifetimePointsPerDay)
	case input.LifetimePointsLifetime < 0 || input.LifetimePointsLifetime > MaxLifetimePointsLifetime:
		return fmt.Errorf("永久奖品折算积分需要在 0–%d 之间", MaxLifetimePointsLifetime)
	case input.LifetimeDraws < 0 || input.LifetimeDraws > MaxLifetimeDraws:
		return fmt.Errorf("改发抽奖次数需要在 0–%d 之间", MaxLifetimeDraws)
	case input.LifetimePolicy == LifetimeDraws && input.LifetimeDraws == 0:
		return fmt.Errorf("改发抽奖次数时次数至少为 1")
	}
	for index, prize := range input.Prizes {
		if prize.LifetimeReward == nil {
			continue
		}
		label := fmt.Sprintf("第 %d 个奖品的改发", index+1)
		if !ownable(prize.Reward.Type) {
			return fmt.Errorf("%s：只有会员与功能奖品可以设置改发", label)
		}
		if !slices.Contains(swapKinds, prize.LifetimeReward.Type) {
			return fmt.Errorf("%s只能是积分、经验或抽奖次数", label)
		}
		if err := ValidateReward(*prize.LifetimeReward); err != nil {
			return fmt.Errorf("%s：%w", label, err)
		}
	}
	return nil
}

// OwnableKinds 可能「已拥有」、因而可以设置改发的奖品类型（控制台用）。
func OwnableKinds() []string {
	out := []string{}
	for _, kind := range kinds {
		if ownable(kind) {
			out = append(out, kind)
		}
	}
	return out
}

// ownable 这一类奖品是否可能「已拥有」。
func ownable(kind string) bool {
	switch kind {
	case KindVipDays, KindVipPlan, KindFeature, KindVipLifetime, KindFeatureTime, KindAdFree:
		return true
	}
	return false
}
