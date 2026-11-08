package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"aegis/internal/domain/levels"
)

// 等级体系的管理端：草稿、发布与版本、用户等级、升级记录、总览、重算任务。

// GetLevelSchemeRecord 应用的体系（草稿 + 发布信息）。还没保存过草稿时草稿是当前生效的体系（默认体系）。
func (r *Repository) GetLevelSchemeRecord(ctx context.Context, appID int64) (*levels.SchemeRecord, error) {
	rec := &levels.SchemeRecord{AppID: appID}
	var draft, published []byte
	var draftAt time.Time
	err := r.pool.QueryRow(ctx, `SELECT draft_graph, draft_revision, draft_updated_by, draft_updated_at,
published_graph, published_version, published_by, published_at FROM level_schemes WHERE appid = $1`, appID).
		Scan(&draft, &rec.DraftRevision, &rec.DraftUpdatedBy, &draftAt, &published, &rec.PublishedVersion, &rec.PublishedBy, &rec.PublishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		graph, err := r.DefaultLevelGraph(ctx)
		if err != nil {
			return nil, err
		}
		rec.Draft = graph
		rec.IsDefault = true
		return rec, nil
	}
	if err != nil {
		return nil, err
	}
	rec.DraftUpdatedAt = &draftAt
	_ = json.Unmarshal(draft, &rec.Draft)
	if len(published) == 0 {
		rec.Dirty = true
	} else {
		rec.Dirty = !sameGraph(draft, published)
	}
	return rec, nil
}

// sameGraph 两份画布的内容是否一致（忽略视口与 JSON 排版）。
func sameGraph(a, b []byte) bool {
	var ga, gb levels.Graph
	if json.Unmarshal(a, &ga) != nil || json.Unmarshal(b, &gb) != nil {
		return bytes.Equal(a, b)
	}
	ga.Viewport, gb.Viewport = nil, nil
	ra, _ := json.Marshal(levels.NormalizeGraph(ga))
	rb, _ := json.Marshal(levels.NormalizeGraph(gb))
	return bytes.Equal(ra, rb)
}

// SaveLevelSchemeDraft 保存草稿。revision 是编辑开始时的修订号（0 表示还没有草稿，负数表示不比对直接覆盖）；对不上说明别人改过。
func (r *Repository) SaveLevelSchemeDraft(ctx context.Context, appID int64, graph levels.Graph, revision int, operator string) (*levels.SchemeRecord, error) {
	raw, err := json.Marshal(graph)
	if err != nil {
		return nil, err
	}
	tag, err := r.pool.Exec(ctx, `INSERT INTO level_schemes (appid, draft_graph, draft_revision, draft_updated_by, draft_updated_at)
VALUES ($1, $2, 1, $3, NOW())
ON CONFLICT (appid) DO UPDATE SET draft_graph = EXCLUDED.draft_graph, draft_revision = level_schemes.draft_revision + 1,
    draft_updated_by = EXCLUDED.draft_updated_by, draft_updated_at = NOW(), updated_at = NOW()
WHERE $4 < 0 OR level_schemes.draft_revision = $4`, appID, raw, operator, revision)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrLevelSchemeConflict
	}
	return r.GetLevelSchemeRecord(ctx, appID)
}

