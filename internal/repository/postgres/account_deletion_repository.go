package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"aegis/internal/domain/accountdeletion"
)

// 账号注销：配置、申请、墓碑。状态迁移一律写成「WHERE status IN (...)」的条件更新，
// 并发的两次操作（例如用户撤销与后台到点执行）只会有一次落地，另一次拿到 ErrDeletionStateChanged。

var (
	// ErrDeletionRequestNotFound 申请不存在（或不属于这个应用）
	ErrDeletionRequestNotFound = errors.New("account deletion request not found")
	// ErrDeletionOpenExists 这个账号已经有一条进行中的申请
	ErrDeletionOpenExists = errors.New("account deletion request already open")
	// ErrDeletionStateChanged 申请的状态已经不允许这次操作
	ErrDeletionStateChanged = errors.New("account deletion request state changed")
)

// ── 配置 ──

// GetAccountDeletionSettings 应用的注销配置；没保存过时返回关闭 + 默认配置。
func (r *Repository) GetAccountDeletionSettings(ctx context.Context, appID int64) (*accountdeletion.Settings, error) {
	settings := accountdeletion.Settings{AppID: appID, Config: accountdeletion.DefaultConfig()}
	var raw []byte
	var updatedAt time.Time
	err := r.pool.QueryRow(ctx, `SELECT enabled, config, COALESCE(updated_by, ''), updated_at
FROM account_deletion_settings WHERE appid = $1`, appID).Scan(&settings.Enabled, &raw, &settings.UpdatedBy, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return &settings, nil
	}
	if err != nil {
		return nil, err
	}
	if len(raw) > 0 {
		// 缺的字段保留默认值：新加的配置项对老配置也有一个合理的取值
		if err := json.Unmarshal(raw, &settings.Config); err != nil {
			return nil, fmt.Errorf("decode account deletion config: %w", err)
		}
	}
	settings.Config = accountdeletion.NormalizeConfig(settings.Config)
	settings.UpdatedAt = &updatedAt
	settings.Configured = true
	return &settings, nil
}

// SaveAccountDeletionSettings 保存开关与配置（调用方已清洗、校验）。
func (r *Repository) SaveAccountDeletionSettings(ctx context.Context, appID int64, enabled bool, cfg accountdeletion.Config, operator string) (*accountdeletion.Settings, error) {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	if _, err := r.pool.Exec(ctx, `INSERT INTO account_deletion_settings (appid, enabled, config, updated_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (appid) DO UPDATE SET enabled = EXCLUDED.enabled, config = EXCLUDED.config,
    updated_by = EXCLUDED.updated_by, updated_at = NOW()`, appID, enabled, raw, nullableString(operator)); err != nil {
		return nil, err
	}
	return r.GetAccountDeletionSettings(ctx, appID)
}

// ── 前置条件的事实 ──

// AccountDeletionFacts 一次取齐判定前置条件要用的事实。
func (r *Repository) AccountDeletionFacts(ctx context.Context, appID, userID int64) (*accountdeletion.Facts, error) {
	var facts accountdeletion.Facts
	var balance string
	if err := r.pool.QueryRow(ctx, `SELECT
  EXISTS (SELECT 1 FROM user_account_bans b WHERE b.appid = $1 AND b.user_id = $2 AND b.status = 'active'
          AND b.start_at <= NOW() AND (b.end_at IS NULL OR b.end_at > NOW())),
  COALESCE((SELECT b.reason FROM user_account_bans b WHERE b.appid = $1 AND b.user_id = $2 AND b.status = 'active'
          AND b.start_at <= NOW() AND (b.end_at IS NULL OR b.end_at > NOW()) ORDER BY b.id DESC LIMIT 1), ''),
  (SELECT COUNT(*) FROM payment_orders o WHERE o.appid = $1 AND o.user_id = $2 AND o.status = 'pending'
          AND o.created_at > NOW() - INTERVAL '48 hours'),
  (SELECT COUNT(*) FROM payment_refunds f WHERE f.appid = $1 AND f.user_id = $2 AND f.status IN ('pending', 'processing')),
  COALESCE((SELECT (w.balance + w.frozen)::text FROM user_wallets w WHERE w.user_id = $2 AND w.appid = $1), '0'),
  COALESCE((SELECT (w.balance + w.frozen) > 0 FROM user_wallets w WHERE w.user_id = $2 AND w.appid = $1), FALSE),
  u.vip_expire_at, u.vip_lifetime_at IS NOT NULL, u.integral
FROM users u WHERE u.id = $2 AND u.appid = $1`, appID, userID).Scan(
		&facts.BanActive, &facts.BanReason, &facts.PendingOrders, &facts.PendingRefunds,
		&balance, &facts.WalletPositive, &facts.VipExpireAt, &facts.Lifetime, &facts.Points); err != nil {
		return nil, err
	}
	facts.WalletBalance = balance
	return &facts, nil
}

