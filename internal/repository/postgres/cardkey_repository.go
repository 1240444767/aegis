package postgres

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	cardkeydomain "aegis/internal/domain/cardkey"
)

// 卡密核销与登录的判定结果。服务层用 errors.Is 比对后翻成业务错误码 ——
// 仓储层不认识 HTTP 状态码，也不该认识。
var (
	ErrCardKeyNotFound         = errors.New("card key not found")
	ErrCardKeyUsed             = errors.New("card key already used")
	ErrCardKeyDisabled         = errors.New("card key disabled")
	ErrCardKeyExpired          = errors.New("card key expired")
	ErrCardKeyNotStarted       = errors.New("card key not started yet")
	ErrCardKeyKindMismatch     = errors.New("card key kind mismatch")
	ErrCardKeyDeviceLimit      = errors.New("card key device limit reached")
	ErrCardKeyBoundOther       = errors.New("card key bound to another user")
	ErrCardKeyNoLoginCard      = errors.New("no active login card")
	ErrCardKeyLicensePermanent = errors.New("login cards are already permanent")
	ErrCardKeyBatchMissing     = errors.New("card key batch not found")
	ErrCardKeyAlreadyRedeemed  = errors.New("card key already redeemed by this user")
	ErrCardKeyUserLimit        = errors.New("card key per-user limit reached")
	ErrCardKeyAudience         = errors.New("card key audience mismatch")
	ErrCardKeyUnbindLimit      = errors.New("card key self unbind limit reached")
	ErrCardKeyUnbindDisabled   = errors.New("card key self unbind disabled")
	ErrCardKeyDeviceMissing    = errors.New("card key device not bound")
	ErrCardKeyBatchInUse       = errors.New("card key batch has redemptions")
	ErrCardKeyRewardMissing    = errors.New("card key reward target missing")
	ErrCardKeyCodeTaken        = errors.New("card key code already exists")
)

// CardKeyAudienceError 领取对象不符，带上批次要求的对象，让服务层说清楚「仅限会员」还是「仅限非会员」。
type CardKeyAudienceError struct{ Audience string }

func (e *CardKeyAudienceError) Error() string { return "card key audience mismatch: " + e.Audience }
func (e *CardKeyAudienceError) Is(target error) bool {
	return target == ErrCardKeyAudience
}

// CardKeyUserLimitError 每人限兑张数已满，带上上限。
type CardKeyUserLimitError struct{ Limit int }

func (e *CardKeyUserLimitError) Error() string {
	return "card key per-user limit: " + strconv.Itoa(e.Limit)
}
func (e *CardKeyUserLimitError) Is(target error) bool {
	return target == ErrCardKeyUserLimit
}

// CardKeyNotStartedError 未到生效时间，带上生效时间。
type CardKeyNotStartedError struct{ StartsAt time.Time }

func (e *CardKeyNotStartedError) Error() string { return "card key starts at " + e.StartsAt.String() }
func (e *CardKeyNotStartedError) Is(target error) bool {
	return target == ErrCardKeyNotStarted
}

// CardKeyCodeTakenError 导入的卡面与已有的卡撞了，带上撞了哪些（最多几张，够运营定位就行）。
type CardKeyCodeTakenError struct{ Codes []string }

func (e *CardKeyCodeTakenError) Error() string {
	return "card key codes taken: " + strings.Join(e.Codes, ",")
}
func (e *CardKeyCodeTakenError) Is(target error) bool {
	return target == ErrCardKeyCodeTaken
}

// cardKeyColumns 与 scanCardKey 的读取顺序严格一一对应。
// 提成常量是因为它出现在多处 SQL 里，逐处手抄迟早会漏掉新加的列，
// 而 pgx 的表现是「Scan 位置错位」而不是编译错误。
const cardKeyColumns = `id, appid, batch_id, code, kind, status, bound_user_id, max_devices, max_uses, use_count,
activated_at, expires_at, used_at, disabled_at, disabled_reason, distributed_at, distribution_note, remark,
created_at, updated_at`

const cardKeyBatchColumns = `id, appid, name, kind, remark, code_prefix, segments, segment_length, charset, source,
rewards, max_devices, max_uses, per_user_limit, audience, starts_at, validity_mode, validity_days, valid_until,
self_unbind_limit, channel, face_value::text, total, status, created_by, created_at, updated_at`

// cardKeyColumnsAs 带表别名的单卡列，联表查询用。
func cardKeyColumnsAs(alias string) string {
	parts := strings.Split(cardKeyColumns, ",")
	for index, part := range parts {
		parts[index] = alias + "." + strings.TrimSpace(part)
	}
	return strings.Join(parts, ", ")
}

func cardKeyScanTargets(item *cardkeydomain.Card) []any {
	return []any{&item.ID, &item.AppID, &item.BatchID, &item.Code, &item.Kind, &item.Status,
		&item.BoundUserID, &item.MaxDevices, &item.MaxUses, &item.UseCount, &item.ActivatedAt, &item.ExpiresAt,
		&item.UsedAt, &item.DisabledAt, &item.DisabledReason, &item.DistributedAt, &item.DistributionNote,
		&item.Remark, &item.CreatedAt, &item.UpdatedAt}
}

func scanCardKey(row pgx.Row) (*cardkeydomain.Card, error) {
	var item cardkeydomain.Card
	if err := row.Scan(cardKeyScanTargets(&item)...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCardKeyNotFound
		}
		return nil, err
	}
	return &item, nil
}

func scanCardKeyBatch(row pgx.Row) (*cardkeydomain.Batch, error) {
	var item cardkeydomain.Batch
	var rewards []byte
	var faceValue string
	if err := row.Scan(&item.ID, &item.AppID, &item.Name, &item.Kind, &item.Remark, &item.CodePrefix,
		&item.Segments, &item.SegmentLength, &item.Charset, &item.Origin, &rewards, &item.MaxDevices,
		&item.MaxUses, &item.PerUserLimit, &item.Audience, &item.StartsAt, &item.ValidityMode,
		&item.ValidityDays, &item.ValidUntil, &item.SelfUnbindLimit, &item.Channel, &faceValue,
		&item.Total, &item.Status, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCardKeyBatchMissing
		}
		return nil, err
	}
	_ = json.Unmarshal(rewards, &item.Rewards)
	if item.Rewards == nil {
		item.Rewards = []cardkeydomain.Reward{}
	}
	item.FaceValue, _ = decimal.NewFromString(faceValue)
	return &item, nil
}

