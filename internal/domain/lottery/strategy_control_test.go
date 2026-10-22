package lottery

import (
	"math"
	"testing"
	"time"

	"gonum.org/v1/gonum/stat/distuv"
)

func TestPRDConstantMatchesKnownTable(t *testing.T) {
	// 公开的 PRD 常数表：25% → 8.4744%，50% → 30.2103%，5% → 0.3802%
	for _, tc := range []struct{ p, c float64 }{{0.25, 0.084744}, {0.5, 0.302103}, {0.05, 0.003802}} {
		if got := PRDConstant(tc.p); math.Abs(got-tc.c) > 2e-5 {
			t.Fatalf("PRDConstant(%v) = %v，应为 %v", tc.p, got, tc.c)
		}
	}
}

// TestPRDKeepsRateAndCutsDroughts 伪随机分布：长期中奖率不变，最长连败与每人中奖次数的离散度都明显变小。
func TestPRDKeepsRateAndCutsDroughts(t *testing.T) {
	input := SimulationInput{Users: 2000, DrawsPerUser: 60, Seed: 11, IsolatedStock: true}
	independent := Simulate(Activity{}, Strategy{}, strategyPrizes(), input)
	prd := Simulate(Activity{}, Strategy{Variance: VariancePRD}, strategyPrizes(), input)
	if math.Abs(prd.WinRate-independent.WinRate) > 0.01 {
		t.Fatalf("PRD 改变了长期中奖率：%v 对 %v", prd.WinRate, independent.WinRate)
	}
	if prd.MissStreak.P99 >= independent.MissStreak.P99 || prd.MissStreak.Max >= independent.MissStreak.Max {
		t.Fatalf("PRD 应缩短连败：%+v 对 %+v", prd.MissStreak, independent.MissStreak)
	}
	spread := func(d Distribution) float64 { return d.P90 - d.P50 }
	if spread(prd.WinsPerUser) >= spread(independent.WinsPerUser) {
		t.Fatalf("PRD 应让每人中奖次数更集中：%+v 对 %+v", prd.WinsPerUser, independent.WinsPerUser)
	}
}

func TestLuckBalanceShiftsBothWays(t *testing.T) {
	strategy := Strategy{Luck: &LuckBalance{Strength: 0.3}}
	base := PlanDraw(Activity{}, strategy, strategyPrizes(), DrawState{Now: time.Now()}).WinRate
	unlucky := PlanDraw(Activity{}, strategy, strategyPrizes(), DrawState{Now: time.Now(), WinsTotal: 2, LuckExpected: 10, LuckVariance: 5})
	lucky := PlanDraw(Activity{}, strategy, strategyPrizes(), DrawState{Now: time.Now(), WinsTotal: 18, LuckExpected: 10, LuckVariance: 5})
	if unlucky.WinRate <= base || lucky.WinRate >= base || unlucky.Luck >= 0 || lucky.Luck <= 0 {
		t.Fatalf("倒霉的人应上调、走运的人应下调：base=%v unlucky=%+v lucky=%+v", base, unlucky.WinRate, lucky.WinRate)
	}
	// 对数几率上对称
	if d1, d2 := logit(unlucky.WinRate)-logit(base), logit(base)-logit(lucky.WinRate); math.Abs(d1-d2) > 1e-3 {
		t.Fatalf("校正量应对称：%v 对 %v", d1, d2)
	}
	// 样本太少不校正
	early := PlanDraw(Activity{}, strategy, strategyPrizes(), DrawState{Now: time.Now(), LuckExpected: 0.4, LuckVariance: 0.24})
	if early.WinRate != base {
		t.Fatalf("方差不足 1 时不该校正：%v", early.WinRate)
	}
}

// TestLuckBalanceNarrowsSpread 运气平衡在模拟里应让每人中奖次数更集中，且不明显改变总体中奖率。
func TestLuckBalanceNarrowsSpread(t *testing.T) {
	input := SimulationInput{Users: 2000, DrawsPerUser: 80, Seed: 5, IsolatedStock: true}
	plain := Simulate(Activity{}, Strategy{}, strategyPrizes(), input)
	balanced := Simulate(Activity{}, Strategy{Luck: &LuckBalance{Strength: 0.5}}, strategyPrizes(), input)
	if math.Abs(balanced.WinRate-plain.WinRate) > 0.015 {
		t.Fatalf("运气平衡不该明显改变总体中奖率：%v 对 %v", balanced.WinRate, plain.WinRate)
	}
	if balanced.WinsPerUser.P90-balanced.WinsPerUser.P50 >= plain.WinsPerUser.P90-plain.WinsPerUser.P50 {
		t.Fatalf("运气平衡应缩小每人中奖次数的差距：%+v 对 %+v", balanced.WinsPerUser, plain.WinsPerUser)
	}
}

func TestWinRateControlPullsTowardTarget(t *testing.T) {
	strategy := Strategy{WinRate: &WinRateControl{Target: 0.44, Gain: 4}}
	low := PlanDraw(Activity{}, strategy, strategyPrizes(), DrawState{Now: time.Now(), ActivityDrawsToday: 1000, ActivityWinsToday: 300})
	high := PlanDraw(Activity{}, strategy, strategyPrizes(), DrawState{Now: time.Now(), ActivityDrawsToday: 1000, ActivityWinsToday: 600})
	if low.WinRate <= 0.44 || high.WinRate >= 0.44 {
		t.Fatalf("全站中奖率低于目标应上调、高于应下调：%v / %v", low.WinRate, high.WinRate)
	}
	none := PlanDraw(Activity{}, strategy, strategyPrizes(), DrawState{Now: time.Now()})
	if none.WinRate != 0.44 {
		t.Fatalf("今天还没有样本时不该调整：%v", none.WinRate)
	}
}

