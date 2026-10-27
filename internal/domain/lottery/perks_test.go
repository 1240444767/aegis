package lottery

import (
	vipdomain "aegis/internal/domain/vip"

	"math"
	"strings"
	"testing"
	"time"
)

func TestValidatePerks(t *testing.T) {
	ok := []Perk{{Name: "永久会员特权", Audience: AudienceLifetime, AdSkip: 100, ExtraFree: 2},
		{Name: "签到奖励", SignedToday: true, ExtraFree: 1},
		{Name: "老用户", When: "user.registeredDays > 30 && draw.hour >= 20", WinBoost: 1.2}}
	if err := ValidatePerks(NormalizePerks(ok)); err != nil {
		t.Fatalf("合法的特权被拒：%v", err)
	}
	cases := []struct {
		perk Perk
		want string
	}{
		{Perk{Audience: "vip"}, "未知的人群"},
		{Perk{Audience: AudienceAll}, "没有设置任何效果"},
		{Perk{AdSkip: 120}, "免看广告概率"},
		{Perk{PointsDiscount: 95}, "积分折扣"},
		{Perk{WinBoost: 0.5}, "中奖加成"},
		{Perk{ExtraFree: 1, When: "user.nope > 1"}, "条件表达式"},
		{Perk{ExtraFree: 1, When: "user.missStreak > 1"}, "条件表达式"},
	}
	for _, tc := range cases {
		err := ValidatePerks(NormalizePerks([]Perk{tc.perk}))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%+v：想要包含「%s」的错误，得到 %v", tc.perk, tc.want, err)
		}
	}
}

func TestResolvePerksAudienceAndStacking(t *testing.T) {
	perks := NormalizePerks([]Perk{
		{Name: "会员", Audience: AudienceMember, ExtraFree: 1, AdSkip: 50, PointsDiscount: 20},
		{Name: "永久", Audience: AudienceLifetime, ExtraFree: 2, AdSkip: 50, PointsDiscount: 30, WinBoost: 2},
		{Name: "签到", SignedToday: true, ExtraFree: 1, WinBoost: 2},
		{Name: "在线", OnlineMinutes: 30, PityCut: 3},
		{Name: "非会员", Audience: AudienceNonMember, ExtraFree: 5},
		{Name: "停用", Disabled: true, ExtraFree: 9},
	})
	effects := ResolvePerks(perks, PerkFacts{Member: true, Lifetime: true, SignedToday: true, OnlineMinutesToday: 31})
	if got := strings.Join(effects.Names, ","); got != "会员,永久,签到,在线" {
		t.Fatalf("生效的特权 = %s", got)
	}
	if effects.ExtraFree != 4 || effects.PointsDiscount != 30 || effects.PityCut != 3 {
		t.Fatalf("叠加口径不对：%+v", effects)
	}
	if math.Abs(effects.AdSkip-0.75) > 1e-9 {
		t.Fatalf("两条 50%% 独立合并应为 75%%，得到 %v", effects.AdSkip)
	}
	if effects.WinBoost != MaxPerkWinBoost {
		t.Fatalf("中奖加成相乘后应封顶 %d 倍，得到 %v", MaxPerkWinBoost, effects.WinBoost)
	}
	none := ResolvePerks(perks, PerkFacts{OnlineMinutesToday: 10})
	if strings.Join(none.Names, ",") != "非会员" || none.ExtraFree != 5 {
		t.Fatalf("非会员只应得到非会员特权：%+v", none)
	}
}

func TestPerkWhenExpression(t *testing.T) {
	perks := []Perk{{Name: "晚间", When: "draw.hour >= 20 && user.adFree", ExtraFree: 1}}
	evening := time.Date(2026, 10, 26, 21, 0, 0, 0, time.UTC)
	if ResolvePerks(perks, PerkFacts{Now: evening, AdFree: true}).Empty() {
		t.Fatal("晚间且免广告应生效")
	}
	if !ResolvePerks(perks, PerkFacts{Now: evening.Add(-10 * time.Hour), AdFree: true}).Empty() {
		t.Fatal("白天不应生效")
	}
}

