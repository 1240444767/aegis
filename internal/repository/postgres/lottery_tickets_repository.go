package postgres

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	lotterydomain "aegis/internal/domain/lottery"
)

// 抽奖次数（抽奖的 bonus 来源）。
//
// 应用级、任意活动可用。看激励广告、卡密、积分商城、邀请、排行榜与抽奖本身发的「抽奖次数」，每一次发放是一批
// （lottery_draw_lots），每一次增减一条流水（lottery_draw_ledger）；lottery_draw_grants 是这个人全部批次的汇总，
// 发放与扣减都先锁它这一行，同一个人的次数变动是串行的。扣减在抽奖事务里（DrawLottery），与发奖同成同败。
//
// 使用顺序见 lotterydomain.TicketConsumeOrder：赠送的先于兑换的，同一档先到先用。

var (
	// ErrLotteryTicketsInsufficient 扣减时次数不够
	ErrLotteryTicketsInsufficient = errors.New("lottery tickets insufficient")
)

// lotteryTicketUse 从一批里用掉的次数。
type lotteryTicketUse struct {
	LotID  int64
	Origin string
	Paid   bool
	Count  int64
}

// lotteryLockTicketsTx 锁住这个人的次数汇总行并与批次对齐，返回余额。create 为 false 且还没有账户时返回 0、不建行。
//
// 对齐：汇总比批次多（分批记账之前的余额、滚动发布期间旧版本发的）补一批 legacy；汇总比批次少（旧版本直接扣了汇总）
// 按使用顺序从批次里扣掉差额。两种都只在版本交替时出现，对齐之后两边恒等。
func lotteryLockTicketsTx(ctx context.Context, tx pgx.Tx, appID, userID int64, create bool) (int64, error) {
	if create {
		if _, err := tx.Exec(ctx, `INSERT INTO lottery_draw_grants (appid, user_id) VALUES ($1, $2)
ON CONFLICT (appid, user_id) DO NOTHING`, appID, userID); err != nil {
			return 0, err
		}
	}
	var balance int64
	if err := tx.QueryRow(ctx, `SELECT balance FROM lottery_draw_grants WHERE appid = $1 AND user_id = $2 FOR UPDATE`,
		appID, userID).Scan(&balance); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	var open int64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(SUM(remaining), 0) FROM lottery_draw_lots
WHERE appid = $1 AND user_id = $2 AND remaining > 0`, appID, userID).Scan(&open); err != nil {
		return 0, err
	}
	switch {
	case balance > open:
		var lotID int64
		diff := balance - open
		if err := tx.QueryRow(ctx, `INSERT INTO lottery_draw_lots (appid, user_id, origin, paid, amount, remaining, title)
VALUES ($1, $2, $3, FALSE, $4, $4, '早期抽奖次数') RETURNING id`, appID, userID, lotterydomain.TicketOriginLegacy, diff).
			Scan(&lotID); err != nil {
			return 0, err
		}
		if err := insertLotteryLedgerTx(ctx, tx, appID, userID, &lotID, lotterydomain.LedgerGrant,
			lotterydomain.TicketOriginLegacy, false, diff, balance, "", "早期抽奖次数", ""); err != nil {
			return 0, err
		}
	case balance < open:
		if _, err := takeLotteryLotsTx(ctx, tx, appID, userID, open-balance, open,
			lotterydomain.LedgerConsume, "", "抽奖使用（对账）", ""); err != nil {
			return 0, err
		}
	}
	return balance, nil
}

// takeLotteryLotsTx 按使用顺序从批次里取 count 次，逐批记流水（不动汇总行，由调用方更新）。before 是取之前的余额。
func takeLotteryLotsTx(ctx context.Context, tx pgx.Tx, appID, userID, count, before int64, kind, ref, title,
	operator string) ([]lotteryTicketUse, error) {
	uses := make([]lotteryTicketUse, 0, 1)
	balance := before
	for left := count; left > 0; {
		var use lotteryTicketUse
		var remaining int64
		if err := tx.QueryRow(ctx, `SELECT id, origin, paid, remaining FROM lottery_draw_lots
WHERE appid = $1 AND user_id = $2 AND remaining > 0
ORDER BY `+lotterydomain.TicketConsumeOrder+` LIMIT 1 FOR UPDATE`, appID, userID).
			Scan(&use.LotID, &use.Origin, &use.Paid, &remaining); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, ErrLotteryTicketsInsufficient
			}
			return nil, err
		}
		use.Count = min(remaining, left)
		if _, err := tx.Exec(ctx, `UPDATE lottery_draw_lots SET remaining = remaining - $2, updated_at = NOW() WHERE id = $1`,
			use.LotID, use.Count); err != nil {
			return nil, err
		}
		balance -= use.Count
		lotID := use.LotID
		if err := insertLotteryLedgerTx(ctx, tx, appID, userID, &lotID, kind, use.Origin, use.Paid, -use.Count, balance,
			ref, title, operator); err != nil {
			return nil, err
		}
		uses = append(uses, use)
		left -= use.Count
	}
	return uses, nil
}

func insertLotteryLedgerTx(ctx context.Context, tx pgx.Tx, appID, userID int64, lotID *int64, kind, origin string, paid bool,
	delta, balanceAfter int64, ref, title, operator string) error {
	_, err := tx.Exec(ctx, `INSERT INTO lottery_draw_ledger
(appid, user_id, lot_id, kind, origin, paid, delta, balance_after, ref, title, operator)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		appID, userID, lotID, kind, origin, paid, delta, balanceAfter, truncateColumn(ref, 64), truncateColumn(title, 128),
		nullableString(truncateColumn(operator, 128)))
	return err
}

