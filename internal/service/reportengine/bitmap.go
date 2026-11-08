package reportengine

import (
	"github.com/RoaringBitmap/roaring/v2/roaring64"
)

// DaySets 每天一个用户位图。索引从 -Lookback 到 Days+Extra-1，
// 窗口之前的天用于 WAU / MAU 的滚动并集与生命周期的上期对照，窗口之后的天用于留存与漏斗的回访观察。
type DaySets struct {
	Lookback int
	sets     []*roaring64.Bitmap
}

// NewDaySets 覆盖 [-lookback, days+extra) 的日集合
func NewDaySets(lookback, days, extra int) *DaySets {
	n := lookback + days + extra
	sets := make([]*roaring64.Bitmap, n)
	for i := range sets {
		sets[i] = roaring64.New()
	}
	return &DaySets{Lookback: lookback, sets: sets}
}

// Len 覆盖的总天数
func (d *DaySets) Len() int { return len(d.sets) }

// First / Last 可用的最小、最大天索引（相对窗口首日）
func (d *DaySets) First() int { return -d.Lookback }
func (d *DaySets) Last() int  { return len(d.sets) - d.Lookback - 1 }

// Add 记一次「用户 uid 在第 day 天活跃」，越界的静默丢弃
func (d *DaySets) Add(day int, uid uint64) {
	if i := day + d.Lookback; i >= 0 && i < len(d.sets) {
		d.sets[i].Add(uid)
	}
}

// Day 第 day 天的位图（越界返回空位图，只读）
func (d *DaySets) Day(day int) *roaring64.Bitmap {
	if i := day + d.Lookback; i >= 0 && i < len(d.sets) {
		return d.sets[i]
	}
	return empty
}

var empty = roaring64.New()

// Union [from, to] 闭区间内各天的并集，自动截到可用范围
func (d *DaySets) Union(from, to int) *roaring64.Bitmap {
	if from < d.First() {
		from = d.First()
	}
	if to > d.Last() {
		to = d.Last()
	}
	if from > to {
		return roaring64.New()
	}
	return roaring64.FastOr(d.sets[from+d.Lookback : to+d.Lookback+1]...)
}

// Merge 把另一组同形状的日集合并进来
func (d *DaySets) Merge(other *DaySets) {
	for i := range d.sets {
		if i < len(other.sets) {
			d.sets[i].Or(other.sets[i])
		}
	}
}

// DayCounts 每个用户在 [from, to] 内活跃的天数
func (d *DaySets) DayCounts(from, to int) map[uint64]int {
	counts := make(map[uint64]int)
	for day := from; day <= to; day++ {
		it := d.Day(day).Iterator()
		for it.HasNext() {
			counts[it.Next()]++
		}
	}
	return counts
}

// Rolling 以窗口内每一天为终点、向前 span 天（含当天）的滚动去重人数
func (d *DaySets) Rolling(days, span int) []float64 {
	out := make([]float64, days)
	if span <= 1 {
		for i := 0; i < days; i++ {
			out[i] = float64(d.Day(i).GetCardinality())
		}
		return out
	}
	for i := 0; i < days; i++ {
		out[i] = float64(d.Union(i-span+1, i).GetCardinality())
	}
	return out
}

// Cardinality 位图人数（float64，便于直接进序列）
func Cardinality(b *roaring64.Bitmap) float64 { return float64(b.GetCardinality()) }

// And / AndNot 返回新位图，不改动参数
func And(a, b *roaring64.Bitmap) *roaring64.Bitmap    { return roaring64.And(a, b) }
func AndNot(a, b *roaring64.Bitmap) *roaring64.Bitmap { return roaring64.AndNot(a, b) }
func Or(bs ...*roaring64.Bitmap) *roaring64.Bitmap    { return roaring64.FastOr(bs...) }
