package lottery

import (
	"fmt"
	"math"

	"gonum.org/v1/gonum/stat/distuv"
)

// 概率控制层：在各维度系数（strategy.go）之后、抽样之前，把「这一次」的分布整体校正到可控的范围。
//
// 各维度系数是连乘的：行为画像 ×1.5、规则 ×2、匀速追赶 ×1.5 叠在一起，中奖率与每次发出去的价值都会漂得
// 没有边。控制层只在「中奖 / 未中」两组之间或按奖品价值做最小的改动，按顺序：
//
//  1. 运气平衡   这个人在本活动里「实际中奖次数 − 期望中奖次数」的 z 分数，越倒霉越往上校、越走运越往下校，
//     校正量加在中奖概率的对数几率上（logit），极端值截断在 ±3σ
//  2. 波动模式   independent 为独立抽取；prd 为伪随机分布：距上次中奖第 n 次的中奖率为 min(1, n·C)，
//     C 由当前中奖率反解，长期中奖率不变，连败与连中都大幅减少（替代软保底的作用）
//  3. 全站反馈   全站今日中奖率对目标值的比例反馈（样本越多越信任）；积分预算按当天时间进度放量，
//     超前收紧、落后放宽，替代「过半后线性收紧」
//  4. 护栏投影   中奖率区间、每次期望价值上限、奖品类型占比上限：在满足约束的分布里取与当前分布
//     KL 散度最小的那个（精确 I-投影，凸对偶上的投影牛顿法，见 projection.go）
//
// 价值统一折成「积分当量」：积分按数量；经验 × experienceRate；会员天数 × 每天折算积分；
// 抽奖次数 × drawValue；永久功能按永久折算积分；奖品单独设了 value 时以它为准（人工发放、会员套餐建议设置）。

// 波动模式。
const (
	VarianceIndependent = "independent"
	VariancePRD         = "prd"
)

// 系数标识（续）。
const (
	FactorLuck    = "luck"
	FactorPRD     = "prd"
	FactorControl = "control"
	FactorGuard   = "guard"
)

// 控制层的取值上界。
const (
	MaxLuckStrength     = 2
	MaxControlGain      = 20
	luckClampZ          = 3
	luckMinVariance     = 1
	controlConfidenceAt = 50
	guardIterations     = 60
	DefaultDrawValue    = 10
	DefaultExpRate      = 0.1
)

// LuckBalance 运气平衡：Strength 为每 1σ 在对数几率上校正多少（0.3 约等于倒霉 2σ 的人中奖率 ×1.8）。
type LuckBalance struct {
	Strength float64 `json:"strength"`
}

// WinRateControl 全站中奖率反馈：今日全站中奖率偏离 Target 时，按 Gain 在对数几率上拉回。
//
// 今天的中奖率用 Beta–二项共轭后验估计：先验 Beta(κ·Target, κ·(1−Target))，看到 n 次里中了 k 次后
// 后验均值为 (k + κ·Target)/(n + κ)。样本少时自然收缩到目标值、不乱调，样本多时信任实际值（贝叶斯收缩）。
// 校正量 = Gain ×（logit(Target) − logit(后验均值)），Gain = 1 表示把偏差在对数几率上完全拉回。
type WinRateControl struct {
	Target float64 `json:"target"`
	Gain   float64 `json:"gain"`
	// Prior 先验强度 κ（相当于多少次「虚拟抽奖」），0 为 50
	Prior float64 `json:"prior,omitempty"`
}

// Guard 护栏：这一次的中奖率限制在 [MinWinRate, MaxWinRate]，期望价值（积分当量）不超过 MaxValue。0 为不限。
type Guard struct {
	MinWinRate float64 `json:"minWinRate"`
	MaxWinRate float64 `json:"maxWinRate"`
	MaxValue   float64 `json:"maxValue"`
	// ExperienceRate 1 点经验折多少积分；DrawValue 1 次抽奖折多少积分（0 时取活动的积分抽奖价，再取 10）
	ExperienceRate float64 `json:"experienceRate"`
	DrawValue      float64 `json:"drawValue"`
	// KindCaps 按奖品类型的单次占比上限（0–1），如 {"vip_days": 0.01}
	KindCaps map[string]float64 `json:"kindCaps,omitempty"`
}

