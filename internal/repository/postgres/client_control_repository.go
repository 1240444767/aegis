package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	appdomain "aegis/internal/domain/app"

	"github.com/jackc/pgx/v5"
)

// 版本管控的数据访问。规则见 internal/domain/app/client_control.go 与 docs/client-control.md。

const clientControlSettingsColumns = `appid, enabled, policy_version, allowed_packages, allowed_certs,
verdict_ttl_seconds, recheck_seconds, offline_policy, watch_enabled, server_gate,
block_title, block_message, updated_by, created_at, updated_at`

func scanClientControlSettings(row pgx.Row) (*appdomain.ClientControlSettings, error) {
	var item appdomain.ClientControlSettings
	var createdAt time.Time
	if err := row.Scan(&item.AppID, &item.Enabled, &item.PolicyVersion, &item.AllowedPackages, &item.AllowedCerts,
		&item.VerdictTTLSeconds, &item.RecheckSeconds, &item.OfflinePolicy, &item.WatchEnabled, &item.ServerGate,
		&item.BlockTitle, &item.BlockMessage, &item.UpdatedBy, &createdAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	item.CreatedAt = &createdAt
	if item.AllowedPackages == nil {
		item.AllowedPackages = []string{}
	}
	if item.AllowedCerts == nil {
		item.AllowedCerts = []string{}
	}
	return &item, nil
}

// GetClientControlSettings 没有设置行时返回 nil。
func (r *Repository) GetClientControlSettings(ctx context.Context, appID int64) (*appdomain.ClientControlSettings, error) {
	item, err := scanClientControlSettings(r.pool.QueryRow(ctx,
		`SELECT `+clientControlSettingsColumns+` FROM app_client_control_settings WHERE appid = $1`, appID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return item, err
}

// SaveClientControlSettings 写入整份设置并把策略版本加一。
func (r *Repository) SaveClientControlSettings(ctx context.Context, s appdomain.ClientControlSettings) (*appdomain.ClientControlSettings, error) {
	return scanClientControlSettings(r.pool.QueryRow(ctx, `
INSERT INTO app_client_control_settings (appid, enabled, policy_version, allowed_packages, allowed_certs,
    verdict_ttl_seconds, recheck_seconds, offline_policy, watch_enabled, server_gate, block_title, block_message,
    updated_by, updated_at)
VALUES ($1, $2, 2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NOW())
ON CONFLICT (appid) DO UPDATE SET
    enabled = EXCLUDED.enabled,
    policy_version = app_client_control_settings.policy_version + 1,
    allowed_packages = EXCLUDED.allowed_packages,
    allowed_certs = EXCLUDED.allowed_certs,
    verdict_ttl_seconds = EXCLUDED.verdict_ttl_seconds,
    recheck_seconds = EXCLUDED.recheck_seconds,
    offline_policy = EXCLUDED.offline_policy,
    watch_enabled = EXCLUDED.watch_enabled,
    server_gate = EXCLUDED.server_gate,
    block_title = EXCLUDED.block_title,
    block_message = EXCLUDED.block_message,
    updated_by = EXCLUDED.updated_by,
    updated_at = NOW()
RETURNING `+clientControlSettingsColumns,
		s.AppID, s.Enabled, s.AllowedPackages, s.AllowedCerts, s.VerdictTTLSeconds, s.RecheckSeconds,
		s.OfflinePolicy, s.WatchEnabled, s.ServerGate, s.BlockTitle, s.BlockMessage, s.UpdatedBy))
}

// BumpClientControlPolicyVersion 规则变更后把策略版本加一；没有设置行时建一行（未启用）。
func (r *Repository) BumpClientControlPolicyVersion(ctx context.Context, appID int64) (int64, error) {
	var version int64
	err := r.pool.QueryRow(ctx, `
INSERT INTO app_client_control_settings (appid, policy_version) VALUES ($1, 2)
ON CONFLICT (appid) DO UPDATE SET policy_version = app_client_control_settings.policy_version + 1, updated_at = NOW()
RETURNING policy_version`, appID).Scan(&version)
	return version, err
}

/* ───────────────────── 密钥 ───────────────────── */

const clientControlKeyColumns = `id, appid, key_id, kind, public_key, private_key_cipher, status, endorsed_by, endorsement, created_at, retired_at`

func scanClientControlKey(row pgx.Row) (*appdomain.ClientControlKey, error) {
	var item appdomain.ClientControlKey
	if err := row.Scan(&item.ID, &item.AppID, &item.KeyID, &item.Kind, &item.PublicKey, &item.PrivateKeyCipher,
		&item.Status, &item.EndorsedBy, &item.Endorsement, &item.CreatedAt, &item.RetiredAt); err != nil {
		return nil, err
	}
	return &item, nil
}

// ListClientControlKeys 全部密钥（含已退役），按创建时间升序 —— 背书链就是这个顺序。
func (r *Repository) ListClientControlKeys(ctx context.Context, appID int64) ([]appdomain.ClientControlKey, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+clientControlKeyColumns+`
FROM app_client_control_keys WHERE appid = $1 ORDER BY created_at, id`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []appdomain.ClientControlKey{}
	for rows.Next() {
		item, err := scanClientControlKey(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

// InsertClientControlKey 把同类 active 密钥退役并写入新密钥，同一事务内完成。
// 并发轮换时唯一索引 uq_app_client_control_keys_active 保证只有一把 active。
func (r *Repository) InsertClientControlKey(ctx context.Context, key appdomain.ClientControlKey) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `UPDATE app_client_control_keys SET status = 'retired', retired_at = NOW()
WHERE appid = $1 AND kind = $2 AND status = 'active'`, key.AppID, key.Kind); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO app_client_control_keys
(appid, key_id, kind, public_key, private_key_cipher, status, endorsed_by, endorsement)
VALUES ($1, $2, $3, $4, $5, 'active', $6, $7)`,
		key.AppID, key.KeyID, key.Kind, key.PublicKey, key.PrivateKeyCipher, key.EndorsedBy, key.Endorsement); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// InsertClientControlKeyIfAbsent 首次生成密钥用：该应用该类已有 active 密钥时什么也不做，返回 false。
// 多个实例同时懒生成时只有一个写得进去，其余重新读取即可，不会把刚生成的根密钥挤成退役。
func (r *Repository) InsertClientControlKeyIfAbsent(ctx context.Context, key appdomain.ClientControlKey) (bool, error) {
	tag, err := r.pool.Exec(ctx, `INSERT INTO app_client_control_keys
(appid, key_id, kind, public_key, private_key_cipher, status, endorsed_by, endorsement)
VALUES ($1, $2, $3, $4, $5, 'active', $6, $7)
ON CONFLICT (appid, kind) WHERE status = 'active' DO NOTHING`,
		key.AppID, key.KeyID, key.Kind, key.PublicKey, key.PrivateKeyCipher, key.EndorsedBy, key.Endorsement)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

/* ───────────────────── 规则 ───────────────────── */

const clientControlRuleColumns = `id, appid, name, enabled, priority, action, conditions, title, message,
action_url, action_label, starts_at, ends_at, note, hit_count, last_hit_at, created_by, created_at, updated_at`

func scanClientControlRule(row pgx.Row) (*appdomain.ClientControlRule, error) {
	var item appdomain.ClientControlRule
	var raw []byte
	if err := row.Scan(&item.ID, &item.AppID, &item.Name, &item.Enabled, &item.Priority, &item.Action, &raw,
		&item.Title, &item.Message, &item.ActionURL, &item.ActionLabel, &item.StartsAt, &item.EndsAt, &item.Note,
		&item.HitCount, &item.LastHitAt, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &item.Conditions); err != nil {
			return nil, fmt.Errorf("decode client control conditions %d: %w", item.ID, err)
		}
	}
	return &item, nil
}

func (r *Repository) ListClientControlRules(ctx context.Context, appID int64) ([]appdomain.ClientControlRule, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+clientControlRuleColumns+`
FROM app_client_control_rules WHERE appid = $1 ORDER BY priority DESC, id`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []appdomain.ClientControlRule{}
	for rows.Next() {
		item, err := scanClientControlRule(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func (r *Repository) GetClientControlRule(ctx context.Context, appID, ruleID int64) (*appdomain.ClientControlRule, error) {
	item, err := scanClientControlRule(r.pool.QueryRow(ctx, `SELECT `+clientControlRuleColumns+`
FROM app_client_control_rules WHERE appid = $1 AND id = $2`, appID, ruleID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return item, err
}

func (r *Repository) InsertClientControlRule(ctx context.Context, rule appdomain.ClientControlRule) (*appdomain.ClientControlRule, error) {
	conditions, err := json.Marshal(rule.Conditions)
	if err != nil {
		return nil, err
	}
	return scanClientControlRule(r.pool.QueryRow(ctx, `INSERT INTO app_client_control_rules
(appid, name, enabled, priority, action, conditions, title, message, action_url, action_label, starts_at, ends_at, note, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
RETURNING `+clientControlRuleColumns,
		rule.AppID, rule.Name, rule.Enabled, rule.Priority, rule.Action, conditions, rule.Title, rule.Message,
		rule.ActionURL, rule.ActionLabel, rule.StartsAt, rule.EndsAt, rule.Note, rule.CreatedBy))
}

// UpdateClientControlRule 整体覆盖（命中计数与创建信息不动）。规则不存在时返回 nil。
func (r *Repository) UpdateClientControlRule(ctx context.Context, rule appdomain.ClientControlRule) (*appdomain.ClientControlRule, error) {
	conditions, err := json.Marshal(rule.Conditions)
	if err != nil {
		return nil, err
	}
	item, err := scanClientControlRule(r.pool.QueryRow(ctx, `UPDATE app_client_control_rules SET
name = $3, enabled = $4, priority = $5, action = $6, conditions = $7, title = $8, message = $9,
action_url = $10, action_label = $11, starts_at = $12, ends_at = $13, note = $14, updated_at = NOW()
WHERE appid = $1 AND id = $2
RETURNING `+clientControlRuleColumns,
		rule.AppID, rule.ID, rule.Name, rule.Enabled, rule.Priority, rule.Action, conditions, rule.Title, rule.Message,
		rule.ActionURL, rule.ActionLabel, rule.StartsAt, rule.EndsAt, rule.Note))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return item, err
}

func (r *Repository) SetClientControlRuleEnabled(ctx context.Context, appID, ruleID int64, enabled bool) (*appdomain.ClientControlRule, error) {
	item, err := scanClientControlRule(r.pool.QueryRow(ctx, `UPDATE app_client_control_rules SET enabled = $3, updated_at = NOW()
WHERE appid = $1 AND id = $2 RETURNING `+clientControlRuleColumns, appID, ruleID, enabled))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return item, err
}

// DeleteClientControlRule 返回是否删到了。
func (r *Repository) DeleteClientControlRule(ctx context.Context, appID, ruleID int64) (bool, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM app_client_control_rules WHERE appid = $1 AND id = $2`, appID, ruleID)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// SetClientControlRulePriorities 按给定顺序重排：第一个最高。不属于该应用的 id 被忽略。
func (r *Repository) SetClientControlRulePriorities(ctx context.Context, appID int64, orderedIDs []int64) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	top := len(orderedIDs) * 10
	for i, id := range orderedIDs {
		if _, err := tx.Exec(ctx, `UPDATE app_client_control_rules SET priority = $3, updated_at = NOW()
WHERE appid = $1 AND id = $2`, appID, id, top-i*10); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (r *Repository) RecordClientControlRuleHit(ctx context.Context, ruleID int64) error {
	_, err := r.pool.Exec(ctx, `UPDATE app_client_control_rules SET hit_count = hit_count + 1, last_hit_at = NOW() WHERE id = $1`, ruleID)
	return err
}

/* ───────────────────── 安装与统计 ───────────────────── */

// UpsertClientInstall 记下一次上报。返回这台设备今天是否第一次出现（用于按天去重计数）。
func (r *Repository) UpsertClientInstall(ctx context.Context, appID int64, item appdomain.ClientInstall) (bool, error) {
	var previous *time.Time
	err := r.pool.QueryRow(ctx, `
WITH prior AS (
    SELECT last_seen_at FROM app_client_installs WHERE appid = $1 AND device_id = $2 AND package_name = $3
)
INSERT INTO app_client_installs (appid, device_id, package_name, version_name, version_code, platform, cert_sha256,
    installer, os_version, model, brand, abi, user_id, ip, last_action, last_reason, last_rule_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
ON CONFLICT (appid, device_id, package_name) DO UPDATE SET
    version_name = EXCLUDED.version_name,
    version_code = EXCLUDED.version_code,
    platform = EXCLUDED.platform,
    cert_sha256 = EXCLUDED.cert_sha256,
    installer = EXCLUDED.installer,
    os_version = EXCLUDED.os_version,
    model = EXCLUDED.model,
    brand = EXCLUDED.brand,
    abi = EXCLUDED.abi,
    user_id = COALESCE(EXCLUDED.user_id, app_client_installs.user_id),
    ip = EXCLUDED.ip,
    last_action = EXCLUDED.last_action,
    last_reason = EXCLUDED.last_reason,
    last_rule_id = EXCLUDED.last_rule_id,
    report_count = app_client_installs.report_count + 1,
    last_seen_at = NOW()
RETURNING (SELECT last_seen_at FROM prior)`,
		appID, item.DeviceID, item.PackageName, item.VersionName, item.VersionCode, item.Platform, item.CertSHA256,
		item.Installer, item.OSVersion, item.Model, item.Brand, item.ABI, item.UserID, item.IP,
		item.LastAction, item.LastReason, item.LastRuleID).Scan(&previous)
	if err != nil {
		return false, err
	}
	if previous == nil {
		return true, nil
	}
	y1, m1, d1 := previous.Date()
	y2, m2, d2 := time.Now().In(previous.Location()).Date()
	return y1 != y2 || m1 != m2 || d1 != d2, nil
}

func (r *Repository) RecordClientControlDaily(ctx context.Context, appID int64, day time.Time, newDevice, blocked bool) error {
	devices, blockedCount := 0, 0
	if newDevice {
		devices = 1
	}
	if blocked {
		blockedCount = 1
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO app_client_control_daily (appid, day, checks, devices, blocked)
VALUES ($1, $2, 1, $3, $4)
ON CONFLICT (appid, day) DO UPDATE SET checks = app_client_control_daily.checks + 1,
    devices = app_client_control_daily.devices + EXCLUDED.devices,
    blocked = app_client_control_daily.blocked + EXCLUDED.blocked`,
		appID, day.Format("2006-01-02"), devices, blockedCount)
	return err
}

func (r *Repository) ListClientControlDaily(ctx context.Context, appID int64, since time.Time) ([]appdomain.ClientControlDayStat, error) {
	rows, err := r.pool.Query(ctx, `SELECT to_char(day, 'YYYY-MM-DD'), devices, blocked FROM app_client_control_daily
WHERE appid = $1 AND day >= $2 ORDER BY day`, appID, since.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []appdomain.ClientControlDayStat{}
	for rows.Next() {
		var item appdomain.ClientControlDayStat
		if err := rows.Scan(&item.Day, &item.Devices, &item.Blocked); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

const clientInstallColumns = `device_id, package_name, version_name, version_code, platform, cert_sha256, installer,
os_version, model, brand, abi, user_id, ip, last_action, last_reason, last_rule_id, report_count, first_seen_at, last_seen_at`

func (r *Repository) ListClientInstalls(ctx context.Context, appID int64, q appdomain.ClientInstallQuery) ([]appdomain.ClientInstall, int64, error) {
	where := []string{"appid = $1"}
	args := []any{appID}
	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, strings.ReplaceAll(clause, "?", fmt.Sprintf("$%d", len(args))))
	}
	if q.Days > 0 {
		add("last_seen_at >= ?", time.Now().Add(-time.Duration(q.Days)*24*time.Hour))
	}
	if q.PackageName != "" {
		add("package_name = ?", q.PackageName)
	}
	if q.VersionCode > 0 {
		add("version_code = ?", q.VersionCode)
	}
	if q.CertSHA256 != "" {
		add("cert_sha256 = ?", q.CertSHA256)
	}
	if q.Action != "" {
		add("last_action = ?", q.Action)
	}
	if kw := strings.TrimSpace(q.Keyword); kw != "" {
		add("(device_id ILIKE ? OR model ILIKE ? OR brand ILIKE ? OR ip ILIKE ? OR package_name ILIKE ? OR version_name ILIKE ?)", "%"+kw+"%")
	}
	clause := strings.Join(where, " AND ")
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM app_client_installs WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, q.Limit, (q.Page-1)*q.Limit)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`SELECT `+clientInstallColumns+` FROM app_client_installs WHERE %s
ORDER BY last_seen_at DESC LIMIT $%d OFFSET $%d`, clause, len(args)-1, len(args)), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := []appdomain.ClientInstall{}
	for rows.Next() {
		var item appdomain.ClientInstall
		if err := rows.Scan(&item.DeviceID, &item.PackageName, &item.VersionName, &item.VersionCode, &item.Platform,
			&item.CertSHA256, &item.Installer, &item.OSVersion, &item.Model, &item.Brand, &item.ABI, &item.UserID,
			&item.IP, &item.LastAction, &item.LastReason, &item.LastRuleID, &item.ReportCount,
			&item.FirstSeenAt, &item.LastSeenAt); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

// ListClientInstallGroups 版本分布：按包名、版本号、签名、平台聚合，设备数多的在前。
func (r *Repository) ListClientInstallGroups(ctx context.Context, appID int64, since time.Time, limit int) ([]appdomain.ClientInstallGroup, error) {
	rows, err := r.pool.Query(ctx, `SELECT package_name, MAX(version_name), version_code, cert_sha256, platform,
COUNT(*), SUM(report_count), COUNT(*) FILTER (WHERE last_action IN ('block', 'force_update')),
MIN(first_seen_at), MAX(last_seen_at)
FROM app_client_installs WHERE appid = $1 AND last_seen_at >= $2
GROUP BY package_name, version_code, cert_sha256, platform
ORDER BY COUNT(*) DESC, version_code DESC LIMIT $3`, appID, since, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []appdomain.ClientInstallGroup{}
	for rows.Next() {
		var item appdomain.ClientInstallGroup
		if err := rows.Scan(&item.PackageName, &item.VersionName, &item.VersionCode, &item.CertSHA256, &item.Platform,
			&item.Devices, &item.Reports, &item.Blocked, &item.FirstSeenAt, &item.LastSeenAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ClientControlCounts 概览用的计数。
type ClientControlCounts struct {
	Devices24h, Devices7d, Devices30d, Blocked7d, Packages, Versions int64
	Actions                                                          map[string]int64
}

func (r *Repository) ClientControlCounts(ctx context.Context, appID int64) (*ClientControlCounts, error) {
	now := time.Now()
	result := &ClientControlCounts{Actions: map[string]int64{}}
	if err := r.pool.QueryRow(ctx, `SELECT
COUNT(*) FILTER (WHERE last_seen_at >= $2),
COUNT(*) FILTER (WHERE last_seen_at >= $3),
COUNT(*) FILTER (WHERE last_seen_at >= $4),
COUNT(*) FILTER (WHERE last_seen_at >= $3 AND last_action IN ('block', 'force_update')),
COUNT(DISTINCT package_name) FILTER (WHERE last_seen_at >= $4),
COUNT(DISTINCT (package_name, version_code)) FILTER (WHERE last_seen_at >= $4)
FROM app_client_installs WHERE appid = $1`, appID, now.Add(-24*time.Hour), now.Add(-7*24*time.Hour), now.Add(-30*24*time.Hour)).
		Scan(&result.Devices24h, &result.Devices7d, &result.Devices30d, &result.Blocked7d, &result.Packages, &result.Versions); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT last_action, COUNT(*) FROM app_client_installs
WHERE appid = $1 AND last_seen_at >= $2 GROUP BY last_action`, appID, now.Add(-7*24*time.Hour))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var action string
		var count int64
		if err := rows.Scan(&action, &count); err != nil {
			return nil, err
		}
		result.Actions[action] = count
	}
	return result, rows.Err()
}

func (r *Repository) DeleteClientInstall(ctx context.Context, appID int64, deviceID, packageName string) (bool, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM app_client_installs WHERE appid = $1 AND device_id = $2 AND package_name = $3`,
		appID, deviceID, packageName)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}
