package storage

import "time"

// 存储池：把多个平台级存储配置并成一个写入目标。
//
// 写入按策略在成员间挑一个，成员满了、出错了就换下一个；读取永远按对象记录里的
// config_id 去原处读，池只决定「新内容写到哪里」。所以成员可以随时加减、调顺序，
// 已经写进去的对象不受影响。

// 选择策略。
const (
	// PoolStrategyPriority 按顺序写满一个再用下一个。
	PoolStrategyPriority = "priority"
	// PoolStrategyRoundRobin 轮流写入各成员。
	PoolStrategyRoundRobin = "round_robin"
	// PoolStrategyWeighted 按权重随机分配。
	PoolStrategyWeighted = "weighted"
	// PoolStrategyLeastUsed 优先写入占用比例最低的成员。
	PoolStrategyLeastUsed = "least_used"
)

// 成员角色。
const (
	// MemberModeActive 正常参与写入。
	MemberModeActive = "active"
	// MemberModeStandby 备用：只有所有 active 成员都不可写时才写入。
	MemberModeStandby = "standby"
	// MemberModeReadOnly 只读（排空）：不再写入，已有对象照常读取。
	MemberModeReadOnly = "readonly"
)

// 成员的运行状态（由健康记录与用量现算，不落库）。
const (
	MemberStateHealthy  = "healthy"
	MemberStateDegraded = "degraded" // 最近失败过，但没在退避
	MemberStateBackoff  = "backoff"  // 连续失败，冷却中，暂不写入
	MemberStateNearFull = "near_full"
	MemberStateFull     = "full"
	MemberStateReadOnly = "readonly"
	MemberStateDisabled = "disabled" // 存储配置被停用
)

// 池与成员的上下限。
const (
	PoolMaxMembers        = 32
	PoolDefaultAttempts   = 3
	PoolMaxAttempts       = 10
	PoolDefaultWarnPct    = 85
	PoolMemberMaxWeight   = 1000
	PoolMemberMaxPriority = 10000
)

// Pool 存储池。
type Pool struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Strategy    string `json:"strategy"`
	Enabled     bool   `json:"enabled"`
	// IsDefault 默认池：没有指定配置名、应用也没有自己的存储时，写入走这个池。
	IsDefault bool `json:"isDefault"`
	// ReadFallback 读取时对象在记录的成员里找不到，去池里其他成员找同一个键。
	ReadFallback bool `json:"readFallback"`
	// MaxAttempts 一次写入最多尝试几个成员。
	MaxAttempts int `json:"maxAttempts"`
	// WarnPercent 成员用量超过容量的这个比例时记一条预警。
	WarnPercent int          `json:"warnPercent"`
	Members     []PoolMember `json:"members"`
	CreatedAt   time.Time    `json:"createdAt"`
	UpdatedAt   time.Time    `json:"updatedAt"`
}

// PoolMember 池成员。
type PoolMember struct {
	ConfigID int64 `json:"configId"`
	// Priority 越小越先用；优先级策略与同分时的次序都看它。
	Priority int `json:"priority"`
	Weight   int `json:"weight"`
	// CapacityBytes 写入上限，0 为不限。到达上限后不再写入，转去其他成员。
	CapacityBytes int64  `json:"capacityBytes"`
	Mode          string `json:"mode"`

	// 以下为运行时信息，只读。
	Config *PoolMemberConfig `json:"config,omitempty"`
	Usage  *MemberUsage      `json:"usage,omitempty"`
	Health *MemberHealth     `json:"health,omitempty"`
	State  string            `json:"state,omitempty"`
}

// PoolMemberConfig 成员对应存储配置的摘要。
type PoolMemberConfig struct {
	Name       string `json:"name"`
	Provider   string `json:"provider"`
	Enabled    bool   `json:"enabled"`
	AccessMode string `json:"accessMode"`
}

