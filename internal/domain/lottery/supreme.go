package lottery

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	vipdomain "aegis/internal/domain/vip"
)

// 至尊加成：永久会员（或指定会员档位）的每日运气在自然运气之上再抬一截。
//
// 每日运气（fortune.go）人人一个系数 f = exp(σz − σ²/2)，管理员干预（fortune_control.go）改的是 z。
// 至尊加成接在干预之后，直接作用在系数上：
//
//	f' = max(f, min(max(f × 加成幅度, 保底运气), 上限))
//
//   - 加成幅度：系数乘多少（1–3）
//   - 保底运气：加成之后至少是多少（0 为不设；1 即「至少平均水平」）
//   - 上限：加成之后最多是多少（0 为按 MaxSupremeFactor 封顶）
//   - 外层的 max(f, …)：加成只会让运气更好，自然运气已经高过上限的那天按自然运气算，不会被上限拉低
//
// f' 关于 f 单调不减，所以这个人的系数范围就是把自然范围的两端各套一次（SupremeRange），积分账与无套利
// 照旧按「最走运的那一天」检查。生效点与 f 完全相同（FortunePrizes：兜底奖、谢谢参与以外的奖品权重 × f'），
// 公示概率、中奖率护栏、无套利比例都按乘过之后的个人奖盘算，概率照常归一。
//
// 谁能得到：scope = lifetime 时只看是不是永久会员（vip.Holdings.Lifetime，与会员判定同一份事实）；
// scope = tiers 时按会员档位分级（vip.Holdings.Tiers），每档一组参数，可以只认永久开通；
// 同时满足几档时取对他今天最好的那一档；没落在任何一档的永久会员，顶层参数有效果时按顶层参数算。
//
// 配置有两层：应用的默认（lottery_app_settings.supreme）与活动策略里的 fortune.supreme。
// 活动里不写时跟随应用；写了就以活动为准（enabled=false 即这个活动不加成）。只有开了每日运气的活动才有加成。

const (
	SupremeScopeLifetime = "lifetime"
	SupremeScopeTiers    = "tiers"

	// MaxSupremeBoost 加成幅度上限（与人群特权的中奖加成一致）
	MaxSupremeBoost = 3
	// MaxSupremeFactor 加成之后系数的硬上限（不设上限时按它封顶）
	MaxSupremeFactor = 5
	MaxSupremeTiers  = 20
	// MaxSupremeTitleRunes 文案名称上限
	MaxSupremeTitleRunes = 16
	// DefaultSupremeTitle 默认名称
	DefaultSupremeTitle = "至尊加成"
)

// Supreme 至尊加成的配置。
type Supreme struct {
	Enabled bool `json:"enabled"`
	// Scope lifetime：只对永久会员；tiers：按会员档位分级
	Scope string `json:"scope"`
	// Boost / Floor / Cap 永久会员的加成幅度、保底运气、上限；tiers 时是「没落在任何一档的永久会员」的参数（没有效果时不给）
	Boost float64       `json:"boost,omitempty"`
	Floor float64       `json:"floor,omitempty"`
	Cap   float64       `json:"cap,omitempty"`
	Tiers []SupremeTier `json:"tiers,omitempty"`
	// Title 展示给用户的名称（默认「至尊加成」）
	Title string `json:"title,omitempty"`
}

// SupremeTier 一个会员档位的加成。
type SupremeTier struct {
	TierID int64 `json:"tierId"`
	// LifetimeOnly 只认这个档位里的永久开通（限时会员不加成）
	LifetimeOnly bool    `json:"lifetimeOnly,omitempty"`
	Boost        float64 `json:"boost,omitempty"`
	Floor        float64 `json:"floor,omitempty"`
	Cap          float64 `json:"cap,omitempty"`
}

// SupremeGrade 一组生效的参数（这个人落在哪一档）。
type SupremeGrade struct {
	Boost float64 `json:"boost"`
	Floor float64 `json:"floor,omitempty"`
	Cap   float64 `json:"cap,omitempty"`
	// TierID / TierName 按档位分级时落在的档位；永久会员（scope lifetime 或顶层参数）为 0
	TierID   int64  `json:"tierId,omitempty"`
	TierName string `json:"tierName,omitempty"`
}

func (g SupremeGrade) hasEffect() bool { return g.Boost > 1 || g.Floor > 0 }