// PublishLevelScheme 发布：画布写成已发布、版本号加一、留一份快照。调用方已校验没有错误。
func (r *Repository) PublishLevelScheme(ctx context.Context, appID int64, graph levels.Graph, summary levels.Summary,
	note, operator string) (int, error) {
	raw, err := json.Marshal(graph)
	if err != nil {
		return 0, err
	}
	rawSummary, _ := json.Marshal(summary)
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var version int
	if err := tx.QueryRow(ctx, `INSERT INTO level_schemes (appid, draft_graph, draft_updated_by, published_graph, published_version, published_by, published_at)
VALUES ($1, $2, $3, $2, 1, $3, NOW())
ON CONFLICT (appid) DO UPDATE SET draft_graph = EXCLUDED.draft_graph, draft_revision = level_schemes.draft_revision + 1,
    published_graph = EXCLUDED.published_graph, published_version = level_schemes.published_version + 1,
    published_by = EXCLUDED.published_by, published_at = NOW(), updated_at = NOW()
RETURNING published_version`, appID, raw, operator).Scan(&version); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO level_scheme_versions (appid, version, graph, summary, note, published_by)
VALUES ($1, $2, $3, $4, $5, $6)`, appID, version, raw, rawSummary, truncateRunes(note, 200), operator); err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	r.InvalidateLevelScheme(appID)
	return version, nil
}

// ListLevelSchemeVersions 发布历史（不带画布）。
func (r *Repository) ListLevelSchemeVersions(ctx context.Context, appID int64) ([]levels.Version, error) {
	rows, err := r.pool.Query(ctx, `SELECT version, summary, note, published_by, published_at FROM level_scheme_versions
WHERE appid = $1 ORDER BY version DESC LIMIT 100`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []levels.Version{}
	for rows.Next() {
		var v levels.Version
		var summary []byte
		if err := rows.Scan(&v.Version, &summary, &v.Note, &v.PublishedBy, &v.PublishedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(summary, &v.Summary)
		out = append(out, v)
	}
	return out, rows.Err()
}

// GetLevelSchemeVersion 一次发布（带画布）。
func (r *Repository) GetLevelSchemeVersion(ctx context.Context, appID int64, version int) (*levels.Version, error) {
	var v levels.Version
	var summary, graph []byte
	err := r.pool.QueryRow(ctx, `SELECT version, summary, note, published_by, published_at, graph FROM level_scheme_versions
WHERE appid = $1 AND version = $2`, appID, version).Scan(&v.Version, &summary, &v.Note, &v.PublishedBy, &v.PublishedAt, &graph)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrLevelVersionMissing
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal(summary, &v.Summary)
	var g levels.Graph
	_ = json.Unmarshal(graph, &g)
	v.Graph = &g
	return &v, nil
}

// ── 用户等级 ──

// ListUserLevels 用户等级列表（没有等级记录的人按 Lv.1 计）。
func (r *Repository) ListUserLevels(ctx context.Context, query levels.UserQuery) (*levels.Page[levels.UserLevel], error) {
	scheme, _, err := r.LevelScheme(ctx, query.AppID)
	if err != nil {
		return nil, err
	}
	page, limit := normalizePage(query.Page, query.Limit)
	args := []any{query.AppID}
	where := []string{"u.appid = $1"}
	add := func(cond string, value any) {
		args = append(args, value)
		where = append(where, strings.ReplaceAll(cond, "?", "$"+strconv.Itoa(len(args))))
	}
	if query.Level > 0 {
		add("COALESCE(lr.current_level, 1) = ?", query.Level)
	}
	if query.MinLevel > 0 {
		add("COALESCE(lr.current_level, 1) >= ?", query.MinLevel)
	}
	if query.MaxLevel > 0 {
		add("COALESCE(lr.current_level, 1) <= ?", query.MaxLevel)
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		if id, err := strconv.ParseInt(keyword, 10, 64); err == nil {
			args = append(args, id, "%"+keyword+"%")
			where = append(where, "(u.id = $"+strconv.Itoa(len(args)-1)+" OR u.account ILIKE $"+strconv.Itoa(len(args))+")")
		} else {
			add("(u.account ILIKE ? OR up.nickname ILIKE ?)", "%"+keyword+"%")
		}
	}
	order := "COALESCE(lr.current_level, 1) DESC, u.experience DESC, u.id ASC"
	switch query.Sort {
	case "experience":
		order = "u.experience DESC, u.id ASC"
	case "recent":
		order = "lr.last_level_up_at DESC NULLS LAST, u.id DESC"
	}
	cond := strings.Join(where, " AND ")
	joins := `FROM users u
LEFT JOIN user_profiles up ON up.user_id = u.id
LEFT JOIN user_level_records lr ON lr.user_id = u.id AND lr.appid = u.appid`
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) `+joins+` WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, err
	}
	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, `SELECT u.id, u.account, COALESCE(up.nickname, ''), COALESCE(up.avatar, ''),
COALESCE(lr.current_level, 1), u.experience, COALESCE(lr.highest_level, 1), COALESCE(lr.level_up_count, 0),
lr.last_level_up_at, lr.updated_at, COALESCE(lr.locked_level, 0),
(SELECT COUNT(*) FROM user_level_rewards w WHERE w.appid = u.appid AND w.user_id = u.id AND w.status = 'claimable')
`+joins+` WHERE `+cond+` ORDER BY `+order+`
LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := &levels.Page[levels.UserLevel]{Items: []levels.UserLevel{}, Total: total, Page: page, Limit: limit}
	for rows.Next() {
		var item levels.UserLevel
		if err := rows.Scan(&item.UserID, &item.Account, &item.Nickname, &item.Avatar, &item.Level, &item.Experience,
			&item.HighestLevel, &item.LevelUpCount, &item.LastLevelUpAt, &item.UpdatedAt, &item.LockedLevel, &item.Claimable); err != nil {
			return nil, err
		}
		state := scheme.StateOf(item.Level, item.Experience)
		item.Level = state.Level
		item.LevelName = state.Config.Name
		item.Progress = state.Progress
		out.Items = append(out.Items, item)
	}
	return out, rows.Err()
}

