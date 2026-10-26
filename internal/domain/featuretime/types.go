// Package featuretime 功能时长：按实际使用的秒数计量的功能权益。
//
// ── 与会员、功能解锁的区别 ──
//
// 会员与功能解锁回答「这段时间里能不能用」，到期之前用多用少都一样；功能时长回答「还能用多久」，
// 只在真正使用时才扣。典型场景是在线听歌：非会员看一次激励广告换 60 分钟，按实际播放的时长扣，
// 暂停期间不扣。会员（或单独持有该功能的人）免扣，余额原样留着，会员到期后接着用。
//
// ── 租约 ──
//
// 客户端不能「先用后报」：那样只要不报，就等于无限使用。于是改成**先预扣再用**：
// 开始使用前向服务端租一段（≤ MaxLeaseSeconds 秒），这段时间从余额里扣走；用完再租下一段；
// 停下来时结算，没用完的退回余额。客户端拿不到任何「没扣就能用」的路径，
// 不结算只会让用户自己吃亏（那一段不退）。
//
// 租约在 LeaseTTL 之后过期，过期的租约结算时不再退款。过期不需要定时任务去翻状态：
// 结算时拿 expires_at 与当前时间比一下就知道，状态在第一次结算时写成 expired。
//
// ── 账目 ──
//
// 每个 (应用, 用户, 功能) 一行余额，只由发放（目前是激励广告）增加、由租约扣减，
// 每一次变动都在流水里记一笔（earn / lease / refund），余额永远等于流水之和。
package featuretime

import (
	"fmt"
	"strings"
	"time"

	vipdomain "aegis/internal/domain/vip"
)

const (
	// MaxLeaseSeconds 一次最多租多少秒。
	//
	// 租得越长，「租了不用、也不结算」时用户白白损失的就越多；租得越短，客户端续租越频繁。
	// 十分钟是两者的折中：一首歌四五分钟，一次续租覆盖两首。
	MaxLeaseSeconds = 600
	// LeaseTTL 租约多久之后过期。过期的租约不再退款。
	//
	// 远长于 MaxLeaseSeconds：正常使用时一段租约十分钟内就会用完并结算，
	// 两小时只是给「切到后台、网络断了一阵再回来结算」的情况留余地。
	LeaseTTL = 2 * time.Hour
	// LeaseNoPrefix 租约号前缀。
	LeaseNoPrefix = "FTL"
)

// 流水类型。
const (
	KindEarn   = "earn"   // 发放（看激励广告等），delta 为正
	KindLease  = "lease"  // 租出，delta 为负
	KindRefund = "refund" // 结算时退回没用完的部分，delta 为正
)

// 租约状态。
const (
	LeaseOpen    = "open"    // 已租出、尚未结算（过期与否看 expires_at）
	LeaseSettled = "settled" // 已结算，没用完的已退回
	LeaseExpired = "expired" // 过期后才来结算，不退款
)

// 发放来源（流水的 ref_type）。
const (
	RefRewardedAd = "rewarded_ad"
	RefLease      = "lease"
	RefLottery    = "lottery"
)

// Status 一个用户在一个功能上的时长概况。
type Status struct {
	Feature string `json:"feature"`
	// Name 功能目录里的展示名，目录里没有时为空串
	Name           string `json:"name"`
	BalanceSeconds int64  `json:"balanceSeconds"`
	// Exempt 免扣：当前是会员，或单独持有该功能（含功能解锁）。免扣时租约接口不扣余额
	Exempt        bool  `json:"exempt"`
	EarnedSeconds int64 `json:"earnedSeconds"`
	// UsedSeconds 实际用掉的：租出减去退回
	UsedSeconds int64 `json:"usedSeconds"`
}

// Account 余额账户（一行）。
type Account struct {
	AppID          int64
	UserID         int64
	Feature        string
	BalanceSeconds int64
	EarnedSeconds  int64
	UsedSeconds    int64
	UpdatedAt      time.Time
}

