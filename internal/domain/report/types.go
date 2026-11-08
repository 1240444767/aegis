// Package report 报表分析中心的对外数据形状。
//
// 所有报表共用一个统计窗口（Window）：起止日期是**调用方时区**下的日历日，含首尾两天；
// 时间桶按 day / week / month 切分，桶序与 Trend.Buckets 一一对应，没有数据的桶补 0，
// 图上不会因为缺桶把两天连成一条斜线。KPI 自带上一个等长窗口的对照值。
package report

import "time"

// 时间粒度
const (
	GranularityDay   = "day"
	GranularityWeek  = "week"
	GranularityMonth = "month"
)

// Window 统计窗口
type Window struct {
	From         string    `json:"from"` // 含，YYYY-MM-DD
	To           string    `json:"to"`   // 含
	PreviousFrom string    `json:"previousFrom"`
	PreviousTo   string    `json:"previousTo"`
	TimeZone     string    `json:"timeZone"`
	Granularity  string    `json:"granularity"`
	Days         int       `json:"days"`
	Buckets      []string  `json:"buckets"`
	GeneratedAt  time.Time `json:"generatedAt"`
}

// KPI 一个指标：本期值、上期值与相对变化（0.12 即 +12%），Spark 为本期按桶的走势
type KPI struct {
	Value    float64   `json:"value"`
	Previous *float64  `json:"previous,omitempty"`
	Change   *float64  `json:"change,omitempty"`
	Spark    []float64 `json:"spark,omitempty"`
}

// Trend 按桶的多条序列（列式：Series[key][i] 对应 Buckets[i]）
type Trend struct {
	Buckets []string             `json:"buckets"`
	Series  map[string][]float64 `json:"series"`
}

// Slice 维度分布里的一项。Count 为次数，Users 为去重人数，Amount 为金额（元），
// Value 为各报表自定义的第二指标（如通知的已读数、风控规则的累计分），Share 为占比（0~1）
type Slice struct {
	Key    string            `json:"key"`
	Label  string            `json:"label,omitempty"`
	Count  int64             `json:"count"`
	Users  int64             `json:"users,omitempty"`
	Amount float64           `json:"amount,omitempty"`
	Value  float64           `json:"value,omitempty"`
	Share  float64           `json:"share"`
	Meta   map[string]string `json:"meta,omitempty"`
}

// Heatmap 星期 × 小时。Cells[0] 为周一，按调用方时区
type Heatmap struct {
	Cells [7][24]int64 `json:"cells"`
	Max   int64        `json:"max"`
	Total int64        `json:"total"`
}

// Summary 一组数值的描述统计
type Summary struct {
	Count int64   `json:"count"`
	Sum   float64 `json:"sum"`
	Mean  float64 `json:"mean"`
	Std   float64 `json:"std"`
	Min   float64 `json:"min"`
	P25   float64 `json:"p25"`
	P50   float64 `json:"p50"`
	P75   float64 `json:"p75"`
	P90   float64 `json:"p90"`
	P99   float64 `json:"p99"`
	Max   float64 `json:"max"`
}

// Bin 直方图的一格，[Min, Max)
type Bin struct {
	Label string  `json:"label"`
	Min   float64 `json:"min"`
	Max   float64 `json:"max"`
	Count int64   `json:"count"`
}

// Insight 自动发现的异常点或趋势
type Insight struct {
	Kind     string  `json:"kind"` // spike / drop / growth / decline
	Metric   string  `json:"metric"`
	Bucket   string  `json:"bucket,omitempty"`
	Value    float64 `json:"value"`
	Expected float64 `json:"expected"`
	Score    float64 `json:"score"` // 偏离的标准差倍数，或趋势的相对斜率
}

// Lifecycle 用户生命周期分层（相对上一个等长窗口）
type Lifecycle struct {
	New         int64 `json:"new"`         // 本期注册且本期活跃
	Retained    int64 `json:"retained"`    // 上期活跃、本期仍活跃
	Resurrected int64 `json:"resurrected"` // 上期未活跃、更早注册、本期回来了
	Churned     int64 `json:"churned"`     // 上期活跃、本期未活跃
}

