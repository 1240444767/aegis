package service

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	redislib "github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"aegis/internal/config"
	appdomain "aegis/internal/domain/app"
	authdomain "aegis/internal/domain/auth"
	"aegis/internal/domain/pointstore"
	userdomain "aegis/internal/domain/user"
	vipdomain "aegis/internal/domain/vip"
	"aegis/internal/event"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"
)

type recordedNotice struct {
	userID int64
	title  string
	status string
}

type fakeAutoSignNotifier struct{ notices []recordedNotice }

func (n *fakeAutoSignNotifier) NotifyUser(_ context.Context, _ int64, userID int64, _ string, title string, _ string, _ string, metadata map[string]any) error {
	status, _ := metadata["status"].(string)
	n.notices = append(n.notices, recordedNotice{userID: userID, title: title, status: status})
	return nil
}

// 自动签到的端到端集成测试：真实 Postgres（全部迁移）+ miniredis。
// 「首次取用户信息初始化设置 → 未开放时不许开 → 仅会员开放 → 非会员被拒、会员开启并排班 →
// 到点签到并通知、次日排班 → 会员到期后暂停并通知、个人设置保留 → 改成指定功能开放 →
// 重建排班 → 统计」。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestAutoSignIntegration
func TestAutoSignIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	mr := miniredis.RunT(t)
	rdb := redislib.NewClient(&redislib.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	sessions := redisrepo.NewSessionRepository(rdb, "t")
	schedules := redisrepo.NewAutoSignRepository(rdb, "t")
	publisher := event.NewPublisher(&captureJetStream{published: map[string][][]byte{}})
	log := zap.NewNop()

	apps := NewAppService(log, pg, sessions)
	users := NewUserService(log, pg, sessions, publisher, nil)
	signIn := NewSignInService(log, pg, sessions, publisher)
	autoSign := NewAutoSignService(config.AutoSignConfig{
		Enabled: true, Timezone: "Asia/Shanghai", BatchSize: 50, Concurrency: 4, RetryDelay: 5 * time.Minute,
	}, log, pg, schedules, signIn)
	notifier := &fakeAutoSignNotifier{}
	autoSign.SetNotifier(notifier)
	users.SetAutoSignGuard(autoSign)
	vip := NewVipService(log, pg)

	const appID = int64(31000)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'auto-sign-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	alice := insertTestUser(t, ctx, pool, appID, "auto-alice", "0")
	bob := insertTestUser(t, ctx, pool, appID, "auto-bob", "0")
	sessionOf := func(userID int64) *authdomain.Session { return &authdomain.Session{AppID: appID, UserID: userID} }
	enable := func(userID int64, patch map[string]any) error {
		_, err := users.UpdateSettings(ctx, sessionOf(userID), "autoSign", patch)
		return err
	}
	status := func(userID int64) *userdomain.AutoSignStatus {
		t.Helper()
		st, err := autoSign.Status(ctx, appID, userID)
		if err != nil {
			t.Fatal(err)
		}
		return st
	}

	// ── 首次取用户信息时把各类设置的默认值落库 ──
	if _, err := users.GetMy(ctx, sessionOf(alice)); err != nil {
		t.Fatal(err)
	}
	var categories int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM user_settings WHERE user_id = $1`, alice).Scan(&categories); err != nil {
		t.Fatal(err)
	}
	if categories != len(users.ListSettingCategories()) {
		t.Fatalf("首次取用户信息后应初始化 %d 类设置，实际 %d", len(users.ListSettingCategories()), categories)
	}
	stored, err := pg.GetUserSettings(ctx, alice, "autoSign")
	if err != nil || stored == nil || userdomain.AutoSignSettingsFromMap(stored.Settings) != userdomain.DefaultAutoSignSettings() {
		t.Fatalf("自动签到默认设置不对：%+v %v", stored, err)
	}
	// 再取一次不重复插入，已有设置不被覆盖
	if _, err := pool.Exec(ctx, `DELETE FROM user_settings WHERE user_id = $1 AND category = 'ui'`, alice); err != nil {
		t.Fatal(err)
	}
	if inserted, err := pg.EnsureDefaultUserSettings(ctx, alice, map[string]map[string]any{
		"ui": {"theme": "dark"}, "autoSign": {"enabled": true},
	}); err != nil || inserted != 1 {
		t.Fatalf("只应补上缺的 ui 一类，得到 %d %v", inserted, err)
	}
	if again, _ := pg.GetUserSettings(ctx, alice, "autoSign"); again.Settings["enabled"] != false {
		t.Fatal("已有的设置不应被默认值覆盖")
	}

	// ── 未开放：不许开，也报不出可用 ──
	if err := enable(alice, map[string]any{"enabled": true}); errCode(err) != errCodeAutoSignDisabled {
		t.Fatalf("未开放时开启应返回 %d，得到 %v", errCodeAutoSignDisabled, err)
	}
	if st := status(alice); st.Available || st.Reason != appdomain.AutoSignReasonDisabled || st.Access.Enabled {
		t.Fatalf("未开放时状态不对：%+v", st)
	}
	// 关着的时候改别的字段不受影响；坏值被拒
	if err := enable(alice, map[string]any{"time": "7:5"}); err != nil {
		t.Fatal(err)
	}
	if err := enable(alice, map[string]any{"time": "25:00"}); errCode(err) != 40000 {
		t.Fatalf("非法时刻应返回 40000，得到 %v", err)
	}

	// ── 仅会员开放 ──
	if _, err := autoSign.SavePolicy(ctx, apps, appID, appdomain.AutoSignPolicy{Enabled: true, Audience: appdomain.AutoSignAudienceVip, IncludeTrial: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := autoSign.SavePolicy(ctx, apps, appID, appdomain.AutoSignPolicy{Enabled: true, Audience: appdomain.AutoSignAudienceFeature, FeatureTag: "missing"}); errCode(err) != errCodeFeatureUnknown {
		t.Fatalf("未登记的功能应被拒，得到 %v", err)
	}
	if err := enable(bob, map[string]any{"enabled": true}); errCode(err) != errCodeAutoSignNotEligible {
		t.Fatalf("非会员开启应返回 %d，得到 %v", errCodeAutoSignNotEligible, err)
	}
	if st := status(bob); st.Reason != appdomain.AutoSignReasonVipRequired || st.Message != "自动签到仅限会员使用" {
		t.Fatalf("非会员状态不对：%+v", st)
	}
	if _, err := vip.AdminGrantVip(ctx, AdminVipGrantInput{UserID: alice, AppID: appID, Days: 7, Operator: "admin"}); err != nil {
		t.Fatal(err)
	}
	if err := enable(alice, map[string]any{"enabled": true, "time": "00:00"}); err != nil {
		t.Fatalf("会员应当能开启：%v", err)
	}
	if err := autoSign.SyncUserSchedule(ctx, alice, appID); err != nil {
		t.Fatal(err)
	}
	st := status(alice)
	if !st.Active || st.Settings.Time != "00:00" || st.NextRunAt == nil {
		t.Fatalf("开启后应当已排班：%+v", st)
	}

	// ── 到点签到：成功、通知、排到次日同一时刻；另一个实例取不到同一条 ──
	processed, err := autoSign.RunDue(ctx)
	if err != nil || processed != 1 {
		t.Fatalf("应执行 1 条，得到 %d %v", processed, err)
	}
	if again, _ := autoSign.RunDue(ctx); again != 0 {
		t.Fatalf("签完不应再执行，得到 %d", again)
	}
	st = status(alice)
	if st.LastRun == nil || st.LastRun.Status != userdomain.AutoSignRunSuccess || st.NextRunAt == nil || !st.NextRunAt.After(time.Now()) {
		t.Fatalf("签到后状态不对：%+v %+v", st, st.LastRun)
	}
	var source string
	if err := pool.QueryRow(ctx, `SELECT sign_in_source FROM daily_signins WHERE user_id = $1`, alice).Scan(&source); err != nil || source != autoSignSource {
		t.Fatalf("签到记录来源应为 auto，得到 %q %v", source, err)
	}
	if len(notifier.notices) != 1 || notifier.notices[0].status != userdomain.AutoSignRunSuccess {
		t.Fatalf("应发一条成功通知：%+v", notifier.notices)
	}

	// ── 今天已签：再到点只记 already_signed，不再通知 ──
	if err := schedules.Schedule(ctx, appID, alice, time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := autoSign.RunDue(ctx); err != nil {
		t.Fatal(err)
	}
	if st = status(alice); st.LastRun.Status != userdomain.AutoSignRunAlreadySigned || len(notifier.notices) != 1 {
		t.Fatalf("已签时状态不对：%+v，通知 %d 条", st.LastRun, len(notifier.notices))
	}

	// ── 会员到期：到点时暂停并通知一次，个人设置保留 ──
	if _, err := pool.Exec(ctx, `UPDATE users SET vip_expire_at = NOW() - INTERVAL '1 minute' WHERE id = $1`, alice); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE vip_transactions SET expire_after = NOW() - INTERVAL '1 minute' WHERE user_id = $1`, alice); err != nil {
		t.Fatal(err)
	}
	if err := schedules.Schedule(ctx, appID, alice, time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := autoSign.RunDue(ctx); err != nil {
		t.Fatal(err)
	}
	st = status(alice)
	if st.Active || st.Available || !st.Settings.Enabled || st.LastRun.Status != userdomain.AutoSignRunSuspended || st.NextRunAt != nil {
		t.Fatalf("到期后应暂停且保留开关：%+v %+v", st, st.LastRun)
	}
	if len(notifier.notices) != 2 || notifier.notices[1].status != userdomain.AutoSignRunSuspended {
		t.Fatalf("应发一条暂停通知：%+v", notifier.notices)
	}
	// 暂停中仍可改时刻、可关掉
	if err := enable(alice, map[string]any{"time": "09:30"}); err != nil {
		t.Fatalf("暂停中应能改时刻：%v", err)
	}

	// ── 改成「持有指定功能」开放：功能解锁（非会员）即有资格，重建排班把人排回来 ──
	if _, err := vip.AdminSaveFeature(ctx, vipdomain.FeatureMutation{AppID: appID, Tag: "auto_sign", Name: ptrTo("自动签到")}); err != nil {
		t.Fatal(err)
	}
	if _, err := autoSign.SavePolicy(ctx, apps, appID, appdomain.AutoSignPolicy{Enabled: true, Audience: appdomain.AutoSignAudienceFeature, FeatureTag: "auto_sign"}); err != nil {
		t.Fatal(err)
	}
	if st = status(alice); st.Reason != appdomain.AutoSignReasonFeatureRequired || st.Message != "自动签到需要开通「自动签到」" {
		t.Fatalf("缺功能时状态不对：%+v", st)
	}
	store := NewPointsStoreService(log, pg)
	if _, err := store.GrantUnlock(ctx, pointstore.UnlockGrantInput{AppID: appID, UserID: alice, Tag: "auto_sign", Operator: "admin"}); err != nil {
		t.Fatal(err)
	}
	if scheduled, err := autoSign.RebuildApp(ctx, appID); err != nil || scheduled != 1 {
		t.Fatalf("重建应排上 1 人，得到 %d %v", scheduled, err)
	}
	if st = status(alice); !st.Active || st.NextRunAt == nil || st.Access.FeatureName != "自动签到" {
		t.Fatalf("解锁功能后应恢复：%+v", st)
	}

	// ── 关闭开放：所有人撤下 ──
	if _, err := autoSign.SavePolicy(ctx, apps, appID, appdomain.AutoSignPolicy{Enabled: false, Audience: appdomain.AutoSignAudienceAll}); err != nil {
		t.Fatal(err)
	}
	if _, err := autoSign.RebuildApp(ctx, appID); err != nil {
		t.Fatal(err)
	}
	if next, _ := schedules.NextRunAt(ctx, appID, alice); next != nil {
		t.Fatal("关闭开放后应撤下排班")
	}

	// ── 统计 ──
	overview, err := autoSign.AdminOverview(ctx, appID)
	if err != nil {
		t.Fatal(err)
	}
	if overview.EnabledUsers != 1 || overview.TodayCount != 1 || len(overview.Trend) != autoSignTrendDays || overview.Trend[autoSignTrendDays-1].Count != 1 {
		t.Fatalf("统计不对：%+v", overview)
	}
}
