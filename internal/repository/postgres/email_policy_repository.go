package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	emaildomain "aegis/internal/domain/email"
)

// 邮件策略、抑制名单、投递详情与概览统计。作用域的 0 ↔ NULL 映射与 email_repository.go 一致。

// emailScopeConditionOn 带表别名的作用域条件（联表查询用）。
func emailScopeConditionOn(alias string, appID int64, args *[]any) string {
	if appID == emaildomain.PlatformAppID {
		return alias + ".appid IS NULL"
	}
	*args = append(*args, appID)
	return fmt.Sprintf("%s.appid = $%d", alias, len(*args))
}

// ── 策略 ──

// GetEmailPolicy 作用域的邮件策略；没保存过时返回默认策略（Configured 为 false）。
func (r *Repository) GetEmailPolicy(ctx context.Context, appID int64) (*emaildomain.PolicyRecord, error) {
	record := emaildomain.PolicyRecord{AppID: appID, Policy: emaildomain.DefaultPolicy()}
	args := make([]any, 0, 1)
	scope := emailScopeCondition(appID, &args)
	var raw []byte
	var updatedAt time.Time
	err := r.pool.QueryRow(ctx, `SELECT policy, COALESCE(updated_by, ''), updated_at FROM email_policies WHERE `+scope, args...).
		Scan(&raw, &record.UpdatedBy, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return &record, nil
	}
	if err != nil {
		return nil, err
	}
	if len(raw) > 0 {
		// 缺的字段保留默认值：新加的策略项对老策略也有合理取值
		if err := json.Unmarshal(raw, &record.Policy); err != nil {
			return nil, fmt.Errorf("decode email policy: %w", err)
		}
	}
	record.Policy = emaildomain.NormalizePolicy(record.Policy)
	record.Configured = true
	record.UpdatedAt = &updatedAt
	return &record, nil
}

// SaveEmailPolicy 保存策略（调用方已规整、校验）。
func (r *Repository) SaveEmailPolicy(ctx context.Context, appID int64, policy emaildomain.Policy, operator string) (*emaildomain.PolicyRecord, error) {
	raw, err := json.Marshal(policy)
	if err != nil {
		return nil, err
	}
	if _, err := r.pool.Exec(ctx, `INSERT INTO email_policies (appid, policy, updated_by)
VALUES ($1, $2, $3)
ON CONFLICT ((COALESCE(appid, 0))) DO UPDATE SET policy = EXCLUDED.policy,
    updated_by = EXCLUDED.updated_by, updated_at = NOW()`, nullableAppID(appID), raw, nullableString(operator)); err != nil {
		return nil, err
	}
	return r.GetEmailPolicy(ctx, appID)
}

// ── 抑制名单 ──

const emailSuppressionColumns = `id, COALESCE(appid, 0), address, reason, source, note, COALESCE(delivery_id, 0), created_by, expires_at, created_at, updated_at`

