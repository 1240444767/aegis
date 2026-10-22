package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	securitydomain "aegis/internal/domain/security"
)

// ════════════════════════════════════════════════════════════
//  名单库（risk_lists）
// ════════════════════════════════════════════════════════════

const riskListColumns = `id, app_id, list_type, value, kind, scenes, reason, source, assessment_id,
       expires_at, hit_count, last_hit_at, created_by, created_at, updated_at`

// UpsertRiskListEntries 批量写入名单。同一作用域里同一对象只有一条：已存在则改写种类、场景、理由与到期，
// 「先加白再拉黑」得到的是一条黑名单，而不是一黑一白两条互相打架。
// 返回 (新建数, 改写数, 写入后的条目)。
func (r *Repository) UpsertRiskListEntries(ctx context.Context, input securitydomain.ListEntryInput) (int, int, []securitydomain.RiskListEntry, error) {
	if len(input.Values) == 0 {
		return 0, 0, nil, nil
	}
	scenes := input.Scenes
	if scenes == nil {
		scenes = []string{}
	}
	source := input.Source
	if source == "" {
		source = "manual"
	}
	created, updated := 0, 0
	entries := make([]securitydomain.RiskListEntry, 0, len(input.Values))
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, 0, nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	for _, value := range input.Values {
		var inserted bool
		row := tx.QueryRow(ctx, `INSERT INTO risk_lists
    (app_id, list_type, value, kind, scenes, reason, source, assessment_id, expires_at, created_by, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW(), NOW())
ON CONFLICT (COALESCE(app_id, 0), list_type, value) DO UPDATE SET
    kind = EXCLUDED.kind,
    scenes = EXCLUDED.scenes,
    reason = CASE WHEN EXCLUDED.reason <> '' THEN EXCLUDED.reason ELSE risk_lists.reason END,
    source = EXCLUDED.source,
    assessment_id = COALESCE(EXCLUDED.assessment_id, risk_lists.assessment_id),
    expires_at = EXCLUDED.expires_at,
    updated_at = NOW()
RETURNING `+riskListColumns+`, (xmax = 0)`,
			riskAppIDArg(input.AppID), input.ListType, truncateColumn(value, 256), input.Kind, scenes,
			input.Reason, source, input.AssessmentID, input.ExpiresAt, input.CreatedBy)
		entry, err := scanRiskListEntry(row, &inserted)
		if err != nil {
			return 0, 0, nil, err
		}
		if inserted {
			created++
		} else {
			updated++
		}
		entries = append(entries, *entry)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, 0, nil, err
	}
	return created, updated, entries, nil
}

// GetRiskListEntry 按 ID 取名单条目
func (r *Repository) GetRiskListEntry(ctx context.Context, id int64) (*securitydomain.RiskListEntry, error) {
	return scanRiskListEntry(r.pool.QueryRow(ctx, `SELECT `+riskListColumns+` FROM risk_lists WHERE id = $1`, id), nil)
}

// UpdateRiskListEntry 修改名单条目
func (r *Repository) UpdateRiskListEntry(ctx context.Context, id int64, input securitydomain.ListEntryUpdate) (*securitydomain.RiskListEntry, error) {
	sets := make([]string, 0, 5)
	args := make([]any, 0, 6)
	idx := 1
	add := func(column string, value any) {
		sets = append(sets, fmt.Sprintf("%s = $%d", column, idx))
		args = append(args, value)
		idx++
	}
	if input.Kind != nil {
		add("kind", *input.Kind)
	}
	if input.Scenes != nil {
		scenes := *input.Scenes
		if scenes == nil {
			scenes = []string{}
		}
		add("scenes", scenes)
	}
	if input.Reason != nil {
		add("reason", *input.Reason)
	}
	if input.ExpiresAt != nil {
		add("expires_at", *input.ExpiresAt)
	}
	if len(sets) == 0 {
		return r.GetRiskListEntry(ctx, id)
	}
	sets = append(sets, "updated_at = NOW()")
	args = append(args, id)
	return scanRiskListEntry(r.pool.QueryRow(ctx,
		fmt.Sprintf("UPDATE risk_lists SET %s WHERE id = $%d RETURNING %s", strings.Join(sets, ", "), idx, riskListColumns),
		args...), nil)
}

