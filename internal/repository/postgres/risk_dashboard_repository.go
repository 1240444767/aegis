package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	securitydomain "aegis/internal/domain/security"
)

// ════════════════════════════════════════════════════════════
//  风控大盘
//
//  全部聚合共用一个窗口：$1 起、$2 止、$3 应用（NULL = 全平台）。
//  各段各拼一套 WHERE 是「同一个应用筛选在趋势图与 Top 榜上对不上」的来源。
// ════════════════════════════════════════════════════════════

// riskWindow 大盘查询窗口
type riskWindow struct {
	start time.Time
	end   time.Time
	appID any
}

func (w riskWindow) args(extra ...any) []any {
	return append([]any{w.start, w.end, w.appID}, extra...)
}

// riskWindowWhere 带表别名 a 的窗口条件
const riskWindowWhere = `a.created_at BETWEEN $1 AND $2 AND ($3::bigint IS NULL OR a.app_id = $3)`

// GetRiskDashboard 组装风控统计大盘。
//
// 额外查一次**上一等长周期**做对照 —— 只给绝对数看不出「今天是不是不对劲」，
// 而那正是大盘唯一的用途。
func (r *Repository) GetRiskDashboard(ctx context.Context, start, end time.Time, bucket string, appID *int64) (*securitydomain.RiskDashboard, error) {
	w := riskWindow{start: start, end: end, appID: riskAppIDArg(appID)}
	dash := &securitydomain.RiskDashboard{
		Range: securitydomain.DashboardRange{Start: start, End: end, Bucket: normalizeBucket(bucket)},
	}

	summary, err := r.riskSummary(ctx, w)
	if err != nil {
		return nil, err
	}
	span := end.Sub(start)
	prev, err := r.riskSummary(ctx, riskWindow{start: start.Add(-span), end: start, appID: w.appID})
	if err != nil {
		return nil, err
	}
	summary.PrevTotalAssessments = prev.TotalAssessments
	summary.PrevTotalBlocked = prev.TotalBlocked
	summary.PrevBlockRate = prev.BlockRate
	summary.PrevAvgScore = prev.AvgScore
	dash.Summary = summary

	steps := []func() error{
		func() (err error) { dash.Series, err = r.riskSeries(ctx, w, dash.Range.Bucket); return },
		func() (err error) { dash.SceneDistribution, err = r.riskSceneDistribution(ctx, w); return },
		func() (err error) { dash.LevelDistribution, err = r.riskLevelDistribution(ctx, w); return },
		func() (err error) { dash.ActionDistribution, err = r.riskActionDistribution(ctx, w); return },
		func() (err error) { dash.ScoreHistogram, err = r.riskScoreHistogram(ctx, w); return },
		func() (err error) { dash.TopRules, err = r.riskTopRules(ctx, w, 10); return },
		func() (err error) { dash.TopIPs, err = r.riskTopIPs(ctx, w, 10); return },
		func() (err error) { dash.TopDevices, err = r.riskTopDevices(ctx, w, 10); return },
		func() (err error) { dash.TopCountries, err = r.riskTopCountries(ctx, w, 10); return },
		func() (err error) { dash.Dimensions, err = r.riskDimensionStats(ctx, w); return },
		func() (err error) { dash.DecisionSources, err = r.riskDecisionSources(ctx, w); return },
		func() (err error) { dash.Lists, err = r.riskListStats(ctx, w); return },
		func() (err error) {
			dash.TopAccounts, _, err = r.ListAccountRiskProfiles(ctx, securitydomain.AccountQuery{
				AppID: appID, OnlyRisk: true, Start: &start, Page: 1, PageSize: 10,
			})
			return
		},
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return nil, err
		}
	}
	return dash, nil
}

