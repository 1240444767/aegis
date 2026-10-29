package lottery

import (
	"math"
	"strings"
	"testing"
	"time"

	vipdomain "aegis/internal/domain/vip"
)

// 加成公式：乘幅度、抬到保底、压到上限，且永远不低于自然运气；关于自然运气单调不减。
func TestSupremeGradeApply(t *testing.T) {
	grade := SupremeGrade{Boost: 1.5, Floor: 1, Cap: 2}
	cases := []struct{ natural, want float64 }{
		{0.5, 1},    // 0.75 抬到保底 1
		{0.8, 1.2},  // 乘 1.5
		{1.2, 1.8},  // 乘 1.5
		{1.6, 2},    // 2.4 压到上限 2
		{2.5, 2.5},  // 自然运气已高过上限：不被拉低
		{1.0, 1.5},  // 平均水平
		{0.52, 1.0}, // 最倒霉的人也到保底
	}
	for _, c := range cases {
		if got := grade.Apply(c.natural); math.Abs(got-c.want) > 1e-9 {
			t.Fatalf("自然运气 %v 加成后应为 %v，得到 %v", c.natural, c.want, got)
		}
	}
	previous := 0.0
	for natural := 0.3; natural < 3; natural += 0.01 {
		got := grade.Apply(natural)
		if got < natural-1e-9 || got < previous-1e-9 {
			t.Fatalf("加成必须不低于自然运气且单调：f=%v 得到 %v（上一个 %v）", natural, got, previous)
		}
		previous = got
	}
	// 不设上限时按硬上限封顶
	if got := (SupremeGrade{Boost: 3}).Apply(4); got != MaxSupremeFactor {
		t.Fatalf("不设上限时应封顶 %d，得到 %v", MaxSupremeFactor, got)
	}
}

func TestValidateSupreme(t *testing.T) {
	bad := []*Supreme{
		{Enabled: true, Scope: SupremeScopeLifetime},                                 // 没有效果
		{Enabled: true, Scope: SupremeScopeLifetime, Boost: 0.5},                     // 幅度小于 1
		{Enabled: true, Scope: SupremeScopeLifetime, Boost: 4},                       // 幅度超过 3
		{Enabled: true, Scope: SupremeScopeLifetime, Boost: 1.5, Floor: 2, Cap: 1.5}, // 保底高于上限
		{Enabled: true, Scope: SupremeScopeLifetime, Boost: 1.5, Cap: 0.8},           // 上限低于 1
		{Enabled: true, Scope: SupremeScopeLifetime, Boost: 1.5, Floor: 6},           // 保底超过硬上限
		{Enabled: true, Scope: "vip"},                                                // 未知范围
		{Enabled: true, Scope: SupremeScopeTiers},                                    // 分级却一个档位都没有
		{Enabled: true, Scope: SupremeScopeTiers, Tiers: []SupremeTier{{TierID: 1}}}, // 档位没有效果
		{Enabled: true, Scope: SupremeScopeTiers, Tiers: []SupremeTier{{Boost: 2}}},  // 没选档位
		{Enabled: true, Scope: SupremeScopeLifetime, Boost: 2, Title: strings.Repeat("至", MaxSupremeTitleRunes+1)},
	}
	for index, supreme := range bad {
		if err := ValidateSupreme(NormalizeSupreme(supreme)); err == nil {
			t.Fatalf("第 %d 个配置应被拒：%+v", index+1, supreme)
		}
	}
	good := []*Supreme{
		nil,
		{Enabled: false, Scope: SupremeScopeLifetime}, // 关闭时参数可以留空
		{Enabled: true, Boost: 1.3},                   // 空范围按永久会员
		{Enabled: true, Scope: SupremeScopeLifetime, Floor: 1},
		{Enabled: true, Scope: SupremeScopeTiers, Tiers: []SupremeTier{{TierID: 3, Boost: 1.2}, {TierID: 4, Boost: 1.5, Floor: 1, Cap: 2.5}}},
	}
	for index, supreme := range good {
		if err := ValidateSupreme(NormalizeSupreme(supreme)); err != nil {
			t.Fatalf("第 %d 个配置应通过：%v", index+1, err)
		}
	}
	normalized := NormalizeSupreme(&Supreme{Enabled: true, Boost: 1, Floor: 1, Tiers: []SupremeTier{{TierID: 1, Boost: 2}}})
	if normalized.Scope != SupremeScopeLifetime || normalized.Boost != 0 || normalized.Tiers != nil {
		t.Fatalf("清洗不对：%+v", normalized)
	}
	deduped := NormalizeSupreme(&Supreme{Scope: SupremeScopeTiers, Tiers: []SupremeTier{{TierID: 1, Boost: 2}, {TierID: 1, Boost: 1.5}}})
	if len(deduped.Tiers) != 1 || deduped.Tiers[0].Boost != 1.5 {
		t.Fatalf("同一档位应只留最后一条，得到 %+v", deduped.Tiers)
	}
}

