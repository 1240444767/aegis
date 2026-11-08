package httptransport

// ReportQueryParams 报表查询参数。start / end 可以是 RFC3339 时刻，也可以是 YYYY-MM-DD（按 tz 解释）；
// 两种写法都按 tz 取日历日，含首尾两天。
type ReportQueryParams struct {
	Start       string `form:"start" binding:"required"`
	End         string `form:"end" binding:"required"`
	TZ          string `form:"tz"`
	Granularity string `form:"granularity"`
	// 留存：unit = day / week，mode = exact / rolling
	Unit string `form:"unit"`
	Mode string `form:"mode"`
}

// ExportQueryParams 报表导出参数。type 为报表名或 all
type ExportQueryParams struct {
	ReportQueryParams
	Type string `form:"type" binding:"required"`
}