// ceiling 加成之后系数的上限。
func (g SupremeGrade) ceiling() float64 {
	if g.Cap > 0 {
		return g.Cap
	}
	return MaxSupremeFactor
}

// Apply 自然运气 f 加成之后的系数。
func (g SupremeGrade) Apply(natural float64) float64 {
	boost := max(g.Boost, 1)
	lifted := math.Min(math.Max(natural*boost, g.Floor), g.ceiling())
	return round6(math.Max(natural, lifted))
}

// NormalizeSupreme 清洗：空范围按永久会员、不加成的幅度归零、文案去空白、档位去重（后出现的覆盖先出现的）。
func NormalizeSupreme(s *Supreme) *Supreme {
	if s == nil {
		return nil
	}
	out := *s
	out.Scope = strings.TrimSpace(out.Scope)
	if out.Scope == "" {
		out.Scope = SupremeScopeLifetime
	}
	out.Title = strings.TrimSpace(out.Title)
	if out.Boost == 1 {
		out.Boost = 0
	}
	if out.Scope == SupremeScopeLifetime {
		out.Tiers = nil
	}
	tiers := make([]SupremeTier, 0, len(out.Tiers))
	for _, tier := range out.Tiers {
		if tier.Boost == 1 {
			tier.Boost = 0
		}
		if index := slices.IndexFunc(tiers, func(t SupremeTier) bool { return t.TierID == tier.TierID }); index >= 0 {
			tiers[index] = tier
			continue
		}
		tiers = append(tiers, tier)
	}
	if len(tiers) == 0 {
		tiers = nil
	}
	out.Tiers = tiers
	return &out
}

// ValidateSupreme 校验配置。关闭时只校验名称，其余参数可以留着下次再开。
func ValidateSupreme(s *Supreme) error {
	if s == nil {
		return nil
	}
	if utf8.RuneCountInString(s.Title) > MaxSupremeTitleRunes {
		return fmt.Errorf("至尊加成的名称不能超过 %d 字", MaxSupremeTitleRunes)
	}
	if s.Scope != SupremeScopeLifetime && s.Scope != SupremeScopeTiers {
		return fmt.Errorf("未知的至尊加成范围：%s", s.Scope)
	}
	if len(s.Tiers) > MaxSupremeTiers {
		return fmt.Errorf("至尊加成最多设置 %d 个档位", MaxSupremeTiers)
	}
	if err := validateGrade("永久会员", SupremeGrade{Boost: s.Boost, Floor: s.Floor, Cap: s.Cap}); err != nil {
		return err
	}
	for index, tier := range s.Tiers {
		label := fmt.Sprintf("第 %d 个档位", index+1)
		if tier.TierID <= 0 {
			return fmt.Errorf("%s没有选择会员档位", label)
		}
		grade := SupremeGrade{Boost: tier.Boost, Floor: tier.Floor, Cap: tier.Cap}
		if err := validateGrade(label, grade); err != nil {
			return err
		}
		if s.Enabled && !grade.hasEffect() {
			return fmt.Errorf("%s没有设置加成幅度或保底运气", label)
		}
	}
	if !s.Enabled {
		return nil
	}
	top := SupremeGrade{Boost: s.Boost, Floor: s.Floor, Cap: s.Cap}
	switch {
	case s.Scope == SupremeScopeLifetime && !top.hasEffect():
		return errors.New("至尊加成需要设置加成幅度或保底运气")
	case s.Scope == SupremeScopeTiers && len(s.Tiers) == 0 && !top.hasEffect():
		return errors.New("按会员档位分级时至少设置一个档位")
	}
	return nil
}

func validateGrade(label string, g SupremeGrade) error {
	switch {
	case math.IsNaN(g.Boost) || math.IsNaN(g.Floor) || math.IsNaN(g.Cap):
		return fmt.Errorf("%s的至尊加成参数无效", label)
	case g.Boost != 0 && (g.Boost < 1 || g.Boost > MaxSupremeBoost):
		return fmt.Errorf("%s的加成幅度需要在 1–%d 倍之间", label, MaxSupremeBoost)
	case g.Floor < 0 || g.Floor > MaxSupremeFactor:
		return fmt.Errorf("%s的保底运气需要在 0–%d 之间", label, MaxSupremeFactor)
	case g.Cap < 0 || g.Cap > MaxSupremeFactor:
		return fmt.Errorf("%s的运气上限需要在 0–%d 之间", label, MaxSupremeFactor)
	case g.Cap > 0 && g.Cap < 1:
		return fmt.Errorf("%s的运气上限不能低于 1", label)
	case g.Cap > 0 && g.Floor > g.Cap:
		return fmt.Errorf("%s的保底运气不能高于上限", label)
	}
	return nil
}