// 活动写了就以活动为准（写了关闭即不加成），没写跟随应用；没开每日运气时一律没有。
func TestEffectiveSupreme(t *testing.T) {
	app := &Supreme{Enabled: true, Scope: SupremeScopeLifetime, Boost: 1.5}
	own := &Supreme{Enabled: true, Scope: SupremeScopeLifetime, Boost: 2}
	if got, source := EffectiveSupreme(&Fortune{Spread: 0.3}, app); got != app || source != SupremeSourceApp {
		t.Fatalf("没写时应跟随应用，得到 %+v %s", got, source)
	}
	if got, source := EffectiveSupreme(&Fortune{Spread: 0.3, Supreme: own}, app); got != own || source != SupremeSourceActivity {
		t.Fatalf("活动单独设置应优先，得到 %+v %s", got, source)
	}
	if got, source := EffectiveSupreme(&Fortune{Spread: 0.3, Supreme: &Supreme{Scope: SupremeScopeLifetime}}, app); got != nil || source != SupremeSourceActivity {
		t.Fatalf("活动单独关闭时不应有加成，得到 %+v %s", got, source)
	}
	if got, _ := EffectiveSupreme(nil, app); got != nil {
		t.Fatal("没开每日运气时不应有加成")
	}
	if got, _ := EffectiveSupreme(&Fortune{Spread: 0.3}, &Supreme{Scope: SupremeScopeLifetime, Boost: 2}); got != nil {
		t.Fatal("应用设置关闭时不应有加成")
	}
}

// 只对永久会员：判据是 Holdings.Lifetime；按档位分级：判据是 Holdings.Tiers，取对他最好的一档。
func TestSupremeGrades(t *testing.T) {
	lifetime := vipdomain.Holdings{Lifetime: true, Tiers: []vipdomain.HeldTier{{ID: 7, Name: "至尊", Lifetime: true}}}
	timed := vipdomain.Holdings{Tiers: []vipdomain.HeldTier{{ID: 8, Name: "高级"}}}
	plain := vipdomain.Holdings{}

	only := &Supreme{Enabled: true, Scope: SupremeScopeLifetime, Boost: 1.5}
	if len(SupremeGrades(only, lifetime)) != 1 || len(SupremeGrades(only, timed)) != 0 || len(SupremeGrades(only, plain)) != 0 {
		t.Fatal("只对永久会员时，限时会员与普通用户不应有加成")
	}
	if ResolveSupreme(only, plain, 0.8) != nil {
		t.Fatal("普通用户的运气不应被改动")
	}

	tiers := &Supreme{Enabled: true, Scope: SupremeScopeTiers, Boost: 1.1, Tiers: []SupremeTier{
		{TierID: 7, Boost: 1.5, Cap: 2},
		{TierID: 8, Boost: 1.2},
		{TierID: 9, Floor: 1.5, LifetimeOnly: true},
	}}
	if grades := SupremeGrades(tiers, timed); len(grades) != 1 || grades[0].TierID != 8 || grades[0].TierName != "高级" {
		t.Fatalf("限时会员应按所在档位加成，得到 %+v", grades)
	}
	timedInLifetimeOnly := vipdomain.Holdings{Tiers: []vipdomain.HeldTier{{ID: 9}}}
	if grades := SupremeGrades(tiers, timedInLifetimeOnly); len(grades) != 0 {
		t.Fatalf("只认永久开通的档位不应给限时会员，得到 %+v", grades)
	}
	orphan := vipdomain.Holdings{Lifetime: true}
	if grades := SupremeGrades(tiers, orphan); len(grades) != 1 || grades[0].TierID != 0 || grades[0].Boost != 1.1 {
		t.Fatalf("没落在任何一档的永久会员应按顶层参数，得到 %+v", grades)
	}
	// 同时在两档：取对他今天最好的那一档
	both := vipdomain.Holdings{Lifetime: true, Tiers: []vipdomain.HeldTier{{ID: 7, Lifetime: true}, {ID: 9, Lifetime: true}}}
	if outcome := ResolveSupreme(tiers, both, 0.6); outcome == nil || outcome.Grade.TierID != 9 || outcome.Factor != 1.5 {
		t.Fatalf("倒霉的那天保底 1.5 更好，得到 %+v", outcome)
	}
	if outcome := ResolveSupreme(tiers, both, 1.2); outcome == nil || outcome.Grade.TierID != 7 || outcome.Factor != 1.8 {
		t.Fatalf("走运的那天 ×1.5 更好，得到 %+v", outcome)
	}
}

