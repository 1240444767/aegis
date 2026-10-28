package service

import (
	"math"
	"testing"

	"go.uber.org/zap"

	lotterydomain "aegis/internal/domain/lottery"
	pgrepo "aegis/internal/repository/postgres"
)

// 运气控制的端到端集成测试（真实 Postgres）：总览 → 定档（活动页与抽奖事务同一个系数）→ 全站偏移与个人定档的优先级
// → 预演 → 记录 → 全部撤销恢复自然运气 → 重抽 → 撤销不存在的 → 跨应用用户 → 关闭 / 重开每日运气。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestLotteryFortuneControlIntegration
func TestLotteryFortuneControlIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	lottery := NewLotteryService(zap.NewNop(), pg, nil)
	const appID = int64(40300)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'lottery-fortune'), ($1 + 1, 'other') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	erin := insertTestUser(t, ctx, pool, appID, "fortune-erin", "0")
	finn := insertTestUser(t, ctx, pool, appID, "fortune-finn", "0")
	stranger := insertTestUser(t, ctx, pool, appID+1, "fortune-stranger", "0")

	prizes := make([]lotterydomain.PrizeInput, 0, 8)
	for position := 0; position < 8; position++ {
		prizes = append(prizes, lotterydomain.PrizeInput{Position: position, Reward: lotterydomain.Reward{Type: lotterydomain.KindNone}, Quantity: -1})
	}
	prizes[0] = lotterydomain.PrizeInput{Position: 0, Reward: lotterydomain.Reward{Type: lotterydomain.KindExperience, Amount: 5},
		Weight: 50, Quantity: -1, Fallback: true}
	prizes[1] = lotterydomain.PrizeInput{Position: 1, Reward: lotterydomain.Reward{Type: lotterydomain.KindPoints, Amount: 3}, Weight: 50, Quantity: -1}
	activity, err := lottery.CreateActivity(ctx, lotterydomain.ActivityInput{
		AppID: appID, Name: "运气控制", UIMode: lotterydomain.UIModeGrid, Status: lotterydomain.StatusActive,
		FreeDaily: 10, PityPosition: -1, Prizes: prizes, Operator: "admin",
		Strategy: lotterydomain.Strategy{Fortune: &lotterydomain.Fortune{Spread: 0.3}},
	})
	if err != nil {
		t.Fatalf("新建活动失败：%v", err)
	}
	control := func(input lotterydomain.ControlInput) []lotterydomain.FortuneControl {
		t.Helper()
		input.AppID, input.ActivityID, input.Operator = appID, activity.ID, "admin"
		items, err := lottery.CreateFortuneControls(ctx, input)
		if err != nil {
			t.Fatalf("下干预失败：%v", err)
		}
		return items
	}
	reading := func(userID int64) *lotterydomain.FortuneReading {
		t.Helper()
		state, err := lottery.State(ctx, appID, userID, activity.ID)
		if err != nil {
			t.Fatal(err)
		}
		return state.Fortune
	}
	draw := func(userID int64) float64 {
		t.Helper()
		result, err := lottery.Draw(ctx, LotteryDrawRequest{AppID: appID, UserID: userID, ActivityID: activity.ID})
		if err != nil {
			t.Fatalf("抽奖失败：%v", err)
		}
		var factor float64
		if err := pool.QueryRow(ctx, `SELECT COALESCE((trace->>'fortune')::float8, 0) FROM lottery_draws WHERE id = $1`,
			result.Draw.ID).Scan(&factor); err != nil {
			t.Fatal(err)
		}
		return factor
	}

	// ── 总览 ──
	overview, err := lottery.FortuneOverview(ctx, appID, activity.ID)
	if err != nil || !overview.Enabled || overview.Spread != 0.3 || len(overview.Levels) != 4 || overview.Today.Users != 0 {
		t.Fatalf("总览不对：%+v %v", overview, err)
	}
	natural := reading(erin)
	if got := draw(erin); math.Abs(got-natural.Factor) > 1e-9 {
		t.Fatalf("没有干预时抽奖用的系数 %v 应与活动页 %v 一致", got, natural.Factor)
	}

	// ── 个人定档：活动页与抽奖事务同一个系数 ──
	pinned := control(lotterydomain.ControlInput{UserIDs: []int64{erin}, Kind: lotterydomain.ControlPin, Level: lotterydomain.FortuneBurst, Note: "测试"})
	if len(pinned) != 1 || pinned[0].Status != lotterydomain.ControlActive || pinned[0].Account != "fortune-erin" || pinned[0].Salt != "" {
		t.Fatalf("定档记录不对：%+v", pinned)
	}
	burst := reading(erin)
	if burst.Level != lotterydomain.FortuneBurst {
		t.Fatalf("定档后应是运气大爆发，得到 %+v", burst)
	}
	if got := draw(erin); math.Abs(got-burst.Factor) > 1e-9 {
		t.Fatalf("定档后抽奖用的系数 %v 应与活动页 %v 一致", got, burst.Factor)
	}
	user, err := lottery.FortuneUser(ctx, appID, activity.ID, erin)
	if err != nil || user.Base.Factor != natural.Factor || user.Current.Level != lotterydomain.FortuneBurst || len(user.Applied) != 1 || len(user.Controls) != 1 {
		t.Fatalf("单人读数不对：%+v %v", user, err)
	}

	// ── 全站偏移：别人整体变倒霉，个人定档的不受影响 ──
	control(lotterydomain.ControlInput{AllUsers: true, Kind: lotterydomain.ControlShift, Shift: -4})
	if got := reading(finn); got.Level != lotterydomain.FortuneQuiet || math.Abs(got.Factor-overview.Low) > 1e-6 {
		t.Fatalf("全站 -4 后应是最低系数 %v，得到 %+v", overview.Low, got)
	}
	if reading(erin).Level != lotterydomain.FortuneBurst {
		t.Fatal("个人定档应优先于全站偏移")
	}
	overview, _ = lottery.FortuneOverview(ctx, appID, activity.ID)
	if overview.Today.Users != 1 || overview.Today.Controlled != 1 || overview.Today.Levels[lotterydomain.FortuneBurst] != 1 ||
		len(overview.Global) != 1 || overview.Counts.ActiveGlobal != 1 || overview.Counts.ActiveUsers != 1 {
		t.Fatalf("总览的今日分布与干预数不对：%+v", overview)
	}

	// ── 预演：不落库 ──
	preview, err := lottery.PreviewFortuneControl(ctx, lotterydomain.ControlInput{AppID: appID, ActivityID: activity.ID,
		UserIDs: []int64{finn}, Kind: lotterydomain.ControlPin, Level: lotterydomain.FortuneLucky})
	if err != nil || preview.Users != 1 || preview.Levels[lotterydomain.FortuneLucky] != 1 {
		t.Fatalf("预演不对：%+v %v", preview, err)
	}
	if reading(finn).Level != lotterydomain.FortuneQuiet {
		t.Fatal("预演不应落库")
	}

	// ── 记录 ──
	page, err := lottery.ListFortuneControls(ctx, pgrepo.FortuneControlQuery{AppID: appID, ActivityID: activity.ID, Status: lotterydomain.ControlActive})
	if err != nil || page.Total != 2 {
		t.Fatalf("应有 2 条生效中的干预，得到 %+v %v", page, err)
	}

	// ── 全部撤销：恢复自然运气 ──
	if count, err := lottery.RevokeFortuneControls(ctx, appID, activity.ID, nil, true, "admin"); err != nil || count != 2 {
		t.Fatalf("应撤销 2 条，得到 %d %v", count, err)
	}
	if got := reading(erin); got.Factor != natural.Factor {
		t.Fatalf("撤销后应回到自然运气 %v，得到 %v", natural.Factor, got.Factor)
	}
	if _, err := lottery.RevokeFortuneControls(ctx, appID, activity.ID, []int64{pinned[0].ID}, false, "admin"); errCode(err) != errCodeFortuneControlNotFound {
		t.Fatalf("重复撤销应报 %d，得到 %v", errCodeFortuneControlNotFound, err)
	}
	if page, _ := lottery.ListFortuneControls(ctx, pgrepo.FortuneControlQuery{AppID: appID, ActivityID: activity.ID, Status: lotterydomain.ControlRevoked}); page.Total != 2 {
		t.Fatalf("撤销的干预应留痕，得到 %+v", page)
	}

	// ── 重抽：换一个手气 ──
	control(lotterydomain.ControlInput{UserIDs: []int64{erin}, Kind: lotterydomain.ControlReroll})
	user, _ = lottery.FortuneUser(ctx, appID, activity.ID, erin)
	if user.Z == user.BaseZ || reading(erin).Factor == natural.Factor {
		t.Fatalf("重抽后应换一个系数，得到 %+v", user)
	}

	// ── 跨应用用户、关闭每日运气 ──
	if _, err := lottery.CreateFortuneControls(ctx, lotterydomain.ControlInput{AppID: appID, ActivityID: activity.ID,
		UserIDs: []int64{stranger}, Kind: lotterydomain.ControlReroll}); errCode(err) != 40401 {
		t.Fatalf("别的应用的用户应被拒，得到 %v", err)
	}
	off, err := lottery.SetFortuneSpread(ctx, appID, activity.ID, 0, "admin")
	if err != nil || off.Enabled || reading(erin) != nil {
		t.Fatalf("关闭每日运气后不应再有运气，得到 %+v %v", off, err)
	}
	if _, err := lottery.CreateFortuneControls(ctx, lotterydomain.ControlInput{AppID: appID, ActivityID: activity.ID,
		AllUsers: true, Kind: lotterydomain.ControlReroll}); errCode(err) != 40000 {
		t.Fatalf("没开每日运气时应拒绝干预，得到 %v", err)
	}
	on, err := lottery.SetFortuneSpread(ctx, appID, activity.ID, 0.5, "admin")
	if err != nil || !on.Enabled || on.Spread != 0.5 {
		t.Fatalf("重开每日运气失败：%+v %v", on, err)
	}
	if saved, _ := lottery.GetActivity(ctx, appID, activity.ID); saved.Prizes[1].Weight != 50 || saved.FreeDaily != 10 {
		t.Fatalf("调整运气差异不应动到奖盘与次数，得到 %+v", saved)
	}
}
