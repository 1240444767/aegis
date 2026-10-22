package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	devicedomain "aegis/internal/domain/device"
	"aegis/internal/domain/devicelimit"

	"github.com/jackc/pgx/v5"
)

// 登录设备数控制的数据访问：单人例外、设备台账与处置记录。规则见 docs/device-limit.md。

const deviceLimitOverrideColumns = `o.user_id, COALESCE(u.account, ''), o.exempt, o.max_devices, o.class_limits, o.note,
o.expires_at, o.updated_by, o.created_at, o.updated_at`

func scanDeviceLimitOverride(row pgx.Row) (*devicelimit.Override, error) {
	var item devicelimit.Override
	var limits []byte
	if err := row.Scan(&item.UserID, &item.Account, &item.Exempt, &item.MaxDevices, &limits, &item.Note,
		&item.ExpiresAt, &item.UpdatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	item.ClassLimits = map[string]int{}
	if len(limits) > 0 {
		_ = json.Unmarshal(limits, &item.ClassLimits)
	}
	return &item, nil
}

// GetDeviceLimitOverride 没有例外时返回 nil。
func (r *Repository) GetDeviceLimitOverride(ctx context.Context, appID, userID int64) (*devicelimit.Override, error) {
	item, err := scanDeviceLimitOverride(r.pool.QueryRow(ctx, `SELECT `+deviceLimitOverrideColumns+`
FROM user_device_limit_overrides o LEFT JOIN users u ON u.id = o.user_id
WHERE o.appid = $1 AND o.user_id = $2`, appID, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if item != nil {
		item.AppID = appID
	}
	return item, err
}

// UpsertDeviceLimitOverride 写入（覆盖）一个用户的例外。
func (r *Repository) UpsertDeviceLimitOverride(ctx context.Context, appID, userID int64, in devicelimit.OverrideInput, operator string) (*devicelimit.Override, error) {
	limits, err := json.Marshal(in.ClassLimits)
	if err != nil {
		return nil, err
	}
	if _, err := r.pool.Exec(ctx, `
INSERT INTO user_device_limit_overrides (appid, user_id, exempt, max_devices, class_limits, note, expires_at, updated_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (appid, user_id) DO UPDATE SET
    exempt = EXCLUDED.exempt,
    max_devices = EXCLUDED.max_devices,
    class_limits = EXCLUDED.class_limits,
    note = EXCLUDED.note,
    expires_at = EXCLUDED.expires_at,
    updated_by = EXCLUDED.updated_by,
    updated_at = NOW()`,
		appID, userID, in.Exempt, in.MaxDevices, limits, in.Note, in.ExpiresAt, operator); err != nil {
		return nil, err
	}
	return r.GetDeviceLimitOverride(ctx, appID, userID)
}

// DeleteDeviceLimitOverride 删除例外，返回是否真的删了一行。
func (r *Repository) DeleteDeviceLimitOverride(ctx context.Context, appID, userID int64) (bool, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM user_device_limit_overrides WHERE appid = $1 AND user_id = $2`, appID, userID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ListDeviceLimitOverrides 例外名单，最近修改的在前。关键字匹配账号或用户 ID。
func (r *Repository) ListDeviceLimitOverrides(ctx context.Context, appID int64, q devicelimit.OverrideQuery) ([]devicelimit.Override, int64, error) {
	where := []string{"o.appid = $1"}
	args := []any{appID}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		args = append(args, "%"+kw+"%")
		clause := fmt.Sprintf("(u.account ILIKE $%d OR o.note ILIKE $%d", len(args), len(args))
		if id, err := strconv.ParseInt(kw, 10, 64); err == nil {
			args = append(args, id)
			clause += fmt.Sprintf(" OR o.user_id = $%d", len(args))
		}
		where = append(where, clause+")")
	}
	clause := strings.Join(where, " AND ")
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM user_device_limit_overrides o LEFT JOIN users u ON u.id = o.user_id WHERE `+clause,
		args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, q.Limit, (q.Page-1)*q.Limit)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`SELECT `+deviceLimitOverrideColumns+`
FROM user_device_limit_overrides o LEFT JOIN users u ON u.id = o.user_id
WHERE %s ORDER BY o.updated_at DESC LIMIT $%d OFFSET $%d`, clause, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []devicelimit.Override{}
	for rows.Next() {
		item, err := scanDeviceLimitOverride(rows)
		if err != nil {
			return nil, 0, err
		}
		item.AppID = appID
		items = append(items, *item)
	}
	return items, total, rows.Err()
}

// TouchLoginDevice 记一次登录或刷新到设备台账。
func (r *Repository) TouchLoginDevice(ctx context.Context, touch devicelimit.LedgerTouch) error {
	var extra any
	if touch.Extra != nil {
		data, err := json.Marshal(touch.Extra)
		if err != nil {
			return err
		}
		extra = data
	}
	loginInc := 0
	if touch.Login {
		loginInc = 1
	}
	at := touch.At
	if at.IsZero() {
		at = time.Now()
	}
	_, err := r.pool.Exec(ctx, `
INSERT INTO user_login_devices (appid, user_id, device_key, platform, model, name, extra, first_seen_at, last_seen_at, last_ip, login_count)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $8, $9, $10)
ON CONFLICT (appid, user_id, device_key) DO UPDATE SET
    platform = COALESCE(NULLIF(EXCLUDED.platform, ''), user_login_devices.platform),
    model = COALESCE(NULLIF(EXCLUDED.model, ''), user_login_devices.model),
    name = COALESCE(NULLIF(EXCLUDED.name, ''), user_login_devices.name),
    extra = COALESCE(EXCLUDED.extra, user_login_devices.extra),
    last_seen_at = GREATEST(EXCLUDED.last_seen_at, user_login_devices.last_seen_at),
    last_ip = COALESCE(NULLIF(EXCLUDED.last_ip, ''), user_login_devices.last_ip),
    login_count = user_login_devices.login_count + $10`,
		touch.AppID, touch.UserID, clipString(touch.DeviceKey, 160), clipString(touch.Platform, 16), clipString(touch.Model, 64),
		clipString(touch.Name, 128), extra, at, clipString(touch.IP, 64), loginInc)
	return err
}

// LoginDeviceKnown 这台设备以前登录过没有。
func (r *Repository) LoginDeviceKnown(ctx context.Context, appID, userID int64, key string) (bool, error) {
	var known bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM user_login_devices WHERE appid = $1 AND user_id = $2 AND device_key = $3)`,
		appID, userID, key).Scan(&known)
	return known, err
}

// CountNewLoginDevices 某个时间之后首次出现的设备数。
func (r *Repository) CountNewLoginDevices(ctx context.Context, appID, userID int64, since time.Time) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM user_login_devices WHERE appid = $1 AND user_id = $2 AND first_seen_at >= $3`,
		appID, userID, since).Scan(&count)
	return count, err
}

