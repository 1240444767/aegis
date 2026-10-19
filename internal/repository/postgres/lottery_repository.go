package postgres

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	lotterydomain "aegis/internal/domain/lottery"
	"aegis/internal/domain/pointstore"
	vipdomain "aegis/internal/domain/vip"
)

// 抽奖：活动与奖品的维护、单事务抽奖、人工发放的处理与统计。规则见 docs/lottery.md。

var (
	ErrLotteryActivityNotFound = errors.New("lottery activity not found")
	ErrLotteryDrawNotFound     = errors.New("lottery draw not found")
	// ErrLotteryDrawState 只有待发放的记录能被标记发放
	ErrLotteryDrawState = errors.New("lottery draw is not pending")
	// ErrLotteryActivityHasDraws 有抽奖记录的活动不能删除，只能结束
	ErrLotteryActivityHasDraws = errors.New("lottery activity has draws")
	// ErrLotteryPoolBroken 奖池里找不到可发的奖品（保存时的校验被绕过了）
	ErrLotteryPoolBroken = errors.New("lottery prize pool has no drawable prize")
	// ErrLotteryRewardUnavailable 奖品引用的套餐或功能已失效
	ErrLotteryRewardUnavailable = errors.New("lottery prize reward unavailable")
)

// LotteryUnavailableError 这一次不能抽（原因随错误带出，与 Chances 同一套判据）。
type LotteryUnavailableError struct {
	Reason string
}

func (e *LotteryUnavailableError) Error() string { return "lottery unavailable: " + e.Reason }

// LotteryStockError 改库存时把库存改到了已抽中数量以下。
type LotteryStockError struct {
	Position int
	Used     int64
}

func (e *LotteryStockError) Error() string {
	return fmt.Sprintf("lottery prize at position %d already used %d", e.Position, e.Used)
}

func lotteryUnavailable(reason string) error { return &LotteryUnavailableError{Reason: reason} }

// ── 扫描 ──

const lotteryActivityColumns = `a.id, a.appid, a.name, a.description, a.rules, a.ui_mode, a.status, a.start_time, a.end_time,
a.free_daily, a.points_cost, a.points_daily, a.video_scene, a.pity_threshold, a.pity_prize_id, a.sort_order,
COALESCE(a.created_by, ''), a.created_at, a.updated_at`

func scanLotteryActivity(row interface{ Scan(dest ...any) error }, extra ...any) (*lotterydomain.Activity, error) {
	var item lotterydomain.Activity
	dest := []any{
		&item.ID, &item.AppID, &item.Name, &item.Description, &item.Rules, &item.UIMode, &item.Status,
		&item.StartTime, &item.EndTime, &item.FreeDaily, &item.PointsCost, &item.PointsDaily, &item.VideoScene,
		&item.PityThreshold, &item.PityPrizeID, &item.SortOrder, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt,
	}
	if err := row.Scan(append(dest, extra...)...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrLotteryActivityNotFound
		}
		return nil, err
	}
	return &item, nil
}

const lotteryPrizeColumns = `p.id, p.activity_id, p.name, p.kind, p.reward, COALESCE(p.image_url, ''), p.quantity, p.used,
p.weight, p.position, p.is_default, p.created_at, p.updated_at`

func scanLotteryPrize(row interface{ Scan(dest ...any) error }) (*lotterydomain.Prize, error) {
	var item lotterydomain.Prize
	var reward []byte
	var quantity, used int32
	if err := row.Scan(&item.ID, &item.ActivityID, &item.Name, &item.Kind, &reward, &item.ImageURL, &quantity, &used,
		&item.Weight, &item.Position, &item.Fallback, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	item.Quantity, item.Used = int64(quantity), int64(used)
	if err := json.Unmarshal(reward, &item.Reward); err != nil || item.Reward.Type == "" {
		item.Reward = lotterydomain.Reward{Type: item.Kind}
	}
	return &item, nil
}

const lotteryDrawColumns = `d.id, d.draw_no, d.appid, d.activity_id, COALESCE(a.name, ''), d.user_id, COALESCE(u.account, ''),
COALESCE(up.nickname, ''), d.prize_id, d.prize_name, d.prize_kind, d.reward, d.position, d.source, d.points_cost, d.pity,
d.status, d.results, COALESCE(d.note, ''), COALESCE(d.handled_by, ''), d.handled_at, COALESCE(d.request_id, ''),
COALESCE(d.device_id, ''), COALESCE(d.client_ip, ''), d.created_at`

const lotteryDrawJoins = `FROM lottery_draws d
LEFT JOIN lottery_activities a ON a.id = d.activity_id
LEFT JOIN users u ON u.id = d.user_id
LEFT JOIN user_profiles up ON up.user_id = d.user_id`

func scanLotteryDraw(row interface{ Scan(dest ...any) error }) (*lotterydomain.Draw, error) {
	var item lotterydomain.Draw
	var reward, results []byte
	if err := row.Scan(&item.ID, &item.DrawNo, &item.AppID, &item.ActivityID, &item.ActivityName, &item.UserID,
		&item.Account, &item.Nickname, &item.PrizeID, &item.PrizeName, &item.PrizeKind, &reward, &item.Position,
		&item.Source, &item.PointsCost, &item.Pity, &item.Status, &results, &item.Note, &item.HandledBy,
		&item.HandledAt, &item.RequestID, &item.DeviceID, &item.ClientIP, &item.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrLotteryDrawNotFound
		}
		return nil, err
	}
	if err := json.Unmarshal(reward, &item.Reward); err != nil || item.Reward.Type == "" {
		item.Reward = lotterydomain.Reward{Type: item.PrizeKind}
	}
	_ = json.Unmarshal(results, &item.Results)
	if item.Results == nil {
		item.Results = []lotterydomain.RewardResult{}
	}
	return &item, nil
}

