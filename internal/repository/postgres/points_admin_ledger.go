package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	pointdomain "aegis/internal/domain/points"
)

// 管理端按用户查看积分 / 经验流水与汇总。
//
// 两张流水表结构一致（经验表多出等级列），这里按 Kind 选表名；表名来自固定白名单，
// 不拼接任何外部输入。方向按 amount 正负划分，见 pointdomain.LedgerQuery 的说明。

func ledgerTable(kind pointdomain.Kind) (string, error) {
	switch kind {
	case pointdomain.KindIntegral:
		return "integral_transactions", nil
	case pointdomain.KindExperience:
		return "experience_transactions", nil
	}
	return "", fmt.Errorf("points ledger: unknown kind %q", kind)
}

func ledgerColumns(kind pointdomain.Kind) string {
	return ledgerColumnsAs(kind, "")
}

// ledgerColumnsAs 同 ledgerColumns，列名带表别名前缀（与 users 等表联查时避免列名歧义）。
func ledgerColumnsAs(kind pointdomain.Kind, alias string) string {
	t := alias
	levels := "0 AS level_before, 0 AS level_after"
	levelUp := "false AS is_level_up"
	if kind == pointdomain.KindExperience {
		levels = t + "level_before, " + t + "level_after"
		levelUp = t + "is_level_up"
	}
	return t + `id, ` + t + `transaction_no, ` + t + `user_id, ` + t + `appid, ` + t + `type, ` + t + `category, ` +
		t + `amount, ` + t + `balance_before, ` + t + `balance_after, ` + levels +
		`, ` + t + `status, ` + t + `title, COALESCE(` + t + `description, ''), ` + t + `source_id, COALESCE(` + t + `source_type, ''), ` +
		t + `multiplier, ` + levelUp +
		`, COALESCE(` + t + `client_ip, ''), COALESCE(` + t + `user_agent, ''), COALESCE(` + t + `extra_data, '{}'::jsonb), ` + t + `created_at`
}

// ledgerWhere 拼出筛选条件；返回的条件串只引用占位符，参数按序追加在 args 里。
func ledgerWhere(q pointdomain.LedgerQuery) (string, []any) {
	conds := []string{"user_id = $1", "appid = $2"}
	args := []any{q.UserID, q.AppID}
	add := func(cond string, value any) {
		args = append(args, value)
		conds = append(conds, strings.ReplaceAll(cond, "?", fmt.Sprintf("$%d", len(args))))
	}
	switch q.Direction {
	case "in":
		conds = append(conds, "amount > 0")
	case "out":
		conds = append(conds, "amount < 0")
	}
	if c := strings.TrimSpace(q.Category); c != "" {
		add("category = ?", c)
	}
	if k := strings.TrimSpace(q.Keyword); k != "" {
		// 同一个参数在条件里引用多次：add 会把每个 ? 都换成同一个占位符
		add(`(transaction_no ILIKE ? OR title ILIKE ? OR COALESCE(description, '') ILIKE ?`+
			` OR COALESCE(extra_data->>'reason', '') ILIKE ? OR COALESCE(extra_data->>'adminAccount', '') ILIKE ?)`,
			"%"+escapeLikePattern(k)+"%")
	}
	if q.From != nil {
		add("created_at >= ?", *q.From)
	}
	if q.To != nil {
		add("created_at < ?", *q.To)
	}
	return strings.Join(conds, " AND "), args
}

// AdminListPointLedger 分页列出某用户的积分或经验流水，附带当前筛选下的入账、出账合计与当前余额。
func (r *Repository) AdminListPointLedger(ctx context.Context, q pointdomain.LedgerQuery) (*pointdomain.LedgerPage, error) {
	table, err := ledgerTable(q.Kind)
	if err != nil {
		return nil, err
	}
	if q.Page < 1 {
		q.Page = 1
	}
	if q.Limit < 1 {
		q.Limit = 20
	}
	where, args := ledgerWhere(q)

	page := &pointdomain.LedgerPage{Page: q.Page, Limit: q.Limit, Kind: q.Kind, Items: []pointdomain.Transaction{}}
	if err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*), COALESCE(SUM(amount) FILTER (WHERE amount > 0), 0), COALESCE(-SUM(amount) FILTER (WHERE amount < 0), 0) FROM `+table+` WHERE `+where,
		args...,
	).Scan(&page.Total, &page.SumIn, &page.SumOut); err != nil {
		return nil, err
	}

	balanceColumn := "integral"
	if q.Kind == pointdomain.KindExperience {
		balanceColumn = "experience"
	}
	if err := r.pool.QueryRow(ctx, `SELECT `+balanceColumn+` FROM users WHERE id = $1 AND appid = $2`, q.UserID, q.AppID).Scan(&page.Balance); err != nil && err != pgx.ErrNoRows {
		return nil, err
	}

	listArgs := append(append([]any{}, args...), q.Limit, (q.Page-1)*q.Limit)
	rows, err := r.pool.Query(ctx,
		fmt.Sprintf(`SELECT %s FROM %s WHERE %s ORDER BY created_at DESC, id DESC LIMIT $%d OFFSET $%d`,
			ledgerColumns(q.Kind), table, where, len(args)+1, len(args)+2),
		listArgs...,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanTransaction(rows)
		if err != nil {
			return nil, err
		}
		page.Items = append(page.Items, *item)
	}
	return page, rows.Err()
}

// AdminGetPointLedgerTransaction 按流水号取一笔，限定在该用户与应用内。
func (r *Repository) AdminGetPointLedgerTransaction(ctx context.Context, kind pointdomain.Kind, appID, userID int64, transactionNo string) (*pointdomain.Transaction, error) {
	table, err := ledgerTable(kind)
	if err != nil {
		return nil, err
	}
	row := r.pool.QueryRow(ctx,
		`SELECT `+ledgerColumns(kind)+` FROM `+table+` WHERE appid = $1 AND user_id = $2 AND transaction_no = $3`,
		appID, userID, transactionNo,
	)
	item, err := scanTransaction(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return item, err
}

// AdminPointLedgerSummary 汇总某用户的积分或经验：总入账 / 出账、管理员调整、按分类聚合、近 days 天的每日走势。
// 日期按 loc 切日，与控制台使用的浏览器时区一致。
func (r *Repository) AdminPointLedgerSummary(ctx context.Context, kind pointdomain.Kind, appID, userID int64, days int, loc *time.Location) (*pointdomain.LedgerSummary, error) {
	table, err := ledgerTable(kind)
	if err != nil {
		return nil, err
	}
	if loc == nil {
		loc = time.UTC
	}
	summary := &pointdomain.LedgerSummary{
		Kind: kind, Days: days, TimeZone: loc.String(),
		Categories: []pointdomain.LedgerCategory{}, Daily: []pointdomain.LedgerDay{},
	}

	balanceColumn := "integral"
	if kind == pointdomain.KindExperience {
		balanceColumn = "experience"
	}
	if err := r.pool.QueryRow(ctx, `SELECT `+balanceColumn+` FROM users WHERE id = $1 AND appid = $2`, userID, appID).Scan(&summary.Balance); err != nil && err != pgx.ErrNoRows {
		return nil, err
	}

	// 管理员调整的分类：admin_adjust（单笔）与 admin_manual（批量）
	if err := r.pool.QueryRow(ctx, `
