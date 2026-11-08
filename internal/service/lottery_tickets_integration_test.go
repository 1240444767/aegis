package service

import (
	"testing"

	"go.uber.org/zap"

	lotterydomain "aegis/internal/domain/lottery"
	pgrepo "aegis/internal/repository/postgres"
)

// 抽奖次数分批记账与扣除顺序的端到端集成测试（真实 Postgres）：
// 早期余额对齐成一批 → 管理员发放赠送 / 兑换两档 → 活动页拆分 → 指定 points 也先扣免费 →
// 次数先用赠送的、再用兑换的、最后才花积分（单抽与十连抽同一顺序）→ 流水与累计 → 管理员扣减 → 总览。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestLotteryTicketsIntegration
func TestLotteryTicketsIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	lottery := NewLotteryService(zap.NewNop(), pg, nil)
	const appID = int64(40300)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'lottery-tickets') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	fay := insertTestUser(t, ctx, pool, appID, "tickets-fay", "0")

	prizes := make([]lotterydomain.PrizeInput, 0, 8)
	for position := 0; position < 8; position++ {
		prizes = append(prizes, lotterydomain.PrizeInput{Position: position, Reward: lotterydomain.Reward{Type: lotterydomain.KindNone},
			Weight: 1, Quantity: -1, Fallback: position == 0})
	}
	activity, err := lottery.CreateActivity(ctx, lotterydomain.ActivityInput{
		AppID: appID, Name: "次数", UIMode: lotterydomain.UIModeGrid, Status: lotterydomain.StatusActive,
		FreeDaily: 1, PointsCost: 10, PityPosition: -1, Prizes: prizes, Operator: "admin",
	})
	if err != nil {
		t.Fatalf("新建活动失败：%v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE users SET integral = 30 WHERE id = $1`, fay); err != nil {
		t.Fatal(err)
	}

	// ── 分批记账之前的余额：汇总里有 2 次、没有批次，发放时对齐成一批 legacy ──
	if _, err := pool.Exec(ctx, `INSERT INTO lottery_draw_grants (appid, user_id, balance, total_granted) VALUES ($1, $2, 2, 2)`,
		appID, fay); err != nil {
		t.Fatal(err)
	}
	adjust := func(amount int64, paid bool) *lotterydomain.TicketAccount {
		t.Helper()
		account, err := lottery.AdjustTickets(ctx, lotterydomain.TicketAdjust{
			AppID: appID, UserID: fay, Amount: amount, Paid: paid, Reason: "测试", Operator: "admin",
		})
		if err != nil {
			t.Fatalf("增减 %d 次失败：%v", amount, err)
		}
		return account
	}
	adjust(3, true)
	account := adjust(1, false)
	if account.Balance != 6 || account.Tickets != (lotterydomain.TicketBalance{Gift: 3, Purchased: 3}) {
		t.Fatalf("早期 2 + 兑换 3 + 赠送 1，得到 %+v", account)
	}
	if len(account.Lots) != 3 || account.Lots[0].Origin != lotterydomain.TicketOriginLegacy ||
		account.Lots[1].Origin != lotterydomain.TicketOriginAdmin || account.Lots[1].Paid || !account.Lots[2].Paid {
		t.Fatalf("批次按使用顺序：早期、赠送、兑换，得到 %+v", account.Lots)
	}
	if _, err := lottery.AdjustTickets(ctx, lotterydomain.TicketAdjust{AppID: appID, UserID: fay, Amount: 1, Operator: "admin"}); errCode(err) != 40000 {
		t.Fatalf("没写原因应被拒，得到 %v", err)
	}

	state, err := lottery.State(ctx, appID, fay, activity.ID)
	if err != nil {
		t.Fatal(err)
	}
	if state.Chances.Next != lotterydomain.SourceFree || state.Chances.Tickets != (lotterydomain.TicketBalance{Gift: 3, Purchased: 3}) {
		t.Fatalf("活动页先用免费、次数按档拆开，得到 %+v", state.Chances)
	}

	// ── 客户端按过时的页面传 points：免费还在，先扣免费 ──
	result, err := lottery.Draw(ctx, LotteryDrawRequest{AppID: appID, UserID: fay, ActivityID: activity.ID,
		Source: lotterydomain.SourcePoints, RequestID: "tk-1"})
	if err != nil || result.Draw.Source != lotterydomain.SourceFree || result.Draw.Ticket != nil {
		t.Fatalf("指定积分时仍先扣免费，得到 %+v %v", result, err)
	}
	// 指定免费、免费已用完：不越级去用次数
	if _, err := lottery.Draw(ctx, LotteryDrawRequest{AppID: appID, UserID: fay, ActivityID: activity.ID,
		Source: lotterydomain.SourceFree, RequestID: "tk-2"}); errCode(err) != errCodeLotteryFreeExhausted {
		t.Fatalf("指定免费且已用完时报免费用完，得到 %v", err)
	}
	result, err = lottery.Draw(ctx, LotteryDrawRequest{AppID: appID, UserID: fay, ActivityID: activity.ID,
		Source: lotterydomain.SourcePoints, RequestID: "tk-3"})
	if err != nil || result.Draw.Source != lotterydomain.SourceBonus || result.Draw.Ticket == nil ||
		result.Draw.Ticket.Origin != lotterydomain.TicketOriginLegacy || result.Draw.Ticket.Paid {
		t.Fatalf("免费用完后指定积分也先用最早的赠送次数，得到 %+v %v", result.Draw.Ticket, err)
	}
	if result.Chances.Tickets != (lotterydomain.TicketBalance{Gift: 2, Purchased: 3}) {
		t.Fatalf("用掉一次赠送的，得到 %+v", result.Chances.Tickets)
	}

	// ── 十连抽：赠送 2 → 兑换 3 → 积分 3 次（30 积分），第 9 次积分不够停下 ──
	batch, err := lottery.DrawBatch(ctx, LotteryBatchRequest{
		LotteryDrawRequest: LotteryDrawRequest{AppID: appID, UserID: fay, ActivityID: activity.ID, RequestID: "tk-batch"},
		Count:              10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(batch.Draws) != 8 || batch.Stopped == nil || batch.Stopped.Code != errCodeLotteryNoChance {
		t.Fatalf("赠送 2 + 兑换 3 + 积分 3 后停下，得到 %d 次 %+v", len(batch.Draws), batch.Stopped)
	}
	for index, draw := range batch.Draws {
		switch {
		case index < 2:
			if draw.Source != lotterydomain.SourceBonus || draw.Ticket == nil || draw.Ticket.Paid {
				t.Fatalf("第 %d 次应用赠送的次数，得到 %s %+v", index+1, draw.Source, draw.Ticket)
			}
		case index < 5:
			if draw.Source != lotterydomain.SourceBonus || draw.Ticket == nil || !draw.Ticket.Paid {
				t.Fatalf("第 %d 次应用兑换的次数，得到 %s %+v", index+1, draw.Source, draw.Ticket)
			}
		default:
			if draw.Source != lotterydomain.SourcePoints || draw.PointsCost != 10 {
				t.Fatalf("第 %d 次才花积分，得到 %s %d", index+1, draw.Source, draw.PointsCost)
			}
		}
	}

	// ── 流水与累计 ──
	account, err = lottery.TicketAccount(ctx, appID, fay)
	if err != nil {
		t.Fatal(err)
	}
	if account.Balance != 0 || len(account.Lots) != 0 || account.TotalConsumed != 6 || account.TotalGranted != 6 {
		t.Fatalf("次数用完，累计发放 6、用掉 6，得到 %+v", account)
	}
	mine, err := lottery.MyTickets(ctx, appID, fay, 1, 50)
	if err != nil {
		t.Fatal(err)
	}
	// 早期 1 + 管理员 2 条发放，6 条使用
	if mine.Ledger.Total != 9 || mine.Ledger.Items[0].Kind != lotterydomain.LedgerConsume || mine.Ledger.Items[0].BalanceAfter != 0 ||
		mine.Ledger.Items[0].Operator != "" {
		t.Fatalf("流水 9 条、最新一条是使用后余额 0、不给用户端回显操作人，得到 %+v", mine.Ledger)
	}
	if last := mine.Ledger.Items[len(mine.Ledger.Items)-1]; last.Origin != lotterydomain.TicketOriginLegacy || last.Delta != 2 {
		t.Fatalf("最早一条是早期余额对齐，得到 %+v", last)
	}

	// ── 管理员扣减：不够扣时拒绝；够时先扣赠送的 ──
	if _, err := lottery.AdjustTickets(ctx, lotterydomain.TicketAdjust{AppID: appID, UserID: fay, Amount: -1, Reason: "多发", Operator: "admin"}); errCode(err) != 40000 {
		t.Fatalf("余额 0 时扣减应被拒，得到 %v", err)
	}
	adjust(2, true)
	adjust(1, false)
	account = adjust(-2, false)
	if account.Tickets != (lotterydomain.TicketBalance{Gift: 0, Purchased: 1}) {
		t.Fatalf("扣 2 次：先扣赠送的 1、再扣兑换的 1，得到 %+v", account.Tickets)
	}
	ledger, err := lottery.TicketLedger(ctx, lotterydomain.TicketQuery{AppID: appID, Kind: lotterydomain.LedgerDeduct})
	if err != nil || ledger.Total != 2 || ledger.Items[0].Operator != "admin" {
		t.Fatalf("扣减跨两批记两条流水，得到 %+v %v", ledger, err)
	}

	overview, err := lottery.TicketOverview(ctx, appID)
	if err != nil || overview.Holders != 1 || overview.Outstanding != 1 || overview.Tickets.Purchased != 1 || overview.UsedToday != 6 {
		t.Fatalf("总览：1 人持有 1 次兑换的、今天用掉 6 次，得到 %+v %v", overview, err)
	}
	accounts, err := lottery.TicketAccounts(ctx, lotterydomain.TicketQuery{AppID: appID, Keyword: "fay"})
	if err != nil || accounts.Total != 1 || accounts.Items[0].Tickets.Purchased != 1 {
		t.Fatalf("按账号搜到这个人，得到 %+v %v", accounts, err)
	}
}
