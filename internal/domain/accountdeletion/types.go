// Package accountdeletion 账号注销：应用级配置、注销申请的状态机、前置条件与验证方式的判定。
//
// 判定都是纯函数（EvaluateGuards、PlanVerification、NextStatus），服务层取齐事实后调用：
// App 注销页上「为什么不能注销 / 要验证什么」与提交时的报错出自同一套判据。
package accountdeletion

import "time"

// 注销方式。
const (
	// ModeImmediate 验证通过（且无需审核）后当场删除账号
	ModeImmediate = "immediate"
	// ModeCooling 先进入冷静期，期满由后台删除；期间本人可以撤销
	ModeCooling = "cooling"
)

// 申请状态。
const (
	StatusReviewing  = "reviewing"
	StatusScheduled  = "scheduled"
	StatusProcessing = "processing"
	StatusCompleted  = "completed"
	StatusCancelled  = "cancelled"
	StatusRejected   = "rejected"
	StatusFailed     = "failed"
)

// 申请来源。
const (
	SourceUser  = "user"
	SourceAdmin = "admin"
)

// 验证方式。
const (
	VerifyPassword = "password"
	VerifyEmail    = "email"
	VerifyPhone    = "phone"
	// VerifyTOTP / VerifyRecovery 两步验证（身份验证器动态码或一个恢复码）
	VerifyTOTP     = "totp"
	VerifyRecovery = "recovery"
	// VerifySession 账号没有任何可用的验证方式时，凭「刚刚登录」放行
	VerifySession = "session"
)

// 前置条件的处置档位。
const (
	GuardOff   = "off"
	GuardWarn  = "warn"
	GuardBlock = "block"
)

// 前置条件的键，顺序即 App 与控制台的展示顺序。
const (
	GuardActiveBan      = "activeBan"
	GuardPendingOrders  = "pendingOrders"
	GuardPendingRefunds = "pendingRefunds"
	GuardWalletBalance  = "walletBalance"
	GuardActiveVip      = "activeVip"
	GuardLifetimeVip    = "lifetimeVip"
	GuardPoints         = "points"
)

// 冷静期内登录的处理。
const (
	// LoginKeep 登录不影响冷静期，App 里提示并可以手动撤销
	LoginKeep = "keep"
	// LoginCancel 冷静期内登录即视为撤销
	LoginCancel = "cancel"
)

// 撤销人。
const (
	CancelledByUser  = "user"
	CancelledByLogin = "login"
	// CancelledByAdminPrefix 管理员撤销时记为 admin:<账号>
	CancelledByAdminPrefix = "admin:"
)

// 墓碑标识的种类。
const (
	TombstoneAccount = "account"
	TombstoneEmail   = "email"
	TombstonePhone   = "phone"
)

// 取值范围。
const (
	MinCoolingDays        = 1
	MaxCoolingDays        = 180
	DefaultCoolingDays    = 15
	MaxRecentLoginMinutes = 1440
	MaxRemindBeforeHours  = 720
	MaxReregisterDays     = 3650
	MaxReasons            = 20
	MaxReasonCodeRunes    = 64
	MaxReasonLabelRunes   = 60
	MaxReasonTextRunes    = 500
	MaxNoticeTitleRunes   = 60
	MaxNoticeRunes        = 8000
	MaxAgreementRunes     = 120
	MaxSupportRunes       = 200
)

// Reason 一项注销原因。Code 记进申请，Label 给人看。
type Reason struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

// Guards 每一项前置条件的处置档位（off / warn / block）。
type Guards struct {
	// ActiveBan 账号处于封禁中
	ActiveBan string `json:"activeBan"`
	// PendingOrders 有尚未支付完成的订单（最近 48 小时内创建）
	PendingOrders string `json:"pendingOrders"`
	// PendingRefunds 有处理中的退款
	PendingRefunds string `json:"pendingRefunds"`
	// WalletBalance 钱包里还有余额（含冻结）
	WalletBalance string `json:"walletBalance"`
	// ActiveVip 会员还没到期
	ActiveVip string `json:"activeVip"`
	// LifetimeVip 是永久会员
	LifetimeVip string `json:"lifetimeVip"`
	// Points 还有积分
	Points string `json:"points"`
}

