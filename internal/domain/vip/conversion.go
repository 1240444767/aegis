package vip

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strconv"
	"time"
)

// 会员权益的统一折算。
//
// ── 两种「拿到了等于没拿到」 ──
//
//	已拥有发放  永久会员又收到「会员 7 天」、他已包含的套餐、永久会员已包含的功能解锁……
//	           照常记一段什么也没给他（功能取两条线的并集，永久那条线已经全包了）。
//	开通永久    开通前买的限时会员、限时功能解锁、功能时长余额、免广告时长，
//	           开通之后都被永久那条线盖住，剩下的时间白白流走。
//
// 两种情况都按应用的同一份折算策略（ConversionPolicy）处理：按剩余价值折成积分，
// 记一笔折算记录（Conversion），会员账本里留一行说明。判「已拥有」只有一个入口（Holdings），
// 购买、积分商城、抽奖、激励广告、卡密、邀请、排行榜、管理员发放都问它。
//
// 抽奖与激励广告各自另有「改发什么」的配置（活动 / 场景级），那是它们自己的玩法；
// 但「对这个人是不是空的」与这里同一个判据。

// 已拥有的原因。
const (
	OwnedLifetime = "lifetime" // 永久会员已包含
	OwnedFeature  = "feature"  // 已永久解锁这个功能
)

// ── 策略 ──

// 已拥有发放的处理方式。
const (
	OwnedConvert = "convert" // 折算为积分（默认）
	OwnedGrant   = "grant"   // 照常发放（旧行为）
)

// 开通永久时存量的处理方式。
const (
	UpgradeConvert = "convert" // 折算为积分并结束被覆盖的存量（默认）
	UpgradeKeep    = "keep"    // 保留存量（旧行为）
)

// 默认折算与上界。
const (
	DefaultPerVipDay      = 10
	DefaultPerFeatureDay  = 5
	DefaultPerFeatureHour = 10
	DefaultPerLifetime    = 3650
	MaxConversionRate     = 1_000_000
	// MaxConversionPoints 一次折算最多给多少积分（剩余天数很大时乘出来可能撑破余额上限）
	MaxConversionPoints = 100_000_000
)

// ConversionPolicy 应用的会员折算策略。没配过的应用按 DefaultConversionPolicy。
type ConversionPolicy struct {
	AppID int64 `json:"appid"`
	// Owned 已拥有的权益被发放时（卡密、邀请、排行榜、管理员发放、远程函数）怎么办
	Owned string `json:"owned"`
	// Upgrade 开通永久会员时，被覆盖的存量怎么办
	Upgrade string `json:"upgrade"`
	// PerVipDay 会员每 1 天折多少积分（按秒计、向上取整）
	PerVipDay int64 `json:"perVipDay"`
	// PerFeatureDay 限时功能解锁每 1 天折多少积分；免广告时长也按它
	PerFeatureDay int64 `json:"perFeatureDay"`
	// PerFeatureHour 功能使用时长每 60 分钟折多少积分
	PerFeatureHour int64 `json:"perFeatureHour"`
	// PerLifetime 已包含的永久套餐、已永久解锁的功能再被发放一次时给的固定积分
	PerLifetime int64 `json:"perLifetime"`
	// RestoreOnRevoke 永久会员被取消或退款时，把开通时折掉的存量还回去、收回折给的积分（余额不足时收到 0 为止）
	RestoreOnRevoke bool       `json:"restoreOnRevoke"`
	Configured      bool       `json:"configured"`
	UpdatedBy       string     `json:"updatedBy,omitempty"`
	UpdatedAt       *time.Time `json:"updatedAt,omitempty"`
}

