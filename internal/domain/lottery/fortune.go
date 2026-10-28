package lottery

import (
	"fmt"
	"hash/fnv"
	"math"
	"slices"
	"time"

	"gonum.org/v1/gonum/stat/distuv"
)

// 每日运气：每个人每天一个运气系数，乘在奖盘里「真正的奖品」的权重上（兜底奖、谢谢参与不变）。
//
// 运气平衡（Luck）把每个人往应得的中奖次数拉，人人趋同；每日运气反过来，让不同的人、同一个人的不同日子
// 手气不一样：有人今天连出会员和大额积分，有人今天多是经验。
//
//	z      = Φ⁻¹(hash(活动, 用户, 日期))，截在 ±2
//	系数   = exp(σ·z − σ²/2)
//
// 系数服从对数正态，所有人平均下来约为 1，总体发放与公示接近；σ = 0.3 时最走运与最倒霉的人（±2σ）
// 真实奖品的权重相差约 3.3 倍。哈希决定，同一天里刷新、重进都不变，过了零点换一个。
//
// 运气系数在个人奖盘上生效（与人群特权的中奖加成同一层），公示概率、无套利比例都按乘过之后的奖盘算：
// 走运的永久会员如果因此能用积分买次数稳赚，照样按比例缩减折算，保证不变。

// Fortune 每日运气。
type Fortune struct {
	// Spread 运气差异 σ（对数标准差，0–1）
	Spread float64 `json:"spread"`
}

const (
	FactorFortune    = "fortune"
	MaxFortuneSpread = 1
	fortuneClampZ    = 2
	// fortuneScale 乘系数前先把权重放大这么多倍，整数权重才乘得出小数系数
	fortuneScale = 100
)

// FortuneOf 这个人在 day（当天零点）这一天的运气系数；没开每日运气时为 1。
func FortuneOf(fortune *Fortune, activityID, userID int64, day time.Time) float64 {
	if fortune == nil || fortune.Spread <= 0 || userID <= 0 {
		return 1
	}
	hash := fnv.New64a()
	_, _ = fmt.Fprintf(hash, "fortune:%d:%d:%s", activityID, userID, day.Format(time.DateOnly))
	return fortuneFactor(fortune.Spread, hash.Sum64())
}

// FortuneRange 运气系数的范围（最倒霉、最走运）。
func FortuneRange(fortune *Fortune) (low, high float64) {
	if fortune == nil || fortune.Spread <= 0 {
		return 1, 1
	}
	return fortuneAt(fortune.Spread, -fortuneClampZ), fortuneAt(fortune.Spread, fortuneClampZ)
}

func fortuneFactor(spread float64, hash uint64) float64 {
	// FNV 对只差几位数字的输入雪崩不够，再过一遍 splitmix64 的终混
	hash ^= hash >> 30
	hash *= 0xbf58476d1ce4e5b9
	hash ^= hash >> 27
	hash *= 0x94d049bb133111eb
	hash ^= hash >> 31
	u := (float64(hash>>11) + 0.5) / (1 << 53)
	return fortuneAt(spread, distuv.UnitNormal.Quantile(u))
}

func fortuneAt(spread, z float64) float64 {
	z = math.Max(-fortuneClampZ, math.Min(fortuneClampZ, z))
	return round6(math.Exp(spread*z - spread*spread/2))
}

// FortunePrizes 把运气系数乘到奖盘上：兜底奖与谢谢参与不变，其余奖品权重 × factor，公示概率随之重算。
func FortunePrizes(prizes []Prize, factor float64) []Prize {
	if factor <= 0 || factor == 1 {
		return prizes
	}
	out := slices.Clone(prizes)
	for index := range out {
		weight := out[index].Weight * fortuneScale
		if out[index].Winning() && !out[index].Fallback {
			weight = int(math.Round(float64(weight) * factor))
		}
		out[index].Weight = weight
	}
	return out
}
