package lottery

import (
	"math"
	"testing"

	"aegis/internal/domain/pointstore"
	vipdomain "aegis/internal/domain/vip"
)

// 线上出事的那个奖盘：积分商城 10 积分一次，永久会员抽中月度 / 季度会员按会员单价折成 300 / 1240 积分。
func productionBoard() ([]Prize, map[int64]pointstore.PlanRef) {
	plans := map[int64]pointstore.PlanRef{
		14: {ID: 14, Name: "月度会员", DurationDays: 30, Features: []string{"ai"}},
		15: {ID: 15, Name: "季度会员", DurationDays: 124, Features: []string{"ai"}},
	}
	prize := func(position, weight int, reward Reward) Prize {
		return Prize{ID: int64(position + 1), Name: reward.Type, Kind: reward.Type, Reward: reward, Weight: weight, Quantity: -1, Position: position}
	}
	return []Prize{
		prize(0, 7, Reward{Type: KindVipPlan, RefID: 15}),
		prize(1, 30, Reward{Type: KindExperience, Amount: 10}),
		prize(2, 15, Reward{Type: KindPoints, Amount: 10}),
		prize(3, 10, Reward{Type: KindExperience, Amount: 20}),
		prize(4, 10, Reward{Type: KindPoints, Amount: 50}),
		prize(5, 7, Reward{Type: KindLotteryDraws, Amount: 1}),
		prize(6, 5, Reward{Type: KindPoints, Amount: 20}),
		prize(7, 7, Reward{Type: KindVipPlan, RefID: 14}),
	}, plans
}

func TestEconomyCapsLifetimeConversionBelowDrawPrice(t *testing.T) {
	prizes, plans := productionBoard()
	lifetime := MemberCoverage{
		Holdings: vipdomain.Holdings{Lifetime: true, LifetimeAdFree: true, LifetimeFeatures: []string{"ai"}},
		Policy:   vipdomain.DefaultConversionPolicy(1),
	}
	rule := RuleOf(Activity{LifetimePolicy: LifetimePoints}, lifetime.Policy)

	uncapped := EconomyOf(rule, lifetime, prizes, plans, 0)
	if uncapped.Cap != NoCap {
		t.Fatalf("积分换不到次数时不封顶，得到 %+v", uncapped)
	}
	// 不封顶时：(750 + 7×300 + 7×1240)/91 ÷ (1 − 7/91) ≈ 141 积分一次，远超 10 积分的价格
	if got := uncapped.ExpectedReturn(1240); got < 100 {
		t.Fatalf("复现的期望回报应远超价格，得到 %.1f", got)
	}

	economy := EconomyOf(rule, lifetime, prizes, plans, DrawPrice(Activity{}, 10))
	if economy.Cap < 0 || economy.Cap > 10 {
		t.Fatalf("10 积分一次时折算上限应在 0–10 之间，得到 %+v", economy)
	}
	if got := economy.ExpectedReturn(economy.Cap); got > 10+1e-9 {
		t.Fatalf("封顶之后期望回报不能超过价格，得到 %.3f（%+v）", got, economy)
	}
	if got := economy.ExpectedReturn(economy.Cap + 1); got <= 10 {
		t.Fatalf("上限应是最大的那个整数，+1 后 %.3f 仍不超价", got)
	}
	capped := rule.WithCap(economy)
	alt, ok := Swap(capped, prizes[7], plans)
	if !ok || alt.Type != KindPoints || alt.Amount != economy.Cap {
		t.Fatalf("月度会员应折成上限 %d 积分，得到 %+v", economy.Cap, alt)
	}

	// 普通用户：会员格子不是已拥有，没有可封的；奖盘本身不构成套利
	plain := EconomyOf(rule, MemberCoverage{Policy: lifetime.Policy}, prizes, plans, 10)
	if plain.Cap != NoCap || plain.Arbitrage() {
		t.Fatalf("普通用户不封顶、奖盘本身不套利，得到 %+v", plain)
	}
}

