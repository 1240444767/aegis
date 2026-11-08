package reportengine

import (
	"math"
	"testing"
	"time"

	reportdomain "aegis/internal/domain/report"
)

func TestWindowBucketsAndCivilDays(t *testing.T) {
	sh, _ := time.LoadLocation("Asia/Shanghai")
	// 前端传上海零点的 RFC3339（UTC 表示是前一天 16:00），必须落在上海的 10-01
	from := time.Date(2026, 9, 30, 16, 0, 0, 0, time.UTC)
	to := time.Date(2026, 10, 14, 15, 59, 59, 0, time.UTC)
	w, err := NewWindow(from, to, "Asia/Shanghai", reportdomain.GranularityWeek)
	if err != nil {
		t.Fatal(err)
	}
	if w.Days != 14 || w.DayLabel(0) != "2026-10-01" || w.DayLabel(13) != "2026-10-14" {
		t.Fatalf("窗口 = %d 天 %s ~ %s", w.Days, w.DayLabel(0), w.DayLabel(13))
	}
	// 2026-10-01 是周四：周桶以周一为标签，首桶标签早于窗口首日
	want := []string{"2026-09-28", "2026-10-05", "2026-10-12"}
	if got := w.Buckets(); len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("周桶 = %v", got)
	}
	if w.Bucket(3) != 0 || w.Bucket(4) != 1 || w.Bucket(11) != 2 || w.Bucket(14) != -1 {
		t.Fatalf("天到桶的映射不对")
	}
	if got := w.DayIndexOf(time.Date(2026, 10, 1, 23, 30, 0, 0, sh)); got != 0 {
		t.Fatalf("上海 10-01 23:30 应在第 0 天，得到 %d", got)
	}
	if got := w.DayIndexOf(time.Date(2026, 10, 1, 16, 30, 0, 0, time.UTC)); got != 1 {
		t.Fatalf("UTC 10-01 16:30 = 上海 10-02 00:30，应在第 1 天，得到 %d", got)
	}
	if !w.End().Equal(time.Date(2026, 10, 15, 0, 0, 0, 0, sh)) || !w.PrevStart().Equal(time.Date(2026, 9, 17, 0, 0, 0, 0, sh)) {
		t.Fatalf("End / PrevStart 不对：%v %v", w.End(), w.PrevStart())
	}
	if _, err := NewWindow(to, from, "Asia/Shanghai", ""); err == nil {
		t.Fatal("结束早于开始应报错")
	}
	if _, err := NewWindow(from, from.AddDate(2, 0, 0), "UTC", ""); err == nil {
		t.Fatal("超过 366 天应报错")
	}
}

func TestWindowDaylightSaving(t *testing.T) {
	ny, _ := time.LoadLocation("America/New_York")
	// 2026-11-01 纽约夏令时结束，那天有 25 小时
	w, err := NewWindow(time.Date(2026, 10, 31, 12, 0, 0, 0, ny), time.Date(2026, 11, 2, 12, 0, 0, 0, ny), "America/New_York", "")
	if err != nil {
		t.Fatal(err)
	}
	if w.Days != 3 {
		t.Fatalf("天数 = %d", w.Days)
	}
	if got := w.DayIndexOf(time.Date(2026, 11, 1, 23, 30, 0, 0, ny)); got != 1 {
		t.Fatalf("11-01 23:30 应在第 1 天，得到 %d", got)
	}
	if !w.DayStart(2).Equal(time.Date(2026, 11, 2, 0, 0, 0, 0, ny)) {
		t.Fatalf("11-02 零点 = %v", w.DayStart(2))
	}
}

