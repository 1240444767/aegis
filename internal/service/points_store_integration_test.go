package service

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	"aegis/internal/domain/pointstore"
	vipdomain "aegis/internal/domain/vip"
	pgrepo "aegis/internal/repository/postgres"
)

// 积分商城的端到端集成测试：真实 Postgres，跑全部迁移（两遍），再走一遍
// 「开关 → 建商品（含引用校验）→ 积分不足 → 兑换功能解锁（不成为会员、顺延、幂等、限兑）→
// 库存 → 人工发放（取消退积分、发放）→ 永久解锁与已拥有 → 管理员授予与收回 →
// 永久套餐与已拥有 → 并发抢库存与同一 requestId 并发 → 统计」。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestPointsStoreIntegration
func TestPointsStoreIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	vip := NewVipService(zap.NewNop(), pg)
	store := NewPointsStoreService(zap.NewNop(), pg)
	const appID = int64(30000)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'points-store-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	for _, tag := range []string{"online_music", "doubao_analyze", "export"} {
		if _, err := vip.AdminSaveFeature(ctx, vipdomain.FeatureMutation{AppID: appID, Tag: tag, Name: ptrTo(tag)}); err != nil {
			t.Fatal(err)
		}
	}
	lifetimePlan := mustSavePlan(t, vip, vipdomain.PlanMutation{AppID: appID, Name: ptrTo("永久基础版"),
		Kind: ptrTo(vipdomain.KindPaid), Lifetime: ptrTo(true), Features: &[]string{"export"},
		Price: ptrTo(decimal.NewFromInt(99))})
	trialPlan := mustSavePlan(t, vip, vipdomain.PlanMutation{AppID: appID, Name: ptrTo("试用"),
		Kind: ptrTo(vipdomain.KindTrial), DurationDays: ptrTo(3), Price: ptrTo(decimal.Zero)})

	alice := insertTestUser(t, ctx, pool, appID, "store-alice", "0")
	bob := insertTestUser(t, ctx, pool, appID, "store-bob", "0")

	redeem := func(userID, itemID int64, requestID string) (*pointstore.RedeemResult, error) {
		return store.Redeem(ctx, pointstore.RedeemInput{AppID: appID, UserID: userID, ItemID: itemID, RequestID: requestID})
	}
	mustRedeem := func(userID, itemID int64, requestID string) *pointstore.RedeemResult {
		t.Helper()
		result, err := redeem(userID, itemID, requestID)
		if err != nil {
			t.Fatalf("兑换商品 %d 失败：%v", itemID, err)
		}
		return result
	}
	mustCreate := func(input pointstore.ItemInput) *pointstore.Item {
		t.Helper()
		input.AppID = appID
		item, err := store.CreateItem(ctx, input)
		if err != nil {
			t.Fatalf("新建商品失败：%v", err)
		}
		return item
	}
	setIntegral := func(userID, value int64) {
		t.Helper()
		if _, err := pool.Exec(ctx, `UPDATE users SET integral = $2 WHERE id = $1`, userID, value); err != nil {
			t.Fatal(err)
		}
	}

	// ── 引用在保存时就核对 ──
	if _, err := store.CreateItem(ctx, pointstore.ItemInput{AppID: appID, Title: "坏商品", Cost: 10,
		Rewards: []pointstore.Reward{{Type: pointstore.RewardFeature, Tag: "missing", Amount: 7}}}); errCode(err) != 40000 {
		t.Fatalf("未登记的功能应被拒，得到 %v", err)
	}
	if _, err := store.CreateItem(ctx, pointstore.ItemInput{AppID: appID, Title: "坏商品", Cost: 10,
		Rewards: []pointstore.Reward{{Type: pointstore.RewardVipPlan, RefID: trialPlan.ID}}}); errCode(err) != 40000 {
		t.Fatalf("试用套餐应被拒，得到 %v", err)
	}

	music7 := mustCreate(pointstore.ItemInput{Title: "在线音乐 7 天", Category: "功能", Cost: 100,
		LimitPeriod: pointstore.PeriodWeek, LimitCount: 2,
		Rewards: []pointstore.Reward{{Type: pointstore.RewardFeature, Tag: "online_music", Amount: 7}}})
	vip3 := mustCreate(pointstore.ItemInput{Title: "会员 3 天", Category: "会员", Cost: 200, Stock: ptrTo(int64(1)),
		Rewards: []pointstore.Reward{{Type: pointstore.RewardVipDays, Amount: 3}}})
	merch := mustCreate(pointstore.ItemInput{Title: "周边", Category: "福利", Cost: 50,
		Rewards: []pointstore.Reward{{Type: pointstore.RewardManual, Text: "定制贴纸一套"}}})
	musicForever := mustCreate(pointstore.ItemInput{Title: "在线音乐永久", Category: "功能", Cost: 1000,
		Rewards: []pointstore.Reward{{Type: pointstore.RewardFeature, Tag: "online_music", Amount: 0}}})
	lifetime := mustCreate(pointstore.ItemInput{Title: "永久会员", Category: "会员", Cost: 300,
		Rewards: []pointstore.Reward{{Type: pointstore.RewardVipPlan, RefID: lifetimePlan.ID}}})
	bundle := mustCreate(pointstore.ItemInput{Title: "大礼包", Category: "福利", Cost: 10, Audience: pointstore.AudienceVip,
		Rewards: []pointstore.Reward{
			{Type: pointstore.RewardExperience, Amount: 50},
			{Type: pointstore.RewardBalance, Money: decimal.RequireFromString("1.50")},
			{Type: pointstore.RewardLotteryDraws, Amount: 2},
		}})

	// ── 开关 ──
	page, err := store.Store(ctx, appID, alice)
	if err != nil {
		t.Fatal(err)
	}
	if page.Enabled || len(page.Items) != 0 {
		t.Fatalf("没开启时应 enabled=false 且无商品，得到 %+v", page)
	}
	if _, err := redeem(alice, music7.ID, ""); errCode(err) != errCodePointsStoreDisabled {
		t.Fatalf("没开启时兑换应报 40303，得到 %v", err)
	}
	if _, err := store.SaveSettings(ctx, appID, true, "admin"); err != nil {
		t.Fatal(err)
	}

	// ── 积分不足：列表与兑换同一个判据 ──
	page, err = store.Store(ctx, appID, alice)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 6 || len(page.Categories) != 3 || page.Items[0].Availability.Reason != pointstore.ReasonInsufficientPoints {
		t.Fatalf("商城首页不对：%+v", page)
	}
	if _, err := redeem(alice, music7.ID, ""); errCode(err) != errCodePointsInsufficient {
		t.Fatalf("积分不足应报 40307，得到 %v", err)
	}

	// ── 功能解锁：不成为会员、顺延、幂等、限兑 ──
	setIntegral(alice, 5000)
	first := mustRedeem(alice, music7.ID, "req-1")
	if first.Balance != 4900 || first.Redemption.Status != pointstore.RedemptionCompleted || len(first.Redemption.Results) != 1 ||
		first.Redemption.Results[0].ExpireAt == nil {
		t.Fatalf("第一次兑换结果不对：%+v", first)
	}
	ent := mustEntitlement(t, vip, appID, alice)
	if ent.IsVIP || !ent.HasFeature("online_music") || len(ent.Unlocks) != 1 {
		t.Fatalf("功能解锁应只加功能、不成为会员：%+v", ent)
	}
	firstEnd := *ent.Unlocks[0].ExpireAt
	replay := mustRedeem(alice, music7.ID, "req-1")
	if !replay.Replayed || replay.Balance != 4900 || replay.Redemption.RedemptionNo != first.Redemption.RedemptionNo {
		t.Fatalf("同一 requestId 应原样返回第一次的结果：%+v", replay)
	}
	mustRedeem(alice, music7.ID, "req-2")
	ent = mustEntitlement(t, vip, appID, alice)
	if got := ent.Unlocks[0].ExpireAt.Sub(firstEnd); got < 7*24*time.Hour-time.Minute || got > 7*24*time.Hour+time.Minute {
		t.Fatalf("第二次兑换应顺延 7 天，实际顺延 %v", got)
	}
	if _, err := redeem(alice, music7.ID, "req-3"); errCode(err) != errCodePointsStoreLimitReached {
		t.Fatalf("本周第三次应报 40306，得到 %v", err)
	}

	// ── 库存与兑换对象 ──
	if _, err := redeem(alice, bundle.ID, ""); errCode(err) != errCodePointsStoreRequirement {
		t.Fatalf("非会员兑换会员专属应报 40308，得到 %v", err)
	}
	mustRedeem(alice, vip3.ID, "")
	ent = mustEntitlement(t, vip, appID, alice)
	if !ent.IsVIP || ent.Source != vipdomain.SourcePointsStore || !ent.HasFeature("online_music") {
		t.Fatalf("兑换会员天数后应是会员、来源为积分兑换，且解锁的功能仍在：%+v", ent)
	}
	setIntegral(bob, 5000)
	if _, err := redeem(bob, vip3.ID, ""); errCode(err) != errCodePointsStoreSoldOut {
		t.Fatalf("库存为 0 应报 40305，得到 %v", err)
	}
	gift := mustRedeem(alice, bundle.ID, "")
	if len(gift.Redemption.Results) != 3 {
		t.Fatalf("礼包应发出三项：%+v", gift.Redemption.Results)
	}
	var wallet string
	if err := pool.QueryRow(ctx, `SELECT balance::text FROM user_wallets WHERE user_id = $1`, alice).Scan(&wallet); err != nil {
		t.Fatal(err)
	}
	if !decimal.RequireFromString(wallet).Equal(decimal.RequireFromString("1.50")) {
		t.Fatalf("余额应到账 1.50，实际 %s", wallet)
	}

	// ── 人工发放：取消退积分、库存加回；已处理的不能再处理 ──
	before := integralOf(t, ctx, pool, alice)
	pending := mustRedeem(alice, merch.ID, "")
	if pending.Redemption.Status != pointstore.RedemptionPending || !pending.Redemption.Results[0].Pending {
		t.Fatalf("人工发放应为待发放：%+v", pending.Redemption)
	}
	cancelled, err := store.Cancel(ctx, pointstore.HandleInput{AppID: appID, RedemptionID: pending.Redemption.ID, Note: "库存不足", Operator: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if cancelled.Status != pointstore.RedemptionCancelled || cancelled.RefundTransactionNo == "" || integralOf(t, ctx, pool, alice) != before {
		t.Fatalf("取消应退回积分：%+v", cancelled)
	}
	if _, err := store.Cancel(ctx, pointstore.HandleInput{AppID: appID, RedemptionID: pending.Redemption.ID}); errCode(err) != errCodePointsStoreHandled {
		t.Fatalf("重复处理应报 40912，得到 %v", err)
	}
	again := mustRedeem(alice, merch.ID, "")
	done, err := store.Fulfill(ctx, pointstore.HandleInput{AppID: appID, RedemptionID: again.Redemption.ID, Note: "快递单号 SF123", Operator: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != pointstore.RedemptionCompleted || done.Note != "快递单号 SF123" || done.Results[0].Pending {
		t.Fatalf("发放结果不对：%+v", done)
	}
	merchNow, err := pg.GetPointsStoreItem(ctx, appID, merch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if merchNow.SoldCount != 1 {
		t.Fatalf("取消的那笔不该计入已兑数，实际 %d", merchNow.SoldCount)
	}

	// ── 永久解锁之后，同一功能的限时商品判为已拥有 ──
	mustRedeem(alice, musicForever.ID, "")
	page, err = store.Store(ctx, appID, alice)
	if err != nil {
		t.Fatal(err)
	}
	if page.Items[0].Availability.Reason != pointstore.ReasonLimitReached {
		t.Fatalf("限兑优先于已拥有：%+v", page.Items[0].Availability)
	}
	if _, err := redeem(alice, musicForever.ID, ""); errCode(err) != errCodePointsStoreOwned {
		t.Fatalf("已永久解锁再兑应报 40309，得到 %v", err)
	}

	// ── 管理员授予与收回 ──
	granted, err := store.GrantUnlock(ctx, pointstore.UnlockGrantInput{AppID: appID, UserID: bob, Tag: "doubao_analyze", Days: 0, Operator: "admin"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.GrantUnlock(ctx, pointstore.UnlockGrantInput{AppID: appID, UserID: bob, Tag: "doubao_analyze", Days: 7}); errCode(err) != errCodeFeatureAlreadyUnlocked {
		t.Fatalf("已永久解锁再授予应报 40913，得到 %v", err)
	}
	if !mustEntitlement(t, vip, appID, bob).HasFeature("doubao_analyze") {
		t.Fatal("授予后应拥有该功能")
	}
	if _, err := store.RevokeUnlock(ctx, appID, bob, granted.ID, "admin", "测试"); err != nil {
		t.Fatal(err)
	}
	if mustEntitlement(t, vip, appID, bob).HasFeature("doubao_analyze") {
		t.Fatal("收回后不应再拥有该功能")
	}
	if _, err := store.RevokeUnlock(ctx, appID, bob, granted.ID, "admin", ""); errCode(err) != errCodeFeatureUnlockNotFound {
		t.Fatalf("重复收回应报 40445，得到 %v", err)
	}

	// ── 永久套餐：兑换后成为永久会员，再兑判为已拥有 ──
	mustRedeem(bob, lifetime.ID, "")
	bobEnt := mustEntitlement(t, vip, appID, bob)
	if !bobEnt.IsLifetime || !bobEnt.HasFeature("export") {
		t.Fatalf("兑换永久套餐后应是永久会员：%+v", bobEnt)
	}
	if _, err := redeem(bob, lifetime.ID, ""); errCode(err) != errCodePointsStoreOwned {
		t.Fatalf("永久会员再兑同一永久套餐应报 40309，得到 %v", err)
	}

	// ── 并发：库存 3 件、8 个人同时抢，恰好成交 3 笔 ──
	hot := mustCreate(pointstore.ItemInput{Title: "限量", Cost: 10, Stock: ptrTo(int64(3)),
		Rewards: []pointstore.Reward{{Type: pointstore.RewardLotteryDraws, Amount: 1}}})
	buyers := make([]int64, 8)
	for i := range buyers {
		buyers[i] = insertTestUser(t, ctx, pool, appID, fmt.Sprintf("store-buyer-%d", i), "0")
		setIntegral(buyers[i], 100)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	succeeded, soldOut := 0, 0
	for _, buyer := range buyers {
		wg.Add(1)
		go func(userID int64) {
			defer wg.Done()
			_, err := redeem(userID, hot.ID, "")
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				succeeded++
			case errCode(err) == errCodePointsStoreSoldOut:
				soldOut++
			default:
				t.Errorf("并发兑换出现意外错误：%v", err)
			}
		}(buyer)
	}
	wg.Wait()
	if succeeded != 3 || soldOut != 5 {
		t.Fatalf("库存 3 件应恰好成交 3 笔，实际成交 %d、售罄 %d", succeeded, soldOut)
	}

	// ── 并发：同一个 requestId 连点五次，只扣一次 ──
	repeat := mustCreate(pointstore.ItemInput{Title: "经验包", Cost: 10,
		Rewards: []pointstore.Reward{{Type: pointstore.RewardExperience, Amount: 5}}})
	tapper := buyers[0]
	start := integralOf(t, ctx, pool, tapper)
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := redeem(tapper, repeat.ID, "same-request"); err != nil && errCode(err) != errCodePointsStoreRedeeming {
				t.Errorf("同一 requestId 的并发提交出现意外错误：%v", err)
			}
		}()
	}
	wg.Wait()
	if got := start - integralOf(t, ctx, pool, tapper); got != 10 {
		t.Fatalf("同一 requestId 只该扣一次 10 积分，实际扣了 %d", got)
	}

	// ── 统计与管理端列表 ──
	stats, err := store.Stats(ctx, appID, 7)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Summary.Pending != 0 || stats.Summary.Redemptions == 0 || len(stats.Trend) != 7 || len(stats.TopItems) == 0 {
		t.Fatalf("统计不对：%+v", stats.Summary)
	}
	items, err := store.ListItems(ctx, appID)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.ID == music7.ID && (item.Redemptions != 2 || item.Redeemers != 1 || item.PointsSpent != 200) {
			t.Fatalf("商品汇总不对：%+v", item)
		}
	}
	mine, err := store.MyRedemptions(ctx, appID, alice, 1, 50)
	if err != nil {
		t.Fatal(err)
	}
	if mine.Total == 0 || mine.Items[0].ClientIP != "" || mine.Items[0].Rewards != nil {
		t.Fatalf("我的兑换记录不对：%+v", mine.Items[0])
	}

	// ── 删除商品：记录保留快照 ──
	if err := store.DeleteItem(ctx, appID, music7.ID); err != nil {
		t.Fatal(err)
	}
	history, err := store.ListRedemptions(ctx, pointstore.RedemptionQuery{AppID: appID, UserID: alice, Keyword: "在线音乐 7"})
	if err != nil {
		t.Fatal(err)
	}
	if history.Total != 2 || history.Items[0].ItemID != nil || history.Items[0].ItemTitle != "在线音乐 7 天" {
		t.Fatalf("删除商品后记录应保留快照：%+v", history.Items)
	}
}

func integralOf(t *testing.T, ctx context.Context, pool *pgxpool.Pool, userID int64) int64 {
	t.Helper()
	var value int64
	if err := pool.QueryRow(ctx, `SELECT integral FROM users WHERE id = $1`, userID).Scan(&value); err != nil {
		t.Fatal(err)
	}
	return value
}