// ── 申请 ──

const deletionRequestColumns = `r.id, r.appid, r.user_id, r.account, r.nickname, r.mode, r.status, r.source,
r.reason_code, r.reason_text, r.cooling_days, r.scheduled_at, r.remind_at, r.reminded_at,
r.reviewed_at, r.reviewed_by, r.review_note, r.cancelled_at, r.cancelled_by, r.cancel_reason,
r.completed_at, r.failure, r.attempts, r.verified_by, r.operator, r.ip, r.device_id, r.device, r.user_agent,
r.snapshot, r.created_at, r.updated_at,
EXISTS (SELECT 1 FROM users u WHERE u.id = r.user_id AND u.appid = r.appid)`

func scanDeletionRequest(row interface{ Scan(dest ...any) error }) (*accountdeletion.Request, error) {
	var req accountdeletion.Request
	var verified, snapshot []byte
	if err := row.Scan(&req.ID, &req.AppID, &req.UserID, &req.Account, &req.Nickname, &req.Mode, &req.Status, &req.Source,
		&req.ReasonCode, &req.ReasonText, &req.CoolingDays, &req.ScheduledAt, &req.RemindAt, &req.RemindedAt,
		&req.ReviewedAt, &req.ReviewedBy, &req.ReviewNote, &req.CancelledAt, &req.CancelledBy, &req.CancelReason,
		&req.CompletedAt, &req.Failure, &req.Attempts, &verified, &req.Operator, &req.IP, &req.DeviceID, &req.Device, &req.UserAgent,
		&snapshot, &req.CreatedAt, &req.UpdatedAt, &req.UserExists); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrDeletionRequestNotFound
		}
		return nil, err
	}
	req.VerifiedBy = []string{}
	if len(verified) > 0 {
		if err := json.Unmarshal(verified, &req.VerifiedBy); err != nil {
			return nil, fmt.Errorf("decode deletion verified_by: %w", err)
		}
	}
	req.Snapshot = map[string]any{}
	if len(snapshot) > 0 {
		if err := json.Unmarshal(snapshot, &req.Snapshot); err != nil {
			return nil, fmt.Errorf("decode deletion snapshot: %w", err)
		}
	}
	return &req, nil
}

// CreateAccountDeletionRequest 写入一条新申请。已有进行中的申请时返回 ErrDeletionOpenExists。
func (r *Repository) CreateAccountDeletionRequest(ctx context.Context, input accountdeletion.RequestInput) (*accountdeletion.Request, error) {
	verified, err := json.Marshal(nonNilStrings(input.VerifiedBy))
	if err != nil {
		return nil, err
	}
	if input.Snapshot == nil {
		input.Snapshot = map[string]any{}
	}
	snapshot, err := json.Marshal(input.Snapshot)
	if err != nil {
		return nil, err
	}
	var id int64
	err = r.pool.QueryRow(ctx, `INSERT INTO account_deletion_requests
(appid, user_id, account, nickname, mode, status, source, reason_code, reason_text, cooling_days,
 scheduled_at, remind_at, verified_by, operator, ip, device_id, device, user_agent, snapshot)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
RETURNING id`,
		input.AppID, input.UserID, input.Account, input.Nickname, input.Mode, input.Status, input.Source,
		input.ReasonCode, input.ReasonText, input.CoolingDays, input.ScheduledAt, input.RemindAt, verified,
		input.Operator, truncateRunes(input.IP, 64), truncateRunes(input.DeviceID, 128), truncateRunes(input.Device, 255),
		input.UserAgent, snapshot).Scan(&id)
	if err != nil {
		if uniqueConstraintName(err) == "uq_account_deletion_open" {
			return nil, ErrDeletionOpenExists
		}
		return nil, err
	}
	return r.GetAccountDeletionRequest(ctx, input.AppID, id)
}

