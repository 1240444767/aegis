package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// 对外用户 ID（users.public_id）与主键之间的换算。号由数据库发放且不可改，规则见 docs/user-public-id.md。

// GetUserPublicIDs 按主键批量取对外 ID；查不到的主键（已删除）不出现在结果里。
func (r *Repository) GetUserPublicIDs(ctx context.Context, userIDs []int64) (map[int64]int64, error) {
	out := make(map[int64]int64, len(userIDs))
	if len(userIDs) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT id, public_id FROM users WHERE id = ANY($1)`, userIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, publicID int64
		if err := rows.Scan(&id, &publicID); err != nil {
			return nil, err
		}
		out[id] = publicID
	}
	return out, rows.Err()
}

// FindUserIDByPublicID 在应用内按对外 ID 找主键；找不到返回 0。
// 限定 appid：对外 ID 全局唯一，但一个应用只该摸得到自己的用户。
func (r *Repository) FindUserIDByPublicID(ctx context.Context, appID, publicID int64) (int64, error) {
	var id int64
	err := r.pool.QueryRow(ctx, `SELECT id FROM users WHERE appid = $1 AND public_id = $2`, appID, publicID).Scan(&id)
	if err == pgx.ErrNoRows {
		return 0, nil
	}
	return id, err
}
