package lottery

import (
	"fmt"
	"strings"
	"time"
)

// 抽奖次数（bonus）是分批记账的：每一次发放是一批（lottery_draw_lots），记着从哪来、是不是花积分或钱换来的、
// 还剩几次；每一次增减另记一条流水（lottery_draw_ledger）。lottery_draw_grants 只是这个人全部批次的汇总。
//
// 扣除顺序从头到尾只有一条，抽奖、连抽、管理员扣减都一样（ConsumptionOrder）：
//
//	今日免费次数 → 赠送的抽奖次数（先到先用）→ 兑换 / 购买的抽奖次数（先到先用）→ 积分
//
// 赠送的先于买来的：用户付过代价的那部分留到最后，管理员撤销兑换、对账时它们还在。

// 次数的来源（批次的 origin）。
const (
	TicketOriginRewardedAd  = "rewarded_ad"
	TicketOriginPointsStore = "points_store"
	TicketOriginCardKey     = "cardkey"
	TicketOriginInvite      = "invite"
	TicketOriginLeaderboard = "leaderboard"
	// TicketOriginLottery 抽中的「抽奖次数」，以及已拥有奖品按活动策略改发的次数
	TicketOriginLottery = "lottery"
	TicketOriginAdmin   = "admin"
	// TicketOriginItemGrant 物品发放规则发的次数（算赠送）
	TicketOriginItemGrant = "item_grant"
	// TicketOriginLegacy 分批记账之前的余额（迁移 000136 一次性转成一批）
	TicketOriginLegacy = "legacy"
)

// TicketOrigins 全部来源，按控制台筛选的展示顺序。
func TicketOrigins() []string {
	return []string{
		TicketOriginRewardedAd, TicketOriginLottery, TicketOriginInvite, TicketOriginLeaderboard,
		TicketOriginCardKey, TicketOriginPointsStore, TicketOriginItemGrant, TicketOriginAdmin, TicketOriginLegacy,
	}
}

// TicketOriginLabel 来源的展示名。
func TicketOriginLabel(origin string) string {
	switch origin {
	case TicketOriginRewardedAd:
		return "看视频"
	case TicketOriginPointsStore:
		return "积分兑换"
	case TicketOriginCardKey:
		return "卡密"
	case TicketOriginInvite:
		return "邀请奖励"
	case TicketOriginLeaderboard:
		return "排行榜奖励"
	case TicketOriginLottery:
		return "抽奖所得"
	case TicketOriginItemGrant:
		return "物品发放"
	case TicketOriginAdmin:
		return "系统发放"
	case TicketOriginLegacy:
		return "早期余额"
	}
	return "其他"
}

// 流水的种类。
const (
	// LedgerGrant 发放（+）
	LedgerGrant = "grant"
	// LedgerConsume 抽奖用掉（−1）
	LedgerConsume = "consume"
	// LedgerDeduct 管理员扣减（−）
	LedgerDeduct = "deduct"
)

// LedgerKindLabel 流水种类的展示名。
func LedgerKindLabel(kind string) string {
	switch kind {
	case LedgerGrant:
		return "获得"
	case LedgerConsume:
		return "抽奖使用"
	case LedgerDeduct:
		return "系统扣减"
	}
	return kind
}

// 管理员一次增减的上界。
const (
	MaxTicketAdjust      = 10000
	MaxTicketReasonRunes = 100
)

// TicketGrant 一次发放：发给谁、几次、从哪来。Paid 为用积分或钱换来的（排在赠送的之后用）。
type TicketGrant struct {
	AppID  int64
	UserID int64
	Amount int64
	Origin string
	Paid   bool
	// Ref 来源单号（兑换单号、卡号、抽奖单号、激励广告 transId…），对账用
	Ref string
	// Title 用户能看懂的一句话（「激励广告 每日抽奖」「积分兑换 抽奖次数 5 次」）
	Title    string
	Operator string
}

// TicketConsumeOrder 批次的使用顺序（SQL ORDER BY 片段）：赠送的先于买来的，同一档先到先用。
const TicketConsumeOrder = "paid ASC, id ASC"

// ConsumptionOrder 扣除顺序的说明（活动规则、文档、客户端共用这一份措辞）。
func ConsumptionOrder() []string {
	return []string{"今日免费次数", "赠送的抽奖次数", "兑换的抽奖次数", "积分"}
}

// TicketBalance 次数余额按档拆开：Gift + Purchased = Chances.Bonus。
type TicketBalance struct {
	// Gift 赠送的（看视频、抽中、邀请、排行榜、活动码、系统发放），先用
	Gift int64 `json:"gift"`
	// Purchased 用积分或钱换来的（积分商城、一卡一用的卡密），后用
	Purchased int64 `json:"purchased"`
}