func TestAdSkipSlotDeterministicAndCalibrated(t *testing.T) {
	day := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	if !AdSkipSlot(1, 2, day, 3, 1) || AdSkipSlot(1, 2, day, 3, 0) {
		t.Fatal("概率 100% 应免、0 应不免")
	}
	first := AdSkipSlot(7, 42, day, 4, 0.3)
	for i := 0; i < 5; i++ {
		if AdSkipSlot(7, 42, day, 4, 0.3) != first {
			t.Fatal("同一人同一天同一次的结论必须稳定")
		}
	}
	// 每次判定是独立的 p 伯努利：2 万人 × 10 次的均值应在 0.3 附近（标准误约 0.001）
	total := 0
	for user := int64(1); user <= 20000; user++ {
		for k := 1; k <= 10; k++ {
			if AdSkipSlot(9, user, day, k, 0.3) {
				total++
			}
		}
	}
	if mean := float64(total) / 200000; math.Abs(mean-0.3) > 0.006 {
		t.Fatalf("免广告比例 %.4f 偏离 0.3", mean)
	}
}

// 回归：旧实现一早就把全天判中的次数一次性并进免费次数（46 次视频 × 25% → 开页就多 8 次）。
// 现在任何时刻最多只解锁「当前这一次」，用掉（或看广告越过）之后才轮到下一次。
func TestWalkAdSkipUnlocksOneAtATime(t *testing.T) {
	day := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	for user := int64(1); user <= 2000; user++ {
		progress := WalkAdSkip(5, user, day, 46, 0.25, 0, 0)
		if progress.Unlocked() > 1 {
			t.Fatalf("什么都没做时最多解锁 1 次，用户 %d 解锁了 %d 次", user, progress.Unlocked())
		}
	}
	// 100%：每用掉一次免看才解锁下一次，全部用完后不再解锁
	for used := 0; used <= 4; used++ {
		progress := WalkAdSkip(5, 1, day, 4, 1, 0, used)
		want := min(used+1, 4)
		if progress.Unlocked() != want || progress.Used != used || progress.Next != (used < 4) {
			t.Fatalf("用掉 %d 次后应解锁 %d 次，得到 %+v", used, want, progress)
		}
	}
	// 看广告越过的那几次不再解锁：4 次全看了广告，就没有免看的了
	if progress := WalkAdSkip(5, 1, day, 4, 1, 4, 0); progress.Unlocked() != 0 {
		t.Fatalf("视频次数已经用完时不应再解锁，得到 %+v", progress)
	}
	// 0% 永不解锁
	if progress := WalkAdSkip(5, 1, day, 4, 0, 0, 0); progress.Unlocked() != 0 {
		t.Fatalf("概率 0 不解锁，得到 %+v", progress)
	}
}

func TestWithAdSkipUsesFreeOverflow(t *testing.T) {
	day := time.Date(2026, 10, 26, 0, 0, 0, 0, time.UTC)
	activity := Activity{ID: 3, FreeDaily: 1}
	video := VideoChances{Enabled: true, DailyLimit: 4, Remaining: 4}
	effects := PerkEffects{Names: []string{"永久"}, ExtraFree: 1, AdSkip: 1}
	// 基础 1 + 额外 1 还没用完：只解锁当前这 1 次
	got := effects.WithAdSkip(activity, 9, day, video, 1)
	if got.Waived != 1 || got.WaivedUsed != 0 || !got.NextWaived {
		t.Fatalf("基础免费没用完时只解锁 1 次，得到 %+v", got)
	}
	if personal := got.Personalize(activity); personal.FreeDaily != 3 {
		t.Fatalf("免费次数 = 1 + 1 + 1，得到 %d", personal.FreeDaily)
	}
	if adjusted := got.AdjustVideo(video); adjusted.Remaining != 3 {
		t.Fatalf("视频次数扣掉解锁的 1 次，得到 %d", adjusted.Remaining)
	}
	// 免费用了 4 次：超出基础的 2 次是免看广告，解锁到第 3 次
	got = effects.WithAdSkip(activity, 9, day, video, 4)
	if got.Waived != 3 || got.WaivedUsed != 2 {
		t.Fatalf("用掉 2 次免看后应解锁 3 次，得到 %+v", got)
	}
	// 视频没开：不解锁
	if got = effects.WithAdSkip(activity, 9, day, VideoChances{}, 0); got.Waived != 0 {
		t.Fatalf("视频抽奖没开时不解锁，得到 %+v", got)
	}
}

