// Package cardkey 定义应用级卡密的领域类型。
//
// 一张卡有两种形态：授权卡（卡即登录凭证）与兑换卡（给已登录用户发权益）。
// 两者共用同一套生成、作废、核销与权益目录，只有「怎么被使用」不同。
//
// 兑换卡又分两种用法，差别只在 MaxUses：
//
//	一卡一用   MaxUses = 1，卖给一个人的码（激活码、礼品卡）
//	活动码     MaxUses > 1，一个码被很多人各用一次（直播口令、群发福利）
package cardkey

import (
	"time"

	"github.com/shopspring/decimal"
)

// 卡的形态。
const (
	// KindLogin 授权卡：卡本身就是登录凭证，首次使用自动建号并绑定。
	KindLogin = "login"
	// KindRedeem 兑换卡：给已登录用户发权益。
	KindRedeem = "redeem"
)

// 卡的状态。
//
// 「已过期」刻意不在其中：它是 ExpiresAt 与当前时间比出来的结论，
// 存成状态就需要一个定时任务去翻它，而那个任务停掉的表现是「过期卡还能用」。
//
// 活动码在用满之前一直是 unused（UseCount 记已用次数），用满才变成 used。
const (
	StatusUnused   = "unused"
	StatusActive   = "active"
	StatusUsed     = "used"
	StatusDisabled = "disabled"
)

// 有效期模式。
const (
	// ValidityPermanent 永不过期。
	ValidityPermanent = "permanent"
	// ValidityFixedUntil 批次统一到期，生成时即写死。
	ValidityFixedUntil = "fixed_until"
	// ValidityFromFirstUse 激活即计时。卖出去到被使用之间的时间不算进授权期。
	ValidityFromFirstUse = "days_from_first_use"
)

// 核销来源。
const (
	SourceRedeem = "redeem"
	SourceLogin  = "login"
	SourceAdmin  = "admin"
)

// 批次状态。
const (
	BatchActive   = "active"
	BatchDisabled = "disabled"
)

// 卡面字符集。
const (
	// CharsetSafe 剔掉 I / O / 0 / 1 的 32 个字符：要被人抄写的卡用它。
	CharsetSafe = "safe"
	// CharsetDigits 纯数字：电话报码、刮刮卡。同样位数下组合数远少于 safe。
	CharsetDigits = "digits"
	// CharsetLetters 纯字母（同样剔掉 I / O）。
	CharsetLetters = "letters"
)

// 批次的卡面来源。
const (
	// OriginGenerated 按格式随机生成。
	OriginGenerated = "generated"
	// OriginImported 导入现成的卡面（从别的平台迁过来、或运营自己定的口令）。
	OriginImported = "imported"
)

// 领取对象。
const (
	AudienceAll    = "all"
	AudienceVip    = "vip"
	AudienceNonVip = "non_vip"
)

// 设备解绑来源。
const (
	UnbindSelf  = "self"
	UnbindAdmin = "admin"
)

// 有效期调整方式（管理员调整单卡到期时间）。
const (
	ExpiryExtend    = "extend"
	ExpirySet       = "set"
	ExpiryPermanent = "permanent"
)

// SelfUnbindWindow 自助解绑的限次窗口。
const SelfUnbindWindow = 30 * 24 * time.Hour

// Reward 一项权益。
//
// 取值字段互斥，由权益目录的 Value 决定用哪一个：整数量走 Amount，
// 金额走 Money，引用既有实体走 RefID，功能解锁走 Feature + Amount。
// 合成一个 any 字段会让「30 天」与「套餐 30 号」在 JSON 里长得一模一样。
type Reward struct {
	Type   string          `json:"type"`
	Amount int64           `json:"amount,omitempty"`
	Money  decimal.Decimal `json:"money,omitempty"`
	RefID  int64           `json:"refId,omitempty"`
	// Feature 功能标识，与 Amount 一起用（取值形态 ValueFeature）：卡密的「功能解锁」（Amount 为天数，0 为永久），
	// 以及激励广告的「功能时长」（Amount 为分钟，见 rewardedad.RewardFeatureMinutes）。
	Feature string `json:"feature,omitempty"`
}

