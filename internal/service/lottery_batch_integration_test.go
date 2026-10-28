package service

import (
	"testing"

	"go.uber.org/zap"

	lotterydomain "aegis/internal/domain/lottery"
	pgrepo "aegis/internal/repository/postgres"
)

// 十连抽与今日运气的端到端集成测试（真实 Postgres）：
// 连抽逐次到账、次数用完时停下并写明原因 → 同一个 requestId 重试原样返回 → 第一次就抽不了时报错 →
// 次数上限 → 活动页的今日运气、公示原概率与身份。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestLotteryBatchIntegration
func TestLotteryBatchIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	lottery := NewLotteryService(zap.NewNop(), pg, nil)
	const appID = int64(40200)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'lottery-batch') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	erin := insertTestUser(t, ctx, pool, appID, "batch-erin", "0")

	// 第 0 格兜底经验，第 1 格积分（常中），其余谢谢参与；每天 3 次免费，开每日运气
	prizes := make([]lotterydomain.PrizeInput, 0, 8)
	for position := 0; position < 8; position++ {
		prizes = append(prizes, lotterydomain.PrizeInput{Position: position, Reward: lotterydomain.Reward{Type: lotterydomain.KindNone},
			Quantity: -1})
	}
	prizes[0] = lotterydomain.PrizeInput{Position: 0, Reward: lotterydomain.Reward{Type: lotterydomain.KindExperience, Amount: 5},
		Weight: 50, Quantity: -1, Fallback: true}
	prizes[1] = lotterydomain.PrizeInput{Position: 1, Reward: lotterydomain.Reward{Type: lotterydomain.KindPoints, Amount: 3},
		Weight: 50, Quantity: -1}
	activity, err := lottery.CreateActivity(ctx, lotterydomain.ActivityInput{
		AppID: appID, Name: "十连抽", UIMode: lotterydomain.UIModeGrid, Status: lotterydomain.StatusActive,
		FreeDaily: 3, PityPosition: -1, Prizes: prizes, Operator: "admin",
		Strategy: lotterydomain.Strategy{Fortune: &lotterydomain.Fortune{Spread: 0.3}},
	})
	if err != nil {
		t.Fatalf("新建活动失败：%v", err)
	}
	batch := func(count int, requestID string) (*lotterydomain.BatchResult, error) {
		return lottery.DrawBatch(ctx, LotteryBatchRequest{
			LotteryDrawRequest: LotteryDrawRequest{AppID: appID, UserID: erin, ActivityID: activity.ID, RequestID: requestID},
			Count:              count,
		})
	}

	// ── 活动页：今日运气、公示原概率、身份 ──
	state, err := lottery.State(ctx, appID, erin, activity.ID)
	if err != nil {
		t.Fatal(err)
	}
	fortune := state.Fortune
	if fortune == nil {
		t.Fatal("开了每日运气，活动页应有今日运气")
	}
	day := lotteryDayStart(state.ServerTime)
	if want := lotterydomain.FortuneOf(activity.Strategy.Fortune, activity.ID, erin, day); fortune.Factor != want {
		t.Fatalf("今日运气的系数应与出奖用的相同 %v，得到 %v", want, fortune.Factor)
	}
	if fortune.Percentile < 1 || fortune.Percentile > 99 || fortune.Title == "" || fortune.Low >= fortune.High {
		t.Fatalf("今日运气不完整：%+v", fortune)
	}
	if fortune.BaseWinRate != 50 || (fortune.Factor > 1) != (fortune.WinRate > fortune.BaseWinRate) {
		t.Fatalf("中奖率应随运气变化（原 50%%），得到 %+v", fortune)
	}
	if prize := state.Activity.Prizes[1]; prize.BaseProbability != 50 {
		t.Fatalf("公示原概率应为 50%%，得到 %+v", prize)
	}
	if state.Viewer.Lifetime {
		t.Fatal("普通用户不应标成永久会员")
	}

	// ── 上限 ──
	if _, err := batch(lotterydomain.MaxBatchDraws+1, "too-many"); errCode(err) != 40000 {
		t.Fatalf("超过 %d 次应被拒，得到 %v", lotterydomain.MaxBatchDraws, err)
	}

	// ── 十连抽：只有 3 次免费，抽 3 次后停下 ──
	first, err := batch(10, "batch-1")
	if err != nil {
		t.Fatalf("十连抽失败：%v", err)
	}
	if len(first.Draws) != 3 || first.Requested != 10 || first.Replayed {
		t.Fatalf("应抽到 3 次（共要求 10 次），得到 %d / %d replayed=%v", len(first.Draws), first.Requested, first.Replayed)
	}
	if first.Stopped == nil || first.Stopped.Code != errCodeLotteryNoChance || first.Stopped.Message == "" {
		t.Fatalf("次数用完应写明停下的原因 42915，得到 %+v", first.Stopped)
	}
	if first.Chances.Free.Remaining != 0 || first.Chances.Next != "" {
		t.Fatalf("抽完后次数应为 0，得到 %+v", first.Chances)
	}
	seen := map[string]bool{}
	for _, draw := range first.Draws {
		if draw.Source != lotterydomain.SourceFree || seen[draw.DrawNo] || draw.RequestID != "" || draw.Trace != nil {
			t.Fatalf("每一抽都应是独立的免费抽奖记录且去掉内部字段，得到 %+v", draw)
		}
		seen[draw.DrawNo] = true
	}

	// ── 同一个 requestId 重试：原样返回，不再扣次数 ──
	again, err := batch(10, "batch-1")
	if err != nil {
		t.Fatalf("重试失败：%v", err)
	}
	if !again.Replayed || len(again.Draws) != 3 || again.Stopped == nil {
		t.Fatalf("重试应原样返回 3 次并停下，得到 %+v", again)
	}
	for index := range again.Draws {
		if again.Draws[index].DrawNo != first.Draws[index].DrawNo || again.Draws[index].PrizeName != first.Draws[index].PrizeName {
			t.Fatalf("第 %d 次重试应返回原结果，得到 %+v", index, again.Draws[index])
		}
	}
	var total int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM lottery_draws WHERE user_id = $1`, erin).Scan(&total); err != nil || total != 3 {
		t.Fatalf("库里应只有 3 条记录，得到 %d %v", total, err)
	}

	// ── 一次都抽不了：报错，与单抽同一个码 ──
	if _, err := batch(10, "batch-2"); errCode(err) != errCodeLotteryNoChance {
		t.Fatalf("没有次数时十连抽应报 42915，得到 %v", err)
	}
}
