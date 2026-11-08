package rewardedad

import (
	"strings"
	"testing"

	cardkeydomain "aegis/internal/domain/cardkey"
	vipdomain "aegis/internal/domain/vip"
)

func lifetimeScene(rule SceneLifetime, rewards ...Reward) Scene {
	return NormalizeScene(Scene{Key: "s", Name: "场景", PlacementID: "p", Enabled: true, Rewards: rewards, Lifetime: rule})
}

func TestResolveConvertsOwnedRewardsByValue(t *testing.T) {
	lifetime := Coverage{Holdings: vipdomain.Holdings{Lifetime: true, LifetimeAdFree: true}}
	plans := map[int64]vipdomain.Plan{
		7: {ID: 7, Name: "周卡", Kind: vipdomain.KindPaid, DurationDays: 7, BonusIntegral: 5},
	}
	scene := lifetimeScene(SceneLifetime{},
		Reward{Type: cardkeydomain.RewardVipDays, Amount: 2},
		Reward{Type: cardkeydomain.RewardVipPlan, RefID: 7},
		Reward{Type: cardkeydomain.RewardExperience, Amount: 3},
		Reward{Type: RewardFeatureMinutes, Feature: "music", Amount: 30},
	)
	out := Resolve(scene, lifetime, plans, Labels{Plans: map[int64]string{7: "周卡"}, Features: map[string]string{"music": "听歌"}})
	if out.Hidden || out.Conversion == nil {
		t.Fatalf("永久会员应当折算：%+v", out)
	}
	// 2 天 + 7 天 = 9 天 × 10 = 90；30 分钟 × 10/小时 = 5；共 95，再加套餐附赠 5 积分
	var integral int64
	experience := false
	for _, grant := range out.Grants {
		switch grant.Reward.Type {
		case cardkeydomain.RewardIntegral:
			if !grant.Converted {
				t.Fatal("折算出的积分要标成 converted")
			}
			integral += grant.Reward.Amount
		case cardkeydomain.RewardExperience:
			experience = !grant.Converted && grant.Reward.Amount == 3
		default:
			t.Fatalf("已拥有的权益不应再发：%+v", grant)
		}
	}
	if integral != 100 || !experience {
		t.Fatalf("折算结果不对：integral=%d experience=%v grants=%+v", integral, experience, out.Grants)
	}
	if len(out.Conversion.Owned) != 3 || out.Conversion.Reason != OwnedLifetime ||
		!strings.Contains(out.Conversion.Message, "你已是永久会员") || !strings.Contains(out.Conversion.Message, "周卡") {
		t.Fatalf("折算说明不对：%+v", out.Conversion)
	}
	if hint := out.ClientHint(); hint == nil || !strings.Contains(hint.Message, "将折算为") {
		t.Fatalf("客户端提示不对：%+v", hint)
	}
}

func TestResolveKeepsUpgradesAndNonMembers(t *testing.T) {
	scene := lifetimeScene(SceneLifetime{}, Reward{Type: cardkeydomain.RewardVipDays, Amount: 1})
	scene.Membership.Features = []string{"export"}

	// 非永久会员：照常发放
	if out := Resolve(scene, Coverage{}, nil, Labels{}); out.Conversion != nil || len(out.Grants) != 1 {
		t.Fatalf("非永久会员照常发放：%+v", out)
	}
	// 永久会员没有 export：这一天会员是实打实的升级，照发
	if out := Resolve(scene, Coverage{Holdings: vipdomain.Holdings{Lifetime: true, LifetimeFeatures: []string{"ai"}, LifetimeAdFree: true}}, nil, Labels{}); out.Conversion != nil {
		t.Fatalf("带永久会员没有的功能时不算已拥有：%+v", out)
	}
	// 已包含 export：折算
	if out := Resolve(scene, Coverage{Holdings: vipdomain.Holdings{Lifetime: true, LifetimeFeatures: []string{"export"}, LifetimeAdFree: true}}, nil, Labels{}); out.Conversion == nil {
		t.Fatal("功能都已包含时应当折算")
	}
	// 套餐带永久会员没有的功能：照发
	plans := map[int64]vipdomain.Plan{3: {ID: 3, Kind: vipdomain.KindPaid, DurationDays: 30, Features: []string{"pro"}}}
	planScene := lifetimeScene(SceneLifetime{}, Reward{Type: cardkeydomain.RewardVipPlan, RefID: 3})
	if out := Resolve(planScene, Coverage{Holdings: vipdomain.Holdings{Lifetime: true, LifetimeAdFree: true}}, plans, Labels{}); out.Conversion != nil {
		t.Fatalf("高级套餐对永久基础会员是升级：%+v", out)
	}
	// 已永久解锁功能的非会员：功能时长折算，原因是 feature
	minutes := lifetimeScene(SceneLifetime{}, Reward{Type: RewardFeatureMinutes, Feature: "music", Amount: 60})
	out := Resolve(minutes, Coverage{Holdings: vipdomain.Holdings{PermanentUnlocks: []string{"music"}}}, nil, Labels{})
	if out.Conversion == nil || out.Conversion.Reason != OwnedFeature || len(out.Grants) != 1 || out.Grants[0].Reward.Amount != 10 {
		t.Fatalf("永久解锁的功能时长应当折算：%+v", out)
	}
}

