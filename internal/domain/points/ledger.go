package points

import "time"

// 管理端按用户查看积分 / 经验流水。
//
// 方向按 amount 的正负划分（in 为入账、out 为出账），不按 type 列：
// 历史上 type 有 earn / spend / admin 等多种写法，正负号才是唯一可靠的口径。

// LedgerQuery 流水筛选条件。From / To 为左闭右开的时间区间。
type LedgerQuery struct {
	Kind      Kind
	AppID     int64
	UserID    int64
	Direction string
	Category  string
	Keyword   string
	From      *time.Time
	To        *time.Time
	Page      int
	Limit     int
}

// LedgerPage 流水分页结果，附带当前筛选条件下的入账、出账合计。
type LedgerPage struct {
	Items   []Transaction `json:"items"`
	Total   int64         `json:"total"`
	Page    int           `json:"page"`
	Limit   int           `json:"limit"`
	SumIn   int64         `json:"sumIn"`
	SumOut  int64         `json:"sumOut"`
	Kind    Kind          `json:"kind"`
	Balance int64         `json:"balance"`
	Level   int           `json:"level,omitempty"`
}

// LedgerCategory 按分类聚合的流水。Title 取该分类最近一笔的标题，用作展示名。
type LedgerCategory struct {
	Category string     `json:"category"`
	Title    string     `json:"title"`
	In       int64      `json:"in"`
	Out      int64      `json:"out"`
	Count    int64      `json:"count"`
	LastAt   *time.Time `json:"lastAt,omitempty"`
}

// LedgerDay 某一天（按请求时区）的入账、出账与当日最后一笔后的余额。
// 没有流水的日子不返回，由前端沿用前一天的余额补齐。
type LedgerDay struct {
	Date    string `json:"date"`
	In      int64  `json:"in"`
	Out     int64  `json:"out"`
	Count   int64  `json:"count"`
	Balance int64  `json:"balance"`
}

// LedgerSummary 单个用户某一类数值的整体画像。
type LedgerSummary struct {
	Kind       Kind             `json:"kind"`
	Balance    int64            `json:"balance"`
	TotalIn    int64            `json:"totalIn"`
	TotalOut   int64            `json:"totalOut"`
	Count      int64            `json:"count"`
	AdminIn    int64            `json:"adminIn"`
	AdminOut   int64            `json:"adminOut"`
	FirstAt    *time.Time       `json:"firstAt,omitempty"`
	LastAt     *time.Time       `json:"lastAt,omitempty"`
	Categories []LedgerCategory `json:"categories"`
	Days       int              `json:"days"`
	TimeZone   string           `json:"timeZone"`
	// BalanceBefore 统计窗口开始前的余额，前端补齐每日余额曲线的起点
	BalanceBefore int64       `json:"balanceBefore"`
	Daily         []LedgerDay `json:"daily"`
}

// ── 全应用视图：不限定用户 ──
//
// 按用户一个个点过去无法对账，所以另有一套以应用为范围的流水与汇总。
// 查询条件复用 LedgerQuery，UserID 为 0 即全应用；关键词额外匹配账号与昵称。

// AppLedgerItem 全应用流水的一行：流水本身加上用户的账号与昵称。
type AppLedgerItem struct {
	Transaction
	Account  string `json:"account,omitempty"`
	Nickname string `json:"nickname,omitempty"`
}

// AppLedgerPage 全应用流水分页，附带当前筛选下的入账、出账合计与涉及人数。
type AppLedgerPage struct {
	Items  []AppLedgerItem `json:"items"`
	Total  int64           `json:"total"`
	Page   int             `json:"page"`
	Limit  int             `json:"limit"`
	SumIn  int64           `json:"sumIn"`
	SumOut int64           `json:"sumOut"`
	Users  int64           `json:"users"`
	Kind   Kind            `json:"kind"`
}

// AppLedgerDay 某一天（按请求时区）的全应用入账、出账、笔数与涉及人数。
type AppLedgerDay struct {
	Date  string `json:"date"`
	In    int64  `json:"in"`
	Out   int64  `json:"out"`
	Count int64  `json:"count"`
	Users int64  `json:"users"`
}

// AppLedgerHolder 余额排行里的一位用户。
type AppLedgerHolder struct {
	UserID   int64  `json:"userId"`
	Account  string `json:"account,omitempty"`
	Nickname string `json:"nickname,omitempty"`
	Balance  int64  `json:"balance"`
}

// AppLedgerSummary 应用某一类数值的整体画像。
//
// Holders / Outstanding / TopHolders 是当前值（users 表上的余额），其余都是 Days 天窗口内的口径，
// 每日走势缺失的日期由前端补零。
type AppLedgerSummary struct {
	Kind        Kind              `json:"kind"`
	Days        int               `json:"days"`
	TimeZone    string            `json:"timeZone"`
	Users       int64             `json:"users"`
	Holders     int64             `json:"holders"`
	Outstanding int64             `json:"outstanding"`
	TotalIn     int64             `json:"totalIn"`
	TotalOut    int64             `json:"totalOut"`
	Count       int64             `json:"count"`
	ActiveUsers int64             `json:"activeUsers"`
	AdminIn     int64             `json:"adminIn"`
	AdminOut    int64             `json:"adminOut"`
	TodayIn     int64             `json:"todayIn"`
	TodayOut    int64             `json:"todayOut"`
	Categories  []LedgerCategory  `json:"categories"`
	Daily       []AppLedgerDay    `json:"daily"`
	TopHolders  []AppLedgerHolder `json:"topHolders"`
}
