package postgres

import (
	"context"
	"strconv"
	"strings"

	userdomain "aegis/internal/domain/user"
)

// ListAppDevices 应用的设备目录：按设备 ID 聚合登录记录，取每台设备最近一次登录的元数据与使用者。
//
// 登录记录是唯一完整记下「哪台设备、什么型号、谁在用」的地方：会话会过期，
// 设备 ID 却会被发布中心的内测、排除名单长期引用。只看近 365 天，太旧的设备不再出现在搜索里，
// 但按 ID 批量取时不受这个限制 —— 名单里存着的设备总要能认出来。
func (r *Repository) ListAppDevices(ctx context.Context, appID int64, query userdomain.AdminDeviceQuery) ([]userdomain.AdminDeviceItem, error) {
	limit := query.Limit
	if limit <= 0 || limit > 200 {
		limit = 30
	}
	args := []any{appID}
	where := []string{"l.appid = $1", "l.device_id IS NOT NULL", "l.device_id <> ''"}
	if len(query.IDs) > 0 {
		ids := query.IDs
		if len(ids) > 200 {
			ids = ids[:200]
		}
		args = append(args, ids)
		where = append(where, "l.device_id = ANY($2)")
		limit = len(ids)
	} else {
		where = append(where, "l.created_at > NOW() - INTERVAL '365 days'")
		if query.UserID > 0 {
			args = append(args, query.UserID)
			where = append(where, "l.user_id = $"+strconv.Itoa(len(args)))
		}
		if kw := strings.TrimSpace(query.Keyword); kw != "" {
			args = append(args, "%"+likeEscaper.Replace(kw)+"%")
			p := "$" + strconv.Itoa(len(args))
			where = append(where, "(l.device_id ILIKE "+p+" ESCAPE '\\' OR l.metadata->>'device_model' ILIKE "+p+" ESCAPE '\\' OR l.metadata->>'device' ILIKE "+p+" ESCAPE '\\'"+
				" OR l.user_id IN (SELECT u.id FROM users u LEFT JOIN user_profiles up ON up.user_id = u.id WHERE u.appid = $1 AND (u.account ILIKE "+p+" ESCAPE '\\' OR COALESCE(up.nickname, '') ILIKE "+p+" ESCAPE '\\') LIMIT 200))")
		}
	}
	args = append(args, limit)
	sql := `WITH d AS (
    SELECT l.device_id, MIN(l.created_at) AS first_seen, MAX(l.created_at) AS last_seen,
           COUNT(*) AS logins, COUNT(DISTINCT l.user_id) AS users
    FROM login_audit_logs l
    WHERE ` + strings.Join(where, " AND ") + `
    GROUP BY l.device_id
    ORDER BY MAX(l.created_at) DESC
    LIMIT $` + strconv.Itoa(len(args)) + `
)
SELECT d.device_id, d.first_seen, d.last_seen, d.logins, d.users,
       COALESCE(latest.metadata, '{}'::jsonb), COALESCE(latest.user_agent, ''), COALESCE(host(latest.login_ip), ''),
       COALESCE(latest.user_id, 0), COALESCE(u.account, ''), COALESCE(up.nickname, '')
FROM d
JOIN LATERAL (
    SELECT l.metadata, l.user_agent, l.login_ip, l.user_id
    FROM login_audit_logs l
    WHERE l.appid = $1 AND l.device_id = d.device_id
    ORDER BY l.created_at DESC
    LIMIT 1
) latest ON TRUE
LEFT JOIN users u ON u.id = latest.user_id
LEFT JOIN user_profiles up ON up.user_id = latest.user_id
ORDER BY d.last_seen DESC`
	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]userdomain.AdminDeviceItem, 0, limit)
	for rows.Next() {
		var item userdomain.AdminDeviceItem
		var userID int64
		var account, nickname string
		if err := rows.Scan(&item.DeviceID, &item.FirstSeenAt, &item.LastSeenAt, &item.Logins, &item.Users,
			&item.Metadata, &item.UserAgent, &item.LastIP, &userID, &account, &nickname); err != nil {
			return nil, err
		}
		if userID > 0 {
			item.LastUser = &userdomain.AdminDeviceUser{ID: userID, Account: account, Nickname: nickname}
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
