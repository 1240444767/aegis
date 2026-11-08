package service

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	redislib "github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	authdomain "aegis/internal/domain/auth"
	notificationdomain "aegis/internal/domain/notification"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
)

// 用户消息中心的端到端集成测试：真实 Postgres（全部迁移两遍）+ miniredis + 假的实时发布器。
// 「迁移回填分类（重跑不覆盖显式分类）→ Deliver 校验（分类、动作、配图、过期时间）→ 返回完整 Item →
// notification.created 事件内容与 silent → 偏好（安全类不可关、未知分类）→ 列表按分类 / 状态过滤、
// 过期排除、缓存键含分类 → 未读按分类计数 → summary → 详情 → 标已读 / 标未读 → 批量已读 →
// 按分类全部已读 → 批量删除 → 按分类清空 → 跨用户越权按不存在处理 → 管理员群发与列表的分类」。
//
// AEGIS_TEST_PG_DSN=postgres://aegis:<密码>@127.0.0.1:15432/aegis_notification_test?sslmode=disable \
//
//	go test ./internal/service -run TestNotificationCenterIntegration
//
// 注意：openTestDatabase 会 DROP SCHEMA public，DSN 绝不能指向开发库。
func TestNotificationCenterIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	mr := miniredis.RunT(t)
	rdb := redislib.NewClient(&redislib.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	sessions := redisrepo.NewSessionRepository(rdb, "t")
	rt := &fakeRealtime{}
	svc := NewNotificationService(zap.NewNop(), pg, sessions, rt)

	const appID = int64(33000)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'notification-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	alice := insertTestUser(t, ctx, pool, appID, "alice", "0")
	bob := insertTestUser(t, ctx, pool, appID, "bob", "0")
	carol := insertTestUser(t, ctx, pool, appID, "carol", "0")
	aliceSession := &authdomain.Session{AppID: appID, UserID: alice}
	bobSession := &authdomain.Session{AppID: appID, UserID: bob}

	expectCode := func(name string, err error, want int) {
		t.Helper()
		if errCode(err) != want {
			t.Fatalf("%s：期望错误码 %d，得到 %v", name, want, err)
		}
	}
	deliver := func(in notificationdomain.DeliverInput) *notificationdomain.Item {
		t.Helper()
		if in.AppID == 0 {
			in.AppID = appID
		}
		item, err := svc.Deliver(ctx, in)
		if err != nil {
			t.Fatalf("投递 %s 失败：%v", in.Title, err)
		}
		return item
	}
	drainEvents := func() []fakeRealtimeEvent {
		svc.waitPendingEvents()
		rt.mu.Lock()
		defer rt.mu.Unlock()
		events := rt.events
		rt.events = nil
		return events
	}
	eventsOf := func(events []fakeRealtimeEvent, userID int64, eventType string) []fakeRealtimeEvent {
		out := []fakeRealtimeEvent{}
		for _, event := range events {
			if event.UserID == userID && event.Type == eventType {
				out = append(out, event)
			}
		}
		return out
	}
	list := func(session *authdomain.Session, status, category string) *notificationdomain.ListResponse {
		t.Helper()
		result, err := svc.List(ctx, session, notificationdomain.UserListQuery{Status: status, Category: category, Page: 1, Limit: 50})
		if err != nil {
			t.Fatalf("列表（%s / %s）失败：%v", status, category, err)
		}
		return result
	}
	ids := func(items []notificationdomain.Item) []int64 {
		out := []int64{}
		for _, item := range items {
			out = append(out, item.ID)
		}
		return out
	}
	sameCounts := func(name string, got map[string]int64, want map[string]int64) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("%s：分类未读 %v，期望 %v", name, got, want)
		}
		for key, n := range want {
			if got[key] != n {
				t.Fatalf("%s：分类未读 %v，期望 %v", name, got, want)
			}
		}
	}

	// ── 迁移回填：老数据（category 为空）按 type 与 metadata 归类；重跑不覆盖显式分类 ──
	if _, err := pool.Exec(ctx, `ALTER TABLE notifications ALTER COLUMN category DROP NOT NULL`); err != nil {
		t.Fatal(err)
	}
	legacy := []struct{ typ, metadata, want string }{
		{"vip", `{}`, notificationdomain.CategoryMembership},
		{"campaign", `{}`, notificationdomain.CategoryAnnouncement},
		{"security", `{}`, notificationdomain.CategorySecurity},
		{"security", `{"module":"account_deletion"}`, notificationdomain.CategoryMembership},
		{"system", `{"scene":"auto_sign"}`, notificationdomain.CategoryReward},
		{"Leaderboard", `{}`, notificationdomain.CategoryReward},
		{"feedback", `{}`, notificationdomain.CategoryTicket},
		{"something_else", `{}`, notificationdomain.CategorySystem},
	}
	legacyIDs := make([]int64, len(legacy))
	for i, row := range legacy {
		if err := pool.QueryRow(ctx, `INSERT INTO notifications (appid, user_id, type, category, title, content, metadata)
VALUES ($1, $2, $3, NULL, 'legacy', 'legacy', $4::jsonb) RETURNING id`, appID, carol, row.typ, row.metadata).Scan(&legacyIDs[i]); err != nil {
			t.Fatal(err)
		}
	}
	var explicitID int64
	if err := pool.QueryRow(ctx, `INSERT INTO notifications (appid, user_id, type, category, title, content)
VALUES ($1, $2, 'security', 'reward', 'explicit', 'explicit') RETURNING id`, appID, carol).Scan(&explicitID); err != nil {
		t.Fatal(err)
	}
	upSQL, err := os.ReadFile("../../migrations/postgres/000108_notification_center.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 2; round++ {
		if _, err := pool.Exec(ctx, string(upSQL)); err != nil {
			t.Fatalf("重跑 000108 失败：%v", err)
		}
	}
	for i, row := range legacy {
		var category string
		if err := pool.QueryRow(ctx, `SELECT category FROM notifications WHERE id = $1`, legacyIDs[i]).Scan(&category); err != nil {
			t.Fatal(err)
		}
		if category != row.want {
			t.Errorf("回填：type=%s metadata=%s 得到 %s，期望 %s", row.typ, row.metadata, category, row.want)
		}
		// 回填与运行时的 CategoryOf 一致（metadata 识别的两种历史写法除外）
		if row.metadata == `{}` && notificationdomain.CategoryOf(row.typ) != row.want {
			t.Errorf("回填与 CategoryOf(%s) 不一致", row.typ)
		}
	}
	var explicitCategory string
	var nullable string
	if err := pool.QueryRow(ctx, `SELECT category FROM notifications WHERE id = $1`, explicitID).Scan(&explicitCategory); err != nil {
		t.Fatal(err)
	}
	if explicitCategory != notificationdomain.CategoryReward {
		t.Fatalf("重跑迁移覆盖了显式分类：%s", explicitCategory)
	}
	if err := pool.QueryRow(ctx, `SELECT is_nullable FROM information_schema.columns WHERE table_name = 'notifications' AND column_name = 'category'`).Scan(&nullable); err != nil || nullable != "NO" {
		t.Fatalf("category 应恢复 NOT NULL：%s %v", nullable, err)
	}

	// ── Deliver 校验 ──
	_, err = svc.Deliver(ctx, notificationdomain.DeliverInput{AppID: appID, UserID: alice, Title: "x", Category: "nope"})
	expectCode("未知分类", err, errCodeNotificationCategoryInvalid)
	_, err = svc.Deliver(ctx, notificationdomain.DeliverInput{AppID: appID, UserID: alice, Title: "x",
		Action: &notificationdomain.Action{Type: "route", Target: "settings"}})
	expectCode("未知路由键", err, errCodeNotificationActionInvalid)
	_, err = svc.Deliver(ctx, notificationdomain.DeliverInput{AppID: appID, UserID: alice, Title: "x",
		Action: &notificationdomain.Action{Type: "url", Target: "http://example.com"}})
	expectCode("非 https 链接", err, errCodeNotificationActionInvalid)
	_, err = svc.Deliver(ctx, notificationdomain.DeliverInput{AppID: appID, UserID: alice, Title: "x", Image: "http://example.com/a.png"})
	expectCode("非 https 配图", err, 40010)
	past := time.Now().Add(-time.Minute)
	_, err = svc.Deliver(ctx, notificationdomain.DeliverInput{AppID: appID, UserID: alice, Title: "x", ExpiresAt: &past})
	expectCode("过期时间在过去", err, 40010)
	_, err = svc.Deliver(ctx, notificationdomain.DeliverInput{AppID: appID, UserID: alice, Title: "", Content: "x"})
	expectCode("标题为空", err, 40010)
	_, err = svc.Deliver(ctx, notificationdomain.DeliverInput{AppID: appID, UserID: alice, Title: "x", Level: "fatal"})
	expectCode("未知级别", err, 40010)
	drainEvents()

	// ── 投递：返回完整 Item ──
	rank := deliver(notificationdomain.DeliverInput{UserID: alice, Type: "leaderboard", Title: "排行榜奖励已到账",
		Content: "你在本周积分周榜获得第 3 名。", Level: "success", Metadata: map[string]any{"rank": 3},
		Action: notificationdomain.RouteAction(notificationdomain.RouteLeaderboard, "查看排行榜")})
	if rank.ID == 0 || rank.Category != notificationdomain.CategoryReward || rank.CategoryLabel != "积分与奖励" ||
		rank.Status != "unread" || rank.Read || rank.ReadAt != nil || rank.Level != "success" || rank.Type != "leaderboard" {
		t.Fatalf("返回的 Item 不完整：%+v", rank)
	}
	if rank.Action == nil || rank.Action.Type != "route" || rank.Action.Target != "leaderboard" || rank.Action.Label != "查看排行榜" {
		t.Fatalf("动作没落库：%+v", rank.Action)
	}
	if rank.UserID == nil || *rank.UserID != alice || rank.Metadata["rank"] != float64(3) {
		t.Fatalf("归属或 metadata 不对：%+v", rank)
	}
	secure := deliver(notificationdomain.DeliverInput{UserID: alice, Type: "security", Title: "异常登录提醒", Level: "critical"})
	if secure.Category != notificationdomain.CategorySecurity || secure.Content != "异常登录提醒" || secure.Level != "error" {
		t.Fatalf("分类、正文兜底或级别归一不对：%+v", secure)
	}
	future := time.Now().Add(24 * time.Hour)
	promo := deliver(notificationdomain.DeliverInput{UserID: alice, Type: "system", Category: notificationdomain.CategoryAnnouncement,
		Title: "国庆活动", Content: "活动开始了", Image: "https://cdn.example.com/a.png", ExpiresAt: &future,
		Action: &notificationdomain.Action{Type: "url", Target: "https://example.com/event"}})
	if promo.Category != notificationdomain.CategoryAnnouncement || promo.Image == nil || *promo.Image != "https://cdn.example.com/a.png" ||
		promo.ExpiresAt == nil || promo.Action == nil || promo.Action.Type != "url" {
		t.Fatalf("显式分类、配图、过期时间或链接动作没落库：%+v", promo)
	}
	stale := deliver(notificationdomain.DeliverInput{UserID: alice, Type: "system", Title: "即将过期"})
	bobItem := deliver(notificationdomain.DeliverInput{UserID: bob, Type: "invite", Title: "邀请奖励已到账"})

	// ── 实时事件：notification.created 带完整 Item、计数与 silent，旧事件照发 ──
	events := drainEvents()
	created := eventsOf(events, alice, notificationdomain.EventCreated)
	if len(created) != 4 {
		t.Fatalf("alice 应收到 4 条 created 事件，实际 %d：%+v", len(created), events)
	}
	if len(eventsOf(events, alice, notificationdomain.EventStateChanged)) != 4 {
		t.Fatal("旧事件 notification.state.changed 应照发")
	}
	if len(eventsOf(events, bob, notificationdomain.EventCreated)) != 1 {
		t.Fatal("bob 应收到自己的 created 事件")
	}
	var rankEvent *fakeRealtimeEvent
	for i := range created {
		if item, ok := created[i].Data["notification"].(notificationdomain.Item); ok && item.ID == rank.ID {
			rankEvent = &created[i]
		}
	}
	if rankEvent == nil {
		t.Fatalf("created 事件里找不到排行榜那条：%+v", created)
	}
	if item := rankEvent.Data["notification"].(notificationdomain.Item); item.Action == nil || item.Category != notificationdomain.CategoryReward {
		t.Fatalf("created 事件的 Item 不完整：%+v", item)
	}
	if silent, ok := rankEvent.Data["silent"].(bool); !ok || silent {
		t.Fatalf("没关提醒时 silent 应为 false：%v", rankEvent.Data["silent"])
	}
	if _, ok := rankEvent.Data["unread"].(int64); !ok {
		t.Fatalf("created 事件应带 unread：%+v", rankEvent.Data)
	}
	if _, ok := rankEvent.Data["categoryUnread"].(map[string]int64); !ok {
		t.Fatalf("created 事件应带 categoryUnread：%+v", rankEvent.Data)
	}

	// ── 偏好：安全类不可关、未知分类、关闭后 silent ──
	_, err = svc.UpdatePreferences(ctx, aliceSession, map[string]bool{notificationdomain.CategorySecurity: false})
	expectCode("关闭账号安全", err, errCodeNotificationPushLocked)
	_, err = svc.UpdatePreferences(ctx, aliceSession, map[string]bool{"nope": false})
	expectCode("未知分类偏好", err, errCodeNotificationCategoryInvalid)
	prefs, err := svc.UpdatePreferences(ctx, aliceSession, map[string]bool{notificationdomain.CategoryReward: false, notificationdomain.CategorySecurity: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(prefs.Categories) != len(notificationdomain.Categories()) {
		t.Fatalf("偏好应列出全部分类：%+v", prefs.Categories)
	}
	for _, item := range prefs.Categories {
		switch item.Key {
		case notificationdomain.CategoryReward:
			if item.Push || !item.Mutable {
				t.Fatalf("积分与奖励应已关闭：%+v", item)
			}
		case notificationdomain.CategorySecurity:
			if !item.Push || item.Mutable {
				t.Fatalf("账号安全应恒开且不可改：%+v", item)
			}
		default:
			if !item.Push {
				t.Fatalf("没写过的分类应默认开启：%+v", item)
			}
		}
	}
	if again, err := svc.Preferences(ctx, aliceSession); err != nil || again.Categories[4].Key != notificationdomain.CategoryReward || again.Categories[4].Push {
		t.Fatalf("偏好没持久化：%+v %v", again, err)
	}
	muted := deliver(notificationdomain.DeliverInput{UserID: alice, Type: "points_store", Title: "兑换已完成"})
	loud := deliver(notificationdomain.DeliverInput{UserID: alice, Type: "security", Title: "密码已重置"})
	events = drainEvents()
	for _, event := range eventsOf(events, alice, notificationdomain.EventCreated) {
		item := event.Data["notification"].(notificationdomain.Item)
		silent := event.Data["silent"].(bool)
		if item.ID == muted.ID && !silent {
			t.Fatal("关闭提醒的分类，created 事件应 silent=true")
		}
		if item.ID == loud.ID && silent {
			t.Fatal("账号安全类永远提醒")
		}
	}

	// 一条改成已过期：不出现、不计数、读写按不存在
	if _, err := pool.Exec(ctx, `UPDATE notifications SET expires_at = NOW() - INTERVAL '1 second' WHERE id = $1`, stale.ID); err != nil {
		t.Fatal(err)
	}
	// 改库绕过了服务，手动清一下缓存（真实场景里缓存寿命不超过最早一条的过期时间）
	svc.invalidateCaches(ctx, appID, alice)

	// ── 列表：分类 / 状态过滤、过期排除、未读按分类计数 ──
	all := list(aliceSession, "", "")
	if all.Total != 5 {
		t.Fatalf("alice 应有 5 条未过期的消息，实际 %d：%v", all.Total, ids(all.Items))
	}
	for _, item := range all.Items {
		if item.ID == stale.ID {
			t.Fatal("过期消息不应出现在列表里")
		}
		if item.ID == bobItem.ID {
			t.Fatal("列表里出现了别人的消息")
		}
	}
	if all.Items[0].ID != loud.ID {
		t.Fatalf("列表应按时间倒序：%v", ids(all.Items))
	}
	if all.Unread != 5 {
		t.Fatalf("未读应为 5（过期的不计），实际 %d", all.Unread)
	}
	sameCounts("列表计数", all.CategoryUnread, map[string]int64{
		notificationdomain.CategoryReward: 2, notificationdomain.CategorySecurity: 2, notificationdomain.CategoryAnnouncement: 1,
	})
	rewards := list(aliceSession, "", notificationdomain.CategoryReward)
	if rewards.Total != 2 || rewards.Items[0].ID != muted.ID || rewards.Items[1].ID != rank.ID {
		t.Fatalf("按分类过滤不对：%v", ids(rewards.Items))
	}
	// 缓存键含分类：换个分类不会读到上一个分类的缓存
	security := list(aliceSession, "", notificationdomain.CategorySecurity)
	if security.Total != 2 || security.Items[0].Category != notificationdomain.CategorySecurity {
		t.Fatalf("换分类读到了别的缓存：%v", ids(security.Items))
	}
	if _, err := svc.List(ctx, aliceSession, notificationdomain.UserListQuery{Category: "nope"}); errCode(err) != errCodeNotificationCategoryInvalid {
		t.Fatalf("未知分类筛选应报 42231：%v", err)
	}
	if _, err := svc.List(ctx, aliceSession, notificationdomain.UserListQuery{Status: "delivered"}); errCode(err) != 40010 {
		t.Fatalf("未知状态应报 40010：%v", err)
	}
	unreadCount, err := svc.UnreadCount(ctx, aliceSession)
	if err != nil || unreadCount.Unread != 5 || unreadCount.Categories[notificationdomain.CategoryReward] != 2 {
		t.Fatalf("unread-count 不对：%+v %v", unreadCount, err)
	}

	// ── summary：全部分类都列出 ──
	summary, err := svc.Summary(ctx, aliceSession)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Unread != 5 || len(summary.Categories) != len(notificationdomain.Categories()) {
		t.Fatalf("summary 不对：%+v", summary)
	}
	for _, row := range summary.Categories {
		switch row.Key {
		case notificationdomain.CategoryReward:
			if row.Total != 2 || row.Unread != 2 || row.Latest == nil || row.Latest.ID != muted.ID || row.Label != "积分与奖励" {
				t.Fatalf("summary 积分与奖励不对：%+v", row)
			}
		case notificationdomain.CategorySystem:
			if row.Total != 0 || row.Latest != nil {
				t.Fatalf("system 只有一条过期消息，应 total=0、latest=null：%+v", row)
			}
		case notificationdomain.CategoryTicket:
			if row.Total != 0 || row.Unread != 0 || row.Latest != nil {
				t.Fatalf("没有消息的分类应 total=0：%+v", row)
			}
		}
	}

	// ── 详情与越权 ──
	if item, err := svc.Get(ctx, aliceSession, rank.ID); err != nil || item.ID != rank.ID || item.Action == nil {
		t.Fatalf("详情不对：%+v %v", item, err)
	}
	_, err = svc.Get(ctx, bobSession, rank.ID)
	expectCode("读别人的消息", err, errCodeNotificationNotFound)
	_, err = svc.Get(ctx, aliceSession, stale.ID)
	expectCode("读过期消息", err, errCodeNotificationNotFound)
	_, err = svc.MarkRead(ctx, bobSession, rank.ID)
	expectCode("标别人的消息已读", err, errCodeNotificationNotFound)
	_, err = svc.MarkRead(ctx, aliceSession, stale.ID)
	expectCode("标过期消息已读", err, errCodeNotificationNotFound)
	_, err = svc.Delete(ctx, bobSession, rank.ID)
	expectCode("删别人的消息", err, errCodeNotificationNotFound)
	if item, _ := svc.Get(ctx, aliceSession, rank.ID); item == nil {
		t.Fatal("越权删除不应生效")
	}
	drainEvents()

	// ── 标已读 / 标未读 ──
	read, err := svc.MarkRead(ctx, aliceSession, rank.ID)
	if err != nil || !read.Read || read.Unread != 4 || read.CategoryUnread[notificationdomain.CategoryReward] != 1 {
		t.Fatalf("标已读：%+v %v", read, err)
	}
	if item, _ := svc.Get(ctx, aliceSession, rank.ID); item.Status != "read" || !item.Read || item.ReadAt == nil {
		t.Fatalf("标已读后状态不对：%+v", item)
	}
	unread, err := svc.MarkUnread(ctx, aliceSession, rank.ID)
	if err != nil || unread.Read || unread.Unread != 5 || unread.CategoryUnread[notificationdomain.CategoryReward] != 2 {
		t.Fatalf("标未读：%+v %v", unread, err)
	}
	if item, _ := svc.Get(ctx, aliceSession, rank.ID); item.Status != "unread" || item.Read || item.ReadAt != nil {
		t.Fatalf("标未读后状态不对：%+v", item)
	}
	events = drainEvents()
	readEvents := eventsOf(events, alice, notificationdomain.EventRead)
	if len(readEvents) != 2 || readEvents[0].Data["read"] != true || readEvents[1].Data["read"] != false {
		t.Fatalf("应有一条 read=true 与一条 read=false 的事件：%+v", readEvents)
	}
	if got := readEvents[1].Data["ids"].([]int64); len(got) != 1 || got[0] != rank.ID || readEvents[1].Data["unread"] != int64(5) {
		t.Fatalf("标未读事件内容不对：%+v", readEvents[1].Data)
	}

	// ── 批量已读：别人的不算 ──
	batch, err := svc.MarkReadBatch(ctx, aliceSession, []int64{rank.ID, secure.ID, bobItem.ID, rank.ID})
	if err != nil || batch.Requested != 3 || batch.Updated != 2 || batch.Unread != 3 {
		t.Fatalf("批量已读：%+v %v", batch, err)
	}
	if item, _ := svc.Get(ctx, bobSession, bobItem.ID); item.Read {
		t.Fatal("批量已读改到了别人的消息")
	}
	tooMany := make([]int64, notificationdomain.MaxBatchIDs+1)
	for i := range tooMany {
		tooMany[i] = int64(i + 1)
	}
	_, err = svc.MarkReadBatch(ctx, aliceSession, tooMany)
	expectCode("批量超过 200 条", err, 40010)
	drainEvents()

	// ── 按分类全部已读 ──
	readAll, err := svc.MarkAllRead(ctx, aliceSession, notificationdomain.CategoryReward)
	if err != nil || readAll.Updated != 1 || readAll.Category != notificationdomain.CategoryReward || readAll.Unread != 2 {
		t.Fatalf("按分类全部已读：%+v %v", readAll, err)
	}
	sameCounts("按分类已读后", readAll.CategoryUnread, map[string]int64{
		notificationdomain.CategorySecurity: 1, notificationdomain.CategoryAnnouncement: 1,
	})
	events = drainEvents()
	if readEvents = eventsOf(events, alice, notificationdomain.EventRead); len(readEvents) != 1 ||
		readEvents[0].Data["all"] != true || readEvents[0].Data["category"] != notificationdomain.CategoryReward {
		t.Fatalf("按分类已读事件不对：%+v", readEvents)
	}

	// ── 批量删除：别人的、过期的不算 ──
	deleted, err := svc.DeleteBatch(ctx, aliceSession, []int64{muted.ID, bobItem.ID, stale.ID})
	if err != nil || deleted.Deleted != 1 || len(deleted.IDs) != 1 || deleted.IDs[0] != muted.ID {
		t.Fatalf("批量删除：%+v %v", deleted, err)
	}
	if item, err := svc.Get(ctx, bobSession, bobItem.ID); err != nil || item == nil {
		t.Fatalf("批量删除删到了别人的消息：%v", err)
	}
	_, err = svc.Delete(ctx, aliceSession, muted.ID)
	expectCode("删已删除的消息", err, errCodeNotificationNotFound)

	// ── 按分类清空 ──
	cleared, err := svc.ClearFiltered(ctx, aliceSession, "all", notificationdomain.CategoryAnnouncement, "", "")
	if err != nil || cleared.Deleted != 1 || cleared.Category != notificationdomain.CategoryAnnouncement || cleared.Unread != 1 {
		t.Fatalf("按分类清空：%+v %v", cleared, err)
	}
	sameCounts("清空公告后", cleared.CategoryUnread, map[string]int64{notificationdomain.CategorySecurity: 1})
	readCleared, err := svc.ClearFiltered(ctx, aliceSession, "read", "", "", "")
	if err != nil || readCleared.Deleted != 2 { // rank（已读）与 secure（已读）
		t.Fatalf("清空已读：%+v %v", readCleared, err)
	}
	if left := list(aliceSession, "", ""); left.Total != 1 || left.Items[0].ID != loud.ID {
		t.Fatalf("清空后应只剩一条未读的安全消息：%v", ids(left.Items))
	}
	events = drainEvents()
	deletedEvents := eventsOf(events, alice, notificationdomain.EventDeleted)
	if len(deletedEvents) != 3 || deletedEvents[0].Data["cleared"] != false ||
		deletedEvents[1].Data["cleared"] != true || deletedEvents[1].Data["category"] != notificationdomain.CategoryAnnouncement {
		t.Fatalf("删除事件不对：%+v", deletedEvents)
	}
	if got := deletedEvents[0].Data["ids"].([]int64); len(got) != 1 || got[0] != muted.ID {
		t.Fatalf("批量删除事件的 ids 不对：%v", got)
	}
	if len(eventsOf(events, bob, notificationdomain.EventDeleted)) != 0 {
		t.Fatal("alice 的操作不应给 bob 发事件")
	}

	// ── 管理员群发：默认公告活动，带动作、配图、过期时间；列表按分类筛选 ──
	_, err = svc.AdminBulkSend(ctx, appID, notificationdomain.AdminBulkSendCommand{UserIDs: []int64{alice}, Type: "system",
		Title: "x", Content: "x", Action: &notificationdomain.Action{Type: "route", Target: "nope"}})
	expectCode("群发非法动作", err, errCodeNotificationActionInvalid)
	bulk, err := svc.AdminBulkSend(ctx, appID, notificationdomain.AdminBulkSendCommand{
		UserIDs: []int64{alice, bob}, Type: "system", Title: "版本更新", Content: "新版本已发布",
		Action: &notificationdomain.Action{Type: "route", Target: "points"}, Image: "https://cdn.example.com/b.png", ExpiresAt: &future,
	})
	if err != nil || bulk.Delivered != 2 {
		t.Fatalf("群发：%+v %v", bulk, err)
	}
	adminList, err := svc.AdminList(ctx, appID, notificationdomain.AdminListQuery{Category: notificationdomain.CategoryAnnouncement, Keyword: "版本更新"})
	if err != nil || adminList.Total != 2 {
		t.Fatalf("管理端按分类筛选：%+v %v", adminList, err)
	}
	for _, item := range adminList.Items {
		if item.Category != notificationdomain.CategoryAnnouncement || item.CategoryLabel != "公告活动" || item.Action == nil ||
			item.Action.Target != "points" || item.ExpiresAt == nil || item.Image == nil || item.Status != "unread" {
			t.Fatalf("管理端列表项缺字段：%+v", item)
		}
	}
	if _, err := svc.AdminList(ctx, appID, notificationdomain.AdminListQuery{Category: "nope"}); errCode(err) != errCodeNotificationCategoryInvalid {
		t.Fatalf("管理端未知分类应报 42231：%v", err)
	}
	events = drainEvents()
	if len(eventsOf(events, alice, notificationdomain.EventCreated)) != 1 || len(eventsOf(events, bob, notificationdomain.EventCreated)) != 1 {
		t.Fatalf("群发应给每个收件人发 created 事件：%+v", events)
	}
	// 管理员删除：受影响的用户收到 notification.deleted
	if _, err := svc.AdminDelete(ctx, appID, []int64{adminList.Items[0].ID}); err != nil {
		t.Fatal(err)
	}
	events = drainEvents()
	if len(events) == 0 || !strings.HasPrefix(events[0].Type, "notification.") {
		t.Fatalf("管理员删除应推送事件：%+v", events)
	}
	if deletedOwner := *adminList.Items[0].UserID; len(eventsOf(events, deletedOwner, notificationdomain.EventDeleted)) != 1 {
		t.Fatalf("被删消息的主人应收到 deleted 事件：%+v", events)
	}
}