// 范围与积分账：拿到加成的人的范围两端各套一次；永久会员最走运的系数取所有参数组的最高。
func TestSupremeRange(t *testing.T) {
	fortune := &Fortune{Spread: 0.3}
	low, high := FortuneRange(fortune)
	grade := SupremeGrade{Boost: 1.5, Floor: 1, Cap: 2}
	gotLow, gotHigh := SupremeRange(fortune, grade)
	if gotLow != grade.Apply(low) || gotHigh != 2 {
		t.Fatalf("范围应为 [%v, 2]，得到 [%v, %v]", grade.Apply(low), gotLow, gotHigh)
	}
	if SupremeHigh(fortune, nil) != high {
		t.Fatal("没有加成时积分账按自然运气最高")
	}
	tiers := &Supreme{Enabled: true, Scope: SupremeScopeTiers, Tiers: []SupremeTier{{TierID: 1, Boost: 1.2}, {TierID: 2, Boost: 2, Cap: 3}}}
	if got := SupremeHigh(fortune, tiers); math.Abs(got-math.Min(high*2, 3)) > 1e-6 {
		t.Fatalf("应取各档最高，得到 %v", got)
	}
}

// 用户端：系数、档位、超过多少人换成加成之后的，加成之前的在 supreme 里；系数与出奖用的一致。
func TestApplySupremeReading(t *testing.T) {
	fortune := &Fortune{Spread: 0.3}
	day := time.Date(2026, 10, 29, 0, 0, 0, 0, time.FixedZone("CST", 8*3600))
	supreme := &Supreme{Enabled: true, Scope: SupremeScopeLifetime, Boost: 1.5, Floor: 1}
	lifetime := vipdomain.Holdings{Lifetime: true}
	var seen bool
	for user := int64(1); user <= 2000; user++ {
		reading := ReadFortune(fortune, 3, user, day)
		outcome := ResolveSupreme(supreme, lifetime, reading.Factor)
		lifted := ApplySupremeReading(reading, fortune, supreme, outcome)
		if lifted.Factor < reading.Factor || lifted.Factor < 1 || lifted.Supreme == nil {
			t.Fatalf("加成后的运气应不低于原值与保底：%+v → %+v", reading, lifted)
		}
		if lifted.Supreme.BaseFactor != reading.Factor || lifted.Supreme.BaseLevel != reading.Level ||
			lifted.Supreme.Title != DefaultSupremeTitle || !strings.Contains(lifted.Supreme.Message, "永久会员至尊加成") {
			t.Fatalf("加成说明不对：%+v", lifted.Supreme)
		}
		if lifted.Percentile < reading.Percentile || lifted.Factor < lifted.Low || lifted.Factor > lifted.High {
			t.Fatalf("百分位与范围不对：%+v", lifted)
		}
		if reading.Level == FortuneQuiet && lifted.Level != FortuneQuiet {
			seen = true
		}
		if strings.Contains(lifted.Supreme.Message, "·") {
			t.Fatal("文案不用「·」")
		}
	}
	if !seen {
		t.Fatal("保底应把手气平淡的人抬出最低档")
	}
	if ApplySupremeReading(nil, fortune, supreme, nil) != nil {
		t.Fatal("没开每日运气时仍为 nil")
	}
}