// RewardResult 一项权益的实际发放结果，写进核销流水。
//
// 排障时要靠它回答「用户说没到账」到底是没发、发少了、还是发到别处了 ——
// 只记「发过什么」而不记「发成了什么」的话，这个问题没有答案。
type RewardResult struct {
	Type   string `json:"type"`
	Label  string `json:"label"`
	Detail string `json:"detail"`
	// TransactionNo 对应的账本流水号（会员 / 积分 / 钱包各有一套），没有则为空。
	TransactionNo string `json:"transactionNo,omitempty"`
	// ExpireAt 功能解锁 / 授权时长发放后的到期时间（永久为空）。
	ExpireAt *time.Time `json:"expireAt,omitempty"`
}

// Batch 一批卡密。
type Batch struct {
	ID     int64  `json:"id"`
	AppID  int64  `json:"appid"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Remark string `json:"remark,omitempty"`

	// 卡面规则。只作留档：改这里不会重排已有的卡，补发时照原样式生成。
	CodePrefix    string `json:"codePrefix,omitempty"`
	Segments      int    `json:"segments"`
	SegmentLength int    `json:"segmentLength"`
	Charset       string `json:"charset"`
	Origin        string `json:"origin"`

	Rewards    []Reward `json:"rewards"`
	MaxDevices int      `json:"maxDevices"`
	// MaxUses 每张卡能被几个人各用一次。1 为一卡一用，>1 为活动码；授权卡恒为 1。
	MaxUses int `json:"maxUses"`

	// 领取规则（只对兑换生效；授权卡首登建号时没有「已有账号」可判）。
	PerUserLimit int        `json:"perUserLimit"`
	Audience     string     `json:"audience"`
	StartsAt     *time.Time `json:"startsAt,omitempty"`

	ValidityMode string     `json:"validityMode"`
	ValidityDays int        `json:"validityDays"`
	ValidUntil   *time.Time `json:"validUntil,omitempty"`

	// SelfUnbindLimit 授权卡用户每 30 天能在客户端自助解绑几次设备；0 为不允许自助解绑。
	SelfUnbindLimit int `json:"selfUnbindLimit"`

	Channel   string          `json:"channel,omitempty"`
	FaceValue decimal.Decimal `json:"faceValue"`

	Total     int    `json:"total"`
	Status    string `json:"status"`
	CreatedBy string `json:"createdBy,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// Stats 各状态的卡数量，列表接口随批次一并下发。
	Stats *BatchStats `json:"stats,omitempty"`
}

// BatchStats 一批卡的核销进度。
type BatchStats struct {
	Total    int64 `json:"total"`
	Unused   int64 `json:"unused"`
	Active   int64 `json:"active"`
	Used     int64 `json:"used"`
	Disabled int64 `json:"disabled"`
	// Expired 已过期（按 ExpiresAt 算出来的，不是一档状态）。
	Expired int64 `json:"expired"`
	// Distributed 已经提取（交给渠道）的卡。
	Distributed int64 `json:"distributed"`
	// Available 还能提取的卡：未使用、未提取、未过期。
	Available int64 `json:"available"`
	// Uses 累计被使用的人次（活动码一张卡算多次）。
	Uses int64 `json:"uses"`
}