// Overview 总览
type Overview struct {
	Window    Window         `json:"window"`
	KPIs      map[string]KPI `json:"kpis"`
	Trend     Trend          `json:"trend"`
	Lifecycle Lifecycle      `json:"lifecycle"`
	Insights  []Insight      `json:"insights"`
}

// Users 用户增长（注册）
type Users struct {
	Window    Window         `json:"window"`
	KPIs      map[string]KPI `json:"kpis"`
	Trend     Trend          `json:"trend"`
	Sources   []Slice        `json:"sources"`
	Provinces []Slice        `json:"provinces"`
	Weekdays  []Slice        `json:"weekdays"`
	Heatmap   Heatmap        `json:"heatmap"`
	Insights  []Insight      `json:"insights"`
}

// Login 登录
type Login struct {
	Window     Window         `json:"window"`
	KPIs       map[string]KPI `json:"kpis"`
	Trend      Trend          `json:"trend"`
	Types      []Slice        `json:"types"`
	Providers  []Slice        `json:"providers"`
	Failures   []Slice        `json:"failures"`
	FailureIPs []Slice        `json:"failureIps"`
	Frequency  []Slice        `json:"frequency"`
	Heatmap    Heatmap        `json:"heatmap"`
}

// Active 活跃
type Active struct {
	Window    Window         `json:"window"`
	KPIs      map[string]KPI `json:"kpis"`
	Trend     Trend          `json:"trend"`
	Lifecycle Lifecycle      `json:"lifecycle"`
	Frequency []Slice        `json:"frequency"`
	Signals   []Slice        `json:"signals"`
	Heatmap   Heatmap        `json:"heatmap"`
}

// Cohort 留存矩阵的一行。Retained / Rates 里 nil 表示该格对应的日子还没到
type Cohort struct {
	Cohort   string     `json:"cohort"`
	Size     int64      `json:"size"`
	Retained []*int64   `json:"retained"`
	Rates    []*float64 `json:"rates"`
}

// Retention 留存
type Retention struct {
	Window  Window         `json:"window"`
	Unit    string         `json:"unit"` // day / week
	Mode    string         `json:"mode"` // exact：恰好第 N 期回访；rolling：第 N 期及之后任一期回访
	Offsets []int          `json:"offsets"`
	Rows    []Cohort       `json:"rows"`
	Average []*float64     `json:"average"` // 按队列人数加权
	KPIs    map[string]KPI `json:"kpis"`
}

// Device 设备
type Device struct {
	Window        Window         `json:"window"`
	KPIs          map[string]KPI `json:"kpis"`
	Platforms     []Slice        `json:"platforms"`
	Manufacturers []Slice        `json:"manufacturers"`
	Models        []Slice        `json:"models"`
	OSVersions    []Slice        `json:"osVersions"`
	AppVersions   []Slice        `json:"appVersions"`
	Browsers      []Slice        `json:"browsers"`
}

// Region 地区
type Region struct {
	Window            Window         `json:"window"`
	KPIs              map[string]KPI `json:"kpis"`
	Countries         []Slice        `json:"countries"`
	Provinces         []Slice        `json:"provinces"`
	Cities            []Slice        `json:"cities"`
	ISPs              []Slice        `json:"isps"`
	RegisterProvinces []Slice        `json:"registerProvinces"`
}

// Channel 渠道
type Channel struct {
	Window         Window         `json:"window"`
	KPIs           map[string]KPI `json:"kpis"`
	Trend          Trend          `json:"trend"` // 按注册来源堆叠，键为来源
	RegisterSource []Slice        `json:"registerSources"`
	LoginProviders []Slice        `json:"loginProviders"`
	InviteSources  []Slice        `json:"inviteSources"`
	CardKeySources []Slice        `json:"cardKeySources"`
}

// Payment 支付
type Payment struct {
	Window    Window         `json:"window"`
	KPIs      map[string]KPI `json:"kpis"`
	Trend     Trend          `json:"trend"`
	Methods   []Slice        `json:"methods"`
	Providers []Slice        `json:"providers"`
	Products  []Slice        `json:"products"`
	Statuses  []Slice        `json:"statuses"`
	VipPlans  []Slice        `json:"vipPlans"`
	Amounts   Summary        `json:"amounts"`
	Histogram []Bin          `json:"histogram"`
	Heatmap   Heatmap        `json:"heatmap"`
	Insights  []Insight      `json:"insights"`
}

