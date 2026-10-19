package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"aegis/internal/domain/invite"
	"aegis/internal/domain/leaderboard"
	"aegis/internal/domain/pointstore"
)

// 积分排行榜：设置、偏好、移出名单、实时名次（一条 SQL 算出 RANK 并列）、周期结算快照与奖励发放。
// 规则见 docs/leaderboard.md。

var (
	ErrLeaderboardSnapshotNotFound  = errors.New("leaderboard snapshot not found")
	ErrLeaderboardExclusionNotFound = errors.New("leaderboard exclusion not found")
	// ErrLeaderboardRewardUnavailable 奖励引用的套餐被删、功能被停用：这一份发不出去
	ErrLeaderboardRewardUnavailable = errors.New("leaderboard reward unavailable")
)

// ── 设置 ──

// GetLeaderboardSettings 读设置；没保存过时返回默认配置、关闭、configured=false。
func (r *Repository) GetLeaderboardSettings(ctx context.Context, appID int64) (*leaderboard.Settings, error) {
	settings := leaderboard.Settings{AppID: appID, Config: leaderboard.DefaultConfig()}
	var raw []byte
	var updatedAt time.Time
	err := r.pool.QueryRow(ctx, `SELECT enabled, config, COALESCE(updated_by, ''), updated_at
FROM leaderboard_settings WHERE appid = $1`, appID).Scan(&settings.Enabled, &raw, &settings.UpdatedBy, &updatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		settings.Config = leaderboard.NormalizeConfig(settings.Config)
		return &settings, nil
	}
	if err != nil {
		return nil, err
	}
	if err := decodeLeaderboardConfig(raw, &settings.Config); err != nil {
		return nil, err
	}
	settings.UpdatedAt = &updatedAt
	settings.Configured = true
	return &settings, nil
}

func decodeLeaderboardConfig(raw []byte, cfg *leaderboard.Config) error {
	if len(raw) > 0 {
		// 缺的字段保留默认值：新加的配置项对老配置也有一个合理的取值
		if err := json.Unmarshal(raw, cfg); err != nil {
			return fmt.Errorf("decode leaderboard config: %w", err)
		}
	}
	*cfg = leaderboard.NormalizeConfig(*cfg)
	return nil
}

// SaveLeaderboardSettings 保存开关与配置（调用方已清洗、校验）。
func (r *Repository) SaveLeaderboardSettings(ctx context.Context, appID int64, enabled bool, cfg leaderboard.Config, operator string) (*leaderboard.Settings, error) {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	if _, err := r.pool.Exec(ctx, `INSERT INTO leaderboard_settings (appid, enabled, config, updated_by)
VALUES ($1, $2, $3, $4)
ON CONFLICT (appid) DO UPDATE SET enabled = EXCLUDED.enabled, config = EXCLUDED.config,
    updated_by = EXCLUDED.updated_by, updated_at = NOW()`, appID, enabled, raw, nullableString(operator)); err != nil {
		return nil, err
	}
	return r.GetLeaderboardSettings(ctx, appID)
}

// ListEnabledLeaderboardSettings 开启了排行榜的全部应用（结算循环用）。
func (r *Repository) ListEnabledLeaderboardSettings(ctx context.Context) ([]leaderboard.Settings, error) {
	rows, err := r.pool.Query(ctx, `SELECT s.appid, s.config FROM leaderboard_settings s
JOIN apps a ON a.id = s.appid
WHERE s.enabled = true ORDER BY s.appid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []leaderboard.Settings{}
	for rows.Next() {
		settings := leaderboard.Settings{Enabled: true, Configured: true, Config: leaderboard.DefaultConfig()}
		var raw []byte
		if err := rows.Scan(&settings.AppID, &raw); err != nil {
			return nil, err
		}
		if err := decodeLeaderboardConfig(raw, &settings.Config); err != nil {
			return nil, err
		}
		out = append(out, settings)
	}
	return out, rows.Err()
}

// ── 偏好 ──

// GetLeaderboardPreference 用户的匿名上榜偏好（没设置过为 false）。
func (r *Repository) GetLeaderboardPreference(ctx context.Context, appID, userID int64) (bool, error) {
	var anonymous bool
	err := r.pool.QueryRow(ctx, `SELECT anonymous FROM leaderboard_preferences WHERE appid = $1 AND user_id = $2`,
		appID, userID).Scan(&anonymous)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return anonymous, err
}

// SetLeaderboardPreference 保存匿名上榜偏好。
func (r *Repository) SetLeaderboardPreference(ctx context.Context, appID, userID int64, anonymous bool) error {
	tag, err := r.pool.Exec(ctx, `INSERT INTO leaderboard_preferences (appid, user_id, anonymous)
SELECT $1, u.id, $3 FROM users u WHERE u.id = $2 AND u.appid = $1
ON CONFLICT (appid, user_id) DO UPDATE SET anonymous = EXCLUDED.anonymous, updated_at = NOW()`, appID, userID, anonymous)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrUserNotFound
	}
	return nil
}

// ── 移出名单 ──

// LeaderboardExcluded 这个人是否被移出排行榜。
func (r *Repository) LeaderboardExcluded(ctx context.Context, appID, userID int64) (bool, error) {
	var excluded bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM leaderboard_exclusions WHERE appid = $1 AND user_id = $2)`,
		appID, userID).Scan(&excluded)
	return excluded, err
}