func TestEconomyZeroCapGrantsNothing(t *testing.T) {
	prizes, plans := productionBoard()
	lifetime := MemberCoverage{Holdings: vipdomain.Holdings{Lifetime: true, LifetimeAdFree: true, LifetimeFeatures: []string{"ai"}}}
	rule := RuleOf(Activity{LifetimePolicy: LifetimePoints}, vipdomain.DefaultConversionPolicy(1))
	// 价格 8 积分：积分奖品本身的期望已接近价格，留给折算的是 0
	economy := EconomyOf(rule, lifetime, prizes, plans, 8)
	if economy.Cap != 0 {
		t.Fatalf("价格 8 时折算上限应为 0，得到 %+v", economy)
	}
	if !economy.Arbitrage() {
		t.Fatal("8 积分一次时奖盘本身就套利，应能识别出来")
	}
	alt, ok := Swap(rule.WithCap(economy), prizes[7], plans)
	if !ok || alt.Type != KindNone {
		t.Fatalf("上限为 0 时改发谢谢参与，得到 %+v %v", alt, ok)
	}
	if swap := DescribeSwap(rule.WithCap(economy), lifetime, prizes[7], plans); swap == nil || swap.Label != "转为谢谢参与" || swap.Reward.Type != KindNone {
		t.Fatalf("活动页提示应说明不再重复发放，得到 %+v", swap)
	}
	if msg := ConversionMessage(OwnedLifetime, "月度会员", SwapLabel(alt)); msg != "你已是永久会员，「月度会员」不再重复发放" {
		t.Fatalf("说明不对：%s", msg)
	}
	// 奖品单独设的改发积分同样封顶
	prize := prizes[7]
	prize.LifetimeReward = &Reward{Type: KindPoints, Amount: 999}
	if alt, _ := Swap(rule.WithCap(Economy{Cap: 3}), prize, plans); alt.Amount != 3 {
		t.Fatalf("改发积分也要封顶，得到 %+v", alt)
	}
	// 改发经验、抽奖次数之外的不受影响
	prize.LifetimeReward = &Reward{Type: KindExperience, Amount: 999}
	if alt, _ := Swap(rule.WithCap(Economy{Cap: 0}), prize, plans); alt.Type != KindExperience || alt.Amount != 999 {
		t.Fatalf("改发经验不封顶，得到 %+v", alt)
	}
}

func TestEconomyRedrawsAndPrice(t *testing.T) {
	if DrawPrice(Activity{PointsCost: 30}, 10) != 10 || DrawPrice(Activity{PointsCost: 5}, 10) != 5 ||
		DrawPrice(Activity{}, 0) != 0 || DrawPrice(Activity{PointsCost: 7}, 0) != 7 {
		t.Fatal("价格取所有积分路径里最便宜的")
	}
	if got := (Economy{Redraws: 1}).ExpectedReturn(0); !math.IsInf(got, 1) {
		t.Fatalf("再得次数期望不小于 1 时抽不完，得到 %v", got)
	}
	// 永久会员已拥有的格子改发抽奖次数：算进再得次数，不是积分
	prizes, plans := productionBoard()
	lifetime := MemberCoverage{Holdings: vipdomain.Holdings{Lifetime: true, LifetimeAdFree: true, LifetimeFeatures: []string{"ai"}}}
	draws := RuleOf(Activity{LifetimePolicy: LifetimeDraws, LifetimeDraws: 1}, vipdomain.DefaultConversionPolicy(1))
	if free := EconomyOf(draws, lifetime, prizes, plans, 0); free.Owned != 0 || math.Abs(free.Redraws-21.0/91) > 0.01 {
		t.Fatalf("积分换不到次数时，改发的次数只是再抽一次，得到 %+v", free)
	}
	economy := EconomyOf(draws, lifetime, prizes, plans, 10)
	if math.Abs(economy.Owned-14.0/91) > 0.01 || math.Abs(economy.Redraws-7.0/91) > 0.01 || economy.Cap >= 10 {
		t.Fatalf("10 积分一次时改发的次数按 10 积分封顶，得到 %+v", economy)
	}
	alt, _ := Swap(draws.WithCap(economy), prizes[7], plans)
	if alt.Type != KindPoints || alt.Amount != economy.Cap || economy.ExpectedReturn(economy.Cap) > 10 {
		t.Fatalf("超过上限的改发次数换成上限积分，得到 %+v（%+v）", alt, economy)
	}
}
