package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	securitydomain "aegis/internal/domain/security"

	"github.com/jackc/pgx/v5"
)

// ════════════════════════════════════════════════════════════
//  风险规则 CRUD
// ════════════════════════════════════════════════════════════

// riskRuleColumns 规则查询列，顺序与 scanRiskRule 一一对应。
const riskRuleColumns = `id, name, description, scene, condition_type, condition_data, score, is_active, priority,
       hit_count, last_hit_at, created_by, updated_by, created_at, updated_at, app_id, mode, decision, template_key`

// RiskScopeFilter 规则 / 策略 / 名单的作用域过滤。
// AppID 为 nil 不过滤；指向 0 只看全平台；指向 N 看该应用的 + 全平台的（评估时实际生效的集合）。
type RiskScopeFilter struct {
	Scene string
	AppID *int64
}

func (f RiskScopeFilter) where(startIdx int) (string, []any) {
	where := make([]string, 0, 2)
	args := make([]any, 0, 2)
	idx := startIdx
	if f.Scene != "" {
		where = append(where, fmt.Sprintf("(scene = $%d OR scene = '*')", idx))
		args = append(args, f.Scene)
		idx++
	}
	if f.AppID != nil {
		if *f.AppID <= 0 {
			where = append(where, "app_id IS NULL")
		} else {
			where = append(where, fmt.Sprintf("(app_id IS NULL OR app_id = $%d)", idx))
			args = append(args, *f.AppID)
		}
	}
	if len(where) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(where, " AND "), args
}

func riskAppIDArg(appID *int64) any {
	if appID == nil || *appID <= 0 {
		return nil
	}
	return *appID
}

// CreateRiskRule 创建风险规则
func (r *Repository) CreateRiskRule(ctx context.Context, input securitydomain.CreateRiskRuleInput, createdBy int64) (*securitydomain.RiskRule, error) {
	condJSON, err := json.Marshal(input.ConditionData)
	if err != nil {
		return nil, fmt.Errorf("marshal condition_data: %w", err)
	}
	isActive := true
	if input.IsActive != nil {
		isActive = *input.IsActive
	}
	mode := input.Mode
	if mode == "" {
		mode = securitydomain.RuleModeEnforce
	}
	query := `INSERT INTO risk_rules (name, description, scene, condition_type, condition_data, score, is_active, priority, created_by,
    app_id, mode, decision, template_key, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, NOW(), NOW())
RETURNING ` + riskRuleColumns
	return scanRiskRule(r.pool.QueryRow(ctx, query,
		input.Name, input.Description, input.Scene, input.ConditionType,
		condJSON, input.Score, isActive, input.Priority, createdBy,
		riskAppIDArg(input.AppID), mode, input.Decision, input.TemplateKey,
	))
}

// ListRiskRules 列出风险规则（按场景与作用域过滤；场景过滤包含「全部场景」规则）
func (r *Repository) ListRiskRules(ctx context.Context, filter RiskScopeFilter) ([]securitydomain.RiskRule, error) {
	where, args := filter.where(1)
	query := `SELECT ` + riskRuleColumns + ` FROM risk_rules` + where + ` ORDER BY scene ASC, priority ASC, id ASC`
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectRiskRules(rows)
}

// GetRiskRule 按 ID 查询单条规则
func (r *Repository) GetRiskRule(ctx context.Context, id int64) (*securitydomain.RiskRule, error) {
	return scanRiskRule(r.pool.QueryRow(ctx, `SELECT `+riskRuleColumns+` FROM risk_rules WHERE id = $1`, id))
}

// UpdateRiskRule 更新风险规则（可选字段）
func (r *Repository) UpdateRiskRule(ctx context.Context, id int64, input securitydomain.UpdateRiskRuleInput) error {
	sets := make([]string, 0, 9)
	args := make([]any, 0, 10)
	idx := 1
	add := func(column string, value any) {
		sets = append(sets, fmt.Sprintf("%s = $%d", column, idx))
		args = append(args, value)
		idx++
	}

	if input.Name != nil {
		add("name", *input.Name)
	}
	if input.Description != nil {
		add("description", *input.Description)
	}
	if input.Scene != nil {
		add("scene", *input.Scene)
	}
	if input.ConditionType != nil {
		add("condition_type", *input.ConditionType)
	}
	if input.ConditionData != nil {
		condJSON, err := json.Marshal(*input.ConditionData)
		if err != nil {
			return fmt.Errorf("marshal condition_data: %w", err)
		}
		add("condition_data", condJSON)
	}
	if input.Score != nil {
		add("score", *input.Score)
	}
	if input.IsActive != nil {
		add("is_active", *input.IsActive)
	}
	if input.Priority != nil {
		add("priority", *input.Priority)
	}
	if input.Mode != nil {
		add("mode", *input.Mode)
	}
	if input.Decision != nil {
		add("decision", *input.Decision)
	}
	if input.AppID != nil {
		add("app_id", riskAppIDArg(input.AppID))
	}
	if input.UpdatedBy != nil {
		add("updated_by", *input.UpdatedBy)
	}
	if len(sets) == 0 {
		return nil
	}
	sets = append(sets, "updated_at = NOW()")
	args = append(args, id)
	query := fmt.Sprintf("UPDATE risk_rules SET %s WHERE id = $%d", strings.Join(sets, ", "), idx)
	_, err := r.pool.Exec(ctx, query, args...)
	return err
}

