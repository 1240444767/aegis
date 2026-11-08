package service

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	lotterydomain "aegis/internal/domain/lottery"
	vipdomain "aegis/internal/domain/vip"
	pgrepo "aegis/internal/repository/postgres"
)

// 至尊加成的端到端集成测试（真实 Postgres，跑全部迁移）：应用设置保存与校验 → 永久会员的活动页与抽奖事务
// 同一个加成后的系数、非永久会员不受影响 → 模拟（真实用户与虚拟用户）带对照组 → 活动单独关闭 → 按档位分级
// → 管理端单人读数 → 调整运气差异不丢活动的设置 → 积分账。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55491/postgres go test ./internal/service -run TestLotterySupremeIntegration
func TestLotterySupremeIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	lottery := NewLotteryService(zap.NewNop(), pg, nil)
	vip := NewVipService(zap.NewNop(), pg)
	const appID = int64(40420)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'lottery-supreme') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	for _, tag := range []string{"ai", "style"} {
		if _, err := vip.AdminSaveFeature(ctx, vipdomain.FeatureMutation{AppID: appID, Tag: tag, Name: ptrTo(tag)}); err != nil {
			t.Fatal(err)
		}
	}
	lifetimePlan := mustSavePlan(t, vip, vipdomain.PlanMutation{
		AppID: appID, Name: ptrTo("永久会员"), Lifetime: ptrTo(true), Price: ptrTo(decimal.NewFromInt(99)), Features: &[]string{"ai", "style"},
	})
	monthlyPlan := mustSavePlan(t, vip, vipdomain.PlanMutation{
		AppID: appID, Name: ptrTo("月度会员"), DurationDays: ptrTo(30), Price: ptrTo(decimal.NewFromInt(9)), Features: &[]string{"ai"},
	})
	if lifetimePlan.TierID == monthlyPlan.TierID {
		t.Fatal("两个套餐应各有一个档位")
	}
	lifer := insertTestUser(t, ctx, pool, appID, "supreme-lifer", "0")
	monthly := insertTestUser(t, ctx, pool, appID, "supreme-monthly", "0")
	plain := insertTestUser(t, ctx, pool, appID, "supreme-plain", "0")
	for user, plan := range map[int64]int64{lifer: lifetimePlan.ID, monthly: monthlyPlan.ID} {
		if _, err := vip.AdminGrantVip(ctx, AdminVipGrantInput{UserID: user, AppID: appID, PlanID: plan, Quantity: 1}); err != nil {
			t.Fatal(err)
		}
	}

	prizes := make([]lotterydomain.PrizeInput, 0, 8)
	for position := 0; position < 8; position++ {
		prizes = append(prizes, lotterydomain.PrizeInput{Position: position, Reward: lotterydomain.Reward{Type: lotterydomain.KindNone}, Quantity: -1})
	}
	prizes[0] = lotterydomain.PrizeInput{Position: 0, Reward: lotterydomain.Reward{Type: lotterydomain.KindExperience, Amount: 5},
		Weight: 70, Quantity: -1, Fallback: true}
	prizes[1] = lotterydomain.PrizeInput{Position: 1, Reward: lotterydomain.Reward{Type: lotterydomain.KindPoints, Amount: 3}, Weight: 30, Quantity: -1}
	activity, err := lottery.CreateActivity(ctx, lotterydomain.ActivityInput{
		AppID: appID, Name: "至尊加成", UIMode: lotterydomain.UIModeGrid, Status: lotterydomain.StatusActive,
		FreeDaily: 20, PityPosition: -1, Prizes: prizes, Operator: "admin",
		Strategy: lotterydomain.Strategy{Fortune: &lotterydomain.Fortune{Spread: 0.3}},
	})
	if err != nil {
		t.Fatalf("新建活动失败：%v", err)
	}
	state := func(userID int64) *lotterydomain.ClientState {
		t.Helper()
		got, err := lottery.State(ctx, appID, userID, activity.ID)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	type traced struct {
		Fortune     float64                     `json:"fortune"`
		FortuneBase float64                     `json:"fortuneBase"`
		Supreme     *lotterydomain.SupremeGrade `json:"supreme"`
	}
	draw := func(userID int64) traced {
		t.Helper()
		result, err := lottery.Draw(ctx, LotteryDrawRequest{AppID: appID, UserID: userID, ActivityID: activity.ID})
		if err != nil {
			t.Fatalf("抽奖失败：%v", err)
		}
		var raw []byte
		if err := pool.QueryRow(ctx, `SELECT trace FROM lottery_draws WHERE id = $1`, result.Draw.ID).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var out traced
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	natural := map[int64]float64{}
	for _, user := range []int64{lifer, monthly, plain} {
		reading := state(user).Fortune
		if reading == nil || reading.Supreme != nil {
			t.Fatalf("没配置至尊加成时不应有加成：%+v", reading)
		}
		natural[user] = reading.Factor
	}

	// ── 应用设置：校验与保存 ──
	if _, err := lottery.SaveSettings(ctx, appID, lotterydomain.Supreme{Enabled: true, Scope: lotterydomain.SupremeScopeLifetime, Boost: 5}, "admin"); errCode(err) != 40000 {
		t.Fatalf("幅度超出范围应被拒，得到 %v", err)
	}
	if _, err := lottery.SaveSettings(ctx, appID, lotterydomain.Supreme{Enabled: true, Scope: lotterydomain.SupremeScopeTiers,
		Tiers: []lotterydomain.SupremeTier{{TierID: 999999, Boost: 2}}}, "admin"); errCode(err) != 40000 {
		t.Fatalf("不属于这个应用的档位应被拒，得到 %v", err)
	}
	grade := lotterydomain.SupremeGrade{Boost: 1.5, Floor: 1.1, Cap: 2.5}
	settings, err := lottery.SaveSettings(ctx, appID, lotterydomain.Supreme{Enabled: true, Scope: lotterydomain.SupremeScopeLifetime,
		Boost: grade.Boost, Floor: grade.Floor, Cap: grade.Cap, Title: "至尊加成"}, "admin")
	if err != nil {
		t.Fatalf("保存应用设置失败：%v", err)
	}
	if !settings.Supreme.Enabled || settings.Supreme.Boost != 1.5 || settings.UpdatedBy != "admin" || len(settings.Tiers) != 2 ||
		len(settings.Activities) != 1 || settings.Activities[0].Source != lotterydomain.SupremeSourceApp || !settings.Activities[0].Enabled {
		t.Fatalf("应用设置不对：%+v", settings)
	}
	if again, _ := lottery.Settings(ctx, appID); again.Supreme.Floor != 1.1 || again.Supreme.Cap != 2.5 {
		t.Fatalf("重新读出的设置不对：%+v", again)
	}

	// ── 永久会员：活动页与抽奖事务同一个加成后的系数 ──
	reading := state(lifer).Fortune
	want := grade.Apply(natural[lifer])
	if reading.Supreme == nil || math.Abs(reading.Factor-want) > 1e-9 || reading.Supreme.BaseFactor != natural[lifer] ||
		reading.Factor < 1.1 || reading.Supreme.BaseWinRate > reading.WinRate || reading.Supreme.Message == "" {
		t.Fatalf("永久会员的今日运气应为 %v（原 %v），得到 %+v %+v", want, natural[lifer], reading, reading.Supreme)
	}
	if got := draw(lifer); math.Abs(got.Fortune-reading.Factor) > 1e-9 || got.FortuneBase != natural[lifer] || got.Supreme == nil {
		t.Fatalf("抽奖用的系数 %+v 应与活动页 %v 一致", got, reading.Factor)
	}
	// ── 非永久会员不受影响 ──
	for _, user := range []int64{monthly, plain} {
		if got := state(user).Fortune; got.Supreme != nil || got.Factor != natural[user] {
			t.Fatalf("非永久会员不应有加成，得到 %+v", got)
		}
		if got := draw(user); got.Supreme != nil || got.FortuneBase != 0 || (natural[user] != 1 && math.Abs(got.Fortune-natural[user]) > 1e-9) {
			t.Fatalf("非永久会员抽奖用的应是自然运气 %v，得到 %+v", natural[user], got)
		}
	}

	// ── 模拟：真实用户与虚拟用户都能看到加成与对照组 ──
	options := lotterydomain.SimulationInput{Users: 1, DrawsPerUser: 200, Seed: 7}
	sim, err := lottery.Simulate(ctx, LotterySimulation{AppID: appID, ActivityID: activity.ID, UserID: lifer, Options: options})
	if err != nil {
		t.Fatal(err)
	}
	if sim.Supreme == nil || !sim.Supreme.Applied || sim.Supreme.Source != lotterydomain.SupremeSourceApp ||
		math.Abs(sim.Supreme.Factor-reading.Factor) > 1e-9 || sim.Supreme.Baseline == nil || sim.Current == nil || sim.Current.FortuneBase != natural[lifer] {
		t.Fatalf("真实用户的模拟应带上加成与对照组：%+v", sim.Supreme)
	}
	if sim.Result.Prizes[1].Rate < sim.Supreme.Baseline.Prizes[1].Rate {
		t.Fatalf("加成后积分奖不应更少：%v < %v", sim.Result.Prizes[1].Rate, sim.Supreme.Baseline.Prizes[1].Rate)
	}
	if sim, err := lottery.Simulate(ctx, LotterySimulation{AppID: appID, ActivityID: activity.ID, UserID: plain, Options: options}); err != nil ||
		sim.Supreme == nil || sim.Supreme.Applied || sim.Supreme.Baseline != nil {
		t.Fatalf("普通用户的模拟不应有加成：%+v %v", sim, err)
	}
	options.Users = 300
	virtual, err := lottery.Simulate(ctx, LotterySimulation{AppID: appID, ActivityID: activity.ID, Options: options,
		PerkFacts: &lotterydomain.PerkFacts{Lifetime: true, Member: true}})
	if err != nil || !virtual.Supreme.Applied || virtual.Result.Fortune == nil || virtual.Result.FortuneBase == nil ||
		virtual.Result.Fortune.Mean <= virtual.Result.FortuneBase.Mean || virtual.Supreme.Baseline == nil {
		t.Fatalf("虚拟永久会员的模拟应带加成：%+v %v", virtual, err)
	}

	// ── 活动单独关闭：以活动为准 ──
	current, err := lottery.GetActivity(ctx, appID, activity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Economy == nil {
		t.Fatal("积分账应照常算出")
	}
	input := activityInputOf(current)
	input.Operator = "admin"
	input.Strategy.Fortune = &lotterydomain.Fortune{Spread: 0.3, Supreme: &lotterydomain.Supreme{Scope: lotterydomain.SupremeScopeLifetime}}
	if _, err := lottery.UpdateActivity(ctx, activity.ID, input); err != nil {
		t.Fatal(err)
	}
	if got := state(lifer).Fortune; got.Supreme != nil || got.Factor != natural[lifer] {
		t.Fatalf("活动单独关闭后不应有加成，得到 %+v", got)
	}
	if settings, _ := lottery.Settings(ctx, appID); settings.Activities[0].Source != lotterydomain.SupremeSourceActivity || settings.Activities[0].Enabled {
		t.Fatalf("活动的来源应为单独设置且关闭，得到 %+v", settings.Activities)
	}
	// 调整运气差异不丢活动的设置
	if _, err := lottery.SetFortuneSpread(ctx, appID, activity.ID, 0.3, "admin"); err != nil {
		t.Fatal(err)
	}
	if saved, _ := lottery.GetActivity(ctx, appID, activity.ID); saved.Strategy.Fortune == nil || saved.Strategy.Fortune.Supreme == nil {
		t.Fatalf("调整运气差异后活动的至尊加成设置应保留，得到 %+v", saved.Strategy.Fortune)
	}
	// 活动里引用不存在的档位会被拒
	input.Strategy.Fortune.Supreme = &lotterydomain.Supreme{Enabled: true, Scope: lotterydomain.SupremeScopeTiers,
		Tiers: []lotterydomain.SupremeTier{{TierID: 999999, Boost: 2}}}
	if _, err := lottery.UpdateActivity(ctx, activity.ID, input); errCode(err) != 40000 {
		t.Fatalf("活动里引用不存在的档位应被拒，得到 %v", err)
	}

	// ── 按会员档位分级：月度会员所在档位加成，永久会员不在任何一档、顶层不加成 ──
	input.Strategy.Fortune = &lotterydomain.Fortune{Spread: 0.3}
	if _, err := lottery.UpdateActivity(ctx, activity.ID, input); err != nil {
		t.Fatal(err)
	}
	if _, err := lottery.SaveSettings(ctx, appID, lotterydomain.Supreme{Enabled: true, Scope: lotterydomain.SupremeScopeTiers,
		Tiers: []lotterydomain.SupremeTier{{TierID: monthlyPlan.TierID, Boost: 2}}}, "admin"); err != nil {
		t.Fatal(err)
	}
	if got := state(monthly).Fortune; got.Supreme == nil || got.Supreme.Tier != "月度会员" ||
		math.Abs(got.Factor-(lotterydomain.SupremeGrade{Boost: 2}).Apply(natural[monthly])) > 1e-9 {
		t.Fatalf("月度会员应按档位加成，得到 %+v %+v", got, got.Supreme)
	}
	if got := state(lifer).Fortune; got.Supreme != nil {
		t.Fatalf("永久会员不在所列档位时不应加成，得到 %+v", got.Supreme)
	}
	if got := state(plain).Fortune; got.Supreme != nil || got.Factor != natural[plain] {
		t.Fatalf("普通用户不受影响，得到 %+v", got)
	}
	// 只认永久开通时，限时会员落选
	if _, err := lottery.SaveSettings(ctx, appID, lotterydomain.Supreme{Enabled: true, Scope: lotterydomain.SupremeScopeTiers,
		Tiers: []lotterydomain.SupremeTier{{TierID: monthlyPlan.TierID, Boost: 2, LifetimeOnly: true}, {TierID: lifetimePlan.TierID, Floor: 1.6}}}, "admin"); err != nil {
		t.Fatal(err)
	}
	if got := state(monthly).Fortune; got.Supreme != nil {
		t.Fatalf("只认永久开通的档位不应给限时会员，得到 %+v", got.Supreme)
	}
	user, err := lottery.FortuneUser(ctx, appID, activity.ID, lifer)
	if err != nil || !user.Lifetime || len(user.Tiers) != 1 || user.Current.Supreme == nil || user.Current.Factor < 1.6 ||
		user.Base.Factor != natural[lifer] {
		t.Fatalf("管理端单人读数应带上加成：%+v %v", user, err)
	}
	overview, err := lottery.FortuneOverview(ctx, appID, activity.ID)
	if err != nil || overview.Supreme == nil || overview.SupremeSource != lotterydomain.SupremeSourceApp || overview.SupremeHigh < overview.High {
		t.Fatalf("总览应给出生效的至尊加成：%+v %v", overview, err)
	}

	// ── 应用关闭：恢复自然运气 ──
	if _, err := lottery.SaveSettings(ctx, appID, lotterydomain.Supreme{Scope: lotterydomain.SupremeScopeLifetime, Boost: 1.5}, "admin"); err != nil {
		t.Fatal(err)
	}
	if got := state(lifer).Fortune; got.Supreme != nil || got.Factor != natural[lifer] {
		t.Fatalf("关闭后应回到自然运气，得到 %+v", got)
	}
}