// TierIDs 配置里引用的会员档位（保存时校验它们属于这个应用）。
func (s *Supreme) TierIDs() []int64 {
	if s == nil {
		return nil
	}
	ids := make([]int64, 0, len(s.Tiers))
	for _, tier := range s.Tiers {
		ids = append(ids, tier.TierID)
	}
	return ids
}

// EffectiveSupreme 活动实际生效的至尊加成：活动策略里写了就用活动的，没写跟随应用；没开每日运气、关闭时为 nil。
// source 是 activity / app，给控制台看来源。
func EffectiveSupreme(fortune *Fortune, app *Supreme) (supreme *Supreme, source string) {
	if fortune == nil || fortune.Spread <= 0 {
		return nil, ""
	}
	switch {
	case fortune.Supreme != nil:
		supreme, source = fortune.Supreme, SupremeSourceActivity
	case app != nil:
		supreme, source = app, SupremeSourceApp
	default:
		return nil, ""
	}
	if !supreme.Enabled {
		return nil, source
	}
	return supreme, source
}

const (
	SupremeSourceActivity = "activity"
	SupremeSourceApp      = "app"
)

// SupremeGrades 这个人能得到的全部参数组（没有时为空）。
func SupremeGrades(s *Supreme, holdings vipdomain.Holdings) []SupremeGrade {
	if s == nil || !s.Enabled {
		return nil
	}
	top := SupremeGrade{Boost: s.Boost, Floor: s.Floor, Cap: s.Cap}
	if s.Scope != SupremeScopeTiers {
		if holdings.Lifetime && top.hasEffect() {
			return []SupremeGrade{top}
		}
		return nil
	}
	grades := []SupremeGrade{}
	for _, tier := range s.Tiers {
		held, ok := holdings.HoldsTier(tier.TierID, tier.LifetimeOnly)
		if !ok {
			continue
		}
		grade := SupremeGrade{Boost: tier.Boost, Floor: tier.Floor, Cap: tier.Cap, TierID: tier.TierID, TierName: held.Name}
		if grade.hasEffect() {
			grades = append(grades, grade)
		}
	}
	if len(grades) == 0 && holdings.Lifetime && top.hasEffect() {
		grades = append(grades, top)
	}
	return grades
}

// BestSupreme 几组参数里对自然运气 f 最好的一组（同样好时取先出现的）。
func BestSupreme(grades []SupremeGrade, natural float64) (*SupremeGrade, float64) {
	var best *SupremeGrade
	factor := natural
	for index := range grades {
		if got := grades[index].Apply(natural); best == nil || got > factor {
			best, factor = &grades[index], got
		}
	}
	return best, factor
}

// SupremeOutcome 加成的结果。
type SupremeOutcome struct {
	Grade SupremeGrade
	// Base 加成之前（自然运气，已计入管理员干预）；Factor 加成之后
	Base   float64
	Factor float64
}

// ResolveSupreme 这个人今天的至尊加成；拿不到加成时为 nil（系数照旧）。
func ResolveSupreme(s *Supreme, holdings vipdomain.Holdings, natural float64) *SupremeOutcome {
	grade, factor := BestSupreme(SupremeGrades(s, holdings), natural)
	if grade == nil {
		return nil
	}
	return &SupremeOutcome{Grade: *grade, Base: natural, Factor: factor}
}

// SupremeRange 拿到这组加成的人的系数范围（f' 关于 f 单调，两端各套一次）。
func SupremeRange(fortune *Fortune, grade SupremeGrade) (low, high float64) {
	low, high = FortuneRange(fortune)
	return grade.Apply(low), grade.Apply(high)
}

// SupremeHigh 永久会员最走运的那一天的系数（积分账按它检查）：所有可能落到永久会员身上的参数组取最高。
func SupremeHigh(fortune *Fortune, s *Supreme) float64 {
	_, high := FortuneRange(fortune)
	if s == nil || !s.Enabled {
		return high
	}
	grades := []SupremeGrade{{Boost: s.Boost, Floor: s.Floor, Cap: s.Cap}}
	if s.Scope == SupremeScopeTiers {
		for _, tier := range s.Tiers {
			grades = append(grades, SupremeGrade{Boost: tier.Boost, Floor: tier.Floor, Cap: tier.Cap})
		}
	}
	best := high
	for _, grade := range grades {
		if grade.hasEffect() {
			best = math.Max(best, grade.Apply(high))
		}
	}
	return best
}

