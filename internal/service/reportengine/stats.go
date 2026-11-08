package reportengine

import (
	"math"
	"slices"
	"sort"

	reportdomain "aegis/internal/domain/report"

	"gonum.org/v1/gonum/floats"
	"gonum.org/v1/gonum/stat"
)

// Summarize 描述统计（分位数用经验分布，与 Excel 的 PERCENTILE.INC 不同但对报表足够，且不插值出不存在的金额）
func Summarize(values []float64) reportdomain.Summary {
	if len(values) == 0 {
		return reportdomain.Summary{}
	}
	x := slices.Clone(values)
	sort.Float64s(x)
	mean, std := stat.MeanStdDev(x, nil)
	if len(x) < 2 || math.IsNaN(std) {
		std = 0
	}
	q := func(p float64) float64 { return round(stat.Quantile(p, stat.Empirical, x, nil), 4) }
	return reportdomain.Summary{
		Count: int64(len(x)),
		Sum:   round(floats.Sum(x), 4),
		Mean:  round(mean, 4),
		Std:   round(std, 4),
		Min:   x[0],
		P25:   q(0.25),
		P50:   q(0.5),
		P75:   q(0.75),
		P90:   q(0.9),
		P99:   q(0.99),
		Max:   x[len(x)-1],
	}
}

// Histogram 按给定边界分箱：edges 升序，最后一箱为 [edges[n-1], +∞)
func Histogram(values []float64, edges []float64, label func(lo, hi float64) string) []reportdomain.Bin {
	if len(edges) == 0 {
		return []reportdomain.Bin{}
	}
	bins := make([]reportdomain.Bin, len(edges))
	for i, lo := range edges {
		hi := math.Inf(1)
		if i+1 < len(edges) {
			hi = edges[i+1]
		}
		bins[i] = reportdomain.Bin{Label: label(lo, hi), Min: lo, Max: hi}
	}
	for _, v := range values {
		i := sort.SearchFloat64s(edges, v)
		if i == len(edges) || edges[i] > v {
			i--
		}
		if i < 0 {
			i = 0
		}
		bins[i].Count++
	}
	// JSON 编不了 +Inf
	if last := &bins[len(bins)-1]; math.IsInf(last.Max, 1) {
		last.Max = 0
	}
	return bins
}

// MoneyEdges 金额直方图的边界（元）：按 1-2-5 序列铺到覆盖最大值
func MoneyEdges(maxValue float64) []float64 {
	edges := []float64{0}
	steps := []float64{1, 2, 5}
	for scale := 1.0; ; scale *= 10 {
		for _, s := range steps {
			edge := s * scale
			edges = append(edges, edge)
			if edge > maxValue || len(edges) >= 14 {
				return edges
			}
		}
	}
}

// Anomalies 在一条按桶序列上找突增 / 突降：以前 lookback 个桶为基线算均值与标准差，
// 偏离超过 threshold 个标准差、且绝对偏差不小于 minDelta 的桶记一条。
// 另外对整条序列做一元线性回归，斜率折算到整段的相对变化超过 30% 时记一条增长 / 下滑。
func Anomalies(metric string, buckets []string, series []float64, lookback int, threshold, minDelta float64) []reportdomain.Insight {
	out := make([]reportdomain.Insight, 0)
	if len(series) < 4 {
		return out
	}
	for i := 3; i < len(series); i++ {
		from := i - lookback
		if from < 0 {
			from = 0
		}
		base := series[from:i]
		mean, std := stat.MeanStdDev(base, nil)
		if math.IsNaN(std) {
			continue
		}
		// 基线完全平稳时标准差为 0：用均值的平方根（泊松近似）兜底，避免任何波动都被判成异常
		floor := math.Max(math.Sqrt(math.Max(mean, 1)), 1)
		if std < floor {
			std = floor
		}
		delta := series[i] - mean
		z := delta / std
		if math.Abs(z) < threshold || math.Abs(delta) < minDelta {
			continue
		}
		kind := "spike"
		if z < 0 {
			kind = "drop"
		}
		out = append(out, reportdomain.Insight{
			Kind: kind, Metric: metric, Bucket: buckets[i],
			Value: series[i], Expected: round(mean, 2), Score: round(z, 2),
		})
	}
	if trend := Trend(series); trend != nil {
		if math.Abs(*trend) >= 0.3 {
			kind := "growth"
			if *trend < 0 {
				kind = "decline"
			}
			out = append(out, reportdomain.Insight{Kind: kind, Metric: metric, Value: round(*trend, 4), Score: round(*trend, 4)})
		}
	}
	return out
}

// Trend 一元线性回归的斜率折算成整段的相对变化：slope × (n-1) / 均值。均值为 0 时无意义，返回 nil
func Trend(series []float64) *float64 {
	n := len(series)
	if n < 4 {
		return nil
	}
	mean := stat.Mean(series, nil)
	if mean <= 0 {
		return nil
	}
	xs := make([]float64, n)
	for i := range xs {
		xs[i] = float64(i)
	}
	_, slope := stat.LinearRegression(xs, series, nil, false)
	if math.IsNaN(slope) {
		return nil
	}
	rel := slope * float64(n-1) / mean
	return &rel
}

// NewKPI 本期值、上期值（可为 nil）与走势
func NewKPI(value float64, previous *float64, spark []float64) reportdomain.KPI {
	k := reportdomain.KPI{Value: round(value, 4), Spark: spark}
	if previous != nil {
		p := round(*previous, 4)
		k.Previous = &p
		if p != 0 {
			c := round((value-p)/math.Abs(p), 4)
			k.Change = &c
		}
	}
	return k
}

// Ptr 取地址的小工具
func Ptr(v float64) *float64 { return &v }

// Ratio 安全除法，分母为 0 时得 0
func Ratio(a, b float64) float64 {
	if b == 0 {
		return 0
	}
	return a / b
}

// Sum 序列求和
func Sum(series []float64) float64 { return floats.Sum(series) }

// Round 保留 n 位小数
func Round(v float64, n int) float64 { return round(v, n) }

func round(v float64, n int) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	p := math.Pow10(n)
	return math.Round(v*p) / p
}
