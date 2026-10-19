package vip

import (
	"sort"
	"time"
)

// 功能解锁：不当会员，也能单独拥有某个功能。
//
// ── 为什么不是一段「只带功能的会员期」 ──
//
// 会员段一旦存在，这个人就**是会员**：免广告、会员价、会员专属入口全部跟着打开。
// 积分兑换「在线音乐 7 天」的用户只该拿到在线音乐，所以功能解锁是与会员并列的第三条线，
// 只往 features 里加东西，不改 isVip / adFree / 会员来源。
//
// 判定口径与会员段一致：解锁的功能同样要与**启用中的**功能目录取交集 ——
// 停用一个功能标识即对会员与解锁同时生效，不会出现「会员用不了、兑换的人还能用」。
//
// 同一功能多次限时解锁时顺延（发放时就排好位置，见 grantFeatureUnlockTx），
// 永久解锁没有终点（EndsAt 为空）。

// 解锁来源。
const (
	UnlockSourcePointsStore = "points_store" // 积分兑换
	UnlockSourceAdminGrant  = "admin_grant"  // 管理员授予
	UnlockSourceInvite      = "invite"       // 邀请奖励
	UnlockSourceLeaderboard = "leaderboard"  // 排行榜奖励
)

// FeatureUnlock 一条功能解锁记录（账本行）。
type FeatureUnlock struct {
	ID        int64  `json:"id"`
	AppID     int64  `json:"appid,omitempty"`
	UserID    int64  `json:"userId,omitempty"`
	Tag       string `json:"tag"`
	Source    string `json:"source"`
	SourceRef string `json:"sourceRef,omitempty"`
	// DurationDays 0 表示永久
	DurationDays int        `json:"durationDays"`
	StartsAt     time.Time  `json:"startsAt"`
	EndsAt       *time.Time `json:"endsAt,omitempty"`
	RevokedAt    *time.Time `json:"revokedAt,omitempty"`
	RevokedBy    string     `json:"revokedBy,omitempty"`
	RevokeReason string     `json:"revokeReason,omitempty"`
	Operator     string     `json:"operator,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
}

// Permanent 永久解锁。
func (u FeatureUnlock) Permanent() bool { return u.EndsAt == nil }

// LiveAt 这条解锁在 now 时刻是否仍贡献功能。
//
// 与会员段同理，**不要求** StartsAt <= now：第二次兑换的七天排在第一次之后，
// 但这个功能此刻本来就开着，不存在「还没轮到」的问题。
func (u FeatureUnlock) LiveAt(now time.Time) bool {
	if u.RevokedAt != nil {
		return false
	}
	return u.EndsAt == nil || u.EndsAt.After(now)
}

// UnlockView 某个功能的解锁状态（同一功能的多条记录合成一条）。
type UnlockView struct {
	Tag string `json:"tag"`
	// Permanent 永久解锁：ExpireAt 为空
	Permanent bool       `json:"permanent"`
	ExpireAt  *time.Time `json:"expireAt,omitempty"`
}

// resolveUnlocks 仍生效、且在启用中功能目录里的解锁，按功能合并。
func resolveUnlocks(unlocks []FeatureUnlock, catalog []string, now time.Time) []UnlockView {
	if len(unlocks) == 0 || len(catalog) == 0 {
		return []UnlockView{}
	}
	enabled := make(map[string]struct{}, len(catalog))
	for _, tag := range NormalizeFeatureTags(catalog) {
		enabled[tag] = struct{}{}
	}
	merged := map[string]*UnlockView{}
	for _, unlock := range unlocks {
		if !unlock.LiveAt(now) {
			continue
		}
		tags := NormalizeFeatureTags([]string{unlock.Tag})
		if len(tags) == 0 {
			continue
		}
		tag := tags[0]
		if _, ok := enabled[tag]; !ok {
			continue
		}
		view, ok := merged[tag]
		if !ok {
			view = &UnlockView{Tag: tag}
			merged[tag] = view
		}
		if unlock.Permanent() {
			view.Permanent = true
			continue
		}
		if view.ExpireAt == nil || unlock.EndsAt.After(*view.ExpireAt) {
			end := unlock.EndsAt.UTC()
			view.ExpireAt = &end
		}
	}
	out := make([]UnlockView, 0, len(merged))
	for _, view := range merged {
		if view.Permanent {
			view.ExpireAt = nil
		}
		out = append(out, *view)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Tag < out[j].Tag })
	return out
}

// mergeFeatureTags 两组功能标识的并集（已规整、有序）。
func mergeFeatureTags(base []string, extra []UnlockView) []string {
	if len(extra) == 0 {
		return base
	}
	all := make([]string, 0, len(base)+len(extra))
	all = append(all, base...)
	for _, view := range extra {
		all = append(all, view.Tag)
	}
	return NormalizeFeatureTags(all)
}

// PermanentUnlockTags 永久解锁了哪些功能（不与目录取交集：判「已拥有」用，
// 暂时停用的功能他有就是有，不该因为停用就允许再兑换一次）。
func PermanentUnlockTags(unlocks []FeatureUnlock, now time.Time) []string {
	out := make([]string, 0, len(unlocks))
	for _, unlock := range unlocks {
		if unlock.LiveAt(now) && unlock.Permanent() {
			out = append(out, unlock.Tag)
		}
	}
	return NormalizeFeatureTags(out)
}