// DeleteRiskListEntries 删除名单条目，返回被删掉的条目（调用方据此同步设备 / IP 标签）
func (r *Repository) DeleteRiskListEntries(ctx context.Context, ids []int64) ([]securitydomain.RiskListEntry, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := r.pool.Query(ctx, `DELETE FROM risk_lists WHERE id = ANY($1) RETURNING `+riskListColumns, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectRiskListEntries(rows)
}

// DeleteRiskListEntriesByAssessment 撤销某条评估记录自动产生的名单（复核通过时）
func (r *Repository) DeleteRiskListEntriesByAssessment(ctx context.Context, assessmentID int64, kind string) ([]securitydomain.RiskListEntry, error) {
	rows, err := r.pool.Query(ctx, `DELETE FROM risk_lists WHERE assessment_id = $1 AND kind = $2 RETURNING `+riskListColumns,
		assessmentID, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectRiskListEntries(rows)
}

// ListRiskListEntries 分页查询名单
func (r *Repository) ListRiskListEntries(ctx context.Context, query securitydomain.ListQuery) ([]securitydomain.RiskListEntry, int64, error) {
	page, limit := normalizePage(query.Page, query.PageSize)
	where := make([]string, 0, 6)
	args := make([]any, 0, 8)
	idx := 1
	add := func(clause string, value any) {
		where = append(where, fmt.Sprintf(clause, idx))
		args = append(args, value)
		idx++
	}
	if query.AppID != nil {
		if *query.AppID <= 0 {
			where = append(where, "app_id IS NULL")
		} else {
			add("(app_id IS NULL OR app_id = $%d)", *query.AppID)
		}
	}
	if query.ListType != "" {
		add("list_type = $%d", query.ListType)
	}
	if query.Kind != "" {
		add("kind = $%d", query.Kind)
	}
	if query.Source != "" {
		add("source = $%d", query.Source)
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		where = append(where, fmt.Sprintf("(value ILIKE $%d OR reason ILIKE $%d)", idx, idx))
		args = append(args, "%"+keyword+"%")
		idx++
	}
	if query.Active != nil {
		if *query.Active {
			where = append(where, "(expires_at IS NULL OR expires_at > NOW())")
		} else {
			where = append(where, "expires_at IS NOT NULL AND expires_at <= NOW()")
		}
	}
	clause := ""
	if len(where) > 0 {
		clause = " WHERE " + strings.Join(where, " AND ")
	}
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM risk_lists`+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []securitydomain.RiskListEntry{}, 0, nil
	}
	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`SELECT %s FROM risk_lists%s ORDER BY updated_at DESC, id DESC LIMIT $%d OFFSET $%d`,
		riskListColumns, clause, idx, idx+1), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items, err := collectRiskListEntries(rows)
	return items, total, err
}

// ListActiveRiskListEntries 全部现行名单，评估引擎整体装进内存快照
func (r *Repository) ListActiveRiskListEntries(ctx context.Context) ([]securitydomain.RiskListEntry, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+riskListColumns+` FROM risk_lists
WHERE expires_at IS NULL OR expires_at > NOW()`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectRiskListEntries(rows)
}

// FindRiskListEntriesFor 某些对象的现行名单（详情页展示用）
func (r *Repository) FindRiskListEntriesFor(ctx context.Context, appID *int64, pairs map[string][]string) ([]securitydomain.RiskListEntry, error) {
	clauses := make([]string, 0, len(pairs))
	args := []any{riskAppIDArg(appID)}
	idx := 2
	for listType, values := range pairs {
		filtered := make([]string, 0, len(values))
		for _, v := range values {
			if strings.TrimSpace(v) != "" {
				filtered = append(filtered, v)
			}
		}
		if len(filtered) == 0 {
			continue
		}
		clauses = append(clauses, fmt.Sprintf("(list_type = $%d AND value = ANY($%d))", idx, idx+1))
		args = append(args, listType, filtered)
		idx += 2
	}
	if len(clauses) == 0 {
		return []securitydomain.RiskListEntry{}, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT `+riskListColumns+` FROM risk_lists
WHERE (expires_at IS NULL OR expires_at > NOW())
  AND ($1::bigint IS NULL OR app_id IS NULL OR app_id = $1)
  AND (`+strings.Join(clauses, " OR ")+`)
ORDER BY kind, list_type`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectRiskListEntries(rows)
}

// BumpRiskListHits 累加名单命中计数
func (r *Repository) BumpRiskListHits(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := r.pool.Exec(ctx, `UPDATE risk_lists SET hit_count = hit_count + 1, last_hit_at = NOW() WHERE id = ANY($1)`, ids)
	return err
}

// PurgeExpiredRiskLists 清掉过期很久的名单（保留一段时间供查证）
func (r *Repository) PurgeExpiredRiskLists(ctx context.Context, before time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM risk_lists WHERE expires_at IS NOT NULL AND expires_at < $1`, before)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func scanRiskListEntry(row rowScanner, inserted *bool) (*securitydomain.RiskListEntry, error) {
	var item securitydomain.RiskListEntry
	dest := []any{
		&item.ID, &item.AppID, &item.ListType, &item.Value, &item.Kind, &item.Scenes, &item.Reason, &item.Source,
		&item.AssessmentID, &item.ExpiresAt, &item.HitCount, &item.LastHitAt, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt,
	}
	if inserted != nil {
		dest = append(dest, inserted)
	}
	if err := row.Scan(dest...); err != nil {
		return nil, normalizeNotFound(err)
	}
	if item.Scenes == nil {
		item.Scenes = []string{}
	}
	return &item, nil
}

func collectRiskListEntries(rows rowsScanner) ([]securitydomain.RiskListEntry, error) {
	items := make([]securitydomain.RiskListEntry, 0, 32)
	for rows.Next() {
		item, err := scanRiskListEntry(rows, nil)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

// ════════════════════════════════════════════════════════════
//  账号画像
// ════════════════════════════════════════════════════════════

// ListAccountRiskProfiles 按「应用 + 账号」聚合评估记录。
// 只登录过的账号有 account，已登录场景（抽奖、签到……）只有 user_id：
// 两者都归到一个主体键上，否则同一个人在画像里会分裂成两行。
func (r *Repository) ListAccountRiskProfiles(ctx context.Context, query securitydomain.AccountQuery) ([]securitydomain.AccountRiskProfile, int64, error) {
	page, limit := normalizePage(query.Page, query.PageSize)
	where := []string{"(a.account <> '' OR a.user_id IS NOT NULL)"}
	args := make([]any, 0, 6)
	idx := 1
	add := func(clause string, value any) {
		where = append(where, fmt.Sprintf(clause, idx))
		args = append(args, value)
		idx++
	}
	if query.AppID != nil && *query.AppID > 0 {
		add("a.app_id = $%d", *query.AppID)
	}
	if query.Start != nil {
		add("a.created_at >= $%d", *query.Start)
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		where = append(where, fmt.Sprintf("(a.account ILIKE $%d OR a.user_id::text = $%d)", idx, idx+1))
		args = append(args, "%"+keyword+"%", keyword)
		idx += 2
	}
	having := ""
	if query.OnlyRisk {
		having = " HAVING MAX(a.total_score) > 20 OR COUNT(*) FILTER (WHERE a.action <> 'pass') > 0"
	}
	base := `
FROM risk_assessments a
WHERE ` + strings.Join(where, " AND ") + `
GROUP BY a.app_id, COALESCE(NULLIF(a.account, ''), '#' || a.user_id::text)` + having

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM (SELECT 1 `+base+`) t`, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []securitydomain.AccountRiskProfile{}, 0, nil
	}
	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`
SELECT a.app_id,
       COALESCE(MAX(app.name), ''),
       COALESCE(MAX(NULLIF(a.account, '')), ''),
       MAX(a.user_id),
       COUNT(*),
       COUNT(*) FILTER (WHERE a.action IN ('block','ban')),
       COUNT(*) FILTER (WHERE a.action IN ('captcha','review')),
       COUNT(*) FILTER (WHERE a.risk_level IN ('high','critical')),
       COALESCE(AVG(a.total_score), 0),
       COALESCE(MAX(a.total_score), 0),
       COUNT(DISTINCT a.ip) FILTER (WHERE a.ip <> ''),
       COUNT(DISTINCT a.device_id) FILTER (WHERE a.device_id <> ''),
       (ARRAY_AGG(a.risk_level ORDER BY a.created_at DESC))[1],
       (ARRAY_AGG(a.scene ORDER BY a.created_at DESC))[1],
       MAX(a.created_at)
%s
ORDER BY MAX(a.total_score) DESC, MAX(a.created_at) DESC
LIMIT $%d OFFSET $%d`, strings.Replace(base, "FROM risk_assessments a\n", "FROM risk_assessments a\nLEFT JOIN apps app ON app.id = a.app_id\n", 1), idx, idx+1), args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]securitydomain.AccountRiskProfile, 0, limit)
	for rows.Next() {
		var p securitydomain.AccountRiskProfile
		if err := rows.Scan(&p.AppID, &p.AppName, &p.Account, &p.UserID, &p.TotalAssessments, &p.Blocked, &p.Challenged,
			&p.HighRisk, &p.AvgScore, &p.MaxScore, &p.DistinctIPs, &p.DistinctDevices, &p.LastLevel, &p.LastScene, &p.LastSeenAt); err != nil {
			return nil, 0, err
		}
		items = append(items, p)
	}
	return items, total, rows.Err()
}

