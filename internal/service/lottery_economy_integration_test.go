package service

import (
	"testing"

	"github.com/shopspring/decimal"
	"go.uber.org/zap"

	"aegis/internal/config"
	lotterydomain "aegis/internal/domain/lottery"
	vipdomain "aegis/internal/domain/vip"
	pgrepo "aegis/internal/repository/postgres"
)

// 无套利的端到端集成测试：真实 Postgres，跑全部迁移。
//
// 复现线上的环：积分商城 10 积分卖一次抽奖，奖盘里有月度会员；永久会员抽中月度会员按会员单价折成 300 积分，
// 买一次赚一次。修好之后折算积分封在「买一次、抽一次」回本的上限之内，活动页的提示与实际发放一致，
// 控制台的积分账能看出奖盘本身的套利。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55441/postgres go test ./internal/service -run TestLotteryEconomyIntegration
func TestLotteryEconomyIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	vip := NewVipService(zap.NewNop(), pg)
	rewarded := NewRewardedAdService(zap.NewNop(), pg, config.Config{})
	lottery := NewLotteryService(zap.NewNop(), pg, rewarded)
	const appID = int64(40133)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'lottery-economy-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	if _, err := vip.AdminSaveFeature(ctx, vipdomain.FeatureMutation{AppID: appID, Tag: "ai", Name: ptrTo("AI")}); err != nil {
		t.Fatal(err)
	}
	monthly := mustSavePlan(t, vip, vipdomain.PlanMutation{AppID: appID, Name: ptrTo("月度会员"), DurationDays: ptrTo(30),
		Price: ptrTo(decimal.NewFromInt(5)), Features: &[]string{"ai"}})
	lifetimePlan := mustSavePlan(t, vip, vipdomain.PlanMutation{AppID: appID, Name: ptrTo("永久会员"), Lifetime: ptrTo(true),
		Price: ptrTo(decimal.NewFromInt(99)), Features: &[]string{"ai"}})

	frank := insertTestUser(t, ctx, pool, appID, "economy-frank", "0")
	if _, err := vip.AdminGrantVip(ctx, AdminVipGrantInput{UserID: frank, AppID: appID, PlanID: lifetimePlan.ID,
		Quantity: 1, Lifetime: true, Reason: "官方赠送", Operator: "admin"}); err != nil {
		t.Fatal(err)
	}

	// 线上的奖盘：月度会员 7、10 经验 30、10 积分 15、20 经验 10、50 积分 10、抽奖 1 次 7、20 积分 5、月度会员 7
	weights := []int{7, 30, 15, 10, 10, 7, 5, 7}
	rewards := []lotterydomain.Reward{
		{Type: lotterydomain.KindVipPlan, RefID: monthly.ID},
		{Type: lotterydomain.KindExperience, Amount: 10},
		{Type: lotterydomain.KindPoints, Amount: 10},
		{Type: lotterydomain.KindExperience, Amount: 20},
		{Type: lotterydomain.KindPoints, Amount: 50},
		{Type: lotterydomain.KindLotteryDraws, Amount: 1},
		{Type: lotterydomain.KindPoints, Amount: 20},
		{Type: lotterydomain.KindVipPlan, RefID: monthly.ID},
	}
	prizes := make([]lotterydomain.PrizeInput, 0, 8)
	for position := range weights {
		prizes = append(prizes, lotterydomain.PrizeInput{Position: position, Reward: rewards[position],
			Weight: weights[position], Quantity: -1, Fallback: position == 1})
	}
	activity, err := lottery.CreateActivity(ctx, lotterydomain.ActivityInput{AppID: appID, Name: "每日幸运抽奖",
		UIMode: lotterydomain.UIModeGrid, Status: lotterydomain.StatusActive, FreeDaily: 1, PityPosition: -1,
		LifetimePolicy: lotterydomain.LifetimePoints, Prizes: prizes, Operator: "admin"})
	if err != nil {
		t.Fatal(err)
	}

	// 积分商城还没卖次数：积分换不到次数，不封顶，月度会员按会员单价折 300 积分
	state, err := lottery.State(ctx, appID, frank, activity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if swap := state.Activity.Prizes[7].Swap; swap == nil || swap.Reward == nil || swap.Reward.Amount != 300 {
		t.Fatalf("不卖次数时月度会员折 300 积分，得到 %+v", swap)
	}
	if activity.Economy == nil || activity.Economy.Price != 0 || len(activity.Economy.Warnings) != 0 {
		t.Fatalf("积分换不到次数时没有套利，得到 %+v", activity.Economy)
	}

	// 上架：10 积分一次抽奖
	if _, err := pool.Exec(ctx, `INSERT INTO points_store_items (appid, title, cost, rewards, status)
VALUES ($1, '抽奖次数 1 次', 10, '[{"type": "lottery_draws", "amount": 1}]'::jsonb, 'active')`, appID); err != nil {
		t.Fatal(err)
	}
	state, err = lottery.State(ctx, appID, frank, activity.ID)
	if err != nil {
		t.Fatal(err)
	}
	swap := state.Activity.Prizes[7].Swap
	if swap == nil || swap.Reward == nil || swap.Reward.Type != lotterydomain.KindPoints || swap.Reward.Amount <= 0 || swap.Reward.Amount >= 10 {
		t.Fatalf("10 积分一次时月度会员的折算应封在 10 积分以内，得到 %+v", swap)
	}
	capped := swap.Reward.Amount

	admin, err := lottery.GetActivity(ctx, appID, activity.ID)
	if err != nil || admin.Economy == nil || admin.Economy.Price != 10 || admin.Economy.LifetimeCap != capped ||
		admin.Economy.LifetimeReturn > 10 || admin.Economy.MemberReturn >= 10 || len(admin.Economy.Warnings) != 0 {
		t.Fatalf("控制台积分账不对：%+v %v", admin.Economy, err)
	}

	// 只让月度会员那两格出：抽中就是已拥有，实际发放与提示一致
	for _, position := range []int{1, 2, 3, 4, 5, 6} {
		if _, err := pool.Exec(ctx, `UPDATE lottery_prizes SET weight = 0 WHERE activity_id = $1 AND position = $2`, activity.ID, position); err != nil {
			t.Fatal(err)
		}
	}
	var before int64
	if err := pool.QueryRow(ctx, `SELECT integral FROM users WHERE id = $1`, frank).Scan(&before); err != nil {
		t.Fatal(err)
	}
	result, err := lottery.Draw(ctx, LotteryDrawRequest{AppID: appID, UserID: frank, ActivityID: activity.ID, RequestID: "economy-1"})
	if err != nil {
		t.Fatal(err)
	}
	conversion := result.Draw.Conversion
	var after int64
	if err := pool.QueryRow(ctx, `SELECT integral FROM users WHERE id = $1`, frank).Scan(&after); err != nil {
		t.Fatal(err)
	}
	// 奖盘里只剩会员格子时积分奖品的期望为 0，上限变成 10 × (1 − 0) / 1 = 10
	if conversion == nil || conversion.Reward.Type != lotterydomain.KindPoints || conversion.Reward.Amount > 10 || after-before != conversion.Reward.Amount {
		t.Fatalf("永久会员抽中月度会员应折成不超过 10 积分，得到 %+v（+%d）", conversion, after-before)
	}

	// 积分奖品本身就让人稳赚：50 积分那格独大，控制台要给出警告
	if _, err := pool.Exec(ctx, `UPDATE lottery_prizes SET weight = 100 WHERE activity_id = $1 AND position = 4`, activity.ID); err != nil {
		t.Fatal(err)
	}
	admin, err = lottery.GetActivity(ctx, appID, activity.ID)
	if err != nil || admin.Economy == nil || len(admin.Economy.Warnings) != 1 || admin.Economy.MemberReturn <= 10 {
		t.Fatalf("积分奖品期望超过价格时应警告，得到 %+v %v", admin.Economy, err)
	}
	// 这时连永久会员的折算也是 0：已拥有的格子不再折积分，活动页写明
	state, err = lottery.State(ctx, appID, frank, activity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if swap := state.Activity.Prizes[7].Swap; swap == nil || swap.Label != "转为谢谢参与" || swap.Reward == nil || swap.Reward.Type != lotterydomain.KindNone {
		t.Fatalf("奖盘本身套利时已拥有的格子不再折积分，得到 %+v", swap)
	}
}
