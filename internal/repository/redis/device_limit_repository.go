package redis

import (
	"context"
	"errors"
	"fmt"
	"time"

	"aegis/internal/domain/devicelimit"

	redislib "github.com/redis/go-redis/v9"
)

// 登录设备数控制在 Redis 上的状态：同一用户登录的串行锁、选择页暂存的登录现场、
// 以及「为什么失效」—— 被挤下线的令牌在黑名单里记下原因，客户端据此区分
// 「在别处登录被顶掉」与普通的登录过期。

// RevokeReason 令牌或令牌族失效的原因。普通注销、改密等仍写 "1"，读出来就是 RevokeReasonGeneric。
const (
	RevokeReasonGeneric     = "1"
	RevokeReasonDeviceLimit = "device_limit"
	RevokeReasonRemoved     = "device_removed"
)

// BlacklistTokenWithReason 把令牌拉黑并记下原因。
func (r *SessionRepository) BlacklistTokenWithReason(ctx context.Context, tokenID, reason string, ttl time.Duration) error {
	if reason == "" {
		reason = RevokeReasonGeneric
	}
	return r.client.Set(ctx, r.blacklistKey(tokenID), reason, ttl).Err()
}

// TokenBlacklistReason 令牌是否在黑名单里、原因是什么。
func (r *SessionRepository) TokenBlacklistReason(ctx context.Context, tokenID string) (string, bool, error) {
	value, err := r.client.Get(ctx, r.blacklistKey(tokenID)).Result()
	if errors.Is(err, redislib.Nil) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

// RevokeRefreshFamilyWithReason 撤销一个刷新令牌族并记下原因。
func (r *SessionRepository) RevokeRefreshFamilyWithReason(ctx context.Context, appID, userID int64, familyID, reason string, ttl time.Duration) error {
	if familyID == "" {
		return nil
	}
	if ttl <= 0 {
		ttl = time.Hour
	}
	if reason == "" {
		reason = RevokeReasonGeneric
	}
	return r.client.Set(ctx, r.refreshFamilyRevokedKey(appID, userID, familyID), reason, ttl).Err()
}

// RefreshFamilyRevokedReason 刷新令牌族是否已撤销、原因是什么。
func (r *SessionRepository) RefreshFamilyRevokedReason(ctx context.Context, appID, userID int64, familyID string) (string, bool, error) {
	if familyID == "" {
		return "", false, nil
	}
	value, err := r.client.Get(ctx, r.refreshFamilyRevokedKey(appID, userID, familyID)).Result()
	if errors.Is(err, redislib.Nil) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

// deviceLimitUnlockScript 只删自己持有的锁：锁过期后被别人拿走时，不能把别人的锁删了。
var deviceLimitUnlockScript = redislib.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0`)

// AcquireDeviceLimitLock 同一用户的登录判定与签发串行化，避免两台设备同时登录时都以为还有空位。
// 等到 wait 仍拿不到返回 false（调用方按不加锁继续，不因锁阻塞登录）。
func (r *SessionRepository) AcquireDeviceLimitLock(ctx context.Context, appID, userID int64, token string, ttl, wait time.Duration) (bool, error) {
	key := r.deviceLimitLockKey(appID, userID)
	deadline := time.Now().Add(wait)
	for {
		ok, err := r.client.SetNX(ctx, key, token, ttl).Result()
		if err != nil || ok {
			return ok, err
		}
		if time.Now().After(deadline) {
			return false, nil
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(40 * time.Millisecond):
		}
	}
}

// ReleaseDeviceLimitLock 释放自己持有的锁。
func (r *SessionRepository) ReleaseDeviceLimitLock(ctx context.Context, appID, userID int64, token string) error {
	return deviceLimitUnlockScript.Run(ctx, r.client, []string{r.deviceLimitLockKey(appID, userID)}, token).Err()
}

// SetDeviceLimitChallenge 暂存选择页期间的登录现场。
func (r *SessionRepository) SetDeviceLimitChallenge(ctx context.Context, ticket string, pending devicelimit.PendingLogin, ttl time.Duration) error {
	return r.setJSON(ctx, r.deviceLimitChallengeKey(ticket), pending, ttl)
}

// GetDeviceLimitChallenge 取回登录现场，不存在或已过期返回 nil。
func (r *SessionRepository) GetDeviceLimitChallenge(ctx context.Context, ticket string) (*devicelimit.PendingLogin, error) {
	var pending devicelimit.PendingLogin
	found, err := r.getJSON(ctx, r.deviceLimitChallengeKey(ticket), &pending)
	if err != nil || !found {
		return nil, err
	}
	return &pending, nil
}

// DeleteDeviceLimitChallenge 登录完成后作废凭证。
func (r *SessionRepository) DeleteDeviceLimitChallenge(ctx context.Context, ticket string) error {
	return r.client.Del(ctx, r.deviceLimitChallengeKey(ticket)).Err()
}

func (r *SessionRepository) deviceLimitLockKey(appID, userID int64) string {
	return fmt.Sprintf("%s:auth:device-limit:lock:%d:%d", r.keyPrefix, appID, userID)
}

func (r *SessionRepository) deviceLimitChallengeKey(ticket string) string {
	return fmt.Sprintf("%s:auth:device-limit:challenge:%s", r.keyPrefix, hashToken(ticket))
}
