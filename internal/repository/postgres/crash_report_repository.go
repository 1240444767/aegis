package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"aegis/internal/domain/crashreport"
)

// 客户端崩溃报告（docs/crash-reports.md）。列表是动态筛选，按仓储层约定手写 pgx。

// crashReportColumns 列表与详情共用的列；正文只在 withReport 时取，列表不把几百 KB 的文本拖出库。
func crashReportColumns(withReport bool) string {
	report := "''"
	if withReport {
		report = "r.report"
	}
	return `r.id, r.public_id, r.appid, COALESCE(r.user_id, 0), COALESCE(u.public_id, 0), COALESCE(u.account, ''),
r.report_id, r.kind, r.title, r.detail, ` + report + `, r.report_bytes, r.app_version, r.version_code, r.os_version,
r.model, r.abi, r.process_name, r.source, r.device_id, r.device_info, r.user_agent, r.client_ip,
r.country_code, r.country, r.region, r.city, r.resolved_at, r.resolved_by, r.occurred_at, r.created_at`
}

const crashReportFrom = ` FROM app_crash_reports r LEFT JOIN users u ON u.id = r.user_id `

func scanCrashReport(row interface{ Scan(dest ...any) error }) (*crashreport.Report, error) {
	var item crashreport.Report
	var deviceInfo []byte
	err := row.Scan(&item.ID, &item.PublicID, &item.AppID, &item.UserID, &item.UserPublicID, &item.Account,
		&item.ReportID, &item.Kind, &item.Title, &item.Detail, &item.Report, &item.ReportBytes, &item.AppVersion,
		&item.VersionCode, &item.OSVersion, &item.Model, &item.ABI, &item.ProcessName, &item.Source, &item.DeviceID,
		&deviceInfo, &item.UserAgent, &item.ClientIP, &item.Location.CountryCode, &item.Location.Country,
		&item.Location.Region, &item.Location.City, &item.ResolvedAt, &item.ResolvedBy, &item.OccurredAt, &item.CreatedAt)
	if err != nil {
		return nil, err
	}
	if len(deviceInfo) > 0 {
		if err := json.Unmarshal(deviceInfo, &item.Device); err != nil {
			return nil, fmt.Errorf("decode crash report device info: %w", err)
		}
	}
	item.Resolved = item.ResolvedAt != nil
	return &item, nil
}

// InsertCrashReport 落一份报告。同一应用内 reportId 已存在时不写，返回 (nil, false, nil)，
// 由服务层回头取原来那一条 —— 并发的两次重传只有一次真正落库。
func (r *Repository) InsertCrashReport(ctx context.Context, item crashreport.Report) (*crashreport.Report, bool, error) {
	deviceInfo, err := json.Marshal(item.Device)
	if err != nil {
		return nil, false, err
	}
	var id int64
	err = r.pool.QueryRow(ctx, `INSERT INTO app_crash_reports
(public_id, appid, user_id, report_id, kind, title, detail, report, report_bytes, app_version, version_code,
 os_version, model, abi, process_name, source, device_id, device_info, user_agent, client_ip,
 country_code, country, region, city, occurred_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25)
ON CONFLICT (appid, report_id) DO NOTHING
RETURNING id`,
		item.PublicID, item.AppID, nullableInt64(item.UserID), item.ReportID, item.Kind, item.Title, item.Detail,
		item.Report, item.ReportBytes, item.AppVersion, item.VersionCode, item.OSVersion, item.Model, item.ABI,
		item.ProcessName, item.Source, item.DeviceID, deviceInfo, item.UserAgent, item.ClientIP,
		item.Location.CountryCode, item.Location.Country, item.Location.Region, item.Location.City,
		item.OccurredAt).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}
	saved, err := r.GetCrashReport(ctx, item.AppID, id)
	if err != nil {
		return nil, false, err
	}
	return saved, true, nil
}