// DeleteRiskRule 删除风险规则
func (r *Repository) DeleteRiskRule(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM risk_rules WHERE id = $1`, id)
	return err
}

// ListActiveRiskRules 全部启用中的规则（按优先级排序）。评估引擎把它整体装进内存快照，
// 每次评估不再查库。
func (r *Repository) ListActiveRiskRules(ctx context.Context) ([]securitydomain.RiskRule, error) {
	query := `SELECT ` + riskRuleColumns + `
FROM risk_rules WHERE is_active = TRUE ORDER BY priority ASC, id ASC`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectRiskRules(rows)
}

// ListRiskRuleTemplateKeys 已安装的模板规则键（按作用域）
func (r *Repository) ListRiskRuleTemplateKeys(ctx context.Context, appID *int64) (map[string]bool, error) {
	rows, err := r.pool.Query(ctx, `SELECT template_key FROM risk_rules
WHERE template_key <> '' AND COALESCE(app_id, 0) = COALESCE($1::bigint, 0)`, riskAppIDArg(appID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		out[key] = true
	}
	return out, rows.Err()
}

// BumpRuleHits 批量累加规则命中计数。
// 一次评估可能命中多条规则，逐条 UPDATE 会产生 N 次往返；
// 用 ANY($1) 一次改完，且天然按主键顺序加锁，不会与并发评估互相死锁。
func (r *Repository) BumpRuleHits(ctx context.Context, ruleIDs []int64) error {
	if len(ruleIDs) == 0 {
		return nil
	}
	_, err := r.pool.Exec(ctx,
		`UPDATE risk_rules SET hit_count = hit_count + 1, last_hit_at = NOW() WHERE id = ANY($1)`, ruleIDs)
	return err
}

// GetRuleHitStat 统计一条规则在区间内的命中效果。
// matched_rules 是 JSONB 数组，用 @> 做包含判断即可走 GIN 索引。
func (r *Repository) GetRuleHitStat(ctx context.Context, ruleID int64, start, end time.Time) (securitydomain.RuleHitStat, error) {
	stat := securitydomain.RuleHitStat{RuleID: ruleID}
	// ScoreSum 统计的是**这条规则**贡献的分数，不是命中请求的总分 ——
	// 后者会把其他规则的贡献一并算进来，用它衡量单条规则的影响是错的。
	err := r.pool.QueryRow(ctx, `
SELECT COALESCE(COUNT(*), 0),
       COALESCE(COUNT(*) FILTER (WHERE a.action IN ('block','ban')), 0),
       COALESCE(SUM((m.rule ->> 'score')::int), 0),
       MAX(a.created_at)
FROM risk_assessments a
CROSS JOIN LATERAL jsonb_array_elements(`+matchedRulesArray+`) AS m(rule)
WHERE a.created_at BETWEEN $2 AND $3
  AND (m.rule ->> 'ruleId')::bigint = $1`,
		ruleID, start, end).Scan(&stat.Hits, &stat.Blocked, &stat.ScoreSum, &stat.LastHitAt)
	if err != nil {
		return stat, err
	}
	if err := r.pool.QueryRow(ctx,
		`SELECT name, scene, condition_type, score, is_active FROM risk_rules WHERE id = $1`, ruleID,
	).Scan(&stat.RuleName, &stat.Scene, &stat.ConditionType, &stat.Score, &stat.IsActive); err != nil {
		return stat, normalizeNotFound(err)
	}
	return stat, nil
}

// GetRuleHitSeries 一条规则的命中时间序列
func (r *Repository) GetRuleHitSeries(ctx context.Context, ruleID int64, start, end time.Time, bucket string) ([]securitydomain.RuleHitPoint, error) {
	unit := normalizeBucket(bucket)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`
SELECT buckets.slot, COALESCE(hits.total, 0)
FROM generate_series(date_trunc('%[1]s', $2::timestamptz), date_trunc('%[1]s', $3::timestamptz), interval '1 %[1]s') AS buckets(slot)
LEFT JOIN (
    SELECT date_trunc('%[1]s', created_at) AS slot, COUNT(*) AS total
    FROM risk_assessments
    WHERE created_at BETWEEN $2 AND $3 AND matched_rules @> $1::jsonb
    GROUP BY 1
) AS hits ON hits.slot = buckets.slot
ORDER BY buckets.slot ASC`, unit), ruleMatchFilter(ruleID), start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	points := make([]securitydomain.RuleHitPoint, 0, 64)
	for rows.Next() {
		var point securitydomain.RuleHitPoint
		if err := rows.Scan(&point.Time, &point.Count); err != nil {
			return nil, err
		}
		points = append(points, point)
	}
	return points, rows.Err()
}

// matchedRulesArray 把 matched_rules 收敛成 JSONB 数组之后再展开。
//
// 这一层防御不是多余的：旧写入路径对一个 nil 切片调 json.Marshal 得到的是
// **标量 `null`**（不是 `[]`），而 jsonb_array_elements 展开标量会直接抛
// 22023 cannot extract elements from a scalar —— 一条历史脏数据就能让整个
// 大盘 500。列上的 DEFAULT '[]' 挡不住显式写入的 null。
const matchedRulesArray = `CASE WHEN jsonb_typeof(a.matched_rules) = 'array' THEN a.matched_rules ELSE '[]'::jsonb END`

// ruleMatchFilter 构造 `matched_rules @> '[{"ruleId":N}]'` 的右操作数
func ruleMatchFilter(ruleID int64) string {
	return fmt.Sprintf(`[{"ruleId":%d}]`, ruleID)
}

// normalizeBucket 只允许 hour / day 两种粒度。
// 这个值会被拼进 SQL（date_trunc 的单位不能参数化），所以必须在这里收敛成白名单。
func normalizeBucket(bucket string) string {
	if strings.EqualFold(strings.TrimSpace(bucket), "day") {
		return "day"
	}
	return "hour"
}

// ════════════════════════════════════════════════════════════
//  评估记录
// ════════════════════════════════════════════════════════════

// riskAssessmentColumns 评估记录的查询列，顺序与 scanRiskAssessment 一一对应。
// review_result 建表时可空，而域类型里是非指针 string —— 未复核的记录直接 Scan 会报
// "cannot scan NULL into *string"。所有查询共用这份清单，避免有人新加查询时漏掉 COALESCE。
const riskAssessmentColumns = `a.id, a.scene, a.app_id, a.user_id, a.identity_id, a.account, a.ip, a.device_id,
       a.user_agent, a.country, a.total_score, a.risk_level, a.matched_rules, a.eval_context, a.latency_ms,
       a.action, a.action_detail, a.reviewed, a.reviewer_id, COALESCE(adm.account, ''),
       COALESCE(a.review_result, ''), a.review_comment, a.reviewed_at, a.created_at,
       a.dimension_scores, a.list_hits, a.decision_by, a.enforcement`

const riskAssessmentFrom = `FROM risk_assessments a LEFT JOIN admin_accounts adm ON adm.id = a.reviewer_id`

// normalizeAssessmentShape 把两个 JSONB 字段的形状定死在写入入口。
//
// nil 切片 / nil map 的 json.Marshal 结果是**标量 `null`**，不是 `[]` / `{}`；
// 列上的 DEFAULT 挡不住显式写入。落库之后，任何
// jsonb_array_elements(matched_rules) 都会在这一行上抛
// 22023 cannot extract elements from a scalar —— 一条脏数据足以让整个大盘 500。
func normalizeAssessmentShape(a *securitydomain.RiskAssessment) {
	if a.MatchedRules == nil {
		a.MatchedRules = []securitydomain.MatchedRule{}
	}
	if a.EvalContext == nil {
		a.EvalContext = map[string]any{}
	}
	if a.DimensionScores == nil {
		a.DimensionScores = map[string]int{}
	}
	if a.ListHits == nil {
		a.ListHits = []securitydomain.ListHit{}
	}
	if a.Enforcement == nil {
		a.Enforcement = []securitydomain.Enforcement{}
	}
}

// CreateRiskAssessment 创建风险评估记录
func (r *Repository) CreateRiskAssessment(ctx context.Context, a securitydomain.RiskAssessment) (*securitydomain.RiskAssessment, error) {
	normalizeAssessmentShape(&a)
	rulesJSON, err := json.Marshal(a.MatchedRules)
	if err != nil {
		return nil, fmt.Errorf("marshal matched_rules: %w", err)
	}
	contextJSON, err := json.Marshal(a.EvalContext)
	if err != nil {
		return nil, fmt.Errorf("marshal eval_context: %w", err)
	}
	dimJSON, _ := json.Marshal(a.DimensionScores)
	listJSON, _ := json.Marshal(a.ListHits)
	enforceJSON, _ := json.Marshal(a.Enforcement)
	var id int64
	err = r.pool.QueryRow(ctx, `INSERT INTO risk_assessments
    (scene, app_id, user_id, identity_id, account, ip, device_id, user_agent, country,
     total_score, risk_level, matched_rules, eval_context, latency_ms, action, action_detail,
     dimension_scores, list_hits, decision_by, enforcement, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, NOW())
RETURNING id`,
		a.Scene, a.AppID, a.UserID, a.IdentityID, truncateColumn(a.Account, 190),
		a.IP, truncateColumn(a.DeviceID, 256), a.UserAgent, truncateColumn(a.Country, 64),
		a.TotalScore, a.RiskLevel, rulesJSON, contextJSON, a.LatencyMS, a.Action, a.ActionDetail,
		dimJSON, listJSON, a.DecisionBy, enforceJSON,
	).Scan(&id)
	if err != nil {
		return nil, err
	}
	a.ID = id
	return &a, nil
}

// buildAssessmentFilter 把查询条件翻译成 WHERE 子句。
// 列表页、详情页的关联查询、规则详情的最近命中共用它 ——
// 各写一套过滤条件是「同一个筛选在两个页面结果不一致」的来源。
func buildAssessmentFilter(query securitydomain.AssessmentQuery, startIdx int) (string, []any) {
	where := make([]string, 0, 12)
	args := make([]any, 0, 12)
	idx := startIdx
	add := func(clause string, value any) {
		where = append(where, fmt.Sprintf(clause, idx))
		args = append(args, value)
		idx++
	}

	if query.AppID != nil {
		add("a.app_id = $%d", *query.AppID)
	}
	if query.UserID != nil {
		add("a.user_id = $%d", *query.UserID)
	}
	if query.DecisionBy != "" {
		add("a.decision_by = $%d", query.DecisionBy)
	}
	if query.Dimension != "" {
		add("COALESCE((a.dimension_scores ->> $%d)::int, 0) > 0", query.Dimension)
	}
	if query.Scene != "" {
		add("a.scene = $%d", query.Scene)
	}
	if query.RiskLevel != "" {
		add("a.risk_level = $%d", query.RiskLevel)
	}
	if query.Action != "" {
		add("a.action = $%d", query.Action)
	}
	if query.IP != "" {
		add("a.ip = $%d", query.IP)
	}
	if query.DeviceID != "" {
		add("a.device_id = $%d", query.DeviceID)
	}
	if query.Account != "" {
		add("a.account = $%d", query.Account)
	}
	if query.RuleID > 0 {
		add("a.matched_rules @> $%d::jsonb", ruleMatchFilter(query.RuleID))
	}
	if query.Reviewed != nil {
		add("a.reviewed = $%d", *query.Reviewed)
	}
	if query.MinScore != nil {
		add("a.total_score >= $%d", *query.MinScore)
	}
	if query.MaxScore != nil {
		add("a.total_score <= $%d", *query.MaxScore)
	}
	if query.Start != nil {
		add("a.created_at >= $%d", *query.Start)
	}
	if query.End != nil {
		add("a.created_at <= $%d", *query.End)
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		// 一个搜索框同时找 IP / 设备 / 账号：三者在排查时是同一件事的三个入口，
		// 让运维先想清楚"这串字符属于哪一类"是多余的负担。
		where = append(where, fmt.Sprintf("(a.ip ILIKE $%d OR a.device_id ILIKE $%d OR a.account ILIKE $%d)", idx, idx, idx))
		args = append(args, "%"+keyword+"%")
		idx++
	}

	if len(where) == 0 {
		return "", args
	}
	return "WHERE " + strings.Join(where, " AND "), args
}

// ListRiskAssessments 分页查询评估记录
func (r *Repository) ListRiskAssessments(ctx context.Context, query securitydomain.AssessmentQuery) ([]securitydomain.RiskAssessment, int64, error) {
	page, limit := normalizePage(query.Page, query.PageSize)
	whereClause, args := buildAssessmentFilter(query, 1)

	var total int64
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM risk_assessments a %s", whereClause)
	if err := r.pool.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []securitydomain.RiskAssessment{}, 0, nil
	}

	dataArgs := append(append([]any{}, args...), limit, (page-1)*limit)
	dataQuery := fmt.Sprintf(`SELECT %s %s %s ORDER BY a.created_at DESC, a.id DESC LIMIT $%d OFFSET $%d`,
		riskAssessmentColumns, riskAssessmentFrom, whereClause, len(args)+1, len(args)+2)

	rows, err := r.pool.Query(ctx, dataQuery, dataArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items, err := collectRiskAssessments(rows)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// GetRiskAssessment 获取单条评估记录
func (r *Repository) GetRiskAssessment(ctx context.Context, id int64) (*securitydomain.RiskAssessment, error) {
	query := fmt.Sprintf(`SELECT %s %s WHERE a.id = $1`, riskAssessmentColumns, riskAssessmentFrom)
	return scanRiskAssessment(r.pool.QueryRow(ctx, query, id))
}

// ReviewRiskAssessment 复核评估记录
func (r *Repository) ReviewRiskAssessment(ctx context.Context, id, reviewerID int64, result, comment string) error {
	tag, err := r.pool.Exec(ctx, `UPDATE risk_assessments
SET reviewed = TRUE, reviewer_id = $2, review_result = $3, review_comment = $4, reviewed_at = NOW()
WHERE id = $1 AND NOT reviewed`, id, reviewerID, result, comment)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("评估记录不存在或已复核")
	}
	return nil
}

// AppendAssessmentEnforcement 往一条评估记录追加处置留痕（复核驳回、人工处置）
func (r *Repository) AppendAssessmentEnforcement(ctx context.Context, id int64, items []securitydomain.Enforcement) error {
	if len(items) == 0 {
		return nil
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return err
	}
	_, err = r.pool.Exec(ctx, `UPDATE risk_assessments
SET enforcement = CASE WHEN jsonb_typeof(enforcement) = 'array' THEN enforcement ELSE '[]'::jsonb END || $2::jsonb
WHERE id = $1`, id, raw)
	return err
}

// PurgeRiskAssessments 清理指定时刻之前的评估记录，返回删除行数
func (r *Repository) PurgeRiskAssessments(ctx context.Context, before time.Time) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM risk_assessments WHERE created_at < $1`, before)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// GetEntitySummary 聚合某个实体（ip / device / account）的画像。
// 详情页靠它回答「这个 IP 到底闹了多久、碰过多少账号」。
func (r *Repository) GetEntitySummary(ctx context.Context, kind, value string) (securitydomain.RiskEntitySummary, error) {
	var column string
	switch kind {
	case "ip":
		column = "ip"
	case "device":
		column = "device_id"
	case "account":
		column = "account"
	case "user":
		column = "user_id::text"
	default:
		return securitydomain.RiskEntitySummary{}, fmt.Errorf("未知实体类型：%s", kind)
	}

	var summary securitydomain.RiskEntitySummary
	err := r.pool.QueryRow(ctx, fmt.Sprintf(`
SELECT COUNT(*),
       COUNT(*) FILTER (WHERE action IN ('block','ban')),
       COALESCE(AVG(total_score), 0),
       COALESCE(MAX(total_score), 0),
       COUNT(DISTINCT account) FILTER (WHERE account <> ''),
       COUNT(DISTINCT device_id) FILTER (WHERE device_id <> ''),
       COUNT(DISTINCT ip) FILTER (WHERE ip <> ''),
       MIN(created_at), MAX(created_at)
FROM risk_assessments WHERE %s = $1`, column), value).Scan(
		&summary.TotalAssessments, &summary.Blocked, &summary.AvgScore, &summary.MaxScore,
		&summary.DistinctAccounts, &summary.DistinctDevices, &summary.DistinctIPs,
		&summary.FirstSeenAt, &summary.LastSeenAt,
	)
	return summary, err
}

