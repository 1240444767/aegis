package postgres

import (
	"context"
	"strings"
	"time"

	userdomain "aegis/internal/domain/user"

	"github.com/jackc/pgx/v5"
)

// 账号初始化（docs/account-setup.md）。表结构见迁移 000139。

const accountSetupColumns = `user_id, appid, source, provider, account_pending, account_set_at,
    prompted_at, snoozed_until, completed_at, created_at, updated_at`

func scanAccountSetup(row pgx.Row) (*userdomain.AccountSetupRecord, error) {
	var record userdomain.AccountSetupRecord
	if err := row.Scan(&record.UserID, &record.AppID, &record.Source, &record.Provider, &record.AccountPending,
		&record.AccountSetAt, &record.PromptedAt, &record.SnoozedUntil, &record.CompletedAt,
		&record.CreatedAt, &record.UpdatedAt); err != nil {
		return nil, normalizeNotFound(err)
	}
	return &record, nil
}

// TrackAccountSetup 快速登录建号后登记一行。已有就不动（重复登记不会把完成的号改回待设置）。
func (r *Repository) TrackAccountSetup(ctx context.Context, appID, userID int64, source, provider string) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO user_account_setup (user_id, appid, source, provider)
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id) DO NOTHING`, userID, appID, source, strings.TrimSpace(provider))
	return err
}

// GetAccountSetup 读一行；没有登记过返回 nil。
func (r *Repository) GetAccountSetup(ctx context.Context, userID int64) (*userdomain.AccountSetupRecord, error) {
	return scanAccountSetup(r.pool.QueryRow(ctx,
		`SELECT `+accountSetupColumns+` FROM user_account_setup WHERE user_id = $1`, userID))
}

// MarkAccountSetupPrompted 记下首次弹窗已展示。只记第一次。
func (r *Repository) MarkAccountSetupPrompted(ctx context.Context, userID int64, at time.Time) error {
	_, err := r.pool.Exec(ctx, `UPDATE user_account_setup
SET prompted_at = COALESCE(prompted_at, $2), updated_at = NOW()
WHERE user_id = $1`, userID, at.UTC())
	return err
}

// SnoozeAccountSetup 用户关掉横幅：在 until 之前不再提醒。同时视作已提醒过（不会再弹窗）。
func (r *Repository) SnoozeAccountSetup(ctx context.Context, userID int64, now, until time.Time) error {
	_, err := r.pool.Exec(ctx, `UPDATE user_account_setup
SET snoozed_until = $3, prompted_at = COALESCE(prompted_at, $2), updated_at = NOW()
WHERE user_id = $1`, userID, now.UTC(), until.UTC())
	return err
}

// SetAccountSetupCompleted 写入（或清掉）完成时刻。
func (r *Repository) SetAccountSetupCompleted(ctx context.Context, userID int64, completedAt *time.Time) error {
	_, err := r.pool.Exec(ctx, `UPDATE user_account_setup
SET completed_at = $2, updated_at = NOW()
WHERE user_id = $1`, userID, nullableTimePtr(completedAt))
	return err
}

// ResetAccountSetup 管理员要求重新初始化：用户名重新算作待设置、清掉提醒记录。
// 没登记过的账号补登一行（来源记为管理员给的 source）。
func (r *Repository) ResetAccountSetup(ctx context.Context, appID, userID int64, source string, accountPending bool) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO user_account_setup (user_id, appid, source, account_pending)
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id) DO UPDATE SET
    account_pending = EXCLUDED.account_pending,
    account_set_at = CASE WHEN EXCLUDED.account_pending THEN NULL ELSE user_account_setup.account_set_at END,
    prompted_at = NULL, snoozed_until = NULL, completed_at = NULL, updated_at = NOW()`,
		userID, appID, source, accountPending)
	return err
}

// UsernameTaken 同一应用里是否已有同名账号（忽略大小写，exceptUserID 是自己）。
func (r *Repository) UsernameTaken(ctx context.Context, appID int64, account string, exceptUserID int64) (bool, error) {
	var taken bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(
    SELECT 1 FROM users WHERE appid = $1 AND LOWER(account) = LOWER($2) AND id <> $3)`,
		appID, account, exceptUserID).Scan(&taken)
	return taken, err
}

// RenameUserAccount 改账号名，并把初始化记录里的用户名标为已设置。
//
// 撞名靠唯一约束兜底（并发改成同一个名字时只有一个成功），返回 ErrAccountAlreadyExists。
func (r *Repository) RenameUserAccount(ctx context.Context, appID, userID int64, account string) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `UPDATE users SET account = $3, updated_at = NOW() WHERE id = $1 AND appid = $2`,
		userID, appID, account)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrAccountAlreadyExists
		}
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	if _, err := tx.Exec(ctx, `UPDATE user_account_setup
SET account_pending = FALSE, account_set_at = COALESCE(account_set_at, NOW()), updated_at = NOW()
WHERE user_id = $1`, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// AccountSetupStats 应用的初始化统计。pending 按「用户名待设或没有密码」算，与策略要求无关，
// 控制台据此看清有多少号还停在系统生成的状态。
func (r *Repository) AccountSetupStats(ctx context.Context, appID int64) (*userdomain.AccountSetupStats, error) {
	stats := &userdomain.AccountSetupStats{BySource: map[string]int64{}}
	rows, err := r.pool.Query(ctx, `SELECT s.source,
    COUNT(*),
    COUNT(*) FILTER (WHERE s.completed_at IS NOT NULL),
    COUNT(*) FILTER (WHERE s.account_pending OR COALESCE(u.password_hash, '') = ''),
    COUNT(*) FILTER (WHERE s.account_pending),
    COUNT(*) FILTER (WHERE s.prompted_at IS NOT NULL)
FROM user_account_setup s
JOIN users u ON u.id = s.user_id
WHERE s.appid = $1
GROUP BY s.source`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var source string
		var tracked, completed, pending, accountPending, prompted int64
		if err := rows.Scan(&source, &tracked, &completed, &pending, &accountPending, &prompted); err != nil {
			return nil, err
		}
		stats.BySource[source] = tracked
		stats.Tracked += tracked
		stats.Completed += completed
		stats.Pending += pending
		stats.AccountPending += accountPending
		stats.Prompted += prompted
	}
	return stats, rows.Err()
}