// AccountAssessmentFilter 账号详情的过滤：账号名或用户 ID 任一匹配
func accountSubjectClause(appID *int64, account string, userID *int64) (string, []any) {
	where := make([]string, 0, 2)
	args := make([]any, 0, 3)
	subject := make([]string, 0, 2)
	idx := 1
	if account != "" {
		subject = append(subject, fmt.Sprintf("a.account = $%d", idx))
		args = append(args, account)
		idx++
	}
	if userID != nil && *userID > 0 {
		subject = append(subject, fmt.Sprintf("a.user_id = $%d", idx))
		args = append(args, *userID)
		idx++
	}
	if len(subject) == 0 {
		return "FALSE", args
	}
	where = append(where, "("+strings.Join(subject, " OR ")+")")
	if appID != nil && *appID > 0 {
		where = append(where, fmt.Sprintf("a.app_id = $%d", idx))
		args = append(args, *appID)
	}
	return strings.Join(where, " AND "), args
}

// GetAccountRiskProfile 单个账号的聚合画像
func (r *Repository) GetAccountRiskProfile(ctx context.Context, appID *int64, account string, userID *int64) (*securitydomain.AccountRiskProfile, error) {
	clause, args := accountSubjectClause(appID, account, userID)
	var p securitydomain.AccountRiskProfile
	err := r.pool.QueryRow(ctx, `
SELECT MAX(a.app_id), COALESCE(MAX(app.name), ''), COALESCE(MAX(NULLIF(a.account, '')), ''), MAX(a.user_id),
       COUNT(*),
       COUNT(*) FILTER (WHERE a.action IN ('block','ban')),
       COUNT(*) FILTER (WHERE a.action IN ('captcha','review')),
       COUNT(*) FILTER (WHERE a.risk_level IN ('high','critical')),
       COALESCE(AVG(a.total_score), 0), COALESCE(MAX(a.total_score), 0),
       COUNT(DISTINCT a.ip) FILTER (WHERE a.ip <> ''),
       COUNT(DISTINCT a.device_id) FILTER (WHERE a.device_id <> ''),
       COALESCE((ARRAY_AGG(a.risk_level ORDER BY a.created_at DESC))[1], ''),
       COALESCE((ARRAY_AGG(a.scene ORDER BY a.created_at DESC))[1], ''),
       MAX(a.created_at)
FROM risk_assessments a LEFT JOIN apps app ON app.id = a.app_id
WHERE `+clause, args...).Scan(&p.AppID, &p.AppName, &p.Account, &p.UserID, &p.TotalAssessments, &p.Blocked, &p.Challenged,
		&p.HighRisk, &p.AvgScore, &p.MaxScore, &p.DistinctIPs, &p.DistinctDevices, &p.LastLevel, &p.LastScene, &p.LastSeenAt)
	if err != nil {
		return nil, err
	}
	if p.Account == "" {
		p.Account = account
	}
	if p.UserID == nil {
		p.UserID = userID
	}
	if p.AppID == nil {
		p.AppID = appID
	}
	return &p, nil
}

