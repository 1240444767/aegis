package vip

import (
	"testing"
	"time"
)

// 开通永久时的存量折算。

var upgradeNow = time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

func upDay(n int) time.Time { return upgradeNow.Add(time.Duration(n) * 24 * time.Hour) }

func lifetimeHoldings(features ...string) Holdings {
	return Holdings{Lifetime: true, LifetimeAdFree: true, LifetimeFeatures: features}
}

// 正在走的基础版被覆盖：截在此刻，按剩余 10 天折积分，到期时间回到此刻。
func TestPlanUpgradeCutsRunningCoveredSegment(t *testing.T) {
	expire := upDay(10)
	plan := PlanUpgrade(UpgradeInput{
		Now:      upgradeNow,
		ExpireAt: &expire,
		Segments: []TimedSegment{{ID: 1, TransactionNo: "T1", Label: "月卡", Features: []string{"export"}, From: upDay(-20), Until: upDay(10)}},
		Holdings: lifetimeHoldings("export"),
		Policy:   DefaultConversionPolicy(1),
	})
	if plan.RemovedSeconds != 10*86400 || plan.Points != 100 {
		t.Fatalf("应折掉 10 天、100 积分，得到 %d 秒 / %d", plan.RemovedSeconds, plan.Points)
	}
	if len(plan.Moves) != 1 || !plan.Moves[0].Removed || !plan.Moves[0].Until.Equal(upgradeNow) {
		t.Fatalf("被覆盖的段应截在此刻：%+v", plan.Moves)
	}
	if plan.NewExpireAt == nil || !plan.NewExpireAt.Equal(upgradeNow) {
		t.Fatalf("到期时间应回到此刻：%v", plan.NewExpireAt)
	}
	if plan.Items[0].Label != "月卡 10 天" || plan.Items[0].Ref != "T1" {
		t.Fatalf("折算项：%+v", plan.Items[0])
	}
}

// 基础版（被覆盖）之后排着一段高级版（多出功能，不被覆盖）：高级版前移到此刻开始，长度不变。
func TestPlanUpgradeShiftsUncoveredSegmentForward(t *testing.T) {
	expire := upDay(40)
	plan := PlanUpgrade(UpgradeInput{
		Now:      upgradeNow,
		ExpireAt: &expire,
		Segments: []TimedSegment{
			{ID: 1, Label: "基础版", Features: []string{"export"}, From: upDay(-5), Until: upDay(10)},
			{ID: 2, Label: "高级版", Features: []string{"export", "ai"}, From: upDay(10), Until: upDay(40)},
		},
		Holdings: lifetimeHoldings("export"),
		Policy:   DefaultConversionPolicy(1),
	})
	if plan.RemovedSeconds != 10*86400 {
		t.Fatalf("只折基础版剩余的 10 天，得到 %d", plan.RemovedSeconds)
	}
	var moved *SegmentMove
	for index := range plan.Moves {
		if plan.Moves[index].ID == 2 {
			moved = &plan.Moves[index]
		}
	}
	if moved == nil || moved.Removed || !moved.From.Equal(upgradeNow) || !moved.Until.Equal(upDay(30)) {
		t.Fatalf("高级版应前移到 [now, +30d]：%+v", plan.Moves)
	}
	if plan.NewExpireAt == nil || !plan.NewExpireAt.Equal(upDay(30)) {
		t.Fatalf("到期时间应为高级版的新终点：%v", plan.NewExpireAt)
	}
}

// 没有开通记录的那截到期时间（老系统写进 users 的）一并折掉。
func TestPlanUpgradeConvertsOrphanExpiry(t *testing.T) {
	expire := upDay(3)
	plan := PlanUpgrade(UpgradeInput{Now: upgradeNow, ExpireAt: &expire, Holdings: lifetimeHoldings(), Policy: DefaultConversionPolicy(1)})
	if plan.RemovedSeconds != 3*86400 || plan.Points != 30 || plan.NewExpireAt == nil {
		t.Fatalf("应折掉 3 天：%+v", plan)
	}
}

// 功能解锁、功能时长、免广告一并折算；永久会员不包含的功能解锁不动。
func TestPlanUpgradeFeatureLines(t *testing.T) {
	adFree := upDay(2)
	plan := PlanUpgrade(UpgradeInput{
		Now: upgradeNow,
		Unlocks: []TimedUnlock{
			{ID: 7, Tag: "export", Name: "导出", Starts: upDay(-1), Ends: upDay(4)},
			{ID: 8, Tag: "ai", Name: "AI", Starts: upDay(-1), Ends: upDay(4)},
		},
		Balances: []FeatureTimeBalance{{Feature: "online_music", Name: "在线听歌", Seconds: 5400}},
		AdFree:   &adFree,
		Holdings: lifetimeHoldings("export"),
		Policy:   DefaultConversionPolicy(1),
	})
	if len(plan.UnlockIDs) != 1 || plan.UnlockIDs[0] != 7 {
		t.Fatalf("只折已包含功能的解锁：%v", plan.UnlockIDs)
	}
	if len(plan.Balances) != 1 || !plan.EndAdFree {
		t.Fatalf("功能时长与免广告应折掉：%+v", plan)
	}
	// 解锁 4 天 × 5 = 20；时长 1.5 小时 × 10 向上取整 = 15；免广告 2 天 × 5 = 10
	if plan.Points != 45 {
		t.Fatalf("积分应为 45，得到 %d（%+v）", plan.Points, plan.Items)
	}
}

func TestPlanUpgradeNotLifetimeDoesNothing(t *testing.T) {
	expire := upDay(3)
	plan := PlanUpgrade(UpgradeInput{Now: upgradeNow, ExpireAt: &expire, Policy: DefaultConversionPolicy(1)})
	if !plan.Empty() {
		t.Fatalf("不是永久会员不折：%+v", plan)
	}
}

func TestHoldingsCovers(t *testing.T) {
	h := Holdings{Lifetime: true, LifetimeAdFree: true, LifetimeFeatures: []string{"export"}, PermanentUnlocks: []string{"ai"}}
	if !h.CoversMembership(nil) || !h.CoversMembership([]string{"ai", "export"}) || h.CoversMembership([]string{"music"}) {
		t.Fatal("会员天数对永久会员是空的；永久解锁的功能也算已有；多出功能的不是")
	}
	if reason, ok := h.CoversFeature("ai"); !ok || reason != OwnedFeature {
		t.Fatal("已永久解锁")
	}
	if reason, ok := h.CoversFeature("export"); !ok || reason != OwnedLifetime {
		t.Fatal("永久会员已包含")
	}
	if _, ok := h.CoversFeature("other"); ok {
		t.Fatal("没有的功能不是已拥有")
	}
}

func TestConversionPolicyValidate(t *testing.T) {
	policy := DefaultConversionPolicy(1)
	if err := policy.Validate(); err != nil {
		t.Fatal(err)
	}
	policy.Owned = "x"
	if policy.Validate() == nil {
		t.Fatal("未知处理方式应报错")
	}
	policy = DefaultConversionPolicy(1)
	policy.PerVipDay = -1
	if policy.Validate() == nil {
		t.Fatal("负数应报错")
	}
}
