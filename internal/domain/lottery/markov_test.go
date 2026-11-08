package lottery

import (
	"math"
	"testing"
	"time"
)

// TestAnalyzeStreakGeometric 独立抽取时 T 服从几何分布：E[T] = 1/p，Std = √(1−p)/p，长期中奖率 = p。
func TestAnalyzeStreakGeometric(t *testing.T) {
	a := AnalyzeStreak(Activity{}, Strategy{}, strategyPrizes(), DrawState{Now: time.Now()})
	p := 0.44
	if math.Abs(a.Mean-1/p) > 1e-4 || math.Abs(a.Std-math.Sqrt(1-p)/p) > 1e-4 || math.Abs(a.LongRunWinRate-p) > 1e-4 {
		t.Fatalf("几何分布不对：%+v", a)
	}
	// P(T ≤ n) ≥ 0.9 的最小 n：1 − 0.56^n ≥ 0.9 → n = 4
	if a.P90 != 4 || a.P50 != 2 {
		t.Fatalf("分位数不对：p50=%d p90=%d", a.P50, a.P90)
	}
}

// TestAnalyzeStreakMatchesSimulation 精确分析与蒙特卡洛模拟一致（硬保底 + PRD）。
func TestAnalyzeStreakMatchesSimulation(t *testing.T) {
	pityID := int64(2)
	activity := Activity{PityThreshold: 6, PityPrizeID: &pityID}
	strategy := Strategy{Variance: VariancePRD}
	prizes := strategyPrizes()
	for i := range prizes {
		if prizes[i].Winning() {
			prizes[i].Weight = prizes[i].Weight / 4
		}
	}
	a := AnalyzeStreak(activity, strategy, prizes, DrawState{Now: time.Now()})
	if a.Hazard[5] != 1 || len(a.Hazard) != 6 {
		t.Fatalf("第 6 次应必中：%v", a.Hazard)
	}
	sim := Simulate(activity, strategy, prizes, SimulationInput{Users: 3000, DrawsPerUser: 60, Seed: 3, IsolatedStock: true})
	if math.Abs(sim.WinRate-a.LongRunWinRate) > 0.01 {
		t.Fatalf("长期中奖率：分析 %v，模拟 %v", a.LongRunWinRate, sim.WinRate)
	}
	if math.Abs(sim.FirstWin.Mean-a.Mean) > 0.1 {
		t.Fatalf("首中均值：分析 %v，模拟 %v", a.Mean, sim.FirstWin.Mean)
	}
}

func TestAnalyzeStreakNever(t *testing.T) {
	prizes := strategyPrizes()
	for i := range prizes {
		if prizes[i].Winning() {
			prizes[i].Weight = 0
		}
	}
	if a := AnalyzeStreak(Activity{}, Strategy{}, prizes, DrawState{Now: time.Now()}); !a.Never {
		t.Fatalf("没有可中的奖品时应为永不中奖：%+v", a.Mean)
	}
}
