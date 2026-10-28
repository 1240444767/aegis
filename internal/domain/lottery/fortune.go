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
	return fortuneFactor(fortune.Spread, fortuneHash(activityID, userID, day))
}

func fortuneHash(activityID, userID int64, day time.Time) uint64 {
	hash := fnv.New64a()
	_, _ = fmt.Fprintf(hash, "fortune:%d:%d:%s", activityID, userID, day.Format(time.DateOnly))
	return hash.Sum64()
}

// 今日运气的档位，按 z 分：运气大爆发约 7% 的人，鸿运当头约 24%，运势平稳约 38%，手气平淡约 31%。
const (
	FortuneBurst  = "burst"
	FortuneLucky  = "lucky"
	FortuneSteady = "steady"
	FortuneQuiet  = "quiet"

	fortuneBurstZ  = 1.5
	fortuneLuckyZ  = 0.5
	fortuneSteadyZ = -0.5
)

// FortuneReading 用户端的今日运气：系数、在所有人里的位置与档位。系数与 FortuneOf 是同一个，
// 也就是抽奖事务里真正乘在奖盘上的那个，不是另算的一个「好看的数」。
type FortuneReading struct {
	Factor float64 `json:"factor"`
	// Percentile 今天手气不如他的人占多少（1–99）
	Percentile int `json:"percentile"`
	// Level burst / lucky / steady / quiet
	Level   string `json:"level"`
	Title   string `json:"title"`
	Message string `json:"message"`
	// Low / High 运气系数的范围（最倒霉、最走运）
	Low  float64 `json:"low"`
	High float64 `json:"high"`
	// WinRate / BaseWinRate 抽中兜底奖以外的奖品的概率（百分比），乘今日运气之后与之前
	WinRate     float64 `json:"winRate"`
	BaseWinRate float64 `json:"baseWinRate"`
	// ResetsAt 下一次换运气的时刻（明天零点）
	ResetsAt time.Time `json:"resetsAt"`
}

// ReadFortune 这个人今天的运气解读（不计管理员干预）；没开每日运气时为 nil。胜率由调用方按个人奖盘填。
func ReadFortune(fortune *Fortune, activityID, userID int64, day time.Time) *FortuneReading {
	if fortune == nil || fortune.Spread <= 0 || userID <= 0 {
		return nil
	}
	return readingOf(fortune, fortuneZ(fortuneHash(activityID, userID, day)), day)
}

// readingOf 按 z 拼出运气解读：系数、超过多少人、档位与文案。
func readingOf(fortune *Fortune, z float64, day time.Time) *FortuneReading {
	low, high := FortuneRange(fortune)
	reading := &FortuneReading{
		Factor:     fortuneAt(fortune.Spread, z),
		Percentile: PercentileOfZ(z),
		Level:      LevelOfZ(z),
		Low:        low,
		High:       high,
		ResetsAt:   day.AddDate(0, 0, 1).UTC(),
	}
	reading.Title = fortuneTitles[reading.Level]
	reading.Message = fmt.Sprintf("今日手气超过 %d%% 的用户，奖品权重 ×%s", reading.Percentile, trimFloat(reading.Factor))
	return reading
}

var fortuneTitles = map[string]string{
	FortuneBurst:  "运气大爆发",
	FortuneLucky:  "鸿运当头",
	FortuneSteady: "运势平稳",
	FortuneQuiet:  "手气平淡",
}

// RealPrizeShare 抽中兜底奖与谢谢参与以外的奖品的概率（百分比）。
func RealPrizeShare(prizes []Prize) float64 {
	probabilities := Probabilities(prizes)
	total := 0.0
	for index, prize := range prizes {
		if prize.Winning() && !prize.Fallback {
			total += probabilities[index]
		}
	}
	return PublicProbability(total)
}

// FortuneRange 运气系数的范围（最倒霉、最走运）。
func FortuneRange(fortune *Fortune) (low, high float64) {
	if fortune == nil || fortune.Spread <= 0 {
		return 1, 1
	}
	return fortuneAt(fortune.Spread, -fortuneClampZ), fortuneAt(fortune.Spread, fortuneClampZ)
}

func fortuneFactor(spread float64, hash uint64) float64 {
	return fortuneAt(spread, distuv.UnitNormal.Quantile(fortuneUniform(hash)))
}

// fortuneUniform 哈希映到 (0, 1)。FNV 对只差几位数字的输入雪崩不够，再过一遍 splitmix64 的终混。
func fortuneUniform(hash uint64) float64 {
	hash ^= hash >> 30
	hash *= 0xbf58476d1ce4e5b9
	hash ^= hash >> 27
	hash *= 0x94d049bb133111eb
	hash ^= hash >> 31
	return (float64(hash>>11) + 0.5) / (1 << 53)
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
