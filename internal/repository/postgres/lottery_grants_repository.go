package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// 抽奖次数余额。
//
// 应用级的抽奖次数余额（抽奖的 bonus 来源）：看激励广告、卡密、积分商城、邀请、排行榜与抽奖本身
// 发的「抽奖次数」都进这里，任意活动可用。扣减在抽奖事务里（DrawLottery），与发奖同成同败。

// grantLotteryDrawsTx 事务内发放抽奖次数（供卡密核销在同一事务里调用）。
//
// 用 UPSERT 而不是「先查再插」：后者在同一用户并发领两张卡时会有一个撞唯一约束，
// 而那次失败会把整个核销事务连带回滚 —— 用户看到的是「兑换失败」，卡却已经作废。
//
// $3 显式转成 bigint：它同时落进 INTEGER 的 balance 与 BIGINT 的 total_granted，
// 不写类型时 Postgres 会对同一个参数推出两种类型，整条语句报 42P08。
func grantLotteryDrawsTx(ctx context.Context, tx pgx.Tx, appID int64, userID int64, amount int64) (int64, error) {
	var balance int64
	err := tx.QueryRow(ctx, `INSERT INTO lottery_draw_grants (appid, user_id, balance, total_granted)
VALUES ($1, $2, $3::bigint, $3::bigint)
ON CONFLICT (appid, user_id) DO UPDATE
SET balance = lottery_draw_grants.balance + EXCLUDED.balance,
    total_granted = lottery_draw_grants.total_granted + EXCLUDED.total_granted,
    updated_at = NOW()
RETURNING balance`, appID, userID, amount).Scan(&balance)
	return balance, err
}

// GetLotteryDrawBalance 查询剩余赠送次数；没有账户时返回 0。
func (r *Repository) GetLotteryDrawBalance(ctx context.Context, appID int64, userID int64) (int64, error) {
	var balance int64
	err := r.pool.QueryRow(ctx,
		`SELECT balance FROM lottery_draw_grants WHERE appid = $1 AND user_id = $2`,
		appID, userID).Scan(&balance)
	if err == pgx.ErrNoRows {
		return 0, nil
	}
	return balance, err
}