// ── 活动与奖品 ──

// GetLotteryActivity 取一个活动（必须属于这个应用）。
func (r *Repository) GetLotteryActivity(ctx context.Context, appID, activityID int64) (*lotterydomain.Activity, error) {
	return scanLotteryActivity(r.pool.QueryRow(ctx, `SELECT `+lotteryActivityColumns+`
FROM lottery_activities a WHERE a.id = $1 AND a.appid = $2`, activityID, appID))
}

// ListLotteryPrizes 一个活动的奖品，按位置排序。
func (r *Repository) ListLotteryPrizes(ctx context.Context, activityID int64) ([]lotterydomain.Prize, error) {
	return listLotteryPrizes(ctx, r.pool, activityID)
}

func listLotteryPrizes(ctx context.Context, q queryExecutor, activityID int64) ([]lotterydomain.Prize, error) {
	rows, err := q.Query(ctx, `SELECT `+lotteryPrizeColumns+`
FROM lottery_prizes p WHERE p.activity_id = $1 ORDER BY p.position, p.id`, activityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]lotterydomain.Prize, 0, 8)
	for rows.Next() {
		prize, err := scanLotteryPrize(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *prize)
	}
	return out, rows.Err()
}

// ListLotteryActivities 应用的活动（activeOnly 只取 active 状态，时间窗由调用方判）。
func (r *Repository) ListLotteryActivities(ctx context.Context, appID int64, activeOnly bool) ([]lotterydomain.Activity, error) {
	query := `SELECT ` + lotteryActivityColumns + ` FROM lottery_activities a WHERE a.appid = $1`
	if activeOnly {
		query += ` AND a.status = 'active'`
	}
	query += ` ORDER BY a.sort_order, a.id DESC`
	rows, err := r.pool.Query(ctx, query, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]lotterydomain.Activity, 0, 4)
	for rows.Next() {
		item, err := scanLotteryActivity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

// ListLotteryAdminActivities 管理端活动列表：活动、奖品与汇总。
func (r *Repository) ListLotteryAdminActivities(ctx context.Context, appID int64, dayStart time.Time) ([]lotterydomain.AdminActivity, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+lotteryActivityColumns+`,
  COALESCE(s.total_draws, 0), COALESCE(s.total_users, 0), COALESCE(s.today_draws, 0), COALESCE(s.pending, 0)
FROM lottery_activities a
LEFT JOIN LATERAL (
  SELECT COUNT(*) AS total_draws, COUNT(DISTINCT d.user_id) AS total_users,
         COUNT(*) FILTER (WHERE d.created_at >= $2) AS today_draws,
         COUNT(*) FILTER (WHERE d.status = 'pending') AS pending
  FROM lottery_draws d WHERE d.activity_id = a.id
) s ON TRUE
WHERE a.appid = $1
ORDER BY a.sort_order, a.id DESC`, appID, dayStart)
	if err != nil {
		return nil, err
	}
	out := make([]lotterydomain.AdminActivity, 0, 4)
	for rows.Next() {
		var item lotterydomain.AdminActivity
		activity, err := scanLotteryActivity(rows, &item.TotalDraws, &item.TotalUsers, &item.TodayDraws, &item.Pending)
		if err != nil {
			rows.Close()
			return nil, err
		}
		item.Activity = *activity
		out = append(out, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for index := range out {
		prizes, err := r.ListLotteryPrizes(ctx, out[index].ID)
		if err != nil {
			return nil, err
		}
		out[index].Prizes = prizes
	}
	return out, nil
}

// GetLotteryAdminActivity 管理端单个活动（含奖品与汇总）。
func (r *Repository) GetLotteryAdminActivity(ctx context.Context, appID, activityID int64, dayStart time.Time) (*lotterydomain.AdminActivity, error) {
	var item lotterydomain.AdminActivity
	activity, err := scanLotteryActivity(r.pool.QueryRow(ctx, `SELECT `+lotteryActivityColumns+`,
  (SELECT COUNT(*) FROM lottery_draws d WHERE d.activity_id = a.id),
  (SELECT COUNT(DISTINCT d.user_id) FROM lottery_draws d WHERE d.activity_id = a.id),
  (SELECT COUNT(*) FROM lottery_draws d WHERE d.activity_id = a.id AND d.created_at >= $3),
  (SELECT COUNT(*) FROM lottery_draws d WHERE d.activity_id = a.id AND d.status = 'pending')
FROM lottery_activities a WHERE a.id = $1 AND a.appid = $2`, activityID, appID, dayStart),
		&item.TotalDraws, &item.TotalUsers, &item.TodayDraws, &item.Pending)
	if err != nil {
		return nil, err
	}
	item.Activity = *activity
	item.Prizes, err = r.ListLotteryPrizes(ctx, activityID)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// SaveLotteryActivity 新建（activityID 为 0）或整体更新一个活动及其全部奖品（单事务）。
//
// 奖品按 ID 对上：带 ID 的更新（保留已抽中计数），不带的新建，原有而这次没带的删除
// （抽奖记录里有快照，删除奖品不影响历史）。保底奖按位置指定，存的是落库后的奖品 ID。
func (r *Repository) SaveLotteryActivity(ctx context.Context, activityID int64, input lotterydomain.ActivityInput) (*lotterydomain.Activity, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	if activityID == 0 {
		if err := tx.QueryRow(ctx, `INSERT INTO lottery_activities
(appid, name, description, rules, ui_mode, status, start_time, end_time, free_daily, points_cost, points_daily,
 video_scene, pity_threshold, sort_order, created_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) RETURNING id`,
			input.AppID, input.Name, input.Description, input.Rules, input.UIMode, input.Status, input.StartTime,
			input.EndTime, input.FreeDaily, input.PointsCost, input.PointsDaily, input.VideoScene, input.PityThreshold,
			input.SortOrder, nullableString(input.Operator)).Scan(&activityID); err != nil {
			return nil, err
		}
	} else {
		// 锁活动行：与进行中的抽奖（FOR SHARE）互斥，奖盘不会在一次抽奖的中途被换掉
		tag, err := tx.Exec(ctx, `UPDATE lottery_activities SET
  name = $3, description = $4, rules = $5, ui_mode = $6, status = $7, start_time = $8, end_time = $9,
  free_daily = $10, points_cost = $11, points_daily = $12, video_scene = $13, pity_threshold = $14,
  sort_order = $15, updated_at = NOW()
WHERE id = $1 AND appid = $2`, activityID, input.AppID, input.Name, input.Description, input.Rules, input.UIMode,
			input.Status, input.StartTime, input.EndTime, input.FreeDaily, input.PointsCost, input.PointsDaily,
			input.VideoScene, input.PityThreshold, input.SortOrder)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() == 0 {
			return nil, ErrLotteryActivityNotFound
		}
	}

	existing, err := listLotteryPrizes(ctx, tx, activityID)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]lotterydomain.Prize, len(existing))
	for _, prize := range existing {
		byID[prize.ID] = prize
	}
	kept := make(map[int64]bool, len(input.Prizes))
	var pityPrizeID *int64
	for _, prize := range input.Prizes {
		reward, err := json.Marshal(prize.Reward)
		if err != nil {
			return nil, err
		}
		var id int64
		if old, ok := byID[prize.ID]; ok && prize.ID > 0 {
			if prize.Quantity >= 0 && prize.Quantity < old.Used {
				return nil, &LotteryStockError{Position: prize.Position, Used: old.Used}
			}
			if _, err := tx.Exec(ctx, `UPDATE lottery_prizes SET
  name = $2, kind = $3, reward = $4, image_url = $5, quantity = $6, weight = $7, position = $8, is_default = $9,
  updated_at = NOW()
WHERE id = $1`, prize.ID, prize.Name, prize.Reward.Type, reward, nullableString(prize.ImageURL), prize.Quantity,
				prize.Weight, prize.Position, prize.Fallback); err != nil {
				return nil, err
			}
			id = prize.ID
		} else {
			// 带了不属于这个活动的 ID：按新建处理，不去碰别的活动的奖品
			if err := tx.QueryRow(ctx, `INSERT INTO lottery_prizes
(activity_id, name, kind, reward, image_url, quantity, weight, position, is_default)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`, activityID, prize.Name, prize.Reward.Type, reward,
				nullableString(prize.ImageURL), prize.Quantity, prize.Weight, prize.Position, prize.Fallback).Scan(&id); err != nil {
				return nil, err
			}
		}
		kept[id] = true
		if input.PityThreshold > 0 && prize.Position == input.PityPosition {
			pityID := id
			pityPrizeID = &pityID
		}
	}
	for _, prize := range existing {
		if kept[prize.ID] {
			continue
		}
		if _, err := tx.Exec(ctx, `DELETE FROM lottery_prizes WHERE id = $1`, prize.ID); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE lottery_activities SET pity_prize_id = $2 WHERE id = $1`, activityID, pityPrizeID); err != nil {
		return nil, err
	}

	activity, err := scanLotteryActivity(tx.QueryRow(ctx, `SELECT `+lotteryActivityColumns+`
FROM lottery_activities a WHERE a.id = $1`, activityID))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return activity, nil
}

// SetLotteryActivityStatus 改状态（上线 / 暂停 / 结束）。
func (r *Repository) SetLotteryActivityStatus(ctx context.Context, appID, activityID int64, status string) (*lotterydomain.Activity, error) {
	return scanLotteryActivity(r.pool.QueryRow(ctx, `UPDATE lottery_activities a SET status = $3, updated_at = NOW()
WHERE a.id = $1 AND a.appid = $2 RETURNING `+lotteryActivityColumns, activityID, appID, status))
}

// DeleteLotteryActivity 删除没有抽奖记录的活动。有记录的只能结束：删掉它会连带删掉用户的中奖记录。
func (r *Repository) DeleteLotteryActivity(ctx context.Context, appID, activityID int64) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM lottery_activities a WHERE a.id = $1 AND a.appid = $2
AND NOT EXISTS (SELECT 1 FROM lottery_draws d WHERE d.activity_id = a.id)`, activityID, appID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return nil
	}
	if _, err := r.GetLotteryActivity(ctx, appID, activityID); err != nil {
		return err
	}
	return ErrLotteryActivityHasDraws
}

// ── 用户的次数 ──

// LotteryUserFacts 一个人在一个活动里判定次数所需的事实（活动页用；抽奖时在事务里另取一份）。
func (r *Repository) LotteryUserFacts(ctx context.Context, appID, activityID, userID int64, dayStart time.Time) (lotterydomain.UserFacts, error) {
	return lotteryUserFacts(ctx, r.pool, appID, activityID, userID, dayStart, false)
}

func lotteryUserFacts(ctx context.Context, q queryExecutor, appID, activityID, userID int64, dayStart time.Time,
	lock bool) (lotterydomain.UserFacts, error) {
	var facts lotterydomain.UserFacts
	if err := q.QueryRow(ctx, `SELECT
  COUNT(*) FILTER (WHERE source = 'free'),
  COUNT(*) FILTER (WHERE source = 'points')
FROM lottery_draws WHERE activity_id = $1 AND user_id = $2 AND created_at >= $3`,
		activityID, userID, dayStart).Scan(&facts.FreeUsedToday, &facts.PointsUsedToday); err != nil {
		return facts, err
	}
	grantSQL := `SELECT balance FROM lottery_draw_grants WHERE appid = $1 AND user_id = $2`
	if lock {
		grantSQL += ` FOR UPDATE`
	}
	if err := q.QueryRow(ctx, grantSQL, appID, userID).Scan(&facts.Bonus); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return facts, err
	}
	if err := q.QueryRow(ctx, `SELECT integral FROM users WHERE id = $1 AND appid = $2`, userID, appID).
		Scan(&facts.Integral); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return facts, ErrUserNotFound
		}
		return facts, err
	}
	stateSQL := `SELECT miss_streak, total_draws FROM lottery_user_states WHERE activity_id = $1 AND user_id = $2`
	if lock {
		stateSQL += ` FOR UPDATE`
	}
	if err := q.QueryRow(ctx, stateSQL, activityID, userID).Scan(&facts.MissStreak, &facts.TotalDraws); err != nil &&
		!errors.Is(err, pgx.ErrNoRows) {
		return facts, err
	}
	return facts, nil
}