// SplitTickets 由总余额与其中买来的部分拆出两档（买来的不会超过总数）。
func SplitTickets(total, purchased int64) TicketBalance {
	total = max(total, 0)
	purchased = min(max(purchased, 0), total)
	return TicketBalance{Gift: total - purchased, Purchased: purchased}
}

// TicketLot 一批次数。
type TicketLot struct {
	ID          int64     `json:"id"`
	Origin      string    `json:"origin"`
	OriginLabel string    `json:"originLabel"`
	Paid        bool      `json:"paid"`
	Title       string    `json:"title"`
	Ref         string    `json:"ref,omitempty"`
	Amount      int64     `json:"amount"`
	Remaining   int64     `json:"remaining"`
	CreatedAt   time.Time `json:"createdAt"`
}

// TicketEntry 一条次数流水。
type TicketEntry struct {
	ID          int64  `json:"id"`
	UserID      int64  `json:"userId,omitempty"`
	Account     string `json:"account,omitempty"`
	Nickname    string `json:"nickname,omitempty"`
	LotID       *int64 `json:"lotId,omitempty"`
	Kind        string `json:"kind"`
	KindLabel   string `json:"kindLabel"`
	Origin      string `json:"origin"`
	OriginLabel string `json:"originLabel"`
	Paid        bool   `json:"paid"`
	// Delta 这一条的增减（发放为正、使用与扣减为负）
	Delta        int64     `json:"delta"`
	BalanceAfter int64     `json:"balanceAfter"`
	Title        string    `json:"title"`
	Ref          string    `json:"ref,omitempty"`
	Operator     string    `json:"operator,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

// TicketAccount 一个人的抽奖次数：余额、两档拆分、累计、还有剩余的批次（按使用顺序）。
type TicketAccount struct {
	UserID        int64         `json:"userId,omitempty"`
	Account       string        `json:"account,omitempty"`
	Nickname      string        `json:"nickname,omitempty"`
	Balance       int64         `json:"balance"`
	Tickets       TicketBalance `json:"tickets"`
	TotalGranted  int64         `json:"totalGranted"`
	TotalConsumed int64         `json:"totalConsumed"`
	// Order 扣除顺序（ConsumptionOrder）
	Order []string `json:"order"`
	// Lots 还有剩余的批次，排在前面的先用
	Lots      []TicketLot `json:"lots"`
	UpdatedAt *time.Time  `json:"updatedAt,omitempty"`
}

// TicketQuery 次数账户 / 流水筛选（管理端）。
type TicketQuery struct {
	AppID   int64
	UserID  int64
	Kind    string
	Origin  string
	Keyword string
	Page    int
	Limit   int
}

// TicketAdjust 管理员增减次数：Amount 为正是发放、为负是扣减（按扣除顺序从赠送的开始扣）。
type TicketAdjust struct {
	AppID    int64
	UserID   int64
	Amount   int64
	Paid     bool
	Reason   string
	Operator string
}

// Normalize 校验并整理管理员的增减。
func (a TicketAdjust) Normalize() (TicketAdjust, error) {
	a.Reason = strings.TrimSpace(a.Reason)
	switch {
	case a.UserID <= 0:
		return a, fmt.Errorf("请选择用户")
	case a.Amount == 0:
		return a, fmt.Errorf("增减次数不能为 0")
	case a.Amount > MaxTicketAdjust || a.Amount < -MaxTicketAdjust:
		return a, fmt.Errorf("一次最多增减 %d 次", MaxTicketAdjust)
	case a.Reason == "":
		return a, fmt.Errorf("请填写原因")
	case len([]rune(a.Reason)) > MaxTicketReasonRunes:
		return a, fmt.Errorf("原因不能超过 %d 个字", MaxTicketReasonRunes)
	}
	if a.Amount < 0 {
		a.Paid = false
	}
	return a, nil
}

// TicketOriginStat 一个来源的发放与剩余（管理端总览）。
type TicketOriginStat struct {
	Origin      string `json:"origin"`
	OriginLabel string `json:"originLabel"`
	Paid        bool   `json:"paid"`
	Granted     int64  `json:"granted"`
	Remaining   int64  `json:"remaining"`
	Users       int64  `json:"users"`
}

// TicketOverview 次数总览（管理端）。
type TicketOverview struct {
	Holders      int64              `json:"holders"`
	Outstanding  int64              `json:"outstanding"`
	Tickets      TicketBalance      `json:"tickets"`
	GrantedToday int64              `json:"grantedToday"`
	UsedToday    int64              `json:"usedToday"`
	Origins      []TicketOriginStat `json:"origins"`
	Order        []string           `json:"order"`
}

// DrawTicket 一次抽奖用的是哪一批次数（只有 source = bonus 时有）。
type DrawTicket struct {
	LotID       int64  `json:"lotId,omitempty"`
	Origin      string `json:"origin"`
	OriginLabel string `json:"originLabel"`
	Paid        bool   `json:"paid"`
}