// Card 一张卡。
type Card struct {
	ID      int64  `json:"id"`
	AppID   int64  `json:"appid"`
	BatchID int64  `json:"batchId"`
	Code    string `json:"code"`
	Kind    string `json:"kind"`
	Status  string `json:"status"`

	BoundUserID *int64 `json:"boundUserId,omitempty"`
	MaxDevices  int    `json:"maxDevices"`
	MaxUses     int    `json:"maxUses"`
	UseCount    int    `json:"useCount"`

	ActivatedAt      *time.Time `json:"activatedAt,omitempty"`
	ExpiresAt        *time.Time `json:"expiresAt,omitempty"`
	UsedAt           *time.Time `json:"usedAt,omitempty"`
	DisabledAt       *time.Time `json:"disabledAt,omitempty"`
	DisabledReason   string     `json:"disabledReason,omitempty"`
	DistributedAt    *time.Time `json:"distributedAt,omitempty"`
	DistributionNote string     `json:"distributionNote,omitempty"`
	Remark           string     `json:"remark,omitempty"`

	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`

	// 以下为查询时联表补齐，不落库。
	BatchName    string `json:"batchName,omitempty"`
	BoundAccount string `json:"boundAccount,omitempty"`
	DeviceCount  int    `json:"deviceCount"`
}

// Expired 这张卡是否已过授权期。
//
// 写成方法而不是让调用方各自比时间：判据只有一处，
// 且「没有到期时间 = 永久有效」这条容易被写反。
func (c *Card) Expired(now time.Time) bool {
	return c.ExpiresAt != nil && !c.ExpiresAt.After(now)
}

// RemainingUses 还能被几个人使用。
func (c *Card) RemainingUses() int {
	if c.Status == StatusUsed || c.Status == StatusDisabled {
		return 0
	}
	if left := c.MaxUses - c.UseCount; left > 0 {
		return left
	}
	return 0
}

// Device 一张卡绑定的一台设备。
type Device struct {
	ID          int64     `json:"id"`
	CardKeyID   int64     `json:"cardKeyId"`
	DeviceID    string    `json:"deviceId"`
	DeviceName  string    `json:"deviceName,omitempty"`
	FirstSeenAt time.Time `json:"firstSeenAt"`
	LastSeenAt  time.Time `json:"lastSeenAt"`
	SeenCount   int64     `json:"seenCount"`
}

// Unbind 一次设备解绑。
type Unbind struct {
	ID         int64     `json:"id"`
	CardKeyID  int64     `json:"cardKeyId"`
	DeviceID   string    `json:"deviceId"`
	DeviceName string    `json:"deviceName,omitempty"`
	Source     string    `json:"source"`
	Operator   string    `json:"operator,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}

// Redemption 一条核销流水。
type Redemption struct {
	ID        int64          `json:"id"`
	AppID     int64          `json:"appid"`
	CardKeyID int64          `json:"cardKeyId"`
	BatchID   *int64         `json:"batchId,omitempty"`
	Code      string         `json:"code"`
	UserID    int64          `json:"userId"`
	Rewards   []Reward       `json:"rewards"`
	Results   []RewardResult `json:"results"`
	Source    string         `json:"source"`
	DeviceID  string         `json:"deviceId,omitempty"`
	ClientIP  string         `json:"clientIp,omitempty"`
	UserAgent string         `json:"userAgent,omitempty"`
	Operator  string         `json:"operator,omitempty"`
	CreatedAt time.Time      `json:"createdAt"`

	// 查询时联表补齐。
	Account   string `json:"account,omitempty"`
	Nickname  string `json:"nickname,omitempty"`
	BatchName string `json:"batchName,omitempty"`
	Kind      string `json:"kind,omitempty"`
}

// CardDetail 一张卡的全貌：控制台单卡抽屉一次拿齐。
type CardDetail struct {
	Card        Card         `json:"card"`
	Batch       Batch        `json:"batch"`
	Devices     []Device     `json:"devices"`
	Redemptions []Redemption `json:"redemptions"`
	Unbinds     []Unbind     `json:"unbinds"`
}

// GenerateInput 生成（或导入）一批卡密。
type GenerateInput struct {
	AppID         int64
	Name          string
	Kind          string
	Remark        string
	Count         int
	CodePrefix    string
	Segments      int
	SegmentLength int
	Charset       string
	// Codes 导入的卡面。非空时按导入处理：Count 与卡面格式不再生效。
	Codes           []string
	Rewards         []Reward
	MaxDevices      int
	MaxUses         int
	PerUserLimit    int
	Audience        string
	StartsAt        *time.Time
	ValidityMode    string
	ValidityDays    int
	ValidUntil      *time.Time
	SelfUnbindLimit int
	Channel         string
	FaceValue       decimal.Decimal
	Operator        string

	// Origin 由服务层按 Codes 是否为空填写。
	Origin string
}

// BatchUpdateInput 修改批次。
//
// 能改的都是「之后才生效」的东西：名称、渠道、权益（核销流水存快照，已发出的不受影响）、
// 领取规则、生效时间。卡面格式、形态、有效期模式与单卡设备数不能改 —— 它们已经落在每一张卡上了。
type BatchUpdateInput struct {
	AppID           int64
	BatchID         int64
	Name            string
	Remark          string
	Channel         string
	FaceValue       decimal.Decimal
	Rewards         []Reward
	PerUserLimit    int
	Audience        string
	StartsAt        *time.Time
	ValidUntil      *time.Time
	SelfUnbindLimit int
}

// AppendInput 给一批卡补发。
type AppendInput struct {
	AppID    int64
	BatchID  int64
	Count    int
	Operator string
}

// ExtractInput 从一批里提取一些还没交给渠道的卡。
type ExtractInput struct {
	AppID   int64
	BatchID int64
	Count   int
	Note    string
}

// CardUpdateInput 管理员调整一张卡。字段为 nil 表示不改。
type CardUpdateInput struct {
	AppID      int64
	CardID     int64
	Remark     *string
	MaxDevices *int
	// ExpiryAction 为空表示不改到期时间。
	ExpiryAction string
	ExtendDays   int
	ExpiresAt    *time.Time
}

// RedeemInput 一次兑换核销。
type RedeemInput struct {
	AppID     int64
	Code      string
	UserID    int64
	Source    string
	DeviceID  string
	ClientIP  string
	UserAgent string
	Operator  string
}

// ActivateLoginInput 授权卡的一次登录（首次为激活，之后为续用）。
type ActivateLoginInput struct {
	AppID      int64
	CardID     int64
	UserID     int64
	DeviceID   string
	DeviceName string
	ClientIP   string
	UserAgent  string
}

// UnbindInput 解绑一台设备。
type UnbindInput struct {
	AppID    int64
	CardID   int64
	DeviceID string
	// UserID 自助解绑时为本人，校验这张卡属于他；管理员解绑为 0。
	UserID   int64
	Source   string
	Operator string
	// Limit 自助解绑的窗口内次数上限（由批次配置决定，服务层填写）。
	Limit int
}

// CardQuery 单卡列表的筛选条件。
type CardQuery struct {
	AppID   int64
	BatchID int64
	Status  string
	Kind    string
	Keyword string
	UserID  int64
	// Distributed "yes" 只看已提取 / "no" 只看未提取 / 空不限。
	Distributed string
	Page        int
	Limit       int
}

// RedemptionQuery 核销记录的筛选条件。
type RedemptionQuery struct {
	AppID   int64
	BatchID int64
	CardID  int64
	UserID  int64
	Source  string
	Keyword string
	From    *time.Time
	To      *time.Time
	Page    int
	Limit   int
}

// ExportQuery 导出的范围。
type ExportQuery struct {
	AppID   int64
	BatchID int64
	// Status 空为全部；unused 只导出未使用（且未过期）的卡。
	Status string
	// OnlyUndistributed 只导出还没交给渠道的卡。
	OnlyUndistributed bool
}

// CardPage 单卡分页结果。
type CardPage struct {
	Items []Card `json:"items"`
	Total int64  `json:"total"`
	Page  int    `json:"page"`
	Limit int    `json:"limit"`
}

// RedemptionPage 核销记录分页结果。
type RedemptionPage struct {
	Items []Redemption `json:"items"`
	Total int64        `json:"total"`
	Page  int          `json:"page"`
	Limit int          `json:"limit"`
}

// RedeemResult 一次核销的结果。
type RedeemResult struct {
	Code      string         `json:"code"`
	Kind      string         `json:"kind"`
	BatchName string         `json:"batchName,omitempty"`
	Results   []RewardResult `json:"results"`
	// ExpiresAt 授权卡核销后的授权到期时间。
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	// RedeemedAt 核销时间。
	RedeemedAt time.Time `json:"redeemedAt"`
}

// LoginAuthorization 授权卡登录成功时的授权快照，随登录结果下发给客户端。
type LoginAuthorization struct {
	Code        string     `json:"code"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
	MaxDevices  int        `json:"maxDevices"`
	DeviceCount int        `json:"deviceCount"`
	// FirstActivation 本次是不是这张卡的首次激活（首次才发权益）。
	FirstActivation bool `json:"firstActivation"`
}

// ── 统计 ──

// Stats 卡密概览。
type Stats struct {
	Batches         int64           `json:"batches"`
	Cards           int64           `json:"cards"`
	Unused          int64           `json:"unused"`
	Available       int64           `json:"available"`
	Distributed     int64           `json:"distributed"`
	Disabled        int64           `json:"disabled"`
	Expired         int64           `json:"expired"`
	ActiveLicenses  int64           `json:"activeLicenses"`
	Redemptions     int64           `json:"redemptions"`
	RedemptionsDay  int64           `json:"redemptionsToday"`
	Users           int64           `json:"users"`
	FaceValueIssued decimal.Decimal `json:"faceValueIssued"`
	FaceValueUsed   decimal.Decimal `json:"faceValueUsed"`
	Trend           []TrendPoint    `json:"trend"`
	Sources         []SourceCount   `json:"sources"`
	TopBatches      []BatchUsage    `json:"topBatches"`
}

// TrendPoint 某一天的核销量。
type TrendPoint struct {
	Day         string `json:"day"`
	Redemptions int64  `json:"redemptions"`
	Users       int64  `json:"users"`
}

// SourceCount 核销来源分布。
type SourceCount struct {
	Source string `json:"source"`
	Count  int64  `json:"count"`
}

// BatchUsage 统计窗口内核销最多的批次。
type BatchUsage struct {
	BatchID     int64  `json:"batchId"`
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Redemptions int64  `json:"redemptions"`
}

// ── 用户端 ──

// ClientReward 一项权益的人话描述（预览时展示「这张卡会发什么」）。
type ClientReward struct {
	Type   string `json:"type"`
	Label  string `json:"label"`
	Detail string `json:"detail"`
}

// Preview 兑换前的预览：这张卡是什么、会发什么、现在能不能用。
//
// 不能用时仍然返回 200 与原因（ReasonCode 与兑换时的错误码一致），
// 让客户端在用户按下「兑换」之前就能说清楚为什么不行 —— 「卡密不存在」除外，
// 它照常以 40441 报错，否则预览接口就成了一个不计失败次数的查码器。
type Preview struct {
	Code          string         `json:"code"`
	Kind          string         `json:"kind"`
	BatchName     string         `json:"batchName"`
	Rewards       []ClientReward `json:"rewards"`
	ValidityMode  string         `json:"validityMode"`
	ValidityDays  int            `json:"validityDays,omitempty"`
	ExpiresAt     *time.Time     `json:"expiresAt,omitempty"`
	StartsAt      *time.Time     `json:"startsAt,omitempty"`
	MaxDevices    int            `json:"maxDevices,omitempty"`
	MaxUses       int            `json:"maxUses"`
	RemainingUses int            `json:"remainingUses"`
	Audience      string         `json:"audience"`
	Usable        bool           `json:"usable"`
	ReasonCode    int            `json:"reasonCode,omitempty"`
	Reason        string         `json:"reason,omitempty"`
}

// ClientDevice 「我的授权」里的一台设备。
type ClientDevice struct {
	DeviceID    string    `json:"deviceId"`
	DeviceName  string    `json:"deviceName,omitempty"`
	FirstSeenAt time.Time `json:"firstSeenAt"`
	LastSeenAt  time.Time `json:"lastSeenAt"`
	// Current 就是发起这次请求的设备。
	Current bool `json:"current"`
}

// ClientLicense 「我的授权」里的一张授权卡。
type ClientLicense struct {
	ID          int64          `json:"id"`
	Code        string         `json:"code"`
	BatchName   string         `json:"batchName"`
	Status      string         `json:"status"`
	Expired     bool           `json:"expired"`
	ActivatedAt *time.Time     `json:"activatedAt,omitempty"`
	ExpiresAt   *time.Time     `json:"expiresAt,omitempty"`
	MaxDevices  int            `json:"maxDevices"`
	Devices     []ClientDevice `json:"devices"`
	// SelfUnbindLimit 每 30 天可自助解绑的次数；0 为不允许自助解绑。
	SelfUnbindLimit int `json:"selfUnbindLimit"`
	// SelfUnbindRemaining 当前窗口内还能自助解绑几次。
	SelfUnbindRemaining int `json:"selfUnbindRemaining"`
}

// ClientRedemption 「兑换记录」里的一条。
type ClientRedemption struct {
	ID        int64          `json:"id"`
	Code      string         `json:"code"`
	Kind      string         `json:"kind"`
	BatchName string         `json:"batchName"`
	Source    string         `json:"source"`
	Results   []RewardResult `json:"results"`
	CreatedAt time.Time      `json:"createdAt"`
}

// ClientRedemptionPage 兑换记录分页。
type ClientRedemptionPage struct {
	Items []ClientRedemption `json:"items"`
	Total int64              `json:"total"`
	Page  int                `json:"page"`
	Limit int                `json:"limit"`
}