// grantLotteryDrawsTx 事务内发放抽奖次数（积分商城、卡密、激励广告、邀请、排行榜、抽奖奖品在各自的事务里调用），返回发放后的余额。
//
// 汇总行用 UPSERT 建出来再锁，而不是「先查再插」：后者在同一用户并发领两张卡时会有一个撞唯一约束，
// 而那次失败会把整个核销事务连带回滚 —— 用户看到的是「兑换失败」，卡却已经作废。
func grantLotteryDrawsTx(ctx context.Context, tx pgx.Tx, grant lotterydomain.TicketGrant) (int64, error) {
	if grant.Amount <= 0 {
		return lotteryLockTicketsTx(ctx, tx, grant.AppID, grant.UserID, false)
	}
	before, err := lotteryLockTicketsTx(ctx, tx, grant.AppID, grant.UserID, true)
	if err != nil {
		return 0, err
	}
	origin := grant.Origin
	if origin == "" {
		origin = lotterydomain.TicketOriginAdmin
	}
	title := strings.TrimSpace(grant.Title)
	if title == "" {
		title = lotterydomain.TicketOriginLabel(origin)
	}
	var lotID int64
	if err := tx.QueryRow(ctx, `INSERT INTO lottery_draw_lots (appid, user_id, origin, paid, amount, remaining, ref, title)
VALUES ($1, $2, $3, $4, $5, $5, $6, $7) RETURNING id`, grant.AppID, grant.UserID, origin, grant.Paid, grant.Amount,
		truncateColumn(grant.Ref, 64), truncateColumn(title, 128)).Scan(&lotID); err != nil {
		return 0, err
	}
	balance := before + grant.Amount
	if _, err := tx.Exec(ctx, `UPDATE lottery_draw_grants
SET balance = $3, total_granted = total_granted + $4, updated_at = NOW() WHERE appid = $1 AND user_id = $2`,
		grant.AppID, grant.UserID, balance, grant.Amount); err != nil {
		return 0, err
	}
	if err := insertLotteryLedgerTx(ctx, tx, grant.AppID, grant.UserID, &lotID, lotterydomain.LedgerGrant, origin, grant.Paid,
		grant.Amount, balance, grant.Ref, title, grant.Operator); err != nil {
		return 0, err
	}
	return balance, nil
}

