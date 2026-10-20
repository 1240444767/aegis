package service

import (
	"context"
	"sync"

	pgrepo "aegis/internal/repository/postgres"
)

// 对外用户 ID 的换算（docs/user-public-id.md）。
//
// 用户与第三方能看到的地方只给 users.public_id；服务端内部、外键、管理端照旧用主键。
// 两者是终生一一对应的（号不可改、注销后不复用），所以换算结果可以只进不出地缓存：
// 不存在「缓存里的号过期了」这回事，唯一要防的是内存无限增长，满了整张表清掉重来。

type userPublicIDStore interface {
	GetUserPublicIDs(ctx context.Context, userIDs []int64) (map[int64]int64, error)
	FindUserIDByPublicID(ctx context.Context, appID, publicID int64) (int64, error)
}

const userPublicIDCacheLimit = 200_000

// UserPublicIDs 主键 ↔ 对外 ID。零值不可用，用 NewUserPublicIDs 构造；nil 接收者安全（全部返回 0）。
type UserPublicIDs struct {
	store userPublicIDStore

	mu       sync.RWMutex
	toPublic map[int64]int64
}

func NewUserPublicIDs(store userPublicIDStore) *UserPublicIDs {
	return &UserPublicIDs{store: store, toPublic: make(map[int64]int64)}
}

// newUserPublicIDsFor 各服务构造时的默认换算（各自一份缓存）；bootstrap 随后换成全进程共用的那份。
// pg 为 nil 时（单测）得到一个永远换不出号的换算器，而不是带着 nil 指针的接口。
func newUserPublicIDsFor(pg *pgrepo.Repository) *UserPublicIDs {
	if pg == nil {
		return NewUserPublicIDs(nil)
	}
	return NewUserPublicIDs(pg)
}

// Of 取单个用户的对外 ID；用户不存在或查询失败返回 0。
func (p *UserPublicIDs) Of(ctx context.Context, userID int64) int64 {
	if userID <= 0 {
		return 0
	}
	return p.Map(ctx, []int64{userID})[userID]
}

// Map 批量取对外 ID。查询失败时返回已缓存的部分，调用方对缺的那几个按 0 处理（不下发）。
func (p *UserPublicIDs) Map(ctx context.Context, userIDs []int64) map[int64]int64 {
	out := make(map[int64]int64, len(userIDs))
	if p == nil || len(userIDs) == 0 {
		return out
	}
	missing := make([]int64, 0, len(userIDs))
	p.mu.RLock()
	for _, id := range userIDs {
		if id <= 0 {
			continue
		}
		if publicID, ok := p.toPublic[id]; ok {
			out[id] = publicID
		} else {
			missing = append(missing, id)
		}
	}
	p.mu.RUnlock()
	if len(missing) == 0 || p.store == nil {
		return out
	}
	found, err := p.store.GetUserPublicIDs(ctx, dedupeInt64(missing))
	if err != nil {
		return out
	}
	p.mu.Lock()
	if len(p.toPublic)+len(found) > userPublicIDCacheLimit {
		p.toPublic = make(map[int64]int64, len(found))
	}
	for id, publicID := range found {
		p.toPublic[id] = publicID
		out[id] = publicID
	}
	p.mu.Unlock()
	return out
}

// Resolve 把客户端传来的对外 ID 换回应用内的主键；不是本应用的用户返回 0。
func (p *UserPublicIDs) Resolve(ctx context.Context, appID, publicID int64) (int64, error) {
	if p == nil || p.store == nil || publicID <= 0 {
		return 0, nil
	}
	return p.store.FindUserIDByPublicID(ctx, appID, publicID)
}