// DefaultConversionPolicy 没配过的应用的策略。
func DefaultConversionPolicy(appID int64) ConversionPolicy {
	return ConversionPolicy{
		AppID: appID, Owned: OwnedConvert, Upgrade: UpgradeConvert,
		PerVipDay: DefaultPerVipDay, PerFeatureDay: DefaultPerFeatureDay,
		PerFeatureHour: DefaultPerFeatureHour, PerLifetime: DefaultPerLifetime,
		RestoreOnRevoke: true,
	}
}

// Validate 校验策略。
func (p ConversionPolicy) Validate() error {
	switch {
	case p.Owned != OwnedConvert && p.Owned != OwnedGrant:
		return fmt.Errorf("未知的已拥有处理方式：%s", p.Owned)
	case p.Upgrade != UpgradeConvert && p.Upgrade != UpgradeKeep:
		return fmt.Errorf("未知的开通永久处理方式：%s", p.Upgrade)
	}
	for _, rate := range []struct {
		label string
		value int64
	}{
		{"会员每天折算积分", p.PerVipDay}, {"功能解锁每天折算积分", p.PerFeatureDay},
		{"功能时长每小时折算积分", p.PerFeatureHour}, {"永久权益折算积分", p.PerLifetime},
	} {
		if rate.value < 0 || rate.value > MaxConversionRate {
			return fmt.Errorf("%s需要在 0–%d 之间", rate.label, MaxConversionRate)
		}
	}
	return nil
}

// ConversionModeSpec 控制台上一种处理方式的自述。
type ConversionModeSpec struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Hint  string `json:"hint"`
}

// OwnedModeCatalog 已拥有发放的处理方式目录。
func OwnedModeCatalog() []ConversionModeSpec {
	return []ConversionModeSpec{
		{OwnedConvert, "折算为积分", "会员按天、功能解锁按天、永久权益按固定值折成积分，账本里留一行说明"},
		{OwnedGrant, "照常发放", "仍记一段会员或功能（永久会员实际拿不到新东西）"},
	}
}

// UpgradeModeCatalog 开通永久时存量的处理方式目录。
func UpgradeModeCatalog() []ConversionModeSpec {
	return []ConversionModeSpec{
		{UpgradeConvert, "折算为积分", "被永久会员覆盖的限时会员、限时功能解锁、功能时长与免广告时长按剩余时间折成积分并就此结束"},
		{UpgradeKeep, "保留", "存量原样保留，继续走到原来的到期时间"},
	}
}

// ── 折算记录 ──

// 折算的种类。
const (
	ConversionOwned   = "owned"   // 发放时已拥有
	ConversionUpgrade = "upgrade" // 开通永久时折算存量
)

// 折算记录的状态。
const (
	ConversionApplied  = "applied"
	ConversionRestored = "restored" // 永久会员被取消 / 退款，已还回
)

// 折算项的种类。
const (
	ItemVipTime       = "vip_time"       // 一段限时会员的剩余时间
	ItemVipPlan       = "vip_plan"       // 一个已包含的套餐（限时）
	ItemVipLifetime   = "vip_lifetime"   // 一个已包含的永久套餐 / 永久会员
	ItemFeatureUnlock = "feature_unlock" // 限时功能解锁（剩余时间）
	ItemFeaturePerm   = "feature_permanent"
	ItemFeatureTime   = "feature_time" // 功能使用时长余额
	ItemAdFree        = "ad_free"      // 免广告时长
)

// ConversionItem 折掉的一项。
type ConversionItem struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
	// Seconds 折掉的时长（会员、功能解锁、功能时长、免广告）；永久项为 0
	Seconds  int64    `json:"seconds,omitempty"`
	Feature  string   `json:"feature,omitempty"`
	Features []string `json:"features,omitempty"`
	// Ref 被折掉的那一段：会员开通单号 / 功能解锁 ID
	Ref    string `json:"ref,omitempty"`
	Points int64  `json:"points"`
}