// ── 抽奖 ──

// LotteryDrawOutcome 抽奖事务的产出：记录、当时的活动与奖品，以及是否是重复提交的回放。
type LotteryDrawOutcome struct {
	Draw     lotterydomain.Draw
	Activity lotterydomain.Activity
	Prizes   []lotterydomain.Prize
	Replayed bool
}

// DrawLottery 抽一次（单事务，要么全成要么全不成）。
//
// 顺序：读活动（FOR SHARE，与改奖盘互斥）→ 判时间窗 → 锁用户行 → 幂等键回放 → 锁本人状态行与
// 次数余额 → 判定用哪种次数 → 扣次数 → 选奖品（保底 / 加权随机）→ 扣库存（抢光了改发兜底奖）→
// 落记录 → 扣积分 → 发奖 → 更新连续未中次数。
//
// 锁用户行在最前：今天用了几次、次数余额、积分余额、连续未中都是这个人的事实，不锁就有
// 「两个并发请求都看到还剩 1 次免费」的窗口（旧版的日限额就是这样被穿透的）。
// 奖品的库存是所有人共享的，扣减用条件 UPDATE（used < quantity），不锁整张奖盘，
// 只有限量奖品会被更新；不限量的奖品（通常是谢谢参与）不写，热门奖品行上没有锁竞争。
//
// 随机数来自 crypto/rand，在服务端产生、不出事务：客户端拿到的只是结果。
func (r *Repository) DrawLottery(ctx context.Context, input lotterydomain.DrawInput) (*LotteryDrawOutcome, error) {
	now := input.Now
	if now.IsZero() {
		now = time.Now()
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	activity, err := scanLotteryActivity(tx.QueryRow(ctx, `SELECT `+lotteryActivityColumns+`
FROM lottery_activities a WHERE a.id = $1 AND a.appid = $2 FOR SHARE`, input.ActivityID, input.AppID))
	if err != nil {
		return nil, err
	}

	var locked int64
	if err := tx.QueryRow(ctx, `SELECT id FROM users WHERE id = $1 AND appid = $2 FOR UPDATE`,
		input.UserID, input.AppID).Scan(&locked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}

	prizes, err := listLotteryPrizes(ctx, tx, activity.ID)
	if err != nil {
		return nil, err
	}

	// 幂等键在锁用户行之后查：同一个键的并发请求，后一个等前一个提交后在这里看到它的记录
	if input.RequestID != "" {
		existing, err := scanLotteryDraw(tx.QueryRow(ctx, `SELECT `+lotteryDrawColumns+` `+lotteryDrawJoins+`
WHERE d.activity_id = $1 AND d.user_id = $2 AND d.request_id = $3`, activity.ID, input.UserID, input.RequestID))
		if err == nil {
			if err := tx.Commit(ctx); err != nil {
				return nil, err
			}
			tx = nil
			return &LotteryDrawOutcome{Draw: *existing, Activity: *activity, Prizes: prizes, Replayed: true}, nil
		}
		if !errors.Is(err, ErrLotteryDrawNotFound) {
			return nil, err
		}
	}

	if reason := lotterydomain.Window(*activity, now); reason != lotterydomain.ReasonOK {
		return nil, lotteryUnavailable(reason)
	}

	if _, err := tx.Exec(ctx, `INSERT INTO lottery_user_states (activity_id, user_id) VALUES ($1, $2)
ON CONFLICT (activity_id, user_id) DO NOTHING`, activity.ID, input.UserID); err != nil {
		return nil, err
	}
	facts, err := lotteryUserFacts(ctx, tx, input.AppID, activity.ID, input.UserID, input.DayStart, true)
	if err != nil {
		return nil, err
	}
	chances := lotterydomain.EvaluateChances(*activity, facts, lotterydomain.VideoChances{})
	source, reason := lotterydomain.ResolveSource(chances, input.Source)
	if reason != lotterydomain.ReasonOK {
		return nil, lotteryUnavailable(reason)
	}

	if source == lotterydomain.SourceBonus {
		tag, err := tx.Exec(ctx, `UPDATE lottery_draw_grants
SET balance = balance - 1, total_consumed = total_consumed + 1, updated_at = NOW()
WHERE appid = $1 AND user_id = $2 AND balance > 0`, input.AppID, input.UserID)
		if err != nil {
			return nil, err
		}
		if tag.RowsAffected() == 0 {
			return nil, lotteryUnavailable(lotterydomain.ReasonBonusExhausted)
		}
	}

	prize, pity, err := pickLotteryPrize(ctx, tx, *activity, prizes, facts.MissStreak)
	if err != nil {
		return nil, err
	}

	status := lotterydomain.DrawGranted
	if prize.Kind == lotterydomain.KindManual {
		status = lotterydomain.DrawPending
	}
	var pointsCost int64
	if source == lotterydomain.SourcePoints {
		pointsCost = activity.PointsCost
	}
	reward, err := json.Marshal(prize.Reward)
	if err != nil {
		return nil, err
	}
	prizeID := prize.ID
	var drawID int64
	drawNo := lotteryDrawNo(now)
	if err := tx.QueryRow(ctx, `INSERT INTO lottery_draws
(draw_no, appid, activity_id, user_id, prize_id, prize_name, prize_kind, reward, position, source, points_cost, pity,
 status, request_id, device_id, client_ip, user_agent, created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18) RETURNING id`,
		drawNo, input.AppID, activity.ID, input.UserID, prizeID, truncateColumn(prize.Name, 64), prize.Kind, reward,
		prize.Position, source, pointsCost, pity, status, nullableString(input.RequestID),
		nullableString(truncateColumn(input.DeviceID, 128)), nullableString(truncateColumn(input.ClientIP, 64)),
		nullableString(truncateColumn(input.UserAgent, 256)), now).Scan(&drawID); err != nil {
		return nil, err
	}

	meta := map[string]any{"drawNo": drawNo, "activityId": activity.ID, "prizeId": prize.ID}
	var integralTxnNo string
	if pointsCost > 0 {
		_, _, integralTxnNo, err = applyIntegralChangeTx(ctx, tx, input.UserID, input.AppID, -pointsCost,
			"consume", lotterydomain.IntegralCategory, "积分抽奖", activity.Name, lotterydomain.SourceType, &drawID, meta)
		if err != nil {
			return nil, err
		}
	}

	results, err := r.grantLotteryPrizeTx(ctx, tx, lotteryPrizeGrant{
		AppID: input.AppID, UserID: input.UserID, DrawID: drawID, DrawNo: drawNo,
		Title: "抽奖奖品", Source: activity.Name, Meta: meta,
	}, prize)
	if err != nil {
		return nil, err
	}
	resultsJSON, err := json.Marshal(results)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE lottery_draws SET results = $2, integral_transaction_no = $3 WHERE id = $1`,
		drawID, resultsJSON, nullableString(integralTxnNo)); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE lottery_user_states
SET miss_streak = $3, total_draws = total_draws + 1, last_draw_at = $4, updated_at = NOW()
WHERE activity_id = $1 AND user_id = $2`, activity.ID, input.UserID,
		lotterydomain.NextMissStreak(facts.MissStreak, prize), now); err != nil {
		return nil, err
	}

	draw, err := scanLotteryDraw(tx.QueryRow(ctx, `SELECT `+lotteryDrawColumns+` `+lotteryDrawJoins+` WHERE d.id = $1`, drawID))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return &LotteryDrawOutcome{Draw: *draw, Activity: *activity, Prizes: prizes}, nil
}