// ListIPPeers 列出与某 IP 共现的设备与账号
func (r *Repository) ListIPPeers(ctx context.Context, ip string, limit int) ([]securitydomain.DeviceFingerprint, []string, error) {
	deviceRows, err := r.pool.Query(ctx, `SELECT `+deviceFingerprintColumns+`
FROM device_fingerprints
WHERE device_id IN (SELECT DISTINCT device_id FROM risk_assessments WHERE ip = $1 AND device_id <> '' LIMIT $2)
ORDER BY last_seen_at DESC`, ip, limit)
	if err != nil {
		return nil, nil, err
	}
	defer deviceRows.Close()
	devices, err := collectDeviceFingerprints(deviceRows)
	if err != nil {
		return nil, nil, err
	}

	accounts, err := r.distinctColumn(ctx, `SELECT DISTINCT account FROM risk_assessments WHERE ip = $1 AND account <> '' ORDER BY account LIMIT $2`, ip, limit)
	if err != nil {
		return devices, nil, err
	}
	return devices, accounts, nil
}

// ListDevicePeers 列出某设备用过的 IP 与账号
func (r *Repository) ListDevicePeers(ctx context.Context, deviceID string, limit int) ([]string, []string, error) {
	ips, err := r.distinctColumn(ctx, `SELECT DISTINCT ip FROM risk_assessments WHERE device_id = $1 AND ip <> '' ORDER BY ip LIMIT $2`, deviceID, limit)
	if err != nil {
		return nil, nil, err
	}
	accounts, err := r.distinctColumn(ctx, `SELECT DISTINCT account FROM risk_assessments WHERE device_id = $1 AND account <> '' ORDER BY account LIMIT $2`, deviceID, limit)
	if err != nil {
		return ips, nil, err
	}
	return ips, accounts, nil
}