// ── 批次 ──

// CreateCardKeyBatch 建批次并一次性写入全部卡面（单事务）。
//
// 批次与卡必须同事务：只建批次会留下一个「总数 500、一张卡都没有」的空壳，
// 而运营看到的是生成成功。批量插入用 CopyFrom —— 五千张卡逐条 INSERT
// 是五千次往返，控制台上的表现是点了生成之后转圈半分钟。
func (r *Repository) CreateCardKeyBatch(ctx context.Context, input cardkeydomain.GenerateInput, codes []string) (*cardkeydomain.Batch, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	rewards, err := json.Marshal(cardkeydomain.NormalizeRewards(input.Rewards))
	if err != nil {
		return nil, err
	}

	batch, err := scanCardKeyBatch(tx.QueryRow(ctx, `INSERT INTO card_key_batches
(appid, name, kind, remark, code_prefix, segments, segment_length, charset, source, rewards, max_devices,
max_uses, per_user_limit, audience, starts_at, validity_mode, validity_days, valid_until, self_unbind_limit,
channel, face_value, total, created_by)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21::numeric,$22,$23)
RETURNING `+cardKeyBatchColumns,
		input.AppID, input.Name, input.Kind, input.Remark, input.CodePrefix, input.Segments,
		input.SegmentLength, input.Charset, input.Origin, rewards, input.MaxDevices, input.MaxUses,
		input.PerUserLimit, input.Audience, input.StartsAt, input.ValidityMode, input.ValidityDays,
		input.ValidUntil, input.SelfUnbindLimit, input.Channel, input.FaceValue.StringFixed(2),
		len(codes), input.Operator))
	if err != nil {
		return nil, err
	}

	if err := copyCardKeysTx(ctx, tx, batch, codes); err != nil {
		return nil, err
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return batch, nil
}

// copyCardKeysTx 把一批卡面写进 card_keys。
//
// 固定到期模式在生成时就写死 expires_at；激活即计时的那一档留空，首次使用时才写。
func copyCardKeysTx(ctx context.Context, tx pgx.Tx, batch *cardkeydomain.Batch, codes []string) error {
	var expiresAt *time.Time
	if batch.ValidityMode == cardkeydomain.ValidityFixedUntil {
		expiresAt = batch.ValidUntil
	}
	rows := make([][]any, 0, len(codes))
	for _, code := range codes {
		rows = append(rows, []any{
			batch.AppID, batch.ID, code, batch.Kind, cardkeydomain.StatusUnused,
			batch.MaxDevices, batch.MaxUses, expiresAt,
		})
	}
	_, err := tx.CopyFrom(ctx,
		pgx.Identifier{"card_keys"},
		[]string{"appid", "batch_id", "code", "kind", "status", "max_devices", "max_uses", "expires_at"},
		pgx.CopyFromRows(rows),
	)
	return err
}

// AppendCardKeys 给一批卡补发（单事务：写卡 + 改总数）。
func (r *Repository) AppendCardKeys(ctx context.Context, appID int64, batchID int64, codes []string) (*cardkeydomain.Batch, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	batch, err := scanCardKeyBatch(tx.QueryRow(ctx, `UPDATE card_key_batches
SET total = total + $3, updated_at = NOW()
WHERE appid = $1 AND id = $2
RETURNING `+cardKeyBatchColumns, appID, batchID, len(codes)))
	if err != nil {
		return nil, err
	}
	if err := copyCardKeysTx(ctx, tx, batch, codes); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return batch, nil
}

// UpdateCardKeyBatch 修改批次。
//
// 固定到期模式改了到期时间时，同步改这一批里**还没被用过**的卡：
// 已经激活的授权卡有自己的到期时间（可能被管理员单独调整过、被续费卡延长过），不跟着动。
func (r *Repository) UpdateCardKeyBatch(ctx context.Context, input cardkeydomain.BatchUpdateInput) (*cardkeydomain.Batch, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	current, err := scanCardKeyBatch(tx.QueryRow(ctx,
		`SELECT `+cardKeyBatchColumns+` FROM card_key_batches WHERE appid = $1 AND id = $2 FOR UPDATE`,
		input.AppID, input.BatchID))
	if err != nil {
		return nil, err
	}

	rewards, err := json.Marshal(cardkeydomain.NormalizeRewards(input.Rewards))
	if err != nil {
		return nil, err
	}
	validUntil := current.ValidUntil
	if current.ValidityMode == cardkeydomain.ValidityFixedUntil && input.ValidUntil != nil {
		validUntil = input.ValidUntil
	}

	batch, err := scanCardKeyBatch(tx.QueryRow(ctx, `UPDATE card_key_batches
SET name = $3, remark = $4, channel = $5, face_value = $6::numeric, rewards = $7, per_user_limit = $8,
    audience = $9, starts_at = $10, valid_until = $11, self_unbind_limit = $12, updated_at = NOW()
WHERE appid = $1 AND id = $2
RETURNING `+cardKeyBatchColumns,
		input.AppID, input.BatchID, input.Name, input.Remark, input.Channel, input.FaceValue.StringFixed(2),
		rewards, input.PerUserLimit, input.Audience, input.StartsAt, validUntil, input.SelfUnbindLimit))
	if err != nil {
		return nil, err
	}

	if current.ValidityMode == cardkeydomain.ValidityFixedUntil && validUntil != nil &&
		(current.ValidUntil == nil || !current.ValidUntil.Equal(*validUntil)) {
		if _, err := tx.Exec(ctx, `UPDATE card_keys SET expires_at = $3, updated_at = NOW()
WHERE appid = $1 AND batch_id = $2 AND status = 'unused' AND use_count = 0`,
			input.AppID, input.BatchID, validUntil); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return batch, nil
}

// ListCardKeyBatches 批次列表，随行带上各状态的卡数量。
//
// 汇总跟着列表一起下发而不是另开一个接口：运营看批次列表时唯一关心的就是
// 「发出去多少、用掉多少」，分两次请求会出现「列表已刷新、进度还是上一次」的画面。
func (r *Repository) ListCardKeyBatches(ctx context.Context, appID int64, keyword string, kind string) ([]cardkeydomain.Batch, error) {
	where := []string{"appid = $1"}
	args := []any{appID}
	if keyword = strings.TrimSpace(keyword); keyword != "" {
		args = append(args, "%"+keyword+"%")
		where = append(where, fmt.Sprintf("(name ILIKE $%d OR channel ILIKE $%d OR remark ILIKE $%d)",
			len(args), len(args), len(args)))
	}
	if kind != "" {
		args = append(args, kind)
		where = append(where, fmt.Sprintf("kind = $%d", len(args)))
	}
	rows, err := r.pool.Query(ctx, `SELECT `+cardKeyBatchColumns+` FROM card_key_batches WHERE `+
		strings.Join(where, " AND ")+` ORDER BY id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]cardkeydomain.Batch, 0, 16)
	for rows.Next() {
		batch, err := scanCardKeyBatch(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *batch)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return items, nil
	}

	stats, err := r.cardKeyBatchStats(ctx, appID, 0)
	if err != nil {
		return nil, err
	}
	for index := range items {
		if stat, ok := stats[items[index].ID]; ok {
			items[index].Stats = stat
		} else {
			items[index].Stats = &cardkeydomain.BatchStats{}
		}
	}
	return items, nil
}

// cardKeyBatchStats 一次查出全部（或一个）批次的核销进度。
//
// 「已过期」在 SQL 里算而不是取一档状态：过期是时间比出来的结论，
// 存成状态就需要定时任务去翻，而那个任务停掉的表现是「过期卡还显示可用」。
func (r *Repository) cardKeyBatchStats(ctx context.Context, appID int64, batchID int64) (map[int64]*cardkeydomain.BatchStats, error) {
	rows, err := r.pool.Query(ctx, `SELECT batch_id,
COUNT(*),
COUNT(*) FILTER (WHERE status = 'unused'),
COUNT(*) FILTER (WHERE status = 'active'),
COUNT(*) FILTER (WHERE status = 'used'),
COUNT(*) FILTER (WHERE status = 'disabled'),
COUNT(*) FILTER (WHERE status <> 'disabled' AND expires_at IS NOT NULL AND expires_at <= NOW()),
COUNT(*) FILTER (WHERE distributed_at IS NOT NULL),
COUNT(*) FILTER (WHERE status = 'unused' AND use_count = 0 AND distributed_at IS NULL
                   AND (expires_at IS NULL OR expires_at > NOW())),
COALESCE(SUM(use_count), 0)
FROM card_keys WHERE appid = $1 AND ($2 = 0 OR batch_id = $2) GROUP BY batch_id`, appID, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[int64]*cardkeydomain.BatchStats)
	for rows.Next() {
		var id int64
		stat := &cardkeydomain.BatchStats{}
		if err := rows.Scan(&id, &stat.Total, &stat.Unused, &stat.Active, &stat.Used, &stat.Disabled,
			&stat.Expired, &stat.Distributed, &stat.Available, &stat.Uses); err != nil {
			return nil, err
		}
		out[id] = stat
	}
	return out, rows.Err()
}

// GetCardKeyBatch 取单个批次（带进度）。
func (r *Repository) GetCardKeyBatch(ctx context.Context, appID int64, batchID int64) (*cardkeydomain.Batch, error) {
	batch, err := scanCardKeyBatch(r.pool.QueryRow(ctx,
		`SELECT `+cardKeyBatchColumns+` FROM card_key_batches WHERE appid = $1 AND id = $2`, appID, batchID))
	if err != nil {
		return nil, err
	}
	stats, err := r.cardKeyBatchStats(ctx, appID, batchID)
	if err != nil {
		return nil, err
	}
	if stat, ok := stats[batch.ID]; ok {
		batch.Stats = stat
	} else {
		batch.Stats = &cardkeydomain.BatchStats{}
	}
	return batch, nil
}

// SetCardKeyBatchStatus 启停批次。
//
// 停用批次**不改单卡状态**：卡还是那些卡，只是这一批整体不再可用。
// 逐张改成 disabled 会让「先停批次、再启用」变成一次不可逆的操作 ——
// 那些本来就已经被作废的卡分不出来了。
func (r *Repository) SetCardKeyBatchStatus(ctx context.Context, appID int64, batchID int64, status string) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE card_key_batches SET status = $3, updated_at = NOW() WHERE appid = $1 AND id = $2`,
		appID, batchID, status)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrCardKeyBatchMissing
	}
	return nil
}

// DeleteCardKeyBatch 删除批次（级联删除其下全部卡）。
//
// 有核销记录的批次拒删：级联会把核销流水一起带走，而那是「谁领了什么」唯一的凭据 ——
// 客诉来时查无此卡比查到一张已作废的卡糟糕得多。要停发就停用批次。
func (r *Repository) DeleteCardKeyBatch(ctx context.Context, appID int64, batchID int64) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var locked int64
	if err := tx.QueryRow(ctx, `SELECT id FROM card_key_batches WHERE appid = $1 AND id = $2 FOR UPDATE`,
		appID, batchID).Scan(&locked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrCardKeyBatchMissing
		}
		return err
	}
	var used bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM card_key_redemptions WHERE batch_id = $1)