func normalizeControl(s *Strategy) {
	if s.Luck != nil && s.Luck.Strength <= 0 {
		s.Luck = nil
	}
	if s.Variance != VariancePRD {
		s.Variance = ""
	}
	if s.WinRate != nil && (s.WinRate.Target <= 0 || s.WinRate.Gain <= 0) {
		s.WinRate = nil
	}
	if g := s.Guard; g != nil {
		if g.MinWinRate <= 0 && (g.MaxWinRate <= 0 || g.MaxWinRate >= 1) && g.MaxValue <= 0 && len(g.KindCaps) == 0 {
			s.Guard = nil
		} else if g.MaxWinRate <= 0 {
			g.MaxWinRate = 1
		}
	}
}

func validateControl(s Strategy) error {
	if l := s.Luck; l != nil && l.Strength > MaxLuckStrength {
		return fmt.Errorf("运气平衡强度需要在 0–%d 之间", MaxLuckStrength)
	}
	if w := s.WinRate; w != nil {
		if w.Target >= 1 {
			return fmt.Errorf("全站目标中奖率需要在 0–100%% 之间")
		}
		if w.Gain > MaxControlGain {
			return fmt.Errorf("全站反馈增益需要在 0–%d 之间", MaxControlGain)
		}
	}
	if g := s.Guard; g != nil {
		switch {
		case g.MinWinRate < 0 || g.MinWinRate >= 1:
			return fmt.Errorf("中奖率下限需要在 0–100%% 之间")
		case g.MaxWinRate <= 0 || g.MaxWinRate > 1:
			return fmt.Errorf("中奖率上限需要在 0–100%% 之间")
		case g.MinWinRate > g.MaxWinRate:
			return fmt.Errorf("中奖率下限不能高于上限")
		case g.MaxValue < 0 || g.ExperienceRate < 0 || g.DrawValue < 0:
			return fmt.Errorf("期望价值与折算比例不能为负")
		}
		for kind, limit := range g.KindCaps {
			if _, ok := FindSpec(kind); !ok {
				return fmt.Errorf("护栏里的奖品类型 %s 不存在", kind)
			}
			if limit < 0 || limit > 1 {
				return fmt.Errorf("奖品类型占比上限需要在 0–100%% 之间")
			}
		}
	}
	return nil
}

// ── 价值 ──

// PrizeValue 奖品的积分当量。
func PrizeValue(activity Activity, guard *Guard, prize Prize) float64 {
	if prize.Value > 0 {
		return float64(prize.Value)
	}
	amount := float64(prize.Reward.Amount)
	expRate, drawValue := DefaultExpRate, float64(DefaultDrawValue)
	if activity.PointsCost > 0 {
		drawValue = float64(activity.PointsCost)
	}
	if guard != nil {
		if guard.ExperienceRate > 0 {
			expRate = guard.ExperienceRate
		}
		if guard.DrawValue > 0 {
			drawValue = guard.DrawValue
		}
	}
	perDay := float64(activity.LifetimePointsPerDay)
	if perDay <= 0 {
		perDay = DefaultLifetimePointsPerDay
	}
	switch prize.Kind {
	case KindPoints:
		return amount
	case KindExperience:
		return amount * expRate
	case KindLotteryDraws:
		return amount * drawValue
	case KindVipDays, KindAdFree:
		return amount * perDay
	case KindFeatureTime:
		return math.Ceil(amount/60) * perDay
	case KindVipLifetime:
		lifetime := float64(activity.LifetimePointsLifetime)
		if lifetime <= 0 {
			lifetime = DefaultLifetimePointsLifetime
		}
		return lifetime
	case KindFeature:
		if prize.Reward.Amount == 0 {
			lifetime := float64(activity.LifetimePointsLifetime)
			if lifetime <= 0 {
				lifetime = DefaultLifetimePointsLifetime
			}
			return lifetime
		}
		return amount * perDay
	case KindVipPlan:
		// 套餐时长在套餐目录里，这里拿不到：按 30 天估，需要准确时请给奖品设 value
		return 30 * perDay
	}
	return 0
}

// ── 两组之间的整体缩放 ──

func groupWeights(odds []PrizeOdds, prizes []Prize) (winning, losing float64) {
	for index, item := range odds {
		if prizes[index].Winning() {
			winning += item.Weight
		} else {
			losing += item.Weight
		}
	}
	return winning, losing
}