// Conversion 一次折算。
type Conversion struct {
	ID           int64  `json:"id"`
	ConversionNo string `json:"conversionNo"`
	AppID        int64  `json:"appid"`
	UserID       int64  `json:"userId"`
	Kind         string `json:"kind"`
	// Source 触发这次折算的发放渠道（与会员开通渠道同一套取值）
	Source string `json:"source"`
	// Reason lifetime / feature
	Reason string           `json:"reason"`
	Items  []ConversionItem `json:"items"`
	Points int64            `json:"points"`
	// IntegralTransactionNo 折给的积分那笔流水
	IntegralTransactionNo string `json:"integralTransactionNo,omitempty"`
	// TriggerTransactionNo 开通永久的那一笔（upgrade）
	TriggerTransactionNo string `json:"triggerTransactionNo,omitempty"`
	// VipTransactionNo 会员账本里留的那一行
	VipTransactionNo string         `json:"vipTransactionNo,omitempty"`
	Message          string         `json:"message"`
	Status           string         `json:"status"`
	RestoredAt       *time.Time     `json:"restoredAt,omitempty"`
	Restore          map[string]any `json:"restore,omitempty"`
	Operator         string         `json:"operator,omitempty"`
	CreatedAt        time.Time      `json:"createdAt"`
}

// ConversionSummary 会员账本那一行带的折算摘要（transaction.conversion）。
type ConversionSummary struct {
	ConversionNo string `json:"conversionNo"`
	Kind         string `json:"kind"`
	Reason       string `json:"reason"`
	Points       int64  `json:"points"`
	Label        string `json:"label"`
	Message      string `json:"message"`
}

// Summary 账本行与发放结果用的摘要。
func (c Conversion) Summary() *ConversionSummary {
	return &ConversionSummary{
		ConversionNo: c.ConversionNo, Kind: c.Kind, Reason: c.Reason, Points: c.Points,
		Label: ItemsLabel(c.Items), Message: c.Message,
	}
}

// ItemsLabel 折掉的东西的一句话（「会员 12 天、在线听歌 30 分钟」）。
func ItemsLabel(items []ConversionItem) string {
	out := ""
	for index, item := range items {
		if index > 0 {
			out += "、"
		}
		out += item.Label
	}
	return out
}

// ConversionMessage 给用户看的说明。
func ConversionMessage(kind, reason string, items []ConversionItem, points int64) string {
	label := ItemsLabel(items)
	tail := "已折算为 " + strconv.FormatInt(points, 10) + " 积分"
	if points <= 0 {
		tail = "已结束"
	}
	switch {
	case kind == ConversionUpgrade:
		return "开通永久会员后，原有的" + label + tail
	case reason == OwnedFeature:
		return "你已永久拥有该功能，" + label + tail
	default:
		return "你已是永久会员，" + label + tail
	}
}

// ── 价值 ──

func perSeconds(seconds int64, rate int64, unit int64) int64 {
	if seconds <= 0 || rate <= 0 {
		return 0
	}
	// 天数极大的奖品 × 单价会溢出 int64：先按上界截住
	if seconds > (math.MaxInt64-unit)/rate {
		return MaxConversionPoints
	}
	// 向上取整：剩 1 小时的会员也值点什么
	value := (seconds*rate + unit - 1) / unit
	return min(value, MaxConversionPoints)
}

const (
	secondsPerDay  = 86400
	secondsPerHour = 3600
)

// VipPoints 一段会员时间值多少积分。
func (p ConversionPolicy) VipPoints(seconds int64) int64 {
	return perSeconds(seconds, p.PerVipDay, secondsPerDay)
}

// FeatureDayPoints 一段功能解锁 / 免广告时间值多少积分。
func (p ConversionPolicy) FeatureDayPoints(seconds int64) int64 {
	return perSeconds(seconds, p.PerFeatureDay, secondsPerDay)
}

// FeatureTimePoints 一段功能使用时长值多少积分。
func (p ConversionPolicy) FeatureTimePoints(seconds int64) int64 {
	return perSeconds(seconds, p.PerFeatureHour, secondsPerHour)
}