// LevelAdminSync 管理员补判 / 强制升级一个人的选项。
type LevelAdminSync struct {
	// Force > 0 时直接调到至少这一级（越过门槛与晋级条件）；0 = 只按当前体系补判
	Force       int
	Lock        bool
	Unlock      bool
	SkipRewards bool
	Silent      bool
	Operator    string
	Note        string
}

// SyncUserLevelAdmin 管理员：按当前体系补判一个人，或强制升级。
func (r *Repository) SyncUserLevelAdmin(ctx context.Context, appID, userID int64, opts LevelAdminSync) (*levels.UserLevel, int, int, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, 0, 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var experience int64
	if err := tx.QueryRow(ctx, `SELECT experience FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`, userID, appID).Scan(&experience); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, 0, 0, ErrUserNotFound
		}
		return nil, 0, 0, err
	}
	synced, err := r.syncUserLevel(ctx, tx, userID, appID, experience, time.Now().UTC(), levelSyncOptions{
		Reason: levels.ReasonAdmin, Operator: opts.Operator, Note: opts.Note, ForceLevel: opts.Force,
		Lock: opts.Lock, Unlock: opts.Unlock, SkipRewards: opts.SkipRewards, Silent: opts.Silent,
	})
	if err != nil {
		return nil, 0, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, 0, err
	}
	page, err := r.ListUserLevels(ctx, levels.UserQuery{AppID: appID, Keyword: strconv.FormatInt(userID, 10), Limit: 1})
	if err != nil {
		return nil, 0, 0, err
	}
	for _, item := range page.Items {
		if item.UserID == userID {
			return &item, synced.From, synced.To, nil
		}
	}
	return nil, synced.From, synced.To, ErrUserNotFound
}

// UserExperience 一个人的累计经验。
func (r *Repository) UserExperience(ctx context.Context, appID, userID int64) (int64, error) {
	var experience int64
	err := r.pool.QueryRow(ctx, `SELECT experience FROM users WHERE id = $1 AND appid = $2`, userID, appID).Scan(&experience)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrUserNotFound
	}
	return experience, err
}

// ── 升级记录 ──

