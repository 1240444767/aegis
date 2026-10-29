package lottery

import (
	"math"
	"testing"
	"time"

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

// 线上的 12 格转盘：会员 3 / 7 天、免广告、工具台样式、永久会员，积分奖品期望 7 积分左右；15 经验是兜底奖。
func wheelBoard() ([]Prize, map[int64]pointstore.PlanRef) {
	plans := map[int64]pointstore.PlanRef{17: {ID: 17, Name: "永久会员", Lifetime: true, Features: []string{"ai"}}}
	prize := func(position, weight int, reward Reward) Prize {
		return Prize{ID: int64(position + 1), Name: reward.Type, Kind: reward.Type, Reward: reward, Weight: weight, Quantity: -1, Position: position}
	}
	prizes := []Prize{
		prize(0, 1, Reward{Type: KindVipPlan, RefID: 17}),
		prize(1, 22000, Reward{Type: KindPoints, Amount: 10}),
		prize(2, 200, Reward{Type: KindVipDays, Amount: 7}),
		prize(3, 49299, Reward{Type: KindExperience, Amount: 15}),
		prize(4, 4000, Reward{Type: KindAdFree, Amount: 1}),
		prize(5, 5000, Reward{Type: KindPoints, Amount: 66}),
		prize(6, 1500, Reward{Type: KindFeature, Tag: "ai", Amount: 3}),
		prize(7, 9000, Reward{Type: KindExperience, Amount: 30}),
		prize(8, 800, Reward{Type: KindPoints, Amount: 188}),
		prize(9, 6000, Reward{Type: KindLotteryDraws, Amount: 1}),
		prize(10, 1200, Reward{Type: KindVipDays, Amount: 3}),
		prize(11, 1000, Reward{Type: KindAdFree, Amount: 3}),
	}
	prizes[3].Fallback = true
	return prizes, plans
}

func lifetimeMember() MemberCoverage {
	return MemberCoverage{
		Holdings: vipdomain.Holdings{Lifetime: true, LifetimeAdFree: true, LifetimeFeatures: []string{"ai"}},
		Policy:   vipdomain.DefaultConversionPolicy(1),
	}
}

// 正常的奖盘不动折算：永久会员抽中会员 7 天拿 70 积分、抽中永久会员拿永久折算，与积分商城卖不卖次数无关。
func TestEconomyKeepsFullValueOnHealthyBoard(t *testing.T) {
	prizes, plans := wheelBoard()
	lifetime := lifetimeMember()
	rule := RuleOf(Activity{LifetimePolicy: LifetimePoints}, lifetime.Policy)
	economy := EconomyOf(rule, lifetime, prizes, plans, 10)
	if economy.Scaled() || economy.Scale != 1 {
		t.Fatalf("原价折算不构成套利时不缩减，得到 %+v", economy)
	}
	if got := economy.ExpectedReturn(1); got > 10 {
		t.Fatalf("原价折算的期望回报应低于价格，得到 %.3f", got)
	}
	priced := rule.WithEconomy(economy)
	for position, want := range map[int]int64{2: 70, 10: 30, 0: lifetime.Policy.PerLifetime} {
		if alt, ok := Swap(priced, prizes[position], plans); !ok || alt.Type != KindPoints || alt.Amount != want {
			t.Errorf("位置 %d 应按原价折 %d 积分，得到 %+v", position, want, alt)
		}
	}
	if audit := AuditEconomy(Activity{LifetimePolicy: LifetimePoints}, prizes, plans, lifetime, 10, nil); audit.LifetimeScale != 1 || len(audit.Warnings) != 0 {
		t.Fatalf("控制台积分账不应警告，得到 %+v", audit)
	}
}

// 出事的奖盘：原价折算一次期望一百多积分，按比例缩到刚好回本；月度与季度的相对大小不变。
func TestEconomyScalesOnlyWhenArbitrage(t *testing.T) {
	prizes, plans := productionBoard()
	lifetime := lifetimeMember()
	rule := RuleOf(Activity{LifetimePolicy: LifetimePoints}, lifetime.Policy)

	free := EconomyOf(rule, lifetime, prizes, plans, 0)
	if free.Scaled() {
		t.Fatalf("积分换不到次数时不缩减，得到 %+v", free)
	}
	// 原价：(750 + 7×300 + 7×1240)/91 ÷ (1 − 7/91) ≈ 137 积分一次，远超 10 积分的价格
	if got := free.ExpectedReturn(1); got < 100 {
		t.Fatalf("复现的期望回报应远超价格，得到 %.1f", got)
	}

	economy := EconomyOf(rule, lifetime, prizes, plans, DrawPrice(Activity{}, 10))
	if !economy.Scaled() || economy.Scale <= 0 || economy.Scale >= 0.1 {
		t.Fatalf("10 积分一次时应按很小的比例缩减，得到 %+v", economy)
	}
	if got := economy.ExpectedReturn(economy.Scale); math.Abs(got-10) > 1e-9 {
		t.Fatalf("缩减之后期望回报应刚好等于价格，得到 %.6f", got)
	}
	scaled := rule.WithEconomy(economy)
	quarter, _ := Swap(scaled, prizes[0], plans)
	monthly, _ := Swap(scaled, prizes[7], plans)
	if quarter.Type != KindPoints || monthly.Type != KindPoints || quarter.Amount <= monthly.Amount {
		t.Fatalf("季度会员折得应比月度多（按比例缩），得到 季度 %+v 月度 %+v", quarter, monthly)
	}
	if want := int64(math.Floor(300 * economy.Scale)); monthly.Amount != want {
		t.Fatalf("月度会员应折 floor(300 × %.4f) = %d，得到 %+v", economy.Scale, want, monthly)
	}

	// 普通用户：会员格子不是已拥有，没有可缩的；奖盘本身不构成套利
	plain := EconomyOf(rule, MemberCoverage{Policy: lifetime.Policy}, prizes, plans, 10)
	if plain.Scaled() || plain.Arbitrage() {
		t.Fatalf("普通用户不缩减、奖盘本身不套利，得到 %+v", plain)
	}
	audit := AuditEconomy(Activity{LifetimePolicy: LifetimePoints}, prizes, plans, lifetime, 10, nil)
	if audit.LifetimeScale >= 1 || len(audit.Warnings) != 1 {
		t.Fatalf("永久会员被缩减时控制台要提示，得到 %+v", audit)
	}
}

// 缩到不足 1 积分（积分奖品本身就接近价格）时改发经验，不会是谢谢参与。
func TestEconomyScaledToZeroGrantsExperience(t *testing.T) {
	prizes, plans := productionBoard()
	lifetime := lifetimeMember()
	rule := RuleOf(Activity{LifetimePolicy: LifetimePoints}, lifetime.Policy)
	economy := EconomyOf(rule, lifetime, prizes, plans, 8)
	if economy.Scale != 0 || !economy.Arbitrage() {
		t.Fatalf("价格 8 时积分奖品本身就套利，比例应为 0，得到 %+v", economy)
	}
	alt, ok := Swap(rule.WithEconomy(economy), prizes[7], plans)
	if !ok || alt.Type != KindExperience || alt.Amount != MaxScaledExperience {
		t.Fatalf("比例为 0 时改发经验（最多 %d 点），得到 %+v %v", MaxScaledExperience, alt, ok)
	}
	if swap := DescribeSwap(rule.WithEconomy(economy), lifetime, prizes[7], plans); swap == nil || swap.Reward.Type != KindExperience {
		t.Fatalf("活动页提示应写改发的经验，得到 %+v", swap)
	}
	// 奖品单独设的改发积分同样按比例
	prize := prizes[7]
	prize.LifetimeReward = &Reward{Type: KindPoints, Amount: 999}
	if alt, _ := Swap(rule.WithEconomy(Economy{Scale: 0.5}), prize, plans); alt.Type != KindPoints || alt.Amount != 499 {
		t.Fatalf("改发积分也按比例，得到 %+v", alt)
	}
	// 改发经验不受影响
	prize.LifetimeReward = &Reward{Type: KindExperience, Amount: 999}
	if alt, _ := Swap(rule.WithEconomy(Economy{Scale: 0}), prize, plans); alt.Type != KindExperience || alt.Amount != 999 {
		t.Fatalf("改发经验不缩减，得到 %+v", alt)
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
	// 永久会员已拥有的格子改发抽奖次数：积分换不到次数时算进再得次数，换得到时按价格当钱
	prizes, plans := productionBoard()
	lifetime := lifetimeMember()
	draws := RuleOf(Activity{LifetimePolicy: LifetimeDraws, LifetimeDraws: 1}, lifetime.Policy)
	if free := EconomyOf(draws, lifetime, prizes, plans, 0); free.Owned != 0 || math.Abs(free.Redraws-21.0/91) > 0.01 {
		t.Fatalf("积分换不到次数时，改发的次数只是再抽一次，得到 %+v", free)
	}
	economy := EconomyOf(draws, lifetime, prizes, plans, 10)
	if math.Abs(economy.Owned-14.0/91) > 0.01 || math.Abs(economy.OwnedValue-140.0/91) > 0.01 || math.Abs(economy.Redraws-7.0/91) > 0.01 {
		t.Fatalf("10 积分一次时改发的次数按 10 积分计，得到 %+v", economy)
	}
	if !economy.Scaled() {
		t.Fatalf("(8.24 + 1.54)/(1 − 0.077) > 10，应缩减，得到 %+v", economy)
	}
	alt, _ := Swap(draws.WithEconomy(economy), prizes[7], plans)
	if alt.Type != KindPoints || alt.Amount != int64(math.Floor(10*economy.Scale)) || economy.ExpectedReturn(economy.Scale) > 10+1e-9 {
		t.Fatalf("缩减时改发的次数换成按比例的积分，得到 %+v（%+v）", alt, economy)
	}
}

// 每日运气：系数确定（同人同天不变）、人人不同、平均约为 1；只乘真实奖品，兜底奖不变。
func TestFortune(t *testing.T) {
	fortune := &Fortune{Spread: 0.3}
	day := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	if FortuneOf(nil, 1, 7, day) != 1 || FortuneOf(fortune, 1, 0, day) != 1 {
		t.Fatal("没开每日运气、没有用户时系数为 1")
	}
	if FortuneOf(fortune, 1, 7, day) != FortuneOf(fortune, 1, 7, day.Add(15*time.Hour)) {
		t.Fatal("同一个人同一天的运气不变")
	}
	low, high := FortuneRange(fortune)
	seen := map[float64]bool{}
	var sum, lucky float64
	const users = 20000
	for user := int64(1); user <= users; user++ {
		factor := FortuneOf(fortune, 1, user, day)
		if factor < low || factor > high {
			t.Fatalf("系数 %.4f 超出范围 [%.4f, %.4f]", factor, low, high)
		}
		seen[factor] = true
		sum += factor
		if factor > 1.2 {
			lucky++
		}
	}
	if len(seen) < users*9/10 {
		t.Fatalf("每个人的运气应各不相同，%d 人里只有 %d 种", users, len(seen))
	}
	if mean := sum / users; math.Abs(mean-1) > 0.01 {
		t.Fatalf("平均系数应约为 1，得到 %.4f", mean)
	}
	// σ = 0.3：系数 > 1.2 即 z > (ln1.2 + 0.045)/0.3 ≈ 0.76，约 22%
	if share := lucky / users; share < 0.19 || share > 0.26 {
		t.Fatalf("走运（系数 > 1.2）的人约占两成，得到 %.3f", share)
	}
	if days := map[float64]bool{}; true {
		for offset := range 30 {
			days[FortuneOf(fortune, 1, 7, day.AddDate(0, 0, offset))] = true
		}
		if len(days) < 28 {
			t.Fatalf("同一个人每天的运气应不同，30 天里只有 %d 种", len(days))
		}
	}

	prizes, _ := wheelBoard()
	boosted := FortunePrizes(prizes, high)
	base, after := Probabilities(prizes), Probabilities(boosted)
	if after[3] >= base[3] || after[8] <= base[8] {
		t.Fatalf("走运时兜底经验变少、大额积分变多，得到 经验 %.2f→%.2f 188 积分 %.4f→%.4f", base[3], after[3], base[8], after[8])
	}
	if ratio := (after[8] / after[1]) / (base[8] / base[1]); math.Abs(ratio-1) > 1e-4 {
		t.Fatalf("真实奖品之间的相对比例不变，得到 %.6f", ratio)
	}
	if &FortunePrizes(prizes, 1)[0] != &prizes[0] {
		t.Fatal("系数为 1 时不复制奖盘")
	}
}

// 10-28 的全新转盘：月度 / 季度会员、启动画面、AI 生图时长；真实奖品约占 66%，σ = 0.3 时运气最好的永久会员也不套利。
func fortuneBoard() ([]Prize, map[int64]pointstore.PlanRef) {
	plans := map[int64]pointstore.PlanRef{
		14: {ID: 14, Name: "月度会员", DurationDays: 30},
		15: {ID: 15, Name: "季度会员", DurationDays: 125},
	}
	prize := func(position, weight int, reward Reward) Prize {
		return Prize{ID: int64(position + 1), Name: reward.Type, Kind: reward.Type, Reward: reward, Weight: weight, Quantity: -1, Position: position}
	}
	prizes := []Prize{
		prize(0, 20, Reward{Type: KindVipPlan, RefID: 15}),
		prize(1, 23000, Reward{Type: KindPoints, Amount: 8}),
		prize(2, 1500, Reward{Type: KindFeature, Tag: "ai", Amount: 3}),
		prize(3, 33880, Reward{Type: KindExperience, Amount: 25}),
		prize(4, 100, Reward{Type: KindVipPlan, RefID: 14}),
		prize(5, 3000, Reward{Type: KindAdFree, Amount: 2}),
		prize(6, 4000, Reward{Type: KindLotteryDraws, Amount: 2}),
		prize(7, 28000, Reward{Type: KindPoints, Amount: 3}),
		prize(8, 800, Reward{Type: KindVipDays, Amount: 5}),
		prize(9, 3000, Reward{Type: KindPoints, Amount: 50}),
		prize(10, 2500, Reward{Type: KindFeatureTime, Tag: "ai", Amount: 60}),
		prize(11, 200, Reward{Type: KindPoints, Amount: 288}),
	}
	prizes[3].Fallback = true
	return prizes, plans
}

// 运气最好的永久会员会套利时控制台要提示；10-28 的全新转盘在 σ = 0.3 时不会。
func TestAuditEconomyFortune(t *testing.T) {
	prizes, plans := fortuneBoard()
	lifetime := lifetimeMember()
	activity := Activity{LifetimePolicy: LifetimePoints, Strategy: Strategy{Fortune: &Fortune{Spread: 0.3}}}
	audit := AuditEconomy(activity, prizes, plans, lifetime, 10, nil)
	if len(audit.Warnings) != 0 || audit.LifetimeScale != 1 {
		t.Fatalf("σ = 0.3 时不应警告，得到 %+v", audit)
	}
	_, high := FortuneRange(activity.Strategy.Fortune)
	rule := RuleOf(activity, lifetime.Policy)
	lucky := EconomyOf(rule, lifetime, FortunePrizes(PersonalPrizes(rule, lifetime, prizes, plans), high), plans, 10)
	t.Logf("普通用户 %.2f，永久会员 %.2f，运气最好（%.2f）的永久会员 %.2f", audit.MemberReturn, audit.LifetimeReturn, high, lucky.ExpectedReturn(1))
	if lucky.Scaled() || lucky.ExpectedReturn(1) >= 10 {
		t.Fatalf("运气最好的永久会员也应低于价格，得到 %+v", lucky)
	}
	// 之前那张转盘积分奖品多、余量小：运气最好的永久会员会套利，要提示
	tight, tightPlans := wheelBoard()
	if audit := AuditEconomy(activity, tight, tightPlans, lifetime, 10, nil); len(audit.Warnings) != 1 {
		t.Fatalf("余量小的转盘上运气最好的永久会员会套利，应提示，得到 %+v", audit.Warnings)
	}
}

// 模拟器给每个虚拟用户各抽一个运气系数：走运的人真实奖品更多，总体中奖率仍接近公示。
func TestSimulateFortune(t *testing.T) {
	prizes, _ := fortuneBoard()
	strategy := Strategy{Fortune: &Fortune{Spread: 0.3}}
	result := Simulate(Activity{}, strategy, prizes, SimulationInput{Users: 2000, DrawsPerUser: 20, Seed: 7, IsolatedStock: true})
	if result.Fortune == nil || result.Fortune.P50 >= result.Fortune.P90 || math.Abs(result.Fortune.Mean-1) > 0.03 {
		t.Fatalf("虚拟用户的运气应各不相同、平均约为 1，得到 %+v", result.Fortune)
	}
	real := 1 - result.Prizes[3].Rate
	if math.Abs(real-0.6612) > 0.02 {
		t.Fatalf("真实奖品总占比应接近公示的 66.1%%，得到 %.4f", real)
	}
	fixed := Simulate(Activity{}, strategy, prizes, SimulationInput{Users: 10, DrawsPerUser: 5, Seed: 7, FortuneApplied: true})
	if fixed.Fortune != nil {
		t.Fatal("奖盘已经乘过真实用户的运气时不再抽系数")
	}
}
