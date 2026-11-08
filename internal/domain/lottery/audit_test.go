package lottery

import (
	"math/rand/v2"
	"testing"
	"time"
)

// auditSample 用真实的方案抽 n 次，cheat 为真时把每 10 次里的一次换成第一个中奖奖品。
func auditSample(n int, strategy Strategy, cheat bool, seed uint64) []AuditDraw {
	src := rand.NewPCG(seed, seed+1)
	rng := rand.New(src)
	draws := make([]AuditDraw, 0, n)
	prizes := strategyPrizes()
	for t := range n {
		// 每次的状态不同（软保底、运气），方案也不同
		state := DrawState{Now: time.Now(), MissStreak: rng.IntN(6), WinsTotal: rng.IntN(30), LuckExpected: 15, LuckVariance: 8}
		plan := PlanDraw(Activity{}, strategy, prizes, state)
		index, _ := Sample(plan, src)
		picked := prizes[index].ID
		if cheat && t%10 == 0 {
			picked = prizes[1].ID
		}
		draws = append(draws, AuditDraw{Odds: plan.Odds, Picked: picked})
	}
	return draws
}

func TestAuditPassesHonestDraws(t *testing.T) {
	strategy := Strategy{SoftPity: &SoftPity{Start: 2, Step: 10}, Luck: &LuckBalance{Strength: 0.4}}
	failures := 0
	for seed := range uint64(20) {
		if !Audit(auditSample(5000, strategy, false, seed+1)).Passed {
			failures++
		}
	}
	// α = 0.01、整体加边际两道检验：20 次里最多偶尔误报一两次
	if failures > 2 {
		t.Fatalf("诚实的出奖被判不通过 %d/20 次", failures)
	}
	result := Audit(auditSample(5000, strategy, false, 99))
	if result.DF != 7 || result.Draws != 5000 {
		t.Fatalf("自由度应为类别数 − 1：%+v", result)
	}
}

func TestAuditCatchesBias(t *testing.T) {
	result := Audit(auditSample(5000, Strategy{SoftPity: &SoftPity{Start: 2, Step: 10}}, true, 7))
	if result.Passed || result.PValue > 1e-6 {
		t.Fatalf("偏离方案 10%% 的出奖应被识破：p=%v", result.PValue)
	}
	if !result.Prizes[1].Flagged || result.Prizes[1].Z <= 0 {
		t.Fatalf("被多发的奖品应被标出：%+v", result.Prizes[1])
	}
}

func TestAuditSkipsPityAndFallback(t *testing.T) {
	draws := auditSample(100, Strategy{}, false, 1)
	draws[0].Pity, draws[1].Fallback = true, true
	if r := Audit(draws); r.Draws != 98 || r.Skipped != 2 {
		t.Fatalf("保底与兜底不计入：%+v", r)
	}
}
