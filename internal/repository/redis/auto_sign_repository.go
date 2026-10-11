package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	userdomain "aegis/internal/domain/user"

	redislib "github.com/redis/go-redis/v9"
)

type AutoSignEntry struct {
	UserID int64
	AppID  int64
}

type AutoSignRepository struct {
	client    *redislib.Client
	keyPrefix string
}

func NewAutoSignRepository(client *redislib.Client, keyPrefix string) *AutoSignRepository {
	return &AutoSignRepository{client: client, keyPrefix: keyPrefix}
}

func (r *AutoSignRepository) Schedule(ctx context.Context, appID int64, userID int64, dueAt time.Time) error {
	return r.client.ZAdd(ctx, r.scheduleKey(), redislib.Z{
		Score:  float64(dueAt.UnixMilli()),
		Member: r.member(appID, userID),
	}).Err()
}

func (r *AutoSignRepository) Remove(ctx context.Context, appID int64, userID int64) error {
	return r.client.ZRem(ctx, r.scheduleKey(), r.member(appID, userID)).Err()
}

func (r *AutoSignRepository) GetDue(ctx context.Context, now time.Time, limit int64) ([]AutoSignEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	values, err := r.client.ZRangeByScore(ctx, r.scheduleKey(), &redislib.ZRangeBy{
		Min:    "-inf",
		Max:    strconv.FormatInt(now.UnixMilli(), 10),
		Offset: 0,
		Count:  limit,
	}).Result()
	if err != nil {
		return nil, err
	}
	items := make([]AutoSignEntry, 0, len(values))
	for _, value := range values {
		entry, ok := parseAutoSignMember(value)
		if !ok {
			continue
		}
		items = append(items, entry)
	}
	return items, nil
}

func (r *AutoSignRepository) Count(ctx context.Context) (int64, error) {
	return r.client.ZCard(ctx, r.scheduleKey()).Result()
}

// Claim 认领一条到期的排班：从排班里摘掉，摘到的那个实例才执行。
//
// API 与 Worker 都跑调度循环，同一条到期排班会被两边同时取到；不认领就会签两次、
// 发两条通知。认领后进程若中途退出，排班由周期性的重建补回。
func (r *AutoSignRepository) Claim(ctx context.Context, appID int64, userID int64) (bool, error) {
	removed, err := r.client.ZRem(ctx, r.scheduleKey(), r.member(appID, userID)).Result()
	if err != nil {
		return false, err
	}
	return removed > 0, nil
}

// NextRunAt 排班上的下一次执行时刻；不在排班上返回 nil。
func (r *AutoSignRepository) NextRunAt(ctx context.Context, appID int64, userID int64) (*time.Time, error) {
	score, err := r.client.ZScore(ctx, r.scheduleKey(), r.member(appID, userID)).Result()
	if errors.Is(err, redislib.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	at := time.UnixMilli(int64(score))
	return &at, nil
}

const autoSignRunTTL = 30 * 24 * time.Hour

func (r *AutoSignRepository) SetLastRun(ctx context.Context, appID int64, userID int64, run userdomain.AutoSignRun) error {
	payload, err := json.Marshal(run)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, r.lastRunKey(appID, userID), payload, autoSignRunTTL).Err()
}

func (r *AutoSignRepository) GetLastRun(ctx context.Context, appID int64, userID int64) (*userdomain.AutoSignRun, error) {
	payload, err := r.client.Get(ctx, r.lastRunKey(appID, userID)).Bytes()
	if errors.Is(err, redislib.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var run userdomain.AutoSignRun
	if err := json.Unmarshal(payload, &run); err != nil {
		return nil, nil
	}
	return &run, nil
}

// IncrAttempt 某个签到日的失败次数加一，返回加完后的次数。按签到日分键，次日自然从零开始。
func (r *AutoSignRepository) IncrAttempt(ctx context.Context, appID int64, userID int64, signDate string) (int, error) {
	key := r.attemptKey(appID, userID, signDate)
	count, err := r.client.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	if count == 1 {
		_ = r.client.Expire(ctx, key, 48*time.Hour).Err()
	}
	return int(count), nil
}

func (r *AutoSignRepository) ClearAttempts(ctx context.Context, appID int64, userID int64, signDate string) error {
	return r.client.Del(ctx, r.attemptKey(appID, userID, signDate)).Err()
}

func (r *AutoSignRepository) scheduleKey() string {
	return fmt.Sprintf("%s:autosign:schedule", r.keyPrefix)
}

func (r *AutoSignRepository) lastRunKey(appID int64, userID int64) string {
	return fmt.Sprintf("%s:autosign:last:%d:%d", r.keyPrefix, appID, userID)
}

func (r *AutoSignRepository) attemptKey(appID int64, userID int64, signDate string) string {
	return fmt.Sprintf("%s:autosign:attempt:%d:%d:%s", r.keyPrefix, appID, userID, signDate)
}

func (r *AutoSignRepository) member(appID int64, userID int64) string {
	return fmt.Sprintf("%d:%d", appID, userID)
}

func parseAutoSignMember(value string) (AutoSignEntry, bool) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return AutoSignEntry{}, false
	}
	appID, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return AutoSignEntry{}, false
	}
	userID, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return AutoSignEntry{}, false
	}
	return AutoSignEntry{AppID: appID, UserID: userID}, true
}
