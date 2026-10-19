package app

import "time"

// 版本管控（Client Control）。
//
// 客户端启动、回到前台、收到变更通知时，把自己的包名、版本、签名证书等信息加密上报，
// 服务端按应用的管控规则给出一份 **Ed25519 签名** 的裁决：放行 / 提醒 / 强制更新 / 停用。
// 客户端用内置（或首次信任）的公钥验签后执行，并把裁决落盘；
// 停用状态只能被一份**新鲜的**（绑定本次请求 nonce 的）签名放行裁决解除。
//
// 判定次序（见 service/client_control_engine.go）：
//  1. 管控未启用 → 放行（仍签名下发，用来解除此前的停用）
//  2. 规则按 priority 从高到低、id 从小到大逐条匹配，第一条命中的规则决定结果；
//     allow 规则用作例外（如给测试设备放行）
//  3. 没有规则命中时检查官方身份：配置了官方包名而包名不在其中 → 停用（package_not_allowed）；
//     配置了官方签名而证书不在其中 → 停用（signature_mismatch）
//  4. 默认放行
//
// 规则条件之间是「且」，同一条件内的多个取值是「或」；未填写的条件不参与判定。
// 完整契约见 docs/client-control.md。

const (
	ClientControlProtocol = "aegis-client-control-v1"

	ClientControlActionAllow       = "allow"
	ClientControlActionWarn        = "warn"
	ClientControlActionForceUpdate = "force_update"
	ClientControlActionBlock       = "block"

	// 离线兜底：放行裁决过期且连不上服务端时怎么办。停用裁决不受它影响，永远保持停用。
	ClientControlOfflineAllow = "allow"
	ClientControlOfflineBlock = "block"

	ClientControlReasonDisabled          = "disabled"
	ClientControlReasonRule              = "rule"
	ClientControlReasonPackageNotAllowed = "package_not_allowed"
	ClientControlReasonSignatureMismatch = "signature_mismatch"
	ClientControlReasonDefault           = "default"
	// ClientControlReasonUndetermined 只凭部分字段（网关闸门、版本分布估算）无法确定结果，按放行处理。
	ClientControlReasonUndetermined = "undetermined"

	ClientControlKeySign = "sign"
	ClientControlKeyBox  = "box"

	ClientControlKeyActive  = "active"
	ClientControlKeyRetired = "retired"
)

var ValidClientControlActions = map[string]struct{}{
	ClientControlActionAllow: {}, ClientControlActionWarn: {},
	ClientControlActionForceUpdate: {}, ClientControlActionBlock: {},
}

// ClientControlActionBlocks 该结果是否让客户端不可用（停用与强制更新都是）。
func ClientControlActionBlocks(action string) bool {
	return action == ClientControlActionBlock || action == ClientControlActionForceUpdate
}

// ClientControlSettings 应用级管控设置。PolicyVersion 在任何规则或设置变更时加一，
// 客户端的长轮询据此得知「该重新校验了」。
type ClientControlSettings struct {
	AppID           int64    `json:"appId"`
	Enabled         bool     `json:"enabled"`
	PolicyVersion   int64    `json:"policyVersion"`
	AllowedPackages []string `json:"allowedPackages"`
	// AllowedCerts 官方签名证书的 SHA-256（64 位小写十六进制，不带冒号）。
	AllowedCerts []string `json:"allowedCerts"`
	// VerdictTTLSeconds 放行裁决的离线有效期。
	VerdictTTLSeconds int `json:"verdictTtlSeconds"`
	// RecheckSeconds 客户端在前台时的定期复查间隔。
	RecheckSeconds int    `json:"recheckSeconds"`
	OfflinePolicy  string `json:"offlinePolicy"`
	// WatchEnabled 是否允许客户端用长轮询即时得知变更（未登录也可用）。
	WatchEnabled bool `json:"watchEnabled"`
	// ServerGate 服务端执行：停用的客户端调用网关其余接口一律被拒（40329 / 40326）。
	ServerGate   bool       `json:"serverGate"`
	BlockTitle   string     `json:"blockTitle"`
	BlockMessage string     `json:"blockMessage"`
	UpdatedAt    time.Time  `json:"updatedAt"`
	UpdatedBy    string     `json:"updatedBy"`
	CreatedAt    *time.Time `json:"createdAt,omitempty"`
}

