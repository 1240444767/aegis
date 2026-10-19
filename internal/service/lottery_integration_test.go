package service

import (
	"context"
	"fmt"
	"math"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	"aegis/internal/config"
	cardkeydomain "aegis/internal/domain/cardkey"
	lotterydomain "aegis/internal/domain/lottery"
	rewardedad "aegis/internal/domain/rewardedad"
	vipdomain "aegis/internal/domain/vip"
	pgrepo "aegis/internal/repository/postgres"
)

// 抽奖的端到端集成测试：真实 Postgres，跑全部迁移（两遍），再走一遍
// 「保存时的校验 → 草稿不可见 → 免费次数（幂等回放、用完报错）→ 视频抽奖（激励广告发次数、用 bonus 抽）→
// 积分抽奖（不足、扣积分、每日上限）→ 保底 → 限量奖品被抢光改发兜底（并发）→ 同一 requestId 并发 →
// 会员 / 功能 / 人工发放 → 删除与统计」，最后单独验证旧表形状的搬迁。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestLotteryIntegration
func TestLotteryIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	vip := NewVipService(zap.NewNop(), pg)
	rewarded := NewRewardedAdService(zap.NewNop(), pg, config.Config{})
	lottery := NewLotteryService(zap.NewNop(), pg, rewarded)
	const appID = int64(40000)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'lottery-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	if _, err := vip.AdminSaveFeature(ctx, vipdomain.FeatureMutation{AppID: appID, Tag: "online_music", Name: ptrTo("在线音乐")}); err != nil {
		t.Fatal(err)
	}
	trialPlan := mustSavePlan(t, vip, vipdomain.PlanMutation{AppID: appID, Name: ptrTo("试用"),
		Kind: ptrTo(vipdomain.KindTrial), DurationDays: ptrTo(3), Price: ptrTo(decimal.Zero)})

	alice := insertTestUser(t, ctx, pool, appID, "lottery-alice", "0")
	bob := insertTestUser(t, ctx, pool, appID, "lottery-bob", "0")

	// 激励广告：client 模式下上报即发，场景发 1 次抽奖
	if _, err := rewarded.SaveConfig(ctx, rewardedad.SaveConfigInput{
		AppID: appID, Enabled: true, VerifyMode: rewardedad.VerifyClient, DailyLimit: 0, Operator: "admin",
		Scenes: []rewardedad.Scene{
			{Key: "lottery", Name: "看视频抽奖", PlacementID: "4892299192457961", Enabled: true, DailyLimit: 10,
				Rewards: []rewardedad.Reward{{Type: cardkeydomain.RewardLotteryDraws, Amount: 1}}},
			{Key: "points", Name: "看视频领积分", PlacementID: "4892299192457961", Enabled: true,
				Rewards: []rewardedad.Reward{{Type: cardkeydomain.RewardIntegral, Amount: 5}}},
		},
	}, "lottery-test"); err != nil {
		t.Fatal(err)
	}

	grid := func(name string, special lotterydomain.PrizeInput) lotterydomain.ActivityInput {
		prizes := make([]lotterydomain.PrizeInput, 0, 8)
		for position := 0; position < 8; position++ {
			prizes = append(prizes, lotterydomain.PrizeInput{Position: position, Reward: lotterydomain.Reward{Type: lotterydomain.KindNone},
				Weight: 1, Quantity: -1, Fallback: position == 0})
		}
		special.Position = 1
		prizes[1] = special
		return lotterydomain.ActivityInput{AppID: appID, Name: name, UIMode: lotterydomain.UIModeGrid, Status: lotterydomain.StatusActive,
			FreeDaily: 1, PityPosition: -1, Prizes: prizes, Operator: "admin"}
	}
	mustCreate := func(input lotterydomain.ActivityInput) *lotterydomain.AdminActivity {
		t.Helper()
		activity, err := lottery.CreateActivity(ctx, input)
		if err != nil {
			t.Fatalf("新建活动失败：%v", err)
		}
		return activity
	}
	draw := func(userID, activityID int64, source, requestID string) (*lotterydomain.DrawResult, error) {
		return lottery.Draw(ctx, LotteryDrawRequest{AppID: appID, UserID: userID, ActivityID: activityID, Source: source, RequestID: requestID})
	}
	mustDraw := func(userID, activityID int64, source, requestID string) *lotterydomain.DrawResult {
		t.Helper()
		result, err := draw(userID, activityID, source, requestID)
		if err != nil {
			t.Fatalf("抽奖失败（%s）：%v", source, err)
		}
		return result
	}
	integral := func(userID int64) int64 {
		var value int64
		if err := pool.QueryRow(ctx, `SELECT integral FROM users WHERE id = $1`, userID).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	setIntegral := func(userID, value int64) {
		t.Helper()
		if _, err := pool.Exec(ctx, `UPDATE users SET integral = $2 WHERE id = $1`, userID, value); err != nil {
			t.Fatal(err)
		}
	}
	giveBonus := func(userID int64, count int) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO lottery_draw_grants (appid, user_id, balance, total_granted) VALUES ($1, $2, $3::bigint, $3::bigint)
ON CONFLICT (appid, user_id) DO UPDATE SET balance = lottery_draw_grants.balance + EXCLUDED.balance`, appID, userID, count); err != nil {
			t.Fatal(err)
		}
	}

	// ── 保存时的校验 ──
	bad := grid("坏活动", lotterydomain.PrizeInput{Reward: lotterydomain.Reward{Type: lotterydomain.KindPoints, Amount: 2}, Weight: 1, Quantity: -1})
	bad.Prizes = bad.Prizes[:7]
	if _, err := lottery.CreateActivity(ctx, bad); errCode(err) != 40000 {
		t.Fatalf("九宫格少一格应被拒，得到 %v", err)
	}
	bad = grid("坏活动", lotterydomain.PrizeInput{Reward: lotterydomain.Reward{Type: lotterydomain.KindVipPlan, RefID: trialPlan.ID}, Weight: 1, Quantity: -1})
	if _, err := lottery.CreateActivity(ctx, bad); errCode(err) != 40000 {
		t.Fatalf("试用套餐应被拒，得到 %v", err)
	}
	bad = grid("坏活动", lotterydomain.PrizeInput{Reward: lotterydomain.Reward{Type: lotterydomain.KindFeature, Tag: "missing", Amount: 7}, Weight: 1, Quantity: -1})
	if _, err := lottery.CreateActivity(ctx, bad); errCode(err) != 40000 {
		t.Fatalf("未登记的功能应被拒，得到 %v", err)
	}
	bad = grid("坏活动", lotterydomain.PrizeInput{Reward: lotterydomain.Reward{Type: lotterydomain.KindPoints, Amount: 2}, Weight: 1, Quantity: -1})
	bad.VideoScene = "points"
	if _, err := lottery.CreateActivity(ctx, bad); errCode(err) != 40000 {
		t.Fatalf("不发抽奖次数的广告场景应被拒，得到 %v", err)
	}

	// ── 主活动：草稿不可见，上线后可抽 ──
	main := grid("幸运抽奖", lotterydomain.PrizeInput{Reward: lotterydomain.Reward{Type: lotterydomain.KindPoints, Amount: 2}, Weight: 1, Quantity: -1})
	main.Status = lotterydomain.StatusDraft
	main.VideoScene = "lottery"
	main.PityThreshold, main.PityPosition = 5, 1
	activity := mustCreate(main)
	if activity.Prizes[1].Name != "2 积分" || activity.PityPrizeID == nil || *activity.PityPrizeID != activity.Prizes[1].ID {
		t.Fatalf("默认奖品名与保底奖应落库，得到 %+v", activity)
	}
	if _, err := lottery.State(ctx, appID, alice, activity.ID); errCode(err) != errCodeLotteryNotFound {
		t.Fatalf("草稿对用户不可见，得到 %v", err)
	}
	list, err := lottery.List(ctx, appID)
	if err != nil || len(list.Activities) != 0 {
		t.Fatalf("草稿不进列表，得到 %+v %v", list, err)
	}
	if _, err := lottery.SetStatus(ctx, appID, activity.ID, lotterydomain.StatusActive); err != nil {
		t.Fatal(err)
	}
	state, err := lottery.State(ctx, appID, alice, activity.ID)
	if err != nil {
		t.Fatal(err)
	}
	var total float64
	for _, prize := range state.Activity.Prizes {
		total += prize.Probability
	}
	if math.Abs(total-100) > 0.1 || state.Chances.Next != lotterydomain.SourceFree || state.Chances.Free.Remaining != 1 {
		t.Fatalf("活动页：概率合计 100、先用免费，得到 %.2f %+v", total, state.Chances)
	}
	if !state.Chances.Video.Enabled || state.Chances.Video.Remaining != 10 || state.Chances.Video.DrawsPerView != 1 ||
		state.Chances.Video.UserID == "" || state.Pity == nil || state.Pity.Remaining != 5 {
		t.Fatalf("视频抽奖与保底进度，得到 %+v %+v", state.Chances.Video, state.Pity)
	}

	// ── 免费次数：幂等回放、用完报错 ──
	first := mustDraw(alice, activity.ID, "", "req-free-1")
	if first.Draw.Source != lotterydomain.SourceFree || first.Replayed || first.Chances.Free.Remaining != 0 {
		t.Fatalf("第一次用免费次数，得到 %+v", first)
	}
	replay := mustDraw(alice, activity.ID, "", "req-free-1")
	if !replay.Replayed || replay.Draw.DrawNo != first.Draw.DrawNo {
		t.Fatalf("同一个 requestId 原样回放，得到 %+v", replay)
	}
	if _, err := draw(alice, activity.ID, lotterydomain.SourceFree, ""); errCode(err) != errCodeLotteryFreeExhausted {
		t.Fatalf("免费次数用完应报 42913，得到 %v", err)
	}
	if _, err := draw(alice, activity.ID, "", ""); errCode(err) != errCodeLotteryNoChance {
		t.Fatalf("没有任何次数应报 42915，得到 %v", err)
	}
	if _, err := draw(alice, activity.ID, lotterydomain.SourcePoints, ""); errCode(err) != errCodeLotterySourceUnavailable {
		t.Fatalf("未开放积分抽奖应报 40928，得到 %v", err)
	}

	// ── 视频抽奖：看完广告到账的是抽奖次数，再用 bonus 抽 ──
	claim, err := rewarded.Claim(ctx, RewardedAdClaimInput{AppID: appID, UserID: alice, Scene: "lottery", TransID: "lottery-trans-0001"})
	if err != nil || claim.Status != rewardedad.StatusGranted {
		t.Fatalf("看完视频应发抽奖次数，得到 %+v %v", claim, err)
	}
	state, err = lottery.State(ctx, appID, alice, activity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Chances.Bonus != 1 || state.Chances.Next != lotterydomain.SourceBonus || state.Chances.Video.Remaining != 9 {
		t.Fatalf("视频看完后有 1 次 bonus、视频还剩 9 次，得到 %+v", state.Chances)
	}
	videoDraw := mustDraw(alice, activity.ID, lotterydomain.SourceBonus, "req-video-1")
	if videoDraw.Draw.Source != lotterydomain.SourceBonus || videoDraw.Chances.Bonus != 0 {
		t.Fatalf("用 bonus 抽，抽完余额为 0，得到 %+v", videoDraw)
	}

	// ── 积分抽奖 ──
	main.Status = lotterydomain.StatusActive
	main.PointsCost, main.PointsDaily = 10, 2
	main.Prizes = nil
	for _, prize := range activity.Prizes {
		main.Prizes = append(main.Prizes, lotterydomain.PrizeInput{ID: prize.ID, Name: prize.Name, Reward: prize.Reward,
			Quantity: prize.Quantity, Weight: prize.Weight, Position: prize.Position, Fallback: prize.Fallback})
	}
	if _, err := lottery.UpdateActivity(ctx, activity.ID, main); err != nil {
		t.Fatal(err)
	}
	if _, err := draw(bob, activity.ID, lotterydomain.SourcePoints, ""); errCode(err) != errCodePointsInsufficient {
		t.Fatalf("积分不足应报 40307，得到 %v", err)
	}
	setIntegral(bob, 100)
	pointsDraw := mustDraw(bob, activity.ID, lotterydomain.SourcePoints, "")
	won := int64(0)
	if pointsDraw.Draw.PrizeKind == lotterydomain.KindPoints {
		won = 2
	}
	if got := integral(bob); got != 90+won || pointsDraw.Draw.PointsCost != 10 {
		t.Fatalf("积分抽奖扣 10 积分，得到余额 %d %+v", got, pointsDraw.Draw)
	}
	var consumed int64
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM integral_transactions WHERE user_id = $1 AND category = 'lottery' AND amount = -10`,
		bob).Scan(&consumed); err != nil || consumed != 1 {
		t.Fatalf("积分流水应记一笔抽奖消耗，得到 %d %v", consumed, err)
	}
	mustDraw(bob, activity.ID, lotterydomain.SourcePoints, "")
	if _, err := draw(bob, activity.ID, lotterydomain.SourcePoints, ""); errCode(err) != errCodeLotteryPointsLimit {
		t.Fatalf("积分抽奖每天 2 次，第 3 次应报 42914，得到 %v", err)
	}

	// ── 保底：连续 4 次未中，第 5 次必中 ──
	pity := grid("保底活动", lotterydomain.PrizeInput{Reward: lotterydomain.Reward{Type: lotterydomain.KindPoints, Amount: 3}, Weight: 0, Quantity: -1})
	pity.FreeDaily = 0
	pity.PityThreshold, pity.PityPosition = 5, 1
	pityActivity := mustCreate(pity)
	carol := insertTestUser(t, ctx, pool, appID, "lottery-carol", "0")
	giveBonus(carol, 5)
	for round := 1; round <= 5; round++ {
		result := mustDraw(carol, pityActivity.ID, lotterydomain.SourceBonus, "")
		if round < 5 {
			if result.Draw.PrizeKind != lotterydomain.KindNone || result.Pity == nil || result.Pity.Remaining != 5-round {
				t.Fatalf("第 %d 次应未中、保底还剩 %d 次，得到 %+v %+v", round, 5-round, result.Draw, result.Pity)
			}
			continue
		}
		if !result.Draw.Pity || result.Draw.PrizeKind != lotterydomain.KindPoints || integral(carol) != 3 ||
			result.Pity == nil || result.Pity.Remaining != 5 {
			t.Fatalf("第 5 次必中保底奖并清零进度，得到 %+v %+v", result.Draw, result.Pity)
		}
	}

	// ── 限量奖品被抢光：并发抽奖只出 3 个，其余改发兜底 ──
	stock := grid("限量活动", lotterydomain.PrizeInput{Reward: lotterydomain.Reward{Type: lotterydomain.KindPoints, Amount: 50}, Weight: 1_000_000, Quantity: 3})
	stock.FreeDaily = 0
	stockActivity := mustCreate(stock)
	racers := make([]int64, 10)
	for i := range racers {
		racers[i] = insertTestUser(t, ctx, pool, appID, fmt.Sprintf("lottery-racer-%d", i), "0")
		giveBonus(racers[i], 1)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	winners := 0
	for _, user := range racers {
		wg.Add(1)
		go func(user int64) {
			defer wg.Done()
			result, err := draw(user, stockActivity.ID, lotterydomain.SourceBonus, "")
			if err != nil {
				t.Errorf("并发抽奖失败：%v", err)
				return
			}
			if result.Draw.PrizeKind == lotterydomain.KindPoints {
				mu.Lock()
				winners++
				mu.Unlock()
			}
		}(user)
	}
	wg.Wait()
	var used int64
	if err := pool.QueryRow(ctx, `SELECT used FROM lottery_prizes WHERE activity_id = $1 AND kind = 'points'`, stockActivity.ID).Scan(&used); err != nil {
		t.Fatal(err)
	}
	if winners != 3 || used != 3 {
		t.Fatalf("库存 3 只能抽中 3 次，得到 winners=%d used=%d", winners, used)
	}
	// 售罄后不能把库存改到已抽中数量以下
	stockEdit := stock
	stockEdit.Prizes = nil
	current, err := lottery.GetActivity(ctx, appID, stockActivity.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, prize := range current.Prizes {
		input := lotterydomain.PrizeInput{ID: prize.ID, Reward: prize.Reward, Quantity: prize.Quantity, Weight: prize.Weight,
			Position: prize.Position, Fallback: prize.Fallback}
		if prize.Kind == lotterydomain.KindPoints {
			input.Quantity = 2
		}
		stockEdit.Prizes = append(stockEdit.Prizes, input)
	}
	if _, err := lottery.UpdateActivity(ctx, stockActivity.ID, stockEdit); errCode(err) != 40000 {
		t.Fatalf("库存不能少于已抽中数量，得到 %v", err)
	}

	// ── 同一个 requestId 并发：只抽一次 ──
	dave := insertTestUser(t, ctx, pool, appID, "lottery-dave", "0")
	giveBonus(dave, 5)
	draws := make(chan string, 5)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := draw(dave, stockActivity.ID, lotterydomain.SourceBonus, "same-request")
			if err != nil {
				if errCode(err) != errCodeLotteryDrawing {
					t.Errorf("同一 requestId 并发只允许回放或 40925，得到 %v", err)
				}
				return
			}
			draws <- result.Draw.DrawNo
		}()
	}
	wg.Wait()
	close(draws)
	numbers := map[string]bool{}
	for no := range draws {
		numbers[no] = true
	}
	var daveDraws, daveBonus int64
	_ = pool.QueryRow(ctx, `SELECT COUNT(*) FROM lottery_draws WHERE user_id = $1`, dave).Scan(&daveDraws)
	_ = pool.QueryRow(ctx, `SELECT balance FROM lottery_draw_grants WHERE user_id = $1`, dave).Scan(&daveBonus)
	if len(numbers) != 1 || daveDraws != 1 || daveBonus != 4 {
		t.Fatalf("同一 requestId 只抽一次、只扣一次，得到 %v draws=%d bonus=%d", numbers, daveDraws, daveBonus)
	}

	// ── 会员 / 功能 / 人工发放 ──
	vipActivity := mustCreate(grid("会员活动", lotterydomain.PrizeInput{Reward: lotterydomain.Reward{Type: lotterydomain.KindVipDays, Amount: 7},
		Weight: 1_000_000, Quantity: -1}))
	featureActivity := mustCreate(grid("功能活动", lotterydomain.PrizeInput{Reward: lotterydomain.Reward{Type: lotterydomain.KindFeature, Tag: "online_music", Amount: 3},
		Weight: 1_000_000, Quantity: -1}))
	manualActivity := mustCreate(grid("实物活动", lotterydomain.PrizeInput{Name: "定制贴纸",
		Reward: lotterydomain.Reward{Type: lotterydomain.KindManual, Text: "定制贴纸一套"}, Weight: 1_000_000, Quantity: -1}))
	erin := insertTestUser(t, ctx, pool, appID, "lottery-erin", "0")
	won7 := mustDraw(erin, vipActivity.ID, "", "")
	if won7.Draw.PrizeKind != lotterydomain.KindVipDays || len(won7.Draw.Results) != 1 || won7.Draw.Results[0].TransactionNo == "" {
		t.Fatalf("应抽中会员 7 天并到账，得到 %+v", won7.Draw)
	}
	ent := mustEntitlement(t, vip, appID, erin)
	if !ent.IsVIP || ent.Source != vipdomain.SourceLottery {
		t.Fatalf("抽中的会员来源是 lottery，得到 %+v", ent)
	}
	unlock := mustDraw(erin, featureActivity.ID, "", "")
	if unlock.Draw.PrizeKind != lotterydomain.KindFeature || unlock.Draw.Results[0].ExpireAt == nil {
		t.Fatalf("应抽中功能解锁 3 天，得到 %+v", unlock.Draw)
	}
	merch := mustDraw(erin, manualActivity.ID, "", "")
	if merch.Draw.Status != lotterydomain.DrawPending || !merch.Draw.Results[0].Pending {
		t.Fatalf("人工发放的奖品待发放，得到 %+v", merch.Draw)
	}
	fulfilled, err := lottery.Fulfill(ctx, lotterydomain.HandleInput{AppID: appID, DrawID: merch.Draw.ID, Note: "快递单号 SF123", Operator: "admin"})
	if err != nil || fulfilled.Status != lotterydomain.DrawFulfilled || fulfilled.Note != "快递单号 SF123" || fulfilled.Results[0].Pending {
		t.Fatalf("标记已发放，得到 %+v %v", fulfilled, err)
	}
	if _, err := lottery.Fulfill(ctx, lotterydomain.HandleInput{AppID: appID, DrawID: merch.Draw.ID}); errCode(err) != errCodeLotteryDrawHandled {
		t.Fatalf("重复标记应报 40926，得到 %v", err)
	}

	// ── 我的记录、删除、统计 ──
	mine, err := lottery.MyDraws(ctx, appID, alice, activity.ID, 1, 20)
	if err != nil || mine.Total != 2 || mine.Items[0].Account != "" {
		t.Fatalf("alice 在主活动抽了 2 次（免费 + 视频），得到 %+v %v", mine, err)
	}
	if err := lottery.DeleteActivity(ctx, appID, activity.ID); errCode(err) != errCodeLotteryHasDraws {
		t.Fatalf("有记录的活动不能删除，得到 %v", err)
	}
	empty := mustCreate(grid("空活动", lotterydomain.PrizeInput{Reward: lotterydomain.Reward{Type: lotterydomain.KindPoints, Amount: 1}, Weight: 1, Quantity: -1}))
	if err := lottery.DeleteActivity(ctx, appID, empty.ID); err != nil {
		t.Fatalf("没有记录的活动可以删除：%v", err)
	}
	stats, err := lottery.Stats(ctx, appID, activity.ID, 7)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Summary.TotalDraws != 4 || stats.Summary.PointsSpent != 20 || len(stats.Trend) != 7 || len(stats.Prizes) != 8 {
		t.Fatalf("主活动共 4 次（免费 1、视频 1、积分 2），得到 %+v", stats.Summary)
	}

	testLotteryLegacyMigration(t, ctx, pool, appID, alice)
}