// DaysLabel 时长的中文说法：满一天按天（向上取整），不足一天按小时 / 分钟。
func DaysLabel(seconds int64) string {
	switch {
	case seconds >= secondsPerDay:
		return strconv.FormatInt((seconds+secondsPerDay-1)/secondsPerDay, 10) + " 天"
	case seconds >= secondsPerHour:
		return strconv.FormatInt((seconds+secondsPerHour-1)/secondsPerHour, 10) + " 小时"
	default:
		return strconv.FormatInt(max(1, (seconds+59)/60), 10) + " 分钟"
	}
}

// MinutesLabel 功能时长的中文说法。
func MinutesLabel(seconds int64) string {
	if seconds >= secondsPerHour && seconds%secondsPerHour == 0 {
		return strconv.FormatInt(seconds/secondsPerHour, 10) + " 小时"
	}
	return strconv.FormatInt(max(1, (seconds+59)/60), 10) + " 分钟"
}

// SumPoints 各项积分之和（封顶）。
func SumPoints(items []ConversionItem) int64 {
	var total int64
	for _, item := range items {
		total += item.Points
	}
	return min(total, MaxConversionPoints)
}

// ── 开通永久：存量怎么折 ──

// TimedSegment 一段仍生效的限时会员（链上的实际位置）。
type TimedSegment struct {
	ID            int64
	TransactionNo string
	Label         string
	Features      []string
	// AdFree 这一段免广告（看广告领的可能不免）
	AdFree bool
	From   time.Time
	Until  time.Time
}

// TimedUnlock 一条仍生效的限时功能解锁。
type TimedUnlock struct {
	ID     int64
	Tag    string
	Name   string
	Starts time.Time
	Ends   time.Time
}

// FeatureTimeBalance 一个功能的使用时长余额。
type FeatureTimeBalance struct {
	Feature string
	Name    string
	Seconds int64
}

// UpgradeInput 开通永久那一刻的存量。
type UpgradeInput struct {
	Now      time.Time
	ExpireAt *time.Time
	Segments []TimedSegment
	Unlocks  []TimedUnlock
	Balances []FeatureTimeBalance
	AdFree   *time.Time
	// Holdings 开通之后的永久持有（已计入这一笔）
	Holdings Holdings
	Policy   ConversionPolicy
}

// SegmentMove 一段会员期的新位置；Removed 表示整段（剩余部分）被折掉。
type SegmentMove struct {
	ID      int64
	From    time.Time
	Until   time.Time
	Removed bool
}

// UpgradePlan 开通永久时要做的事。
type UpgradePlan struct {
	Moves []SegmentMove
	// RemovedSeconds 限时会员一共折掉了多少（含没有开通记录的老到期时间）
	RemovedSeconds int64
	// NewExpireAt 限时那条线的新到期时间；nil 表示不动
	NewExpireAt *time.Time
	UnlockIDs   []int64
	Balances    []FeatureTimeBalance
	EndAdFree   bool
	Items       []ConversionItem
	Points      int64
}

// Empty 没有任何可折的存量。
func (p UpgradePlan) Empty() bool { return len(p.Items) == 0 }