func scanEmailSuppression(row interface{ Scan(dest ...any) error }) (*emaildomain.Suppression, error) {
	var item emaildomain.Suppression
	if err := row.Scan(&item.ID, &item.AppID, &item.Address, &item.Reason, &item.Source, &item.Note,
		&item.DeliveryID, &item.CreatedBy, &item.ExpiresAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	return &item, nil
}

// GetActiveEmailSuppression 地址此刻是否在抑制名单里（未到期）。
func (r *Repository) GetActiveEmailSuppression(ctx context.Context, appID int64, address string) (*emaildomain.Suppression, error) {
	args := []any{strings.ToLower(strings.TrimSpace(address))}
	scope := emailScopeCondition(appID, &args)
	item, err := scanEmailSuppression(r.pool.QueryRow(ctx, `SELECT `+emailSuppressionColumns+`
FROM email_suppressions WHERE address = $1 AND `+scope+` AND (expires_at IS NULL OR expires_at > NOW())`, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return item, err
}

// UpsertEmailSuppression 加入或刷新一条抑制记录。同一地址只留一行，后来的原因覆盖先前的。
func (r *Repository) UpsertEmailSuppression(ctx context.Context, item emaildomain.Suppression) (*emaildomain.Suppression, error) {
	return scanEmailSuppression(r.pool.QueryRow(ctx, `INSERT INTO email_suppressions
	(appid, address, reason, source, note, delivery_id, created_by, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT ((COALESCE(appid, 0)), address) DO UPDATE SET
	reason = EXCLUDED.reason, source = EXCLUDED.source, note = EXCLUDED.note,
	delivery_id = COALESCE(EXCLUDED.delivery_id, email_suppressions.delivery_id),
	created_by = EXCLUDED.created_by, expires_at = EXCLUDED.expires_at, updated_at = NOW()
RETURNING `+emailSuppressionColumns,
		nullableAppID(item.AppID), strings.ToLower(strings.TrimSpace(item.Address)), item.Reason, item.Source,
		item.Note, nullableInt64(item.DeliveryID), item.CreatedBy, nullableTimePtr(item.ExpiresAt)))
}

// DeleteEmailSuppression 移出抑制名单。
func (r *Repository) DeleteEmailSuppression(ctx context.Context, appID int64, id int64) (bool, error) {
	args := []any{id}
	scope := emailScopeCondition(appID, &args)
	result, err := r.pool.Exec(ctx, `DELETE FROM email_suppressions WHERE id = $1 AND `+scope, args...)
	if err != nil {
		return false, err
	}
	return result.RowsAffected() > 0, nil
}

// ListEmailSuppressions 分页查询抑制名单。
func (r *Repository) ListEmailSuppressions(ctx context.Context, query emaildomain.SuppressionQuery) (*emaildomain.SuppressionPage, error) {
	page, pageSize := query.Page, query.PageSize
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	args := make([]any, 0, 5)
	conditions := []string{emailScopeCondition(query.AppID, &args)}
	if reason := strings.TrimSpace(query.Reason); reason != "" {
		args = append(args, reason)
		conditions = append(conditions, fmt.Sprintf("reason = $%d", len(args)))
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		args = append(args, "%"+strings.ToLower(keyword)+"%")
		conditions = append(conditions, fmt.Sprintf("(address LIKE $%d OR LOWER(note) LIKE $%d)", len(args), len(args)))
	}
	where := "WHERE " + strings.Join(conditions, " AND ")

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM email_suppressions `+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	args = append(args, pageSize, (page-1)*pageSize)
	rows, err := r.pool.Query(ctx, `SELECT `+emailSuppressionColumns+` FROM email_suppressions `+where+
		fmt.Sprintf(" ORDER BY created_at DESC, id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]emaildomain.Suppression, 0, pageSize)
	for rows.Next() {
		item, err := scanEmailSuppression(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &emaildomain.SuppressionPage{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

// ── 投递详情 ──

// GetEmailDelivery 按 id 取一封信的留痕（限定作用域）。
func (r *Repository) GetEmailDelivery(ctx context.Context, appID int64, id int64) (*emaildomain.Delivery, error) {
	args := []any{id}
	scope := emailScopeCondition(appID, &args)
	// scanEmailDelivery 把「没有这一行」收敛成 (nil, nil)
	return scanEmailDelivery(r.pool.QueryRow(ctx, `SELECT `+emailDeliveryColumns+` FROM email_deliveries WHERE id = $1 AND `+scope, args...))
}

// ListEmailDeliveryEvents 一封信的事件，按发生时间排序。
func (r *Repository) ListEmailDeliveryEvents(ctx context.Context, deliveryID int64) ([]emaildomain.DeliveryEvent, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, delivery_id, event, status, detail, payload, occurred_at
FROM email_delivery_events WHERE delivery_id = $1 ORDER BY occurred_at, id LIMIT 200`, deliveryID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]emaildomain.DeliveryEvent, 0, 8)
	for rows.Next() {
		var item emaildomain.DeliveryEvent
		var payload []byte
		if err := rows.Scan(&item.ID, &item.DeliveryID, &item.Event, &item.Status, &item.Detail, &payload, &item.OccurredAt); err != nil {
			return nil, err
		}
		if len(payload) > 0 {
			_ = json.Unmarshal(payload, &item.Payload)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ── 概览 ──

// EmailDeliveryOverview 累计数字、按天趋势（按 tz 分日）与按用途 / 状态的分布。
func (r *Repository) EmailDeliveryOverview(ctx context.Context, appID int64, days int, tz string) (*emaildomain.DeliveryOverview, error) {
	stats, err := r.EmailDeliveryStats(ctx, appID)
	if err != nil {
		return nil, err
	}
	overview := &emaildomain.DeliveryOverview{DeliveryStats: *stats, Days: days}

	// 趋势：日历按 tz 生成，没有发信的日子也占一格
	args := []any{tz, days}
	scope := emailScopeConditionOn("e", appID, &args)
	rows, err := r.pool.Query(ctx, `WITH span AS (
	SELECT generate_series((NOW() AT TIME ZONE $1)::date - ($2::int - 1), (NOW() AT TIME ZONE $1)::date, INTERVAL '1 day')::date AS day
)
SELECT to_char(span.day, 'YYYY-MM-DD'),
	COUNT(e.id),
	COUNT(e.id) FILTER (WHERE e.status = 'delivered'),
	COUNT(e.id) FILTER (WHERE e.status IN ('failed', 'rejected')),
	COUNT(e.id) FILTER (WHERE e.status IN ('bounced', 'complained'))
FROM span
LEFT JOIN email_deliveries e ON (e.created_at AT TIME ZONE $1)::date = span.day
	AND e.created_at >= NOW() - ($2::int + 1) * INTERVAL '1 day' AND `+scope+`
GROUP BY span.day ORDER BY span.day`, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var point emaildomain.DeliveryTrendPoint
		if err := rows.Scan(&point.Date, &point.Total, &point.Delivered, &point.Failed, &point.Bounced); err != nil {
			rows.Close()
			return nil, err
		}
		overview.WindowTotal += point.Total
		overview.Trend = append(overview.Trend, point)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	windowArgs := []any{tz, days}
	windowScope := emailScopeCondition(appID, &windowArgs)
	windowWhere := `WHERE ` + windowScope + ` AND (created_at AT TIME ZONE $1)::date > (NOW() AT TIME ZONE $1)::date - $2::int`
	breakdown := func(column string) ([]emaildomain.DeliveryCount, error) {
		rows, err := r.pool.Query(ctx, `SELECT COALESCE(NULLIF(`+column+`, ''), 'unknown'), COUNT(*) FROM email_deliveries `+
			windowWhere+` GROUP BY 1 ORDER BY 2 DESC LIMIT 12`, windowArgs...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		items := make([]emaildomain.DeliveryCount, 0, 8)
		for rows.Next() {
			var item emaildomain.DeliveryCount
			if err := rows.Scan(&item.Key, &item.Count); err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		return items, rows.Err()
	}
	if overview.ByPurpose, err = breakdown("purpose"); err != nil {
		return nil, err
	}
	if overview.ByStatus, err = breakdown("status"); err != nil {
		return nil, err
	}

	extraArgs := make([]any, 0, 1)
	extraScope := emailScopeCondition(appID, &extraArgs)
	if err := r.pool.QueryRow(ctx, `SELECT
	(SELECT COUNT(*) FROM email_suppressions WHERE `+extraScope+` AND (expires_at IS NULL OR expires_at > NOW())),
	(SELECT COUNT(*) FROM email_deliveries WHERE `+extraScope+` AND status = 'failed' AND created_at >= NOW() - INTERVAL '24 hours')`,
		extraArgs...).Scan(&overview.Suppressions, &overview.FailedLast24h); err != nil {
		return nil, err
	}
	return overview, nil
}
