package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	vipdomain "aegis/internal/domain/vip"
)

// 功能解锁账本（user_feature_unlocks）：与会员段并列的第三条线，规则见 internal/domain/vip/unlock.go。

var (
	ErrFeatureUnlockNotFound = errors.New("feature unlock not found")
	// ErrFeatureUnlockOwned 已永久解锁，再发一段没有意义
	ErrFeatureUnlockOwned = errors.New("feature already permanently unlocked")
)

// vipFeatureUnlocksSQL 某用户尚未结束、未被收回的功能解锁，聚成 JSON 数组。
//
// 与会员段一样并进判定事实的那一条查询里（见 GetVipEntitlementFacts）：
// 分开查就有「中间刚好兑换了」的窗口，拼出来的 features 会与 unlocks 对不上。
// FROM 前面留着缩进：TestVipEntitlementFactsNullableColumnsAreNullSafe 以行首的 FROM 判断顶层。
const vipFeatureUnlocksSQL = `COALESCE((
    SELECT jsonb_agg(jsonb_build_object(
        'id', fu.id,
        'tag', fu.feature_tag,
        'source', fu.source,
        'sourceRef', COALESCE(fu.source_ref, ''),
        'durationDays', fu.duration_days,
        'startsAt', fu.starts_at,
        'endsAt', fu.ends_at,
        'createdAt', fu.created_at
    ) ORDER BY fu.id)
    FROM user_feature_unlocks fu
    WHERE fu.appid = u.appid AND fu.user_id = u.id AND fu.revoked_at IS NULL
      AND (fu.ends_at IS NULL OR fu.ends_at > NOW())
), '[]'::jsonb)`

func decodeFeatureUnlocks(raw []byte) ([]vipdomain.FeatureUnlock, error) {
	if len(raw) == 0 {
		return []vipdomain.FeatureUnlock{}, nil
	}
	unlocks := make([]vipdomain.FeatureUnlock, 0, 2)
	if err := json.Unmarshal(raw, &unlocks); err != nil {
		return nil, fmt.Errorf("decode feature unlocks: %w", err)
	}
	return unlocks, nil
}

const featureUnlockColumns = `id, appid, user_id, feature_tag, source, COALESCE(source_ref, ''), duration_days,
starts_at, ends_at, revoked_at, COALESCE(revoked_by, ''), COALESCE(revoke_reason, ''), COALESCE(operator, ''), created_at`

func scanFeatureUnlock(row interface{ Scan(dest ...any) error }) (*vipdomain.FeatureUnlock, error) {
	var item vipdomain.FeatureUnlock
	if err := row.Scan(&item.ID, &item.AppID, &item.UserID, &item.Tag, &item.Source, &item.SourceRef,
		&item.DurationDays, &item.StartsAt, &item.EndsAt, &item.RevokedAt, &item.RevokedBy,
		&item.RevokeReason, &item.Operator, &item.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrFeatureUnlockNotFound
		}
		return nil, err
	}
	return &item, nil
}

// FeatureUnlockGrant 一次功能解锁。
type FeatureUnlockGrant struct {
	AppID     int64
	UserID    int64
	Tag       string
	Days      int // 0 为永久
	Source    string
	SourceRef string
	Operator  string
}

