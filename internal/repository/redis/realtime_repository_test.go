package redis

import (
	"context"
	"testing"
	"time"

	realtimedomain "aegis/internal/domain/realtime"

	"github.com/alicebob/miniredis/v2"
	redislib "github.com/redis/go-redis/v9"
)

func newTestRealtimeRepo(t *testing.T) (*RealtimeRepository, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redislib.NewClient(&redislib.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return NewRealtimeRepository(client, "test"), mr
}

func presence(id string, appID, userID int64, lastSeen time.Time) realtimedomain.PresenceConnection {
	return realtimedomain.PresenceConnection{ConnectionID: id, AppID: appID, UserID: userID, LastSeenAt: lastSeen, ConnectedAt: lastSeen}
}

func TestRealtimeCountsUsersAppsAndKeepsAdminsSeparate(t *testing.T) {
	repo, _ := newTestRealtimeRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()
	for _, c := range []realtimedomain.PresenceConnection{
		presence("a1", 7, 1, now),
		presence("a2", 7, 1, now), // 同一用户第二个设备
		presence("a3", 7, 2, now),
		presence("b1", 9, 1, now), // 另一个应用里的同号用户，按应用分别计数
		presence("adm", 0, 42, now),
	} {
		if err := repo.UpsertConnection(ctx, c, time.Minute); err != nil {
			t.Fatal(err)
		}
	}

	stats, err := repo.OnlineStats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if stats.OnlineUsers != 3 || stats.OnlineConnections != 4 || stats.OnlineApps != 2 {
		t.Fatalf("全站统计不符：%+v（管理员连接不应计入）", stats)
	}
	apps, err := repo.ActiveApps(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 2 || apps[0].AppID != 7 || apps[0].Users != 2 || apps[0].Connections != 3 {
		t.Fatalf("应用排行不符：%+v", apps)
	}
	admins, adminConns, err := repo.AdminCounts(ctx)
	if err != nil || admins != 1 || adminConns != 1 {
		t.Fatalf("管理员在线不符：%d %d %v", admins, adminConns, err)
	}
	presenceList, err := repo.ListAdminPresence(ctx)
	if err != nil || len(presenceList) != 1 || presenceList[0].AdminID != 42 {
		t.Fatalf("在线管理员列表不符：%+v %v", presenceList, err)
	}

	// 摘掉用户 1 的一条连接，仍在线；两条都摘掉才下线
	if err := repo.RemoveConnection(ctx, 7, 1, "a1"); err != nil {
		t.Fatal(err)
	}
	app, _ := repo.AppOnlineStats(ctx, 7)
	if app.OnlineUsers != 2 || app.OnlineConnections != 2 {
		t.Fatalf("摘一条连接后：%+v", app)
	}
	if err := repo.RemoveConnection(ctx, 7, 1, "a2"); err != nil {
		t.Fatal(err)
	}
	app, _ = repo.AppOnlineStats(ctx, 7)
	if app.OnlineUsers != 1 {
		t.Fatalf("用户全部连接断开后应下线：%+v", app)
	}
	if err := repo.RemoveConnection(ctx, 0, 42, "adm"); err != nil {
		t.Fatal(err)
	}
	if admins, _, _ := repo.AdminCounts(ctx); admins != 0 {
		t.Fatalf("管理员连接断开后应下线，仍有 %d", admins)
	}
}

// 用户分数取名下连接里最晚的过期时刻：一条快到期连接的心跳不能把整个人压到过期。
func TestRealtimeUserScoreUsesLatestConnection(t *testing.T) {
	repo, _ := newTestRealtimeRepo(t)
	ctx := context.Background()
	now := time.Now().UTC()
	if err := repo.UpsertConnection(ctx, presence("late", 7, 1, now), 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	// 另一条连接的最后心跳在 9 分钟前、TTL 10 分钟：单看它一分钟后就过期
	if err := repo.UpsertConnection(ctx, presence("early", 7, 1, now.Add(-9*time.Minute)), 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	score, err := repo.client.ZScore(ctx, repo.appUsersKey(7), "1").Result()
	if err != nil {
		t.Fatal(err)
	}
	if int64(score) != now.Add(10*time.Minute).Unix() {
		t.Fatalf("用户分数被较早的连接压低了：%v", score)
	}
}

func TestRealtimeCleanupReturnsExpiredConnections(t *testing.T) {
	repo, _ := newTestRealtimeRepo(t)
	ctx := context.Background()
	past := time.Now().UTC().Add(-10 * time.Minute)
	if err := repo.UpsertConnection(ctx, presence("dead", 7, 1, past), time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertConnection(ctx, presence("dead-admin", 0, 5, past), time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpsertConnection(ctx, presence("alive", 7, 2, time.Now().UTC()), time.Minute); err != nil {
		t.Fatal(err)
	}
	// 过期但还没清理：读路径按分数计数，已经不算在线
	if users, _, _ := repo.GlobalCounts(ctx); users != 1 {
		t.Fatalf("过期连接不应计入在线，实际 %d", users)
	}
	removed, err := repo.CleanupExpired(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 2 {
		t.Fatalf("应清理两条过期连接，实际 %+v", removed)
	}
	if n, _ := repo.client.ZCard(ctx, repo.globalUsersKey()).Result(); n != 1 {
		t.Fatalf("过期用户索引未清理，剩 %d", n)
	}
	if n, _ := repo.client.ZCard(ctx, repo.adminsKey()).Result(); n != 0 {
		t.Fatalf("过期管理员索引未清理，剩 %d", n)
	}
}

func TestRealtimeSeriesRoundTrip(t *testing.T) {
	repo, _ := newTestRealtimeRepo(t)
	ctx := context.Background()
	base := time.Date(2026, 10, 13, 23, 58, 0, 0, time.UTC) // 跨 UTC 日界
	for i := range 4 {
		at := base.Add(time.Duration(i) * time.Minute).Unix()
		point := realtimedomain.SeriesPoint{At: at, Users: int64(10 + i), Connections: int64(20 + i), Admins: 1}
		if err := repo.WriteSample(ctx, point, []AppCount{{AppID: 7, Users: int64(i), Connections: int64(i)}}, 7*24*time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	global, err := repo.ReadSeries(ctx, 0, base, base.Add(3*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(global) != 4 || global[0].Users != 10 || global[3].Users != 13 || global[3].Admins != 1 {
		t.Fatalf("全站曲线不符：%+v", global)
	}
	app, err := repo.ReadSeries(ctx, 7, base.Add(time.Minute), base.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(app) != 2 || app[0].Users != 1 || app[1].Users != 2 {
		t.Fatalf("应用曲线不符：%+v", app)
	}
}
