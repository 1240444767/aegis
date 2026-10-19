// Package leaderboard 积分排行榜：榜单目录、周期、展示名、设置与奖励档位的校验、积分口径。
//
// 规则见 docs/leaderboard.md。这个包只放类型与纯函数；名次在仓储层用一条 SQL 算出来，
// 结算（快照 + 发奖）在仓储层的事务里完成。
package leaderboard

import (
	"time"

	"aegis/internal/domain/invite"
)

// 积分流水与账本上的来源标记：排行榜奖励发的积分记在这个分类下，
// 周期榜统计「获得的积分」时排除它（否则奖励会滚雪球）。
const (
	IntegralCategory = "leaderboard"
	SourceType       = "leaderboard"
)

// 展示身份。
const (
	IdentityNickname = "nickname" // 有昵称显示昵称，否则账号脱敏
	IdentityMasked   = "masked"   // 一律账号脱敏
)

// 快照状态。
const (
	SnapshotSettling = "settling"
	SnapshotSettled  = "settled"
)

// 快照条目的奖励状态。
const (
	RewardNone    = "none"    // 没落在任何档位里
	RewardPending = "pending" // 等结算循环发放
	RewardGranted = "granted" // 已发放
	RewardFailed  = "failed"  // 发放失败（原因在 rewardError），管理员可重试
	RewardSkipped = "skipped" // 发放时已被移出排行榜或已注销
)

// RewardStatuses 全部奖励状态，顺序即控制台筛选项的顺序。
func RewardStatuses() []string {
	return []string{RewardPending, RewardGranted, RewardFailed, RewardSkipped, RewardNone}
}

// 字段上限与默认值。
const (
	DefaultTopLimit = 100
	MinTopLimit     = 10
	MaxTopLimit     = 500
	DefaultMinValue = 1
	MaxMinValue     = 1_000_000_000
	// MaxTiers 一个榜最多几档奖励；每档的权益数沿用邀请的上限（invite.MaxRewardsPerList）
	MaxTiers = 10
	// MaxPageLimit 用户端每页最多几行
	MaxPageLimit = 50
	// MaxAdminPageLimit 管理端每页最多几行
	MaxAdminPageLimit = 100
	// MaxReasonRunes 移出原因的长度上限
	MaxReasonRunes = 200
	// MaxExclusionBatch 一次最多移出几个人
	MaxExclusionBatch = 100
	// SettleGrace 周期结束后等多久再结算：给结束前一刻的流水留出落库的时间
	SettleGrace = 5 * time.Minute
)

// AnonymousName 匿名上榜的人对别人显示的名字。
const AnonymousName = "匿名用户"

// Reward 一项权益，形状与邀请相同（同一套目录、同一套账本）。
type Reward = invite.Reward

// RewardResult 一项权益的发放结果。
type RewardResult = invite.RewardResult

// ClientReward 用户端看到的一项权益。
type ClientReward = invite.ClientReward

// BoardSetting 一个榜单的开关。设置里的顺序即客户端标签的顺序。
type BoardSetting struct {
	Key     string `json:"key"`
	Enabled bool   `json:"enabled"`
}

// Tier 一档奖励：第 RankFrom 到第 RankTo 名（含两端）各发一份 Rewards。
type Tier struct {
	RankFrom int      `json:"rankFrom"`
	RankTo   int      `json:"rankTo"`
	Rewards  []Reward `json:"rewards"`
}

// BoardRewards 一个周期榜的奖励配置。
type BoardRewards struct {
	Enabled bool   `json:"enabled"`
	Tiers   []Tier `json:"tiers"`
}

// Config 应用的排行榜配置（除总开关以外的全部）。
type Config struct {
	Boards           []BoardSetting          `json:"boards"`
	TopLimit         int                     `json:"topLimit"`
	MinValue         int64                   `json:"minValue"`
	Identity         string                  `json:"identity"`
	AllowAnonymous   bool                    `json:"allowAnonymous"`
	CountAdminAdjust bool                    `json:"countAdminAdjust"`
	Rewards          map[string]BoardRewards `json:"rewards"`
}

// Settings 应用的排行榜开关与配置。
type Settings struct {
	AppID   int64 `json:"appid"`
	Enabled bool  `json:"enabled"`
	Config
	UpdatedBy string     `json:"updatedBy,omitempty"`
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
	// Configured 是否保存过（没保存过时按关闭处理）
	Configured bool `json:"configured"`
}

// Entry 榜上的一行（仓储算出来的原始事实，进 Redis 缓存；不直接出网）。
type Entry struct {
	Rank   int   `json:"rank"`
	UserID int64 `json:"userId"`
	// Name 按应用的展示身份算好的名字（昵称或账号脱敏），不是原始账号
	Name      string `json:"name"`
	RawAvatar string `json:"rawAvatar,omitempty"`
	Value     int64  `json:"value"`
	// Anonymous 用户的「匿名上榜」偏好（是否生效还要看应用是否允许匿名）
	Anonymous bool `json:"anonymous,omitempty"`
	// Experience 等级榜的同级比较依据，也用于算等级榜的差距
	Experience int64  `json:"experience,omitempty"`
	LevelName  string `json:"levelName,omitempty"`
	// LastAt 周期榜里这个人最后一次获得积分的时刻（同名次内先到先得）
	LastAt *time.Time `json:"lastAt,omitempty"`
}