// ListAccountAssessments 账号最近的评估记录
func (r *Repository) ListAccountAssessments(ctx context.Context, appID *int64, account string, userID *int64, limit int) ([]securitydomain.RiskAssessment, error) {
	clause, args := accountSubjectClause(appID, account, userID)
	args = append(args, limit)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`SELECT %s %s WHERE %s ORDER BY a.created_at DESC, a.id DESC LIMIT $%d`,
		riskAssessmentColumns, riskAssessmentFrom, clause, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectRiskAssessments(rows)
}

// ListAccountPeers 账号用过的 IP 与设备
func (r *Repository) ListAccountPeers(ctx context.Context, appID *int64, account string, userID *int64, limit int) ([]string, []string, error) {
	clause, args := accountSubjectClause(appID, account, userID)
	args = append(args, limit)
	n := len(args)
	ips, err := r.distinctColumn(ctx, fmt.Sprintf(`SELECT ip FROM (SELECT a.ip, MAX(a.created_at) AS at FROM risk_assessments a
WHERE %s AND a.ip <> '' GROUP BY a.ip ORDER BY at DESC LIMIT $%d) t`, clause, n), args...)
	if err != nil {
		return nil, nil, err
	}
	devices, err := r.distinctColumn(ctx, fmt.Sprintf(`SELECT device_id FROM (SELECT a.device_id, MAX(a.created_at) AS at FROM risk_assessments a
WHERE %s AND a.device_id <> '' GROUP BY a.device_id ORDER BY at DESC LIMIT $%d) t`, clause, n), args...)
	if err != nil {
		return ips, nil, err
	}
	return ips, devices, nil
}