// pickLotteryPrize 选奖品并扣库存。返回选中的奖品与是否保底触发。
func pickLotteryPrize(ctx context.Context, tx pgx.Tx, activity lotterydomain.Activity, prizes []lotterydomain.Prize,
	missStreak int) (lotterydomain.Prize, bool, error) {
	var chosen lotterydomain.Prize
	var found, pity bool

	if activity.PityPrizeID != nil && lotterydomain.PityDue(activity.PityThreshold, missStreak) {
		for _, prize := range prizes {
			if prize.ID == *activity.PityPrizeID && prize.InStock() {
				chosen, found, pity = prize, true, true
				break
			}
		}
	}
	if !found {
		candidates := lotterydomain.Candidates(prizes)
		if total := lotterydomain.TotalWeight(candidates); total > 0 {
			roll, err := rand.Int(rand.Reader, big.NewInt(total))
			if err != nil {
				return chosen, false, err
			}
			chosen, found = lotterydomain.Pick(candidates, roll.Int64())
		}
	}
	if !found {
		fallback, ok := lotterydomain.FallbackPrize(prizes)
		if !ok {
			return chosen, false, ErrLotteryPoolBroken
		}
		return fallback, false, nil
	}
	if chosen.Quantity < 0 {
		return chosen, pity, nil
	}
	tag, err := tx.Exec(ctx, `UPDATE lottery_prizes SET used = used + 1, updated_at = NOW()
WHERE id = $1 AND quantity >= 0 AND used < quantity`, chosen.ID)
	if err != nil {
		return chosen, false, err
	}
	if tag.RowsAffected() == 1 {
		return chosen, pity, nil
	}
	// 在选中与扣减之间被别人抽走了最后一份：改发兜底奖（不限量），保底不算触发
	fallback, ok := lotterydomain.FallbackPrize(prizes)
	if !ok {
		return chosen, false, ErrLotteryPoolBroken
	}
	return fallback, false, nil
}