func (r *Repository) distinctColumn(ctx context.Context, query string, args ...any) ([]string, error) {
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]string, 0, 16)
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

// ════════════════════════════════════════════════════════════
//  设备指纹
// ════════════════════════════════════════════════════════════

const deviceFingerprintColumns = `id, device_id, user_id, app_id, fingerprint, risk_tag, last_ip, user_agent, note,
       first_seen_at, last_seen_at, seen_count`

// TouchDeviceFingerprint 记录一次设备出现。
//
// 与旧的 UpsertDeviceFingerprint 有两处关键差别：
//   - **不覆盖 risk_tag**。管理员刚把一台设备标成 blocked，下一个请求就把它写回
//     默认值，等于人工处置永远生效不了几秒。
//   - **user_id 只在原本为空时补写**。设备是物理实体，第一次绑定的账号才有归属意义，
//     后来者不断覆盖只会让这一列变成"最后一个用它登录的人"，失去查证价值。
func (r *Repository) TouchDeviceFingerprint(ctx context.Context, fp securitydomain.DeviceFingerprint) (*securitydomain.DeviceFingerprint, error) {
	fpJSON, err := json.Marshal(fp.Fingerprint)
	if err != nil {
		return nil, fmt.Errorf("marshal fingerprint: %w", err)
	}
	riskTag := fp.RiskTag
	if riskTag == "" {
		riskTag = securitydomain.TagNormal
	}
	query := `INSERT INTO device_fingerprints
    (device_id, user_id, app_id, fingerprint, risk_tag, last_ip, user_agent, first_seen_at, last_seen_at, seen_count)
VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), NOW(), 1)
ON CONFLICT (device_id) DO UPDATE SET
    fingerprint  = COALESCE(NULLIF(EXCLUDED.fingerprint, '{}'::jsonb), device_fingerprints.fingerprint),
    user_id      = COALESCE(device_fingerprints.user_id, EXCLUDED.user_id),
    app_id       = COALESCE(device_fingerprints.app_id, EXCLUDED.app_id),
    last_ip      = COALESCE(NULLIF(EXCLUDED.last_ip, ''), device_fingerprints.last_ip),
    user_agent   = COALESCE(NULLIF(EXCLUDED.user_agent, ''), device_fingerprints.user_agent),
    last_seen_at = NOW(),
    seen_count   = device_fingerprints.seen_count + 1
RETURNING ` + deviceFingerprintColumns
	return scanDeviceFingerprint(r.pool.QueryRow(ctx, query,
		fp.DeviceID, fp.UserID, fp.AppID, fpJSON, riskTag, fp.LastIP, fp.UserAgent,
	))
}