// Notification 通知
type Notification struct {
	Window      Window         `json:"window"`
	KPIs        map[string]KPI `json:"kpis"`
	Trend       Trend          `json:"trend"`
	Types       []Slice        `json:"types"`
	Levels      []Slice        `json:"levels"`
	ReadLatency Summary        `json:"readLatency"` // 分钟
}

// Firewall 防火墙拦截（全局，不分应用）
type Firewall struct {
	Total     int64   `json:"total"`
	Trend     Trend   `json:"trend"`
	Severity  []Slice `json:"severity"`
	Rules     []Slice `json:"rules"`
	Countries []Slice `json:"countries"`
	Paths     []Slice `json:"paths"`
}

// Risk 风控
type Risk struct {
	Window   Window         `json:"window"`
	KPIs     map[string]KPI `json:"kpis"`
	Trend    Trend          `json:"trend"` // 键为风险等级
	Levels   []Slice        `json:"levels"`
	Actions  []Slice        `json:"actions"`
	Scenes   []Slice        `json:"scenes"`
	Rules    []Slice        `json:"rules"`
	IPs      []Slice        `json:"ips"`
	Scores   Summary        `json:"scores"`
	Latency  Summary        `json:"latency"` // 毫秒
	Firewall Firewall       `json:"firewall"`
}

// ActivityRow 单个抽奖活动的汇总
type ActivityRow struct {
	ID           int64   `json:"id"`
	Name         string  `json:"name"`
	Status       string  `json:"status"`
	Draws        int64   `json:"draws"`
	Participants int64   `json:"participants"`
	Wins         int64   `json:"wins"`
	WinRate      float64 `json:"winRate"`
	PointsCost   int64   `json:"pointsCost"`
	Pity         int64   `json:"pity"`
}

// Activity 抽奖活动
type Activity struct {
	Window     Window         `json:"window"`
	KPIs       map[string]KPI `json:"kpis"`
	Trend      Trend          `json:"trend"`
	Activities []ActivityRow  `json:"activities"`
	Prizes     []Slice        `json:"prizes"`
	Kinds      []Slice        `json:"kinds"`
	Sources    []Slice        `json:"sources"`
}

// Engagement 互动：签到、积分、激励广告、开屏、卡密
type Engagement struct {
	Window         Window         `json:"window"`
	KPIs           map[string]KPI `json:"kpis"`
	Trend          Trend          `json:"trend"`
	SignSources    []Slice        `json:"signSources"`
	Streaks        []Slice        `json:"streaks"`
	PointsEarned   []Slice        `json:"pointsEarned"`
	PointsSpent    []Slice        `json:"pointsSpent"`
	RewardedAds    []Slice        `json:"rewardedAds"`
	RewardedScenes []Slice        `json:"rewardedScenes"`
	SplashAds      []Slice        `json:"splashAds"`
}

// FunnelStep 漏斗的一步
type FunnelStep struct {
	Key         string  `json:"key"`
	Users       int64   `json:"users"`
	StepRate    float64 `json:"stepRate"`    // 相对上一步
	OverallRate float64 `json:"overallRate"` // 相对第一步
}

// Funnel 本期注册用户的转化漏斗。每一步都是上一步的子集（位图求交），不会出现「后一步比前一步多」
type Funnel struct {
	Window       Window         `json:"window"`
	KPIs         map[string]KPI `json:"kpis"`
	Steps        []FunnelStep   `json:"steps"`
	Trend        Trend          `json:"trend"`        // 每个注册桶的激活率与付费率
	HoursToPay   Summary        `json:"hoursToPay"`   // 注册到首次付费
	DaysToReturn Summary        `json:"daysToReturn"` // 注册到首次回访（注册日之后的第一次活跃）
}

// Table 导出用的二维表
type Table struct {
	Name    string     `json:"name"`
	Columns []string   `json:"columns"`
	Rows    [][]string `json:"rows"`
}