// Lease 一次租约的结果。
//
// 免扣时 Exempt 为 true、LeaseNo 与 ExpiresAt 为空、Seconds 为 0：没有扣余额，也没有建租约，
// 客户端直接使用即可（下一次续租时服务端会重新判定，会员到期了就开始扣）。
type Lease struct {
	Exempt         bool   `json:"exempt"`
	LeaseNo        string `json:"leaseNo"`
	Seconds        int    `json:"seconds"`
	BalanceSeconds int64  `json:"balanceSeconds"`
	// ExpiresAt RFC3339（UTC），免扣时为空串
	ExpiresAt string `json:"expiresAt"`
}

// LeaseRecord 租约行。
type LeaseRecord struct {
	ID              int64
	LeaseNo         string
	AppID           int64
	UserID          int64
	Feature         string
	Seconds         int
	UsedSeconds     int
	RefundedSeconds int
	Status          string
	ExpiresAt       time.Time
	CreatedAt       time.Time
	SettledAt       *time.Time
}

// Settlement 一次结算的结果。重复结算原样返回第一次的结论，BalanceSeconds 是当前余额。
type Settlement struct {
	LeaseNo         string `json:"leaseNo"`
	Feature         string `json:"feature"`
	Status          string `json:"status"`
	Seconds         int    `json:"seconds"`
	UsedSeconds     int    `json:"usedSeconds"`
	RefundedSeconds int    `json:"refundedSeconds"`
	BalanceSeconds  int64  `json:"balanceSeconds"`
}

// LeaseInput 租一段时长。Seconds 已经过 ClampLeaseSeconds。
type LeaseInput struct {
	AppID   int64
	UserID  int64
	Feature string
	Seconds int
	Now     time.Time
}

// SettleInput 结算一个租约。UsedSeconds 是客户端上报的原值，夹取在仓储层按租约的秒数做。
type SettleInput struct {
	AppID       int64
	UserID      int64
	LeaseNo     string
	UsedSeconds int
	Now         time.Time
}

// NormalizeFeature 清洗并校验功能标识：去空白、转小写，形状须合 vip.FeatureTagPattern。
func NormalizeFeature(feature string) (string, error) {
	feature = strings.ToLower(strings.TrimSpace(feature))
	if !vipdomain.FeatureTagPattern.MatchString(feature) {
		return "", fmt.Errorf("功能标识无效")
	}
	return feature, nil
}

// ClampLeaseSeconds 一次租多少秒：小于 1 是参数错误，超过 MaxLeaseSeconds 按上限截断。
//
// 截断而不是报错：客户端要多少是它的估计，服务端只管不让一次扣太多，
// 报错只会让一个想多租一点的客户端什么都租不到。
func ClampLeaseSeconds(seconds int) (int, error) {
	if seconds < 1 {
		return 0, fmt.Errorf("租用时长需为 1–%d 秒", MaxLeaseSeconds)
	}
	return min(seconds, MaxLeaseSeconds), nil
}

// Settle 结算的结论：状态、实际用掉多少、退回多少。纯函数。
//
// usedSeconds 夹到 [0, 租约秒数]；租约已过期（now 不早于 expires_at）时不退款，
// 用掉多少照实记录，只是不再退回。
func Settle(lease LeaseRecord, usedSeconds int, now time.Time) (status string, used int, refund int) {
	used = max(0, min(usedSeconds, lease.Seconds))
	if !now.Before(lease.ExpiresAt) {
		return LeaseExpired, used, 0
	}
	return LeaseSettled, used, lease.Seconds - used
}

// Exempt 免扣判定：当前是会员，或权益里带这个功能（会员段的功能与功能解锁都算）。
func Exempt(entitlement vipdomain.Entitlement, feature string) bool {
	return entitlement.IsVIP || entitlement.HasFeature(feature)
}

// FormatMinutes 秒数折成整分钟（向下取整），用于发放结果里的「余额 N 分钟」。
func FormatMinutes(seconds int64) int64 {
	if seconds <= 0 {
		return 0
	}
	return seconds / 60
}