// UpsertDeviceFingerprint 保留旧名，语义等同 TouchDeviceFingerprint
func (r *Repository) UpsertDeviceFingerprint(ctx context.Context, fp securitydomain.DeviceFingerprint) (*securitydomain.DeviceFingerprint, error) {
	return r.TouchDeviceFingerprint(ctx, fp)
}

// GetDeviceFingerprint 按 device_id 查询设备指纹
func (r *Repository) GetDeviceFingerprint(ctx context.Context, deviceID string) (*securitydomain.DeviceFingerprint, error) {
	return scanDeviceFingerprint(r.pool.QueryRow(ctx,
		`SELECT `+deviceFingerprintColumns+` FROM device_fingerprints WHERE device_id = $1`, deviceID))
}

// ListDeviceFingerprints 分页查询设备。
// OnlyRisk 为 false 时返回全部设备 —— 只能看"可疑设备"意味着无法回答
// 「这个设备号到底有没有在档」这种最常见的查证需求。
func (r *Repository) ListDeviceFingerprints(ctx context.Context, query securitydomain.EntityQuery) ([]securitydomain.DeviceFingerprint, int64, error) {
	page, limit := normalizePage(query.Page, query.PageSize)
	where := make([]string, 0, 3)
	args := make([]any, 0, 3)
	idx := 1
	if query.AppID != nil && *query.AppID > 0 {
		where = append(where, fmt.Sprintf("app_id = $%d", idx))
		args = append(args, *query.AppID)
		idx++
	}
	if query.OnlyRisk {
		where = append(where, "risk_tag NOT IN ('normal','trusted')")
	}
	if query.Tag != "" {
		where = append(where, fmt.Sprintf("risk_tag = $%d", idx))
		args = append(args, query.Tag)
		idx++
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		where = append(where, fmt.Sprintf("(device_id ILIKE $%d OR last_ip ILIKE $%d OR user_agent ILIKE $%d)", idx, idx, idx))
		args = append(args, "%"+keyword+"%")
		idx++
	}
	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}

	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM device_fingerprints "+whereClause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []securitydomain.DeviceFingerprint{}, 0, nil
	}

	dataArgs := append(append([]any{}, args...), limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`SELECT %s FROM device_fingerprints %s
ORDER BY last_seen_at DESC LIMIT $%d OFFSET $%d`, deviceFingerprintColumns, whereClause, idx, idx+1), dataArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items, err := collectDeviceFingerprints(rows)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// UpdateDeviceRiskTag 更新设备风险标签
func (r *Repository) UpdateDeviceRiskTag(ctx context.Context, id int64, tag, note string) error {
	tag2, err := r.pool.Exec(ctx,
		`UPDATE device_fingerprints SET risk_tag = $2, note = COALESCE(NULLIF($3, ''), note) WHERE id = $1`, id, tag, note)
	if err != nil {
		return err
	}
	if tag2.RowsAffected() == 0 {
		return fmt.Errorf("设备记录不存在")
	}
	return nil
}

// SetDeviceRiskTagByDeviceID 按 device_id 更新标签（复核联动用）
func (r *Repository) SetDeviceRiskTagByDeviceID(ctx context.Context, deviceID, tag string) error {
	_, err := r.pool.Exec(ctx, `UPDATE device_fingerprints SET risk_tag = $2 WHERE device_id = $1`, deviceID, tag)
	return err
}

// ════════════════════════════════════════════════════════════
//  IP 风险库
// ════════════════════════════════════════════════════════════

const ipRiskColumns = `id, ip, risk_tag, risk_score, country, region, isp, asn, source, note,
       is_proxy, is_vpn, is_tor, is_datacenter, total_requests, total_blocks, first_seen_at, last_seen_at`

