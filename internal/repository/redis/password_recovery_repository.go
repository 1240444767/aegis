package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	redislib "github.com/redis/go-redis/v9"
)

// PasswordRecoveryFlow 一次找回密码的全过程。
//
// 一个流程就是一个 JSON 值，key 由流程令牌的 SHA-256 派生 —— 令牌明文只在
// 发起响应里出现一次，Redis 里拿不到能直接用的东西。生存期由 key 的 TTL 决定：
// 发起时设好，全部验证通过后续一次「设新密码」的窗口，此外不续期。
//
// 每一步都经 UpdatePasswordRecoveryFlow 在 WATCH 事务里读改写：并发的猜码请求
// 只会有一个成功落盘，失败计数不会因为并发而少记。
type PasswordRecoveryFlow struct {
	AppID  int64 `json:"appId"`
	UserID int64 `json:"userId"`
	// Mode recover（未登录，凭账号发起）或 session（已登录，凭会话发起）
	Mode     string `json:"mode"`
	Account  string `json:"account"`
	DeviceID string `json:"deviceId,omitempty"`
	IP       string `json:"ip,omitempty"`
	// KeepTokenID / KeepFamilyID 已登录发起时的当前会话：设新密码后保留它，其余全部下线
	KeepTokenID  string `json:"keepTokenId,omitempty"`
	KeepFamilyID string `json:"keepFamilyId,omitempty"`

	Channels     []PasswordRecoveryChannel     `json:"channels"`
	TOTP         bool                          `json:"totp"`
	Requirements []PasswordRecoveryRequirement `json:"requirements"`

	// Failures 本流程累计的失败次数（验证码错、联系方式对不上），到上限即作废
	Failures  int       `json:"failures"`
	CreatedAt time.Time `json:"createdAt"`
	ExpiresAt time.Time `json:"expiresAt"`
	// VerifiedAt 全部要求都满足的时刻；此后只剩「设新密码」一步
	VerifiedAt *time.Time `json:"verifiedAt,omitempty"`
}

// PasswordRecoveryChannel 一个可以收验证码的联系方式。
type PasswordRecoveryChannel struct {
	Type   string `json:"type"`
	Value  string `json:"value"`
	Masked string `json:"masked"`
	// Sends / LastSentAt 本流程里向它发过几次、最近一次是什么时候
	Sends      int        `json:"sends"`
	LastSentAt *time.Time `json:"lastSentAt,omitempty"`
	// CodeHash 邮箱验证码的摘要（短信码由 CaptchaService 保管）
	CodeHash      string     `json:"codeHash,omitempty"`
	CodeExpiresAt *time.Time `json:"codeExpiresAt,omitempty"`
	CodeAttempts  int        `json:"codeAttempts"`
	Verified      bool       `json:"verified"`
}

// PasswordRecoveryRequirement 一项验证要求：Any 里任意一种方式通过即满足。
type PasswordRecoveryRequirement struct {
	Any []string `json:"any"`
	// By 实际满足它的方式；空表示尚未满足
	By string `json:"by,omitempty"`
}

// ErrPasswordRecoveryFlowMissing 流程不存在或已过期。
var ErrPasswordRecoveryFlowMissing = errors.New("password recovery flow missing")

// ErrPasswordRecoveryFlowBusy 同一流程的并发写入在重试后仍然冲突。
var ErrPasswordRecoveryFlowBusy = errors.New("password recovery flow busy")

func (r *SessionRepository) passwordRecoveryFlowKey(appID int64, tokenHash string) string {
	return fmt.Sprintf("%s:auth:pwreset:flow:%d:%s", r.keyPrefix, appID, tokenHash)
}

func (r *SessionRepository) passwordRecoveryCounterKey(scope string, subject string) string {
	return fmt.Sprintf("%s:auth:pwreset:count:%s:%s", r.keyPrefix, scope, subject)
}

func (r *SessionRepository) contactChangedKey(appID int64, userID int64, field string) string {
	return fmt.Sprintf("%s:auth:contact_changed:%d:%d:%s", r.keyPrefix, appID, userID, field)
}

// CreatePasswordRecoveryFlow 写入一个新流程。
func (r *SessionRepository) CreatePasswordRecoveryFlow(ctx context.Context, tokenHash string, flow PasswordRecoveryFlow, ttl time.Duration) error {
	data, err := json.Marshal(flow)
	if err != nil {
		return err
	}
	return r.client.Set(ctx, r.passwordRecoveryFlowKey(flow.AppID, tokenHash), data, ttl).Err()
}

// GetPasswordRecoveryFlow 读取流程；不存在时返回 nil, nil。
func (r *SessionRepository) GetPasswordRecoveryFlow(ctx context.Context, appID int64, tokenHash string) (*PasswordRecoveryFlow, error) {
	raw, err := r.client.Get(ctx, r.passwordRecoveryFlowKey(appID, tokenHash)).Bytes()
	if err != nil {
		if errors.Is(err, redislib.Nil) {
			return nil, nil
		}
		return nil, err
	}
	var flow PasswordRecoveryFlow
	if err := json.Unmarshal(raw, &flow); err != nil {
		return nil, err
	}
	return &flow, nil
}