// GetAccountDeletionRequest 按 ID 取一条申请（限定应用）。
func (r *Repository) GetAccountDeletionRequest(ctx context.Context, appID, id int64) (*accountdeletion.Request, error) {
	return scanDeletionRequest(r.pool.QueryRow(ctx, `SELECT `+deletionRequestColumns+`
FROM account_deletion_requests r WHERE r.id = $1 AND r.appid = $2`, id, appID))
}

// GetOpenAccountDeletionRequest 账号当前进行中的申请；没有时为 nil。
func (r *Repository) GetOpenAccountDeletionRequest(ctx context.Context, appID, userID int64) (*accountdeletion.Request, error) {
	req, err := scanDeletionRequest(r.pool.QueryRow(ctx, `SELECT `+deletionRequestColumns+`
FROM account_deletion_requests r
WHERE r.appid = $1 AND r.user_id = $2 AND r.status IN ('reviewing','scheduled','processing','failed')
ORDER BY r.id DESC LIMIT 1`, appID, userID))
	if errors.Is(err, ErrDeletionRequestNotFound) {
		return nil, nil
	}
	return req, err
}

// GetLatestAccountDeletionRequest 账号最近的一条申请（任何状态）；没有时为 nil。
func (r *Repository) GetLatestAccountDeletionRequest(ctx context.Context, appID, userID int64) (*accountdeletion.Request, error) {
	req, err := scanDeletionRequest(r.pool.QueryRow(ctx, `SELECT `+deletionRequestColumns+`
FROM account_deletion_requests r WHERE r.appid = $1 AND r.user_id = $2
ORDER BY r.id DESC LIMIT 1`, appID, userID))
	if errors.Is(err, ErrDeletionRequestNotFound) {
		return nil, nil
	}
	return req, err
}

// ListAccountDeletionRequests 管理端申请列表。
func (r *Repository) ListAccountDeletionRequests(ctx context.Context, query accountdeletion.RequestQuery) (*accountdeletion.RequestPage, error) {
	where := []string{"r.appid = $1"}
	args := []any{query.AppID}
	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(clause, len(args)))
	}
	if query.Status != "" {
		add("r.status = $%d", query.Status)
	}
	if query.Mode != "" {
		add("r.mode = $%d", query.Mode)
	}
	if query.Source != "" {
		add("r.source = $%d", query.Source)
	}
	if query.UserID > 0 {
		add("r.user_id = $%d", query.UserID)
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		args = append(args, "%"+escapeLikePattern(keyword)+"%")
		n := len(args)
		where = append(where, fmt.Sprintf("(r.account ILIKE $%d OR r.nickname ILIKE $%d OR r.reason_text ILIKE $%d OR r.user_id::text = $%d)", n, n, n, n+1))
		args = append(args, keyword)
	}
	if query.From != nil {
		add("r.created_at >= $%d", *query.From)
	}
	if query.To != nil {
		add("r.created_at < $%d", *query.To)
	}
	clause := strings.Join(where, " AND ")
	page := &accountdeletion.RequestPage{Items: []accountdeletion.Request{}, Page: query.Page, Limit: query.Limit}
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM account_deletion_requests r WHERE `+clause, args...).Scan(&page.Total); err != nil {
		return nil, err
	}
	args = append(args, query.Limit, (query.Page-1)*query.Limit)
	rows, err := r.pool.Query(ctx, `SELECT `+deletionRequestColumns+`
FROM account_deletion_requests r WHERE `+clause+fmt.Sprintf(` ORDER BY r.id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		req, err := scanDeletionRequest(rows)
		if err != nil {
			return nil, err
		}
		page.Items = append(page.Items, *req)
	}
	return page, rows.Err()
}

// DeletionTransition 一次状态迁移：From 里任一状态才能迁到 To，Set 为附带要写的列。
type DeletionTransition struct {
	From []string
	To   string
	Set  map[string]any
}