// consumeLotteryTicketTx 抽奖事务里用掉一次：按使用顺序取下一批。没有次数时返回 ErrLotteryTicketsInsufficient。
func consumeLotteryTicketTx(ctx context.Context, tx pgx.Tx, appID, userID int64, ref, title string) (lotteryTicketUse, error) {
	balance, err := lotteryLockTicketsTx(ctx, tx, appID, userID, false)
	if err != nil {
		return lotteryTicketUse{}, err
	}
	if balance <= 0 {
		return lotteryTicketUse{}, ErrLotteryTicketsInsufficient
	}
	uses, err := takeLotteryLotsTx(ctx, tx, appID, userID, 1, balance, lotterydomain.LedgerConsume, ref, title, "")
	if err != nil {
		return lotteryTicketUse{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE lottery_draw_grants
SET balance = balance - 1, total_consumed = total_consumed + 1, updated_at = NOW()
WHERE appid = $1 AND user_id = $2 AND balance > 0`, appID, userID); err != nil {
		return lotteryTicketUse{}, err
	}
	return uses[0], nil
}

// lotteryTicketFactsQ 余额与其中兑换所得的部分（判定次数用；lock 时锁汇总行）。
func lotteryTicketFactsQ(ctx context.Context, q queryExecutor, appID, userID int64, lock bool) (int64, int64, error) {
	balanceSQL := `SELECT balance FROM lottery_draw_grants WHERE appid = $1 AND user_id = $2`
	if lock {
		balanceSQL += ` FOR UPDATE`
	}
	var balance, purchased int64
	if err := q.QueryRow(ctx, balanceSQL, appID, userID).Scan(&balance); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, 0, nil
		}
		return 0, 0, err
	}
	if balance <= 0 {
		return 0, 0, nil
	}
	if err := q.QueryRow(ctx, `SELECT COALESCE(SUM(remaining), 0) FROM lottery_draw_lots
WHERE appid = $1 AND user_id = $2 AND paid AND remaining > 0`, appID, userID).Scan(&purchased); err != nil {
		return 0, 0, err
	}
	return balance, min(purchased, balance), nil
}

// GetLotteryDrawBalance 查询剩余抽奖次数；没有账户时返回 0。
func (r *Repository) GetLotteryDrawBalance(ctx context.Context, appID int64, userID int64) (int64, error) {
	balance, _, err := lotteryTicketFactsQ(ctx, r.pool, appID, userID, false)
	return balance, err
}

// ── 查询 ──

// LotteryTicketAccount 一个人的抽奖次数：余额、两档拆分、累计、还有剩余的批次（按使用顺序）。
func (r *Repository) LotteryTicketAccount(ctx context.Context, appID, userID int64) (*lotterydomain.TicketAccount, error) {
	account := &lotterydomain.TicketAccount{UserID: userID, Order: lotterydomain.ConsumptionOrder(), Lots: []lotterydomain.TicketLot{}}
	var updatedAt time.Time
	err := r.pool.QueryRow(ctx, `SELECT COALESCE(u.account, ''), COALESCE(up.nickname, ''),
  COALESCE(g.balance, 0), COALESCE(g.total_granted, 0), COALESCE(g.total_consumed, 0), COALESCE(g.updated_at, u.created_at)
FROM users u
LEFT JOIN user_profiles up ON up.user_id = u.id
LEFT JOIN lottery_draw_grants g ON g.appid = u.appid AND g.user_id = u.id
WHERE u.id = $1 AND u.appid = $2`, userID, appID).Scan(&account.Account, &account.Nickname, &account.Balance,
		&account.TotalGranted, &account.TotalConsumed, &updatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}
	account.UpdatedAt = &updatedAt
	rows, err := r.pool.Query(ctx, `SELECT id, origin, paid, title, ref, amount, remaining, created_at FROM lottery_draw_lots
WHERE appid = $1 AND user_id = $2 AND remaining > 0 ORDER BY `+lotterydomain.TicketConsumeOrder+` LIMIT 200`, appID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var purchased int64
	for rows.Next() {
		var lot lotterydomain.TicketLot
		if err := rows.Scan(&lot.ID, &lot.Origin, &lot.Paid, &lot.Title, &lot.Ref, &lot.Amount, &lot.Remaining, &lot.CreatedAt); err != nil {
			return nil, err
		}
		lot.OriginLabel = lotterydomain.TicketOriginLabel(lot.Origin)
		if lot.Paid {
			purchased += lot.Remaining
		}
		account.Lots = append(account.Lots, lot)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	account.Tickets = lotterydomain.SplitTickets(account.Balance, purchased)
	return account, nil
}

func ticketPage(page, limit int) (int, int) {
	if page < 1 {
		page = 1
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	return page, limit
}

// ListLotteryTicketLedger 次数流水（新的在前）。UserID 为 0 时是全应用的（管理端）。
func (r *Repository) ListLotteryTicketLedger(ctx context.Context, query lotterydomain.TicketQuery) (
	*lotterydomain.Page[lotterydomain.TicketEntry], error) {
	page, limit := ticketPage(query.Page, query.Limit)
	where := []string{"l.appid = $1"}
	args := []any{query.AppID}
	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, strings.ReplaceAll(clause, "?", "$"+strconv.Itoa(len(args))))
	}
	if query.UserID > 0 {
		add("l.user_id = ?", query.UserID)
	}
	if query.Kind != "" {
		add("l.kind = ?", query.Kind)
	}
	if query.Origin != "" {
		add("l.origin = ?", query.Origin)
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		// add 把每个 ? 换成同一个参数
		add("(u.account ILIKE ? OR up.nickname ILIKE ? OR l.ref ILIKE ? OR l.title ILIKE ?)", "%"+keyword+"%")
	}
	joins := `FROM lottery_draw_ledger l
LEFT JOIN users u ON u.id = l.user_id
LEFT JOIN user_profiles up ON up.user_id = l.user_id`
	whereSQL := strings.Join(where, " AND ")
	result := &lotterydomain.Page[lotterydomain.TicketEntry]{Items: []lotterydomain.TicketEntry{}, Page: page, Limit: limit}
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) `+joins+` WHERE `+whereSQL, args...).Scan(&result.Total); err != nil {
		return nil, err
	}
	result.TotalPages = int((result.Total + int64(limit) - 1) / int64(limit))
	if result.Total == 0 {
		return result, nil
	}
	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, `SELECT l.id, l.user_id, COALESCE(u.account, ''), COALESCE(up.nickname, ''), l.lot_id, l.kind,
  l.origin, l.paid, l.delta, l.balance_after, l.title, l.ref, COALESCE(l.operator, ''), l.created_at `+joins+`
WHERE `+whereSQL+` ORDER BY l.id DESC LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var entry lotterydomain.TicketEntry
		if err := rows.Scan(&entry.ID, &entry.UserID, &entry.Account, &entry.Nickname, &entry.LotID, &entry.Kind, &entry.Origin,
			&entry.Paid, &entry.Delta, &entry.BalanceAfter, &entry.Title, &entry.Ref, &entry.Operator, &entry.CreatedAt); err != nil {
			return nil, err
		}
		entry.KindLabel = lotterydomain.LedgerKindLabel(entry.Kind)
		entry.OriginLabel = lotterydomain.TicketOriginLabel(entry.Origin)
		result.Items = append(result.Items, entry)
	}
	return result, rows.Err()
}

// ListLotteryTicketAccounts 有抽奖次数账户的人（管理端），余额多的在前。
func (r *Repository) ListLotteryTicketAccounts(ctx context.Context, query lotterydomain.TicketQuery) (
	*lotterydomain.Page[lotterydomain.TicketAccount], error) {
	page, limit := ticketPage(query.Page, query.Limit)
	where := []string{"g.appid = $1"}
	args := []any{query.AppID}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		args = append(args, "%"+keyword+"%")
		clause := "(u.account ILIKE $2 OR up.nickname ILIKE $2"
		if id, err := strconv.ParseInt(keyword, 10, 64); err == nil {
			args = append(args, id)
			clause += " OR g.user_id = $3"
		}
		where = append(where, clause+")")
	}
	if query.UserID > 0 {
		args = append(args, query.UserID)
		where = append(where, "g.user_id = $"+strconv.Itoa(len(args)))
	}
	joins := `FROM lottery_draw_grants g
JOIN users u ON u.id = g.user_id
LEFT JOIN user_profiles up ON up.user_id = g.user_id`
	whereSQL := strings.Join(where, " AND ")
	result := &lotterydomain.Page[lotterydomain.TicketAccount]{Items: []lotterydomain.TicketAccount{}, Page: page, Limit: limit}
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) `+joins+` WHERE `+whereSQL, args...).Scan(&result.Total); err != nil {
		return nil, err
	}
	result.TotalPages = int((result.Total + int64(limit) - 1) / int64(limit))
	if result.Total == 0 {
		return result, nil
	}
	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, `SELECT g.user_id, COALESCE(u.account, ''), COALESCE(up.nickname, ''), g.balance,
  COALESCE((SELECT SUM(l.remaining) FROM lottery_draw_lots l
            WHERE l.appid = g.appid AND l.user_id = g.user_id AND l.paid AND l.remaining > 0), 0),
  g.total_granted, g.total_consumed, g.updated_at `+joins+`
WHERE `+whereSQL+` ORDER BY g.balance DESC, g.updated_at DESC LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var account lotterydomain.TicketAccount
		var purchased int64
		var updatedAt time.Time
		if err := rows.Scan(&account.UserID, &account.Account, &account.Nickname, &account.Balance, &purchased,
			&account.TotalGranted, &account.TotalConsumed, &updatedAt); err != nil {
			return nil, err
		}
		account.Tickets = lotterydomain.SplitTickets(account.Balance, purchased)
		account.UpdatedAt = &updatedAt
		account.Lots = []lotterydomain.TicketLot{}
		account.Order = lotterydomain.ConsumptionOrder()
		result.Items = append(result.Items, account)
	}
	return result, rows.Err()
}

// LotteryTicketOverview 次数总览：持有人数、未用的次数（两档）、今天发了几次用了几次、各来源的累计与剩余。
func (r *Repository) LotteryTicketOverview(ctx context.Context, appID int64, dayStart time.Time) (*lotterydomain.TicketOverview, error) {
	overview := &lotterydomain.TicketOverview{Origins: []lotterydomain.TicketOriginStat{}, Order: lotterydomain.ConsumptionOrder()}
	var purchased int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FILTER (WHERE balance > 0), COALESCE(SUM(balance), 0)
FROM lottery_draw_grants WHERE appid = $1`, appID).Scan(&overview.Holders, &overview.Outstanding); err != nil {
		return nil, err
	}
	if err := r.pool.QueryRow(ctx, `SELECT
  COALESCE(SUM(delta) FILTER (WHERE kind = 'grant'), 0),
  COALESCE(-SUM(delta) FILTER (WHERE kind = 'consume'), 0)
FROM lottery_draw_ledger WHERE appid = $1 AND created_at >= $2`, appID, dayStart).
		Scan(&overview.GrantedToday, &overview.UsedToday); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT origin, paid, SUM(amount), SUM(remaining), COUNT(DISTINCT user_id)
