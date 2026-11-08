package service

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"go.uber.org/zap"

	lotterydomain "aegis/internal/domain/lottery"
	notificationdomain "aegis/internal/domain/notification"
	pgrepo "aegis/internal/repository/postgres"
	apperrors "aegis/pkg/errors"
)

// 抽奖次数（bonus）：用户端的「次数明细」与管理端的账户、流水、增减。
// 记账规则与扣除顺序见 lotterydomain（tickets.go）与 docs/lottery.md「抽奖次数」。

// ClientTickets 用户端的次数明细：账户（余额、两档、还有剩余的批次）与流水一页。
type ClientTickets struct {
	Account    lotterydomain.TicketAccount                   `json:"account"`
	Ledger     lotterydomain.Page[lotterydomain.TicketEntry] `json:"ledger"`
	ServerTime time.Time                                     `json:"serverTime"`
}

// MyTickets 我的抽奖次数与流水。
func (s *LotteryService) MyTickets(ctx context.Context, appID, userID int64, page, limit int) (*ClientTickets, error) {
	account, err := s.pg.LotteryTicketAccount(ctx, appID, userID)
	if err != nil {
		return nil, s.translate(err)
	}
	ledger, err := s.pg.ListLotteryTicketLedger(ctx, lotterydomain.TicketQuery{AppID: appID, UserID: userID, Page: page, Limit: limit})
	if err != nil {
		return nil, err
	}
	account.UserID, account.Account, account.Nickname = 0, "", ""
	for index := range ledger.Items {
		// 用户端只看自己的：不回显账号与操作人
		ledger.Items[index].UserID, ledger.Items[index].Account, ledger.Items[index].Nickname = 0, "", ""
		ledger.Items[index].Operator = ""
	}
	return &ClientTickets{Account: *account, Ledger: *ledger, ServerTime: time.Now().UTC()}, nil
}

// TicketOverview 管理端：次数总览。
func (s *LotteryService) TicketOverview(ctx context.Context, appID int64) (*lotterydomain.TicketOverview, error) {
	return s.pg.LotteryTicketOverview(ctx, appID, lotteryDayStart(time.Now()))
}

// TicketAccounts 管理端：有次数账户的人。
func (s *LotteryService) TicketAccounts(ctx context.Context, query lotterydomain.TicketQuery) (
	*lotterydomain.Page[lotterydomain.TicketAccount], error) {
	return s.pg.ListLotteryTicketAccounts(ctx, query)
}

// TicketAccount 管理端：一个人的账户与批次。
func (s *LotteryService) TicketAccount(ctx context.Context, appID, userID int64) (*lotterydomain.TicketAccount, error) {
	account, err := s.pg.LotteryTicketAccount(ctx, appID, userID)
	if err != nil {
		return nil, s.translate(err)
	}
	return account, nil
}

// TicketLedger 管理端：次数流水（可按人、种类、来源、关键词筛）。
func (s *LotteryService) TicketLedger(ctx context.Context, query lotterydomain.TicketQuery) (
	*lotterydomain.Page[lotterydomain.TicketEntry], error) {
	if query.Kind != "" && query.Kind != lotterydomain.LedgerGrant && query.Kind != lotterydomain.LedgerConsume &&
		query.Kind != lotterydomain.LedgerDeduct {
		return nil, apperrors.New(40000, http.StatusBadRequest, "未知的流水种类")
	}
	return s.pg.ListLotteryTicketLedger(ctx, query)
}

// AdjustTickets 管理端：给一个人发放或扣减抽奖次数，返回调整后的账户。发放时给用户发一条站内信。
func (s *LotteryService) AdjustTickets(ctx context.Context, adjust lotterydomain.TicketAdjust) (*lotterydomain.TicketAccount, error) {
	adjust, err := adjust.Normalize()
	if err != nil {
		return nil, apperrors.New(40000, http.StatusBadRequest, err.Error())
	}
	if err := s.pg.AdjustLotteryTickets(ctx, adjust); err != nil {
		if errors.Is(err, pgrepo.ErrLotteryTicketsInsufficient) {
			return nil, apperrors.New(40000, http.StatusBadRequest, "这个人的抽奖次数不够扣")
		}
		return nil, s.translate(err)
	}
	account, err := s.pg.LotteryTicketAccount(ctx, adjust.AppID, adjust.UserID)
	if err != nil {
		return nil, s.translate(err)
	}
	s.publishUser(adjust.AppID, adjust.UserID, "lottery.tickets", map[string]any{"balance": account.Balance})
	if adjust.Amount > 0 {
		s.notifyTickets(adjust.AppID, adjust.UserID, "获得抽奖次数",
			"你获得了 "+strconv.FormatInt(adjust.Amount, 10)+" 次抽奖机会（"+adjust.Reason+"），现有 "+
				strconv.FormatInt(account.Balance, 10)+" 次。")
	}
	return account, nil
}

func (s *LotteryService) notifyTickets(appID, userID int64, title, content string) {
	if s.notifier == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := s.notifier.Deliver(ctx, notificationdomain.DeliverInput{
			AppID: appID, UserID: userID, Type: notificationdomain.TypeLottery,
			Title: title, Content: content, Level: "success",
			Action: notificationdomain.RouteAction(notificationdomain.RouteLottery, "去抽奖"),
		}); err != nil {
			s.log.Warn("lottery: notify tickets failed", zap.Int64("appid", appID), zap.Int64("userId", userID), zap.Error(err))
		}
	}()
}
