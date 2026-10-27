package redis

import (
	"context"
	"fmt"
	"time"
)

// HitAdminMCPKeyRate MCP 密钥的每分钟调用计数（固定窗口），返回本窗口内的第几次。
func (r *SessionRepository) HitAdminMCPKeyRate(ctx context.Context, keyID int64, now time.Time) (int64, error) {
	key := fmt.Sprintf("%s:admin_mcp_key:rate:%d:%d", r.keyPrefix, keyID, now.Unix()/60)
	n, err := r.client.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	if n == 1 {
		_ = r.client.Expire(ctx, key, 2*time.Minute).Err()
	}
	return n, nil
}