// Config 注销的配置。字段含义见 docs/account-deletion.md。
type Config struct {
	// AllowImmediate / AllowCooling 用户能选哪几种方式，至少开一种
	AllowImmediate bool   `json:"allowImmediate"`
	AllowCooling   bool   `json:"allowCooling"`
	DefaultMode    string `json:"defaultMode"`
	CoolingDays    int    `json:"coolingDays"`
	// RequireReview 提交后先由管理员审核；批准后才进入冷静期或执行
	RequireReview bool `json:"requireReview"`

	// VerifyMethods 身份验证：其中任意一种通过即可（password / email / phone）
	VerifyMethods []string `json:"verifyMethods"`
	// RequireTOTP 开了两步验证的账号还要再过一次动态码（或恢复码）
	RequireTOTP bool `json:"requireTotp"`
	// RecentLoginMinutes 账号上一种可用的验证方式都没有时（例如只用第三方登录、没绑邮箱手机），
	// 当前会话签发不超过这么多分钟即可放行；0 为不放行，这类账号要先绑定联系方式
	RecentLoginMinutes int `json:"recentLoginMinutes"`

	Reasons           []Reason `json:"reasons"`
	ReasonRequired    bool     `json:"reasonRequired"`
	AllowCustomReason bool     `json:"allowCustomReason"`

	NoticeTitle    string `json:"noticeTitle"`
	NoticeContent  string `json:"noticeContent"`
	AgreementLabel string `json:"agreementLabel"`
	SupportContact string `json:"supportContact"`

	Guards Guards `json:"guards"`

	// LoginDuringCooling 冷静期内登录：keep 不影响 / cancel 自动撤销
	LoginDuringCooling string `json:"loginDuringCooling"`
	// SignOutOthers 进入冷静期或待审核时，退出当前设备以外的所有登录
	SignOutOthers bool `json:"signOutOthers"`
	// RemindBeforeHours 到期前多少小时提醒一次（站内信与邮件）；0 为不提醒
	RemindBeforeHours int `json:"remindBeforeHours"`
	// ReregisterDays 注销后多少天内同一账号名、邮箱、手机号不能再注册；0 为不限制
	ReregisterDays int `json:"reregisterDays"`
	// NotifyEmail 提交、撤销、驳回、完成时给绑定邮箱发信
	NotifyEmail bool `json:"notifyEmail"`
}

// Settings 应用的注销开关与配置。
type Settings struct {
	AppID   int64  `json:"appId"`
	Enabled bool   `json:"enabled"`
	Config  Config `json:"config"`
	// Configured 是否保存过；没保存过时 Config 是默认值
	Configured bool       `json:"configured"`
	UpdatedBy  string     `json:"updatedBy,omitempty"`
	UpdatedAt  *time.Time `json:"updatedAt,omitempty"`
}

// Request 一条注销申请。
type Request struct {
	ID           int64          `json:"id"`
	AppID        int64          `json:"appId"`
	UserID       int64          `json:"userId"`
	Account      string         `json:"account"`
	Nickname     string         `json:"nickname"`
	Mode         string         `json:"mode"`
	Status       string         `json:"status"`
	Source       string         `json:"source"`
	ReasonCode   string         `json:"reasonCode"`
	ReasonText   string         `json:"reasonText"`
	CoolingDays  int            `json:"coolingDays"`
	ScheduledAt  *time.Time     `json:"scheduledAt,omitempty"`
	RemindAt     *time.Time     `json:"remindAt,omitempty"`
	RemindedAt   *time.Time     `json:"remindedAt,omitempty"`
	ReviewedAt   *time.Time     `json:"reviewedAt,omitempty"`
	ReviewedBy   string         `json:"reviewedBy"`
	ReviewNote   string         `json:"reviewNote"`
	CancelledAt  *time.Time     `json:"cancelledAt,omitempty"`
	CancelledBy  string         `json:"cancelledBy"`
	CancelReason string         `json:"cancelReason"`
	CompletedAt  *time.Time     `json:"completedAt,omitempty"`
	Failure      string         `json:"failure"`
	Attempts     int            `json:"attempts"`
	VerifiedBy   []string       `json:"verifiedBy"`
	Operator     string         `json:"operator"`
	IP           string         `json:"ip"`
	DeviceID     string         `json:"deviceId"`
	Device       string         `json:"device"`
	UserAgent    string         `json:"userAgent"`
	Snapshot     map[string]any `json:"snapshot"`
	CreatedAt    time.Time      `json:"createdAt"`
	UpdatedAt    time.Time      `json:"updatedAt"`
	// UserExists 账号是否还在（列表与详情里填，已注销的为 false）
	UserExists bool `json:"userExists"`
}