func (r *Repository) riskSummary(ctx context.Context, w riskWindow) (securitydomain.RiskSummary, error) {
	var s securitydomain.RiskSummary
	err := r.pool.QueryRow(ctx, `
SELECT COUNT(*),
       COUNT(*) FILTER (WHERE a.action IN ('block','ban')),
       COUNT(*) FILTER (WHERE a.action = 'captcha'),
       COUNT(*) FILTER (WHERE a.action = 'review'),
       COUNT(*) FILTER (WHERE a.action = 'review' AND NOT a.reviewed),
       COUNT(*) FILTER (WHERE a.action = 'pass'),
       COUNT(*) FILTER (WHERE a.risk_level IN ('high','critical')),
       COALESCE(AVG(a.total_score), 0),
       COALESCE(MAX(a.total_score), 0),
       COALESCE(AVG(a.latency_ms), 0),
       COUNT(DISTINCT a.ip) FILTER (WHERE a.ip <> ''),
       COUNT(DISTINCT a.device_id) FILTER (WHERE a.device_id <> ''),
       COUNT(DISTINCT a.account) FILTER (WHERE a.account <> '')
FROM risk_assessments a WHERE `+riskWindowWhere, w.args()...).Scan(
		&s.TotalAssessments, &s.TotalBlocked, &s.TotalChallenged, &s.TotalReviews, &s.PendingReviews,
		&s.TotalPassed, &s.HighRiskCount, &s.AvgScore, &s.MaxScore, &s.AvgLatencyMS,
		&s.DistinctIPs, &s.DistinctDevices, &s.DistinctAccounts,
	)
	if err != nil {
		return s, err
	}
	if s.TotalAssessments > 0 {
		s.BlockRate = float64(s.TotalBlocked) / float64(s.TotalAssessments)
	}
	return s, nil
}

// riskSeries 时间序列。用 generate_series 左连接补齐空桶 ——
// 缺桶的折线图会把「那几个小时没有任何请求」画成一条直连的斜线。
func (r *Repository) riskSeries(ctx context.Context, w riskWindow, bucket string) ([]securitydomain.RiskSeriesPoint, error) {
	unit := normalizeBucket(bucket)
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`
SELECT b.slot,
       COALESCE(d.total, 0), COALESCE(d.normal, 0), COALESCE(d.low, 0), COALESCE(d.medium, 0),
       COALESCE(d.high, 0), COALESCE(d.critical, 0),
       COALESCE(d.pass, 0), COALESCE(d.captcha, 0), COALESCE(d.review, 0), COALESCE(d.block, 0), COALESCE(d.ban, 0),
       COALESCE(d.avg_score, 0)
FROM generate_series(date_trunc('%[1]s', $1::timestamptz), date_trunc('%[1]s', $2::timestamptz), interval '1 %[1]s') AS b(slot)
LEFT JOIN (
    SELECT date_trunc('%[1]s', a.created_at) AS slot,
           COUNT(*) AS total,
           COUNT(*) FILTER (WHERE a.risk_level = 'normal')   AS normal,
           COUNT(*) FILTER (WHERE a.risk_level = 'low')      AS low,
           COUNT(*) FILTER (WHERE a.risk_level = 'medium')   AS medium,
           COUNT(*) FILTER (WHERE a.risk_level = 'high')     AS high,
           COUNT(*) FILTER (WHERE a.risk_level = 'critical') AS critical,
           COUNT(*) FILTER (WHERE a.action = 'pass')    AS pass,
           COUNT(*) FILTER (WHERE a.action = 'captcha') AS captcha,
           COUNT(*) FILTER (WHERE a.action = 'review')  AS review,
           COUNT(*) FILTER (WHERE a.action = 'block')   AS block,
           COUNT(*) FILTER (WHERE a.action = 'ban')     AS ban,
           AVG(a.total_score) AS avg_score
    FROM risk_assessments a WHERE %[2]s GROUP BY 1
) AS d ON d.slot = b.slot
ORDER BY b.slot ASC`, unit, riskWindowWhere), w.args()...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	points := make([]securitydomain.RiskSeriesPoint, 0, 128)
	for rows.Next() {
		var p securitydomain.RiskSeriesPoint
		if err := rows.Scan(&p.Time, &p.Total, &p.Normal, &p.Low, &p.Medium, &p.High, &p.Critical,
			&p.Pass, &p.Captcha, &p.Review, &p.Block, &p.Ban, &p.AvgScore); err != nil {
			return nil, err
		}
		if p.Total > 0 {
			p.BlockRate = float64(p.Block+p.Ban) / float64(p.Total)
		}
		points = append(points, p)
	}
	return points, rows.Err()
}

func (r *Repository) riskSceneDistribution(ctx context.Context, w riskWindow) ([]securitydomain.SceneStat, error) {
	rows, err := r.pool.Query(ctx, `
SELECT a.scene, COUNT(*),
       COUNT(*) FILTER (WHERE a.action IN ('block','ban')),
       COUNT(*) FILTER (WHERE a.action = 'captcha'),
       COUNT(*) FILTER (WHERE a.action = 'review'),
       COALESCE(AVG(a.total_score), 0)
FROM risk_assessments a WHERE `+riskWindowWhere+`
GROUP BY a.scene ORDER BY COUNT(*) DESC`, w.args()...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]securitydomain.SceneStat, 0, 12)
	for rows.Next() {
		var s securitydomain.SceneStat
		if err := rows.Scan(&s.Scene, &s.Count, &s.Blocked, &s.Challenged, &s.Reviewed, &s.AvgScore); err != nil {
			return nil, err
		}
		items = append(items, s)
	}
	return items, rows.Err()
}

