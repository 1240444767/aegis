package lottery

import (
	"math"
	"math/rand/v2"
	"testing"
)

func kl(p, q []float64) float64 {
	var sp, sq float64
	for i := range p {
		sp += p[i]
		sq += q[i]
	}
	var d float64
	for i := range p {
		if p[i] > 0 {
			d += p[i] / sp * math.Log((p[i]/sp)/(q[i]/sq))
		}
	}
	return d
}

func satisfies(p []float64, constraints []linearConstraint) bool {
	var total float64
	for _, w := range p {
		total += w
	}
	for _, c := range constraints {
		var s float64
		for i := range p {
			s += p[i] / total * c.a[i]
		}
		if s > c.b+1e-6 {
			return false
		}
	}
	return true
}

// TestIProjectionIsOptimal 投影结果满足约束，且任何随机生成的可行分布与 q 的 KL 散度都不小于它。
func TestIProjectionIsOptimal(t *testing.T) {
	q := []float64{40, 30, 10, 10, 5, 3, 1, 1}
	win := []float64{0, 1, 0, 1, 0, 1, 0, 1}
	value := []float64{0, 2, 0, 5, 0, 15, 0, 70}
	negWin := make([]float64, len(win))
	for i := range win {
		negWin[i] = -win[i]
	}
	vip := []float64{0, 0, 0, 0, 0, 0, 0, 1}
	constraints := []linearConstraint{
		{a: win, b: 0.35},    // 中奖率 ≤ 35%
		{a: negWin, b: -0.2}, // 中奖率 ≥ 20%
		{a: value, b: 1.5},   // 期望价值 ≤ 1.5
		{a: vip, b: 0.002},   // 最贵的一格 ≤ 0.2%
	}
	p, feasible := iProject(q, constraints)
	if !feasible || !satisfies(p, constraints) {
		t.Fatalf("投影不满足约束：%v feasible=%v", p, feasible)
	}
	best := kl(p, q)
	rng := rand.New(rand.NewPCG(1, 2))
	tried := 0
	for range 200_000 {
		candidate := make([]float64, len(q))
		for i := range q {
			candidate[i] = q[i] * math.Exp(rng.NormFloat64())
		}
		if !satisfies(candidate, constraints) {
			continue
		}
		tried++
		if kl(candidate, q) < best-1e-9 {
			t.Fatalf("找到 KL 更小的可行分布：%v < %v", kl(candidate, q), best)
		}
	}
	if tried < 100 {
		t.Fatalf("随机可行样本太少（%d），检验无效", tried)
	}
}

func TestIProjectionNoopAndInfeasible(t *testing.T) {
	q := []float64{1, 1}
	win := []float64{0, 1}
	p, ok := iProject(q, []linearConstraint{{a: win, b: 0.9}})
	if !ok || math.Abs(p[0]-1) > 1e-9 || math.Abs(p[1]-1) > 1e-9 {
		t.Fatalf("约束本来就满足时不该改动：%v", p)
	}
	// 只剩中奖奖品可发时，中奖率上限不可行
	_, ok = iProject([]float64{0, 1}, []linearConstraint{{a: win, b: 0.5}})
	if ok {
		t.Fatal("支撑集上不可行的约束应报告不可行")
	}
}