func TestDaySetsRollingAndCounts(t *testing.T) {
	d := NewDaySets(2, 5, 1)
	d.Add(-2, 1)
	d.Add(0, 1)
	d.Add(0, 2)
	d.Add(1, 2)
	d.Add(4, 3)
	d.Add(5, 1)
	d.Add(9, 9) // 越界丢弃
	if got := d.Rolling(5, 1); got[0] != 2 || got[1] != 1 || got[4] != 1 {
		t.Fatalf("DAU = %v", got)
	}
	// 3 日滚动：第 1 天的窗口是 [-1, 1]
	if got := d.Rolling(5, 3); got[0] != 2 || got[1] != 2 || got[2] != 2 || got[3] != 1 || got[4] != 1 {
		t.Fatalf("3 日滚动 = %v", got)
	}
	if got := Cardinality(d.Union(-10, 10)); got != 3 {
		t.Fatalf("全段并集 = %v", got)
	}
	counts := d.DayCounts(-2, 5)
	if counts[1] != 3 || counts[2] != 2 || counts[3] != 1 {
		t.Fatalf("活跃天数 = %v", counts)
	}
}

func TestSummaryHistogramAndBands(t *testing.T) {
	s := Summarize([]float64{5, 1, 3, 2, 4})
	if s.Count != 5 || s.Sum != 15 || s.Mean != 3 || s.Min != 1 || s.Max != 5 || s.P50 != 3 {
		t.Fatalf("Summary = %+v", s)
	}
	if math.Abs(s.Std-1.5811) > 1e-3 {
		t.Fatalf("样本标准差 = %v", s.Std)
	}
	edges := MoneyEdges(30)
	bins := Histogram([]float64{0.5, 1, 1.5, 9.99, 30, 31}, edges, func(lo, hi float64) string { return "" })
	var total int64
	for _, b := range bins {
		total += b.Count
	}
	if total != 6 || bins[0].Count != 1 || bins[1].Count != 2 {
		t.Fatalf("直方图 = %+v", bins)
	}
	bands := Bands(map[uint64]int{1: 1, 2: 2, 3: 3, 4: 4, 5: 20}, []int{1, 2, 4, 8, 16})
	if bands[0].Key != "1" || bands[1].Key != "2-3" || bands[4].Key != "16+" ||
		bands[0].Count != 1 || bands[1].Count != 2 || bands[2].Count != 1 || bands[4].Count != 1 {
		t.Fatalf("分档 = %+v", bands)
	}
}

func TestToSlicesMergesSortsAndTruncates(t *testing.T) {
	out := ToSlices([]Group{
		{Key: "a", Count: 1}, {Key: "", Count: 2}, {Key: " ", Count: 3}, {Key: "b", Count: 10}, {Key: "c", Count: 4},
	}, SliceOptions{Top: 2})
	if len(out) != 3 || out[0].Key != "b" || out[1].Key != "unknown" || out[1].Count != 5 || out[2].Key != OtherKey || out[2].Count != 5 {
		t.Fatalf("分布 = %+v", out)
	}
	if math.Abs(out[0].Share-0.5) > 1e-9 {
		t.Fatalf("占比 = %v", out[0].Share)
	}
}

func TestAnomaliesFindsSpikeAndTrend(t *testing.T) {
	series := []float64{10, 11, 9, 10, 12, 10, 11, 80, 10, 9}
	labels := make([]string, len(series))
	for i := range labels {
		labels[i] = string(rune('a' + i))
	}
	found := Anomalies("x", labels, series, 7, 3, 5)
	if len(found) == 0 || found[0].Kind != "spike" || found[0].Bucket != "h" {
		t.Fatalf("应找到第 8 个点的突增：%+v", found)
	}
	growth := Anomalies("x", labels[:8], []float64{10, 20, 30, 40, 50, 60, 70, 80}, 7, 100, 5)
	if len(growth) != 1 || growth[0].Kind != "growth" {
		t.Fatalf("应识别为增长：%+v", growth)
	}
}

func TestNewKPIChange(t *testing.T) {
	k := NewKPI(120, Ptr(100), nil)
	if k.Change == nil || math.Abs(*k.Change-0.2) > 1e-9 {
		t.Fatalf("变化 = %v", k.Change)
	}
	if k := NewKPI(5, Ptr(0), nil); k.Change != nil {
		t.Fatal("上期为 0 时不给变化率")
	}
}