// ListLevelEvents 升级记录。
func (r *Repository) ListLevelEvents(ctx context.Context, query levels.EventQuery) (*levels.Page[levels.Event], error) {
	scheme, _, err := r.LevelScheme(ctx, query.AppID)
	if err != nil {
		return nil, err
	}
	page, limit := normalizePage(query.Page, query.Limit)
	args := []any{query.AppID}
	where := []string{"e.appid = $1"}
	add := func(cond string, value any) {
		args = append(args, value)
		where = append(where, strings.ReplaceAll(cond, "?", "$"+strconv.Itoa(len(args))))
	}
	if query.UserID > 0 {
		add("e.user_id = ?", query.UserID)
	}
	if query.Reason != "" {
		add("e.reason = ?", query.Reason)
	}
	if query.Level > 0 {
		add("e.to_level = ?", query.Level)
	}
	if query.From != nil {
		add("e.created_at >= ?", *query.From)
	}
	if query.To != nil {
		add("e.created_at < ?", *query.To)
	}
	cond := strings.Join(where, " AND ")
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM user_level_events e WHERE `+cond, args...).Scan(&total); err != nil {
		return nil, err
	}
	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, `SELECT e.id, e.appid, e.user_id, COALESCE(u.account, ''), COALESCE(up.nickname, ''),
e.from_level, e.to_level, e.experience, e.reason, e.scheme_version, e.operator, e.note, e.notify_status, e.created_at
FROM user_level_events e
LEFT JOIN users u ON u.id = e.user_id
LEFT JOIN user_profiles up ON up.user_id = e.user_id
WHERE `+cond+` ORDER BY e.created_at DESC, e.id DESC
LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := &levels.Page[levels.Event]{Items: []levels.Event{}, Total: total, Page: page, Limit: limit}
	for rows.Next() {
		var e levels.Event
		if err := rows.Scan(&e.ID, &e.AppID, &e.UserID, &e.Account, &e.Nickname, &e.FromLevel, &e.ToLevel, &e.Experience,
			&e.Reason, &e.SchemeVersion, &e.Operator, &e.Note, &e.NotifyStatus, &e.CreatedAt); err != nil {
			return nil, err
		}
		if e.FromLevel <= scheme.MaxLevel() {
			e.FromName = scheme.At(e.FromLevel).Name
		}
		if e.ToLevel <= scheme.MaxLevel() {
			e.ToName = scheme.At(e.ToLevel).Name
		}
		out.Items = append(out.Items, e)
	}
	return out, rows.Err()
}

// ── 总览 ──