// setWinShare 把中奖组的总占比调到 target：中奖组内部比例不变，两组各乘一个系数。
// 某一组为空（全被挡掉）时无法调整，返回 false。factor 记在被缩放的那一组上。
func setWinShare(odds []PrizeOdds, prizes []Prize, target float64, key, note string) bool {
	winning, losing := groupWeights(odds, prizes)
	if winning <= 0 || losing <= 0 {
		return false
	}
	target = math.Min(1, math.Max(0, target))
	current := winning / (winning + losing)
	if math.Abs(current-target) < 1e-9 {
		return true
	}
	// 保持总权重不变：中奖组 × a，未中组 × b
	total := winning + losing
	a := target * total / winning
	b := (1 - target) * total / losing
	for index := range odds {
		if odds[index].Weight == 0 {
			continue
		}
		scale := b
		if prizes[index].Winning() {
			scale = a
		}
		odds[index].Weight *= scale
		odds[index].Factors = append(odds[index].Factors, Factor{Key: key, Value: round6(scale), Note: note})
	}
	return true
}

func winShare(odds []PrizeOdds, prizes []Prize) float64 {
	winning, losing := groupWeights(odds, prizes)
	if winning+losing <= 0 {
		return 0
	}
	return winning / (winning + losing)
}

func logit(p float64) float64 {
	p = math.Min(1-1e-9, math.Max(1e-9, p))
	return math.Log(p / (1 - p))
}

func sigmoid(x float64) float64 { return 1 / (1 + math.Exp(-x)) }

// ── 1. 运气平衡 ──

// LuckZ 这个人在本活动里的运气 z 分数。
//
// 每次抽奖是成功率各不相同的伯努利试验，中奖次数服从泊松二项分布（Poisson binomial）。
// 均值 μ = Σp、方差 σ² = Σp(1−p)、三阶中心矩 Σp(1−p)(1−2p) 都可以逐次累加。少量样本、p 很小时它明显右偏，
// 直接用 (k − μ)/σ 会把「运气平平」误判成「倒霉」。这里用带偏度修正的精细正态近似（RNA，Volkova 1996）：
//
//	P(X < k) + ½P(X = k) ≈ Φ(x) + γ(1 − x²)φ(x)/6,   x = (k − μ)/σ,  γ = Σp(1−p)(1−2p) / σ³
//
// 左边是 mid-p 分位（离散分布的中位校正），再取 z = Φ⁻¹(分位) 回到标准正态尺度。样本太少（σ² < 1）时为 0。
// 期望与各阶矩按每次的公平中奖率（DrawPlan.Fair，控制层之前）累计，见 DrawState.LuckExpected。
func LuckZ(state DrawState) float64 {
	if state.LuckVariance < luckMinVariance {
		return 0
	}
	sigma := math.Sqrt(state.LuckVariance)
	x := (float64(state.WinsTotal) - state.LuckExpected) / sigma
	gamma := state.LuckSkew / (sigma * sigma * sigma)
	normal := distuv.UnitNormal
	quantile := normal.CDF(x) + gamma*(1-x*x)*normal.Prob(x)/6
	quantile = math.Min(1-1e-9, math.Max(1e-9, quantile))
	return normal.Quantile(quantile)
}

func applyLuck(plan *DrawPlan, prizes []Prize, luck *LuckBalance, state DrawState) {
	z := LuckZ(state)
	plan.Luck = round6(z)
	if luck == nil || z == 0 {
		return
	}
	clamped := math.Max(-luckClampZ, math.Min(luckClampZ, z))
	current := winShare(plan.Odds, prizes)
	target := sigmoid(logit(current) - luck.Strength*clamped)
	setWinShare(plan.Odds, prizes, target, FactorLuck, fmt.Sprintf("z=%.2f", z))
}

// ── 2. 伪随机分布 ──

// PRDConstant 伪随机分布的常数 C：第 n 次（距上次中奖）中奖率为 min(1, n·C)，其长期平均中奖率等于 p。
// 平均中奖率 = 1 / E[次数]，E[次数] = Σ_{n≥1} Π_{k<n}(1 − kC)，对 C 单调，二分求解。
func PRDConstant(p float64) float64 {
	if p <= 0 {
		return 0
	}
	if p >= 1 {
		return 1
	}
	rate := func(c float64) float64 {
		expected, survive := 0.0, 1.0
		for n := 1; survive > 1e-12; n++ {
			expected += survive
			hit := math.Min(1, float64(n)*c)
			survive *= 1 - hit
		}
		return 1 / expected
	}
	lo, hi := 0.0, p
	for range 64 {
		mid := (lo + hi) / 2
		if rate(mid) < p {
			lo = mid
		} else {
			hi = mid
		}
	}
	return (lo + hi) / 2
}