// Open 申请是否还在进行中（占着「一人一条」的位置）。
func (r *Request) Open() bool {
	switch r.Status {
	case StatusReviewing, StatusScheduled, StatusProcessing, StatusFailed:
		return true
	}
	return false
}

// RequestInput 写入一条新申请。
type RequestInput struct {
	AppID       int64
	UserID      int64
	Account     string
	Nickname    string
	Mode        string
	Status      string
	Source      string
	ReasonCode  string
	ReasonText  string
	CoolingDays int
	ScheduledAt *time.Time
	RemindAt    *time.Time
	VerifiedBy  []string
	Operator    string
	IP          string
	DeviceID    string
	Device      string
	UserAgent   string
	Snapshot    map[string]any
}

// RequestQuery 管理端申请列表的筛选。
type RequestQuery struct {
	AppID   int64
	Status  string
	Mode    string
	Source  string
	UserID  int64
	Keyword string
	From    *time.Time
	To      *time.Time
	Page    int
	Limit   int
}

// RequestPage 一页申请。
type RequestPage struct {
	Items []Request `json:"items"`
	Total int64     `json:"total"`
	Page  int       `json:"page"`
	Limit int       `json:"limit"`
}

// Tombstone 注销后留下的一个标识摘要。
type Tombstone struct {
	ID        int64     `json:"id"`
	AppID     int64     `json:"appId"`
	RequestID *int64    `json:"requestId,omitempty"`
	Kind      string    `json:"kind"`
	Hint      string    `json:"hint"`
	ExpiresAt time.Time `json:"expiresAt"`
	CreatedAt time.Time `json:"createdAt"`
	// Account 对应申请里的账号名（申请还在时）
	Account string `json:"account,omitempty"`
}

// TombstonePage 一页墓碑。
type TombstonePage struct {
	Items []Tombstone `json:"items"`
	Total int64       `json:"total"`
	Page  int         `json:"page"`
	Limit int         `json:"limit"`
}

// TombstoneInput 写入一个墓碑。
type TombstoneInput struct {
	Kind      string
	Digest    string
	Hint      string
	ExpiresAt time.Time
}

// Facts 判定前置条件所需的账号事实，由仓储一次取齐。
type Facts struct {
	BanActive      bool       `json:"banActive"`
	BanReason      string     `json:"banReason,omitempty"`
	PendingOrders  int64      `json:"pendingOrders"`
	PendingRefunds int64      `json:"pendingRefunds"`
	WalletBalance  string     `json:"walletBalance"`
	WalletPositive bool       `json:"walletPositive"`
	VipExpireAt    *time.Time `json:"vipExpireAt,omitempty"`
	Lifetime       bool       `json:"lifetime"`
	Points         int64      `json:"points"`
}

// Check 一项前置条件的结论。
type Check struct {
	Key     string `json:"key"`
	Title   string `json:"title"`
	Level   string `json:"level"`
	Passed  bool   `json:"passed"`
	Message string `json:"message"`
}

// Availability 账号上可用的验证方式（已绑定且这个应用发得出验证码）。
type Availability struct {
	Password bool
	// Email / Phone 打码后的联系方式；空为不可用
	Email string
	Phone string
	TOTP  bool
}

// VerifyOption App 上的一种验证方式。
type VerifyOption struct {
	Type   string `json:"type"`
	Masked string `json:"masked,omitempty"`
}