// LevelOverview 等级分布、升级趋势、奖励状态与正在跑的任务。
func (r *Repository) LevelOverview(ctx context.Context, appID int64, dayStart time.Time, days int) (*levels.Overview, error) {
	scheme, version, err := r.LevelScheme(ctx, appID)
	if err != nil {
		return nil, err
	}
	out := &levels.Overview{Version: version, Summary: levels.Summarize(scheme), Distribution: []levels.LevelStat{}, Trend: []levels.TrendPoint{}}
	counts := map[int]int64{}
	rows, err := r.pool.Query(ctx, `SELECT LEAST(GREATEST(COALESCE(lr.current_level, 1), 1), $2) AS lv, COUNT(*)
FROM users u LEFT JOIN user_level_records lr ON lr.user_id = u.id AND lr.appid = u.appid
WHERE u.appid = $1 GROUP BY lv`, appID, scheme.MaxLevel())
	if err != nil {
		return nil, err
	}
	var weighted int64
	for rows.Next() {
		var level int
		var count int64
		if err := rows.Scan(&level, &count); err != nil {
			rows.Close()
			return nil, err
		}
		counts[level] += count
		out.TotalUsers += count
		weighted += int64(level) * count
	}
	rows.Close()
	if out.TotalUsers > 0 {
		out.AverageLevel = float64(weighted*100/out.TotalUsers) / 100
	}
	for _, level := range scheme.Levels {
		out.Distribution = append(out.Distribution, levels.LevelStat{
			Level: level.Level, Name: level.Name, Icon: level.Icon, Color: level.Color, Threshold: level.Threshold, Users: counts[level.Level],
		})
	}
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM user_level_records WHERE appid = $1`, appID).Scan(&out.RankedUsers); err != nil {
		return nil, err
	}

	since := dayStart.AddDate(0, 0, -(days - 1))
	trend := map[string]*levels.TrendPoint{}
	for i := 0; i < days; i++ {
		date := since.AddDate(0, 0, i).Format("2006-01-02")
		point := &levels.TrendPoint{Date: date}
		trend[date] = point
	}
	rows, err = r.pool.Query(ctx, `SELECT to_char(created_at AT TIME ZONE $3, 'YYYY-MM-DD'), COUNT(*) FROM user_level_events
WHERE appid = $1 AND created_at >= $2 AND to_level > from_level GROUP BY 1`, appID, since, dayStart.Location().String())
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var date string
		var count int64
		if err := rows.Scan(&date, &count); err != nil {
			rows.Close()
			return nil, err
		}
		if point, ok := trend[date]; ok {
			point.LevelUps = count
		}
	}
	rows.Close()
	rows, err = r.pool.Query(ctx, `SELECT to_char(granted_at AT TIME ZONE $3, 'YYYY-MM-DD'), COUNT(*) FROM user_level_rewards
WHERE appid = $1 AND granted_at >= $2 GROUP BY 1`, appID, since, dayStart.Location().String())
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var date string
		var count int64
		if err := rows.Scan(&date, &count); err != nil {
			rows.Close()
			return nil, err
		}
		if point, ok := trend[date]; ok {
			point.Rewards = count
		}
	}
	rows.Close()
	for i := 0; i < days; i++ {
		date := since.AddDate(0, 0, i).Format("2006-01-02")
		out.Trend = append(out.Trend, *trend[date])
	}
	if err := r.pool.QueryRow(ctx, `SELECT
COUNT(*) FILTER (WHERE created_at >= $2),
COUNT(*) FILTER (WHERE created_at >= $3)
FROM user_level_events WHERE appid = $1 AND to_level > from_level AND created_at >= $3`,
		appID, dayStart, dayStart.AddDate(0, 0, -6)).Scan(&out.LevelUpsToday, &out.LevelUps7d); err != nil {
		return nil, err
	}
	if err := r.pool.QueryRow(ctx, `SELECT
COUNT(*) FILTER (WHERE status = 'queued'), COUNT(*) FILTER (WHERE status = 'claimable'),
COUNT(*) FILTER (WHERE status = 'granted'), COUNT(*) FILTER (WHERE status = 'pending'),
COUNT(*) FILTER (WHERE status = 'failed')
FROM user_level_rewards WHERE appid = $1`, appID).Scan(&out.Rewards.Queued, &out.Rewards.Claimable, &out.Rewards.Granted,
		&out.Rewards.Pending, &out.Rewards.Failed); err != nil {
		return nil, err
	}
	job, err := r.RunningLevelJob(ctx, appID)
	if err != nil {
		return nil, err
	}
	out.RunningJob = job
	return out, nil
}

// ── 重算任务 ──

const levelJobColumns = `id, appid, kind, params, status, scheme_version, total, processed, changed, cursor_id, message,
created_by, heartbeat_at, created_at, finished_at`

func scanLevelJob(row interface{ Scan(dest ...any) error }) (*levels.Job, error) {
	var job levels.Job
	var params []byte
	if err := row.Scan(&job.ID, &job.AppID, &job.Kind, &params, &job.Status, &job.SchemeVersion, &job.Total, &job.Processed,
		&job.Changed, &job.Cursor, &job.Message, &job.CreatedBy, &job.HeartbeatAt, &job.CreatedAt, &job.FinishedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(params, &job.Params)
	if job.Params == nil {
		job.Params = map[string]any{}
	}
	return &job, nil
}

// CreateLevelJob 新建任务（一个应用同时只有一个在跑）。
func (r *Repository) CreateLevelJob(ctx context.Context, appID int64, kind string, params map[string]any, version int, operator string) (*levels.Job, error) {
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE appid = $1`, appID).Scan(&total); err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(params)
	job, err := scanLevelJob(r.pool.QueryRow(ctx, `INSERT INTO level_jobs (appid, kind, params, scheme_version, total, created_by)
VALUES ($1, $2, $3, $4, $5, $6) RETURNING `+levelJobColumns, appID, kind, raw, version, total, operator))
	if isUniqueViolation(err) {
		return nil, ErrLevelJobBusy
	}
	return job, err
}

