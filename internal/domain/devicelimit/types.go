package devicelimit

import (
	"context"
	"time"

	devicedomain "aegis/internal/domain/device"
)

// LedgerEntry 设备台账：用户登录过的每台设备一行，新设备频率与「常用设备」都以它为准。
// 只记有意义的 Key（见 Ledgerable），单次登录的不记。
type LedgerEntry struct {
	DeviceKey   string              `json:"key"`
	Platform    string              `json:"platform,omitempty"`
	Class       string              `json:"class"`
	Model       string              `json:"-"`
	Name        string              `json:"name,omitempty"`
	Extra       *devicedomain.Extra `json:"-"`
	Info        *devicedomain.Info  `json:"deviceInfo,omitempty"`
	FirstSeenAt time.Time           `json:"firstSeenAt"`
	LastSeenAt  time.Time           `json:"lastSeenAt"`
	LastIP      string              `json:"lastIp,omitempty"`
	LoginCount  int64               `json:"loginCount"`
	// Online 此刻是否仍在登录（读取时按会话补上）
	Online bool `json:"online"`
}

// LedgerTouch 写台账的一次登录或刷新。
type LedgerTouch struct {
	AppID     int64
	UserID    int64
	DeviceKey string
	Platform  string
	Model     string
	Name      string
	Extra     *devicedomain.Extra
	IP        string
	// Login 是新登录（计入登录次数）；刷新只更新最后出现时间
	Login bool
	At    time.Time
}

// 事件类型。
const (
	EventKicked        = "kicked"         // 超限时自动下线
	EventRejected      = "rejected"       // 超限拒绝登录
	EventChallenged    = "challenged"     // 请用户选择下线哪台
	EventResolved      = "resolved"       // 用户选好了，被选中的设备下线
	EventNewDevice     = "new_device"     // 新设备频率超限，拒绝登录
	EventUserRemoved   = "user_removed"   // 用户在设备管理里下线自己的其他设备
	EventAdminRemoved  = "admin_removed"  // 管理员下线
	EventAdminEnforced = "admin_enforced" // 管理员按当前策略整理
)

// EventActions 全部事件类型，顺序即控制台展示顺序。
var EventActions = []string{
	EventKicked, EventRejected, EventChallenged, EventResolved, EventNewDevice,
	EventUserRemoved, EventAdminRemoved, EventAdminEnforced,
}

// Event 一条设备数控制记录。
type Event struct {
	ID        int64  `json:"id"`
	AppID     int64  `json:"-"`
	UserID    int64  `json:"userId"`
	Account   string `json:"account,omitempty"`
	Action    string `json:"action"`
	Dimension string `json:"dimension,omitempty"`
	// DeviceKey / DeviceName / Platform 被处置的那台（被下线的设备，或被拒绝登录的设备）
	DeviceKey  string `json:"deviceKey,omitempty"`
	DeviceName string `json:"deviceName,omitempty"`
	Platform   string `json:"platform,omitempty"`
	Class      string `json:"class,omitempty"`
	// TriggerName 触发这次处置的新登录设备（自动下线时有）
	TriggerName string         `json:"triggerName,omitempty"`
	IP          string         `json:"ip,omitempty"`
	Limit       int            `json:"limit,omitempty"`
	Detail      map[string]any `json:"detail,omitempty"`
	Operator    string         `json:"operator,omitempty"`
	CreatedAt   time.Time      `json:"createdAt"`
}

// EventQuery 管理端事件列表筛选。
type EventQuery struct {
	Action    string
	Dimension string
	UserID    int64
	Keyword   string
	From      *time.Time
	To        *time.Time
	Page      int
	Limit     int
}

// EventList 分页结果。
type EventList struct {
	Items []Event `json:"items"`
	Total int64   `json:"total"`
	Page  int     `json:"page"`
	Limit int     `json:"limit"`
}

// OverrideQuery 例外名单筛选。
type OverrideQuery struct {
	Keyword string
	Page    int
	Limit   int
}

// OverrideList 分页结果。
type OverrideList struct {
	Items []Override `json:"items"`
	Total int64      `json:"total"`
	Page  int        `json:"page"`
	Limit int        `json:"limit"`
}

// Challenge 请用户选择下线哪台设备：登录结果里的 deviceLimit。
// 此时不签发令牌，客户端带着 Ticket 与勾选的 Key 调 /auth/device-limit/resolve 完成登录。
type Challenge struct {
	Ticket       string        `json:"ticket"`
	ExpiresAt    time.Time     `json:"expiresAt"`
	Dimension    string        `json:"dimension"`
	Requirements []Requirement `json:"requirements"`
	// MustRemove 至少勾选几台
	MustRemove int    `json:"mustRemove"`
	Message    string `json:"message"`
	// Devices 已登录的设备；Eligible 为 false 的勾了也不解决问题（例如只超了手机数时的电脑）
	Devices []ChallengeDevice `json:"devices"`
}

// ChallengeDevice 选择页上的一台设备。
type ChallengeDevice struct {
	Device
	Eligible bool `json:"eligible"`
}

// PendingLogin 选择页期间暂存的登录现场，凭 Ticket 取回后继续签发。只存在服务端。
type PendingLogin struct {
	AppID     int64               `json:"appId"`
	UserID    int64               `json:"userId"`
	Provider  string              `json:"provider"`
	LoginType string              `json:"loginType"`
	DeviceID  string              `json:"deviceId"`
	Device    string              `json:"device"`
	IP        string              `json:"ip"`
	UserAgent string              `json:"userAgent"`
	Model     string              `json:"model"`
	Platform  string              `json:"platform"`
	Extra     *devicedomain.Extra `json:"extra,omitempty"`
	CreatedAt time.Time           `json:"createdAt"`
}