// 生效点：加成作用在同一个 FortunePrizes 上，兜底奖不乘，概率照常归一、真实奖品占比升高。
func TestSupremeOnBoard(t *testing.T) {
	prizes := []Prize{
		{ID: 1, Weight: 60, Quantity: -1, Kind: KindExperience, Fallback: true},
		{ID: 2, Weight: 30, Quantity: -1, Kind: KindPoints},
		{ID: 3, Weight: 10, Quantity: -1, Kind: KindVipDays},
	}
	natural := 0.9
	lifted := SupremeGrade{Boost: 1.5}.Apply(natural)
	plain, boosted := FortunePrizes(prizes, natural), FortunePrizes(prizes, lifted)
	sum := 0.0
	for _, p := range Probabilities(boosted) {
		sum += p
	}
	if math.Abs(sum-100) > 1e-6 {
		t.Fatalf("加成后的概率应归一，合计 %v", sum)
	}
	if RealPrizeShare(boosted) <= RealPrizeShare(plain) {
		t.Fatal("加成后真实奖品的占比应升高")
	}
	if boosted[0].Weight != plain[0].Weight {
		t.Fatal("兜底奖的权重不应被加成")
	}
	// 真实奖品之间的比例不变
	if math.Abs(float64(boosted[1].Weight)/float64(boosted[2].Weight)-3) > 0.01 {
		t.Fatal("真实奖品之间的相对比例不应改变")
	}
}

// 虚拟用户模拟：每人按自己的自然运气取加成，系数分布整体抬高，加成之前的分布另给。
func TestSimulateSupreme(t *testing.T) {
	activity := Activity{ID: 1, FreeDaily: 1000}
	strategy := Strategy{Fortune: &Fortune{Spread: 0.3}}
	prizes := []Prize{
		{ID: 1, Weight: 70, Quantity: -1, Kind: KindExperience, Reward: Reward{Type: KindExperience, Amount: 5}, Fallback: true},
		{ID: 2, Weight: 30, Quantity: -1, Kind: KindPoints, Reward: Reward{Type: KindPoints, Amount: 3}},
	}
	input := SimulationInput{Users: 400, DrawsPerUser: 50, Seed: 42, Now: time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)}
	plain := Simulate(activity, strategy, prizes, input)
	input.Supreme = []SupremeGrade{{Boost: 1.5, Floor: 1}}
	lifted := Simulate(activity, strategy, prizes, input)
	if lifted.Fortune == nil || lifted.FortuneBase == nil || plain.FortuneBase != nil {
		t.Fatalf("有加成时应同时给出加成前后的分布：%+v %+v", lifted.Fortune, lifted.FortuneBase)
	}
	if lifted.Fortune.Mean <= lifted.FortuneBase.Mean || lifted.FortuneBase.Mean != plain.Fortune.Mean {
		t.Fatalf("加成后的平均系数应更高，且加成前与不加成一致：%v %v %v", lifted.Fortune.Mean, lifted.FortuneBase.Mean, plain.Fortune.Mean)
	}
	if lifted.Prizes[1].Rate <= plain.Prizes[1].Rate {
		t.Fatalf("加成后积分奖的出现比例应更高：%v ≤ %v", lifted.Prizes[1].Rate, plain.Prizes[1].Rate)
	}
}
