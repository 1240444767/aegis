package service

import (
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"aegis/internal/config"
	cardkeydomain "aegis/internal/domain/cardkey"
	lotterydomain "aegis/internal/domain/lottery"
	rewardedad "aegis/internal/domain/rewardedad"
	vipdomain "aegis/internal/domain/vip"
	pgrepo "aegis/internal/repository/postgres"
)

// 人群特权与三种新奖品的端到端集成测试（真实 Postgres，全部迁移跑两遍）：
// 永久会员奖品（老客户端看到 vip_plan）→ 永久会员特权（视频抽奖 100% 免看广告并进免费次数、额外免费、
// 积分折扣、保底提前、活动页说明）→ 签到条件的特权 → 功能时长奖品 → 免广告时长奖品（功能时长免扣、
// 开屏免除）→ 永久会员抽中免广告时长按已拥有折算 → 模拟里的特权身份。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestLotteryPerksIntegration
func TestLotteryPerksIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	vip := NewVipService(zap.NewNop(), pg)
	rewarded := NewRewardedAdService(zap.NewNop(), pg, config.Config{})
	lottery := NewLotteryService(zap.NewNop(), pg, rewarded)
	featureTime := NewFeatureTimeService(zap.NewNop(), pg)
	adPolicy := NewAdPolicyService(zap.NewNop(), pg, rewarded)
	const appID = int64(40100)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'lottery-perks') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	if _, err := vip.AdminSaveFeature(ctx, vipdomain.FeatureMutation{AppID: appID, Tag: "online_music", Name: ptrTo("在线音乐")}); err != nil {
		t.Fatal(err)
	}
	if _, err := rewarded.SaveConfig(ctx, rewardedad.SaveConfigInput{
		AppID: appID, Enabled: true, VerifyMode: rewardedad.VerifyClient, Operator: "admin",
		Scenes: []rewardedad.Scene{{Key: "lottery", Name: "看视频抽奖", PlacementID: "4892299192457961", Enabled: true,
			DailyLimit: 4, Rewards: []rewardedad.Reward{{Type: cardkeydomain.RewardLotteryDraws, Amount: 1}}}},
	}, "lottery-perks"); err != nil {
		t.Fatal(err)
	}
	carol := insertTestUser(t, ctx, pool, appID, "perks-carol", "0")
	dave := insertTestUser(t, ctx, pool, appID, "perks-dave", "0")

	// 只出一种奖的九宫格：其余 7 格是权重 0 的谢谢参与（第 0 格兜底）
	single := func(name string, reward lotterydomain.Reward) lotterydomain.ActivityInput {
		prizes := make([]lotterydomain.PrizeInput, 0, 8)
		for position := 0; position < 8; position++ {
			prizes = append(prizes, lotterydomain.PrizeInput{Position: position, Reward: lotterydomain.Reward{Type: lotterydomain.KindNone},
				Quantity: -1, Fallback: position == 0})
		}
		prizes[1] = lotterydomain.PrizeInput{Position: 1, Reward: reward, Weight: 1, Quantity: -1}
		return lotterydomain.ActivityInput{AppID: appID, Name: name, UIMode: lotterydomain.UIModeGrid, Status: lotterydomain.StatusActive,
			FreeDaily: 5, PityPosition: -1, Prizes: prizes, Operator: "admin"}
	}
	mustCreate := func(input lotterydomain.ActivityInput) *lotterydomain.AdminActivity {
		t.Helper()
		activity, err := lottery.CreateActivity(ctx, input)
		if err != nil {
			t.Fatalf("新建活动失败：%v", err)
		}
		return activity
	}
	mustDraw := func(userID, activityID int64, source string) *lotterydomain.DrawResult {
		t.Helper()
		result, err := lottery.Draw(ctx, LotteryDrawRequest{AppID: appID, UserID: userID, ActivityID: activityID, Source: source})
		if err != nil {
			t.Fatalf("抽奖失败（%s）：%v", source, err)
		}
		return result
	}
	mustState := func(userID, activityID int64) *lotterydomain.ClientState {
		t.Helper()
		state, err := lottery.State(ctx, appID, userID, activityID)
		if err != nil {
			t.Fatal(err)
		}
		return state
	}

	// ── 保存校验：功能时长要引用登记过的功能，特权表达式写错保存时报错 ──
	if _, err := lottery.CreateActivity(ctx, single("坏", lotterydomain.Reward{Type: lotterydomain.KindFeatureTime, Tag: "missing", Amount: 30})); errCode(err) != 40000 {
		t.Fatalf("未登记功能的时长奖应被拒，得到 %v", err)
	}
	bad := single("坏", lotterydomain.Reward{Type: lotterydomain.KindPoints, Amount: 1})
	bad.Perks = []lotterydomain.Perk{{Name: "坏", ExtraFree: 1, When: "user.unknown > 1"}}
	if _, err := lottery.CreateActivity(ctx, bad); errCode(err) != 40000 {
		t.Fatalf("写错的特权表达式应被拒，得到 %v", err)
	}

	// ── 永久会员奖品：老客户端看到 vip_plan，真实类型在 rewardType ──
	lifetimeActivity := mustCreate(single("永久会员抽奖", lotterydomain.Reward{Type: lotterydomain.KindVipLifetime}))
	if lifetimeActivity.Prizes[1].Name != "永久会员" {
		t.Fatalf("默认奖品名应为「永久会员」，得到 %q", lifetimeActivity.Prizes[1].Name)
	}
	state := mustState(carol, lifetimeActivity.ID)
	if prize := state.Activity.Prizes[1]; prize.Kind != lotterydomain.KindVipPlan || prize.RewardType != lotterydomain.KindVipLifetime {
		t.Fatalf("活动页的奖品类型应按老客户端映射，得到 %+v", prize)
	}
	won := mustDraw(carol, lifetimeActivity.ID, "")
	if won.Draw.PrizeKind != lotterydomain.KindVipPlan || won.Draw.RewardType != lotterydomain.KindVipLifetime || won.Draw.Conversion != nil {
		t.Fatalf("抽中永久会员，得到 %+v", won.Draw)
	}
	var lifetimeAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT vip_lifetime_at FROM users WHERE id = $1`, carol).Scan(&lifetimeAt); err != nil || lifetimeAt == nil {
		t.Fatalf("抽中后应成为永久会员，得到 %v %v", lifetimeAt, err)
	}
	again := mustDraw(carol, lifetimeActivity.ID, "")
	if again.Draw.Conversion == nil || again.Draw.Conversion.Reward.Type != lotterydomain.KindPoints ||
		again.Draw.Conversion.Reward.Amount != lotterydomain.DefaultLifetimePointsLifetime {
		t.Fatalf("已是永久会员再抽中应折算 %d 积分，得到 %+v", lotterydomain.DefaultLifetimePointsLifetime, again.Draw.Conversion)
	}
	if records, err := lottery.MyDraws(ctx, appID, carol, lifetimeActivity.ID, 1, 10); err != nil ||
		records.Items[0].PrizeKind != lotterydomain.KindVipPlan || records.Items[0].RewardType != lotterydomain.KindVipLifetime {
		t.Fatalf("记录里的类型同样映射，得到 %+v %v", records, err)
	}
	if admin, err := lottery.ListDraws(ctx, lotterydomain.DrawQuery{AppID: appID, UserID: carol, Page: 1, Limit: 10}); err != nil ||
		admin.Items[0].PrizeKind != lotterydomain.KindVipLifetime {
		t.Fatalf("管理端看真实类型，得到 %+v %v", admin, err)
	}

	// ── 人群特权 ──
	perksInput := single("特权抽奖", lotterydomain.Reward{Type: lotterydomain.KindPoints, Amount: 1})
	perksInput.FreeDaily = 1
	perksInput.PointsCost, perksInput.PointsDaily = 20, 2
	perksInput.VideoScene = "lottery"
	perksInput.PityThreshold, perksInput.PityPosition = 6, 1
	perksInput.Prizes[2] = lotterydomain.PrizeInput{Position: 2, Reward: lotterydomain.Reward{Type: lotterydomain.KindNone}, Weight: 1000, Quantity: -1}
	perksInput.Perks = []lotterydomain.Perk{
		{Name: "永久会员特权", Audience: lotterydomain.AudienceLifetime, AdSkip: 100, ExtraFree: 1, PointsDiscount: 50,
			ExtraPoints: 1, PityCut: 3},
		{Name: "签到奖励", SignedToday: true, ExtraFree: 2},
	}
	perks := mustCreate(perksInput)
	if len(perks.Perks) != 2 {
		t.Fatalf("特权应落库，得到 %+v", perks.Perks)
	}

	// 非会员、没签到：一切照旧
	plain := mustState(dave, perks.ID)
	if plain.Perks != nil || plain.Chances.Free.Daily != 1 || plain.Chances.Video.Remaining != 4 ||
		plain.Chances.Points.Cost != 20 || plain.Pity == nil || plain.Pity.Remaining != 6 || plain.Lifetime != nil {
		t.Fatalf("没有特权时不变，得到 %+v %+v %+v", plain.Chances, plain.Pity, plain.Perks)
	}

	// 永久会员（100% 免看广告）：免看广告按顺序逐次解锁 —— 开页只解锁当前这 1 次（不是全天 4 次一次性给），
	// 视频次数相应少 1；积分 5 折、每天多 1 次；保底提前 3 次
	vipState := mustState(carol, perks.ID)
	if vipState.Chances.Free.Daily != 1+1+1 || vipState.Chances.Video.Remaining != 3 || !vipState.Chances.Video.Available ||
		vipState.Chances.Points.Cost != 10 || vipState.Chances.Points.Daily != 3 || vipState.Pity == nil ||
		vipState.Pity.Threshold != 3 {
		t.Fatalf("永久会员特权，得到 %+v %+v", vipState.Chances, vipState.Pity)
	}
	if vipState.Perks == nil || vipState.Perks.Waived != 1 || !vipState.Perks.NextWaived || vipState.Lifetime == nil ||
		vipState.Lifetime.Policy != lotterydomain.NoticePerk || !strings.Contains(vipState.Lifetime.Message, "本次免看") {
		t.Fatalf("活动页说明并进提示卡（老客户端原样显示），得到 %+v %+v", vipState.Perks, vipState.Lifetime)
	}
	for i := 0; i < 6; i++ {
		result := mustDraw(carol, perks.ID, "")
		if result.Draw.Source != lotterydomain.SourceFree {
			t.Fatalf("第 %d 次应用免费次数，得到 %s", i+1, result.Draw.Source)
		}
		if i == 2 && !result.Draw.Pity {
			t.Fatalf("保底提前到 3 次：第 3 次必中，得到 %+v", result.Draw)
		}
		// 基础 2 次用完后，每用掉一次免看才解锁下一次：免费总数逐次涨到 6，视频次数逐次降到 0
		if want := min(max(i+2, 3), 6); result.Chances.Free.Daily != want {
			t.Fatalf("第 %d 次之后免费总数应为 %d，得到 %d", i+1, want, result.Chances.Free.Daily)
		}
	}
	if done := mustState(carol, perks.ID); done.Chances.Video.Remaining != 0 || done.Chances.Video.Available ||
		done.Perks.NextWaived || done.Perks.Waived != 4 {
		t.Fatalf("4 次视频全部免看用完，视频次数为 0，得到 %+v %+v", done.Chances.Video, done.Perks)
	}
	if _, err := lottery.Draw(ctx, LotteryDrawRequest{AppID: appID, UserID: carol, ActivityID: perks.ID,
		Source: lotterydomain.SourceFree}); errCode(err) != errCodeLotteryFreeExhausted {
		t.Fatalf("免费次数用完（含免掉的视频）应报 42913，得到 %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET integral = 100 WHERE id = $1`, carol); err != nil {
		t.Fatal(err)
	}
	pointsDraw := mustDraw(carol, perks.ID, "")
	var integral int64
	if err := pool.QueryRow(ctx, `SELECT integral FROM users WHERE id = $1`, carol).Scan(&integral); err != nil {
		t.Fatal(err)
	}
	if pointsDraw.Draw.Source != lotterydomain.SourcePoints || pointsDraw.Draw.PointsCost != 10 || integral < 90 || integral > 91 {
		t.Fatalf("积分抽奖按折后价扣 10，得到 %+v 余额 %d", pointsDraw.Draw, integral)
	}
	var tracePerks []byte
	if err := pool.QueryRow(ctx, `SELECT trace->'perks' FROM lottery_draws WHERE id = $1`, pointsDraw.Draw.ID).Scan(&tracePerks); err != nil ||
		!strings.Contains(string(tracePerks), "永久会员特权") {
		t.Fatalf("出奖方案里记下生效的特权，得到 %s %v", tracePerks, err)
	}

	// 签到之后多 2 次（条件特权，当天生效）
	today := lotteryDayStart(time.Now()).Format(time.DateOnly)
	if _, err := pool.Exec(ctx, `INSERT INTO daily_signins (user_id, appid, sign_date) VALUES ($1, $2, $3::date)`, dave, appID, today); err != nil {
		t.Fatal(err)
	}
	signed := mustState(dave, perks.ID)
	if signed.Chances.Free.Daily != 3 || signed.Perks == nil || signed.Perks.Names[0] != "签到奖励" {
		t.Fatalf("签到后每天多 2 次免费，得到 %+v %+v", signed.Chances.Free, signed.Perks)
	}

	// ── 功能时长奖品 ──
	timeActivity := mustCreate(single("时长抽奖", lotterydomain.Reward{Type: lotterydomain.KindFeatureTime, Tag: "online_music", Amount: 30}))
	timeDraw := mustDraw(dave, timeActivity.ID, "")
	if timeDraw.Draw.PrizeKind != lotterydomain.KindFeature || timeDraw.Draw.RewardType != lotterydomain.KindFeatureTime ||
		len(timeDraw.Draw.Results) != 1 || !strings.Contains(timeDraw.Draw.Results[0].Detail, "余额 30 分钟") {
		t.Fatalf("抽中 30 分钟在线音乐，得到 %+v", timeDraw.Draw)
	}
	status, err := featureTime.Status(ctx, appID, dave, "online_music")
	if err != nil || status.BalanceSeconds != 1800 || status.Exempt {
		t.Fatalf("功能时长到账 1800 秒、仍要扣，得到 %+v %v", status, err)
	}

	// ── 免广告时长：功能时长免扣、开屏免除 ──
	adFreeActivity := mustCreate(single("免广告抽奖", lotterydomain.Reward{Type: lotterydomain.KindAdFree, Amount: 7}))
	adFreeDraw := mustDraw(dave, adFreeActivity.ID, "")
	if adFreeDraw.Draw.RewardType != lotterydomain.KindAdFree || adFreeDraw.Draw.Results[0].ExpireAt == nil {
		t.Fatalf("抽中免广告 7 天，得到 %+v", adFreeDraw.Draw)
	}
	mustDraw(dave, adFreeActivity.ID, "")
	var expires time.Time
	if err := pool.QueryRow(ctx, `SELECT expires_at FROM user_ad_free_passes WHERE appid = $1 AND user_id = $2`, appID, dave).
		Scan(&expires); err != nil || time.Until(expires) < 13*24*time.Hour {
		t.Fatalf("两次抽中顺延到 14 天，得到 %v %v", expires, err)
	}
	if status, err = featureTime.Status(ctx, appID, dave, "online_music"); err != nil || !status.Exempt {
		t.Fatalf("免广告有效期内功能时长免扣，得到 %+v %v", status, err)
	}
	lease, err := featureTime.Lease(ctx, appID, dave, "online_music", 60)
	if err != nil || !lease.Exempt {
		t.Fatalf("免扣时不租时长，得到 %+v %v", lease, err)
	}
	policy, err := adPolicy.ClientPolicy(ctx, appID, dave, "")
	if err != nil || !policy.Exempt || policy.AdFreeUntil == nil || policy.Splash.Available {
		t.Fatalf("免广告有效期内开屏免除，得到 %+v %v", policy, err)
	}
	// 免广告用户这一人群的特权
	adFreePerk := single("免广告特权", lotterydomain.Reward{Type: lotterydomain.KindPoints, Amount: 1})
	adFreePerk.Perks = []lotterydomain.Perk{{Name: "免广告用户", Audience: lotterydomain.AudienceAdFree, ExtraFree: 3}}
	if got := mustState(dave, mustCreate(adFreePerk).ID); got.Chances.Free.Daily != 8 {
		t.Fatalf("免广告用户每天多 3 次，得到 %+v", got.Chances.Free)
	}

	// 永久会员抽中免广告时长：已拥有，按 7 天 × 10 折算 70 积分
	converted := mustDraw(carol, adFreeActivity.ID, "")
	if converted.Draw.Conversion == nil || converted.Draw.Conversion.Reward.Amount != 70 {
		t.Fatalf("永久会员抽中免广告应折算 70 积分，得到 %+v", converted.Draw.Conversion)
	}
	// 永久会员抽中功能时长：会员本就免扣，折算 1 小时 × 10 = 10 积分
	if converted = mustDraw(carol, timeActivity.ID, ""); converted.Draw.Conversion == nil || converted.Draw.Conversion.Reward.Amount != 10 {
		t.Fatalf("永久会员抽中功能时长应折算 10 积分，得到 %+v", converted.Draw.Conversion)
	}

	// ── 模拟：虚拟用户按永久会员身份算，保底提前到 3 次 ──
	sim, err := lottery.Simulate(ctx, LotterySimulation{AppID: appID, ActivityID: perks.ID,
		PerkFacts: &lotterydomain.PerkFacts{Lifetime: true},
		Options:   lotterydomain.SimulationInput{Users: 50, DrawsPerUser: 10, Seed: 7}})
	if err != nil {
		t.Fatal(err)
	}
	if sim.Perks == nil || sim.Perks.PityCut != 3 || sim.Result.PityHits == 0 {
		t.Fatalf("模拟应计入永久会员特权（保底 3 次必中），得到 %+v %+v", sim.Perks, sim.Result)
	}
}