FROM lottery_draw_lots WHERE appid = $1 GROUP BY origin, paid ORDER BY SUM(amount) DESC`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var stat lotterydomain.TicketOriginStat
		if err := rows.Scan(&stat.Origin, &stat.Paid, &stat.Granted, &stat.Remaining, &stat.Users); err != nil {
			return nil, err
		}
		stat.OriginLabel = lotterydomain.TicketOriginLabel(stat.Origin)
		if stat.Paid {
			purchased += stat.Remaining
		}
		overview.Origins = append(overview.Origins, stat)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	overview.Tickets = lotterydomain.SplitTickets(overview.Outstanding, purchased)
	return overview, nil
}

// ── 管理员增减 ──

// AdjustLotteryTickets 管理员发放（Amount > 0，进一批 admin）或扣减（Amount < 0，按使用顺序从赠送的开始扣）。
func (r *Repository) AdjustLotteryTickets(ctx context.Context, adjust lotterydomain.TicketAdjust) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	var locked int64
	if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`,
		adjust.UserID, adjust.AppID).Scan(&locked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrUserNotFound
		}
		return err
	}
	ref := "ADM" + time.Now().UTC().Format("20060102150405")
	if adjust.Amount > 0 {
		if _, err := grantLotteryDrawsTx(ctx, tx, lotterydomain.TicketGrant{
			AppID: adjust.AppID, UserID: adjust.UserID, Amount: adjust.Amount, Origin: lotterydomain.TicketOriginAdmin,
			Paid: adjust.Paid, Ref: ref, Title: adjust.Reason, Operator: adjust.Operator,
		}); err != nil {
			return err
		}
	} else {
		count := -adjust.Amount
		balance, err := lotteryLockTicketsTx(ctx, tx, adjust.AppID, adjust.UserID, false)
		if err != nil {
			return err
		}
		if balance < count {
			return ErrLotteryTicketsInsufficient
		}
		if _, err := takeLotteryLotsTx(ctx, tx, adjust.AppID, adjust.UserID, count, balance, lotterydomain.LedgerDeduct,
			ref, adjust.Reason, adjust.Operator); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE lottery_draw_grants SET balance = balance - $3, updated_at = NOW()
WHERE appid = $1 AND user_id = $2`, adjust.AppID, adjust.UserID, count); err != nil {
			return err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	tx = nil
	return nil
}