// countGroup 按某一列分组计数
func (r *Repository) countGroup(ctx context.Context, w riskWindow, column string) (map[string]int64, error) {
	counts := map[string]int64{}
	rows, err := r.pool.Query(ctx, fmt.Sprintf(`SELECT a.%s, COUNT(*) FROM risk_assessments a
WHERE %s GROUP BY 1`, column, riskWindowWhere), w.args()...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var count int64
		if err := rows.Scan(&key, &count); err != nil {
			return nil, err
		}
		counts[key] = count
	}
	return counts, rows.Err()
}

// riskLevelDistribution 等级分布。五个等级恒定输出（没有的补 0）——
// 饼图缺一块和那一块为零是两回事。
func (r *Repository) riskLevelDistribution(ctx context.Context, w riskWindow) ([]securitydomain.LevelStat, error) {
	counts, err := r.countGroup(ctx, w, "risk_level")
	if err != nil {
		return nil, err
	}
	items := make([]securitydomain.LevelStat, 0, len(securitydomain.RiskLevelBands))
	for _, band := range securitydomain.RiskLevelBands {
		items = append(items, securitydomain.LevelStat{Level: band.Level, Count: counts[band.Level]})
	}
	return items, nil
}

func (r *Repository) riskActionDistribution(ctx context.Context, w riskWindow) ([]securitydomain.ActionStat, error) {
	counts, err := r.countGroup(ctx, w, "action")
	if err != nil {
		return nil, err
	}
	items := make([]securitydomain.ActionStat, 0, 5)
	for _, action := range securitydomain.ActionValues() {
		items = append(items, securitydomain.ActionStat{Action: action, Count: counts[action]})
	}
	return items, nil
}

