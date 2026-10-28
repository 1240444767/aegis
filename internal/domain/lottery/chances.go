package lottery

import "time"

// 不能抽的原因。客户端按 reason 分支，不要匹配文案。
const (
	ReasonOK                = ""
	ReasonNotFound          = "not_found"
	ReasonNotStarted        = "not_started"
	ReasonEnded             = "ended"
	ReasonFreeExhausted     = "free_exhausted"
	ReasonBonusExhausted    = "bonus_exhausted"
	ReasonPointsUnavailable = "points_unavailable"
	ReasonPointsDailyLimit  = "points_daily_limit"
	ReasonPointsShort       = "points_insufficient"
	ReasonNoChance          = "no_chance"
	ReasonUnknownSource     = "unknown_source"
)

// ReasonMessage 不能抽的原因的用户可读文案。
func ReasonMessage(reason string) string {
	switch reason {
	case ReasonNotFound:
		return "活动不存在或已下线"
	case ReasonNotStarted:
		return "活动尚未开始"
	case ReasonEnded:
		return "活动已结束"
	case ReasonFreeExhausted:
		return "今日免费次数已用完"
	case ReasonBonusExhausted:
		return "没有可用的抽奖次数"
	case ReasonPointsUnavailable:
		return "该活动未开放积分抽奖"
	case ReasonPointsDailyLimit:
		return "今日积分抽奖次数已用完"
	case ReasonPointsShort:
		return "积分不足"
	case ReasonNoChance:
		return "抽奖次数已用完"
	case ReasonUnknownSource:
		return "未知的抽奖方式"
	}
	return "暂时无法抽奖"
}

// Window 活动此刻能不能抽：只看状态与时间窗。
func Window(activity Activity, now time.Time) string {
	if activity.Status != StatusActive {
		if activity.Status == StatusEnded {
			return ReasonEnded
		}
		return ReasonNotFound
	}
	if activity.StartTime != nil && now.Before(*activity.StartTime) {
		return ReasonNotStarted
	}
	if activity.EndTime != nil && !now.Before(*activity.EndTime) {
		return ReasonEnded
	}
	return ReasonOK
}

// EvaluateChances 由活动配置与这个人的事实算出各种次数。video 由服务层按激励广告配置填好。
//
// 这是唯一的判据：活动页上显示的剩余次数、「点一下抽奖」用哪种次数、抽奖时报哪个错，
// 都出自这里，用户看到的「还剩 0 次」与点了之后的报错永远是同一件事。
func EvaluateChances(activity Activity, facts UserFacts, video VideoChances) Chances {
	chances := Chances{
		Free: FreeChances{
			Daily: activity.FreeDaily,
			Used:  facts.FreeUsedToday,
		},
		Bonus:   max(facts.Bonus, 0),
		Tickets: SplitTickets(facts.Bonus, facts.BonusPurchased),
		Video:   video,
	}
	chances.Free.Remaining = max(activity.FreeDaily-facts.FreeUsedToday, 0)

	if activity.PointsCost > 0 {
		points := PointsChances{
			Enabled:   true,
			Cost:      activity.PointsCost,
			Daily:     activity.PointsDaily,
			Used:      facts.PointsUsedToday,
			Remaining: -1,
			Balance:   facts.Integral,
		}
		if activity.PointsDaily > 0 {
			points.Remaining = max(activity.PointsDaily-facts.PointsUsedToday, 0)
		}
		points.Affordable = facts.Integral >= activity.PointsCost
		chances.Points = points
	}

	for _, source := range Sources() {
		if CheckSource(chances, source) == ReasonOK {
			chances.Next = source
			break
		}
	}
	return chances
}

// CheckSource 用 source 抽一次行不行；行返回 ReasonOK。
func CheckSource(chances Chances, source string) string {
	switch source {
	case SourceFree:
		if chances.Free.Remaining <= 0 {
			return ReasonFreeExhausted
		}
	case SourceBonus:
		if chances.Bonus <= 0 {
			return ReasonBonusExhausted
		}
	case SourcePoints:
		points := chances.Points
		switch {
		case !points.Enabled:
			return ReasonPointsUnavailable
		case points.Remaining == 0:
			return ReasonPointsDailyLimit
		case !points.Affordable:
			return ReasonPointsShort
		}
	default:
		return ReasonUnknownSource
	}
	return ReasonOK
}

// ResolveSource 客户端指定的来源（空为不设上限）落到具体哪一种次数。
//
// 指定的来源是「最多用到这一档」而不是「必须用这一档」：按 free → bonus → points 走到它为止，取第一个可用的。
// 客户端的活动页可能是过了零点、签到之前拉的，按它缓存的 next 传上来的 bonus / points 不能跳过已经可用的免费次数。
// 走到指定的那一档仍不可用时，报那一档自己的原因（与只看那一档时相同）。
func ResolveSource(chances Chances, requested string) (string, string) {
	if requested == "" {
		if chances.Next == "" {
			return "", ReasonNoChance
		}
		return chances.Next, ReasonOK
	}
	for _, source := range Sources() {
		if CheckSource(chances, source) == ReasonOK {
			return source, ReasonOK
		}
		if source == requested {
			return "", CheckSource(chances, requested)
		}
	}
	return "", ReasonUnknownSource
}
