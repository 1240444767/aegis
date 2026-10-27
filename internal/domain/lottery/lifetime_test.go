package lottery

import (
	vipdomain "aegis/internal/domain/vip"

	"testing"

	"aegis/internal/domain/pointstore"
)

// rates 折算单价：会员与限时功能每天 perDay、功能时长每小时 10、永久 lifetime。
func rates(perDay, lifetime int64) vipdomain.ConversionPolicy {
	policy := vipdomain.DefaultConversionPolicy(1)
	policy.PerVipDay, policy.PerFeatureDay, policy.PerFeatureHour, policy.PerLifetime = perDay, perDay, 10, lifetime
	return policy
}

func TestOwnedAndSwap(t *testing.T) {
	plans := map[int64]pointstore.PlanRef{
		1: {ID: 1, Name: "月卡", DurationDays: 30, Features: []string{"music"}},
		2: {ID: 2, Name: "永久高级版", Lifetime: true, Features: []string{"music", "export"}},
	}
	lifetime := MemberCoverage{Holdings: vipdomain.Holdings{Lifetime: true, LifetimeFeatures: []string{"music"}, LifetimeAdFree: true}}
	none := MemberCoverage{}
	unlocked := MemberCoverage{Holdings: vipdomain.Holdings{PermanentUnlocks: []string{"export"}}}
	rule := SwapRule{Policy: LifetimePoints, Value: rates(10, 3650), Draws: 2}

	cases := []struct {
		name     string
		coverage MemberCoverage
		reward   Reward
		owned    bool
		points   int64
	}{
		{"永久会员抽中会员天数", lifetime, Reward{Type: KindVipDays, Amount: 7}, true, 70},
		{"永久会员抽中已含的月卡", lifetime, Reward{Type: KindVipPlan, RefID: 1}, true, 300},
		{"永久会员抽中含新功能的永久版是升级", lifetime, Reward{Type: KindVipPlan, RefID: 2}, false, 0},
		{"永久会员已含的功能", lifetime, Reward{Type: KindFeature, Tag: "music", Amount: 3}, true, 30},
		{"永久会员未含的功能", lifetime, Reward{Type: KindFeature, Tag: "export", Amount: 3}, false, 0},
		{"已永久解锁的功能（永久奖品）", unlocked, Reward{Type: KindFeature, Tag: "export"}, true, 3650},
		{"普通用户抽中会员", none, Reward{Type: KindVipDays, Amount: 7}, false, 0},
		{"积分从来不算已拥有", lifetime, Reward{Type: KindPoints, Amount: 7}, false, 0},
	}
	for _, tc := range cases {
		_, owned := tc.coverage.Owned(tc.reward, plans)
		if owned != tc.owned {
			t.Fatalf("%s：owned=%v，期望 %v", tc.name, owned, tc.owned)
		}
		if !owned {
			continue
		}
		alt, ok := Swap(rule, Prize{Kind: tc.reward.Type, Reward: tc.reward}, plans)
		if !ok || alt.Type != KindPoints || alt.Amount != tc.points {
			t.Fatalf("%s：折算 %+v，期望 %d 积分", tc.name, alt, tc.points)
		}
	}

	prize := Prize{Kind: KindVipDays, Reward: Reward{Type: KindVipDays, Amount: 7}, Weight: 5}
	if alt, ok := Swap(SwapRule{Policy: LifetimeDraws, Draws: 2}, prize, plans); !ok || alt.Type != KindLotteryDraws || alt.Amount != 2 {
		t.Fatalf("改发抽奖次数，得到 %+v", alt)
	}
	if _, ok := Swap(SwapRule{Policy: LifetimeGrant}, prize, plans); ok {
		t.Fatal("照常发放时不改发")
	}
	if alt, ok := Swap(SwapRule{Policy: LifetimeExclude, Value: rates(10, 0)}, prize, plans); !ok || alt.Amount != 70 {
		t.Fatalf("剔除模式下走到发放（保底）按积分折算，得到 %+v", alt)
	}
	prize.LifetimeReward = &Reward{Type: KindExperience, Amount: 50}
	if alt, ok := Swap(rule, prize, plans); !ok || alt.Type != KindExperience || alt.Amount != 50 {
		t.Fatalf("奖品自己的改发优先，得到 %+v", alt)
	}
	huge := Prize{Kind: KindVipDays, Reward: Reward{Type: KindVipDays, Amount: 1 << 40}}
	if alt, _ := Swap(SwapRule{Policy: LifetimePoints, Value: rates(MaxLifetimePointsPerDay, 0)}, huge, plans); alt.Amount != MaxSwapPoints {
		t.Fatalf("折算积分封顶，得到 %d", alt.Amount)
	}

	exclude := SwapRule{Policy: LifetimeExclude, Value: rates(10, 0)}
	prizes := []Prize{{Kind: KindNone, Reward: Reward{Type: KindNone}, Weight: 1, Quantity: -1},
		{Kind: KindVipDays, Reward: Reward{Type: KindVipDays, Amount: 7}, Weight: 3, Quantity: -1}}
	personal := PersonalPrizes(exclude, lifetime, prizes, plans)
	if personal[1].Weight != 0 || prizes[1].Weight != 3 {
		t.Fatalf("剔除只改这个人眼里的奖盘，得到 %+v / %+v", personal, prizes)
	}
	if p := Probabilities(personal); p[0] != 100 || p[1] != 0 {
		t.Fatalf("剔除后概率重算，得到 %v", p)
	}
	if swap := DescribeSwap(exclude, lifetime, prizes[1], plans); swap == nil || swap.Action != SwapActionExcluded {
		t.Fatalf("剔除的奖品标出不会抽到，得到 %+v", swap)
	}
	if notice := NoticeOf(rule, none, prizes, plans); notice != nil {
		t.Fatalf("普通用户没有提示，得到 %+v", notice)
	}
}