// testLotteryLegacyMigration 旧表形状（000024 + 旧代码写入的数据）经 000110 搬成新形状，且可重复执行。
func testLotteryLegacyMigration(t *testing.T, ctx context.Context, pool *pgxpool.Pool, appID, userID int64) {
	t.Helper()
	read := func(name string) string {
		content, err := os.ReadFile("../../migrations/postgres/" + name)
		if err != nil {
			t.Fatal(err)
		}
		return string(content)
	}
	if _, err := pool.Exec(ctx, `DROP TABLE IF EXISTS lottery_user_states, lottery_draws, lottery_prizes, lottery_activities CASCADE`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, read("000024_add_lottery_system.up.sql")); err != nil {
		t.Fatal(err)
	}
	var activityID, pointsPrize, itemPrize, nonePrize int64
	if err := pool.QueryRow(ctx, `INSERT INTO lottery_activities (appid, name, ui_mode, status, cost_type, cost_amount, daily_limit, start_time, end_time)
VALUES ($1, '旧活动', 'wheel', 'active', 'points', 15, 3, NOW() - INTERVAL '1 day', NOW() + INTERVAL '1 day') RETURNING id`, appID).Scan(&activityID); err != nil {
		t.Fatal(err)
	}
	insertPrize := func(name, kind, value string, position int) int64 {
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO lottery_prizes (activity_id, name, type, value, weight, position)
VALUES ($1, $2, $3, $4, 1, $5) RETURNING id`, activityID, name, kind, value, position).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	pointsPrize = insertPrize("5积分", "points", "5", 0)
	itemPrize = insertPrize("马克杯", "item", "", 1)
	nonePrize = insertPrize("谢谢参与", "custom", "", 2)
	if _, err := pool.Exec(ctx, `INSERT INTO lottery_draws (activity_id, user_id, prize_id, prize_snapshot, status)
VALUES ($1, $2, $3, '{"name":"5积分"}', 'awarded'), ($1, $2, $4, '{"name":"马克杯"}', 'awarded'), ($1, 99999999, $5, '{}', 'awarded')`,
		activityID, userID, pointsPrize, itemPrize, nonePrize); err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 2; round++ {
		if _, err := pool.Exec(ctx, read("000110_lottery_rework.up.sql")); err != nil {
			t.Fatalf("round %d 000110: %v", round+1, err)
		}
	}
	pg := pgrepo.New(pool)
	activity, err := pg.GetLotteryActivity(ctx, appID, activityID)
	if err != nil {
		t.Fatal(err)
	}
	if activity.PointsCost != 15 || activity.PointsDaily != 3 || activity.FreeDaily != 0 || activity.UIMode != lotterydomain.UIModeWheel {
		t.Fatalf("积分活动搬成 pointsCost=15、每日 3 次，得到 %+v", activity)
	}
	prizes, err := pg.ListLotteryPrizes(ctx, activityID)
	if err != nil || len(prizes) != 3 {
		t.Fatalf("奖品 3 个，得到 %+v %v", prizes, err)
	}
	if prizes[0].Kind != lotterydomain.KindPoints || prizes[0].Reward.Amount != 5 ||
		prizes[1].Kind != lotterydomain.KindManual || prizes[1].Reward.Text != "马克杯" || prizes[2].Kind != lotterydomain.KindNone {
		t.Fatalf("奖品类型搬迁不对：%+v", prizes)
	}
	page, err := pg.ListLotteryDraws(ctx, lotterydomain.DrawQuery{AppID: appID, ActivityID: activityID})
	if err != nil || page.Total != 2 {
		t.Fatalf("用户已不存在的记录被清掉，剩 2 条，得到 %+v %v", page, err)
	}
	statuses := map[string]string{}
	for _, item := range page.Items {
		statuses[item.PrizeName] = item.Status
		if item.DrawNo == "" {
			t.Fatalf("旧记录要补上单号：%+v", item)
		}
	}
	if statuses["5积分"] != lotterydomain.DrawGranted || statuses["马克杯"] != lotterydomain.DrawPending {
		t.Fatalf("积分奖品已到账、实物待发放，得到 %v", statuses)
	}
	var participants bool
	if err := pool.QueryRow(ctx, `SELECT to_regclass('lottery_participants') IS NOT NULL`).Scan(&participants); err != nil || participants {
		t.Fatalf("报名表应已删除，得到 %v %v", participants, err)
	}
}