OR EXISTS (SELECT 1 FROM card_keys WHERE batch_id = $1 AND (use_count > 0 OR bound_user_id IS NOT NULL))`,
		batchID).Scan(&used); err != nil {
		return err
	}
	if used {
		return ErrCardKeyBatchInUse
	}
	if _, err := tx.Exec(ctx, `DELETE FROM card_key_batches WHERE appid = $1 AND id = $2`, appID, batchID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ── 提卡 ──

// ExtractCardKeys 从一批里取出 count 张还没交给渠道的未使用卡，并打上提取标记。
//
// 「取」与「标记」在同一条语句里（UPDATE … WHERE id IN (SELECT … FOR UPDATE SKIP LOCKED)）：
// 两个运营同时提卡时各拿各的，不会把同一张卡交给两个渠道 —— 那种事故的表现是
// 两个买家拿着同一个码，先兑换的那个人赢。
func (r *Repository) ExtractCardKeys(ctx context.Context, input cardkeydomain.ExtractInput) ([]cardkeydomain.Card, error) {
	rows, err := r.pool.Query(ctx, `UPDATE card_keys SET distributed_at = NOW(), distribution_note = $4, updated_at = NOW()
WHERE id IN (
    SELECT id FROM card_keys
    WHERE appid = $1 AND batch_id = $2 AND status = 'unused' AND use_count = 0 AND distributed_at IS NULL
      AND (expires_at IS NULL OR expires_at > NOW())
    ORDER BY id
    LIMIT $3
    FOR UPDATE SKIP LOCKED
)
RETURNING `+cardKeyColumns, input.AppID, input.BatchID, input.Count, input.Note)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]cardkeydomain.Card, 0, input.Count)
	for rows.Next() {
		card, err := scanCardKey(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *card)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// RETURNING 的顺序不保证，按 id 排好再交给运营（导出的文件要和控制台列表对得上）。
	sortCardsByID(items)
	return items, nil
}

// SetCardKeysDistributed 批量标记 / 取消标记「已提取」。
func (r *Repository) SetCardKeysDistributed(ctx context.Context, appID int64, ids []int64, distributed bool, note string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	sql := `UPDATE card_keys SET distributed_at = COALESCE(distributed_at, NOW()), distribution_note = $3, updated_at = NOW()
WHERE appid = $1 AND id = ANY($2)`
	if !distributed {
		sql = `UPDATE card_keys SET distributed_at = NULL, distribution_note = $3, updated_at = NOW()