func TestValidateLifetime(t *testing.T) {
	base := func() ActivityInput {
		prizes := make([]PrizeInput, 0, GridPrizeCount)
		for position := 0; position < GridPrizeCount; position++ {
			prizes = append(prizes, PrizeInput{Position: position, Reward: Reward{Type: KindNone}, Weight: 1, Quantity: -1})
		}
		prizes[1].Reward = Reward{Type: KindVipDays, Amount: 7}
		return ActivityInput{Name: "测试", UIMode: UIModeGrid, Status: StatusDraft, PityPosition: -1, Prizes: prizes}
	}
	ok := NormalizeInput(base())
	if err := ValidateInput(ok); err != nil || ok.LifetimePolicy != LifetimePoints || ok.LifetimePointsPerDay != 0 || ok.LifetimePointsLifetime != 0 {
		t.Fatalf("默认折算积分、单价跟随应用策略，得到 %+v %v", ok, err)
	}
	bad := base()
	bad.Prizes[0].LifetimeReward = &Reward{Type: KindPoints, Amount: 1}
	if err := ValidateInput(NormalizeInput(bad)); err == nil {
		t.Fatal("谢谢参与不能设改发")
	}
	bad = base()
	bad.Prizes[1].LifetimeReward = &Reward{Type: KindManual, Text: "x"}
	if err := ValidateInput(NormalizeInput(bad)); err == nil {
		t.Fatal("改发不能是人工发放")
	}
	bad = base()
	bad.Prizes[1].LifetimeReward = &Reward{Type: ""}
	if err := ValidateInput(NormalizeInput(bad)); err != nil {
		t.Fatalf("空的改发视为未设置：%v", err)
	}
	bad = base()
	bad.LifetimePolicy, bad.LifetimePointsPerDay, bad.LifetimePointsLifetime = LifetimePoints, -1, 0
	if err := ValidateInput(NormalizeInput(bad)); err == nil {
		t.Fatal("负的折算积分应被拒")
	}
}

// 折算单价跟随应用的会员折算策略；活动填了单价时以活动为准。
func TestRuleOfFollowsAppPolicy(t *testing.T) {
	policy := vipdomain.DefaultConversionPolicy(1) // 会员 10/天、功能 5/天、时长 10/小时、永久 3650
	lifetime := MemberCoverage{Holdings: vipdomain.Holdings{Lifetime: true, LifetimeAdFree: true}, Policy: policy}
	follow := RuleOf(Activity{LifetimePolicy: LifetimePoints}, lifetime.Policy)
	cases := []struct {
		reward Reward
		points int64
	}{
		{Reward{Type: KindVipDays, Amount: 7}, 70},
		{Reward{Type: KindFeature, Tag: "music", Amount: 4}, 20},
		{Reward{Type: KindFeature, Tag: "music"}, 3650},
		{Reward{Type: KindAdFree, Amount: 2}, 10},
		{Reward{Type: KindFeatureTime, Tag: "music", Amount: 90}, 15},
		{Reward{Type: KindVipLifetime}, 3650},
	}
	for _, tc := range cases {
		alt, ok := Swap(follow, Prize{Kind: tc.reward.Type, Reward: tc.reward}, nil)
		if !ok || alt.Type != KindPoints || alt.Amount != tc.points {
			t.Fatalf("%s 跟随应用策略应折 %d，得到 %+v", tc.reward.Type, tc.points, alt)
		}
	}
	custom := RuleOf(Activity{LifetimePolicy: LifetimePoints, LifetimePointsPerDay: 20, LifetimePointsLifetime: 100}, policy)
	if alt, _ := Swap(custom, Prize{Reward: Reward{Type: KindVipDays, Amount: 7}}, nil); alt.Amount != 140 {
		t.Fatalf("活动单价覆盖会员每天，得到 %+v", alt)
	}
	if alt, _ := Swap(custom, Prize{Reward: Reward{Type: KindVipLifetime}}, nil); alt.Amount != 100 {
		t.Fatalf("活动单价覆盖永久，得到 %+v", alt)
	}
	if alt, _ := Swap(custom, Prize{Reward: Reward{Type: KindFeatureTime, Tag: "music", Amount: 60}}, nil); alt.Amount != 10 {
		t.Fatalf("功能时长始终按应用每小时单价，得到 %+v", alt)
	}
}

// 看广告领的永久会员不免广告：抽中会员天数、免广告时长是实打实的，不算已拥有。
func TestOwnedRespectsLifetimeAdFree(t *testing.T) {
	adLifetime := MemberCoverage{Holdings: vipdomain.Holdings{Lifetime: true}}
	for _, reward := range []Reward{{Type: KindVipDays, Amount: 7}, {Type: KindAdFree, Amount: 7}, {Type: KindVipLifetime}} {
		if _, owned := adLifetime.Owned(reward, nil); owned {
			t.Fatalf("不免广告的永久会员抽中 %s 不算已拥有", reward.Type)
		}
	}
	// 月卡的功能全被永久开通与永久解锁覆盖：已拥有
	plans := map[int64]pointstore.PlanRef{1: {ID: 1, DurationDays: 30, Features: []string{"export", "ai"}}}
	covered := MemberCoverage{Holdings: vipdomain.Holdings{
		Lifetime: true, LifetimeAdFree: true, LifetimeFeatures: []string{"export"}, PermanentUnlocks: []string{"ai"},
	}}
	if _, owned := covered.Owned(Reward{Type: KindVipPlan, RefID: 1}, plans); !owned {
		t.Fatal("永久开通加永久解锁已覆盖的月卡应算已拥有")
	}
}
