package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	redislib "github.com/redis/go-redis/v9"
)

// AccountDeletionCode 注销验证发往绑定邮箱的一次验证码（短信码由 CaptchaService 保管，这里只记发送节奏）。
//
// 只存摘要；与公开的 /auth/email/verify 用的 emailCodeKey 分开放，那个 key 能被逐个试探。
type AccountDeletionCode struct {
	Channel   string    `json:"channel"`
	CodeHash  string    `json:"codeHash,omitempty"`
	ExpiresAt time.Time `json:"expiresAt"`
	SentAt    time.Time `json:"sentAt"`
	Attempts  int       `json:"attempts"`
}

func (r *SessionRepository) accountDeletionCodeKey(appID, userID int64, channel string) string {
	return fmt.Sprintf("%s:account_deletion:code:%d:%d:%s", r.keyPrefix, appID, userID, channel)
}

func (r *SessionRepository) accountDeletionCounterKey(scope string, appID, userID int64) string {
	return fmt.Sprintf("%s:account_deletion:count:%s:%d:%d", r.keyPrefix, scope, appID, userID)
}

// SaveAccountDeletionCode 写入（覆盖）一个渠道的验证码状态。
func (r *SessionRepository) SaveAccountDeletionCode(ctx context.Context, appID, userID int64, code AccountDeletionCode, ttl time.Duration) error {
	data, err := json.Marshal(code)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, r.accountDeletionCodeKey(appID, userID, code.Channel), data, ttl).Err()
}

// GetAccountDeletionCode 读一个渠道的验证码状态；不存在时为 nil。
func (r *SessionRepository) GetAccountDeletionCode(ctx context.Context, appID, userID int64, channel string) (*AccountDeletionCode, error) {
	data, err := r.client.Get(ctx, r.accountDeletionCodeKey(appID, userID, channel)).Bytes()
	if errors.Is(err, redislib.Nil) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var code AccountDeletionCode
	if err := json.Unmarshal(data, &code); err != nil {
		return nil, err
	}
	return &code, nil
}

// DeleteAccountDeletionCode 作废一个渠道的验证码（用过、错太多次或注销完成）。
func (r *SessionRepository) DeleteAccountDeletionCode(ctx context.Context, appID, userID int64, channel string) error {
	return r.client.Del(ctx, r.accountDeletionCodeKey(appID, userID, channel)).Err()
}

// HitAccountDeletionCounter 计数加一并返回当前值；首次出现时设窗口。
func (r *SessionRepository) HitAccountDeletionCounter(ctx context.Context, scope string, appID, userID int64, window time.Duration) (int64, error) {
	key := r.accountDeletionCounterKey(scope, appID, userID)
	n, err := r.client.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	if n == 1 {
		return n, r.client.Expire(ctx, key, window).Err()
	}
	if ttl, err := r.client.PTTL(ctx, key).Result(); err == nil && ttl < 0 {
		_ = r.client.Expire(ctx, key, window).Err()
	}
	return n, nil
}

// PeekAccountDeletionCounter 读计数与剩余窗口，不存在时为 0。
func (r *SessionRepository) PeekAccountDeletionCounter(ctx context.Context, scope string, appID, userID int64) (int64, time.Duration, error) {
	key := r.accountDeletionCounterKey(scope, appID, userID)
	n, err := r.client.Get(ctx, key).Int64()
	if errors.Is(err, redislib.Nil) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	ttl, _ := r.client.PTTL(ctx, key).Result()
	return n, ttl, nil
}

// ResetAccountDeletionCounter 清零一个计数（验证通过后清掉失败计数）。
func (r *SessionRepository) ResetAccountDeletionCounter(ctx context.Context, scope string, appID, userID int64) error {
	return r.client.Del(ctx, r.accountDeletionCounterKey(scope, appID, userID)).Err()
}
