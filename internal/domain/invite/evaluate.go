package invite

import (
	"slices"
	"strconv"
	"time"
)

// 不能填写邀请码的原因。客户端按 reason 分支，不要匹配 message。
const (
	ReasonOK                = "ok"
	ReasonDisabled          = "disabled"           // 应用未开放邀请
	ReasonRequired          = "required"           // 注册必须填写邀请码
	ReasonAlreadyBound      = "already_bound"      // 已经填写过
	ReasonWindowClosed      = "window_closed"      // 超过补填时限（或只能在注册时填写）
	ReasonCodeNotFound      = "code_not_found"     // 邀请码不存在
	ReasonCodeDisabled      = "code_disabled"      // 邀请码已停用
	ReasonCodeNotStarted    = "code_not_started"   // 活动码还没开始
	ReasonCodeExpired       = "code_expired"       // 活动码已结束
	ReasonCodeExhausted     = "code_exhausted"     // 活动码次数已用完
	ReasonSelfInvite        = "self_invite"        // 自己的邀请码
	ReasonCycle             = "cycle"              // 对方是自己邀请来的
	ReasonInviterIneligible = "inviter_ineligible" // 邀请人不满足邀请条件
	ReasonDeviceLimited     = "device_limited"     // 这台设备填写次数已满
	ReasonIPLimited         = "ip_limited"         // 当前网络今日填写次数已满
	ReasonRewardUnavailable = "reward_unavailable" // 配置的奖励已失效
)

var reasonMessages = map[string]string{
	ReasonOK:                "可以填写",
	ReasonDisabled:          "邀请活动暂未开放",
	ReasonRequired:          "请填写邀请码",
	ReasonAlreadyBound:      "已填写过邀请码",
	ReasonWindowClosed:      "已超过填写期限",
	ReasonCodeNotFound:      "邀请码不存在",
	ReasonCodeDisabled:      "邀请码已停用",
	ReasonCodeNotStarted:    "邀请码尚未生效",
	ReasonCodeExpired:       "邀请码已过期",
	ReasonCodeExhausted:     "邀请码使用次数已满",
	ReasonSelfInvite:        "不能填写自己的邀请码",
	ReasonCycle:             "不能填写由你邀请的用户的邀请码",
	ReasonInviterIneligible: "该邀请码暂不可用",
	ReasonDeviceLimited:     "当前设备已填写过邀请码",
	ReasonIPLimited:         "当前网络今日填写次数已满，请稍后再试",
	ReasonRewardUnavailable: "邀请奖励配置已失效，请联系客服",
}

// ReasonMessage 原因对应的说明，未知原因返回空串。
func ReasonMessage(reason string) string { return reasonMessages[reason] }

// BindFacts 判定「这个人此刻能不能填写这个码」所需的事实，由仓储在锁住两个人之后取齐。
type BindFacts struct {
	Now          time.Time
	Source       string
	RegisteredAt time.Time
	AlreadyBound bool
	InviteeID    int64
	// Code 查到的邀请码；nil 为不存在
	Code *Code
	// InviterParentID 邀请人自己的邀请人（判「对方是我邀请来的」）
	InviterParentID int64
	// InviterEligible 邀请人满足邀请条件（EvaluateInviter）
	InviterEligible bool
	// DeviceBindings 这台设备在本应用填写过几次；IPBindingsToday 这个 IP 今天填写过几次
	DeviceBindings  int64
	IPBindingsToday int64
	// RewardsResolvable 这次要发的奖励引用的套餐与功能都还在
	RewardsResolvable bool
}

// EvaluateBind 能不能填写。纯函数，判据顺序即优先级：先说「这个人」（开没开、填没填过、过没过期），
// 再说「这个码」，最后才是防刷 —— 先说「设备已填写过」会让一个本来就过期的人去换设备。
func EvaluateBind(settings Settings, facts BindFacts) string {
	if reason := EvaluateEntry(settings, facts.Source, facts.RegisteredAt, facts.AlreadyBound, facts.Now); reason != ReasonOK {
		return reason
	}
	if reason := CodeUsable(facts.Code, facts.Now); reason != ReasonOK {
		return reason
	}
	code := facts.Code
	if code.OwnerUserID != nil && *code.OwnerUserID == facts.InviteeID {
		return ReasonSelfInvite
	}
	if code.OwnerUserID != nil && facts.InviterParentID == facts.InviteeID {
		return ReasonCycle
	}
	if code.Kind == KindPersonal && !facts.InviterEligible {
		return ReasonInviterIneligible
	}
	if !facts.RewardsResolvable {
		return ReasonRewardUnavailable
	}
	if settings.DeviceLimit > 0 && facts.DeviceBindings >= int64(settings.DeviceLimit) {
		return ReasonDeviceLimited
	}
	if settings.IPDailyLimit > 0 && facts.IPBindingsToday >= int64(settings.IPDailyLimit) {
		return ReasonIPLimited
	}
	return ReasonOK
}