// grantFeatureUnlockTx 事务内发放一次功能解锁。
//
// 限时解锁顺延：新的一段从「这个功能仍生效的限时解锁里最晚的终点」与现在两者较晚的那个开始，
// 与会员到期时间的顺延同一口径 —— 连兑两次七天就是十四天，而不是两段重叠的七天。
// 已永久解锁时拒发（ErrFeatureUnlockOwned）：积分商城在扣积分之前就判过「已拥有」，
// 走到这里说明是管理员授予或并发，前者该报错、后者该回滚。
//
// 锁用户行：调用方（兑换事务）通常已经锁过；管理员授予没有，这一句让两条路串行化，
// 否则同一功能的两次并发顺延会读到同一个终点，排成两段重叠的期间。
func grantFeatureUnlockTx(ctx context.Context, tx pgx.Tx, grant FeatureUnlockGrant) (*vipdomain.FeatureUnlock, error) {
	tags := vipdomain.NormalizeFeatureTags([]string{grant.Tag})
	if len(tags) == 0 {
		return nil, fmt.Errorf("feature unlock: empty tag")
	}
	tag := tags[0]
	var locked int64
	if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`,
		grant.UserID, grant.AppID).Scan(&locked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}

	var permanent bool
	var latestEnd *time.Time
	if err := tx.QueryRow(ctx, `SELECT
    COALESCE(BOOL_OR(ends_at IS NULL), FALSE),
    MAX(ends_at)
FROM user_feature_unlocks
WHERE appid = $1 AND user_id = $2 AND feature_tag = $3 AND revoked_at IS NULL
  AND (ends_at IS NULL OR ends_at > NOW())`, grant.AppID, grant.UserID, tag).Scan(&permanent, &latestEnd); err != nil {
		return nil, err
	}
	if permanent {
		return nil, ErrFeatureUnlockOwned
	}

	now := time.Now().UTC()
	start := now
	if latestEnd != nil && latestEnd.After(now) {
		start = latestEnd.UTC()
	}
	var end *time.Time
	if grant.Days > 0 {
		value := start.AddDate(0, 0, grant.Days)
		end = &value
	} else {
		// 永久解锁从现在起算：它没有终点，排在限时那几段后面没有意义
		start = now
	}

	return scanFeatureUnlock(tx.QueryRow(ctx, `INSERT INTO user_feature_unlocks
(appid, user_id, feature_tag, source, source_ref, duration_days, starts_at, ends_at, operator)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
RETURNING `+featureUnlockColumns,
		grant.AppID, grant.UserID, tag, grant.Source, nullableString(grant.SourceRef), grant.Days,
		start, end, nullableString(grant.Operator)))
}

// GrantFeatureUnlock 管理员授予功能解锁（单独一个事务）。
func (r *Repository) GrantFeatureUnlock(ctx context.Context, grant FeatureUnlockGrant) (*vipdomain.FeatureUnlock, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	unlock, err := grantFeatureUnlockTx(ctx, tx, grant)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return unlock, nil
}

// ListUserFeatureUnlocks 一个用户的全部解锁记录（含已结束、已收回），新的在前。
func (r *Repository) ListUserFeatureUnlocks(ctx context.Context, appID, userID int64) ([]vipdomain.FeatureUnlock, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+featureUnlockColumns+`
FROM user_feature_unlocks WHERE appid = $1 AND user_id = $2 ORDER BY id DESC LIMIT 200`, appID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]vipdomain.FeatureUnlock, 0, 4)
	for rows.Next() {
		item, err := scanFeatureUnlock(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

// RevokeFeatureUnlock 收回一条解锁（只标记，不删除：兑换记录里还指着它）。
//
// 只收回这一条，排在它后面的同一功能的限时解锁**不前移**：前移会让用户看到的到期时间
// 凭空提前，而收回通常是在处理一笔有问题的兑换，不该连带影响其它正常的兑换。
func (r *Repository) RevokeFeatureUnlock(ctx context.Context, appID, userID, unlockID int64, operator, reason string) (*vipdomain.FeatureUnlock, error) {
	item, err := scanFeatureUnlock(r.pool.QueryRow(ctx, `UPDATE user_feature_unlocks
SET revoked_at = NOW(), revoked_by = $4, revoke_reason = $5
WHERE id = $1 AND appid = $2 AND user_id = $3 AND revoked_at IS NULL
RETURNING `+featureUnlockColumns, unlockID, appID, userID,
		nullableString(operator), nullableString(truncateColumn(strings.TrimSpace(reason), 256))))
	if err != nil {
		return nil, err
	}
	return item, nil
}