// ListLeaderboardExclusions 移出名单（keyword 匹配账号、昵称或用户 ID）。
func (r *Repository) ListLeaderboardExclusions(ctx context.Context, appID int64, keyword string, page, limit int) (*leaderboard.Page[leaderboard.Exclusion], error) {
	page, limit = leaderboard.NormalizePage(page, limit, 20, leaderboard.MaxAdminPageLimit)
	where := `x.appid = $1`
	args := []any{appID}
	if keyword = strings.TrimSpace(keyword); keyword != "" {
		args = append(args, "%"+escapeLikePattern(keyword)+"%")
		where += ` AND (u.account ILIKE $2 OR COALESCE(p.nickname, '') ILIKE $2 OR u.id::text = $3)`
		args = append(args, keyword)
	}
	from := `FROM leaderboard_exclusions x JOIN users u ON u.id = x.user_id LEFT JOIN user_profiles p ON p.user_id = x.user_id WHERE ` + where
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) `+from, args...).Scan(&total); err != nil {
		return nil, err
	}
	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, `SELECT x.appid, x.user_id, u.account, COALESCE(p.nickname, ''), x.reason, x.operator, x.created_at `+
		from+` ORDER BY x.created_at DESC, x.user_id DESC LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []leaderboard.Exclusion{}
	for rows.Next() {
		var item leaderboard.Exclusion
		if err := rows.Scan(&item.AppID, &item.UserID, &item.Account, &item.Nickname, &item.Reason, &item.Operator, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &leaderboard.Page[leaderboard.Exclusion]{Items: items, Total: total, Page: page, Limit: limit,
		TotalPages: leaderboard.TotalPages(total, limit)}, nil
}

// AddLeaderboardExclusions 移出一批人（已在名单里的更新原因）。返回本应用里不存在的用户 ID。
func (r *Repository) AddLeaderboardExclusions(ctx context.Context, appID int64, userIDs []int64, reason, operator string) ([]int64, error) {
	rows, err := r.pool.Query(ctx, `INSERT INTO leaderboard_exclusions (appid, user_id, reason, operator)
SELECT $1, u.id, $3, $4 FROM users u WHERE u.appid = $1 AND u.id = ANY($2::bigint[])
ON CONFLICT (appid, user_id) DO UPDATE SET reason = EXCLUDED.reason, operator = EXCLUDED.operator
RETURNING user_id`, appID, userIDs, reason, operator)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		found[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	missing := []int64{}
	for _, id := range userIDs {
		if !found[id] {
			missing = append(missing, id)
		}
	}
	return missing, nil
}

// RemoveLeaderboardExclusion 把一个人移回排行榜。
func (r *Repository) RemoveLeaderboardExclusion(ctx context.Context, appID, userID int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM leaderboard_exclusions WHERE appid = $1 AND user_id = $2`, appID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrLeaderboardExclusionNotFound
	}
	return nil
}

// ── 实时名次 ──

// LeaderboardQuery 一个榜一个周期的取数参数。
type LeaderboardQuery struct {
	AppID            int64
	Board            leaderboard.BoardSpec
	Period           leaderboard.Period
	MinValue         int64
	CountAdminAdjust bool
	// IncludeExcluded 管理端：被移出的人也列出来（标 excluded、不参与排名）
	IncludeExcluded bool
}

// LeaderboardRow 榜上的一行原始事实（含真实账号，只在服务端内部流转）。
type LeaderboardRow struct {
	Rank       int
	Position   int
	UserID     int64
	Account    string
	Nickname   string
	RawAvatar  string
	Anonymous  bool
	Excluded   bool
	Value      int64
	Experience int64
	Level      int
	LevelName  string
	LastAt     *time.Time
}

// leaderboardEligibleSQL 参与资格（别名 u = users）：本应用启用、未冻结、没有生效中的封禁。
// 与 enforceAccountBan 同一判定：有生效中的封禁记录（含「仅限制登录」一档）就不上榜；
// 没有记录时回落到投影（enabled / disabled_end_time），兜住迁移来的老数据。
const leaderboardEligibleSQL = `(u.enabled = true
    AND (u.disabled_end_time IS NULL OR u.disabled_end_time <= NOW())
    AND NOT EXISTS (
        SELECT 1 FROM user_account_bans b
        WHERE b.appid = u.appid AND b.user_id = u.id AND b.status = 'active'
          AND b.start_at <= NOW() AND (b.end_at IS NULL OR b.end_at > NOW())))`

// leaderboardLevelJoin 当前等级（与旧等级榜同一口径：经验达到的最高启用等级，默认 1 级「新手」）。
const leaderboardLevelJoin = `LEFT JOIN LATERAL (
    SELECT level, level_name FROM user_levels
    WHERE is_active = true AND experience_required <= u.experience
    ORDER BY level DESC LIMIT 1) lv ON true`

type sqlArgs struct{ values []any }

func (a *sqlArgs) add(value any) string {
	a.values = append(a.values, value)
	return "$" + strconv.Itoa(len(a.values))
}

// earnedFilterSQL 周期内「获得的积分」的流水条件（别名 t = integral_transactions）。
// 与 leaderboard.Counts 同一口径（TestLeaderboardIntegration 在真实库上逐类核对）。
func earnedFilterSQL(args *sqlArgs, q LeaderboardQuery) string {
	return `t.appid = $1 AND t.created_at >= ` + args.add(q.Period.Start) + ` AND t.created_at < ` + args.add(q.Period.End) + `
    AND t.amount > 0 AND t.status = 'completed'
    AND NOT (t.category = ANY(` + args.add(leaderboard.ExcludedCategories()) + `::text[]))
    AND (` + args.add(q.CountAdminAdjust) + `::boolean OR NOT (t.category = '` + leaderboard.CategoryAdminAdjust + `'
        AND COALESCE(t.source_type, '') = '` + leaderboard.AdminAdjustSourceType + `' AND t.source_id IS NOT NULL))`
}

// leaderboardScoredCTE 取值 ≥ minValue 的参与者（含被移出的人，由 excluded 列区分）。
// 列：user_id, account, nickname, avatar, anonymous, excluded, value, experience, level, level_name, last_at, rank_key。
// rank_key 是排名依据：等级榜按经验（等级由经验决定，同级比经验即全序），其余按取值。
func leaderboardScoredCTE(args *sqlArgs, q LeaderboardQuery) string {
	var base string
	switch {
	case q.Board.Metric == leaderboard.MetricPoints && q.Board.Periodic():
		base = `earned AS (
    SELECT t.user_id, SUM(t.amount)::BIGINT AS value, MAX(t.created_at) AS last_at
    FROM integral_transactions t
    WHERE ` + earnedFilterSQL(args, q) + `
    GROUP BY t.user_id
), base AS (
    SELECT u.id AS user_id, u.account, e.value, u.experience, 0 AS level, ''::text AS level_name, e.last_at, e.value AS rank_key
    FROM earned e JOIN users u ON u.id = e.user_id AND u.appid = $1
    WHERE ` + leaderboardEligibleSQL + `
)`
	case q.Board.Metric == leaderboard.MetricPoints:
		base = `base AS (
    SELECT u.id AS user_id, u.account, u.integral AS value, u.experience, 0 AS level, ''::text AS level_name,
        NULL::timestamptz AS last_at, u.integral AS rank_key
    FROM users u WHERE u.appid = $1 AND ` + leaderboardEligibleSQL + `
)`
	case q.Board.Metric == leaderboard.MetricExperience:
		base = `base AS (
    SELECT u.id AS user_id, u.account, u.experience AS value, u.experience, COALESCE(lv.level, 1) AS level,
        COALESCE(lv.level_name, '新手')::text AS level_name, NULL::timestamptz AS last_at, u.experience AS rank_key
    FROM users u ` + leaderboardLevelJoin + `
    WHERE u.appid = $1 AND ` + leaderboardEligibleSQL + `
)`
	default: // 等级榜
		base = `base AS (
    SELECT u.id AS user_id, u.account, COALESCE(lv.level, 1)::BIGINT AS value, u.experience, COALESCE(lv.level, 1) AS level,
        COALESCE(lv.level_name, '新手')::text AS level_name, NULL::timestamptz AS last_at, u.experience AS rank_key
    FROM users u ` + leaderboardLevelJoin + `
    WHERE u.appid = $1 AND ` + leaderboardEligibleSQL + `
)`
	}
	scored := `, scored AS (
    SELECT b.*, COALESCE(p.nickname, '') AS nickname, COALESCE(p.avatar, '') AS avatar,
        COALESCE(pref.anonymous, false) AS anonymous, (x.user_id IS NOT NULL) AS excluded
    FROM base b
    LEFT JOIN user_profiles p ON p.user_id = b.user_id
    LEFT JOIN leaderboard_preferences pref ON pref.appid = $1 AND pref.user_id = b.user_id
    LEFT JOIN leaderboard_exclusions x ON x.appid = $1 AND x.user_id = b.user_id
    WHERE b.value >= ` + args.add(q.MinValue)
	if !q.IncludeExcluded {
		scored += ` AND x.user_id IS NULL`
	}
	return "WITH " + base + scored + "\n)"
}

// LeaderboardSlice 榜单的一段与总数。
type LeaderboardSlice struct {
	Rows []LeaderboardRow
	// Participants 未被移出的参与者总数（不限 topLimit）
	Participants int64
	// TotalRows 列出的总行数（IncludeExcluded 时含被移出的人）
	TotalRows int64
}

// LeaderboardRows 按展示顺序取一段（名次 RANK 并列；同名次内周期榜先到先得，再按用户 ID）。
func (r *Repository) LeaderboardRows(ctx context.Context, q LeaderboardQuery, limit, offset int) (*LeaderboardSlice, error) {
	return leaderboardRows(ctx, r.pool, q, limit, offset)
}

func leaderboardRows(ctx context.Context, db queryExecutor, q LeaderboardQuery, limit, offset int) (*LeaderboardSlice, error) {
	args := &sqlArgs{values: []any{q.AppID}}
	cte := leaderboardScoredCTE(args, q)
	limitArg, offsetArg := args.add(limit), args.add(offset)
	rows, err := db.Query(ctx, cte+`, ranked AS (
    SELECT s.*,
        RANK() OVER (PARTITION BY s.excluded ORDER BY s.rank_key DESC) AS rank,
        ROW_NUMBER() OVER (ORDER BY s.rank_key DESC, s.last_at ASC NULLS LAST, s.user_id ASC) AS position,
        COUNT(*) FILTER (WHERE NOT s.excluded) OVER () AS participants,
        COUNT(*) OVER () AS total_rows
    FROM scored s
)
SELECT rank, position, user_id, account, nickname, avatar, anonymous, excluded, value, experience, level, level_name,
    last_at, participants, total_rows
FROM ranked ORDER BY position LIMIT `+limitArg+` OFFSET `+offsetArg, args.values...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := &LeaderboardSlice{Rows: []LeaderboardRow{}}
	for rows.Next() {
		var row LeaderboardRow
		var rank, position int64
		if err := rows.Scan(&rank, &position, &row.UserID, &row.Account, &row.Nickname, &row.RawAvatar, &row.Anonymous,
			&row.Excluded, &row.Value, &row.Experience, &row.Level, &row.LevelName, &row.LastAt,
			&out.Participants, &out.TotalRows); err != nil {
			return nil, err
		}
		row.Rank, row.Position = int(rank), int(position)
		out.Rows = append(out.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out.Rows) == 0 && offset > 0 {
		// 翻过了头：总数另查一次
		countArgs := &sqlArgs{values: []any{q.AppID}}
		if err := db.QueryRow(ctx, leaderboardScoredCTE(countArgs, q)+` SELECT COUNT(*) FILTER (WHERE NOT excluded), COUNT(*) FROM scored`,
			countArgs.values...).Scan(&out.Participants, &out.TotalRows); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// LeaderboardStanding 一个人在这个榜这个周期的实时位置：取值、名次（比我大的人数 + 1）、距上一名的差距。
// 用户不存在时返回 ErrUserNotFound。
func (r *Repository) LeaderboardStanding(ctx context.Context, q LeaderboardQuery, userID int64) (*leaderboard.Standing, error) {
	args := &sqlArgs{values: []any{q.AppID}}
	userArg := args.add(userID)
	valueExpr, rankKeyExpr := "u.integral", "u.integral"
	switch {
	case q.Board.Metric == leaderboard.MetricPoints && q.Board.Periodic():
		valueExpr = `(SELECT COALESCE(SUM(t.amount), 0)::BIGINT FROM integral_transactions t
        WHERE t.user_id = u.id AND ` + earnedFilterSQL(args, q) + `)`
		rankKeyExpr = valueExpr
	case q.Board.Metric == leaderboard.MetricExperience:
		valueExpr, rankKeyExpr = "u.experience", "u.experience"
	case q.Board.Metric == leaderboard.MetricLevel:
		valueExpr, rankKeyExpr = "COALESCE(lv.level, 1)::BIGINT", "u.experience"
	}
	standing := leaderboard.Standing{UserID: userID}
	var rankKey int64
	err := r.pool.QueryRow(ctx, `SELECT u.account, COALESCE(p.nickname, ''), COALESCE(p.avatar, ''),
    COALESCE(pref.anonymous, false), (x.user_id IS NOT NULL), `+leaderboardEligibleSQL+`,
    `+valueExpr+`, u.experience, COALESCE(lv.level_name, '新手'), COALESCE(lv.level, 1), `+rankKeyExpr+`
FROM users u
LEFT JOIN user_profiles p ON p.user_id = u.id
LEFT JOIN leaderboard_preferences pref ON pref.appid = $1 AND pref.user_id = u.id
LEFT JOIN leaderboard_exclusions x ON x.appid = $1 AND x.user_id = u.id
`+leaderboardLevelJoin+`
WHERE u.id = `+userArg+` AND u.appid = $1`, args.values...).Scan(&standing.Account, &standing.Nickname, &standing.RawAvatar,
		&standing.Anonymous, &standing.Excluded, &standing.Eligible, &standing.Value, &standing.Experience,
		&standing.LevelName, &standing.Level, &rankKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrUserNotFound
	}
	if err != nil {
		return nil, err
	}
	if q.Board.Metric != leaderboard.MetricExperience && q.Board.Metric != leaderboard.MetricLevel {
		standing.LevelName = ""
	}
	if !standing.Eligible || standing.Excluded || standing.Value < q.MinValue {
		return &standing, nil
	}
	countArgs := &sqlArgs{values: []any{q.AppID}}
	countQuery := q
	countQuery.IncludeExcluded = false
	cte := leaderboardScoredCTE(countArgs, countQuery)
	var ahead, nextKey int64
	if err := r.pool.QueryRow(ctx, cte+` SELECT COUNT(*), COALESCE(MIN(rank_key), 0)::BIGINT FROM scored WHERE rank_key > `+
		countArgs.add(rankKey), countArgs.values...).Scan(&ahead, &nextKey); err != nil {
		return nil, err
	}
	rank := int(ahead) + 1
	standing.Rank = &rank
	if ahead > 0 {
		standing.GapToPrev = nextKey - rankKey
	}
	return &standing, nil
}

// ── 结算快照 ──

// LeaderboardSettleInput 结算一期所需的全部参数（设置在调用方读取，这一刻的设置即这一期的规则）。
type LeaderboardSettleInput struct {
	Query          LeaderboardQuery
	TopLimit       int
	Identity       string
	AllowAnonymous bool
	Tiers          []leaderboard.Tier
	Plans          map[int64]pointstore.PlanRef
	Features       map[string]pointstore.FeatureRef
	Operator       string
}

const leaderboardSnapshotColumns = `s.id, s.appid, s.board, s.period_key, s.period_start, s.period_end, s.status, s.participants,
s.reward_tiers, s.settled_by, s.settled_at, s.created_at`

func scanLeaderboardSnapshot(row interface{ Scan(dest ...any) error }, extra ...any) (*leaderboard.Snapshot, error) {
	var snapshot leaderboard.Snapshot
	var tiers []byte
	dest := []any{&snapshot.ID, &snapshot.AppID, &snapshot.Board, &snapshot.PeriodKey, &snapshot.PeriodStart, &snapshot.PeriodEnd,
		&snapshot.Status, &snapshot.Participants, &tiers, &snapshot.SettledBy, &snapshot.SettledAt, &snapshot.CreatedAt}
	if err := row.Scan(append(dest, extra...)...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrLeaderboardSnapshotNotFound
		}
		return nil, err
	}
	snapshot.Tiers = []leaderboard.Tier{}
	if len(tiers) > 0 {
		if err := json.Unmarshal(tiers, &snapshot.Tiers); err != nil {
			return nil, fmt.Errorf("decode leaderboard tiers: %w", err)
		}
	}
	return &snapshot, nil
}

// SettleLeaderboardPeriod 结算一期：抢到快照行（ON CONFLICT DO NOTHING）的实例在同一个事务里写完前 topLimit 名、
// 按档位写入应发的奖励（pending），再把快照置为 settled。已有快照时原样返回它（created = false）。
func (r *Repository) SettleLeaderboardPeriod(ctx context.Context, in LeaderboardSettleInput) (*leaderboard.Snapshot, bool, error) {
	q := in.Query
	tiersJSON, err := json.Marshal(nonNilTiers(in.Tiers))
	if err != nil {
		return nil, false, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var snapshotID int64
	err = tx.QueryRow(ctx, `INSERT INTO leaderboard_snapshots (appid, board, period_key, period_start, period_end, status, reward_tiers, settled_by)
VALUES ($1, $2, $3, $4, $5, 'settling', $6, $7)
ON CONFLICT (appid, board, period_key) DO NOTHING
RETURNING id`, q.AppID, q.Board.Key, q.Period.Key, q.Period.Start, q.Period.End, tiersJSON, settledBy(in.Operator)).Scan(&snapshotID)
	if errors.Is(err, pgx.ErrNoRows) {
		// 别的实例（或更早的一轮）已经结算过这一期
		_ = tx.Rollback(ctx)
		snapshot, err := r.LeaderboardSnapshotByPeriod(ctx, q.AppID, q.Board.Key, q.Period.Key)
		return snapshot, false, err
	}
	if err != nil {
		return nil, false, err
	}

	q.IncludeExcluded = false
	slice, err := leaderboardRows(ctx, tx, q, in.TopLimit, 0)
	if err != nil {
		return nil, false, err
	}
	participants := slice.Participants
	batch := &pgx.Batch{}
	for _, row := range slice.Rows {
		anonymous := in.AllowAnonymous && row.Anonymous
		avatar := row.RawAvatar
		if anonymous {
			avatar = ""
		}
		levelName := ""
		if q.Board.Metric == leaderboard.MetricExperience || q.Board.Metric == leaderboard.MetricLevel {
			levelName = leaderboard.LevelLabel(row.Level, row.LevelName)
		}
		status := leaderboard.RewardNone
		rewards := []leaderboard.Reward{}
		results := []leaderboard.RewardResult{}
		if tier, ok := leaderboard.TierFor(in.Tiers, row.Rank); ok {
			status = leaderboard.RewardPending
			rewards = invite.NormalizeRewards(tier.Rewards)
			for _, described := range invite.DescribeRewards(rewards, in.Plans, in.Features) {
				results = append(results, leaderboard.RewardResult{Type: described.Type, Label: described.Label, Detail: described.Detail})
			}
		}
		rewardsJSON, _ := json.Marshal(rewards)
		resultsJSON, _ := json.Marshal(results)
		batch.Queue(`INSERT INTO leaderboard_snapshot_entries (snapshot_id, appid, user_id, rank, position, value, display_name,
    avatar, anonymous, level_name, rewards, reward_status, reward_results)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)`,
			snapshotID, q.AppID, row.UserID, row.Rank, row.Position, row.Value,
			leaderboard.DisplayName(in.Identity, row.Nickname, row.Account), avatar, anonymous, levelName,
			rewardsJSON, status, resultsJSON)
	}
	if batch.Len() > 0 {
		results := tx.SendBatch(ctx, batch)
		for i := 0; i < batch.Len(); i++ {
			if _, err := results.Exec(); err != nil {
				_ = results.Close()
				return nil, false, err
			}
		}
		if err := results.Close(); err != nil {
			return nil, false, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE leaderboard_snapshots SET status = 'settled', participants = $2, settled_at = NOW()
WHERE id = $1`, snapshotID, participants); err != nil {
		return nil, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, false, err
	}
	snapshot, err := r.GetLeaderboardSnapshot(ctx, q.AppID, snapshotID)
	return snapshot, true, err
}

func nonNilTiers(tiers []leaderboard.Tier) []leaderboard.Tier {
	if tiers == nil {
		return []leaderboard.Tier{}
	}
	return tiers
}

func settledBy(operator string) string {
	if operator = strings.TrimSpace(operator); operator != "" {
		return operator
	}
	return "system"
}

// LeaderboardSnapshotExists 这一期是否已有快照（结算循环每轮先问一句，避免空开事务）。
func (r *Repository) LeaderboardSnapshotExists(ctx context.Context, appID int64, board, periodKey string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM leaderboard_snapshots WHERE appid = $1 AND board = $2 AND period_key = $3)`,
		appID, board, periodKey).Scan(&exists)
	return exists, err
}

// GetLeaderboardSnapshot 按 ID 取快照（限定应用）。
func (r *Repository) GetLeaderboardSnapshot(ctx context.Context, appID, snapshotID int64) (*leaderboard.Snapshot, error) {
	return scanLeaderboardSnapshot(r.pool.QueryRow(ctx, `SELECT `+leaderboardSnapshotColumns+`
FROM leaderboard_snapshots s WHERE s.appid = $1 AND s.id = $2`, appID, snapshotID))
}

// LeaderboardSnapshotByPeriod 按周期取快照。
func (r *Repository) LeaderboardSnapshotByPeriod(ctx context.Context, appID int64, board, periodKey string) (*leaderboard.Snapshot, error) {
	return scanLeaderboardSnapshot(r.pool.QueryRow(ctx, `SELECT `+leaderboardSnapshotColumns+`
FROM leaderboard_snapshots s WHERE s.appid = $1 AND s.board = $2 AND s.period_key = $3`, appID, board, periodKey))
}

const leaderboardSnapshotStatsSQL = `(SELECT COUNT(*) FROM leaderboard_snapshot_entries e WHERE e.snapshot_id = s.id),
(SELECT COUNT(*) FILTER (WHERE e.reward_status <> 'none') FROM leaderboard_snapshot_entries e WHERE e.snapshot_id = s.id),
(SELECT COUNT(*) FILTER (WHERE e.reward_status = 'pending') FROM leaderboard_snapshot_entries e WHERE e.snapshot_id = s.id),
(SELECT COUNT(*) FILTER (WHERE e.reward_status = 'granted') FROM leaderboard_snapshot_entries e WHERE e.snapshot_id = s.id),
(SELECT COUNT(*) FILTER (WHERE e.reward_status = 'failed') FROM leaderboard_snapshot_entries e WHERE e.snapshot_id = s.id),
(SELECT COUNT(*) FILTER (WHERE e.reward_status = 'skipped') FROM leaderboard_snapshot_entries e WHERE e.snapshot_id = s.id)`

// ListLeaderboardSnapshots 结算记录（board 为空为全部榜），带发放统计，新的在前。
func (r *Repository) ListLeaderboardSnapshots(ctx context.Context, appID int64, board string, page, limit int) (*leaderboard.Page[leaderboard.Snapshot], error) {
	page, limit = leaderboard.NormalizePage(page, limit, 20, leaderboard.MaxAdminPageLimit)
	where := `s.appid = $1 AND s.status = 'settled'`
	args := []any{appID}
	if board = strings.TrimSpace(board); board != "" {
		args = append(args, board)
		where += ` AND s.board = $2`
	}
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM leaderboard_snapshots s WHERE `+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, `SELECT `+leaderboardSnapshotColumns+`, `+leaderboardSnapshotStatsSQL+`
FROM leaderboard_snapshots s WHERE `+where+` ORDER BY s.period_end DESC, s.id DESC
LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []leaderboard.Snapshot{}
	for rows.Next() {
		stats := &leaderboard.SnapshotStats{}
		snapshot, err := scanLeaderboardSnapshot(rows, &stats.Entries, &stats.Rewarded, &stats.Pending, &stats.Granted, &stats.Failed, &stats.Skipped)
		if err != nil {
			return nil, err
		}
		snapshot.Stats = stats
		items = append(items, *snapshot)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &leaderboard.Page[leaderboard.Snapshot]{Items: items, Total: total, Page: page, Limit: limit,
		TotalPages: leaderboard.TotalPages(total, limit)}, nil
}

// LeaderboardSnapshotStats 一期的发放统计。
func (r *Repository) LeaderboardSnapshotStats(ctx context.Context, snapshotID int64) (*leaderboard.SnapshotStats, error) {
	stats := &leaderboard.SnapshotStats{}
	err := r.pool.QueryRow(ctx, `SELECT `+leaderboardSnapshotStatsSQL+` FROM leaderboard_snapshots s WHERE s.id = $1`, snapshotID).
		Scan(&stats.Entries, &stats.Rewarded, &stats.Pending, &stats.Granted, &stats.Failed, &stats.Skipped)
	return stats, err
}

// ListLeaderboardPeriodSnapshots 一个榜的往期（已结算），新的在前。
func (r *Repository) ListLeaderboardPeriodSnapshots(ctx context.Context, appID int64, board string, limit int) ([]leaderboard.Snapshot, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+leaderboardSnapshotColumns+`
FROM leaderboard_snapshots s WHERE s.appid = $1 AND s.board = $2 AND s.status = 'settled'
ORDER BY s.period_end DESC, s.id DESC LIMIT $3`, appID, board, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []leaderboard.Snapshot{}
	for rows.Next() {
		snapshot, err := scanLeaderboardSnapshot(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *snapshot)
	}
	return out, rows.Err()
}

// LatestLeaderboardSettlements 每个榜最近一次结算的时刻。
func (r *Repository) LatestLeaderboardSettlements(ctx context.Context, appID int64) (map[string]time.Time, error) {
	rows, err := r.pool.Query(ctx, `SELECT board, MAX(settled_at) FROM leaderboard_snapshots
WHERE appid = $1 AND status = 'settled' AND settled_at IS NOT NULL GROUP BY board`, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var board string
		var at time.Time
		if err := rows.Scan(&board, &at); err != nil {
			return nil, err
		}
		out[board] = at
	}
	return out, rows.Err()
}

const leaderboardEntryColumns = `e.snapshot_id, e.rank, e.position, e.user_id, e.value, e.display_name, e.avatar, e.anonymous,
e.level_name, e.rewards, e.reward_status, e.reward_results, e.reward_error, e.granted_at`

func scanLeaderboardEntry(row interface{ Scan(dest ...any) error }, extra ...any) (*leaderboard.SnapshotEntry, error) {
	var entry leaderboard.SnapshotEntry
	var rewards, results []byte
	dest := []any{&entry.SnapshotID, &entry.Rank, &entry.Position, &entry.UserID, &entry.Value, &entry.DisplayName,
		&entry.RawAvatar, &entry.Anonymous, &entry.LevelName, &rewards, &entry.RewardStatus, &results, &entry.RewardError, &entry.GrantedAt}
	if err := row.Scan(append(dest, extra...)...); err != nil {
		return nil, err
	}
	entry.Rewards = []leaderboard.Reward{}
	entry.RewardResults = []leaderboard.RewardResult{}
	if len(rewards) > 0 {
		if err := json.Unmarshal(rewards, &entry.Rewards); err != nil {
			return nil, fmt.Errorf("decode leaderboard entry rewards: %w", err)
		}
	}
	if len(results) > 0 {
		if err := json.Unmarshal(results, &entry.RewardResults); err != nil {
			return nil, fmt.Errorf("decode leaderboard entry results: %w", err)
		}
	}
	return &entry, nil
}

// ListLeaderboardSnapshotEntries 快照条目，按展示顺序；status 为空为全部。withAccount 时关联当前账号（管理端）。
func (r *Repository) ListLeaderboardSnapshotEntries(ctx context.Context, snapshotID int64, status string, page, limit, maxLimit int, withAccount bool) (*leaderboard.Page[leaderboard.SnapshotEntry], error) {
	page, limit = leaderboard.NormalizePage(page, limit, 20, maxLimit)
	where := `e.snapshot_id = $1`
	args := []any{snapshotID}
	if status = strings.TrimSpace(status); status != "" {
		args = append(args, status)
		where += ` AND e.reward_status = $2`
	}
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM leaderboard_snapshot_entries e WHERE `+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	args = append(args, limit, (page-1)*limit)
	accountCol := `''::text`
	join := ``
	if withAccount {
		accountCol = `COALESCE(u.account, '')`
		join = ` LEFT JOIN users u ON u.id = e.user_id AND u.appid = e.appid`
	}
	rows, err := r.pool.Query(ctx, `SELECT `+leaderboardEntryColumns+`, `+accountCol+`
FROM leaderboard_snapshot_entries e`+join+` WHERE `+where+` ORDER BY e.position
LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []leaderboard.SnapshotEntry{}
	for rows.Next() {
		var account string
		entry, err := scanLeaderboardEntry(rows, &account)
		if err != nil {
			return nil, err
		}
		entry.Account = account
		items = append(items, *entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &leaderboard.Page[leaderboard.SnapshotEntry]{Items: items, Total: total, Page: page, Limit: limit,
		TotalPages: leaderboard.TotalPages(total, limit)}, nil
}

// LeaderboardEntryOf 某人在某一期的条目（没上榜为 nil）。
func (r *Repository) LeaderboardEntryOf(ctx context.Context, snapshotID, userID int64) (*leaderboard.SnapshotEntry, error) {
	entry, err := scanLeaderboardEntry(r.pool.QueryRow(ctx, `SELECT `+leaderboardEntryColumns+`
FROM leaderboard_snapshot_entries e WHERE e.snapshot_id = $1 AND e.user_id = $2`, snapshotID, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return entry, err
}

// LeaderboardEntriesOf 某人在一组快照里的条目（往期列表的「我的名次」）。
func (r *Repository) LeaderboardEntriesOf(ctx context.Context, appID, userID int64, snapshotIDs []int64) (map[int64]leaderboard.SnapshotEntry, error) {
	out := map[int64]leaderboard.SnapshotEntry{}
	if len(snapshotIDs) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT `+leaderboardEntryColumns+`
FROM leaderboard_snapshot_entries e WHERE e.appid = $1 AND e.user_id = $2 AND e.snapshot_id = ANY($3::bigint[])`,
		appID, userID, snapshotIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		entry, err := scanLeaderboardEntry(rows)
		if err != nil {
			return nil, err
		}
		out[entry.SnapshotID] = *entry
	}
	return out, rows.Err()
}