WHERE appid = $1 AND id = ANY($2)`
		note = ""
	}
	tag, err := r.pool.Exec(ctx, sql, appID, ids, note)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func sortCardsByID(items []cardkeydomain.Card) {
	slices.SortFunc(items, func(a, b cardkeydomain.Card) int { return cmp.Compare(a.ID, b.ID) })
}

// ── 单卡 ──

// ListCardKeys 单卡列表（服务端筛选 + 分页）。
func (r *Repository) ListCardKeys(ctx context.Context, query cardkeydomain.CardQuery) (*cardkeydomain.CardPage, error) {
	where := []string{"c.appid = $1"}
	args := []any{query.AppID}

	if query.BatchID > 0 {
		args = append(args, query.BatchID)
		where = append(where, fmt.Sprintf("c.batch_id = $%d", len(args)))
	}
	if query.Kind != "" {
		args = append(args, query.Kind)
		where = append(where, fmt.Sprintf("c.kind = $%d", len(args)))
	}
	if query.UserID > 0 {
		args = append(args, query.UserID)
		where = append(where, fmt.Sprintf("c.bound_user_id = $%d", len(args)))
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		normalized := cardkeydomain.NormalizeCode(keyword)
		args = append(args, "%"+keyword+"%")
		clause := fmt.Sprintf("c.code ILIKE $%d OR c.remark ILIKE $%d OR c.distribution_note ILIKE $%d OR u.account ILIKE $%d",
			len(args), len(args), len(args), len(args))
		if normalized != "" && normalized != keyword {
			args = append(args, "%"+normalized+"%")
			clause += fmt.Sprintf(" OR c.code ILIKE $%d", len(args))
		}
		where = append(where, "("+clause+")")
	}
	switch query.Distributed {
	case "yes":
		where = append(where, "c.distributed_at IS NOT NULL")
	case "no":
		where = append(where, "c.distributed_at IS NULL")
	}
	// 「已过期」不是一档状态，它在这里被翻译成时间条件；
	// 反过来，「未使用」「使用中」也要排除掉已过期的，否则同一张卡会同时出现在两档筛选里。
	switch query.Status {
	case "":
	case "expired":
		where = append(where, "c.status <> 'disabled' AND c.expires_at IS NOT NULL AND c.expires_at <= NOW()")
	case "partial":
		where = append(where, "c.status = 'unused' AND c.use_count > 0")
	case cardkeydomain.StatusUnused, cardkeydomain.StatusActive:
		args = append(args, query.Status)
		where = append(where, fmt.Sprintf("c.status = $%d AND (c.expires_at IS NULL OR c.expires_at > NOW())", len(args)))
	default:
		args = append(args, query.Status)
		where = append(where, fmt.Sprintf("c.status = $%d", len(args)))
	}

	from := ` FROM card_keys c