func TestResolvePolicies(t *testing.T) {
	lifetime := Coverage{Holdings: vipdomain.Holdings{Lifetime: true, LifetimeAdFree: true}}
	days := Reward{Type: cardkeydomain.RewardVipDays, Amount: 3}

	grant := lifetimeScene(SceneLifetime{Policy: LifetimeGrant}, days)
	if out := Resolve(grant, lifetime, nil, Labels{}); out.Conversion != nil || len(out.Grants) != 1 || out.Grants[0].Converted {
		t.Fatalf("grant 照常发放：%+v", out)
	}

	replace := lifetimeScene(SceneLifetime{Policy: LifetimeReplace, Rewards: []Reward{
		{Type: cardkeydomain.RewardLotteryDraws, Amount: 2}, {Type: RewardAICredit, Amount: 50},
	}}, days)
	out := Resolve(replace, lifetime, nil, Labels{})
	if out.Conversion == nil || len(out.Grants) != 2 || !out.Grants[0].Converted {
		t.Fatalf("replace 改发固定权益：%+v", out)
	}

	hide := lifetimeScene(SceneLifetime{Policy: LifetimeHide, Target: RewardAICredit, PerVipDay: 4}, days)
	out = Resolve(hide, lifetime, nil, Labels{})
	if !out.Hidden || len(out.Grants) != 1 || out.Grants[0].Reward.Type != RewardAICredit || out.Grants[0].Reward.Amount != 12 {
		t.Fatalf("hide 隐藏、到达时按价值折到 AI 额度：%+v", out)
	}
	if out := Resolve(hide, Coverage{}, nil, Labels{}); out.Hidden {
		t.Fatal("没有已拥有的权益时不隐藏")
	}

	// 只配了功能时长的折算率：会员天数折出 0，什么也不发，说明「已包含」
	zero := lifetimeScene(SceneLifetime{PerFeatureHour: 5}, days)
	out = Resolve(zero, lifetime, nil, Labels{})
	if out.Conversion == nil || len(out.Grants) != 0 || !strings.Contains(out.Conversion.Message, "无需重复发放") {
		t.Fatalf("折出 0 时只说明不发放：%+v", out)
	}

	// 折算有上界：不超过去处那一档的上限
	huge := lifetimeScene(SceneLifetime{Target: RewardAICredit, PerVipDay: MaxPerVipDay},
		Reward{Type: cardkeydomain.RewardVipDays, Amount: 3650})
	out = Resolve(huge, lifetime, nil, Labels{})
	if out.Grants[0].Reward.Amount != MaxAICreditPoints {
		t.Fatalf("折算应截在上限：%+v", out.Grants)
	}
}

func TestLifetimeNormalizeAndValidate(t *testing.T) {
	scene := lifetimeScene(SceneLifetime{}, Reward{Type: cardkeydomain.RewardVipDays, Amount: 1})
	if scene.Lifetime.Policy != LifetimeConvert || scene.Lifetime.Target != cardkeydomain.RewardIntegral ||
		scene.Lifetime.PerVipDay != DefaultPerVipDay || scene.Lifetime.PerFeatureHour != DefaultPerFeatureHour {
		t.Fatalf("未设置时补默认值：%+v", scene.Lifetime)
	}
	if err := ValidateScenes([]Scene{scene}); err != nil {
		t.Fatal(err)
	}
	bad := []SceneLifetime{
		{Policy: "nope"},
		{Policy: LifetimeConvert, Target: cardkeydomain.RewardVipDays},
		{Policy: LifetimeConvert, PerVipDay: -1, PerFeatureHour: 1},
		{Policy: LifetimeReplace},
		{Policy: LifetimeReplace, Rewards: []Reward{{Type: cardkeydomain.RewardVipDays, Amount: 1}}},
		{Policy: LifetimeReplace, Rewards: []Reward{{Type: cardkeydomain.RewardIntegral, Amount: 0}}},
	}
	for _, rule := range bad {
		if err := ValidateScenes([]Scene{lifetimeScene(rule, Reward{Type: cardkeydomain.RewardVipDays, Amount: 1})}); err == nil {
			t.Fatalf("应当拒绝：%+v", rule)
		}
	}
	// grant 清掉无关字段
	cleared := lifetimeScene(SceneLifetime{Policy: LifetimeGrant, Target: "x", PerVipDay: 3}, Reward{Type: cardkeydomain.RewardIntegral, Amount: 1})
	if l := cleared.Lifetime; l.Policy != LifetimeGrant || l.Target != "" || l.PerVipDay != 0 || l.Rewards != nil {
		t.Fatalf("grant 只留处理方式：%+v", cleared.Lifetime)
	}
}