// ListLoginDevices 用户的设备台账，最近出现的在前。
func (r *Repository) ListLoginDevices(ctx context.Context, appID, userID int64, limit int) ([]devicelimit.LedgerEntry, error) {
	rows, err := r.pool.Query(ctx, `
SELECT device_key, platform, model, name, extra, first_seen_at, last_seen_at, last_ip, login_count
FROM user_login_devices WHERE appid = $1 AND user_id = $2 ORDER BY last_seen_at DESC LIMIT $3`, appID, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []devicelimit.LedgerEntry{}
	for rows.Next() {
		var item devicelimit.LedgerEntry
		var extra []byte
		if err := rows.Scan(&item.DeviceKey, &item.Platform, &item.Model, &item.Name, &extra, &item.FirstSeenAt,
			&item.LastSeenAt, &item.LastIP, &item.LoginCount); err != nil {
			return nil, err
		}
		if len(extra) > 0 {
			var parsed devicedomain.Extra
			if json.Unmarshal(extra, &parsed) == nil {
				item.Extra = &parsed
			}
		}
		item.Class = devicelimit.ClassOf(item.Platform)
		items = append(items, item)
	}
	return items, rows.Err()
}

// DeleteLoginDevices 清空用户的设备台账（新设备频率从零算起）；key 非空时只删那一台。
func (r *Repository) DeleteLoginDevices(ctx context.Context, appID, userID int64, key string) (int64, error) {
	query := `DELETE FROM user_login_devices WHERE appid = $1 AND user_id = $2`
	args := []any{appID, userID}
	if key != "" {
		query += ` AND device_key = $3`
		args = append(args, key)
	}
	tag, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// InsertDeviceLimitEvents 批量写处置记录。
func (r *Repository) InsertDeviceLimitEvents(ctx context.Context, events []devicelimit.Event) error {
	if len(events) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, event := range events {
		detail := event.Detail
		if detail == nil {
			detail = map[string]any{}
		}
		data, err := json.Marshal(detail)
		if err != nil {
			return err
		}
		batch.Queue(`
INSERT INTO device_limit_events (appid, user_id, action, dimension, device_key, device_name, platform, device_class,
    trigger_name, ip, limit_value, detail, operator)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
			event.AppID, event.UserID, event.Action, event.Dimension, clipString(event.DeviceKey, 160), clipString(event.DeviceName, 128),
			clipString(event.Platform, 16), event.Class, clipString(event.TriggerName, 128), clipString(event.IP, 64), event.Limit,
			data, clipString(event.Operator, 128))
	}
	return r.pool.SendBatch(ctx, batch).Close()
}

const deviceLimitEventColumns = `e.id, e.user_id, COALESCE(u.account, ''), e.action, e.dimension, e.device_key, e.device_name,
e.platform, e.device_class, e.trigger_name, e.ip, e.limit_value, e.detail, e.operator, e.created_at`

func scanDeviceLimitEvent(row pgx.Row) (*devicelimit.Event, error) {
	var item devicelimit.Event
	var detail []byte
	if err := row.Scan(&item.ID, &item.UserID, &item.Account, &item.Action, &item.Dimension, &item.DeviceKey, &item.DeviceName,
		&item.Platform, &item.Class, &item.TriggerName, &item.IP, &item.Limit, &detail, &item.Operator, &item.CreatedAt); err != nil {
		return nil, err
	}
	if len(detail) > 0 {
		_ = json.Unmarshal(detail, &item.Detail)
	}
	if len(item.Detail) == 0 {
		item.Detail = nil
	}
	return &item, nil
}

// ListDeviceLimitEvents 处置记录，最新的在前。
func (r *Repository) ListDeviceLimitEvents(ctx context.Context, appID int64, q devicelimit.EventQuery) ([]devicelimit.Event, int64, error) {
	where := []string{"e.appid = $1"}
	args := []any{appID}
	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, strings.ReplaceAll(clause, "?", fmt.Sprintf("$%d", len(args))))
	}
	if q.Action != "" {
		add("e.action = ?", q.Action)
	}
	if q.Dimension != "" {
		add("e.dimension = ?", q.Dimension)
	}
	if q.UserID > 0 {
		add("e.user_id = ?", q.UserID)
	}
	if q.From != nil {
		add("e.created_at >= ?", *q.From)
	}
	if q.To != nil {
		add("e.created_at < ?", *q.To)
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		add("(u.account ILIKE ? OR e.device_name ILIKE ? OR e.trigger_name ILIKE ? OR e.ip ILIKE ? OR e.device_key ILIKE ?)", "%"+kw+"%")
	}
	clause := strings.Join(where, " AND ")
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM device_limit_events e LEFT JOIN users u ON u.id = e.user_id WHERE `+clause,
		args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, q.Limit, (q.Page-1)*q.Limit)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`SELECT `+deviceLimitEventColumns+`
FROM device_limit_events e LEFT JOIN users u ON u.id = e.user_id
WHERE %s ORDER BY e.created_at DESC, e.id DESC LIMIT $%d OFFSET $%d`, clause, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []devicelimit.Event{}
	for rows.Next() {
		item, err := scanDeviceLimitEvent(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, *item)
	}
	return items, total, rows.Err()
}

// DeviceLimitStats 概览需要的聚合。tz 是 IANA 时区名，按它划分自然日。
type DeviceLimitStats struct {
	Totals        map[string]int64
	Trend         map[string]map[string]int64
	Dimensions    map[string]int64
	ActiveDevices int64
	ActiveUsers   int64
	ByPlatform    map[string]int64
	Distribution  map[int]int64
	AtLimitUsers  int64
	Overrides     int64
	TopUsers      []devicelimit.TopUser
}

// DeviceLimitOverviewStats 概览聚合：since 之后的事件与台账。atLimit > 0 时统计设备数达到它的用户。
func (r *Repository) DeviceLimitOverviewStats(ctx context.Context, appID int64, since time.Time, tz string, atLimit int) (*DeviceLimitStats, error) {
	stats := &DeviceLimitStats{
		Totals: map[string]int64{}, Trend: map[string]map[string]int64{}, Dimensions: map[string]int64{},
		ByPlatform: map[string]int64{}, Distribution: map[int]int64{}, TopUsers: []devicelimit.TopUser{},
	}
	rows, err := r.pool.Query(ctx, `
SELECT to_char(date_trunc('day', created_at AT TIME ZONE $3), 'YYYY-MM-DD') AS day, action, dimension, COUNT(*)
FROM device_limit_events WHERE appid = $1 AND created_at >= $2
GROUP BY day, action, dimension`, appID, since, tz)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var day, action, dimension string
		var count int64
		if err := rows.Scan(&day, &action, &dimension, &count); err != nil {
			rows.Close()
			return nil, err
		}
		stats.Totals[action] += count
		if stats.Trend[day] == nil {
			stats.Trend[day] = map[string]int64{}
		}
		stats.Trend[day][action] += count
		if dimension != "" && (action == devicelimit.EventKicked || action == devicelimit.EventRejected ||
			action == devicelimit.EventChallenged || action == devicelimit.EventNewDevice) {
			stats.Dimensions[dimension] += count
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*), COUNT(DISTINCT user_id) FROM user_login_devices WHERE appid = $1 AND last_seen_at >= $2`,
		appID, since).Scan(&stats.ActiveDevices, &stats.ActiveUsers); err != nil {
		return nil, err
	}
	rows, err = r.pool.Query(ctx, `SELECT platform, COUNT(*) FROM user_login_devices WHERE appid = $1 AND last_seen_at >= $2 GROUP BY platform`,
		appID, since)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var platform string
		var count int64
		if err := rows.Scan(&platform, &count); err != nil {
			rows.Close()
			return nil, err
		}
		stats.ByPlatform[platform] += count
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = r.pool.Query(ctx, `
SELECT LEAST(cnt, 10) AS bucket, COUNT(*), COUNT(*) FILTER (WHERE $3 > 0 AND cnt >= $3)
FROM (SELECT user_id, COUNT(*) AS cnt FROM user_login_devices WHERE appid = $1 AND last_seen_at >= $2 GROUP BY user_id) t
GROUP BY bucket`, appID, since, atLimit)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var bucket int
		var count, reached int64
		if err := rows.Scan(&bucket, &count, &reached); err != nil {
			rows.Close()
			return nil, err
		}
		stats.Distribution[bucket] += count
		stats.AtLimitUsers += reached
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM user_device_limit_overrides WHERE appid = $1 AND (expires_at IS NULL OR expires_at > NOW())`,
		appID).Scan(&stats.Overrides); err != nil {
		return nil, err
	}

	rows, err = r.pool.Query(ctx, `
SELECT e.user_id, COALESCE(u.account, ''), COUNT(*), MAX(e.created_at)
FROM device_limit_events e LEFT JOIN users u ON u.id = e.user_id
WHERE e.appid = $1 AND e.created_at >= $2 AND e.action IN ('kicked', 'rejected', 'challenged', 'new_device')
GROUP BY e.user_id, u.account ORDER BY COUNT(*) DESC, MAX(e.created_at) DESC LIMIT 8`, appID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item devicelimit.TopUser
		if err := rows.Scan(&item.UserID, &item.Account, &item.Count, &item.LastAt); err != nil {
			return nil, err
		}
		stats.TopUsers = append(stats.TopUsers, item)
	}
	return stats, rows.Err()
}

// clipString 按字节截到上限并回退到合法的 UTF-8 边界，避免超长的客户端自报值让写入失败。
func clipString(value string, limit int) string {
	value = strings.TrimSpace(value)
	if len(value) <= limit {
		return value
	}
	return strings.ToValidUTF8(value[:limit], "")
}