// MemberUsage 成员用量。
type MemberUsage struct {
	UsedBytes int64 `json:"usedBytes"`
	Objects   int64 `json:"objects"`
	// Source index：Aegis 索引过的文件之和；cloudflare-analytics：提供商报告的真实用量。
	Source    string    `json:"source"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// MemberHealth 成员健康记录（进程内统计，重启后清零）。
type MemberHealth struct {
	ConsecutiveFailures int        `json:"consecutiveFailures"`
	BackoffUntil        *time.Time `json:"backoffUntil,omitempty"`
	LastError           string     `json:"lastError,omitempty"`
	LastLatencyMs       int64      `json:"lastLatencyMs"`
	LastSuccessAt       *time.Time `json:"lastSuccessAt,omitempty"`
	LastFailureAt       *time.Time `json:"lastFailureAt,omitempty"`
	LastProbeAt         *time.Time `json:"lastProbeAt,omitempty"`
	Uploads             int64      `json:"uploads"`
	Failures            int64      `json:"failures"`
}

// PoolInput 新建 / 修改存储池。
type PoolInput struct {
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	Strategy     string            `json:"strategy"`
	Enabled      bool              `json:"enabled"`
	IsDefault    bool              `json:"isDefault"`
	ReadFallback bool              `json:"readFallback"`
	MaxAttempts  int               `json:"maxAttempts"`
	WarnPercent  int               `json:"warnPercent"`
	Members      []PoolMemberInput `json:"members"`
}

// PoolMemberInput 成员设置。
type PoolMemberInput struct {
	ConfigID      int64  `json:"configId"`
	Priority      int    `json:"priority"`
	Weight        int    `json:"weight"`
	CapacityBytes int64  `json:"capacityBytes"`
	Mode          string `json:"mode"`
}

// PoolPlanStep 一次写入的候选顺序里的一步（模拟用）。
type PoolPlanStep struct {
	ConfigID int64  `json:"configId"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	// Eligible 是否会被尝试；不会尝试时 Reason 说明原因。
	Eligible bool   `json:"eligible"`
	Reason   string `json:"reason,omitempty"`
}

// PoolPlan 模拟一次写入的结果。
type PoolPlan struct {
	PoolID   int64          `json:"poolId"`
	Size     int64          `json:"size"`
	Strategy string         `json:"strategy"`
	Steps    []PoolPlanStep `json:"steps"`
	// Target 实际会先写入的成员；没有可写成员时为 0。
	Target int64 `json:"target"`
}

// 池事件级别与种类。
const (
	EventLevelInfo  = "info"
	EventLevelWarn  = "warn"
	EventLevelError = "error"

	EventFailover  = "failover"
	EventBackoff   = "backoff"
	EventRecovered = "recovered"
	EventNearFull  = "near_full"
	EventFull      = "full"
	EventExhausted = "exhausted"
	EventMigration = "migration"
	EventConfig    = "config"
)

// PoolEvent 存储池与成员的运行事件。
type PoolEvent struct {
	ID         int64          `json:"id"`
	PoolID     *int64         `json:"poolId,omitempty"`
	ConfigID   *int64         `json:"configId,omitempty"`
	Level      string         `json:"level"`
	Kind       string         `json:"kind"`
	Message    string         `json:"message"`
	Detail     map[string]any `json:"detail,omitempty"`
	CreatedAt  time.Time      `json:"createdAt"`
	PoolName   string         `json:"poolName,omitempty"`
	ConfigName string         `json:"configName,omitempty"`
}

// EventQuery 事件查询。
type EventQuery struct {
	PoolID   *int64
	ConfigID *int64
	Level    string
	Limit    int
}

// 迁移任务状态。
const (
	MigrationPending   = "pending"
	MigrationRunning   = "running"
	MigrationPaused    = "paused"
	MigrationCompleted = "completed"
	MigrationFailed    = "failed"
	MigrationCancelled = "cancelled"
)

// Migration 把一个存储配置里已索引的对象复制到另一个配置（键不变）。
type Migration struct {
	ID             int64  `json:"id"`
	SourceConfigID int64  `json:"sourceConfigId"`
	TargetConfigID int64  `json:"targetConfigId"`
	Status         string `json:"status"`
	DeleteSource   bool   `json:"deleteSource"`
	SetRedirect    bool   `json:"setRedirect"`
	TotalObjects   int64  `json:"totalObjects"`
	TotalBytes     int64  `json:"totalBytes"`
	CopiedObjects  int64  `json:"copiedObjects"`
	CopiedBytes    int64  `json:"copiedBytes"`
	// RecoveredObjects 源对象已丢失、从数据库副本写回目标的个数（已计入 CopiedObjects）。
	RecoveredObjects int64 `json:"recoveredObjects"`
	// SkippedObjects 源对象已丢失且没有副本：索引改到目标并标记为 missing。
	SkippedObjects int64      `json:"skippedObjects"`
	FailedObjects  int64      `json:"failedObjects"`
	LastObjectID   int64      `json:"lastObjectId"`
	LastError      string     `json:"lastError,omitempty"`
	CreatedBy      string     `json:"createdBy,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	StartedAt      *time.Time `json:"startedAt,omitempty"`
	FinishedAt     *time.Time `json:"finishedAt,omitempty"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	SourceName     string     `json:"sourceName,omitempty"`
	TargetName     string     `json:"targetName,omitempty"`
}