func TestPersonalizeAndChances(t *testing.T) {
	pity := int64(11)
	activity := Activity{Status: StatusActive, FreeDaily: 1, PointsCost: 25, PointsDaily: 3, PityThreshold: 4, PityPrizeID: &pity}
	effects := PerkEffects{Names: []string{"永久"}, ExtraFree: 2, Waived: 1, PointsDiscount: 30, ExtraPoints: 2, PityCut: 5}
	personal := effects.Personalize(activity)
	if personal.FreeDaily != 4 || personal.PointsCost != 18 || personal.PointsDaily != 5 || personal.PityThreshold != 2 {
		t.Fatalf("个人活动不对：%+v", personal)
	}
	if activity.FreeDaily != 1 {
		t.Fatal("不能改到原活动")
	}
	video := VideoChances{Enabled: true, DailyLimit: 3, Remaining: 1, Available: true}
	adjusted := effects.AdjustVideo(video)
	if adjusted.Remaining != 0 || adjusted.Available {
		t.Fatalf("免掉的视频次数要从视频里扣：%+v", adjusted)
	}
	chances := EvaluateChances(personal, UserFacts{FreeUsedToday: 3, Integral: 18}, adjusted)
	if chances.Free.Remaining != 1 || chances.Next != SourceFree || chances.Points.Cost != 18 || !chances.Points.Affordable {
		t.Fatalf("次数判定不对：%+v", chances)
	}
	unlimited := effects.AdjustVideo(VideoChances{Enabled: true, Remaining: -1})
	if unlimited.Remaining != -1 {
		t.Fatal("不限次的视频不扣")
	}
	if got := VideoSlots(VideoChances{Enabled: true}, PerkEffects{AdSkip: 0.5, AdSkipSlots: 7}); got != 7 {
		t.Fatalf("不限次时按特权的判定次数，得到 %d", got)
	}
	if got := VideoSlots(VideoChances{Enabled: true, DailyLimit: 3}, PerkEffects{AdSkip: 0.5, AdSkipSlots: 7}); got != 3 {
		t.Fatalf("限次时按场景每日上限，得到 %d", got)
	}
}

func TestBoostPrizes(t *testing.T) {
	prizes := []Prize{{ID: 1, Kind: KindNone, Weight: 50, Quantity: -1}, {ID: 2, Kind: KindPoints, Weight: 50, Quantity: -1}}
	boosted := PerkEffects{Names: []string{"x"}, WinBoost: 2}.BoostPrizes(prizes)
	probabilities := Probabilities(boosted)
	if math.Abs(probabilities[1]-66.67) > 0.01 {
		t.Fatalf("加成 2 倍后中奖概率应为 66.67%%，得到 %v", probabilities)
	}
	if prizes[1].Weight != 50 {
		t.Fatal("不能改到原奖盘")
	}
}

