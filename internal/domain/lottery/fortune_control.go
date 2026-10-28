package lottery

import (
	"errors"
	"fmt"
	"hash/fnv"
	"math"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"gonum.org/v1/gonum/stat/distuv"
)

// 运气干预：管理员对每日运气的控制，按人或全站、带生效时间窗，可撤销，全部留痕。
//
// 三种干预，作用在 z 上（系数 = exp(σz − σ²/2)，z 截在 ±2，所以干预之后系数也不会超出 FortuneRange，
// 积分账、无套利的最坏情况照旧成立）：
//
//	reroll  重抽：在哈希里加一段随机盐，换一个新的 z（仍然服从正态，分布不变，只是这个人 / 所有人换了手气）
//	pin     定档：直接把 z 定在某个值（运气大爆发 1.75、鸿运当头 1、运势平稳 0、手气平淡 −1，或按系数反算）
//	shift   偏移：在 z 上加减（全站 +0.5 就是整体走运一点，分布形状不变）
//
// 合成顺序：先按生效中的 reroll 加盐算出 z（全站的盐在前、个人的在后，各按创建先后），
// 再看 pin（个人的优先于全站的，同一层取最新的一条），没有 pin 时加上全部生效中的 shift，最后截在 ±2。
// 活动页、抽奖事务、模拟都走 ResolveFortune，看到的与抽到的永远是同一个系数。

const (
	ControlReroll = "reroll"
	ControlPin    = "pin"
	ControlShift  = "shift"

	// MaxControlShift 偏移的上限（z 的单位）：±2 已经能把最倒霉的人推到最走运
	MaxControlShift = 4
	// MaxControlUsers 一次最多对多少人下干预
	MaxControlUsers = 500
	// MaxControlDays 一条干预最长生效多久
	MaxControlDays = 90
	// MaxControlNoteRunes 备注上限
	MaxControlNoteRunes = 200
)

// 定档时各档位落在的 z（档位分界是 −0.5 / 0.5 / 1.5，取各档中间偏稳的位置）。
var levelZ = map[string]float64{
	FortuneBurst:  1.75,
	FortuneLucky:  1,
	FortuneSteady: 0,
	FortuneQuiet:  -1,
}

// PinZ 定档到某一档时用的 z（未知档位为 0）。
func PinZ(level string) float64 { return levelZ[level] }

// ControlKinds 干预的种类（控制台下拉用）。
func ControlKinds() []string { return []string{ControlReroll, ControlPin, ControlShift} }

// FortuneLevels 运气档位，从高到低。
func FortuneLevels() []string {
	return []string{FortuneBurst, FortuneLucky, FortuneSteady, FortuneQuiet}
}