// UpsertIPRisk 写入情报源返回的 IP 画像。
//
// 关键修正：**空值不覆盖已有值，计数列完全不动**。旧实现无条件用传入结构覆盖，
// 于是一次只填了 IP 与分数的调用会把国家 / 运营商抹成空串、把累计请求数清零。
// 复核拒绝那条路径正是这么用的 —— 复核这个动作本身在销毁证据。
func (r *Repository) UpsertIPRisk(ctx context.Context, rec securitydomain.IPRiskRecord) (*securitydomain.IPRiskRecord, error) {
	riskTag := rec.RiskTag
	if riskTag == "" {
		riskTag = securitydomain.TagNormal
	}
	query := `INSERT INTO ip_risk_records
    (ip, risk_tag, risk_score, country, region, isp, asn, source, note,
     is_proxy, is_vpn, is_tor, is_datacenter, total_requests, total_blocks, first_seen_at, last_seen_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, 0, 0, NOW(), NOW())
ON CONFLICT (ip) DO UPDATE SET
    risk_tag      = EXCLUDED.risk_tag,
    risk_score    = EXCLUDED.risk_score,
    country       = COALESCE(NULLIF(EXCLUDED.country, ''), ip_risk_records.country),
    region        = COALESCE(NULLIF(EXCLUDED.region, ''), ip_risk_records.region),
    isp           = COALESCE(NULLIF(EXCLUDED.isp, ''), ip_risk_records.isp),
    asn           = COALESCE(NULLIF(EXCLUDED.asn, ''), ip_risk_records.asn),
    source        = COALESCE(NULLIF(EXCLUDED.source, ''), ip_risk_records.source),
    note          = COALESCE(NULLIF(EXCLUDED.note, ''), ip_risk_records.note),
    is_proxy      = EXCLUDED.is_proxy,
    is_vpn        = EXCLUDED.is_vpn,
    is_tor        = EXCLUDED.is_tor,
    is_datacenter = EXCLUDED.is_datacenter,
    last_seen_at  = NOW()
RETURNING ` + ipRiskColumns
	return scanIPRiskRecord(r.pool.QueryRow(ctx, query,
		rec.IP, riskTag, rec.RiskScore, rec.Country, rec.Region, rec.ISP, rec.ASN, rec.Source, rec.Note,
		rec.IsProxy, rec.IsVPN, rec.IsTor, rec.IsDatacenter,
	))
}

// TouchIPRiskCounters 累加 IP 的请求 / 拦截计数并补齐归属地。
// 这两个计数列此前从未被写过，「高风险 IP」列表上恒显示 0 —— 管理员因此
// 无从判断一个 IP 是偶发还是持续在打。
func (r *Repository) TouchIPRiskCounters(ctx context.Context, ip string, blocked bool, country, region, isp, asn string) error {
	blockDelta := 0
	if blocked {
		blockDelta = 1
	}
	_, err := r.pool.Exec(ctx, `INSERT INTO ip_risk_records
    (ip, risk_tag, country, region, isp, asn, total_requests, total_blocks, first_seen_at, last_seen_at)
VALUES ($1, 'normal', $2, $3, $4, $5, 1, $6, NOW(), NOW())
ON CONFLICT (ip) DO UPDATE SET
    country        = COALESCE(NULLIF(ip_risk_records.country, ''), EXCLUDED.country),
    region         = COALESCE(NULLIF(ip_risk_records.region, ''), EXCLUDED.region),
    isp            = COALESCE(NULLIF(ip_risk_records.isp, ''), EXCLUDED.isp),
    asn            = COALESCE(NULLIF(ip_risk_records.asn, ''), EXCLUDED.asn),
    total_requests = ip_risk_records.total_requests + 1,
    total_blocks   = ip_risk_records.total_blocks + $6,
    last_seen_at   = NOW()`,
		ip, truncateColumn(country, 64), truncateColumn(region, 128), truncateColumn(isp, 128), truncateColumn(asn, 64), blockDelta)
	return err
}

// GetIPRisk 按 IP 查询风险记录
func (r *Repository) GetIPRisk(ctx context.Context, ip string) (*securitydomain.IPRiskRecord, error) {
	return scanIPRiskRecord(r.pool.QueryRow(ctx, `SELECT `+ipRiskColumns+` FROM ip_risk_records WHERE ip = $1`, ip))
}

// ListIPRiskRecords 分页查询 IP 风险库
func (r *Repository) ListIPRiskRecords(ctx context.Context, query securitydomain.EntityQuery) ([]securitydomain.IPRiskRecord, int64, error) {
	page, limit := normalizePage(query.Page, query.PageSize)
	where := make([]string, 0, 3)
	args := make([]any, 0, 3)
	idx := 1
	if query.OnlyRisk {
		where = append(where, "risk_tag NOT IN ('normal','trusted')")
	}
	if query.Tag != "" {
		where = append(where, fmt.Sprintf("risk_tag = $%d", idx))
		args = append(args, query.Tag)
		idx++
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		where = append(where, fmt.Sprintf("(ip ILIKE $%d OR country ILIKE $%d OR isp ILIKE $%d OR asn ILIKE $%d)", idx, idx, idx, idx))
		args = append(args, "%"+keyword+"%")
		idx++
	}
	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}

	var total int64
	if err := r.pool.QueryRow(ctx, "SELECT COUNT(*) FROM ip_risk_records "+whereClause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []securitydomain.IPRiskRecord{}, 0, nil
	}

	dataArgs := append(append([]any{}, args...), limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`SELECT %s FROM ip_risk_records %s
ORDER BY risk_score DESC, last_seen_at DESC LIMIT $%d OFFSET $%d`, ipRiskColumns, whereClause, idx, idx+1), dataArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items, err := collectIPRiskRecords(rows)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// UpdateIPRiskTag 人工更新 IP 标签，返回被更新的 IP（供调用方失效缓存）
func (r *Repository) UpdateIPRiskTag(ctx context.Context, id int64, tag, source, note string) (string, error) {
	var ip string
	// 这里刻意不走 normalizeNotFound：ID 不存在时静默返回成功，
	// 控制台会提示"标记已更新"而实际什么都没发生。
	if err := r.pool.QueryRow(ctx, `UPDATE ip_risk_records
SET risk_tag = $2, source = $3, note = COALESCE(NULLIF($4, ''), note), last_seen_at = NOW()
WHERE id = $1 RETURNING ip`, id, tag, source, note).Scan(&ip); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("IP 风险记录不存在")
		}
		return "", err
	}
	return ip, nil
}