// FindCrashReportByReportID 按客户端记录编号找；没有时返回 (nil, nil)。只取列表视图，不带正文。
func (r *Repository) FindCrashReportByReportID(ctx context.Context, appID int64, reportID string) (*crashreport.Report, error) {
	item, err := scanCrashReport(r.pool.QueryRow(ctx, `SELECT `+crashReportColumns(false)+crashReportFrom+
		`WHERE r.appid = $1 AND r.report_id = $2`, appID, reportID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return item, err
}

// GetCrashReport 详情（含正文）；不存在或不属于该应用时返回 (nil, nil)。
func (r *Repository) GetCrashReport(ctx context.Context, appID, id int64) (*crashreport.Report, error) {
	item, err := scanCrashReport(r.pool.QueryRow(ctx, `SELECT `+crashReportColumns(true)+crashReportFrom+
		`WHERE r.appid = $1 AND r.id = $2`, appID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return item, err
}

// CountRecentCrashReports 某台设备、某个 IP 自 since 起各报了几份。值为空的那一项返回 0、不查库。
func (r *Repository) CountRecentCrashReports(ctx context.Context, appID int64, deviceID, clientIP string, since time.Time) (int64, int64, error) {
	count := func(column, value string) (int64, error) {
		if value == "" {
			return 0, nil
		}
		var total int64
		err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM app_crash_reports
WHERE appid = $1 AND `+column+` = $2 AND created_at >= $3`, appID, value, since).Scan(&total)
		return total, err
	}
	byDevice, err := count("device_id", deviceID)
	if err != nil {
		return 0, 0, err
	}
	byIP, err := count("client_ip", clientIP)
	if err != nil {
		return 0, 0, err
	}
	return byDevice, byIP, nil
}

// crashReportWhere 列表与筛选共用的条件。
func crashReportWhere(query crashreport.Query) (string, []any) {
	where := []string{"r.appid = $1"}
	args := []any{query.AppID}
	add := func(format string, value any) {
		args = append(args, value)
		where = append(where, fmt.Sprintf(format, len(args)))
	}
	if kind := strings.TrimSpace(query.Kind); kind != "" {
		add("r.kind = $%d", kind)
	}
	if version := strings.TrimSpace(query.AppVersion); version != "" {
		add("r.app_version = $%d", version)
	}
	if query.Resolved != nil {
		if *query.Resolved {
			where = append(where, "r.resolved_at IS NOT NULL")
		} else {
			where = append(where, "r.resolved_at IS NULL")
		}
	}
	if query.UserID > 0 {
		add("r.user_id = $%d", query.UserID)
	}
	if deviceID := strings.TrimSpace(query.DeviceID); deviceID != "" {
		add("r.device_id = $%d", deviceID)
	}
	if query.Start != nil {
		add("r.occurred_at >= $%d", *query.Start)
	}
	if query.End != nil {
		add("r.occurred_at < $%d", *query.End)
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		args = append(args, "%"+escapeLikePattern(keyword)+"%")
		n := len(args)
		// 标题与详情是要找的东西；两个编号是用户反馈时报过来的号，顺带一起认
		where = append(where, fmt.Sprintf(
			"(r.title ILIKE $%d OR r.detail ILIKE $%d OR r.public_id ILIKE $%d OR r.report_id ILIKE $%d)", n, n, n, n))
	}
	return "WHERE " + strings.Join(where, " AND "), args
}

// ListCrashReports 分页列表，不带正文。
func (r *Repository) ListCrashReports(ctx context.Context, query crashreport.Query) (*crashreport.Page, error) {
	clause, args := crashReportWhere(query)
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM app_crash_reports r `+clause, args...).Scan(&total); err != nil {
		return nil, err
	}
	page, limit := adPolicyPaging(query.Page, query.Limit)
	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, `SELECT `+crashReportColumns(false)+crashReportFrom+clause+
		fmt.Sprintf(" ORDER BY r.occurred_at DESC, r.id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]crashreport.Report, 0, limit)
	for rows.Next() {
		item, err := scanCrashReport(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	totalPages := int((total + int64(limit) - 1) / int64(limit))
	return &crashreport.Page{Items: items, Total: total, Page: page, Limit: limit, TotalPages: totalPages}, nil
}

// CrashReportFacets 摘要与筛选项：总数、未处理、近 24 小时、各类型数量、出现过的版本（按数量取前 50）。
func (r *Repository) CrashReportFacets(ctx context.Context, appID int64, now time.Time) (*crashreport.Facets, error) {
	facets := &crashreport.Facets{Kinds: []crashreport.KindCount{}, Versions: []crashreport.VersionCount{}}
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*), COUNT(*) FILTER (WHERE resolved_at IS NULL),
COUNT(*) FILTER (WHERE occurred_at >= $2)
FROM app_crash_reports WHERE appid = $1`, appID, now.Add(-24*time.Hour)).
		Scan(&facets.Total, &facets.Unresolved, &facets.Last24h)
	if err != nil {
		return nil, err
	}
	kindRows, err := r.pool.Query(ctx, `SELECT kind, COUNT(*) FROM app_crash_reports WHERE appid = $1
GROUP BY kind ORDER BY COUNT(*) DESC`, appID)
	if err != nil {
		return nil, err
	}
	for kindRows.Next() {
		var item crashreport.KindCount
		if err := kindRows.Scan(&item.Kind, &item.Count); err != nil {
			kindRows.Close()
			return nil, err
		}
		facets.Kinds = append(facets.Kinds, item)
	}
	kindRows.Close()
	if err := kindRows.Err(); err != nil {
		return nil, err
	}
	versionRows, err := r.pool.Query(ctx, `SELECT app_version, COUNT(*) FROM app_crash_reports
WHERE appid = $1 AND app_version <> '' GROUP BY app_version ORDER BY COUNT(*) DESC, app_version DESC LIMIT 50`, appID)
	if err != nil {
		return nil, err
	}
	defer versionRows.Close()
	for versionRows.Next() {
		var item crashreport.VersionCount
		if err := versionRows.Scan(&item.AppVersion, &item.Count); err != nil {
			return nil, err
		}
		facets.Versions = append(facets.Versions, item)
	}
	return facets, versionRows.Err()
}

// SetCrashReportsResolved 标记或取消「已处理」，返回实际改动的行数。已是目标状态的不重写处理人与时间。
func (r *Repository) SetCrashReportsResolved(ctx context.Context, appID int64, ids []int64, resolved bool, operator string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	var (
		tag pgconn.CommandTag
		err error
	)
	if resolved {
		tag, err = r.pool.Exec(ctx, `UPDATE app_crash_reports SET resolved_at = NOW(), resolved_by = $3
WHERE appid = $1 AND id = ANY($2) AND resolved_at IS NULL`, appID, ids, operator)
	} else {
		tag, err = r.pool.Exec(ctx, `UPDATE app_crash_reports SET resolved_at = NULL, resolved_by = ''
WHERE appid = $1 AND id = ANY($2) AND resolved_at IS NOT NULL`, appID, ids)
	}
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// DeleteCrashReports 删除（真删），返回删掉了几条。只删本应用的。
func (r *Repository) DeleteCrashReports(ctx context.Context, appID int64, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	tag, err := r.pool.Exec(ctx, `DELETE FROM app_crash_reports WHERE appid = $1 AND id = ANY($2)`, appID, ids)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