// FortuneControl 一条运气干预。UserID 为空是全站。
type FortuneControl struct {
	ID         int64  `json:"id"`
	AppID      int64  `json:"appid"`
	ActivityID int64  `json:"activityId"`
	UserID     *int64 `json:"userId,omitempty"`
	Account    string `json:"account,omitempty"`
	Nickname   string `json:"nickname,omitempty"`
	Kind       string `json:"kind"`
	// Z pin：定在的 z；shift：加减的量；reroll 不用
	Z float64 `json:"z"`
	// Level pin 按档位定的时候记下档位（按系数定的为空）
	Level     string     `json:"level,omitempty"`
	Salt      string     `json:"-"`
	StartsAt  time.Time  `json:"startsAt"`
	EndsAt    time.Time  `json:"endsAt"`
	Note      string     `json:"note,omitempty"`
	Operator  string     `json:"operator"`
	Revoked   *time.Time `json:"revokedAt,omitempty"`
	RevokedBy string     `json:"revokedBy,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	// Status 读出时按当前时刻算：active / scheduled / expired / revoked
	Status string `json:"status"`
}

const (
	ControlActive    = "active"
	ControlScheduled = "scheduled"
	ControlExpired   = "expired"
	ControlRevoked   = "revoked"
)

// ActiveAt 这条干预在 now 是否生效。
func (c FortuneControl) ActiveAt(now time.Time) bool {
	return c.Revoked == nil && !now.Before(c.StartsAt) && now.Before(c.EndsAt)
}

// StatusAt 这条干预在 now 的状态。
func (c FortuneControl) StatusAt(now time.Time) string {
	switch {
	case c.Revoked != nil:
		return ControlRevoked
	case now.Before(c.StartsAt):
		return ControlScheduled
	case !now.Before(c.EndsAt):
		return ControlExpired
	default:
		return ControlActive
	}
}

// Global 是不是全站的干预。
func (c FortuneControl) Global() bool { return c.UserID == nil }

// ControlInput 管理员下的一次干预。UserIDs 为空且 AllUsers 为真时对全站。
type ControlInput struct {
	AppID      int64
	ActivityID int64
	UserIDs    []int64
	AllUsers   bool
	Kind       string
	// Level pin 按档位；为空时用 Factor（按系数反算 z）
	Level  string
	Factor float64
	// Shift shift 的 z 偏移
	Shift    float64
	StartsAt time.Time
	EndsAt   time.Time
	Note     string
	Operator string
}

// NormalizeControl 校验并补全一次干预：时间窗默认从现在到明天零点（今天这一天），
// pin 按档位或系数算出 z。[spread] 是活动当前的运气差异（按系数反算 z 要用）。
func NormalizeControl(input ControlInput, spread float64, now, dayStart time.Time) (ControlInput, float64, error) {
	input.Note = strings.TrimSpace(input.Note)
	if utf8.RuneCountInString(input.Note) > MaxControlNoteRunes {
		return input, 0, fmt.Errorf("备注不能超过 %d 字", MaxControlNoteRunes)
	}
	if !slices.Contains(ControlKinds(), input.Kind) {
		return input, 0, fmt.Errorf("未知的干预方式：%s", input.Kind)
	}
	input.UserIDs = dedupeIDs(input.UserIDs)
	if input.AllUsers && len(input.UserIDs) > 0 {
		return input, 0, errors.New("全站与指定用户只能选一种")
	}
	if !input.AllUsers && len(input.UserIDs) == 0 {
		return input, 0, errors.New("请选择用户，或选择全站")
	}
	if len(input.UserIDs) > MaxControlUsers {
		return input, 0, fmt.Errorf("一次最多选择 %d 个用户", MaxControlUsers)
	}
	if input.StartsAt.IsZero() {
		input.StartsAt = now
	}
	if input.EndsAt.IsZero() {
		input.EndsAt = dayStart.AddDate(0, 0, 1)
	}
	if !input.EndsAt.After(input.StartsAt) {
		return input, 0, errors.New("结束时间要晚于开始时间")
	}
	if !input.EndsAt.After(now) {
		return input, 0, errors.New("结束时间已经过去")
	}
	if input.EndsAt.Sub(input.StartsAt) > MaxControlDays*24*time.Hour {
		return input, 0, fmt.Errorf("一条干预最长生效 %d 天", MaxControlDays)
	}
	if spread <= 0 {
		return input, 0, errors.New("活动没有开启每日运气，请先设置运气差异")
	}
	switch input.Kind {
	case ControlPin:
		if input.Level != "" {
			z, ok := levelZ[input.Level]
			if !ok {
				return input, 0, fmt.Errorf("未知的运气档位：%s", input.Level)
			}
			return input, z, nil
		}
		if input.Factor <= 0 || math.IsNaN(input.Factor) || math.IsInf(input.Factor, 0) {
			return input, 0, errors.New("请选择档位，或填写大于 0 的运气系数")
		}
		low, high := FortuneRange(&Fortune{Spread: spread})
		if input.Factor < low-1e-6 || input.Factor > high+1e-6 {
			return input, 0, fmt.Errorf("运气系数需要在 %s–%s 之间", trimFloat(low), trimFloat(high))
		}
		return input, ZOfFactor(spread, input.Factor), nil
	case ControlShift:
		if input.Shift == 0 || math.Abs(input.Shift) > MaxControlShift || math.IsNaN(input.Shift) {
			return input, 0, fmt.Errorf("偏移需要在 ±%d 之间且不为 0", MaxControlShift)
		}
		return input, input.Shift, nil
	default:
		return input, 0, nil
	}
}

// ZOfFactor 系数反算 z：factor = exp(σz − σ²/2)。
func ZOfFactor(spread, factor float64) float64 {
	z := (math.Log(factor) + spread*spread/2) / spread
	return math.Max(-fortuneClampZ, math.Min(fortuneClampZ, z))
}

// FortuneOutcome 一个人在某一刻的运气：没有干预时的原值与干预之后的值，以及生效的干预。
type FortuneOutcome struct {
	BaseZ   float64
	Z       float64
	Factor  float64
	Applied []FortuneControl
}

// Controlled 有没有干预生效（且改变了结果）。
func (o FortuneOutcome) Controlled() bool { return len(o.Applied) > 0 }

// ResolveFortune 这个人此刻的运气（没开每日运气时为 nil）。[controls] 传这个活动里与他相关的干预
// （全站的与他本人的），这里按 now 过滤生效的，与是否已撤销。
func ResolveFortune(fortune *Fortune, activityID, userID int64, day, now time.Time, controls []FortuneControl) *FortuneOutcome {
	if fortune == nil || fortune.Spread <= 0 || userID <= 0 {
		return nil
	}
	active := make([]FortuneControl, 0, len(controls))
	for _, control := range controls {
		if control.ActiveAt(now) && (control.Global() || *control.UserID == userID) {
			active = append(active, control)
		}
	}
	// 全站在前、个人在后，各按创建先后（id 递增）
	slices.SortStableFunc(active, func(a, b FortuneControl) int {
		if a.Global() != b.Global() {
			if a.Global() {
				return -1
			}
			return 1
		}
		return int(a.ID - b.ID)
	})

	baseZ := fortuneZ(fortuneHash(activityID, userID, day))
	out := &FortuneOutcome{BaseZ: baseZ}
	hash := fnv.New64a()
	_, _ = fmt.Fprintf(hash, "fortune:%d:%d:%s", activityID, userID, day.Format(time.DateOnly))
	salted := false
	for _, control := range active {
		if control.Kind == ControlReroll {
			_, _ = fmt.Fprintf(hash, ":%s", control.Salt)
			salted = true
			out.Applied = append(out.Applied, control)
		}
	}
	z := baseZ
	if salted {
		z = fortuneZ(hash.Sum64())
	}
	var userPin, globalPin *FortuneControl
	for index := range active {
		control := active[index]
		if control.Kind != ControlPin {
			continue
		}
		if control.Global() {
			globalPin = &active[index]
		} else {
			userPin = &active[index]
		}
	}
	switch {
	case userPin != nil:
		z = userPin.Z
		out.Applied = append(out.Applied, *userPin)
	case globalPin != nil:
		z = globalPin.Z
		out.Applied = append(out.Applied, *globalPin)
	default:
		for _, control := range active {
			if control.Kind == ControlShift {
				z += control.Z
				out.Applied = append(out.Applied, control)
			}
		}
	}
	out.Z = math.Max(-fortuneClampZ, math.Min(fortuneClampZ, z))
	out.Factor = fortuneAt(fortune.Spread, out.Z)
	return out
}

// FortuneFactor 系数（没开每日运气时为 1）。
func FortuneFactor(fortune *Fortune, activityID, userID int64, day, now time.Time, controls []FortuneControl) float64 {
	if outcome := ResolveFortune(fortune, activityID, userID, day, now, controls); outcome != nil {
		return outcome.Factor
	}
	return 1
}

// ReadFortuneControlled 用户端的今日运气，计入干预。干预对用户不可见：只有系数、档位、超过多少人。
func ReadFortuneControlled(fortune *Fortune, activityID, userID int64, day, now time.Time, controls []FortuneControl) *FortuneReading {
	outcome := ResolveFortune(fortune, activityID, userID, day, now, controls)
	if outcome == nil {
		return nil
	}
	return readingOf(fortune, outcome.Z, day)
}

// LevelOfZ z 落在哪一档。
func LevelOfZ(z float64) string {
	switch {
	case z >= fortuneBurstZ:
		return FortuneBurst
	case z >= fortuneLuckyZ:
		return FortuneLucky
	case z > fortuneSteadyZ:
		return FortuneSteady
	default:
		return FortuneQuiet
	}
}

// PercentileOfZ 手气不如他的人占多少（1–99）。
func PercentileOfZ(z float64) int {
	return max(1, min(99, int(math.Round(distuv.UnitNormal.CDF(z)*100))))
}

func fortuneZ(hash uint64) float64 {
	z := distuv.UnitNormal.Quantile(fortuneUniform(hash))
	return math.Max(-fortuneClampZ, math.Min(fortuneClampZ, z))
}

func dedupeIDs(ids []int64) []int64 {
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id > 0 && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// LevelCounts 一组 z 落在各档的人数（控制台的今日分布）。
func LevelCounts(zs []float64) map[string]int {
	counts := map[string]int{FortuneBurst: 0, FortuneLucky: 0, FortuneSteady: 0, FortuneQuiet: 0}
	for _, z := range zs {
		counts[LevelOfZ(z)]++
	}
	return counts
}

// TheoreticalLevelShares 没有干预时各档的理论占比（正态分布，按档位分界算）。
func TheoreticalLevelShares() map[string]float64 {
	cdf := distuv.UnitNormal.CDF
	return map[string]float64{
		FortuneBurst:  1 - cdf(fortuneBurstZ),
		FortuneLucky:  cdf(fortuneBurstZ) - cdf(fortuneLuckyZ),
		FortuneSteady: cdf(fortuneLuckyZ) - cdf(fortuneSteadyZ),
		FortuneQuiet:  cdf(fortuneSteadyZ),
	}
}