func TestSummaryOf(t *testing.T) {
	activity := Activity{PointsCost: 20, PityThreshold: 10}
	summary := SummaryOf(PerkEffects{Names: []string{"永久会员特权"}, ExtraFree: 2, AdSkip: 0.3, Waived: 1, WaivedUsed: 1,
		PointsDiscount: 20, PityCut: 2, WinBoost: 1.5}, activity, VideoChances{Enabled: true})
	if summary == nil {
		t.Fatal("应有说明")
	}
	want := "永久会员特权：每天多 2 次免费抽奖，视频抽奖 30% 概率免看广告（今天已免 1 次），积分抽奖 8 折，保底提前 2 次，中奖概率提升至 1.5 倍"
	if summary.Message != want {
		t.Fatalf("说明 = %s", summary.Message)
	}
	if SummaryOf(PerkEffects{}, activity, VideoChances{}) != nil {
		t.Fatal("没有特权时不出说明")
	}
	merged := MergePerkNotice(&LifetimeNotice{Lifetime: true, Policy: LifetimePoints, Message: "已拥有"}, summary, true)
	if merged.Policy != NoticePerk || merged.OwnedPolicy != LifetimePoints || !strings.HasPrefix(merged.Message, "已拥有；") {
		t.Fatalf("合并后的提示不对：%+v", merged)
	}
}

func TestNewPrizeKinds(t *testing.T) {
	for _, reward := range []Reward{{Type: KindVipLifetime}, {Type: KindFeatureTime, Tag: "Music", Amount: 30},
		{Type: KindAdFree, Amount: 7}} {
		if err := ValidateReward(NormalizeReward(reward)); err != nil {
			t.Fatalf("%s 被拒：%v", reward.Type, err)
		}
	}
	if NormalizeReward(Reward{Type: KindFeatureTime, Tag: " Music ", Amount: 3}).Tag != "music" {
		t.Fatal("功能标识要规范化")
	}
	if err := ValidateReward(Reward{Type: KindFeatureTime, Amount: 3}); err == nil {
		t.Fatal("功能时长没选功能应被拒")
	}
	if err := ValidateReward(Reward{Type: KindAdFree, Amount: 0}); err == nil {
		t.Fatal("免广告 0 天应被拒")
	}
	if ClientKind(KindVipLifetime) != KindVipPlan || ClientKind(KindAdFree) != KindFeature || ClientKind(KindPoints) != KindPoints {
		t.Fatal("老客户端的类型映射不对")
	}
	lifetime := MemberCoverage{Holdings: vipdomain.Holdings{Lifetime: true, LifetimeAdFree: true}}
	for _, reward := range []Reward{{Type: KindVipLifetime}, {Type: KindAdFree, Amount: 7}, {Type: KindFeatureTime, Tag: "music", Amount: 5}} {
		if _, owned := lifetime.Owned(reward, nil); !owned {
			t.Fatalf("永久会员对 %s 应算已拥有", reward.Type)
		}
	}
	unlocked := MemberCoverage{Holdings: vipdomain.Holdings{PermanentUnlocks: []string{"music"}}}
	if reason, owned := unlocked.Owned(Reward{Type: KindFeatureTime, Tag: "music", Amount: 5}, nil); !owned || reason != OwnedFeature {
		t.Fatal("已永久解锁的功能，其时长应算已拥有")
	}
	rule := SwapRule{Policy: LifetimePoints, Value: rates(10, 3650)}
	// 功能时长按应用策略的每小时单价：90 分钟 × 10/小时 = 15
	cases := map[string]int64{KindVipLifetime: 3650, KindAdFree: 70, KindFeatureTime: 15}
	for kind, want := range cases {
		reward := Reward{Type: kind, Amount: 7, Tag: "music"}
		if kind == KindFeatureTime {
			reward.Amount = 90
		}
		swapped, ok := Swap(rule, Prize{Kind: kind, Reward: reward}, nil)
		if !ok || swapped.Type != KindPoints || swapped.Amount != want {
			t.Fatalf("%s 折算为 %+v，想要 %d 积分", kind, swapped, want)
		}
	}
}