// SetIPRiskTag 按 IP 设置标签（复核联动用）。
// 记录不存在时插入一条最小档案，这样「复核拒绝了但库里查不到这个 IP」不会发生。
func (r *Repository) SetIPRiskTag(ctx context.Context, ip, tag, source, note string) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO ip_risk_records (ip, risk_tag, source, note, first_seen_at, last_seen_at)
VALUES ($1, $2, $3, $4, NOW(), NOW())
ON CONFLICT (ip) DO UPDATE SET
    risk_tag     = EXCLUDED.risk_tag,
    source       = EXCLUDED.source,
    note         = COALESCE(NULLIF(EXCLUDED.note, ''), ip_risk_records.note),
    last_seen_at = NOW()`, ip, tag, source, note)
	return err
}

// ResetIPRiskTagIf 名单撤销后，把仍停留在指定标签上的 IP 档案改回正常
func (r *Repository) ResetIPRiskTagIf(ctx context.Context, ip string, tags []string) error {
	_, err := r.pool.Exec(ctx, `UPDATE ip_risk_records SET risk_tag = 'normal' WHERE ip = $1 AND risk_tag = ANY($2)`, ip, tags)
	return err
}

// ResetDeviceRiskTagIf 名单撤销后，把仍停留在指定标签上的设备档案改回正常
func (r *Repository) ResetDeviceRiskTagIf(ctx context.Context, deviceID string, tags []string) error {
	_, err := r.pool.Exec(ctx, `UPDATE device_fingerprints SET risk_tag = 'normal' WHERE device_id = $1 AND risk_tag = ANY($2)`, deviceID, tags)
	return err
}

// ════════════════════════════════════════════════════════════
//  处置策略
// ════════════════════════════════════════════════════════════

const riskActionColumns = `id, scene, min_score, max_score, action, ban_duration, description, is_active, created_at,
       app_id, ban_targets, updated_at`

// CreateRiskAction 创建自动处置策略
func (r *Repository) CreateRiskAction(ctx context.Context, input securitydomain.CreateRiskActionInput) (*securitydomain.RiskAction, error) {
	isActive := true
	if input.IsActive != nil {
		isActive = *input.IsActive
	}
	targets := input.BanTargets
	if targets == nil {
		targets = []string{securitydomain.BanTargetAccount, securitydomain.BanTargetDevice}
	}
	query := `INSERT INTO risk_actions (scene, min_score, max_score, action, ban_duration, description, is_active, app_id, ban_targets, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), NOW())
RETURNING ` + riskActionColumns
	return scanRiskAction(r.pool.QueryRow(ctx, query,
		input.Scene, input.MinScore, input.MaxScore, input.Action, input.BanDuration, input.Description,
		isActive, riskAppIDArg(input.AppID), targets))
}

// ListRiskActions 查询处置策略（可按 scene 过滤）。
//
// 注意这里**不再过滤 is_active**：判定侧自己会跳过停用项，
// 而管理端需要看到停用的策略才能把它重新打开。旧实现在带 scene 时
// 隐式只返回启用中的，于是控制台上停用一条策略之后它就从列表里消失了。
func (r *Repository) ListRiskActions(ctx context.Context, filter RiskScopeFilter) ([]securitydomain.RiskAction, error) {
	where, args := filter.where(1)
	query := `SELECT ` + riskActionColumns + ` FROM risk_actions` + where + ` ORDER BY app_id ASC NULLS FIRST, scene ASC, min_score ASC`
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectRiskActions(rows)
}

// GetRiskAction 按 ID 查询处置策略
func (r *Repository) GetRiskAction(ctx context.Context, id int64) (*securitydomain.RiskAction, error) {
	return scanRiskAction(r.pool.QueryRow(ctx, `SELECT `+riskActionColumns+` FROM risk_actions WHERE id = $1`, id))
}

// UpdateRiskAction 更新处置策略
func (r *Repository) UpdateRiskAction(ctx context.Context, id int64, input securitydomain.UpdateRiskActionInput) error {
	sets := make([]string, 0, 6)
	args := make([]any, 0, 7)
	idx := 1
	add := func(column string, value any) {
		sets = append(sets, fmt.Sprintf("%s = $%d", column, idx))
		args = append(args, value)
		idx++
	}
	if input.MinScore != nil {
		add("min_score", *input.MinScore)
	}
	if input.MaxScore != nil {
		add("max_score", *input.MaxScore)
	}
	if input.Action != nil {
		add("action", *input.Action)
	}
	if input.BanDuration != nil {
		add("ban_duration", *input.BanDuration)
	}
	if input.Description != nil {
		add("description", *input.Description)
	}
	if input.IsActive != nil {
		add("is_active", *input.IsActive)
	}
	if input.BanTargets != nil {
		add("ban_targets", *input.BanTargets)
	}
	if len(sets) == 0 {
		return nil
	}
	sets = append(sets, "updated_at = NOW()")
	args = append(args, id)
	_, err := r.pool.Exec(ctx, fmt.Sprintf("UPDATE risk_actions SET %s WHERE id = $%d", strings.Join(sets, ", "), idx), args...)
	return err
}

// DeleteRiskAction 删除处置策略
func (r *Repository) DeleteRiskAction(ctx context.Context, id int64) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM risk_actions WHERE id = $1`, id)
	return err
}

// ════════════════════════════════════════════════════════════
//  scan 辅助
// ════════════════════════════════════════════════════════════

func normalizePage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 20
	}
	return page, pageSize
}

// truncateColumn 按 rune 截断，避免超长输入撞上 VARCHAR 上限报错。
// UA 与账号都可能被恶意构造成超长串，让它把一次评估的留痕整条写失败并不划算。
func truncateColumn(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit])
}

type rowScanner interface{ Scan(dest ...any) error }