// lotteryDrawNo 抽奖单号：LTD + UTC 秒 + 10 位加密随机数字。
func lotteryDrawNo(now time.Time) string {
	n, err := rand.Int(rand.Reader, big.NewInt(10_000_000_000))
	if err != nil {
		return generateTransactionNo("LTD")
	}
	return "LTD" + now.UTC().Format("20060102150405") + fmt.Sprintf("%010d", n.Int64())
}

// lotteryPrizeGrant 一次奖品发放的上下文。
type lotteryPrizeGrant struct {
	AppID  int64
	UserID int64
	DrawID int64
	DrawNo string
	Title  string
	Source string
	Meta   map[string]any
}

// grantLotteryPrizeTx 在抽奖事务里发放奖品。
//
// 这里的 switch 与 lottery 的奖品目录**双向钉死**（TestLotteryCatalogHasGrantBranch）：
// 目录多一档 → 配得上却发不出来；这里多一档 → 控制台配不出来。
// 发不出去（套餐被删、功能停用）时整次抽奖回滚：次数与积分都没有花出去。
func (r *Repository) grantLotteryPrizeTx(ctx context.Context, tx pgx.Tx, grant lotteryPrizeGrant,
	prize lotterydomain.Prize) ([]lotterydomain.RewardResult, error) {
	reward := lotterydomain.NormalizeReward(prize.Reward)
	appID, userID := grant.AppID, grant.UserID
	drawID := grant.DrawID
	out := make([]lotterydomain.RewardResult, 0, 1)

	var plans map[int64]pointstore.PlanRef
	var features map[string]pointstore.FeatureRef
	if reward.Type == lotterydomain.KindVipPlan || reward.Type == lotterydomain.KindFeature {
		var err error
		plans, features, err = pointsStoreCatalogRefs(ctx, tx, appID)
		if err != nil {
			return nil, err
		}
	}
	described := lotterydomain.DescribeReward(reward, plans, features)
	result := lotterydomain.RewardResult{Type: reward.Type, Label: described.Label, Detail: described.Detail}

	switch reward.Type {
	case lotterydomain.KindNone:
		return out, nil

	case lotterydomain.KindPoints:
		_, after, txnNo, err := applyIntegralChangeTx(ctx, tx, userID, appID, reward.Amount,
			"earn", lotterydomain.IntegralCategory, grant.Title, grant.Source, lotterydomain.SourceType, &drawID, grant.Meta)
		if err != nil {
			return nil, err
		}
		result.Detail += "（余额 " + strconv.FormatInt(after, 10) + "）"
		result.TransactionNo = txnNo

	case lotterydomain.KindExperience:
		expResult, err := r.applyExperienceChangeTx(ctx, tx, userID, appID, reward.Amount,
			lotterydomain.IntegralCategory, grant.Title, grant.Source, lotterydomain.SourceType, &drawID, grant.Meta)
		if err != nil {
			return nil, err
		}
		if expResult.LevelChanged {
			result.Detail += "（升至 Lv." + strconv.Itoa(expResult.NewLevel) + "）"
		}
		result.TransactionNo = expResult.TransactionNo

	case lotterydomain.KindVipDays:
		txn, err := extendUserVipTx(ctx, tx, vipdomain.Grant{
			UserID: userID, AppID: appID, PlanName: grant.Title, DurationDays: int(reward.Amount),
			PayChannel: vipdomain.ChannelLottery, PayAmount: decimal.Zero, Metadata: grant.Meta,
		})
		if err != nil {
			return nil, err
		}
		result.TransactionNo = txn.TransactionNo

	case lotterydomain.KindVipPlan:
		plan, err := scanVipPlan(tx.QueryRow(ctx,
			`SELECT `+vipPlanColumns+` FROM vip_plans WHERE appid = $1 AND id = $2`, appID, reward.RefID))
		if err != nil {
			return nil, err
		}
		if plan == nil || plan.IsTrial() {
			return nil, ErrLotteryRewardUnavailable
		}
		planID := plan.ID
		txn, err := extendUserVipTx(ctx, tx, vipdomain.Grant{
			UserID: userID, AppID: appID, PlanID: &planID, PlanName: plan.Name, Features: plan.Features,
			DurationDays: plan.DurationDays, Lifetime: plan.Lifetime,
			PayChannel: vipdomain.ChannelLottery, PayAmount: decimal.Zero, Metadata: grant.Meta,
		})
		if err != nil {
			return nil, err
		}
		result.Detail = plan.Name + "（" + plan.TermLabel() + "）"
		result.TransactionNo = txn.TransactionNo

	case lotterydomain.KindFeature:
		if feature, ok := features[reward.Tag]; !ok || !feature.IsActive {
			return nil, ErrLotteryRewardUnavailable
		}
		unlock, err := grantFeatureUnlockTx(ctx, tx, FeatureUnlockGrant{
			AppID: appID, UserID: userID, Tag: reward.Tag, Days: int(reward.Amount),
			Source: vipdomain.UnlockSourceLottery, SourceRef: grant.DrawNo,
		})
		if errors.Is(err, ErrFeatureUnlockOwned) {
			// 已永久拥有：没有可发的，记一笔说明而不是让这次抽奖失败
			result.Detail += "（已永久拥有）"
			break
		}
		if err != nil {
			return nil, err
		}
		result.ExpireAt = unlock.EndsAt
		result.TransactionNo = strconv.FormatInt(unlock.ID, 10)

	case lotterydomain.KindLotteryDraws:
		balance, err := grantLotteryDrawsTx(ctx, tx, appID, userID, reward.Amount)
		if err != nil {
			return nil, err
		}
		result.Detail += "（剩余 " + strconv.FormatInt(balance, 10) + " 次）"

	case lotterydomain.KindManual:
		result.Pending = true

	default:
		// 目录里有、这里没有 —— 由测试挡在合并之前
		return nil, fmt.Errorf("lottery: unsupported prize kind %s", reward.Type)
	}
	return append(out, result), nil
}