// ChallengeTTL 选择页的有效期。
const ChallengeTTL = 5 * time.Minute

// Status 某个用户的设备数状况：用户端「登录设备」页与管理端用户详情共用。
type Status struct {
	Enabled bool     `json:"enabled"`
	Limits  Limits   `json:"limits"`
	Usage   Usage    `json:"usage"`
	Devices []Device `json:"devices"`
	// SameDevice / Overflow 让客户端能解释「为什么另一台被下线了」
	SameDevice string `json:"sameDevice"`
	Overflow   string `json:"overflow"`
	// NewDevicesInWindow 新设备频率限制开启时，窗口内已出现的新设备数
	NewDevicesInWindow int `json:"newDevicesInWindow"`
}

// AdminUserView 管理端查看一个用户。
type AdminUserView struct {
	UserID   int64         `json:"userId"`
	Account  string        `json:"account"`
	Nickname string        `json:"nickname,omitempty"`
	Avatar   string        `json:"avatar,omitempty"`
	Member   Member        `json:"member"`
	Status   Status        `json:"status"`
	Override *Override     `json:"override"`
	Ledger   []LedgerEntry `json:"ledger"`
	Events   []Event       `json:"events"`
}

// SimulateDevice 模拟时虚构的一台已登录设备。
type SimulateDevice struct {
	Key          string `json:"key"`
	Name         string `json:"name"`
	Platform     string `json:"platform"`
	LoginMinutes int    `json:"loginMinutesAgo"`
	IdleMinutes  int    `json:"idleMinutesAgo"`
}

// SimulateInput 管理端模拟一次登录：可以拿真实用户，也可以虚构会员身份与已登录设备。
type SimulateInput struct {
	// Policy 草稿；为空时用已保存的策略
	Policy *Policy `json:"policy"`
	// UserID 给了就用这个用户的真实会员身份、例外与在线设备，忽略 Member 与 Devices
	UserID   int64            `json:"userId"`
	Member   Member           `json:"member"`
	Override *OverrideInput   `json:"override"`
	Devices  []SimulateDevice `json:"devices"`
	// 正在登录的设备
	Platform  string `json:"platform"`
	DeviceKey string `json:"deviceKey"`
	Known     bool   `json:"known"`
	// NewDevicesInWindow 虚构场景下窗口内已有的新设备数
	NewDevicesInWindow int  `json:"newDevicesInWindow"`
	CanAsk             bool `json:"canAsk"`
}

// SimulateResult 模拟结果。
type SimulateResult struct {
	Limits   Limits   `json:"limits"`
	Devices  []Device `json:"devices"`
	Decision Decision `json:"decision"`
}

// Overview 管理端概览。
type Overview struct {
	Policy Policy `json:"policy"`
	// Configured 是否已经用新策略保存过（否则是按旧字段推导出来的）
	Configured bool `json:"configured"`
	Days       int  `json:"days"`
	// Totals 窗口内各类事件次数
	Totals map[string]int64 `json:"totals"`
	// Trend 每天各类事件次数
	Trend []TrendPoint `json:"trend"`
	// Dimensions 自动下线与拒绝的触发维度分布
	Dimensions map[string]int64 `json:"dimensions"`
	// ActiveDevices 窗口内出现过的设备（台账口径）
	ActiveDevices int64            `json:"activeDevices"`
	ActiveUsers   int64            `json:"activeUsers"`
	ByClass       map[string]int64 `json:"byClass"`
	// Distribution 窗口内每个用户的设备数分布：键 1..9，10 表示 10 台及以上
	Distribution map[string]int64 `json:"distribution"`
	// AtLimitUsers 设备数已达默认上限的用户（台账口径，不含档位与例外）
	AtLimitUsers int64 `json:"atLimitUsers"`
	Overrides    int64 `json:"overrides"`
	// TopUsers 窗口内被处置最多的用户
	TopUsers []TopUser `json:"topUsers"`
}

// TrendPoint 一天的事件计数。
type TrendPoint struct {
	Date   string           `json:"date"`
	Counts map[string]int64 `json:"counts"`
}

// TopUser 被处置最多的用户。
type TopUser struct {
	UserID  int64     `json:"userId"`
	Account string    `json:"account"`
	Count   int64     `json:"count"`
	LastAt  time.Time `json:"lastAt"`
}

type challengeSupportKey struct{}

// ChallengeHeader 客户端声明「能让用户选择下线哪台设备」的请求头，值为 1。
// 没有声明的客户端（旧版 SDK、网站扫码登录等）在策略为 ask 时按 askFallback 处理，
// 不会拿到一个看不懂的、没有令牌的登录结果。
const ChallengeHeader = "X-Device-Limit-Challenge"

// WithChallengeSupport 在请求上下文上记下客户端支持选择页。
func WithChallengeSupport(ctx context.Context) context.Context {
	return context.WithValue(ctx, challengeSupportKey{}, true)
}

// ChallengeSupported 客户端是否声明支持选择页。
func ChallengeSupported(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	supported, _ := ctx.Value(challengeSupportKey{}).(bool)
	return supported
}