// ListLeaderboardRewardRecords 我的榜单奖励记录（落在档位里的条目），新的在前。
func (r *Repository) ListLeaderboardRewardRecords(ctx context.Context, appID, userID int64, page, limit int) ([]leaderboard.RewardRecord, int64, error) {
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM leaderboard_snapshot_entries e
WHERE e.appid = $1 AND e.user_id = $2 AND e.reward_status <> 'none'`, appID, userID).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.pool.Query(ctx, `SELECT s.id, s.board, s.period_key, s.period_start, s.period_end, e.rank, e.value,
    e.reward_status, e.reward_results, e.granted_at
FROM leaderboard_snapshot_entries e JOIN leaderboard_snapshots s ON s.id = e.snapshot_id
WHERE e.appid = $1 AND e.user_id = $2 AND e.reward_status <> 'none'
ORDER BY s.period_end DESC, s.id DESC LIMIT $3 OFFSET $4`, appID, userID, limit, (page-1)*limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []leaderboard.RewardRecord{}
	for rows.Next() {
		var record leaderboard.RewardRecord
		var results []byte
		if err := rows.Scan(&record.SnapshotID, &record.Board, &record.PeriodKey, &record.PeriodStart, &record.PeriodEnd,
			&record.Rank, &record.Value, &record.Status, &results, &record.GrantedAt); err != nil {
			return nil, 0, err
		}
		record.RewardResults = []leaderboard.RewardResult{}
		if len(results) > 0 {
			if err := json.Unmarshal(results, &record.RewardResults); err != nil {
				return nil, 0, err
			}
		}
		out = append(out, record)
	}
	return out, total, rows.Err()
}

// RetryLeaderboardSnapshot 把这一期全部 failed 的条目置回 pending（由结算循环发放）。返回置回的条数。
func (r *Repository) RetryLeaderboardSnapshot(ctx context.Context, appID, snapshotID int64) (int64, error) {
	if _, err := r.GetLeaderboardSnapshot(ctx, appID, snapshotID); err != nil {
		return 0, err
	}
	tag, err := r.pool.Exec(ctx, `UPDATE leaderboard_snapshot_entries SET reward_status = 'pending', reward_error = '', updated_at = NOW()
WHERE snapshot_id = $1 AND reward_status = 'failed'`, snapshotID)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// LeaderboardRewardBacklog 全部应用里待发放与发放失败的条数（运维观察用）。
func (r *Repository) LeaderboardRewardBacklog(ctx context.Context, appID int64) (pending, failed int64, err error) {
	err = r.pool.QueryRow(ctx, `SELECT COUNT(*) FILTER (WHERE reward_status = 'pending'), COUNT(*) FILTER (WHERE reward_status = 'failed')
FROM leaderboard_snapshot_entries WHERE appid = $1`, appID).Scan(&pending, &failed)
	return pending, failed, err
}

// ── 发放 ──

// LeaderboardGrantOutcome 一次发放的结论（结算循环据此发站内信）。
type LeaderboardGrantOutcome struct {
	Snapshot leaderboard.Snapshot
	Entry    leaderboard.SnapshotEntry
}

// GrantNextLeaderboardReward 认领一条待发放的条目（FOR UPDATE SKIP LOCKED），一人一个事务发完：
// 被移出或已注销记 skipped；发放失败回滚到保存点、记 failed 与原因；成功记 granted 并回写流水号。
// 没有待发放的条目时返回 nil。多实例同时跑互不重复：锁住的行别人跳过，状态一改就不再是 pending。
func (r *Repository) GrantNextLeaderboardReward(ctx context.Context) (*LeaderboardGrantOutcome, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var snapshotID, userID int64
	err = tx.QueryRow(ctx, `SELECT e.snapshot_id, e.user_id FROM leaderboard_snapshot_entries e
JOIN leaderboard_snapshots s ON s.id = e.snapshot_id
WHERE e.reward_status = 'pending' AND s.status = 'settled'
ORDER BY e.snapshot_id, e.rank, e.user_id
LIMIT 1
FOR UPDATE OF e SKIP LOCKED`).Scan(&snapshotID, &userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	snapshot, err := scanLeaderboardSnapshot(tx.QueryRow(ctx, `SELECT `+leaderboardSnapshotColumns+`
FROM leaderboard_snapshots s WHERE s.id = $1`, snapshotID))
	if err != nil {
		return nil, err
	}
	entry, err := scanLeaderboardEntry(tx.QueryRow(ctx, `SELECT `+leaderboardEntryColumns+`
FROM leaderboard_snapshot_entries e WHERE e.snapshot_id = $1 AND e.user_id = $2`, snapshotID, userID))
	if err != nil {
		return nil, err
	}

	var exists, excluded bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id = $2 AND appid = $1),
    EXISTS (SELECT 1 FROM leaderboard_exclusions WHERE appid = $1 AND user_id = $2)`, snapshot.AppID, userID).
		Scan(&exists, &excluded); err != nil {
		return nil, err
	}
	finish := func(status string, results []leaderboard.RewardResult, reason string) (*LeaderboardGrantOutcome, error) {
		resultsJSON, _ := json.Marshal(results)
		grantedAt := any(nil)
		if status == leaderboard.RewardGranted {
			grantedAt = time.Now().UTC()
		}
		if _, err := tx.Exec(ctx, `UPDATE leaderboard_snapshot_entries
SET reward_status = $3, reward_results = $4, reward_error = $5, granted_at = $6, updated_at = NOW()
WHERE snapshot_id = $1 AND user_id = $2`, snapshotID, userID, status, resultsJSON, reason, grantedAt); err != nil {
			return nil, err
		}
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
		entry.RewardStatus, entry.RewardResults, entry.RewardError = status, results, reason
		if status == leaderboard.RewardGranted {
			now := time.Now().UTC()
			entry.GrantedAt = &now
		}
		return &LeaderboardGrantOutcome{Snapshot: *snapshot, Entry: *entry}, nil
	}
	switch {
	case !exists:
		return finish(leaderboard.RewardSkipped, entry.RewardResults, "账号已注销")
	case excluded:
		return finish(leaderboard.RewardSkipped, entry.RewardResults, "已被移出排行榜")
	}

	plans, features, err := pointsStoreCatalogRefs(ctx, tx, snapshot.AppID)
	if err != nil {
		return nil, err
	}
	spec, _ := leaderboard.FindBoard(snapshot.Board)
	savepoint, err := tx.Begin(ctx)
	if err != nil {
		return nil, err
	}
	results, grantErr := r.grantLeaderboardRewardsTx(ctx, savepoint, leaderboardGrant{
		AppID: snapshot.AppID, UserID: userID, SnapshotID: snapshotID, Board: snapshot.Board, PeriodKey: snapshot.PeriodKey,
		Rank: entry.Rank, Title: spec.Label + "第 " + strconv.Itoa(entry.Rank) + " 名奖励", Plans: plans, Features: features,
	}, entry.Rewards)
	if grantErr != nil {
		_ = savepoint.Rollback(ctx)
		if errors.Is(grantErr, ErrUserNotFound) {
			return finish(leaderboard.RewardSkipped, entry.RewardResults, "账号已注销")
		}
		return finish(leaderboard.RewardFailed, entry.RewardResults, leaderboardGrantErrorText(grantErr))
	}
	if err := savepoint.Commit(ctx); err != nil {
		return nil, err
	}
	return finish(leaderboard.RewardGranted, results, "")
}

// leaderboardGrantErrorText 发放失败的原因（控制台展示给管理员看）。
func leaderboardGrantErrorText(err error) string {
	var balance *BalanceError
	switch {
	case errors.Is(err, ErrLeaderboardRewardUnavailable):
		return "奖励配置已失效：所选套餐已删除或为试用套餐、或功能已停用"
	case errors.As(err, &balance):
		return "积分或余额超出上限"
	}
	text := err.Error()
	if runes := []rune(text); len(runes) > 500 {
		text = string(runes[:500])
	}
	return text
}

// LeaderboardIdempotencyKey 一份奖励的幂等键：同一期同一个人只发一次。
func LeaderboardIdempotencyKey(snapshotID, userID int64) string {
	return "LB:" + strconv.FormatInt(snapshotID, 10) + ":" + strconv.FormatInt(userID, 10)
}
