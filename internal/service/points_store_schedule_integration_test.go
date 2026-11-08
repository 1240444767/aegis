package service

import (
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"

	"aegis/internal/domain/pointstore"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
	"aegis/pkg/timeutil"
)

// 积分商城的时间规则：补货周期、全站名额、每人累计上限、每日开放时段。
// 「过了一天」用把记录与周期起点往前挪一天来模拟，不改系统时钟。
func TestPointsStoreScheduleIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	store := NewPointsStoreService(zap.NewNop(), pg)
	const appID = int64(30100)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'points-store-schedule') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SaveSettings(ctx, appID, true, "admin"); err != nil {
		t.Fatal(err)
	}
	users := map[string]int64{}
	for _, name := range []string{"alice", "bob", "carol"} {
		users[name] = insertTestUser(t, ctx, pool, appID, "schedule-"+name, "0")
		if _, err := pool.Exec(ctx, `UPDATE users SET integral = 100000 WHERE id = $1`, users[name]); err != nil {
			t.Fatal(err)
		}
	}
	alice, bob, carol := users["alice"], users["bob"], users["carol"]

	vipDay := []pointstore.Reward{{Type: pointstore.RewardVipDays, Amount: 1}}
	mustCreate := func(input pointstore.ItemInput) *pointstore.Item {
		t.Helper()
		input.AppID = appID
		item, err := store.CreateItem(ctx, input)
		if err != nil {
			t.Fatalf("新建商品失败：%v", err)
		}
		return item
	}
	redeem := func(userID, itemID int64) (*pointstore.RedeemResult, error) {
		return store.Redeem(ctx, pointstore.RedeemInput{AppID: appID, UserID: userID, ItemID: itemID})
	}
	mustRedeem := func(userID, itemID int64) *pointstore.RedeemResult {
		t.Helper()
		result, err := redeem(userID, itemID)
		if err != nil {
			t.Fatalf("兑换商品 %d 失败：%v", itemID, err)
		}
		return result
	}
	expectRefused := func(userID, itemID int64, code int, message string) {
		t.Helper()
		_, err := redeem(userID, itemID)
		var appErr *apperrors.AppError
		if errCode(err) != code {
			t.Fatalf("期望错误码 %d，得到 %v", code, err)
		}
		if message != "" && (!asAppError(err, &appErr) || appErr.Message != message) {
			t.Fatalf("期望提示「%s」，得到 %v", message, err)
		}
	}
	clientItem := func(userID, itemID int64) pointstore.ClientItem {
		t.Helper()
		page, err := store.Store(ctx, appID, userID)
		if err != nil {
			t.Fatal(err)
		}
		for _, item := range page.Items {
			if item.ID == itemID {
				return item
			}
		}
		t.Fatalf("商城里找不到商品 %d", itemID)
		return pointstore.ClientItem{}
	}
	// 把一件商品的补货周期与它的兑换记录都往前挪一天：等价于「过了一天」
	passDay := func(itemID int64) {
		t.Helper()
		if _, err := pool.Exec(ctx, `UPDATE points_store_items SET stock_window_start = stock_window_start - INTERVAL '1 day'
WHERE id = $1`, itemID); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE points_store_redemptions SET created_at = created_at - INTERVAL '1 day'
WHERE item_id = $1`, itemID); err != nil {
			t.Fatal(err)
		}
	}
	dbStock := func(itemID int64) int64 {
		t.Helper()
		var stock int64
		if err := pool.QueryRow(ctx, `SELECT stock FROM points_store_items WHERE id = $1`, itemID).Scan(&stock); err != nil {
			t.Fatal(err)
		}
		return stock
	}

	// ── 保存时的校验 ──
	if _, err := store.CreateItem(ctx, pointstore.ItemInput{AppID: appID, Title: "坏商品", Cost: 10, Rewards: vipDay,
		RestockPeriod: pointstore.RestockDay}); errCode(err) != 40000 {
		t.Fatalf("开补货不填数量应被拒，得到 %v", err)
	}

	// ── 每日补货 ──
	daily := mustCreate(pointstore.ItemInput{Title: "每日限量", Cost: 10, Rewards: vipDay,
		RestockPeriod: pointstore.RestockDay, RestockQuantity: ptrTo(int64(2))})
	if daily.Stock == nil || *daily.Stock != 2 || daily.StockWindowStart == nil {
		t.Fatalf("开补货不填剩余应从满货开始并记下周期起点，得到 %+v", daily)
	}
	mustRedeem(alice, daily.ID)
	mustRedeem(alice, daily.ID)
	expectRefused(bob, daily.ID, errCodePointsStoreSoldOut, "已兑完，明日补货")
	view := clientItem(bob, daily.ID)
	if view.Stock == nil || *view.Stock != 0 || view.NextRestockAt == nil || view.RestockPeriod != pointstore.RestockDay {
		t.Fatalf("兑完后列表应显示 0 与下次补货时刻，得到 %+v", view)
	}
	passDay(daily.ID)
	if view := clientItem(bob, daily.ID); *view.Stock != 2 || !view.Availability.Redeemable {
		t.Fatalf("新的一天列表应补满到 2，得到 %+v", view)
	}
	if dbStock(daily.ID) != 0 {
		t.Fatal("补货应是惰性的：列表读取不应回写库存")
	}
	mustRedeem(bob, daily.ID)
	if got := dbStock(daily.ID); got != 1 {
		t.Fatalf("新周期第一笔兑换应补满再扣一，库存应为 1，得到 %d", got)
	}

	// ── 取消只退回本周期的那份 ──
	merch := mustCreate(pointstore.ItemInput{Title: "每日周边", Cost: 10,
		Rewards:       []pointstore.Reward{{Type: pointstore.RewardManual, Text: "贴纸"}},
		RestockPeriod: pointstore.RestockDay, RestockQuantity: ptrTo(int64(1))})
	old := mustRedeem(alice, merch.ID).Redemption
	passDay(merch.ID)
	fresh := mustRedeem(bob, merch.ID).Redemption
	if _, err := store.Cancel(ctx, pointstore.HandleInput{AppID: appID, RedemptionID: old.ID, Operator: "admin"}); err != nil {
		t.Fatal(err)
	}
	if got := dbStock(merch.ID); got != 0 {
		t.Fatalf("取消上个周期的兑换不应退回本周期库存，得到 %d", got)
	}
	if _, err := store.Cancel(ctx, pointstore.HandleInput{AppID: appID, RedemptionID: fresh.ID, Operator: "admin"}); err != nil {
		t.Fatal(err)
	}
	if got := dbStock(merch.ID); got != 1 {
		t.Fatalf("取消本周期的兑换应退回库存，得到 %d", got)
	}

	// ── 全站名额 ──
	quota := mustCreate(pointstore.ItemInput{Title: "每日名额", Cost: 10, Rewards: vipDay,
		GlobalLimitPeriod: pointstore.PeriodDay, GlobalLimitCount: 2})
	mustRedeem(alice, quota.ID)
	mustRedeem(bob, quota.ID)
	expectRefused(carol, quota.ID, errCodePointsStoreSoldOut, "今日名额已满")
	if view := clientItem(carol, quota.ID); view.GlobalUsed != 2 || view.GlobalLimitCount != 2 || view.GlobalResetsAt == nil ||
		view.Availability.Reason != pointstore.ReasonQuotaReached {
		t.Fatalf("名额满时列表应显示 2/2 与清零时刻，得到 %+v", view)
	}
	admin, err := store.ListItems(ctx, appID)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range admin {
		if item.ID == quota.ID && item.GlobalUsed != 2 {
			t.Fatalf("管理端应显示今日已用 2 份，得到 %d", item.GlobalUsed)
		}
		if item.ID == daily.ID && (item.CurrentStock == nil || *item.CurrentStock != 1 || item.NextRestockAt == nil) {
			t.Fatalf("管理端应显示当前库存 1 与下次补货时刻，得到 %+v", item)
		}
	}
	passDay(quota.ID)
	mustRedeem(carol, quota.ID)

	// ── 每人：每日 1 次、累计 2 次 ──
	capped := mustCreate(pointstore.ItemInput{Title: "累计两次", Cost: 10, Rewards: vipDay,
		LimitPeriod: pointstore.PeriodDay, LimitCount: 1, UserTotalLimit: 2})
	mustRedeem(alice, capped.ID)
	expectRefused(alice, capped.ID, errCodePointsStoreLimitReached, "今日兑换次数已用完")
	passDay(capped.ID)
	mustRedeem(alice, capped.ID)
	passDay(capped.ID)
	expectRefused(alice, capped.ID, errCodePointsStoreLimitReached, "已达到累计兑换次数上限")
	if view := clientItem(alice, capped.ID); view.UsedTotal != 2 || view.UserTotalLimit != 2 {
		t.Fatalf("列表应显示累计 2/2，得到 %+v", view)
	}

	// ── 每日开放时段 ──
	local := time.Now().In(timeutil.DefaultLocation())
	minute := local.Hour()*60 + local.Minute()
	closedInput := pointstore.ItemInput{Title: "限时开放", Cost: 10, Rewards: vipDay,
		DailyOpenMinute: ptrTo((minute + 120) % pointstore.MinutesPerDay), DailyCloseMinute: ptrTo((minute + 180) % pointstore.MinutesPerDay)}
	hours := mustCreate(closedInput)
	_, err = redeem(alice, hours.ID)
	if errCode(err) != errCodePointsStoreUnavailable || !strings.Contains(err.Error(), "开放兑换") {
		t.Fatalf("时段外兑换应被拒并说明开放时段，得到 %v", err)
	}
	if view := clientItem(alice, hours.ID); view.OpenNow || view.NextOpenAt == nil || view.OpenHours == "" {
		t.Fatalf("时段外列表应显示未开放与下次开放时刻，得到 %+v", view)
	}
	closedInput.AppID = appID
	closedInput.DailyOpenMinute = ptrTo((minute + pointstore.MinutesPerDay - 30) % pointstore.MinutesPerDay)
	closedInput.DailyCloseMinute = ptrTo((minute + 30) % pointstore.MinutesPerDay)
	if _, err := store.UpdateItem(ctx, hours.ID, closedInput); err != nil {
		t.Fatal(err)
	}
	mustRedeem(alice, hours.ID)
}