// GetLevelJob 取一个任务（appID 为 0 时不限应用）。
func (r *Repository) GetLevelJob(ctx context.Context, appID, jobID int64) (*levels.Job, error) {
	job, err := scanLevelJob(r.pool.QueryRow(ctx, `SELECT `+levelJobColumns+` FROM level_jobs WHERE id = $1 AND ($2 = 0 OR appid = $2)`, jobID, appID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrLevelJobMissing
	}
	return job, err
}

// RunningLevelJob 应用正在跑的任务（没有为 nil）。
func (r *Repository) RunningLevelJob(ctx context.Context, appID int64) (*levels.Job, error) {
	job, err := scanLevelJob(r.pool.QueryRow(ctx, `SELECT `+levelJobColumns+` FROM level_jobs WHERE appid = $1 AND status = 'running'`, appID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return job, err
}

// ListLevelJobs 最近的任务。
func (r *Repository) ListLevelJobs(ctx context.Context, appID int64, limit int) ([]levels.Job, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+levelJobColumns+` FROM level_jobs WHERE appid = $1 ORDER BY created_at DESC, id DESC LIMIT $2`, appID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []levels.Job{}
	for rows.Next() {
		job, err := scanLevelJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *job)
	}
	return out, rows.Err()
}

// LevelJobBatch 任务的下一批用户（按 ID 升序，游标之后）。
func (r *Repository) LevelJobBatch(ctx context.Context, appID, cursor int64, limit int) ([]int64, error) {
	rows, err := r.pool.Query(ctx, `SELECT id FROM users WHERE appid = $1 AND id > $2 ORDER BY id LIMIT $3`, appID, cursor, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// RecalcUserLevel 重算任务里的一个人：在自己的短事务里按当前体系判定。
func (r *Repository) RecalcUserLevel(ctx context.Context, appID, userID int64, downgrade bool, operator string) (bool, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var experience int64
	if err := tx.QueryRow(ctx, `SELECT experience FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`, userID, appID).Scan(&experience); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	synced, err := r.syncUserLevel(ctx, tx, userID, appID, experience, time.Now().UTC(), levelSyncOptions{
		Reason: levels.ReasonRecalc, Operator: operator, Recompute: downgrade,
	})
	if err != nil {
		return false, err
	}
	return synced.Changed, tx.Commit(ctx)
}

// AdvanceLevelJob 写进度并续心跳；返回任务的当前状态（被取消时调用方停下）。
func (r *Repository) AdvanceLevelJob(ctx context.Context, jobID int64, processed, changed, cursor int64) (string, error) {
	var status string
	err := r.pool.QueryRow(ctx, `UPDATE level_jobs SET processed = processed + $2, changed = changed + $3, cursor_id = $4, heartbeat_at = NOW()
WHERE id = $1 RETURNING status`, jobID, processed, changed, cursor).Scan(&status)
	return status, err
}

// FinishLevelJob 结束任务。
func (r *Repository) FinishLevelJob(ctx context.Context, jobID int64, status, message string) error {
	_, err := r.pool.Exec(ctx, `UPDATE level_jobs SET status = $2, message = $3, finished_at = NOW()
WHERE id = $1 AND status = 'running'`, jobID, status, message)
	return err
}

// CancelLevelJob 取消任务（这一批处理完就停）。
func (r *Repository) CancelLevelJob(ctx context.Context, appID, jobID int64, operator string) (*levels.Job, error) {
	tag, err := r.pool.Exec(ctx, `UPDATE level_jobs SET status = 'cancelled', message = $3, finished_at = NOW()
WHERE id = $1 AND appid = $2 AND status = 'running'`, jobID, appID, "由 "+operator+" 取消")
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		if _, err := r.GetLevelJob(ctx, appID, jobID); err != nil {
			return nil, err
		}
		return nil, ErrLevelRewardState
	}
	return r.GetLevelJob(ctx, appID, jobID)
}

// ClaimStaleLevelJobs 心跳超时的任务（实例退出了），由本实例接着跑。
func (r *Repository) ClaimStaleLevelJobs(ctx context.Context, staleBefore time.Time, limit int) ([]levels.Job, error) {
	rows, err := r.pool.Query(ctx, `UPDATE level_jobs SET heartbeat_at = NOW()
WHERE id IN (SELECT id FROM level_jobs WHERE status = 'running' AND heartbeat_at < $1 ORDER BY id LIMIT $2 FOR UPDATE SKIP LOCKED)
RETURNING `+levelJobColumns, staleBefore, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []levels.Job
	for rows.Next() {
		job, err := scanLevelJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *job)
	}
	return out, rows.Err()
}
