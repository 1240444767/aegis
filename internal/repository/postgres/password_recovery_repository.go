package postgres

import (
	"context"
	"strings"
)

// ListUserIDsByProfileEmail 按档案邮箱找账号，最多 limit 个。
//
// 与 FindUserIDByProfileEmail 的区别在于不止取一个：找回密码凭邮箱认人时，
// 一个邮箱挂在多个账号上（管理员导入、历史数据）就不能替用户挑一个。
func (r *Repository) ListUserIDsByProfileEmail(ctx context.Context, appID int64, email string, limit int) ([]int64, error) {
	return r.listUserIDs(ctx, `SELECT u.id
FROM users u
JOIN user_profiles p ON p.user_id = u.id
WHERE u.appid = $1 AND LOWER(COALESCE(p.email, '')) = LOWER($2)
ORDER BY u.id
LIMIT $3`, appID, strings.TrimSpace(email), limit)
}

// ListUserIDsByProfilePhone 按档案手机号找账号，最多 limit 个。
func (r *Repository) ListUserIDsByProfilePhone(ctx context.Context, appID int64, phone string, limit int) ([]int64, error) {
	return r.listUserIDs(ctx, `SELECT u.id
FROM users u
JOIN user_profiles p ON p.user_id = u.id
WHERE u.appid = $1 AND COALESCE(p.phone, '') = $2
ORDER BY u.id
LIMIT $3`, appID, strings.TrimSpace(phone), limit)
}

func (r *Repository) listUserIDs(ctx context.Context, query string, appID int64, value string, limit int) ([]int64, error) {
	if value == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 1
	}
	rows, err := r.pool.Query(ctx, query, appID, value, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]int64, 0, limit)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