// SupremeReading 用户端今日运气上的至尊加成说明。
type SupremeReading struct {
	Title string `json:"title"`
	// Tier 落在的会员档位名（按档位分级时）
	Tier string `json:"tier,omitempty"`
	// BaseFactor / BaseLevel / BaseTitle / BasePercentile 加成之前的运气
	BaseFactor     float64 `json:"baseFactor"`
	BaseLevel      string  `json:"baseLevel"`
	BaseTitle      string  `json:"baseTitle"`
	BasePercentile int     `json:"basePercentile"`
	// Boost / Floor / Cap 生效的参数
	Boost float64 `json:"boost"`
	Floor float64 `json:"floor,omitempty"`
	Cap   float64 `json:"cap,omitempty"`
	// Lift 加成之后是之前的几倍（1 为自然运气已高过上限，没有再加）
	Lift float64 `json:"lift"`
	// BaseWinRate 加成之前（只乘自然运气）抽中兜底奖以外奖品的概率（百分比）
	BaseWinRate float64 `json:"baseWinRate"`
	Message     string  `json:"message"`
}

// SupremeTitle 配置里的名称（默认「至尊加成」）。
func SupremeTitle(s *Supreme) string {
	if s == nil || s.Title == "" {
		return DefaultSupremeTitle
	}
	return s.Title
}

// ApplySupremeReading 把加成写进今日运气：系数、档位、超过多少人、范围都换成加成之后的，原来的放进 supreme。
// 系数与抽奖事务乘在奖盘上的是同一个。
func ApplySupremeReading(reading *FortuneReading, fortune *Fortune, s *Supreme, outcome *SupremeOutcome) *FortuneReading {
	if reading == nil || outcome == nil || fortune == nil || fortune.Spread <= 0 {
		return reading
	}
	out := *reading
	z := zOfFactorRaw(fortune.Spread, outcome.Factor)
	out.Factor = outcome.Factor
	out.Level = LevelOfZ(z)
	out.Title = fortuneTitles[out.Level]
	out.Percentile = PercentileOfZ(z)
	out.Low, out.High = SupremeRange(fortune, outcome.Grade)
	out.Message = fmt.Sprintf("今日手气超过 %d%% 的用户，奖品权重 ×%s", out.Percentile, trimFloat(out.Factor))

	title := SupremeTitle(s)
	supreme := &SupremeReading{
		Title: title, Tier: outcome.Grade.TierName,
		BaseFactor: reading.Factor, BaseLevel: reading.Level, BaseTitle: reading.Title, BasePercentile: reading.Percentile,
		Boost: max(outcome.Grade.Boost, 1), Floor: outcome.Grade.Floor, Cap: outcome.Grade.Cap,
		Lift: 1, BaseWinRate: reading.WinRate,
	}
	if reading.Factor > 0 {
		supreme.Lift = round6(outcome.Factor / reading.Factor)
	}
	who := "永久会员"
	if supreme.Tier != "" {
		who = supreme.Tier
	}
	if supreme.Lift > 1+1e-9 {
		supreme.Message = fmt.Sprintf("%s%s：今日运气由 ×%s 提升至 ×%s", who, title,
			trimFloat(reading.Factor), trimFloat(outcome.Factor))
	} else {
		supreme.Message = fmt.Sprintf("%s%s：今日运气已高于加成上限，按原运气生效", who, title)
	}
	out.Supreme = supreme
	return &out
}

// zOfFactorRaw 系数反算 z，不截断（加成之后可以超出自然运气的 ±2，档位与百分位照样按它算）。
func zOfFactorRaw(spread, factor float64) float64 {
	if spread <= 0 || factor <= 0 {
		return 0
	}
	return (math.Log(factor) + spread*spread/2) / spread
}

// AppSettings 抽奖的应用级设置。
type AppSettings struct {
	AppID int64 `json:"appid"`
	// Supreme 每日运气的至尊加成：活动策略里没写 fortune.supreme 时跟随这里
	Supreme   Supreme   `json:"supreme"`
	UpdatedBy string    `json:"updatedBy,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}