// ── 记录 ──

// GetLotteryDraw 取一条记录（必须属于这个应用）。
func (r *Repository) GetLotteryDraw(ctx context.Context, appID, drawID int64) (*lotterydomain.Draw, error) {
	return scanLotteryDraw(r.pool.QueryRow(ctx, `SELECT `+lotteryDrawColumns+` `+lotteryDrawJoins+`
WHERE d.id = $1 AND d.appid = $2`, drawID, appID))
}

// ListLotteryDraws 抽奖记录分页。
func (r *Repository) ListLotteryDraws(ctx context.Context, query lotterydomain.DrawQuery) (*lotterydomain.Page[lotterydomain.Draw], error) {
	where := []string{"d.appid = $1"}
	args := []any{query.AppID}
	add := func(clause string, value any) {
		args = append(args, value)
		where = append(where, strings.ReplaceAll(clause, "?", "$"+strconv.Itoa(len(args))))
	}
	if query.ActivityID > 0 {
		add("d.activity_id = ?", query.ActivityID)
	}
	if query.UserID > 0 {
		add("d.user_id = ?", query.UserID)
	}
	if query.Status != "" {
		add("d.status = ?", query.Status)
	}
	if query.Source != "" {
		add("d.source = ?", query.Source)
	}
	if query.Winning {
		where = append(where, "d.prize_kind <> 'none'")
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		add("(d.draw_no = ? OR d.prize_name ILIKE '%' || ? || '%' OR u.account ILIKE '%' || ? || '%' OR up.nickname ILIKE '%' || ? || '%')", keyword)
	}
	if query.Start != nil {
		add("d.created_at >= ?", *query.Start)
	}
	if query.End != nil {
		add("d.created_at < ?", *query.End)
	}
	whereSQL := strings.Join(where, " AND ")

	page, limit := query.Page, query.Limit
	if page < 1 {
		page = 1
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	result := &lotterydomain.Page[lotterydomain.Draw]{Items: []lotterydomain.Draw{}, Page: page, Limit: limit}
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) `+lotteryDrawJoins+` WHERE `+whereSQL, args...).Scan(&result.Total); err != nil {
		return nil, err
	}
	result.TotalPages = int((result.Total + int64(limit) - 1) / int64(limit))
	if result.Total == 0 {
		return result, nil
	}
	args = append(args, limit, (page-1)*limit)
	rows, err := r.pool.Query(ctx, `SELECT `+lotteryDrawColumns+` `+lotteryDrawJoins+` WHERE `+whereSQL+`
ORDER BY d.created_at DESC, d.id DESC LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanLotteryDraw(rows)
		if err != nil {
			return nil, err
		}
		result.Items = append(result.Items, *item)
	}
	return result, rows.Err()
}