type rowsScanner interface {
	Next() bool
	Scan(dest ...any) error
	Err() error
}

func scanRiskRule(row rowScanner) (*securitydomain.RiskRule, error) {
	var item securitydomain.RiskRule
	var condRaw []byte
	if err := row.Scan(
		&item.ID, &item.Name, &item.Description, &item.Scene,
		&item.ConditionType, &condRaw, &item.Score, &item.IsActive, &item.Priority,
		&item.HitCount, &item.LastHitAt, &item.CreatedBy, &item.UpdatedBy,
		&item.CreatedAt, &item.UpdatedAt, &item.AppID, &item.Mode, &item.Decision, &item.TemplateKey,
	); err != nil {
		return nil, normalizeNotFound(err)
	}
	if len(condRaw) > 0 {
		_ = json.Unmarshal(condRaw, &item.ConditionData)
	}
	if item.ConditionData == nil {
		item.ConditionData = make(map[string]any)
	}
	if item.Mode == "" {
		item.Mode = securitydomain.RuleModeEnforce
	}
	item.Dimension = securitydomain.ConditionDimension(item.ConditionType, item.ConditionData)
	return &item, nil
}

func collectRiskRules(rows rowsScanner) ([]securitydomain.RiskRule, error) {
	items := make([]securitydomain.RiskRule, 0, 32)
	for rows.Next() {
		item, err := scanRiskRule(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func scanRiskAssessment(row rowScanner) (*securitydomain.RiskAssessment, error) {
	var item securitydomain.RiskAssessment
	var rulesRaw, contextRaw, dimRaw, listRaw, enforceRaw []byte
	if err := row.Scan(
		&item.ID, &item.Scene, &item.AppID, &item.UserID, &item.IdentityID, &item.Account,
		&item.IP, &item.DeviceID, &item.UserAgent, &item.Country,
		&item.TotalScore, &item.RiskLevel, &rulesRaw, &contextRaw, &item.LatencyMS,
		&item.Action, &item.ActionDetail,
		&item.Reviewed, &item.ReviewerID, &item.ReviewerName, &item.ReviewResult,
		&item.ReviewComment, &item.ReviewedAt, &item.CreatedAt,
		&dimRaw, &listRaw, &item.DecisionBy, &enforceRaw,
	); err != nil {
		return nil, normalizeNotFound(err)
	}
	if len(dimRaw) > 0 {
		_ = json.Unmarshal(dimRaw, &item.DimensionScores)
	}
	if len(listRaw) > 0 {
		_ = json.Unmarshal(listRaw, &item.ListHits)
	}
	if len(enforceRaw) > 0 {
		_ = json.Unmarshal(enforceRaw, &item.Enforcement)
	}
	normalizeAssessmentShape(&item)
	if len(rulesRaw) > 0 {
		_ = json.Unmarshal(rulesRaw, &item.MatchedRules)
	}
	if item.MatchedRules == nil {
		item.MatchedRules = []securitydomain.MatchedRule{}
	}
	if len(contextRaw) > 0 {
		_ = json.Unmarshal(contextRaw, &item.EvalContext)
	}
	return &item, nil
}

func collectRiskAssessments(rows rowsScanner) ([]securitydomain.RiskAssessment, error) {
	items := make([]securitydomain.RiskAssessment, 0, 32)
	for rows.Next() {
		item, err := scanRiskAssessment(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func scanDeviceFingerprint(row rowScanner) (*securitydomain.DeviceFingerprint, error) {
	var item securitydomain.DeviceFingerprint
	var fpRaw []byte
	if err := row.Scan(
		&item.ID, &item.DeviceID, &item.UserID, &item.AppID, &fpRaw, &item.RiskTag,
		&item.LastIP, &item.UserAgent, &item.Note,
		&item.FirstSeenAt, &item.LastSeenAt, &item.SeenCount,
	); err != nil {
		return nil, normalizeNotFound(err)
	}
	if len(fpRaw) > 0 {
		_ = json.Unmarshal(fpRaw, &item.Fingerprint)
	}
	if item.Fingerprint == nil {
		item.Fingerprint = make(map[string]any)
	}
	return &item, nil
}

func collectDeviceFingerprints(rows rowsScanner) ([]securitydomain.DeviceFingerprint, error) {
	items := make([]securitydomain.DeviceFingerprint, 0, 32)
	for rows.Next() {
		item, err := scanDeviceFingerprint(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func scanIPRiskRecord(row rowScanner) (*securitydomain.IPRiskRecord, error) {
	var item securitydomain.IPRiskRecord
	if err := row.Scan(
		&item.ID, &item.IP, &item.RiskTag, &item.RiskScore,
		&item.Country, &item.Region, &item.ISP, &item.ASN, &item.Source, &item.Note,
		&item.IsProxy, &item.IsVPN, &item.IsTor, &item.IsDatacenter,
		&item.TotalRequests, &item.TotalBlocks, &item.FirstSeenAt, &item.LastSeenAt,
	); err != nil {
		return nil, normalizeNotFound(err)
	}
	return &item, nil
}

func collectIPRiskRecords(rows rowsScanner) ([]securitydomain.IPRiskRecord, error) {
	items := make([]securitydomain.IPRiskRecord, 0, 32)
	for rows.Next() {
		item, err := scanIPRiskRecord(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

func scanRiskAction(row rowScanner) (*securitydomain.RiskAction, error) {
	var item securitydomain.RiskAction
	if err := row.Scan(
		&item.ID, &item.Scene, &item.MinScore, &item.MaxScore,
		&item.Action, &item.BanDuration, &item.Description,
		&item.IsActive, &item.CreatedAt, &item.AppID, &item.BanTargets, &item.UpdatedAt,
	); err != nil {
		return nil, normalizeNotFound(err)
	}
	if item.BanTargets == nil {
		item.BanTargets = []string{}
	}
	return &item, nil
}

func collectRiskActions(rows rowsScanner) ([]securitydomain.RiskAction, error) {
	items := make([]securitydomain.RiskAction, 0, 16)
	for rows.Next() {
		item, err := scanRiskAction(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}
