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