// AdminEntry 管理端看到的一行：带真实账号与用户 ID，并标出匿名与被移出。
type AdminEntry struct {
	// Rank 被移出的人不参与排名，为空
	Rank       *int       `json:"rank"`
	UserID     int64      `json:"userId"`
	Account    string     `json:"account"`
	Nickname   string     `json:"nickname"`
	Name       string     `json:"name"`
	Avatar     string     `json:"avatar"`
	RawAvatar  string     `json:"-"`
	Value      int64      `json:"value"`
	Anonymous  bool       `json:"anonymous"`
	Excluded   bool       `json:"excluded"`
	LevelName  string     `json:"levelName,omitempty"`
	LastAt     *time.Time `json:"lastAt,omitempty"`
	Experience int64      `json:"-"`
}

// Standing 一个人在某个榜某个周期的实时位置（me 每次实时算，不进缓存）。
type Standing struct {
	UserID     int64
	Account    string
	Nickname   string
	RawAvatar  string
	Anonymous  bool
	Value      int64
	Experience int64
	Level      int
	LevelName  string
	// Eligible 本应用启用、未封禁、未冻结（不看移出与 minValue）
	Eligible bool
	Excluded bool
	// Rank 不在榜上（取值不足 / 被移出 / 不可参与）时为空
	Rank *int
	// GapToPrev 距上一名（名次更靠前的那个取值）还差多少
	GapToPrev int64
}

// Snapshot 一期结算快照。
type Snapshot struct {
	ID           int64      `json:"id"`
	AppID        int64      `json:"appid"`
	Board        string     `json:"board"`
	PeriodKey    string     `json:"periodKey"`
	PeriodStart  time.Time  `json:"periodStart"`
	PeriodEnd    time.Time  `json:"periodEnd"`
	Status       string     `json:"status"`
	Participants int64      `json:"participants"`
	Tiers        []Tier     `json:"tiers"`
	SettledBy    string     `json:"settledBy,omitempty"`
	SettledAt    *time.Time `json:"settledAt,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	// 发放统计（列表接口填）
	Stats *SnapshotStats `json:"stats,omitempty"`
}

// SnapshotStats 一期的发放统计：应发 = pending + granted + failed + skipped。
type SnapshotStats struct {
	Entries  int64 `json:"entries"`
	Rewarded int64 `json:"rewarded"`
	Pending  int64 `json:"pending"`
	Granted  int64 `json:"granted"`
	Failed   int64 `json:"failed"`
	Skipped  int64 `json:"skipped"`
}

// SnapshotEntry 快照里的一行。展示名、头像、匿名在结算那一刻冻结。
type SnapshotEntry struct {
	SnapshotID    int64          `json:"snapshotId"`
	Rank          int            `json:"rank"`
	Position      int            `json:"position"`
	UserID        int64          `json:"userId"`
	Value         int64          `json:"value"`
	DisplayName   string         `json:"displayName"`
	RawAvatar     string         `json:"-"`
	Anonymous     bool           `json:"anonymous"`
	LevelName     string         `json:"levelName,omitempty"`
	Rewards       []Reward       `json:"rewards"`
	RewardStatus  string         `json:"rewardStatus"`
	RewardResults []RewardResult `json:"rewardResults"`
	RewardError   string         `json:"rewardError,omitempty"`
	GrantedAt     *time.Time     `json:"grantedAt,omitempty"`
	// Account 管理端才有（关联当前用户表；已注销的为空）
	Account string `json:"account,omitempty"`
	Avatar  string `json:"avatar,omitempty"`
}

// Exclusion 移出名单的一条。
type Exclusion struct {
	AppID     int64     `json:"appid"`
	UserID    int64     `json:"userId"`
	Account   string    `json:"account"`
	Nickname  string    `json:"nickname"`
	Reason    string    `json:"reason"`
	Operator  string    `json:"operator"`
	CreatedAt time.Time `json:"createdAt"`
}

// Page 分页结果。
type Page[T any] struct {
	Items      []T   `json:"items"`
	Total      int64 `json:"total"`
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	TotalPages int   `json:"totalPages"`
}

// TotalPages 按每页 limit 行算总页数。
func TotalPages(total int64, limit int) int {
	if limit <= 0 || total <= 0 {
		return 0
	}
	return int((total + int64(limit) - 1) / int64(limit))
}

// NormalizePage 页码与每页行数的兜底：page ≥ 1，1 ≤ limit ≤ max（0 取 fallback）。
func NormalizePage(page, limit, fallback, max int) (int, int) {
	if page < 1 {
		page = 1
	}
	if limit <= 0 {
		limit = fallback
	}
	if limit > max {
		limit = max
	}
	return page, limit
}