LEFT JOIN card_key_batches b ON b.id = c.batch_id
LEFT JOIN users u ON u.id = c.bound_user_id `
	clause := "WHERE " + strings.Join(where, " AND ")

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)`+from+clause, args...).Scan(&total); err != nil {
		return nil, err
	}

	page, limit := cardKeyPage(query.Page, query.Limit)
	args = append(args, limit, (page-1)*limit)

	rows, err := r.pool.Query(ctx, `SELECT `+cardKeyColumnsAs("c")+`,
COALESCE(b.name, ''), COALESCE(u.account, ''),
(SELECT COUNT(*) FROM card_key_devices d WHERE d.card_key_id = c.id)`+from+clause+
		fmt.Sprintf(" ORDER BY c.id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]cardkeydomain.Card, 0, limit)
	for rows.Next() {
		var item cardkeydomain.Card
		targets := append(cardKeyScanTargets(&item), &item.BatchName, &item.BoundAccount, &item.DeviceCount)
		if err := rows.Scan(targets...); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &cardkeydomain.CardPage{Items: items, Total: total, Page: page, Limit: limit}, nil
}

func cardKeyPage(page int, limit int) (int, int) {
	if page < 1 {
		page = 1
	}
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	return page, limit
}

// ListCardKeysForExport 按批次取出卡面，供导出使用。
//
// 刻意不复用列表接口：导出的是**这一批**而不是「当前筛选条件」。
// 生成完立刻导出时列表可能还没刷新到那一批，按筛选导出会得到一份不完整的卡。
func (r *Repository) ListCardKeysForExport(ctx context.Context, query cardkeydomain.ExportQuery) ([]cardkeydomain.Card, error) {
	where := []string{"c.appid = $1", "c.batch_id = $2"}
	if query.Status == cardkeydomain.StatusUnused {
		where = append(where, "c.status = 'unused' AND (c.expires_at IS NULL OR c.expires_at > NOW())")
	}
	if query.OnlyUndistributed {
		where = append(where, "c.distributed_at IS NULL")
	}
	rows, err := r.pool.Query(ctx, `SELECT `+cardKeyColumnsAs("c")+`, COALESCE(u.account, ''),
(SELECT COUNT(*) FROM card_key_devices d WHERE d.card_key_id = c.id)
FROM card_keys c LEFT JOIN users u ON u.id = c.bound_user_id
WHERE `+strings.Join(where, " AND ")+` ORDER BY c.id`, query.AppID, query.BatchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]cardkeydomain.Card, 0, 256)
	for rows.Next() {
		var item cardkeydomain.Card
		targets := append(cardKeyScanTargets(&item), &item.BoundAccount, &item.DeviceCount)
		if err := rows.Scan(targets...); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// FindCardKeyByCode 按卡面文本查卡。兑换、预览与登录都是从这里进来的。
func (r *Repository) FindCardKeyByCode(ctx context.Context, appID int64, code string) (*cardkeydomain.Card, error) {
	return scanCardKey(r.pool.QueryRow(ctx,
		`SELECT `+cardKeyColumns+` FROM card_keys WHERE appid = $1 AND code = $2`, appID, code))
}

// FindCardKeyByID 按主键查卡（管理端入口，顺带校验它属于这个应用）。
func (r *Repository) FindCardKeyByID(ctx context.Context, appID int64, cardID int64) (*cardkeydomain.Card, error) {
	return scanCardKey(r.pool.QueryRow(ctx,
		`SELECT `+cardKeyColumns+` FROM card_keys WHERE appid = $1 AND id = $2`, appID, cardID))
}

// GetCardKeyDetail 一张卡的全貌：卡、批次、设备、核销流水与解绑流水。
func (r *Repository) GetCardKeyDetail(ctx context.Context, appID int64, cardID int64) (*cardkeydomain.CardDetail, error) {
	var card cardkeydomain.Card
	targets := append(cardKeyScanTargets(&card), &card.BatchName, &card.BoundAccount)
	if err := r.pool.QueryRow(ctx, `SELECT `+cardKeyColumnsAs("c")+`, COALESCE(b.name, ''), COALESCE(u.account, '')
FROM card_keys c
LEFT JOIN card_key_batches b ON b.id = c.batch_id
LEFT JOIN users u ON u.id = c.bound_user_id
WHERE c.appid = $1 AND c.id = $2`, appID, cardID).Scan(targets...); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCardKeyNotFound
		}
		return nil, err
	}
	batch, err := r.GetCardKeyBatch(ctx, appID, card.BatchID)
	if err != nil {
		return nil, err
	}
	devices, err := r.ListCardKeyDevices(ctx, card.ID)
	if err != nil {
		return nil, err
	}
	card.DeviceCount = len(devices)
	redemptions, err := r.ListCardKeyRedemptions(ctx, cardkeydomain.RedemptionQuery{
		AppID: appID, CardID: card.ID, Page: 1, Limit: 50,
	})
	if err != nil {
		return nil, err
	}
	unbinds, err := r.ListCardKeyUnbinds(ctx, card.ID, 50)
	if err != nil {
		return nil, err
	}
	return &cardkeydomain.CardDetail{
		Card: card, Batch: *batch, Devices: devices, Redemptions: redemptions.Items, Unbinds: unbinds,
	}, nil
}

// FilterExistingCardKeyCodes 返回这批卡面里已经被占用的那些。
//
// 生成时的批外去重。卡面格式可以配得很短（1 段 4 位纯数字只有一万种组合），
// 而同一个应用里很可能已经存在同样格式的老批次 —— 不查一次的话，
// 撞码会以「CopyFrom 违反唯一约束」的形式让整批生成失败。
func (r *Repository) FilterExistingCardKeyCodes(ctx context.Context, appID int64, codes []string) (map[string]struct{}, error) {
	out := make(map[string]struct{})
	if len(codes) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx,
		`SELECT code FROM card_keys WHERE appid = $1 AND code = ANY($2)`, appID, codes)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err != nil {
			return nil, err
		}
		out[code] = struct{}{}
	}
	return out, rows.Err()
}

// UpdateCardKey 管理员调整一张卡：备注、可绑设备数、到期时间。
func (r *Repository) UpdateCardKey(ctx context.Context, input cardkeydomain.CardUpdateInput) (*cardkeydomain.Card, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	card, err := scanCardKey(tx.QueryRow(ctx,
		`SELECT `+cardKeyColumns+` FROM card_keys WHERE appid = $1 AND id = $2 FOR UPDATE`, input.AppID, input.CardID))
	if err != nil {
		return nil, err
	}

	remark := card.Remark
	if input.Remark != nil {
		remark = *input.Remark
	}
	maxDevices := card.MaxDevices
	if input.MaxDevices != nil {
		maxDevices = *input.MaxDevices
	}
	expiresAt := card.ExpiresAt
	switch input.ExpiryAction {
	case cardkeydomain.ExpiryPermanent:
		expiresAt = nil
	case cardkeydomain.ExpirySet:
		expiresAt = input.ExpiresAt
	case cardkeydomain.ExpiryExtend:
		// 延长从「原到期时间与现在两者较晚的那个」起算：给一张已经过期的卡加 7 天，
		// 运营的本意是「再给他用 7 天」，而不是「到期时间改成上周的 7 天之后」（仍然是过期的）。
		base := time.Now().UTC()
		if card.ExpiresAt != nil && card.ExpiresAt.After(base) {
			base = *card.ExpiresAt
		}
		if card.ExpiresAt == nil && card.Kind == cardkeydomain.KindRedeem {
			// 永久的兑换卡延长没有意义；保持永久。
			break
		}
		next := base.AddDate(0, 0, input.ExtendDays)
		expiresAt = &next
	}

	updated, err := scanCardKey(tx.QueryRow(ctx, `UPDATE card_keys
SET remark = $3, max_devices = $4, expires_at = $5, updated_at = NOW()
WHERE appid = $1 AND id = $2
RETURNING `+cardKeyColumns, input.AppID, input.CardID, remark, maxDevices, expiresAt))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	tx = nil
	return updated, nil
}

// DisableCardKeys 批量作废。
//
// 按选中的 id 列表作废，不提供「按筛选条件批量」：管理员看到的列表与实际执行
// 之间存在时间差（翻页期间有人兑换），按条件批量会误伤没被看过的卡。
//
// 已核销的卡不再改动 —— 那笔权益已经发出去了，把它标成「已作废」
// 会让核销记录与卡状态自相矛盾。用了一部分的活动码可以作废（之后的人用不了）。
func (r *Repository) DisableCardKeys(ctx context.Context, appID int64, ids []int64, reason string) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	tag, err := r.pool.Exec(ctx, `UPDATE card_keys
SET status = 'disabled', disabled_at = NOW(), disabled_reason = $3, updated_at = NOW()
WHERE appid = $1 AND id = ANY($2) AND status <> 'used' AND status <> 'disabled'`,
		appID, ids, reason)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// RestoreCardKeys 撤销作废，把卡放回作废前的状态。