// MigrationInput 新建迁移。
type MigrationInput struct {
	SourceConfigID int64 `json:"sourceConfigId"`
	TargetConfigID int64 `json:"targetConfigId"`
	DeleteSource   bool  `json:"deleteSource"`
	SetRedirect    bool  `json:"setRedirect"`
}

// ProbeStep 深度检测的一步。
type ProbeStep struct {
	Name      string `json:"name"`
	OK        bool   `json:"ok"`
	LatencyMs int64  `json:"latencyMs"`
	Message   string `json:"message,omitempty"`
	Skipped   bool   `json:"skipped,omitempty"`
}

// ProbeResult 深度检测：连通、写入、读回校验、删除各走一遍。
type ProbeResult struct {
	ConfigID int64          `json:"configId"`
	Provider string         `json:"provider"`
	OK       bool           `json:"ok"`
	Steps    []ProbeStep    `json:"steps"`
	Metadata map[string]any `json:"metadata,omitempty"`
	Usage    *BucketUsage   `json:"usage,omitempty"`
}

// ConfigCapabilities 提供商能力（控制台据此决定显示哪些操作）。
type ConfigCapabilities struct {
	DirectUpload bool `json:"directUpload"`
	Delete       bool `json:"delete"`
	CORS         bool `json:"cors"`
	BucketUsage  bool `json:"bucketUsage"`
}

// ConfigOverview 存储中心总览里的一个配置：配置本身 + 用量 + 健康 + 能力 + 所在的池。
type ConfigOverview struct {
	Config       Config             `json:"config"`
	Usage        *MemberUsage       `json:"usage,omitempty"`
	Health       *MemberHealth      `json:"health,omitempty"`
	State        string             `json:"state"`
	Capabilities ConfigCapabilities `json:"capabilities"`
	PoolIDs      []int64            `json:"poolIds"`
	// CapacityBytes 所在池给它设的容量上限（多个池取最小），0 为不限。
	CapacityBytes int64 `json:"capacityBytes"`
}

// Overview 存储中心总览。
type Overview struct {
	Configs    []ConfigOverview `json:"configs"`
	Pools      []Pool           `json:"pools"`
	Migrations []Migration      `json:"migrations"`
	// WriteTarget 不指定配置名时平台级写入实际落到哪里。
	WriteTarget *WriteTargetInfo `json:"writeTarget,omitempty"`
	Totals      Totals           `json:"totals"`
	GeneratedAt time.Time        `json:"generatedAt"`
}

// Totals 总览汇总。
type Totals struct {
	Configs       int   `json:"configs"`
	Healthy       int   `json:"healthy"`
	Unhealthy     int   `json:"unhealthy"`
	UsedBytes     int64 `json:"usedBytes"`
	Objects       int64 `json:"objects"`
	CapacityBytes int64 `json:"capacityBytes"`
}

// CORSRule 存储桶的一条 CORS 规则。
type CORSRule struct {
	AllowedOrigins []string `json:"allowedOrigins"`
	AllowedMethods []string `json:"allowedMethods"`
	AllowedHeaders []string `json:"allowedHeaders"`
	ExposeHeaders  []string `json:"exposeHeaders"`
	MaxAgeSeconds  int      `json:"maxAgeSeconds"`
}

// WriteTargetInfo 写入目标的描述：一个存储配置，或一个存储池。
type WriteTargetInfo struct {
	Kind     string `json:"kind"` // config / pool
	ConfigID int64  `json:"configId,omitempty"`
	PoolID   int64  `json:"poolId,omitempty"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Scope    string `json:"scope"`
}