// ClientControlSettingsPatch 字段缺省表示不修改。
type ClientControlSettingsPatch struct {
	Enabled           *bool     `json:"enabled"`
	AllowedPackages   *[]string `json:"allowedPackages"`
	AllowedCerts      *[]string `json:"allowedCerts"`
	VerdictTTLSeconds *int      `json:"verdictTtlSeconds"`
	RecheckSeconds    *int      `json:"recheckSeconds"`
	OfflinePolicy     *string   `json:"offlinePolicy"`
	WatchEnabled      *bool     `json:"watchEnabled"`
	ServerGate        *bool     `json:"serverGate"`
	BlockTitle        *string   `json:"blockTitle"`
	BlockMessage      *string   `json:"blockMessage"`
}

// ClientControlConditions 规则条件。条件之间为「且」，同一条件内的多个取值为「或」，留空不参与判定。
// 版本号区间两端都是闭区间，0 表示不限。
type ClientControlConditions struct {
	Packages         []string `json:"packages,omitempty"`
	PackagesNegate   bool     `json:"packagesNegate,omitempty"`
	VersionCodeMin   int64    `json:"versionCodeMin,omitempty"`
	VersionCodeMax   int64    `json:"versionCodeMax,omitempty"`
	VersionNames     []string `json:"versionNames,omitempty"`
	Certs            []string `json:"certs,omitempty"`
	CertsNegate      bool     `json:"certsNegate,omitempty"`
	Platforms        []string `json:"platforms,omitempty"`
	DeviceIDs        []string `json:"deviceIds,omitempty"`
	UserIDs          []int64  `json:"userIds,omitempty"`
	Installers       []string `json:"installers,omitempty"`
	InstallersNegate bool     `json:"installersNegate,omitempty"`
	OSVersionMin     int      `json:"osVersionMin,omitempty"`
	OSVersionMax     int      `json:"osVersionMax,omitempty"`
}

// IsEmpty 没有任何条件 = 命中所有客户端。
func (c ClientControlConditions) IsEmpty() bool {
	return len(c.Packages) == 0 && c.VersionCodeMin == 0 && c.VersionCodeMax == 0 &&
		len(c.VersionNames) == 0 && len(c.Certs) == 0 && len(c.Platforms) == 0 &&
		len(c.DeviceIDs) == 0 && len(c.UserIDs) == 0 && len(c.Installers) == 0 &&
		c.OSVersionMin == 0 && c.OSVersionMax == 0
}

type ClientControlRule struct {
	ID          int64                   `json:"id"`
	AppID       int64                   `json:"appId"`
	Name        string                  `json:"name"`
	Enabled     bool                    `json:"enabled"`
	Priority    int                     `json:"priority"`
	Action      string                  `json:"action"`
	Conditions  ClientControlConditions `json:"conditions"`
	Title       string                  `json:"title"`
	Message     string                  `json:"message"`
	ActionURL   string                  `json:"actionUrl"`
	ActionLabel string                  `json:"actionLabel"`
	StartsAt    *time.Time              `json:"startsAt"`
	EndsAt      *time.Time              `json:"endsAt"`
	Note        string                  `json:"note"`
	HitCount    int64                   `json:"hitCount"`
	LastHitAt   *time.Time              `json:"lastHitAt"`
	CreatedBy   string                  `json:"createdBy"`
	CreatedAt   time.Time               `json:"createdAt"`
	UpdatedAt   time.Time               `json:"updatedAt"`
}

// ActiveAt 规则此刻是否生效（启用且在生效时间窗内）。
func (r ClientControlRule) ActiveAt(now time.Time) bool {
	if !r.Enabled {
		return false
	}
	if r.StartsAt != nil && now.Before(*r.StartsAt) {
		return false
	}
	if r.EndsAt != nil && !now.Before(*r.EndsAt) {
		return false
	}
	return true
}

