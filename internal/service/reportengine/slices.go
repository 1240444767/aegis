package reportengine

import (
	"sort"
	"strconv"
	"strings"

	reportdomain "aegis/internal/domain/report"

	"github.com/RoaringBitmap/roaring/v2/roaring64"
)

// OtherKey 合并进「其他」的那一项的键
const OtherKey = "__other__"

// Group 仓储层按维度聚合出来的一行
type Group struct {
	Key    string
	Label  string
	Count  int64
	Users  int64
	Amount float64
	Value  float64
}

// Counter 在内存里做维度聚合：次数、去重人数（位图）与金额
type Counter struct {
	items map[string]*counterItem
	order []string
}

type counterItem struct {
	count  int64
	amount float64
	users  *roaring64.Bitmap
	meta   map[string]string
}

// NewCounter 空计数器
func NewCounter() *Counter { return &Counter{items: map[string]*counterItem{}} }

// Add 记一次。uid 为 0 时不计人数
func (c *Counter) Add(key string, uid uint64, amount float64) {
	key = strings.TrimSpace(key)
	item := c.items[key]
	if item == nil {
		item = &counterItem{users: roaring64.New()}
		c.items[key] = item
		c.order = append(c.order, key)
	}
	item.count++
	item.amount += amount
	if uid != 0 {
		item.users.Add(uid)
	}
}

// SetMeta 给某一项挂附加信息（如设备图、厂商图标）
func (c *Counter) SetMeta(key, name, value string) {
	if item := c.items[strings.TrimSpace(key)]; item != nil && value != "" {
		if item.meta == nil {
			item.meta = map[string]string{}
		}
		item.meta[name] = value
	}
}

// Groups 转成仓储同形状的行
func (c *Counter) Groups() []Group {
	out := make([]Group, 0, len(c.items))
	for _, key := range c.order {
		item := c.items[key]
		out = append(out, Group{Key: key, Count: item.count, Users: int64(item.users.GetCardinality()), Amount: item.amount})
	}
	return out
}

// Slices 带附加信息的分布
func (c *Counter) Slices(opts SliceOptions) []reportdomain.Slice {
	out := ToSlices(c.Groups(), opts)
	for i := range out {
		if item := c.items[out[i].Key]; item != nil && item.meta != nil {
			out[i].Meta = item.meta
		}
	}
	return out
}

// SliceOptions 分布的排序、截断与标签
type SliceOptions struct {
	// By 排序与占比的依据：count（默认）/ users / amount
	By string
	// Top 保留前 N 项，其余合并成「其他」；0 为不截断
	Top int
	// Empty 空键显示成什么（默认「未知」）
	Empty string
	// Label 键到展示名的映射，nil 为原样
	Label func(key string) string
	// KeepOrder 不排序，保持输入顺序（如频次分档）
	KeepOrder bool
}

// ToSlices 行 → 分布：空键归并、排序、截断、算占比
func ToSlices(groups []Group, opts SliceOptions) []reportdomain.Slice {
	empty := opts.Empty
	if empty == "" {
		empty = "unknown"
	}
	// 空键与同名键归并
	merged := make([]Group, 0, len(groups))
	index := map[string]int{}
	for _, g := range groups {
		key := strings.TrimSpace(g.Key)
		if key == "" {
			key = empty
		}
		if i, ok := index[key]; ok {
			merged[i].Count += g.Count
			merged[i].Users += g.Users
			merged[i].Amount += g.Amount
			merged[i].Value += g.Value
			continue
		}
		index[key] = len(merged)
		g.Key = key
		merged = append(merged, g)
	}
	metric := func(g Group) float64 {
		switch opts.By {
		case "users":
			return float64(g.Users)
		case "amount":
			return g.Amount
		case "value":
			return g.Value
		default:
			return float64(g.Count)
		}
	}
	if !opts.KeepOrder {
		sort.SliceStable(merged, func(i, j int) bool {
			mi, mj := metric(merged[i]), metric(merged[j])
			if mi != mj {
				return mi > mj
			}
			return merged[i].Key < merged[j].Key
		})
	}
	var total float64
	for _, g := range merged {
		total += metric(g)
	}
	if opts.Top > 0 && len(merged) > opts.Top {
		other := Group{Key: OtherKey}
		for _, g := range merged[opts.Top:] {
			other.Count += g.Count
			other.Users += g.Users
			other.Amount += g.Amount
			other.Value += g.Value
		}
		merged = append(merged[:opts.Top], other)
	}
	out := make([]reportdomain.Slice, 0, len(merged))
	for _, g := range merged {
		s := reportdomain.Slice{
			Key:    g.Key,
			Count:  g.Count,
			Users:  g.Users,
			Amount: round(g.Amount, 2),
			Value:  round(g.Value, 4),
			Share:  round(Ratio(metric(g), total), 4),
		}
		if g.Label != "" && g.Label != g.Key {
			s.Label = g.Label
		}
		if opts.Label != nil && g.Key != OtherKey {
			if label := opts.Label(g.Key); label != "" && label != g.Key {
				s.Label = label
			}
		}
		out = append(out, s)
	}
	return out
}

// Bands 把「每个用户的次数」分档：edges 为各档下界，如 [1,2,4,8,16] → 1、2-3、4-7、8-15、16+
func Bands(perUser map[uint64]int, edges []int) []reportdomain.Slice {
	hist := make(map[int]int64)
	for _, n := range perUser {
		hist[n]++
	}
	return BandHistogram(hist, edges)
}

// BandHistogram 同 Bands，输入是「次数 → 人数」的直方图
func BandHistogram(hist map[int]int64, edges []int) []reportdomain.Slice {
	counts := make([]int64, len(edges))
	for n, users := range hist {
		i := sort.SearchInts(edges, n+1) - 1
		if i < 0 {
			continue
		}
		counts[i] += users
	}
	groups := make([]Group, len(edges))
	for i, lo := range edges {
		groups[i] = Group{Key: bandLabel(edges, i, lo), Count: counts[i]}
	}
	return ToSlices(groups, SliceOptions{KeepOrder: true})
}

func bandLabel(edges []int, i, lo int) string {
	if i == len(edges)-1 {
		return strconv.Itoa(lo) + "+"
	}
	hi := edges[i+1] - 1
	if hi == lo {
		return strconv.Itoa(lo)
	}
	return strconv.Itoa(lo) + "-" + strconv.Itoa(hi)
}

// HeatmapAdd 热力图记一格（weekday 0 = 周一）
func HeatmapAdd(h *reportdomain.Heatmap, weekday, hour int, n int64) {
	if weekday < 0 || weekday > 6 || hour < 0 || hour > 23 {
		return
	}
	h.Cells[weekday][hour] += n
	h.Total += n
	if h.Cells[weekday][hour] > h.Max {
		h.Max = h.Cells[weekday][hour]
	}
}
