// Package reportengine 报表分析中心的计算层：时间窗与分桶、用户位图、描述统计与异常检测。
//
// 这里不碰数据库。仓储层只负责把明细（或按日去重后的明细）流出来，所有多维计算在内存里做：
//   - 去重人数一律用 roaring 位图（DAU / WAU / MAU、留存、生命周期分层、漏斗都是位图的并与交），
//     比在 SQL 里对每个窗口各跑一次 COUNT(DISTINCT) 快一个数量级，而且口径天然一致；
//   - 分位数、均值方差、线性趋势用 gonum/stat。
package reportengine

import (
	"fmt"
	"strings"
	"time"

	reportdomain "aegis/internal/domain/report"
)

// MaxDays 单次报表最多覆盖的天数
const MaxDays = 366

// Window 一次报表请求的统计窗口：调用方时区下 [Start, Start+Days) 的日历日。
type Window struct {
	Loc         *time.Location
	TZ          string
	Granularity string
	// Start 首日零点（调用方时区）
	Start time.Time
	Days  int

	startCivil  time.Time // 首日的 UTC 零点，只用来做日历日相减
	bucketOfDay []int
	buckets     []string
}

// NewWindow 由起止时刻构造窗口。from / to 按 tz 取日历日，含首尾两天。
func NewWindow(from, to time.Time, tz, granularity string) (*Window, error) {
	tz = strings.TrimSpace(tz)
	if tz == "" {
		tz = "Asia/Shanghai"
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("无效的时区 %q", tz)
	}
	switch granularity {
	case reportdomain.GranularityDay, reportdomain.GranularityWeek, reportdomain.GranularityMonth:
	case "":
		granularity = reportdomain.GranularityDay
	default:
		return nil, fmt.Errorf("不支持的时间粒度 %q", granularity)
	}
	fromLocal, toLocal := from.In(loc), to.In(loc)
	startCivil := civil(fromLocal)
	endCivil := civil(toLocal)
	days := int(endCivil.Sub(startCivil).Hours()/24) + 1
	if days <= 0 {
		return nil, fmt.Errorf("结束日期早于开始日期")
	}
	if days > MaxDays {
		return nil, fmt.Errorf("统计范围最多 %d 天", MaxDays)
	}
	w := &Window{
		Loc:         loc,
		TZ:          tz,
		Granularity: granularity,
		Start:       time.Date(fromLocal.Year(), fromLocal.Month(), fromLocal.Day(), 0, 0, 0, 0, loc),
		Days:        days,
		startCivil:  startCivil,
	}
	w.layoutBuckets()
	return w, nil
}

func civil(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func (w *Window) layoutBuckets() {
	w.bucketOfDay = make([]int, w.Days)
	w.buckets = w.buckets[:0]
	last := ""
	for i := 0; i < w.Days; i++ {
		day := w.startCivil.AddDate(0, 0, i)
		label := bucketLabel(day, w.Granularity)
		if label != last {
			w.buckets = append(w.buckets, label)
			last = label
		}
		w.bucketOfDay[i] = len(w.buckets) - 1
	}
}

func bucketLabel(day time.Time, granularity string) string {
	switch granularity {
	case reportdomain.GranularityWeek:
		// ISO 周：以周一为起点，标签取周一的日期（可能早于窗口首日）
		offset := (int(day.Weekday()) + 6) % 7
		return day.AddDate(0, 0, -offset).Format("2006-01-02")
	case reportdomain.GranularityMonth:
		return day.Format("2006-01")
	default:
		return day.Format("2006-01-02")
	}
}

// End 窗口结束时刻（不含），即末日次日零点
func (w *Window) End() time.Time { return w.DayStart(w.Days) }

// PrevStart 上一个等长窗口的开始时刻
func (w *Window) PrevStart() time.Time { return w.DayStart(-w.Days) }

// DayStart 第 i 天零点（i 可为负，指窗口之前）。按日历日推算，夏令时切换日也落在正确的零点
func (w *Window) DayStart(i int) time.Time {
	d := w.startCivil.AddDate(0, 0, i)
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, w.Loc)
}

// DayLabel 第 i 天的日期
func (w *Window) DayLabel(i int) string { return w.startCivil.AddDate(0, 0, i).Format("2006-01-02") }

// DayIndex 日期（PG date 列扫出来是 UTC 零点）相对首日的天数，窗口之前为负
func (w *Window) DayIndex(date time.Time) int {
	return int(civil(date).Sub(w.startCivil).Hours() / 24)
}

// DayIndexOf 时刻在调用方时区下相对首日的天数
func (w *Window) DayIndexOf(t time.Time) int {
	return w.DayIndex(t.In(w.Loc))
}

// Bucket 第 i 天所在的桶，窗口外返回 -1
func (w *Window) Bucket(day int) int {
	if day < 0 || day >= w.Days {
		return -1
	}
	return w.bucketOfDay[day]
}

// Buckets 桶标签
func (w *Window) Buckets() []string { return w.buckets }

// BucketCount 桶数
func (w *Window) BucketCount() int { return len(w.buckets) }

// Zeros 一条按桶补零的序列
func (w *Window) Zeros() []float64 { return make([]float64, len(w.buckets)) }

// Public 对外的窗口描述
func (w *Window) Public(now time.Time) reportdomain.Window {
	return reportdomain.Window{
		From:         w.DayLabel(0),
		To:           w.DayLabel(w.Days - 1),
		PreviousFrom: w.DayLabel(-w.Days),
		PreviousTo:   w.DayLabel(-1),
		TimeZone:     w.TZ,
		Granularity:  w.Granularity,
		Days:         w.Days,
		Buckets:      append([]string(nil), w.buckets...),
		GeneratedAt:  now.UTC(),
	}
}

// ElapsedDays 截至 now 窗口里已经开始的天数（今天算一天），用于日均类指标不被未来的空日拉低
func (w *Window) ElapsedDays(now time.Time) int {
	n := w.DayIndexOf(now) + 1
	if n > w.Days {
		return w.Days
	}
	if n < 1 {
		return 1
	}
	return n
}

// Weekday 时刻在调用方时区下的星期（0 = 周一）与小时
func (w *Window) Weekday(t time.Time) (int, int) {
	local := t.In(w.Loc)
	return (int(local.Weekday()) + 6) % 7, local.Hour()
}