// ClientControlRuleInput 新建 / 整体更新规则。
type ClientControlRuleInput struct {
	Name        string                  `json:"name"`
	Enabled     *bool                   `json:"enabled"`
	Priority    int                     `json:"priority"`
	Action      string                  `json:"action"`
	Conditions  ClientControlConditions `json:"conditions"`
	Title       string                  `json:"title"`
	Message     string                  `json:"message"`
	ActionURL   string                  `json:"actionUrl"`
	ActionLabel string                  `json:"actionLabel"`
	StartsAt    *time.Time              `json:"startsAt"`
	EndsAt      *time.Time              `json:"endsAt"`
	Note        string                  `json:"note"`
}

// ClientReport 客户端上报的身份信息（解密后的明文），UserID / IP 由服务端填写。
type ClientReport struct {
	PackageName string `json:"packageName"`
	VersionName string `json:"versionName"`
	VersionCode int64  `json:"versionCode"`
	Platform    string `json:"platform"`
	CertSHA256  string `json:"certSha256"`
	Installer   string `json:"installer"`
	DeviceID    string `json:"deviceId"`
	OSVersion   int    `json:"osVersion"`
	Model       string `json:"model"`
	Brand       string `json:"brand"`
	ABI         string `json:"abi"`
	// PolicyVersion 客户端当前持有的裁决所对应的策略版本，仅用于统计与排查。
	PolicyVersion int64 `json:"policyVersion"`

	UserID int64  `json:"-"`
	IP     string `json:"-"`
}

// ClientVerdictSubject 裁决绑定的客户端身份。客户端只接受与自身一致的裁决。
type ClientVerdictSubject struct {
	PackageName string `json:"packageName"`
	VersionCode int64  `json:"versionCode"`
	CertSHA256  string `json:"certSha256"`
	DeviceID    string `json:"deviceId"`
}

// ClientVerdict 签名裁决的明文。签名覆盖的是它序列化后的原始字节，客户端原样保存这串字节。
type ClientVerdict struct {
	V             int                  `json:"v"`
	AppKey        string               `json:"appKey"`
	PolicyVersion int64                `json:"policyVersion"`
	IssuedAt      int64                `json:"issuedAt"`
	ExpiresAt     int64                `json:"expiresAt"`
	Challenge     string               `json:"challenge"`
	Subject       ClientVerdictSubject `json:"subject"`
	Enabled       bool                 `json:"enabled"`
	Action        string               `json:"action"`
	Reason        string               `json:"reason"`
	RuleID        int64                `json:"ruleId,omitempty"`
	Title         string               `json:"title,omitempty"`
	Message       string               `json:"message,omitempty"`
	ActionURL     string               `json:"actionUrl,omitempty"`
	ActionLabel   string               `json:"actionLabel,omitempty"`
	RecheckAfter  int                  `json:"recheckSeconds"`
	WatchEnabled  bool                 `json:"watchEnabled"`
	OfflinePolicy string               `json:"offlinePolicy"`
}

// ClientControlTraceStep 模拟判定时逐条给出的解释。
type ClientControlTraceStep struct {
	RuleID  int64  `json:"ruleId,omitempty"`
	Name    string `json:"name"`
	Matched bool   `json:"matched"`
	Detail  string `json:"detail"`
}

// ClientControlDecision 引擎的判定结果。
type ClientControlDecision struct {
	Action      string                   `json:"action"`
	Reason      string                   `json:"reason"`
	RuleID      int64                    `json:"ruleId,omitempty"`
	RuleName    string                   `json:"ruleName,omitempty"`
	Title       string                   `json:"title,omitempty"`
	Message     string                   `json:"message,omitempty"`
	ActionURL   string                   `json:"actionUrl,omitempty"`
	ActionLabel string                   `json:"actionLabel,omitempty"`
	Trace       []ClientControlTraceStep `json:"trace,omitempty"`
}