func (r *Repository) RestoreCardKeys(ctx context.Context, appID int64, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	tag, err := r.pool.Exec(ctx, `UPDATE card_keys
SET status = CASE WHEN kind = 'login' AND bound_user_id IS NOT NULL THEN 'active'
                  WHEN kind = 'redeem' AND use_count >= max_uses THEN 'used'
                  ELSE 'unused' END,
    disabled_at = NULL, disabled_reason = '', updated_at = NOW()
WHERE appid = $1 AND id = ANY($2) AND status = 'disabled'`, appID, ids)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// DeleteCardKeys 删除从没被用过的卡（已用过的有核销流水，只能作废）。
func (r *Repository) DeleteCardKeys(ctx context.Context, appID int64, ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `DELETE FROM card_keys c
WHERE c.appid = $1 AND c.id = ANY($2) AND c.use_count = 0 AND c.bound_user_id IS NULL
  AND NOT EXISTS (SELECT 1 FROM card_key_redemptions x WHERE x.card_key_id = c.id)
RETURNING c.batch_id`, appID, ids)
	if err != nil {
		return 0, err
	}
	perBatch := map[int64]int{}
	var deleted int64
	for rows.Next() {
		var batchID int64
		if err := rows.Scan(&batchID); err != nil {
			rows.Close()
			return 0, err
		}
		perBatch[batchID]++
		deleted++
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for batchID, count := range perBatch {
		if _, err := tx.Exec(ctx, `UPDATE card_key_batches SET total = GREATEST(total - $2, 0), updated_at = NOW()
WHERE id = $1`, batchID, count); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return deleted, nil
}

// ── 设备绑定 ──

// ListCardKeyDevices 一张卡绑定的设备。
func (r *Repository) ListCardKeyDevices(ctx context.Context, cardKeyID int64) ([]cardkeydomain.Device, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, card_key_id, device_id, device_name, first_seen_at, last_seen_at, seen_count
FROM card_key_devices WHERE card_key_id = $1 ORDER BY last_seen_at DESC`, cardKeyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]cardkeydomain.Device, 0, 8)
	for rows.Next() {
		var item cardkeydomain.Device
		if err := rows.Scan(&item.ID, &item.CardKeyID, &item.DeviceID, &item.DeviceName,
			&item.FirstSeenAt, &item.LastSeenAt, &item.SeenCount); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ListCardKeyUnbinds 一张卡的解绑流水。
func (r *Repository) ListCardKeyUnbinds(ctx context.Context, cardKeyID int64, limit int) ([]cardkeydomain.Unbind, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, card_key_id, device_id, device_name, source, operator, created_at
FROM card_key_device_unbinds WHERE card_key_id = $1 ORDER BY id DESC LIMIT $2`, cardKeyID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]cardkeydomain.Unbind, 0, 4)
	for rows.Next() {
		var item cardkeydomain.Unbind
		if err := rows.Scan(&item.ID, &item.CardKeyID, &item.DeviceID, &item.DeviceName, &item.Source,
			&item.Operator, &item.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// CountCardKeySelfUnbinds 一张卡在窗口内自助解绑了几次。
func (r *Repository) CountCardKeySelfUnbinds(ctx context.Context, cardKeyID int64, since time.Time) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM card_key_device_unbinds
WHERE card_key_id = $1 AND source = 'self' AND created_at > $2`, cardKeyID, since).Scan(&count)
	return count, err
}

// UnbindCardKeyDevice 解绑一台设备，把名额还回去（单事务：删绑定 + 记流水 + 自助限次）。
//
// 这是「用户换电脑了」唯一的出口。没有它，一张一机卡在用户重装系统之后
// 就永久报废了 —— 而设备标识会随重装变化。
//
// 自助解绑的限次在锁住卡行之后数：同一张卡的两次并发自助解绑会串行，第二次数到的是第一次之后的值。
func (r *Repository) UnbindCardKeyDevice(ctx context.Context, input cardkeydomain.UnbindInput) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	card, err := scanCardKey(tx.QueryRow(ctx,
		`SELECT `+cardKeyColumns+` FROM card_keys WHERE appid = $1 AND id = $2 FOR UPDATE`, input.AppID, input.CardID))
	if err != nil {
		return err
	}
	if input.Source == cardkeydomain.UnbindSelf {
		if card.BoundUserID == nil || *card.BoundUserID != input.UserID {
			return ErrCardKeyNotFound
		}
		if input.Limit <= 0 {
			return ErrCardKeyUnbindDisabled
		}
		var used int
		if err := tx.QueryRow(ctx, `SELECT COUNT(*) FROM card_key_device_unbinds
WHERE card_key_id = $1 AND source = 'self' AND created_at > $2`,
			card.ID, time.Now().UTC().Add(-cardkeydomain.SelfUnbindWindow)).Scan(&used); err != nil {
			return err
		}
		if used >= input.Limit {
			return ErrCardKeyUnbindLimit
		}
	}

	var deviceName string
	if err := tx.QueryRow(ctx, `DELETE FROM card_key_devices WHERE appid = $1 AND card_key_id = $2 AND device_id = $3
RETURNING device_name`, input.AppID, card.ID, input.DeviceID).Scan(&deviceName); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrCardKeyDeviceMissing
		}
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO card_key_device_unbinds (appid, card_key_id, device_id, device_name, source, operator)
VALUES ($1, $2, $3, $4, $5, $6)`, input.AppID, card.ID, input.DeviceID, deviceName, input.Source,
		truncateColumn(input.Operator, 128)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ResetCardKeyDevices 清空一张卡的全部设备（管理员）。
func (r *Repository) ResetCardKeyDevices(ctx context.Context, appID int64, cardID int64, operator string) (int64, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := scanCardKey(tx.QueryRow(ctx,
		`SELECT `+cardKeyColumns+` FROM card_keys WHERE appid = $1 AND id = $2 FOR UPDATE`, appID, cardID)); err != nil {
		return 0, err
	}
	tag, err := tx.Exec(ctx, `WITH removed AS (
    DELETE FROM card_key_devices WHERE appid = $1 AND card_key_id = $2 RETURNING device_id, device_name
)
INSERT INTO card_key_device_unbinds (appid, card_key_id, device_id, device_name, source, operator)
SELECT $1, $2, device_id, device_name, 'admin', $3 FROM removed`, appID, cardID, truncateColumn(operator, 128))
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

// ── 核销记录 ──

// ListCardKeyRedemptions 核销记录（服务端筛选 + 分页）。
func (r *Repository) ListCardKeyRedemptions(ctx context.Context, query cardkeydomain.RedemptionQuery) (*cardkeydomain.RedemptionPage, error) {
	where := []string{"x.appid = $1"}
	args := []any{query.AppID}

	if query.BatchID > 0 {
		args = append(args, query.BatchID)
		where = append(where, fmt.Sprintf("x.batch_id = $%d", len(args)))
	}
	if query.CardID > 0 {
		args = append(args, query.CardID)
		where = append(where, fmt.Sprintf("x.card_key_id = $%d", len(args)))
	}
	if query.UserID > 0 {
		args = append(args, query.UserID)
		where = append(where, fmt.Sprintf("x.user_id = $%d", len(args)))
	}
	if query.Source != "" {
		args = append(args, query.Source)
		where = append(where, fmt.Sprintf("x.source = $%d", len(args)))
	}
	if query.From != nil {
		args = append(args, *query.From)
		where = append(where, fmt.Sprintf("x.created_at >= $%d", len(args)))
	}
	if query.To != nil {
		args = append(args, *query.To)
		where = append(where, fmt.Sprintf("x.created_at < $%d", len(args)))
	}
	if keyword := strings.TrimSpace(query.Keyword); keyword != "" {
		args = append(args, "%"+keyword+"%")
		clause := fmt.Sprintf("x.code ILIKE $%d OR u.account ILIKE $%d", len(args), len(args))
		if normalized := cardkeydomain.NormalizeCode(keyword); normalized != "" && normalized != keyword {
			args = append(args, "%"+normalized+"%")
			clause += fmt.Sprintf(" OR x.code ILIKE $%d", len(args))
		}
		where = append(where, "("+clause+")")
	}
	clause := "WHERE " + strings.Join(where, " AND ")
	from := ` FROM card_key_redemptions x
LEFT JOIN users u ON u.id = x.user_id
LEFT JOIN user_profiles up ON up.user_id = x.user_id
LEFT JOIN card_key_batches b ON b.id = x.batch_id `

	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*)`+from+clause, args...).Scan(&total); err != nil {
		return nil, err
	}

	page, limit := cardKeyPage(query.Page, query.Limit)
	args = append(args, limit, (page-1)*limit)

	rows, err := r.pool.Query(ctx, `SELECT x.id, x.appid, x.card_key_id, x.batch_id, x.code, x.user_id,
x.rewards, x.results, x.source, x.device_id, x.client_ip, x.user_agent, x.operator, x.created_at,
COALESCE(u.account, ''), COALESCE(up.nickname, ''), COALESCE(b.name, ''), COALESCE(b.kind, '')`+from+clause+
		fmt.Sprintf(" ORDER BY x.id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]cardkeydomain.Redemption, 0, limit)
	for rows.Next() {
		var item cardkeydomain.Redemption
		var rewards, results []byte
		var deviceID, clientIP, userAgent, operator *string
		if err := rows.Scan(&item.ID, &item.AppID, &item.CardKeyID, &item.BatchID, &item.Code,
			&item.UserID, &rewards, &results, &item.Source, &deviceID, &clientIP, &userAgent,
			&operator, &item.CreatedAt, &item.Account, &item.Nickname, &item.BatchName, &item.Kind); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(rewards, &item.Rewards)
		_ = json.Unmarshal(results, &item.Results)
		if item.Rewards == nil {
			item.Rewards = []cardkeydomain.Reward{}
		}
		if item.Results == nil {
			item.Results = []cardkeydomain.RewardResult{}
		}
		item.DeviceID = derefString(deviceID)
		item.ClientIP = derefString(clientIP)
		item.UserAgent = derefString(userAgent)
		item.Operator = derefString(operator)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &cardkeydomain.RedemptionPage{Items: items, Total: total, Page: page, Limit: limit}, nil
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// ── 统计 ──

// CardKeyStats 卡密概览：总量、可用、核销趋势、来源分布与热门批次。
//
// 趋势按 timezone 分日（控制台传浏览器时区），since 为趋势的起点。
func (r *Repository) CardKeyStats(ctx context.Context, appID int64, since time.Time, dayStart time.Time, timezone string) (*cardkeydomain.Stats, error) {
	stats := &cardkeydomain.Stats{Trend: []cardkeydomain.TrendPoint{}, Sources: []cardkeydomain.SourceCount{},
		TopBatches: []cardkeydomain.BatchUsage{}}

	var issued, used string
	if err := r.pool.QueryRow(ctx, `SELECT
(SELECT COUNT(*) FROM card_key_batches WHERE appid = $1),
COUNT(*),
COUNT(*) FILTER (WHERE c.status = 'unused' AND (c.expires_at IS NULL OR c.expires_at > NOW())),
COUNT(*) FILTER (WHERE c.status = 'unused' AND c.use_count = 0 AND c.distributed_at IS NULL
                   AND (c.expires_at IS NULL OR c.expires_at > NOW())),
COUNT(*) FILTER (WHERE c.distributed_at IS NOT NULL),
COUNT(*) FILTER (WHERE c.status = 'disabled'),
COUNT(*) FILTER (WHERE c.status <> 'disabled' AND c.expires_at IS NOT NULL AND c.expires_at <= NOW()),
COUNT(*) FILTER (WHERE c.kind = 'login' AND c.status = 'active' AND (c.expires_at IS NULL OR c.expires_at > NOW())),
COALESCE(SUM(b.face_value * c.max_uses), 0)::text,
COALESCE(SUM(b.face_value * c.use_count), 0)::text
FROM card_keys c JOIN card_key_batches b ON b.id = c.batch_id
WHERE c.appid = $1`, appID).Scan(&stats.Batches, &stats.Cards, &stats.Unused, &stats.Available,
		&stats.Distributed, &stats.Disabled, &stats.Expired, &stats.ActiveLicenses, &issued, &used); err != nil {
		return nil, err
	}
	stats.FaceValueIssued, _ = decimal.NewFromString(issued)
	stats.FaceValueUsed, _ = decimal.NewFromString(used)

	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*), COUNT(*) FILTER (WHERE created_at >= $2), COUNT(DISTINCT user_id)
FROM card_key_redemptions WHERE appid = $1`, appID, dayStart).Scan(&stats.Redemptions, &stats.RedemptionsDay,
		&stats.Users); err != nil {
		return nil, err
	}

	rows, err := r.pool.Query(ctx, `SELECT to_char(created_at AT TIME ZONE $3, 'YYYY-MM-DD') AS day,
COUNT(*), COUNT(DISTINCT user_id)
FROM card_key_redemptions WHERE appid = $1 AND created_at >= $2
GROUP BY day ORDER BY day`, appID, since, timezone)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var point cardkeydomain.TrendPoint
		if err := rows.Scan(&point.Day, &point.Redemptions, &point.Users); err != nil {
			rows.Close()
			return nil, err
		}
		stats.Trend = append(stats.Trend, point)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = r.pool.Query(ctx, `SELECT source, COUNT(*) FROM card_key_redemptions
WHERE appid = $1 AND created_at >= $2 GROUP BY source ORDER BY COUNT(*) DESC`, appID, since)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var item cardkeydomain.SourceCount
		if err := rows.Scan(&item.Source, &item.Count); err != nil {
			rows.Close()
			return nil, err
		}
		stats.Sources = append(stats.Sources, item)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = r.pool.Query(ctx, `SELECT b.id, b.name, b.kind, COUNT(*)
FROM card_key_redemptions x JOIN card_key_batches b ON b.id = x.batch_id
WHERE x.appid = $1 AND x.created_at >= $2
GROUP BY b.id, b.name, b.kind ORDER BY COUNT(*) DESC, b.id DESC LIMIT 6`, appID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var item cardkeydomain.BatchUsage
		if err := rows.Scan(&item.BatchID, &item.Name, &item.Kind, &item.Redemptions); err != nil {
			return nil, err
		}
		stats.TopBatches = append(stats.TopBatches, item)
	}
	return stats, rows.Err()
}

// ── 用户端 ──

// ListUserLoginCards 用户名下的授权卡（含设备）。
func (r *Repository) ListUserLoginCards(ctx context.Context, appID int64, userID int64) ([]cardkeydomain.Card, map[int64]*cardkeydomain.Batch, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+cardKeyColumnsAs("c")+`, COALESCE(b.name, '')
FROM card_keys c LEFT JOIN card_key_batches b ON b.id = c.batch_id
WHERE c.appid = $1 AND c.bound_user_id = $2 AND c.kind = 'login'
ORDER BY (c.expires_at IS NULL) DESC, c.expires_at DESC, c.id DESC LIMIT 50`, appID, userID)
	if err != nil {
		return nil, nil, err
	}
	items := make([]cardkeydomain.Card, 0, 2)
	batchIDs := make([]int64, 0, 2)
	for rows.Next() {
		var item cardkeydomain.Card
		targets := append(cardKeyScanTargets(&item), &item.BatchName)
		if err := rows.Scan(targets...); err != nil {
			rows.Close()
			return nil, nil, err
		}
		items = append(items, item)
		batchIDs = append(batchIDs, item.BatchID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	batches := map[int64]*cardkeydomain.Batch{}
	if len(batchIDs) > 0 {
		batchRows, err := r.pool.Query(ctx,
			`SELECT `+cardKeyBatchColumns+` FROM card_key_batches WHERE id = ANY($1)`, batchIDs)
		if err != nil {
			return nil, nil, err
		}
		defer batchRows.Close()
		for batchRows.Next() {
			batch, err := scanCardKeyBatch(batchRows)
			if err != nil {
				return nil, nil, err
			}
			batches[batch.ID] = batch
		}
		if err := batchRows.Err(); err != nil {
			return nil, nil, err
		}
	}
	return items, batches, nil
}

// ListUserCardKeyRedemptions 用户自己的兑换记录。
func (r *Repository) ListUserCardKeyRedemptions(ctx context.Context, appID int64, userID int64, page int, limit int) (*cardkeydomain.ClientRedemptionPage, error) {
	page, limit = cardKeyPage(page, limit)
	if limit > 50 {
		limit = 50
	}
	var total int64
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM card_key_redemptions WHERE appid = $1 AND user_id = $2`,
		appID, userID).Scan(&total); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT x.id, x.code, COALESCE(c.kind, ''), COALESCE(b.name, ''), x.source, x.results, x.created_at
FROM card_key_redemptions x
LEFT JOIN card_keys c ON c.id = x.card_key_id
LEFT JOIN card_key_batches b ON b.id = x.batch_id
WHERE x.appid = $1 AND x.user_id = $2
ORDER BY x.id DESC LIMIT $3 OFFSET $4`, appID, userID, limit, (page-1)*limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]cardkeydomain.ClientRedemption, 0, limit)
	for rows.Next() {
		var item cardkeydomain.ClientRedemption
		var results []byte
		if err := rows.Scan(&item.ID, &item.Code, &item.Kind, &item.BatchName, &item.Source, &results,
			&item.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(results, &item.Results)
		if item.Results == nil {
			item.Results = []cardkeydomain.RewardResult{}
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &cardkeydomain.ClientRedemptionPage{Items: items, Total: total, Page: page, Limit: limit}, nil
}

// HasUserRedeemedCardKey 这个人是否已经兑换过这张卡（活动码的预览要说清楚）。
func (r *Repository) HasUserRedeemedCardKey(ctx context.Context, cardID int64, userID int64) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM card_key_redemptions WHERE card_key_id = $1 AND user_id = $2)`,
		cardID, userID).Scan(&exists)
	return exists, err
}

// CountUserBatchRedemptions 这个人在一批里兑换了几张。
func (r *Repository) CountUserBatchRedemptions(ctx context.Context, batchID int64, userID int64) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM card_key_redemptions WHERE batch_id = $1 AND user_id = $2`,
		batchID, userID).Scan(&count)
	return count, err
}