// AccountDimensionScores 账号近 30 天在各维度上累计的得分
func (r *Repository) AccountDimensionScores(ctx context.Context, appID *int64, account string, userID *int64) (map[string]int, error) {
	clause, args := accountSubjectClause(appID, account, userID)
	rows, err := r.pool.Query(ctx, `
SELECT d.key, COALESCE(SUM(d.value::int), 0)
FROM risk_assessments a
CROSS JOIN LATERAL jsonb_each_text(CASE WHEN jsonb_typeof(a.dimension_scores) = 'object' THEN a.dimension_scores ELSE '{}'::jsonb END) AS d(key, value)
WHERE `+clause+` AND a.created_at >= NOW() - INTERVAL '30 days'
GROUP BY d.key`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var key string
		var value int
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		out[key] = value
	}
	return out, rows.Err()
}

// RiskUserFacts 评估时需要的用户事实：注册时间
func (r *Repository) RiskUserFacts(ctx context.Context, appID int64, userID int64, account string) (int64, *time.Time, error) {
	var id int64
	var createdAt time.Time
	var err error
	switch {
	case userID > 0:
		err = r.pool.QueryRow(ctx, `SELECT id, created_at FROM users WHERE id = $1`, userID).Scan(&id, &createdAt)
	case appID > 0 && account != "":
		err = r.pool.QueryRow(ctx, `SELECT id, created_at FROM users WHERE appid = $1 AND account = $2`, appID, account).Scan(&id, &createdAt)
	default:
		return 0, nil, nil
	}
	if err != nil {
		if normalizeNotFound(err) == nil {
			return 0, nil, nil
		}
		return 0, nil, err
	}
	return id, &createdAt, nil
}

// GetDeviceIDByFingerprintID 设备档案主键 → 设备号
func (r *Repository) GetDeviceIDByFingerprintID(ctx context.Context, id int64) (string, error) {
	var deviceID string
	err := r.pool.QueryRow(ctx, `SELECT device_id FROM device_fingerprints WHERE id = $1`, id).Scan(&deviceID)
	return deviceID, normalizeNotFound(err)
}