// FulfillLotteryDraw 把一条待发放的记录标记为已发放。
func (r *Repository) FulfillLotteryDraw(ctx context.Context, input lotterydomain.HandleInput) (*lotterydomain.Draw, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM lottery_draws WHERE id = $1 AND appid = $2 FOR UPDATE`,
		input.DrawID, input.AppID).Scan(&status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrLotteryDrawNotFound
		}
		return nil, err
	}
	if status != lotterydomain.DrawPending {
		return nil, ErrLotteryDrawState
	}
	if _, err := tx.Exec(ctx, `UPDATE lottery_draws SET status = 'fulfilled', note = $2, handled_by = $3, handled_at = NOW(),
results = (SELECT COALESCE(jsonb_agg(item - 'pending'), '[]'::jsonb) FROM jsonb_array_elements(results) item)
WHERE id = $1`, input.DrawID, nullableString(strings.TrimSpace(input.Note)), nullableString(input.Operator)); err != nil {
		return nil, err
	}
	draw, err := scanLotteryDraw(tx.QueryRow(ctx, `SELECT `+lotteryDrawColumns+` `+lotteryDrawJoins+` WHERE d.id = $1`, input.DrawID))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return draw, nil
}

// ── 统计 ──

// LotteryStats 一个活动的概览：窗口内按天趋势、按来源与按奖品的分布。缺的日期由服务层补零。
func (r *Repository) LotteryStats(ctx context.Context, appID, activityID int64, since, dayStart time.Time, timezone string) (*lotterydomain.Stats, error) {
	stats := &lotterydomain.Stats{ActivityID: activityID, Trend: []lotterydomain.StatsDay{},
		Sources: []lotterydomain.StatsSource{}, Prizes: []lotterydomain.StatsPrize{}}
	s := &stats.Summary
	if err := r.pool.QueryRow(ctx, `SELECT
  COUNT(*) FILTER (WHERE created_at >= $3),
  COUNT(DISTINCT user_id) FILTER (WHERE created_at >= $3),
  COUNT(*) FILTER (WHERE created_at >= $3 AND prize_kind <> 'none'),
  COUNT(*), COUNT(DISTINCT user_id), COALESCE(SUM(points_cost), 0),
  COUNT(*) FILTER (WHERE status = 'pending'), COUNT(*) FILTER (WHERE pity)
FROM lottery_draws WHERE appid = $1 AND activity_id = $2`, appID, activityID, dayStart).Scan(
		&s.TodayDraws, &s.TodayUsers, &s.TodayWins, &s.TotalDraws, &s.TotalUsers, &s.PointsSpent, &s.Pending, &s.PityHits); err != nil {
		return nil, err
	}

	rows, err := r.pool.Query(ctx, `SELECT to_char(created_at AT TIME ZONE $4, 'YYYY-MM-DD') AS day,
  COUNT(*), COUNT(*) FILTER (WHERE prize_kind <> 'none'), COUNT(DISTINCT user_id)
FROM lottery_draws WHERE appid = $1 AND activity_id = $2 AND created_at >= $3
GROUP BY day ORDER BY day`, appID, activityID, since, timezone)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var day lotterydomain.StatsDay
		if err := rows.Scan(&day.Date, &day.Draws, &day.Wins, &day.Users); err != nil {
			rows.Close()
			return nil, err
		}
		stats.Trend = append(stats.Trend, day)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = r.pool.Query(ctx, `SELECT source, COUNT(*) FROM lottery_draws
WHERE appid = $1 AND activity_id = $2 AND created_at >= $3 GROUP BY source ORDER BY COUNT(*) DESC`, appID, activityID, since)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var item lotterydomain.StatsSource
		if err := rows.Scan(&item.Source, &item.Draws); err != nil {
			rows.Close()
			return nil, err
		}
		stats.Sources = append(stats.Sources, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// 现存奖品按位置列出（含窗口内一次没抽中的），已删除奖品的记录按快照名字归到末尾
	rows, err = r.pool.Query(ctx, `SELECT p.id, p.name, p.kind, p.quantity, p.used,
  (SELECT COUNT(*) FROM lottery_draws d WHERE d.prize_id = p.id AND d.created_at >= $3)
FROM lottery_prizes p WHERE p.activity_id = $2 AND EXISTS (SELECT 1 FROM lottery_activities a WHERE a.id = $2 AND a.appid = $1)
ORDER BY p.position, p.id`, appID, activityID, since)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var item lotterydomain.StatsPrize
		var id int64
		var quantity, used int32
		if err := rows.Scan(&id, &item.Name, &item.Kind, &quantity, &used, &item.Draws); err != nil {
			rows.Close()
			return nil, err
		}
		item.PrizeID, item.Quantity, item.Used = &id, int64(quantity), int64(used)
		stats.Prizes = append(stats.Prizes, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = r.pool.Query(ctx, `SELECT prize_name, prize_kind, COUNT(*) FROM lottery_draws
WHERE appid = $1 AND activity_id = $2 AND created_at >= $3 AND prize_id IS NULL
GROUP BY prize_name, prize_kind ORDER BY COUNT(*) DESC`, appID, activityID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		item := lotterydomain.StatsPrize{Quantity: -1}
		if err := rows.Scan(&item.Name, &item.Kind, &item.Draws); err != nil {
			return nil, err
		}
		stats.Prizes = append(stats.Prizes, item)
	}
	return stats, rows.Err()
}