func TestGuardProjectsOntoBounds(t *testing.T) {
	// 规则把积分奖放大 20 倍，护栏把中奖率压回 50% 以内、期望价值压到 2 积分以内
	strategy := NormalizeStrategy(Strategy{
		Rules: []StrategyRule{{When: "", Kinds: []string{KindPoints}, Factor: 20}},
		Guard: &Guard{MinWinRate: 0.2, MaxWinRate: 0.5, MaxValue: 2},
	})
	activity := Activity{LifetimePointsPerDay: 10}
	plan := PlanDraw(activity, strategy, strategyPrizes(), DrawState{Now: time.Now()})
	if plan.WinRate > 0.5+1e-4 || plan.WinRate < 0.2-1e-4 || plan.Value > 2+1e-3 {
		t.Fatalf("护栏未生效：winRate=%v value=%v", plan.WinRate, plan.Value)
	}
	// 只设下限时把中奖率抬上去
	floor := PlanDraw(activity, NormalizeStrategy(Strategy{Guard: &Guard{MinWinRate: 0.7}}), strategyPrizes(), DrawState{Now: time.Now()})
	if math.Abs(floor.WinRate-0.7) > 1e-4 {
		t.Fatalf("中奖率下限应抬到 70%%：%v", floor.WinRate)
	}
	// 价值约束下高价值奖品被压得最多（指数倾斜）
	tilted := PlanDraw(activity, NormalizeStrategy(Strategy{Guard: &Guard{MaxValue: 1}}), strategyPrizes(), DrawState{Now: time.Now()})
	ratio := func(p DrawPlan, i int) float64 { return p.Odds[i].Probability / float64(strategyPrizes()[i].Weight) }
	if !(ratio(tilted, 1) > ratio(tilted, 3) && ratio(tilted, 3) > ratio(tilted, 5) && ratio(tilted, 5) > ratio(tilted, 7)) {
		t.Fatalf("价值越高压得越多：%v %v %v %v", ratio(tilted, 1), ratio(tilted, 3), ratio(tilted, 5), ratio(tilted, 7))
	}
	if tilted.Value > 1+1e-3 {
		t.Fatalf("期望价值应不超过 1：%v", tilted.Value)
	}
}

func TestPacedBudget(t *testing.T) {
	budget := Budget{DailyPoints: 1000, Paced: true}
	early, _ := pacedBudgetFactor(budget, 5, 500, 0.1)  // 早上就发了一半：收紧
	behind, _ := pacedBudgetFactor(budget, 5, 100, 0.8) // 晚上才发一成：放宽
	exact, _ := pacedBudgetFactor(budget, 5, 450, 0.4)  // 正好在进度上
	if early >= 0.2 || behind <= 1.5 || math.Abs(exact-1) > 1e-6 {
		t.Fatalf("按时间进度放量：early=%v behind=%v exact=%v", early, behind, exact)
	}
	if over, _ := pacedBudgetFactor(budget, 50, 980, 0.9); over != 0 {
		t.Fatalf("超过剩余预算的奖品不出：%v", over)
	}
}

func TestValidateControl(t *testing.T) {
	bad := []Strategy{
		{Guard: &Guard{MinWinRate: 0.6, MaxWinRate: 0.4}},
		{Luck: &LuckBalance{Strength: 5}},
		{WinRate: &WinRateControl{Target: 1.2, Gain: 1}},
	}
	for _, s := range bad {
		if err := ValidateStrategy(NormalizeStrategy(s), 8); err == nil {
			t.Fatalf("非法控制参数没有被拒：%+v", s)
		}
	}
}

// TestLuckRNAIsAccurate 与动态规划算出的精确泊松二项 mid-p 分位比较：带偏度修正的 RNA 比普通正态近似更准。
func TestLuckRNAIsAccurate(t *testing.T) {
	ps := make([]float64, 0, 30)
	for i := range 30 {
		ps = append(ps, 0.03+0.01*float64(i%5)) // 小概率、明显右偏
	}
	pmf := []float64{1}
	var mu, variance, skew float64
	for _, p := range ps {
		next := make([]float64, len(pmf)+1)
		for k, v := range pmf {
			next[k] += v * (1 - p)
			next[k+1] += v * p
		}
		pmf = next
		mu += p
		variance += p * (1 - p)
		skew += p * (1 - p) * (1 - 2*p)
	}
	normal := distuv.UnitNormal
	var errRNA, errPlain float64
	for k := 0; k <= 5; k++ {
		var below float64
		for j := 0; j < k; j++ {
			below += pmf[j]
		}
		exact := normal.Quantile(below + pmf[k]/2)
		rna := LuckZ(DrawState{WinsTotal: k, LuckExpected: mu, LuckVariance: variance, LuckSkew: skew})
		plain := (float64(k) - mu) / math.Sqrt(variance)
		errRNA += math.Abs(rna - exact)
		errPlain += math.Abs(plain - exact)
	}
	if errRNA >= errPlain*0.6 {
		t.Fatalf("RNA 应明显更准：RNA 误差 %.4f，正态 %.4f", errRNA, errPlain)
	}
}