// PasswordRecoveryMutation 对流程的一次修改。
//
// 返回 keep=false 表示删除流程（作废或已用完）；ttl 非零时按它重设过期时间，
// 否则沿用剩余 TTL。返回的 error 会原样交给调用方，此时修改仍会落盘 ——
// 「验证码错了」既要报错，也要把失败次数记下来。
type PasswordRecoveryMutation func(flow *PasswordRecoveryFlow) (keep bool, ttl time.Duration, err error)

// UpdatePasswordRecoveryFlow 在 WATCH 事务里读改写一个流程，返回修改后的流程。
func (r *SessionRepository) UpdatePasswordRecoveryFlow(ctx context.Context, appID int64, tokenHash string, mutate PasswordRecoveryMutation) (*PasswordRecoveryFlow, error) {
	key := r.passwordRecoveryFlowKey(appID, tokenHash)
	var (
		result    *PasswordRecoveryFlow
		mutateErr error
	)
	txn := func(tx *redislib.Tx) error {
		raw, err := tx.Get(ctx, key).Bytes()
		if err != nil {
			if errors.Is(err, redislib.Nil) {
				return ErrPasswordRecoveryFlowMissing
			}
			return err
		}
		var flow PasswordRecoveryFlow
		if err := json.Unmarshal(raw, &flow); err != nil {
			return err
		}
		remaining, err := tx.PTTL(ctx, key).Result()
		if err != nil {
			return err
		}
		keep, ttl, err := mutate(&flow)
		mutateErr = err
		if ttl <= 0 {
			ttl = remaining
		}
		data, err := json.Marshal(flow)
		if err != nil {
			return err
		}
		_, err = tx.TxPipelined(ctx, func(pipe redislib.Pipeliner) error {
			if !keep || ttl <= 0 {
				pipe.Del(ctx, key)
				return nil
			}
			pipe.Set(ctx, key, data, ttl)
			return nil
		})
		if err == nil {
			result = &flow
		}
		return err
	}
	for attempt := 0; attempt < 3; attempt++ {
		err := r.client.Watch(ctx, txn, key)
		if err == nil {
			return result, mutateErr
		}
		if errors.Is(err, redislib.TxFailedErr) {
			continue
		}
		return nil, err
	}
	return nil, ErrPasswordRecoveryFlowBusy
}

// DeletePasswordRecoveryFlow 删除流程。
func (r *SessionRepository) DeletePasswordRecoveryFlow(ctx context.Context, appID int64, tokenHash string) error {
	return r.client.Del(ctx, r.passwordRecoveryFlowKey(appID, tokenHash)).Err()
}

// HitPasswordRecoveryCounter 给一个限流计数加一并返回加后的值；首次出现时设窗口。
func (r *SessionRepository) HitPasswordRecoveryCounter(ctx context.Context, scope string, subject string, window time.Duration) (int64, error) {
	key := r.passwordRecoveryCounterKey(scope, subject)
	n, err := r.client.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	// 不用 EXPIRE NX（Redis 7 才有）：首次出现，或上次设窗口时中断留下了无期限的计数，都补上窗口
	if n == 1 {
		return n, r.client.Expire(ctx, key, window).Err()
	}
	if ttl, err := r.client.PTTL(ctx, key).Result(); err == nil && ttl < 0 {
		_ = r.client.Expire(ctx, key, window).Err()
	}
	return n, nil
}

// PeekPasswordRecoveryCounter 读一个限流计数，不存在时为 0。
func (r *SessionRepository) PeekPasswordRecoveryCounter(ctx context.Context, scope string, subject string) (int64, error) {
	n, err := r.client.Get(ctx, r.passwordRecoveryCounterKey(scope, subject)).Int64()
	if errors.Is(err, redislib.Nil) {
		return 0, nil
	}
	return n, err
}

// MarkContactChanged 记下一个联系方式刚被换绑，冷静期内它不能用来找回密码。
func (r *SessionRepository) MarkContactChanged(ctx context.Context, appID int64, userID int64, field string, cooldown time.Duration) error {
	return r.client.Set(ctx, r.contactChangedKey(appID, userID, field), time.Now().UTC().Unix(), cooldown).Err()
}

// ContactChangeCooldown 联系方式冷静期还剩多久；不在冷静期时为 0。
func (r *SessionRepository) ContactChangeCooldown(ctx context.Context, appID int64, userID int64, field string) (time.Duration, error) {
	ttl, err := r.client.PTTL(ctx, r.contactChangedKey(appID, userID, field)).Result()
	if err != nil {
		return 0, err
	}
	if ttl < 0 {
		return 0, nil
	}
	return ttl, nil
}
