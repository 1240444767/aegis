package postgres

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"aegis/internal/domain/featuretime"
)

// 功能时长的账目：余额、流水与租约（见 internal/domain/featuretime）。
//
// 所有改余额的路径都先锁住余额行（发放用 INSERT … ON CONFLICT DO UPDATE，本身就是行锁），
// 判余额与扣减在同一把锁里，两个并发的租约不会都读到「还剩 300 秒」然后各扣 300。
// 结算先锁租约行、再动余额行；租出只锁余额行、新建的租约行别人还看不见 —— 两条路径不会互相等死。

var (
	// ErrFeatureTimeExhausted 余额为 0，租不出来。
	ErrFeatureTimeExhausted = errors.New("feature time exhausted")
	// ErrFeatureTimeLeaseNotFound 租约不存在或不属于当前用户（两者不区分，免得能拿租约号探测别人的记录）。
	ErrFeatureTimeLeaseNotFound = errors.New("feature time lease not found")
)

// GetFeatureTimeAccount 一个用户在一个功能上的余额。没有这一行时返回全零的账户。
func (r *Repository) GetFeatureTimeAccount(ctx context.Context, appID, userID int64, feature string) (*featuretime.Account, error) {
	account := &featuretime.Account{AppID: appID, UserID: userID, Feature: feature}
	err := r.pool.QueryRow(ctx, `SELECT balance_seconds, earned_seconds, used_seconds, updated_at
FROM feature_time_accounts WHERE appid = $1 AND user_id = $2 AND feature = $3`, appID, userID, feature).
		Scan(&account.BalanceSeconds, &account.EarnedSeconds, &account.UsedSeconds, &account.UpdatedAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	return account, nil
}

// grantFeatureTimeTx 在调用方的事务里发放功能时长，返回发放后的余额。
//
// 与抽奖次数同一种写法：INSERT … ON CONFLICT DO UPDATE 一条语句完成「没有就建、有就加」，
// 并在冲突时拿到行锁，与同一用户的租约串行。
func grantFeatureTimeTx(ctx context.Context, tx pgx.Tx, appID, userID int64, feature string, seconds int64,
	refType, refID, remark string) (int64, error) {
	var balance int64
	if err := tx.QueryRow(ctx, `INSERT INTO feature_time_accounts (appid, user_id, feature, balance_seconds, earned_seconds)
VALUES ($1, $2, $3, $4::bigint, $4::bigint)
ON CONFLICT (appid, user_id, feature) DO UPDATE
SET balance_seconds = feature_time_accounts.balance_seconds + EXCLUDED.balance_seconds,
    earned_seconds = feature_time_accounts.earned_seconds + EXCLUDED.earned_seconds,
    updated_at = NOW()
RETURNING balance_seconds`, appID, userID, feature, seconds).Scan(&balance); err != nil {
		return 0, err
	}
	if err := insertFeatureTimeLedgerTx(ctx, tx, appID, userID, feature, seconds, balance,
		featuretime.KindEarn, refType, refID, remark); err != nil {
		return 0, err
	}
	return balance, nil
}

func insertFeatureTimeLedgerTx(ctx context.Context, tx pgx.Tx, appID, userID int64, feature string,
	delta, balanceAfter int64, kind, refType, refID, remark string) error {
	_, err := tx.Exec(ctx, `INSERT INTO feature_time_ledger
(appid, user_id, feature, delta_seconds, balance_after, kind, ref_type, ref_id, remark)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		appID, userID, feature, delta, balanceAfter, kind, refType, truncateColumn(refID, 64), truncateColumn(remark, 256))
	return err
}

// LeaseFeatureTime 租一段时长：锁余额行 → 余额为 0 报 ErrFeatureTimeExhausted →
// 扣 min(要的, 余额) → 建租约 → 记流水。单事务。
func (r *Repository) LeaseFeatureTime(ctx context.Context, input featuretime.LeaseInput) (*featuretime.Lease, error) {
	now := input.Now
	if now.IsZero() {
		now = time.Now()
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	var balance int64
	if err := tx.QueryRow(ctx, `SELECT balance_seconds FROM feature_time_accounts
WHERE appid = $1 AND user_id = $2 AND feature = $3 FOR UPDATE`, input.AppID, input.UserID, input.Feature).
		Scan(&balance); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrFeatureTimeExhausted
		}
		return nil, err
	}
	if balance <= 0 {
		return nil, ErrFeatureTimeExhausted
	}
	seconds := int(min(int64(input.Seconds), balance))
	balance -= int64(seconds)
	if _, err := tx.Exec(ctx, `UPDATE feature_time_accounts
SET balance_seconds = $4, used_seconds = used_seconds + $5, updated_at = NOW()
WHERE appid = $1 AND user_id = $2 AND feature = $3`,
		input.AppID, input.UserID, input.Feature, balance, seconds); err != nil {
		return nil, err
	}

	leaseNo := generateTransactionNo(featuretime.LeaseNoPrefix)
	expiresAt := now.Add(featuretime.LeaseTTL).UTC().Truncate(time.Second)
	if _, err := tx.Exec(ctx, `INSERT INTO feature_time_leases (lease_no, appid, user_id, feature, seconds, expires_at, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		leaseNo, input.AppID, input.UserID, input.Feature, seconds, expiresAt, now.UTC()); err != nil {
		return nil, err
	}
	if err := insertFeatureTimeLedgerTx(ctx, tx, input.AppID, input.UserID, input.Feature, -int64(seconds), balance,
		featuretime.KindLease, featuretime.RefLease, leaseNo, ""); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return &featuretime.Lease{
		LeaseNo:        leaseNo,
		Seconds:        seconds,
		BalanceSeconds: balance,
		ExpiresAt:      expiresAt.Format(time.RFC3339),
	}, nil
}

// SettleFeatureTimeLease 结算一个租约：锁租约行 → 已结算的原样返回 → 按 featuretime.Settle 定结论 →
// 有退款时锁余额行退回并记流水 → 回写租约。单事务。
//
// 租约号是别人的、或根本不存在，一律 ErrFeatureTimeLeaseNotFound。
func (r *Repository) SettleFeatureTimeLease(ctx context.Context, input featuretime.SettleInput) (*featuretime.Settlement, error) {
	now := input.Now
	if now.IsZero() {
		now = time.Now()
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	lease := featuretime.LeaseRecord{}
	if err := tx.QueryRow(ctx, `SELECT id, lease_no, appid, user_id, feature, seconds, used_seconds, refunded_seconds,
  status, expires_at, created_at, settled_at
FROM feature_time_leases WHERE appid = $1 AND lease_no = $2 FOR UPDATE`, input.AppID, input.LeaseNo).Scan(
		&lease.ID, &lease.LeaseNo, &lease.AppID, &lease.UserID, &lease.Feature, &lease.Seconds, &lease.UsedSeconds,
		&lease.RefundedSeconds, &lease.Status, &lease.ExpiresAt, &lease.CreatedAt, &lease.SettledAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrFeatureTimeLeaseNotFound
		}
		return nil, err
	}
	if lease.UserID != input.UserID {
		return nil, ErrFeatureTimeLeaseNotFound
	}

	// 结算过的：原样返回第一次的结论（余额取当前值），网络重试不会退两次。
	if lease.Status != featuretime.LeaseOpen {
		balance, err := featureTimeBalanceTx(ctx, tx, lease.AppID, lease.UserID, lease.Feature)
		if err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		tx = nil
		return settlementOf(lease, balance), nil
	}

	status, used, refund := featuretime.Settle(lease, input.UsedSeconds, now)
	var balance int64
	if refund > 0 {
		if err := tx.QueryRow(ctx, `UPDATE feature_time_accounts
SET balance_seconds = balance_seconds + $4, used_seconds = GREATEST(used_seconds - $4, 0), updated_at = NOW()
WHERE appid = $1 AND user_id = $2 AND feature = $3
RETURNING balance_seconds`, lease.AppID, lease.UserID, lease.Feature, refund).Scan(&balance); err != nil {
			return nil, err
		}
		if err := insertFeatureTimeLedgerTx(ctx, tx, lease.AppID, lease.UserID, lease.Feature, int64(refund), balance,
			featuretime.KindRefund, featuretime.RefLease, lease.LeaseNo, "已用 "+strconv.Itoa(used)+" 秒"); err != nil {
			return nil, err
		}
	} else {
		if balance, err = featureTimeBalanceTx(ctx, tx, lease.AppID, lease.UserID, lease.Feature); err != nil {
			return nil, err
		}
	}

	settledAt := now.UTC()
	if _, err := tx.Exec(ctx, `UPDATE feature_time_leases
SET status = $2, used_seconds = $3, refunded_seconds = $4, settled_at = $5 WHERE id = $1`,
		lease.ID, status, used, refund, settledAt); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil

	lease.Status = status
	lease.UsedSeconds = used
	lease.RefundedSeconds = refund
	lease.SettledAt = &settledAt
	return settlementOf(lease, balance), nil
}

func featureTimeBalanceTx(ctx context.Context, tx pgx.Tx, appID, userID int64, feature string) (int64, error) {
	var balance int64
	err := tx.QueryRow(ctx, `SELECT balance_seconds FROM feature_time_accounts
WHERE appid = $1 AND user_id = $2 AND feature = $3`, appID, userID, feature).Scan(&balance)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return balance, err
}

func settlementOf(lease featuretime.LeaseRecord, balance int64) *featuretime.Settlement {
	return &featuretime.Settlement{
		LeaseNo:         lease.LeaseNo,
		Feature:         lease.Feature,
		Status:          lease.Status,
		Seconds:         lease.Seconds,
		UsedSeconds:     lease.UsedSeconds,
		RefundedSeconds: lease.RefundedSeconds,
		BalanceSeconds:  balance,
	}
}

// featureNameTx 功能目录里的展示名；目录里没有时返回标识本身。
func featureNameTx(ctx context.Context, tx pgx.Tx, appID int64, feature string) (string, error) {
	var name string
	err := tx.QueryRow(ctx, `SELECT name FROM vip_features WHERE appid = $1 AND tag = $2 LIMIT 1`, appID, feature).Scan(&name)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && name == "") {
		return feature, nil
	}
	return name, err
}