// PlanUpgrade 算出开通永久时被覆盖的存量与折算（纯函数，预览与事务共用）。
//
// 限时会员：对他已经是空的那几段（Holdings.Covers：启用中的功能都已永久拥有、免广告也已有）被折掉 —— 正在走的那段截在此刻，还没轮到的整段作废，
// 排在它们后面、没被覆盖的段（比如永久基础版之后另买的限时高级版）整体前移，不留空档。
// 没有开通记录的那截到期时间（老系统直接写进 users 的）不带任何功能，一并折掉。
//
// 限时功能解锁：功能已在永久开通里的折掉。功能时长：永久会员一律免扣，余额全部折掉。
// 免广告时长：永久那条线免广告时折掉。
func PlanUpgrade(in UpgradeInput) UpgradePlan {
	plan := UpgradePlan{}
	if !in.Holdings.Lifetime {
		return plan
	}
	now := in.Now
	segments := slices.Clone(in.Segments)
	sort.SliceStable(segments, func(i, j int) bool {
		if !segments[i].From.Equal(segments[j].From) {
			return segments[i].From.Before(segments[j].From)
		}
		return segments[i].ID < segments[j].ID
	})

	var shift time.Duration
	var latestKept time.Time
	var latestOriginal time.Time
	for _, segment := range segments {
		if !segment.Until.After(now) || !segment.Until.After(segment.From) {
			continue
		}
		if segment.Until.After(latestOriginal) {
			latestOriginal = segment.Until
		}
		start := segment.From
		if start.Before(now) {
			start = now
		}
		if in.Holdings.Covers(MembershipBenefit(segment.Features, false, segment.AdFree)) {
			remaining := segment.Until.Sub(start)
			seconds := int64(remaining / time.Second)
			plan.RemovedSeconds += seconds
			// 正在走的这段截在此刻；还没轮到的段窗口收成空
			cut := start.Add(-shift)
			if cut.Before(now) {
				cut = now
			}
			from := segment.From
			if from.After(cut) {
				from = cut
			}
			plan.Moves = append(plan.Moves, SegmentMove{ID: segment.ID, From: from, Until: cut, Removed: true})
			shift += remaining
			label := segment.Label
			if label == "" {
				label = "限时会员"
			}
			plan.Items = append(plan.Items, ConversionItem{
				Kind: ItemVipTime, Label: label + " " + DaysLabel(seconds), Seconds: seconds,
				Features: NormalizeFeatureTags(segment.Features), Ref: segment.TransactionNo,
				Points: in.Policy.VipPoints(seconds),
			})
			continue
		}
		from := segment.From.Add(-shift)
		until := segment.Until.Add(-shift)
		if segment.From.After(now) && from.Before(now) {
			// 前移不越过此刻：还没轮到的段最多提到现在开始
			until = until.Add(now.Sub(from))
			from = now
		}
		if shift > 0 {
			plan.Moves = append(plan.Moves, SegmentMove{ID: segment.ID, From: from, Until: until})
		}
		if until.After(latestKept) {
			latestKept = until
		}
	}

	// 没有开通记录的那截到期时间：不带功能，对永久会员一律是空的
	if in.ExpireAt != nil && in.ExpireAt.After(now) {
		tailFrom := now
		if latestOriginal.After(tailFrom) {
			tailFrom = latestOriginal
		}
		if in.ExpireAt.After(tailFrom) {
			seconds := int64(in.ExpireAt.Sub(tailFrom) / time.Second)
			if seconds > 0 {
				plan.RemovedSeconds += seconds
				plan.Items = append(plan.Items, ConversionItem{
					Kind: ItemVipTime, Label: "限时会员 " + DaysLabel(seconds), Seconds: seconds,
					Points: in.Policy.VipPoints(seconds),
				})
			}
		}
		if plan.RemovedSeconds > 0 {
			end := now
			if latestKept.After(end) {
				end = latestKept
			}
			plan.NewExpireAt = &end
		}
	}

	for _, unlock := range in.Unlocks {
		if !unlock.Ends.After(now) {
			continue
		}
		if reason, ok := in.Holdings.CoversFeature(unlock.Tag); !ok || reason != OwnedLifetime {
			continue
		}
		start := unlock.Starts
		if start.Before(now) {
			start = now
		}
		seconds := int64(unlock.Ends.Sub(start) / time.Second)
		if seconds <= 0 {
			continue
		}
		plan.UnlockIDs = append(plan.UnlockIDs, unlock.ID)
		plan.Items = append(plan.Items, ConversionItem{
			Kind: ItemFeatureUnlock, Label: nameOr(unlock.Name, unlock.Tag) + " " + DaysLabel(seconds),
			Seconds: seconds, Feature: unlock.Tag, Ref: strconv.FormatInt(unlock.ID, 10),
			Points: in.Policy.FeatureDayPoints(seconds),
		})
	}

	for _, balance := range in.Balances {
		if balance.Seconds <= 0 {
			continue
		}
		if _, ok := in.Holdings.CoversFeatureTime(balance.Feature); !ok {
			continue
		}
		plan.Balances = append(plan.Balances, balance)
		plan.Items = append(plan.Items, ConversionItem{
			Kind: ItemFeatureTime, Label: nameOr(balance.Name, balance.Feature) + " " + MinutesLabel(balance.Seconds),
			Seconds: balance.Seconds, Feature: balance.Feature, Points: in.Policy.FeatureTimePoints(balance.Seconds),
		})
	}

	if in.AdFree != nil && in.AdFree.After(now) && in.Holdings.CoversAdFree() {
		seconds := int64(in.AdFree.Sub(now) / time.Second)
		if seconds > 0 {
			plan.EndAdFree = true
			plan.Items = append(plan.Items, ConversionItem{
				Kind: ItemAdFree, Label: "免广告 " + DaysLabel(seconds), Seconds: seconds,
				Points: in.Policy.FeatureDayPoints(seconds),
			})
		}
	}

	plan.Points = SumPoints(plan.Items)
	return plan
}

