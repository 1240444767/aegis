package vip

import (
	"slices"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func holdingsPlan(lifetime bool, features ...string) Plan {
	days := 30
	if lifetime {
		days = 0
	}
	return Plan{Kind: KindPaid, Lifetime: lifetime, DurationDays: days, Features: features, Price: decimal.NewFromInt(30)}
}

func TestHoldingsCoversPlan(t *testing.T) {
	lifetime := func(features ...string) Holdings {
		return Holdings{Lifetime: true, LifetimeAdFree: true, LifetimeFeatures: features}
	}
	catalog := func(h Holdings, tags ...string) Holdings {
		h.Catalog, h.CatalogKnown = tags, true
		return h
	}
	cases := []struct {
		name     string
		holdings Holdings
		plan     Plan
		want     bool
	}{
		{"不是永久会员：什么都能买", Holdings{PermanentUnlocks: []string{"export"}}, holdingsPlan(false, "export"), false},
		{"永久会员续同档月卡：已包含", lifetime("export"), holdingsPlan(false, "export"), true},
		{"同一个永久套餐买第二次：已包含", lifetime("export"), holdingsPlan(true, "Export "), true},
		{"永久会员升级到含新功能的永久套餐：放行", lifetime("export"), holdingsPlan(true, "ai.chat", "export"), false},
		{"永久基础版买一段限时高级版：放行", lifetime("export"), holdingsPlan(false, "ai.chat"), false},
		{"不带功能的套餐对永久会员没有意义", lifetime(), holdingsPlan(false), true},
		{"试用对永久会员一律视为已包含", lifetime(), Plan{Kind: KindTrial, DurationDays: 7, Features: []string{"ai.chat"}}, true},
		{"永久开通加永久解锁覆盖月卡：已包含",
			Holdings{Lifetime: true, LifetimeAdFree: true, LifetimeFeatures: []string{"export"}, PermanentUnlocks: []string{"ai.chat"}},
			holdingsPlan(false, "export", "ai.chat"), true},
		{"月卡里停用的功能不算差别", catalog(lifetime("export"), "export"), holdingsPlan(false, "export", "legacy"), true},
		{"永久套餐里停用的功能仍算差别", catalog(lifetime("export"), "export"), holdingsPlan(true, "export", "legacy"), false},
		{"目录未知时不过滤", lifetime("export"), holdingsPlan(false, "export", "legacy"), false},
		{"看广告领的永久会员不免广告：月卡多出免广告",
			Holdings{Lifetime: true, LifetimeFeatures: []string{"export"}}, holdingsPlan(false, "export"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.holdings.CoversPlan(tc.plan); got != tc.want {
				t.Fatalf("CoversPlan = %v，期望 %v（gain %+v）", got, tc.want, tc.holdings.GainOf(PlanBenefit(tc.plan)))
			}
		})
	}
}

func TestHoldingsGainNamesWhatIsNew(t *testing.T) {
	holdings := Holdings{Lifetime: true, LifetimeFeatures: []string{"export"}, Catalog: []string{"export", "ai", "music"}, CatalogKnown: true}
	gain := holdings.GainOf(PlanBenefit(holdingsPlan(false, "music", "export", "ai", "gone")))
	if !slices.Equal(gain.Features, []string{"ai", "music"}) || !gain.AdFree || gain.Membership {
		t.Fatalf("新增的应是启用中的 ai、music 与免广告，得到 %+v", gain)
	}
	if gain := (Holdings{}).GainOf(MembershipBenefit(nil, false, true)); !gain.Membership || gain.Empty() {
		t.Fatalf("不是永久会员时会员身份本身就是新增，得到 %+v", gain)
	}
}

func TestHoldingsOfMatchesEvaluate(t *testing.T) {
	now := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	since := now.Add(-time.Hour)
	planID := int64(3)
	falseValue := false
	in := EvalInput{
		LifetimeSince:  &since,
		FeatureCatalog: []string{"export", "ai"},
		Segments: []Segment{
			// 永久开通：套餐当前配置带 export
			{ID: 1, Channel: ChannelAdminGrant, PlanID: &planID, Plan: &SegmentPlan{Name: "永久版", Features: []string{"export"}}, Lifetime: true},
			// 限时段不进永久持有
			{ID: 2, Channel: ChannelWallet, Features: []string{"ai"}, ActiveFrom: now.Add(-time.Hour), ActiveUntil: now.Add(time.Hour)},
			// 已结束的永久之外的段不算
			{ID: 3, Channel: ChannelWallet, Features: []string{"music"}, ActiveFrom: now.Add(-2 * time.Hour), ActiveUntil: now.Add(-time.Hour)},
		},
		Unlocks: []FeatureUnlock{{ID: 9, Tag: "Music", StartsAt: now.Add(-time.Hour)}},
	}
	holdings := HoldingsOf(in, now)
	if !holdings.Lifetime || !holdings.LifetimeAdFree || !slices.Equal(holdings.LifetimeFeatures, []string{"export"}) {
		t.Fatalf("永久持有取永久段的当前配置，得到 %+v", holdings)
	}
	if !slices.Equal(holdings.PermanentUnlocks, []string{"music"}) || !holdings.CatalogKnown {
		t.Fatalf("永久解锁与目录应带上，得到 %+v", holdings)
	}

	// 看广告领的永久段、场景设置不免广告
	in.Segments = []Segment{{ID: 5, Channel: ChannelAdReward, Lifetime: true, AdTerms: &SegmentAdTerms{AdFree: &falseValue}}}
	if HoldingsOf(in, now).LifetimeAdFree {
		t.Fatal("看广告领的永久会员按场景不免广告")
	}
	// 老系统写进 users 的永久会员：没有永久段，按免广告
	in.Segments = nil
	if !HoldingsOf(in, now).LifetimeAdFree {
		t.Fatal("找不到永久开通的永久会员按免广告处理")
	}
	if HoldingsOf(EvalInput{}, now).Lifetime {
		t.Fatal("没有 vip_lifetime_at 就不是永久会员")
	}
}

func TestHoldingsOtherLines(t *testing.T) {
	holdings := Holdings{Lifetime: true, LifetimeAdFree: true, LifetimeFeatures: []string{"export"}, PermanentUnlocks: []string{"ai"}}
	if reason, ok := holdings.CoversFeature("AI"); !ok || reason != OwnedFeature {
		t.Fatalf("已永久解锁的功能，得到 %q %v", reason, ok)
	}
	if reason, ok := holdings.CoversFeature("export"); !ok || reason != OwnedLifetime {
		t.Fatalf("永久会员已含的功能，得到 %q %v", reason, ok)
	}
	if _, ok := holdings.CoversFeature("music"); ok {
		t.Fatal("没有的功能不算已拥有")
	}
	if !holdings.CoversAdFree() || (Holdings{Lifetime: true}).CoversAdFree() {
		t.Fatal("免广告时长只对免广告的永久会员是空的")
	}
	if _, ok := (Holdings{}).CoversFeatureTime("music"); ok {
		t.Fatal("普通用户的功能时长有用")
	}
	if GrantBenefit(Grant{PayChannel: ChannelAdReward, Metadata: map[string]any{"adFree": false}}).AdFree {
		t.Fatal("看广告领的会员按 metadata 里的 adFree")
	}
	assumed := (Holdings{}).Assume(MembershipBenefit([]string{"export"}, true, true))
	if !assumed.Lifetime || !assumed.LifetimeAdFree || !assumed.CoversPlan(holdingsPlan(false, "export")) {
		t.Fatalf("假设开通永久之后月卡已包含，得到 %+v", assumed)
	}
}