func applyPRD(plan *DrawPlan, prizes []Prize, missStreak int) {
	current := winShare(plan.Odds, prizes)
	if current <= 0 || current >= 1 {
		return
	}
	c := PRDConstant(current)
	target := math.Min(1, float64(missStreak+1)*c)
	plan.PRD = round6(target)
	setWinShare(plan.Odds, prizes, target, FactorPRD, fmt.Sprintf("第 %d 次", missStreak+1))
}

// ── 3. 全站反馈 ──

func applyWinRateControl(plan *DrawPlan, prizes []Prize, control *WinRateControl, state DrawState) {
	if control == nil || state.ActivityDrawsToday <= 0 {
		return
	}
	prior := control.Prior
	if prior <= 0 {
		prior = controlConfidenceAt
	}
	posterior := (float64(state.ActivityWinsToday) + prior*control.Target) / (float64(state.ActivityDrawsToday) + prior)
	shift := control.Gain * (logit(control.Target) - logit(posterior))
	if math.Abs(shift) < 1e-6 {
		return
	}
	actual := float64(state.ActivityWinsToday) / float64(state.ActivityDrawsToday)
	current := winShare(plan.Odds, prizes)
	setWinShare(plan.Odds, prizes, sigmoid(logit(current)+shift), FactorControl,
		fmt.Sprintf("今日 %.1f%%，后验 %.1f%%，目标 %.1f%%", actual*100, posterior*100, control.Target*100))
}

// pacedBudgetFactor 积分预算按当天时间进度放量：允许已发 = 预算 ×（时间进度 + 5%）。
// 已发 / 允许 = r，系数 exp(−3(r − 1)) 截在 [0.05, 2]：超前收紧、落后放宽；单个奖品超过剩余预算时不出。
func pacedBudgetFactor(budget Budget, amount, issued int64, dayProgress float64) (float64, string) {
	remaining := budget.DailyPoints - issued
	allowed := float64(budget.DailyPoints) * math.Min(1, dayProgress+0.05)
	note := fmt.Sprintf("%d/%.0f", issued, allowed)
	if remaining <= 0 || amount > remaining {
		return 0, note
	}
	ratio := float64(issued) / math.Max(1, allowed)
	return round6(math.Max(0.05, math.Min(2, math.Exp(-3*(ratio-1))))), note
}

// ── 4. 护栏投影 ──

// applyGuard 把分布投影到护栏约束集上，取 KL 散度最小的那个（精确 I-投影，见 projection.go）：
// 中奖率上下限、期望价值上限、按奖品类型的占比上限，全部是对 p 的线性不等式。
func applyGuard(plan *DrawPlan, prizes []Prize, guard *Guard, values []float64) {
	if guard == nil {
		return
	}
	n := len(plan.Odds)
	q := make([]float64, n)
	winning := make([]float64, n)
	for index := range plan.Odds {
		q[index] = plan.Odds[index].Weight
		if prizes[index].Winning() {
			winning[index] = 1
		}
	}
	var constraints []linearConstraint
	if guard.MaxWinRate > 0 && guard.MaxWinRate < 1 {
		constraints = append(constraints, linearConstraint{a: winning, b: guard.MaxWinRate})
	}
	if guard.MinWinRate > 0 {
		negated := make([]float64, n)
		for i := range winning {
			negated[i] = -winning[i]
		}
		constraints = append(constraints, linearConstraint{a: negated, b: -guard.MinWinRate})
	}
	if guard.MaxValue > 0 {
		// 按上限归一，让各约束的量级相当，牛顿法的条件数更好
		scaled := make([]float64, n)
		for i := range values {
			scaled[i] = values[i] / guard.MaxValue
		}
		constraints = append(constraints, linearConstraint{a: scaled, b: 1})
	}
	for kind, limit := range guard.KindCaps {
		if limit <= 0 || limit >= 1 {
			continue
		}
		member := make([]float64, n)
		for i := range prizes {
			if prizes[i].Kind == kind {
				member[i] = 1
			}
		}
		constraints = append(constraints, linearConstraint{a: member, b: limit})
	}
	projected, feasible := iProject(q, constraints)
	plan.GuardInfeasible = !feasible
	for index := range plan.Odds {
		if q[index] <= 0 {
			continue
		}
		scale := projected[index] / q[index]
		if math.Abs(scale-1) > 1e-6 {
			plan.Odds[index].Weight = projected[index]
			plan.Odds[index].Factors = append(plan.Odds[index].Factors, Factor{Key: FactorGuard, Value: round6(scale)})
		}
	}
}