// riskDecisionSources 判定来源分布。旧记录没有这一列，按「有没有干预」归入策略或无干预。
func (r *Repository) riskDecisionSources(ctx context.Context, w riskWindow) ([]securitydomain.DecisionStat, error) {
	rows, err := r.pool.Query(ctx, `
SELECT CASE WHEN a.decision_by <> '' THEN a.decision_by
            WHEN a.action = 'pass' THEN 'none' ELSE 'policy' END AS source,
       COUNT(*)
FROM risk_assessments a WHERE `+riskWindowWhere+` GROUP BY 1`, w.args()...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := map[string]int64{}
	for rows.Next() {
		var key string
		var count int64
		if err := rows.Scan(&key, &count); err != nil {
			return nil, err
		}
		counts[key] = count
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	items := make([]securitydomain.DecisionStat, 0, 5)
	for _, entry := range securitydomain.DecisionByCatalog() {
		items = append(items, securitydomain.DecisionStat{DecisionBy: entry.Value, Count: counts[entry.Value]})
	}
	return items, nil
}

// riskDimensionStats 各维度的贡献：命中规则次数、贡献分数、涉及请求数。
// 观察模式的命中不计分，但计入命中次数 —— 那正是观察模式要回答的问题。
func (r *Repository) riskDimensionStats(ctx context.Context, w riskWindow) ([]securitydomain.DimensionStat, error) {
	rows, err := r.pool.Query(ctx, `
SELECT COALESCE(NULLIF(m.rule ->> 'dimension', ''), `+conditionDimensionCase()+`) AS dimension,
       COUNT(*) AS hits,
       COALESCE(SUM(CASE WHEN (m.rule ->> 'observe')::boolean IS TRUE THEN 0 ELSE (m.rule ->> 'score')::int END), 0) AS score,
       COUNT(DISTINCT a.id) AS requests
FROM risk_assessments a
CROSS JOIN LATERAL jsonb_array_elements(`+matchedRulesArray+`) AS m(rule)
WHERE `+riskWindowWhere+`
GROUP BY 1`, w.args()...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byDim := map[string]securitydomain.DimensionStat{}
	for rows.Next() {
		var s securitydomain.DimensionStat
		if err := rows.Scan(&s.Dimension, &s.Hits, &s.Score, &s.Requests); err != nil {
			return nil, err
		}
		byDim[s.Dimension] = s
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	items := make([]securitydomain.DimensionStat, 0, len(byDim))
	for _, dim := range securitydomain.DimensionValues() {
		stat := byDim[dim]
		stat.Dimension = dim
		items = append(items, stat)
	}
	return items, nil
}

// conditionDimensionCase 旧留痕没有 dimension 字段，按条件类型从目录推出来。
// 取值全部来自代码里的常量目录，可以直接拼进 SQL。
func conditionDimensionCase() string {
	var b strings.Builder
	b.WriteString("CASE m.rule ->> 'conditionType'")
	for _, entry := range securitydomain.ConditionCatalogEntries() {
		fmt.Fprintf(&b, " WHEN '%s' THEN '%s'", entry.Value, entry.Dimension)
	}
	b.WriteString(" ELSE 'custom' END")
	return b.String()
}

// riskListStats 名单条目数（现行）与区间内命中次数
func (r *Repository) riskListStats(ctx context.Context, w riskWindow) ([]securitydomain.ListKindStat, error) {
	entries := map[string]int64{}
	rows, err := r.pool.Query(ctx, `SELECT kind, COUNT(*) FROM risk_lists
WHERE (expires_at IS NULL OR expires_at > NOW()) AND ($1::bigint IS NULL OR app_id IS NULL OR app_id = $1)
GROUP BY kind`, w.appID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var kind string
		var count int64
		if err := rows.Scan(&kind, &count); err != nil {
			rows.Close()
			return nil, err
		}
		entries[kind] = count
	}
	rows.Close()

	hits := map[string]int64{}
	hitRows, err := r.pool.Query(ctx, `
SELECT h.hit ->> 'kind', COUNT(*)
FROM risk_assessments a
CROSS JOIN LATERAL jsonb_array_elements(CASE WHEN jsonb_typeof(a.list_hits) = 'array' THEN a.list_hits ELSE '[]'::jsonb END) AS h(hit)
WHERE `+riskWindowWhere+` GROUP BY 1`, w.args()...)
	if err != nil {
		return nil, err
	}
	defer hitRows.Close()
	for hitRows.Next() {
		var kind string
		var count int64
		if err := hitRows.Scan(&kind, &count); err != nil {
			return nil, err
		}
		hits[kind] = count
	}
	if err := hitRows.Err(); err != nil {
		return nil, err
	}
	items := make([]securitydomain.ListKindStat, 0, 3)
	for _, entry := range securitydomain.ListKindCatalog() {
		items = append(items, securitydomain.ListKindStat{Kind: entry.Value, Entries: entries[entry.Value], Hits: hits[entry.Value]})
	}
	return items, nil
}

// riskScoreHistogram 分数直方图，桶边界与等级分档一致。
func (r *Repository) riskScoreHistogram(ctx context.Context, w riskWindow) ([]securitydomain.ScoreBucket, error) {
	buckets := make([]securitydomain.ScoreBucket, 0, len(securitydomain.RiskLevelBands))
	for _, band := range securitydomain.RiskLevelBands {
		bucket := securitydomain.ScoreBucket{Min: band.MinScore, Level: band.Level, Max: 1 << 20}
		if band.MaxScore != nil {
			bucket.Max = *band.MaxScore
		}
		if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM risk_assessments a
WHERE `+riskWindowWhere+` AND a.total_score >= $4 AND a.total_score <= $5`,
			w.args(bucket.Min, bucket.Max)...).Scan(&bucket.Count); err != nil {
			return nil, err
		}
		buckets = append(buckets, bucket)
	}
	return buckets, nil
}

// riskTopRules 命中最多的规则。规则可能已被删除，名字取留痕里的快照。
func (r *Repository) riskTopRules(ctx context.Context, w riskWindow, limit int) ([]securitydomain.RuleHitStat, error) {
	rows, err := r.pool.Query(ctx, `
SELECT (m.rule ->> 'ruleId')::bigint AS rule_id,
       COALESCE(MAX(m.rule ->> 'ruleName'), '') AS rule_name,
       COALESCE(MAX(r.scene), '') AS scene,
       COALESCE(MAX(m.rule ->> 'conditionType'), COALESCE(MAX(r.condition_type), '')) AS condition_type,
       COALESCE(MAX((m.rule ->> 'score')::int), 0) AS score,
       COALESCE(bool_or(r.is_active), FALSE) AS is_active,
       COUNT(*) AS hits,
       COUNT(*) FILTER (WHERE a.action IN ('block','ban')) AS blocked,
       COALESCE(SUM((m.rule ->> 'score')::int), 0) AS score_sum,
       MAX(a.created_at) AS last_hit_at
FROM risk_assessments a
CROSS JOIN LATERAL jsonb_array_elements(`+matchedRulesArray+`) AS m(rule)
LEFT JOIN risk_rules r ON r.id = (m.rule ->> 'ruleId')::bigint
WHERE `+riskWindowWhere+`
  AND (m.rule ->> 'ruleId') IS NOT NULL
GROUP BY 1 ORDER BY hits DESC LIMIT $4`, w.args(limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]securitydomain.RuleHitStat, 0, limit)
	for rows.Next() {
		var s securitydomain.RuleHitStat
		if err := rows.Scan(&s.RuleID, &s.RuleName, &s.Scene, &s.ConditionType, &s.Score,
			&s.IsActive, &s.Hits, &s.Blocked, &s.ScoreSum, &s.LastHitAt); err != nil {
			return nil, err
		}
		items = append(items, s)
	}
	return items, rows.Err()
}

func (r *Repository) riskTopIPs(ctx context.Context, w riskWindow, limit int) ([]securitydomain.IPHitStat, error) {
	rows, err := r.pool.Query(ctx, `
SELECT a.ip,
       COALESCE(MAX(a.country), '') AS country,
       COUNT(*) AS total,
       COUNT(*) FILTER (WHERE a.action IN ('block','ban')) AS blocked,
       COALESCE(MAX(a.total_score), 0) AS max_score,
       COALESCE(AVG(a.total_score), 0) AS avg_score,
       COALESCE(MAX(ir.risk_tag), 'normal') AS risk_tag
FROM risk_assessments a
LEFT JOIN ip_risk_records ir ON ir.ip = a.ip
WHERE `+riskWindowWhere+` AND a.ip <> ''
GROUP BY a.ip ORDER BY max_score DESC, total DESC LIMIT $4`, w.args(limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]securitydomain.IPHitStat, 0, limit)
	for rows.Next() {
		var s securitydomain.IPHitStat
		if err := rows.Scan(&s.IP, &s.Country, &s.Count, &s.Blocked, &s.MaxScore, &s.AvgScore, &s.RiskTag); err != nil {
			return nil, err
		}
		items = append(items, s)
	}
	return items, rows.Err()
}

func (r *Repository) riskTopDevices(ctx context.Context, w riskWindow, limit int) ([]securitydomain.DeviceHitStat, error) {
	rows, err := r.pool.Query(ctx, `
SELECT a.device_id,
       COUNT(*) AS total,
       COUNT(*) FILTER (WHERE a.action IN ('block','ban')) AS blocked,
       COALESCE(MAX(a.total_score), 0) AS max_score,
       COALESCE(AVG(a.total_score), 0) AS avg_score,
       COALESCE(MAX(df.risk_tag), 'normal') AS risk_tag,
       COUNT(DISTINCT a.account) FILTER (WHERE a.account <> '') AS accounts
FROM risk_assessments a
LEFT JOIN device_fingerprints df ON df.device_id = a.device_id
WHERE `+riskWindowWhere+` AND a.device_id <> ''
GROUP BY a.device_id ORDER BY max_score DESC, total DESC LIMIT $4`, w.args(limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]securitydomain.DeviceHitStat, 0, limit)
	for rows.Next() {
		var s securitydomain.DeviceHitStat
		if err := rows.Scan(&s.DeviceID, &s.Count, &s.Blocked, &s.MaxScore, &s.AvgScore, &s.RiskTag, &s.Accounts); err != nil {
			return nil, err
		}
		items = append(items, s)
	}
	return items, rows.Err()
}

func (r *Repository) riskTopCountries(ctx context.Context, w riskWindow, limit int) ([]securitydomain.CountryStat, error) {
	rows, err := r.pool.Query(ctx, `
SELECT a.country, COUNT(*), COUNT(*) FILTER (WHERE a.action IN ('block','ban'))
FROM risk_assessments a WHERE `+riskWindowWhere+` AND a.country <> ''
GROUP BY a.country ORDER BY COUNT(*) DESC LIMIT $4`, w.args(limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]securitydomain.CountryStat, 0, limit)
	for rows.Next() {
		var s securitydomain.CountryStat
		if err := rows.Scan(&s.Country, &s.Count, &s.Blocked); err != nil {
			return nil, err
		}
		items = append(items, s)
	}
	return items, rows.Err()
}
