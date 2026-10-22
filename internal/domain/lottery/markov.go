package lottery

import "math"

// 首次中奖的精确分析（吸收马尔可夫链 + 更新理论）。
//
// 把「已经连续未中 k 次」看成马尔可夫链的状态 k，抽一次要么中奖（被吸收，回到 0），要么进入 k+1。
// 第 k 个状态的中奖率 h_k（风险函数 hazard）由同一个 PlanDraw / PityChoice 给出：软保底、PRD、
// 硬保底都只依赖 k，其余维度（画像、规则、全站反馈、护栏）在一次分析里视为固定。于是距中奖次数 T 满足
//
//	P(T > n) = Π_{k<n} (1 − h_k),   E[T] = Σ_{n≥0} P(T > n),   E[T²] = Σ_{n≥0} (2n + 1) P(T > n)
//
// 超出分析范围的尾部按最后一个状态的几何分布补齐。每次中奖都把连败清零，中奖是一个更新过程，
// 由更新报酬定理（renewal-reward theorem），长期中奖率 = 1 / E[T]。
//
// 这是精确值，不是抽样：同样的策略与画像，模拟器的结果会围绕它波动。
// 运气平衡依赖整段历史而不只是 k，这里按分析时的运气状态固定处理。

// StreakAnalysis 距首次中奖次数 T 的分布。
type StreakAnalysis struct {
	// Hazard 已连续未中 k 次时，这一次中奖的概率（下标即 k）
	Hazard []float64 `json:"hazard"`
	// Survival P(T > n)，n = 0..len(Hazard)
	Survival []float64 `json:"survival"`
	Mean     float64   `json:"mean"`
	Std      float64   `json:"std"`
	P50      int       `json:"p50"`
	P90      int       `json:"p90"`
	P99      int       `json:"p99"`
	// LongRunWinRate 长期中奖率 = 1 / E[T]
	LongRunWinRate float64 `json:"longRunWinRate"`
	// Never 永远不会中奖（所有状态的中奖率都为 0）
	Never bool `json:"never,omitempty"`
}

// MaxStreakHorizon 分析的最大状态数。
const MaxStreakHorizon = 1000

// AnalyzeStreak 以 state 为起点（MissStreak 从 0 起）分析距中奖的次数。
func AnalyzeStreak(activity Activity, strategy Strategy, prizes []Prize, state DrawState) StreakAnalysis {
	horizon := 200
	if activity.PityThreshold > 0 {
		horizon = max(horizon, activity.PityThreshold+1)
	}
	if strategy.SoftPity != nil {
		horizon = max(horizon, strategy.SoftPity.Start+int(math.Ceil(100/math.Max(strategy.SoftPity.Step, 0.1)))+1)
	}
	horizon = min(horizon, MaxStreakHorizon)

	result := StreakAnalysis{Hazard: make([]float64, 0, horizon), Survival: []float64{1}}
	survival := 1.0
	var mean, second float64
	for k := 0; k < horizon && survival > 1e-12; k++ {
		st := state
		st.MissStreak = k
		plan := PlanDraw(activity, strategy, prizes, st)
		h := plan.WinRate
		if _, pity := PityChoice(activity, plan, prizes, k); pity {
			h = 1
		}
		result.Hazard = append(result.Hazard, round6(h))
		mean += survival
		second += float64(2*k+1) * survival
		survival *= 1 - h
		result.Survival = append(result.Survival, survival)
	}
	n := len(result.Hazard)
	if survival > 1e-12 {
		last := result.Hazard[n-1]
		if last <= 0 {
			result.Never = true
			return result
		}
		// 几何尾部：Σ_{j≥0} S_n (1−h)^j = S_n / h；Σ (2(n+j)+1) S_n (1−h)^j 的闭式
		q := 1 - last
		mean += survival / last
		second += survival * ((2*float64(n)+1)/last + 2*q/(last*last))
	}
	result.Mean = round6(mean)
	result.Std = round6(math.Sqrt(math.Max(0, second-mean*mean)))
	result.LongRunWinRate = round6(1 / mean)
	result.P50 = streakQuantile(result, 0.5)
	result.P90 = streakQuantile(result, 0.9)
	result.P99 = streakQuantile(result, 0.99)
	for i := range result.Survival {
		result.Survival[i] = round6(result.Survival[i])
	}
	return result
}

// streakQuantile 最小的 n 使 P(T ≤ n) ≥ q；超出分析范围时按几何尾部外推。
func streakQuantile(a StreakAnalysis, q float64) int {
	for n := 1; n < len(a.Survival); n++ {
		if 1-a.Survival[n] >= q-1e-12 {
			return n
		}
	}
	n := len(a.Survival) - 1
	last := a.Hazard[len(a.Hazard)-1]
	s := a.Survival[n]
	if last <= 0 || s <= 0 {
		return n
	}
	// s(1−h)^j ≤ 1 − q
	j := math.Ceil(math.Log((1-q)/s) / math.Log(1-last))
	return n + int(math.Max(0, j))
}