// TransitionAccountDeletionRequest 条件更新一条申请的状态，返回更新后的申请。
// 当前状态不在 From 里时返回 ErrDeletionStateChanged；申请不存在时返回 ErrDeletionRequestNotFound。
func (r *Repository) TransitionAccountDeletionRequest(ctx context.Context, appID, id int64, transition DeletionTransition) (*accountdeletion.Request, error) {
	sets := []string{"status = $3", "updated_at = NOW()"}
	args := []any{id, appID, transition.To, transition.From}
	for column, value := range transition.Set {
		if !deletionWritableColumns[column] {
			return nil, fmt.Errorf("account deletion column %q is not writable", column)
		}
		args = append(args, value)
		if column == "attempts" {
			sets = append(sets, fmt.Sprintf("attempts = attempts + $%d", len(args)))
			continue
		}
		sets = append(sets, fmt.Sprintf("%s = $%d", column, len(args)))
	}
	tag, err := r.pool.Exec(ctx, `UPDATE account_deletion_requests SET `+strings.Join(sets, ", ")+`
WHERE id = $1 AND appid = $2 AND status = ANY($4)`, args...)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		if _, err := r.GetAccountDeletionRequest(ctx, appID, id); err != nil {
			return nil, err
		}
		return nil, ErrDeletionStateChanged
	}
	return r.GetAccountDeletionRequest(ctx, appID, id)
}

var deletionWritableColumns = map[string]bool{
	"scheduled_at": true, "remind_at": true, "reminded_at": true, "reviewed_at": true, "reviewed_by": true,
	"review_note": true, "cancelled_at": true, "cancelled_by": true, "cancel_reason": true, "completed_at": true,
	"failure": true, "attempts": true, "cooling_days": true,
}

