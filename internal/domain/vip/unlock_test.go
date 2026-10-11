package vip

import (
	"testing"
	"time"
)

func unlock(id int64, tag string, end *time.Time) FeatureUnlock {
	return FeatureUnlock{ID: id, Tag: tag, Source: UnlockSourcePointsStore, EndsAt: end}
}

// 功能解锁不让人成为会员，只往 features 里加东西。
func TestUnlockGrantsFeatureWithoutMembership(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	got := Evaluate(EvalInput{
		Unlocks:        []FeatureUnlock{unlock(1, "online_music", ptr(now.Add(72*time.Hour)))},
		FeatureCatalog: []string{"online_music", "doubao_analyze"},
	}, now)
	if got.IsVIP || got.AdFree || got.Source != SourceNone {
		t.Fatalf("功能解锁不该让人成为会员：%+v", got)
	}
	if !got.HasFeature("online_music") || got.HasFeature("doubao_analyze") {
		t.Errorf("features 应当恰好包含解锁的功能，实际 %v", got.Features)
	}
	if len(got.Unlocks) != 1 || got.Unlocks[0].Permanent || got.Unlocks[0].ExpireAt == nil ||
		!got.Unlocks[0].ExpireAt.Equal(now.Add(72*time.Hour)) {
		t.Errorf("unlocks 应当说清到期时间，实际 %+v", got.Unlocks)
	}
	view := got.View()
	if len(view.Unlocks) != 1 || len(view.Features) != 1 {
		t.Errorf("服务端校验的投影要带上解锁：%+v", view)
	}
}

// 会员的功能与解锁的功能取并集；同一功能的多段解锁合成一条，取最晚的终点，永久的盖过限时的。
func TestUnlocksMergeWithMembershipAndEachOther(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	got := Evaluate(EvalInput{
		ExpireAt: ptr(now.Add(24 * time.Hour)),
		Segments: []Segment{segment(1, ChannelWallet, "基础版", now, now.Add(24*time.Hour), "export")},
		Unlocks: []FeatureUnlock{
			unlock(1, "online_music", ptr(now.Add(24*time.Hour))),
			unlock(2, "online_music", ptr(now.Add(96*time.Hour))),
			unlock(3, "doubao_analyze", ptr(now.Add(time.Hour))),
			unlock(4, "doubao_analyze", nil),
		},
		FeatureCatalog: []string{"export", "online_music", "doubao_analyze"},
	}, now)
	if !got.IsVIP {
		t.Fatal("应当仍是会员")
	}
	want := []string{"doubao_analyze", "export", "online_music"}
	if len(got.Features) != len(want) {
		t.Fatalf("features 期望 %v，实际 %v", want, got.Features)
	}
	for i := range want {
		if got.Features[i] != want[i] {
			t.Fatalf("features 期望 %v，实际 %v", want, got.Features)
		}
	}
	if len(got.Unlocks) != 2 {
		t.Fatalf("同一功能应合成一条，实际 %+v", got.Unlocks)
	}
	if got.Unlocks[0].Tag != "doubao_analyze" || !got.Unlocks[0].Permanent || got.Unlocks[0].ExpireAt != nil {
		t.Errorf("永久解锁应盖过限时解锁：%+v", got.Unlocks[0])
	}
	if got.Unlocks[1].ExpireAt == nil || !got.Unlocks[1].ExpireAt.Equal(now.Add(96*time.Hour)) {
		t.Errorf("限时解锁取最晚的终点：%+v", got.Unlocks[1])
	}
}

// 解锁同样只在启用中的功能目录里取：停用一个功能对会员与解锁同时生效。
// 已结束、已收回的解锁不算数。
func TestUnlocksRespectCatalogAndWindow(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	revoked := unlock(3, "online_music", nil)
	revoked.RevokedAt = ptr(now.Add(-time.Hour))
	got := Evaluate(EvalInput{
		Unlocks: []FeatureUnlock{
			unlock(1, "retired", nil),
			unlock(2, "online_music", ptr(now.Add(-time.Minute))),
			revoked,
		},
		FeatureCatalog: []string{"online_music"},
	}, now)
	if len(got.Features) != 0 || len(got.Unlocks) != 0 {
		t.Errorf("停用的、已结束的、已收回的解锁都不该生效：features=%v unlocks=%+v", got.Features, got.Unlocks)
	}
	if got.HasFeature("online_music") {
		t.Error("已结束的解锁不该命中")
	}
}

func TestPermanentUnlockTags(t *testing.T) {
	now := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	tags := PermanentUnlockTags([]FeatureUnlock{
		unlock(1, "online_music", nil),
		unlock(2, "doubao_analyze", ptr(now.Add(time.Hour))),
	}, now)
	if len(tags) != 1 || tags[0] != "online_music" {
		t.Errorf("只有永久解锁才算「已拥有」，实际 %v", tags)
	}
}