SELECT COUNT(*),
       COALESCE(SUM(amount) FILTER (WHERE amount > 0), 0),
       COALESCE(-SUM(amount) FILTER (WHERE amount < 0), 0),
       COALESCE(SUM(amount) FILTER (WHERE amount > 0 AND category IN ('admin_adjust', 'admin_manual')), 0),
       COALESCE(-SUM(amount) FILTER (WHERE amount < 0 AND category IN ('admin_adjust', 'admin_manual')), 0),
       MIN(created_at), MAX(created_at)
FROM `+table+` WHERE user_id = $1 AND appid = $2`, userID, appID,
	).Scan(&summary.Count, &summary.TotalIn, &summary.TotalOut, &summary.AdminIn, &summary.AdminOut, &summary.FirstAt, &summary.LastAt); err != nil {
		return nil, err
	}

	rows, err := r.pool.Query(ctx, `
SELECT category,
       (ARRAY_AGG(title ORDER BY created_at DESC, id DESC))[1],
       COALESCE(SUM(amount) FILTER (WHERE amount > 0), 0),
       COALESCE(-SUM(amount) FILTER (WHERE amount < 0), 0),
       COUNT(*), MAX(created_at)
FROM `+table+` WHERE user_id = $1 AND appid = $2
GROUP BY category
ORDER BY COUNT(*) DESC, category
LIMIT 50`, userID, appID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var item pointdomain.LedgerCategory
		if err := rows.Scan(&item.Category, &item.Title, &item.In, &item.Out, &item.Count, &item.LastAt); err != nil {
			rows.Close()
			return nil, err
		}
		summary.Categories = append(summary.Categories, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if days <= 0 {
		return summary, nil
	}
	now := time.Now().In(loc)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -(days - 1))

	// 窗口开始时的余额：窗口前最后一笔的 balance_after；窗口前没有流水时取窗口内第一笔的 balance_before
	// （注册赠送等不走流水的初始值也能算进去），整段都没有流水时为 0
	err = r.pool.QueryRow(ctx, `
SELECT COALESCE(
  (SELECT balance_after FROM `+table+` WHERE user_id = $1 AND appid = $2 AND created_at < $3 ORDER BY created_at DESC, id DESC LIMIT 1),
  (SELECT balance_before FROM `+table+` WHERE user_id = $1 AND appid = $2 AND created_at >= $3 ORDER BY created_at, id LIMIT 1),
  0)`, userID, appID, start).Scan(&summary.BalanceBefore)
	if err != nil {
		return nil, err
	}

	dayRows, err := r.pool.Query(ctx, `
SELECT TO_CHAR(created_at AT TIME ZONE $4, 'YYYY-MM-DD') AS day,
       COALESCE(SUM(amount) FILTER (WHERE amount > 0), 0),
       COALESCE(-SUM(amount) FILTER (WHERE amount < 0), 0),
       COUNT(*),
       (ARRAY_AGG(balance_after ORDER BY created_at DESC, id DESC))[1]
FROM `+table+` WHERE user_id = $1 AND appid = $2 AND created_at >= $3
GROUP BY day ORDER BY day`, userID, appID, start, loc.String())
	if err != nil {
		return nil, err
	}
	defer dayRows.Close()
	for dayRows.Next() {
		var item pointdomain.LedgerDay
		if err := dayRows.Scan(&item.Date, &item.In, &item.Out, &item.Count, &item.Balance); err != nil {
			return nil, err
		}
		summary.Daily = append(summary.Daily, item)
	}
	return summary, dayRows.Err()
}