// Verification 这个账号注销前要过的验证：Methods 任选其一，TOTP 为真时还要再过两步验证；
// Methods 为空而 Session 为真时，凭最近登录放行；两者都没有时 Available 为假，Message 说明怎么办。
type Verification struct {
	Methods   []VerifyOption `json:"methods"`
	TOTP      bool           `json:"totp"`
	Session   bool           `json:"session"`
	Available bool           `json:"available"`
	Message   string         `json:"message,omitempty"`
}

// ModeOption App 上的一种注销方式。
type ModeOption struct {
	Mode        string `json:"mode"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// Notice 注销须知。
type Notice struct {
	Title          string `json:"title"`
	Content        string `json:"content"`
	AgreementLabel string `json:"agreementLabel"`
}

// ClientRequest App 看到的申请。
type ClientRequest struct {
	ID          int64      `json:"id"`
	Mode        string     `json:"mode"`
	Status      string     `json:"status"`
	Source      string     `json:"source"`
	ReasonCode  string     `json:"reasonCode"`
	ReasonText  string     `json:"reasonText"`
	CoolingDays int        `json:"coolingDays"`
	RequestedAt time.Time  `json:"requestedAt"`
	ScheduledAt *time.Time `json:"scheduledAt,omitempty"`
	ReviewNote  string     `json:"reviewNote,omitempty"`
	CancelledAt *time.Time `json:"cancelledAt,omitempty"`
	// CanCancel 本人现在能不能撤销
	CanCancel bool `json:"canCancel"`
}

// ClientView GET /me/deletion：注销页需要的一切。Enabled 为假时其余字段为空，入口应当隐藏。
type ClientView struct {
	Enabled           bool           `json:"enabled"`
	Modes             []ModeOption   `json:"modes"`
	DefaultMode       string         `json:"defaultMode"`
	CoolingDays       int            `json:"coolingDays"`
	RequireReview     bool           `json:"requireReview"`
	Notice            Notice         `json:"notice"`
	Reasons           []Reason       `json:"reasons"`
	ReasonRequired    bool           `json:"reasonRequired"`
	AllowCustomReason bool           `json:"allowCustomReason"`
	Verification      Verification   `json:"verification"`
	Checks            []Check        `json:"checks"`
	Blocked           bool           `json:"blocked"`
	ReregisterDays    int            `json:"reregisterDays"`
	LoginCancels      bool           `json:"loginCancels"`
	SupportContact    string         `json:"supportContact,omitempty"`
	Request           *ClientRequest `json:"request,omitempty"`
}

// LoginNotice 登录结果里附带的注销状态：冷静期 / 待审核中登录时出现。
type LoginNotice struct {
	Status      string     `json:"status"`
	Mode        string     `json:"mode"`
	ScheduledAt *time.Time `json:"scheduledAt,omitempty"`
	// Cancelled 这次登录把冷静期撤销了（应用配置为登录即撤销）
	Cancelled bool `json:"cancelled"`
}

// SubmitResult POST /me/deletion 的结果。Deleted 为真时账号已经没了，客户端应当清掉本地登录。
type SubmitResult struct {
	Request *ClientRequest `json:"request"`
	Deleted bool           `json:"deleted"`
	// SignedOut 退出了几处其他登录
	SignedOut int `json:"signedOut"`
}

// CountItem 统计里的一项。
type CountItem struct {
	Key   string `json:"key"`
	Label string `json:"label,omitempty"`
	Count int64  `json:"count"`
}

// TrendPoint 某一天的数量。
type TrendPoint struct {
	Date      string `json:"date"`
	Requested int64  `json:"requested"`
	Completed int64  `json:"completed"`
	Cancelled int64  `json:"cancelled"`
}

// Stats 管理端统计。
type Stats struct {
	Total      int64        `json:"total"`
	ByStatus   []CountItem  `json:"byStatus"`
	ByMode     []CountItem  `json:"byMode"`
	BySource   []CountItem  `json:"bySource"`
	Reasons    []CountItem  `json:"reasons"`
	Trend      []TrendPoint `json:"trend"`
	DueIn7Days int64        `json:"dueIn7Days"`
	Tombstones int64        `json:"tombstones"`
	// RetentionRate 已结束的申请里被撤销（含驳回）的比例，0–1
	RetentionRate float64 `json:"retentionRate"`
	Days          int     `json:"days"`
	TimeZone      string  `json:"timeZone"`
}