// ClaimDueAccountDeletions 认领到期的冷静期申请：scheduled → processing，返回被认领的（最多 limit 条）。
// FOR UPDATE SKIP LOCKED 让多个实例同时跑也不会重复执行。
func (r *Repository) ClaimDueAccountDeletions(ctx context.Context, now time.Time, limit int) ([]accountdeletion.Request, error) {
	rows, err := r.pool.Query(ctx, `WITH due AS (
  SELECT id FROM account_deletion_requests
  WHERE status = 'scheduled' AND scheduled_at <= $1
  ORDER BY scheduled_at LIMIT $2 FOR UPDATE SKIP LOCKED
)
UPDATE account_deletion_requests r SET status = 'processing', attempts = r.attempts + 1, updated_at = NOW()
FROM due WHERE r.id = due.id
RETURNING r.id, r.appid`, now, limit)
	if err != nil {
		return nil, err
	}
	type key struct{ id, appID int64 }
	var claimed []key
	for rows.Next() {
		var k key
		if err := rows.Scan(&k.id, &k.appID); err != nil {
			rows.Close()
			return nil, err
		}
		claimed = append(claimed, k)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	items := make([]accountdeletion.Request, 0, len(claimed))
	for _, k := range claimed {
		req, err := r.GetAccountDeletionRequest(ctx, k.appID, k.id)
		if err != nil {
			return nil, err
		}
		items = append(items, *req)
	}
	return items, nil
}

// ClaimDeletionReminders 认领该发到期提醒的申请（一次性：认领即写 reminded_at）。
func (r *Repository) ClaimDeletionReminders(ctx context.Context, now time.Time, limit int) ([]accountdeletion.Request, error) {
	rows, err := r.pool.Query(ctx, `WITH due AS (
  SELECT id FROM account_deletion_requests
  WHERE status = 'scheduled' AND reminded_at IS NULL AND remind_at IS NOT NULL AND remind_at <= $1 AND scheduled_at > $1
  ORDER BY remind_at LIMIT $2 FOR UPDATE SKIP LOCKED
)
UPDATE account_deletion_requests r SET reminded_at = $1, updated_at = NOW()
FROM due WHERE r.id = due.id
RETURNING r.id, r.appid`, now, limit)
	if err != nil {
		return nil, err
	}
	type key struct{ id, appID int64 }
	var claimed []key
	for rows.Next() {
		var k key
		if err := rows.Scan(&k.id, &k.appID); err != nil {
			rows.Close()
			return nil, err
		}
		claimed = append(claimed, k)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	items := make([]accountdeletion.Request, 0, len(claimed))
	for _, k := range claimed {
		req, err := r.GetAccountDeletionRequest(ctx, k.appID, k.id)
		if err != nil {
			return nil, err
		}
		items = append(items, *req)
	}
	return items, nil
}

// RecoverStaleAccountDeletions 进程在执行中途退出会留下 processing：超过 staleAfter 还没结论的改回 failed，
// 由管理员决定重试还是撤销（不自动重跑：删到一半的账号要人看一眼）。
func (r *Repository) RecoverStaleAccountDeletions(ctx context.Context, staleAfter time.Duration) (int64, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE account_deletion_requests
SET status = 'failed', failure = '执行中断（服务重启或超时），请重试', updated_at = NOW()
WHERE status = 'processing' AND updated_at < $1`, time.Now().Add(-staleAfter))
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ── 墓碑 ──

// CreateAccountDeletionTombstones 注销完成后写入墓碑。
func (r *Repository) CreateAccountDeletionTombstones(ctx context.Context, appID, requestID int64, items []accountdeletion.TombstoneInput) error {
	if len(items) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, item := range items {
		batch.Queue(`INSERT INTO account_deletion_tombstones (appid, request_id, kind, digest, hint, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)`, appID, requestID, item.Kind, item.Digest, truncateRunes(item.Hint, 128), item.ExpiresAt)
	}
	return r.pool.SendBatch(ctx, batch).Close()
}

// FindAccountDeletionTombstone 未到期的墓碑里有没有这些摘要之一；有则返回到期最晚的那条。
func (r *Repository) FindAccountDeletionTombstone(ctx context.Context, appID int64, digests []string) (*accountdeletion.Tombstone, error) {
	if len(digests) == 0 {
		return nil, nil
	}
	var item accountdeletion.Tombstone
	err := r.pool.QueryRow(ctx, `SELECT id, appid, request_id, kind, hint, expires_at, created_at
FROM account_deletion_tombstones WHERE appid = $1 AND digest = ANY($2) AND expires_at > NOW()
ORDER BY expires_at DESC LIMIT 1`, appID, digests).Scan(
		&item.ID, &item.AppID, &item.RequestID, &item.Kind, &item.Hint, &item.ExpiresAt, &item.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// ListAccountDeletionTombstones 未到期的墓碑。
func (r *Repository) ListAccountDeletionTombstones(ctx context.Context, appID int64, keyword string, page, limit int) (*accountdeletion.TombstonePage, error) {
	where := "t.appid = $1 AND t.expires_at > NOW()"
	args := []any{appID}
	if keyword = strings.TrimSpace(keyword); keyword != "" {
		args = append(args, "%"+escapeLikePattern(keyword)+"%")
		where += " AND (t.hint ILIKE $2 OR COALESCE(r.account, '') ILIKE $2)"
	}
	result := &accountdeletion.TombstonePage{Items: []accountdeletion.Tombstone{}, Page: page, Limit: limit}
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM account_deletion_tombstones t
LEFT JOIN account_deletion_requests r ON r.id = t.request_id WHERE `+where, args...).Scan(&result.Total); err != nil {
		return nil, err
	}
	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, `SELECT t.id, t.appid, t.request_id, t.kind, t.hint, t.expires_at, t.created_at, COALESCE(r.account, '')
FROM account_deletion_tombstones t LEFT JOIN account_deletion_requests r ON r.id = t.request_id
WHERE `+where+fmt.Sprintf(` ORDER BY t.id DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item accountdeletion.Tombstone
		if err := rows.Scan(&item.ID, &item.AppID, &item.RequestID, &item.Kind, &item.Hint, &item.ExpiresAt, &item.CreatedAt, &item.Account); err != nil {
			return nil, err
		}
		result.Items = append(result.Items, item)
	}
	return result, rows.Err()
}

// DeleteAccountDeletionTombstone 解除一个墓碑（放开重新注册）。
func (r *Repository) DeleteAccountDeletionTombstone(ctx context.Context, appID, id int64) (bool, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM account_deletion_tombstones WHERE id = $1 AND appid = $2`, id, appID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ── 统计 ──

// AccountDeletionStats 管理端统计。since 为趋势起点，timezone 为分日所用的时区名（调用方已校验）。
func (r *Repository) AccountDeletionStats(ctx context.Context, appID int64, since time.Time, timezone string) (*accountdeletion.Stats, error) {
	stats := &accountdeletion.Stats{
		ByStatus: []accountdeletion.CountItem{}, ByMode: []accountdeletion.CountItem{}, BySource: []accountdeletion.CountItem{},
		Reasons: []accountdeletion.CountItem{}, Trend: []accountdeletion.TrendPoint{}, TimeZone: timezone,
	}
	var closed, retained int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*),
  COUNT(*) FILTER (WHERE status = 'scheduled' AND scheduled_at <= NOW() + INTERVAL '7 days'),
  COUNT(*) FILTER (WHERE status IN ('completed','cancelled','rejected')),
  COUNT(*) FILTER (WHERE status IN ('cancelled','rejected')),
  (SELECT COUNT(*) FROM account_deletion_tombstones t WHERE t.appid = $1 AND t.expires_at > NOW()),
  COUNT(*) FILTER (WHERE status = 'reviewing'),
  COUNT(*) FILTER (WHERE status = 'scheduled'),
  COUNT(*) FILTER (WHERE status = 'processing'),
  COUNT(*) FILTER (WHERE status = 'failed'),
  COUNT(*) FILTER (WHERE created_at >= $2)
FROM account_deletion_requests WHERE appid = $1`, appID, since).Scan(&stats.Total, &stats.DueIn7Days, &closed, &retained, &stats.Tombstones,
		&stats.Open.Reviewing, &stats.Open.Scheduled, &stats.Open.Processing, &stats.Open.Failed, &stats.WindowTotal); err != nil {
		return nil, err
	}
	if closed > 0 {
		stats.RetentionRate = float64(retained) / float64(closed)
	}
	group := func(column string) ([]accountdeletion.CountItem, error) {
		rows, err := r.pool.Query(ctx, `SELECT `+column+`, COUNT(*) FROM account_deletion_requests
WHERE appid = $1 AND created_at >= $2 GROUP BY 1 ORDER BY 2 DESC`, appID, since)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		items := []accountdeletion.CountItem{}
		for rows.Next() {
			var item accountdeletion.CountItem
			if err := rows.Scan(&item.Key, &item.Count); err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		return items, rows.Err()
	}
	var err error
	if stats.ByStatus, err = group("status"); err != nil {
		return nil, err
	}
	if stats.ByMode, err = group("mode"); err != nil {
		return nil, err
	}
	if stats.BySource, err = group("source"); err != nil {
		return nil, err
	}
	if stats.Reasons, err = group("CASE WHEN reason_code <> '' THEN reason_code WHEN source = 'admin' THEN '" + accountdeletion.AdminReasonKey + "' ELSE '' END"); err != nil {
		return nil, err
	}

	rows, err := r.pool.Query(ctx, `WITH events AS (
  SELECT created_at AS at, 'requested' AS kind FROM account_deletion_requests WHERE appid = $1 AND created_at >= $2
  UNION ALL
  SELECT completed_at, 'completed' FROM account_deletion_requests WHERE appid = $1 AND completed_at >= $2
  UNION ALL
  SELECT cancelled_at, 'cancelled' FROM account_deletion_requests WHERE appid = $1 AND cancelled_at >= $2
)
SELECT to_char(at AT TIME ZONE $3, 'YYYY-MM-DD') AS day,
       COUNT(*) FILTER (WHERE kind = 'requested'),
       COUNT(*) FILTER (WHERE kind = 'completed'),
       COUNT(*) FILTER (WHERE kind = 'cancelled')
FROM events GROUP BY day ORDER BY day`, appID, since, timezone)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var point accountdeletion.TrendPoint
		if err := rows.Scan(&point.Date, &point.Requested, &point.Completed, &point.Cancelled); err != nil {
			return nil, err
		}
		stats.Trend = append(stats.Trend, point)
	}
	return stats, rows.Err()
}

func nonNilStrings(items []string) []string {
	if items == nil {
		return []string{}
	}
	return items
}

// truncateRunes 按字符截断，避免超出 VARCHAR 长度或截断半个汉字。
func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}