func nameOr(name, tag string) string {
	if name != "" {
		return name
	}
	return tag
}

// ── 发放时已拥有：值多少 ──

// OwnedVipItem 一次已拥有的会员发放折成的那一项。
func (p ConversionPolicy) OwnedVipItem(label string, days int, lifetime bool, features []string) ConversionItem {
	if lifetime {
		return ConversionItem{Kind: ItemVipLifetime, Label: label, Features: NormalizeFeatureTags(features), Points: min(p.PerLifetime, MaxConversionPoints)}
	}
	seconds := int64(days) * secondsPerDay
	return ConversionItem{Kind: ItemVipPlan, Label: label, Seconds: seconds, Features: NormalizeFeatureTags(features), Points: p.VipPoints(seconds)}
}

// OwnedUnlockItem 一次已拥有的功能解锁折成的那一项（days 为 0 即永久解锁）。
func (p ConversionPolicy) OwnedUnlockItem(label, tag string, days int) ConversionItem {
	if days <= 0 {
		return ConversionItem{Kind: ItemFeaturePerm, Label: label, Feature: tag, Points: min(p.PerLifetime, MaxConversionPoints)}
	}
	seconds := int64(days) * secondsPerDay
	return ConversionItem{Kind: ItemFeatureUnlock, Label: label, Feature: tag, Seconds: seconds, Points: p.FeatureDayPoints(seconds)}
}

// UpgradePreview 开通永久之前给用户 / 管理员看的：开通之后存量会怎样。
type UpgradePreview struct {
	// Mode convert（折算）/ keep（保留）
	Mode    string           `json:"mode"`
	Items   []ConversionItem `json:"items"`
	Points  int64            `json:"points"`
	Message string           `json:"message"`
}

// Preview 没有可折的存量时为 nil。
func (p UpgradePlan) Preview(mode string) *UpgradePreview {
	if p.Empty() {
		return nil
	}
	label := ItemsLabel(p.Items)
	preview := &UpgradePreview{Mode: mode, Items: p.Items, Points: p.Points}
	if mode == UpgradeConvert {
		preview.Message = "开通后，原有的" + label + "将折算为 " + strconv.FormatInt(p.Points, 10) + " 积分"
		if p.Points <= 0 {
			preview.Message = "开通后，原有的" + label + "将被永久会员覆盖并结束"
		}
	} else {
		preview.Message = "开通后，原有的" + label + "将保留到原来的到期时间"
		preview.Points = 0
	}
	return preview
}