// EvaluateEntry 这个人此刻还能不能填写（与码无关的部分），邀请页的「填写邀请码」区块据此显示。
func EvaluateEntry(settings Settings, source string, registeredAt time.Time, alreadyBound bool, now time.Time) string {
	switch {
	case !settings.Enabled:
		return ReasonDisabled
	case alreadyBound:
		return ReasonAlreadyBound
	case source == SourceBind && !WithinWindow(settings.Config, registeredAt, now):
		return ReasonWindowClosed
	}
	return ReasonOK
}

// CodeUsable 码本身能不能用（与填写的人无关）。
func CodeUsable(code *Code, now time.Time) string {
	switch {
	case code == nil:
		return ReasonCodeNotFound
	case code.Status != CodeActive:
		return ReasonCodeDisabled
	case code.StartsAt != nil && now.Before(*code.StartsAt):
		return ReasonCodeNotStarted
	case code.EndsAt != nil && !now.Before(*code.EndsAt):
		return ReasonCodeExpired
	case code.MaxUses != nil && code.UsedCount >= *code.MaxUses:
		return ReasonCodeExhausted
	}
	return ReasonOK
}

// BindDeadline 补填截止时间；只能在注册时填写（时限为 0）时为 nil。
func BindDeadline(cfg Config, registeredAt time.Time) *time.Time {
	if cfg.BindWindowHours <= 0 {
		return nil
	}
	deadline := registeredAt.Add(time.Duration(cfg.BindWindowHours) * time.Hour)
	return &deadline
}

// WithinWindow 注册后补填的时限内。
func WithinWindow(cfg Config, registeredAt, now time.Time) bool {
	deadline := BindDeadline(cfg, registeredAt)
	return deadline != nil && now.Before(*deadline)
}

// EvaluateInviter 这个人的个人码能不能被填写，不能时给出说给他自己听的原因。
func EvaluateInviter(cfg Config, isVip bool, level int) (bool, string) {
	if cfg.InviterAudience == AudienceVip && !isVip {
		return false, "开通会员后即可邀请好友"
	}
	if cfg.InviterMinLevel > 0 && level < cfg.InviterMinLevel {
		return false, "达到 Lv." + strconv.Itoa(cfg.InviterMinLevel) + " 后即可邀请好友"
	}
	return true, ""
}

// InviterDue 邀请人奖励的发放条件是否已达成。
func InviterDue(cfg Config, inviteeSignDays int64) bool {
	if cfg.InviterTrigger == TriggerSignIn {
		return inviteeSignDays >= int64(max(cfg.TriggerSignInDays, 1))
	}
	return true
}

// InviterWithinLimits 邀请人这次还能不能拿奖励（已发放次数对照每日与累计上限）。
func InviterWithinLimits(cfg Config, grantedToday, grantedTotal int64) bool {
	if cfg.InviterDailyLimit > 0 && grantedToday >= int64(cfg.InviterDailyLimit) {
		return false
	}
	if cfg.InviterTotalLimit > 0 && grantedTotal >= int64(cfg.InviterTotalLimit) {
		return false
	}
	return true
}

// DueMilestones 累计成功邀请 granted 人时应发而未发的阶梯（按人数升序）。
func DueMilestones(cfg Config, granted int64, already []int) []Milestone {
	out := []Milestone{}
	for _, milestone := range cfg.Milestones {
		if int64(milestone.Count) <= granted && !slices.Contains(already, milestone.Count) {
			out = append(out, milestone)
		}
	}
	return out
}

// InviteeRewardsFor 填写这个码的人拿什么：活动码单独配了就用活动码的，否则跟随应用默认。
func InviteeRewardsFor(cfg Config, code *Code) []Reward {
	if code != nil && code.Rewards != nil {
		return NormalizeRewards(*code.Rewards)
	}
	return cfg.InviteeRewards
}