// ClientInstall 一台设备上的一个安装（同一设备上的不同包名分开记录）。
type ClientInstall struct {
	DeviceID    string    `json:"deviceId"`
	PackageName string    `json:"packageName"`
	VersionName string    `json:"versionName"`
	VersionCode int64     `json:"versionCode"`
	Platform    string    `json:"platform"`
	CertSHA256  string    `json:"certSha256"`
	Installer   string    `json:"installer"`
	OSVersion   int       `json:"osVersion"`
	Model       string    `json:"model"`
	Brand       string    `json:"brand"`
	ABI         string    `json:"abi"`
	UserID      *int64    `json:"userId,omitempty"`
	IP          string    `json:"ip"`
	LastAction  string    `json:"lastAction"`
	LastReason  string    `json:"lastReason"`
	LastRuleID  *int64    `json:"lastRuleId,omitempty"`
	ReportCount int64     `json:"reportCount"`
	FirstSeenAt time.Time `json:"firstSeenAt"`
	LastSeenAt  time.Time `json:"lastSeenAt"`
}

// ClientInstallGroup 按包名 + 版本 + 签名 + 平台聚合的版本分布。
// Decision 是按当前规则重新判定的结果，与客户端上次拿到的 LastAction 可能不同。
type ClientInstallGroup struct {
	PackageName string                 `json:"packageName"`
	VersionName string                 `json:"versionName"`
	VersionCode int64                  `json:"versionCode"`
	CertSHA256  string                 `json:"certSha256"`
	Platform    string                 `json:"platform"`
	Devices     int64                  `json:"devices"`
	Reports     int64                  `json:"reports"`
	Blocked     int64                  `json:"blocked"`
	FirstSeenAt time.Time              `json:"firstSeenAt"`
	LastSeenAt  time.Time              `json:"lastSeenAt"`
	Decision    *ClientControlDecision `json:"decision,omitempty"`
}

type ClientInstallQuery struct {
	Keyword     string
	PackageName string
	VersionCode int64
	CertSHA256  string
	Action      string
	Days        int
	Page        int
	Limit       int
}

type ClientInstallListResult struct {
	Items      []ClientInstall `json:"items"`
	Page       int             `json:"page"`
	Limit      int             `json:"limit"`
	Total      int64           `json:"total"`
	TotalPages int             `json:"totalPages"`
}

// ClientControlKeyView 管控密钥的公开视图。
type ClientControlKeyView struct {
	KeyID       string     `json:"keyId"`
	Kind        string     `json:"kind"`
	PublicKey   string     `json:"publicKey"`
	Status      string     `json:"status"`
	EndorsedBy  string     `json:"endorsedBy,omitempty"`
	Endorsement string     `json:"endorsement,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	RetiredAt   *time.Time `json:"retiredAt,omitempty"`
}

// ClientControlKey 管控密钥（含私钥密文，只在服务内部流转）。
type ClientControlKey struct {
	ID               int64
	AppID            int64
	KeyID            string
	Kind             string
	PublicKey        []byte
	PrivateKeyCipher string
	Status           string
	EndorsedBy       string
	Endorsement      []byte
	CreatedAt        time.Time
	RetiredAt        *time.Time
}

type ClientControlOverview struct {
	Settings        ClientControlSettings  `json:"settings"`
	Devices24h      int64                  `json:"devices24h"`
	Devices7d       int64                  `json:"devices7d"`
	Devices30d      int64                  `json:"devices30d"`
	Blocked7d       int64                  `json:"blocked7d"`
	Packages        int64                  `json:"packages"`
	Versions        int64                  `json:"versions"`
	Rules           int64                  `json:"rules"`
	ActiveRules     int64                  `json:"activeRules"`
	SigningKey      *ClientControlKeyView  `json:"signingKey,omitempty"`
	BoxKey          *ClientControlKeyView  `json:"boxKey,omitempty"`
	ActionBreakdown map[string]int64       `json:"actionBreakdown"`
	Daily           []ClientControlDayStat `json:"daily"`
}

type ClientControlDayStat struct {
	Day     string `json:"day"`
	Devices int64  `json:"devices"`
	Blocked int64  `json:"blocked"`
}

// ClientControlWatchResult 长轮询结果。Changed 为 true 时客户端应立即重新校验；
// WatchEnabled 为 false 时客户端改用定期复查，RetryAfter 秒后再来。
type ClientControlWatchResult struct {
	PolicyVersion int64 `json:"policyVersion"`
	Changed       bool  `json:"changed"`
	WatchEnabled  bool  `json:"watchEnabled"`
	RetryAfter    int   `json:"retryAfter"`
}
