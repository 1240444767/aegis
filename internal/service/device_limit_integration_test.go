package service

import (
	"context"
	"testing"
	"time"

	"aegis/internal/config"
	devicedomain "aegis/internal/domain/device"
	"aegis/internal/domain/devicelimit"
	"aegis/internal/event"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"

	miniredis "github.com/alicebob/miniredis/v2"
	redislib "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

// 登录设备数控制的端到端集成测试：真实 Postgres（全部迁移）+ miniredis。
//
// 同设备顶替 → 总数超限自动下线最早的（被挤下线的令牌与刷新令牌报 40130）→ 类别上限只在同类别里挑 →
// 拒绝模式 40997 → 让用户选择（不签发令牌、凭证完成登录、无效凭证 40496）→ 新设备频率 40998 →
// 单人豁免 → 管理端下线（40131）、按策略整理、台账、概览与处置记录。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55471/postgres go test ./internal/service -run TestDeviceLimitIntegration
func TestDeviceLimitIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	const appID = int64(34000)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'device-limit-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	mr := miniredis.RunT(t)
	rdb := redislib.NewClient(&redislib.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	cfg := config.Config{}
	cfg.JWT.Secret = "device-limit-test-secret-device-limit-test-secret"
	cfg.JWT.Issuer = "aegis-test"
	cfg.JWT.TTL = time.Hour
	cfg.JWT.RefreshTTL = 24 * time.Hour

	log := zap.NewNop()
	pg := pgrepo.New(pool)
	sessions := redisrepo.NewSessionRepository(rdb, "t")
	apps := NewAppService(log, pg, sessions)
	auth := NewAuthService(cfg, log, pg, sessions, event.NewPublisher(&captureJetStream{published: map[string][][]byte{}}), apps, nil)
	limits := NewDeviceLimitService(log, pg, sessions, apps, cfg.JWT.RefreshTTL)
	auth.SetDeviceLimitService(limits)

	hash, _ := bcrypt.GenerateFromPassword([]byte("Passw0rd!"), bcrypt.MinCost)
	user, err := pg.CreateUser(ctx, appID, "alice", string(hash))
	if err != nil {
		t.Fatal(err)
	}

	type login struct{ access, refresh string }
	signIn := func(deviceID, platform string, ask bool) (login, error) {
		t.Helper()
		lctx := devicedomain.WithClient(ctx, devicedomain.Client{Model: "M-" + deviceID, Platform: platform})
		if ask {
			lctx = devicelimit.WithChallengeSupport(lctx)
		}
		result, err := auth.PasswordLogin(lctx, appID, "alice", "Passw0rd!", deviceID, "设备 "+deviceID, "10.0.0.1", "ua-"+deviceID)
		if err != nil {
			return login{}, err
		}
		return login{access: result.AccessToken, refresh: result.RefreshToken}, nil
	}
	mustSignIn := func(deviceID, platform string) login {
		t.Helper()
		item, err := signIn(deviceID, platform, false)
		if err != nil {
			t.Fatalf("%s 登录失败：%v", deviceID, err)
		}
		if item.access == "" {
			t.Fatalf("%s 登录没有拿到令牌", deviceID)
		}
		return item
	}
	expectCode := func(name string, err error, want int) {
		t.Helper()
		if errCode(err) != want {
			t.Fatalf("%s：期望错误码 %d，实际 %v", name, want, err)
		}
	}
	expectValid := func(name string, item login) {
		t.Helper()
		if _, err := auth.ValidateAccessToken(ctx, item.access); err != nil {
			t.Fatalf("%s 应仍然有效：%v", name, err)
		}
	}
	activeKeys := func() []string {
		t.Helper()
		devices, err := limits.ActiveDevices(ctx, appID, user.ID)
		if err != nil {
			t.Fatal(err)
		}
		keys := []string{}
		for _, device := range devices {
			keys = append(keys, device.Key)
		}
		return keys
	}
	savePolicy := func(mutate func(*devicelimit.Policy)) {
		t.Helper()
		policy := devicelimit.DefaultPolicy()
		policy.Enabled = true
		mutate(&policy)
		if _, err := limits.SavePolicy(ctx, appID, policy); err != nil {
			t.Fatalf("保存策略失败：%v", err)
		}
	}

	// ── 未配置时按旧字段推导：没开就不限制 ──
	view, err := limits.Policy(ctx, appID)
	if err != nil || view.Configured || view.Policy.Enabled {
		t.Fatalf("未配置时应是未开启、未保存：%+v %v", view, err)
	}

	// ── 总数 2，自动下线最早登录的 ──
	savePolicy(func(p *devicelimit.Policy) { p.MaxDevices = 2 })
	a1 := mustSignIn("dev-A", "android")
	b := mustSignIn("dev-B", "windows")
	a2 := mustSignIn("dev-A", "android") // 同设备再登录：顶掉 a1，不算超限
	expectCode("同设备被顶掉的旧令牌", func() error { _, err := auth.ValidateAccessToken(ctx, a1.access); return err }(), 40104)
	expectValid("dev-B", b)
	if keys := activeKeys(); len(keys) != 2 {
		t.Fatalf("同设备再登录后仍应是 2 台，实际 %v", keys)
	}
	c := mustSignIn("dev-C", "ios")
	expectCode("被挤下线的访问令牌", func() error { _, err := auth.ValidateAccessToken(ctx, b.access); return err }(), 40130)
	_, err = auth.Refresh(ctx, b.refresh, "dev-B", "10.0.0.1", "ua-dev-B")
	expectCode("被挤下线的刷新令牌", err, 40130)
	expectValid("dev-A", a2)
	expectValid("dev-C", c)

	// ── 类别上限：手机 1 台，只在手机里挑 ──
	savePolicy(func(p *devicelimit.Policy) {
		p.MaxDevices = 3
		p.ClassLimits = map[string]int{devicelimit.ClassMobile: 1}
	})
	pc := mustSignIn("dev-PC", "macos")
	d := mustSignIn("dev-D", "android")
	expectCode("手机超限下线 dev-A", func() error { _, err := auth.ValidateAccessToken(ctx, a2.access); return err }(), 40130)
	expectCode("手机超限下线 dev-C", func() error { _, err := auth.ValidateAccessToken(ctx, c.access); return err }(), 40130)
	expectValid("电脑不受手机上限影响", pc)
	expectValid("dev-D", d)

	// ── 拒绝模式 ──
	savePolicy(func(p *devicelimit.Policy) { p.MaxDevices = 2; p.Overflow = devicelimit.OverflowReject })
	_, err = signIn("dev-E", "windows", false)
	expectCode("拒绝模式", err, 40997)
	expectValid("拒绝时已登录的不受影响", pc)

	// ── 让用户选择 ──
	savePolicy(func(p *devicelimit.Policy) {
		p.MaxDevices = 2
		p.Overflow = devicelimit.OverflowAsk
		p.AskFallback = devicelimit.OverflowReject
	})
	_, err = signIn("dev-E", "windows", false)
	expectCode("没有声明选择页时按兜底方式拒绝", err, 40997)
	lctx := devicelimit.WithChallengeSupport(devicedomain.WithClient(ctx, devicedomain.Client{Platform: "windows"}))
	pending, err := auth.PasswordLogin(lctx, appID, "alice", "Passw0rd!", "dev-E", "设备 dev-E", "10.0.0.2", "ua-dev-E")
	if err != nil {
		t.Fatalf("选择页登录失败：%v", err)
	}
	if pending.AccessToken != "" || pending.DeviceLimit == nil || pending.AuthenticationState != AuthStateDeviceLimitRequired {
		t.Fatalf("应返回选择页且不签发令牌：%+v", pending)
	}
	if pending.DeviceLimit.MustRemove != 1 || len(pending.DeviceLimit.Devices) != 2 {
		t.Fatalf("选择页内容不对：%+v", pending.DeviceLimit)
	}
	_, err = auth.ResolveDeviceLimitChallenge(ctx, appID, "not-a-ticket", []string{"dev-PC"})
	expectCode("无效凭证", err, 40496)
	resolved, err := auth.ResolveDeviceLimitChallenge(ctx, appID, pending.DeviceLimit.Ticket, []string{"dev-PC"})
	if err != nil || resolved.AccessToken == "" {
		t.Fatalf("选好后应完成登录：%+v %v", resolved, err)
	}
	expectCode("用户选中下线的 dev-PC", func() error { _, err := auth.ValidateAccessToken(ctx, pc.access); return err }(), 40130)
	expectValid("dev-D", d)
	_, err = auth.ResolveDeviceLimitChallenge(ctx, appID, pending.DeviceLimit.Ticket, []string{"dev-D"})
	expectCode("凭证用过即作废", err, 40496)

	// ── 新设备频率：台账里已有 A B C PC D E 六台 ──
	savePolicy(func(p *devicelimit.Policy) {
		p.MaxDevices = 0
		p.NewDevice = devicelimit.NewDeviceRule{Enabled: true, WindowDays: 30, MaxNew: 6}
	})
	_, err = signIn("dev-F", "android", false)
	expectCode("窗口内新设备已满", err, 40998)
	mustSignIn("dev-B", "windows") // 登录过的设备不受影响

	// ── 单人豁免 ──
	if _, err := limits.SaveOverride(ctx, appID, user.ID, devicelimit.OverrideInput{Exempt: true, Note: "测试账号"}, "root"); err != nil {
		t.Fatal(err)
	}
	f := mustSignIn("dev-F", "android")
	if err := limits.DeleteOverride(ctx, appID, user.ID); err != nil {
		t.Fatal(err)
	}

	// ── 管理端：下线、整理、用户视图 ──
	removed, err := limits.RemoveDevices(ctx, appID, user.ID, []string{"dev-F"}, devicelimit.EventAdminRemoved, "root", "")
	if err != nil || removed != 1 {
		t.Fatalf("管理员下线失败：%d %v", removed, err)
	}
	expectCode("被管理员下线", func() error { _, err := auth.ValidateAccessToken(ctx, f.access); return err }(), 40131)
	savePolicy(func(p *devicelimit.Policy) { p.MaxDevices = 1 })
	trimmed, err := limits.Enforce(ctx, appID, user.ID, "root")
	if err != nil {
		t.Fatal(err)
	}
	if keys := activeKeys(); len(keys) != 1 || trimmed == 0 {
		t.Fatalf("整理后应只剩 1 台，下线了 %d 台，剩 %v", trimmed, keys)
	}
	userView, err := limits.UserView(ctx, appID, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(userView.Ledger) != 7 || userView.Status.Usage.Total != 1 || userView.Status.Limits.MaxDevices != 1 {
		t.Fatalf("用户视图不对：台账 %d 台、在线 %d 台、上限 %d", len(userView.Ledger), userView.Status.Usage.Total, userView.Status.Limits.MaxDevices)
	}

	// ── 处置记录是异步写的，等一会儿再看概览 ──
	var overview *devicelimit.Overview
	deadline := time.Now().Add(5 * time.Second)
	for {
		overview, err = limits.Overview(context.Background(), appID, 7, "Asia/Shanghai")
		if err != nil {
			t.Fatal(err)
		}
		if overview.Totals[devicelimit.EventAdminRemoved] == 1 && overview.Totals[devicelimit.EventAdminEnforced] == int64(trimmed) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("处置记录没写全：%+v", overview.Totals)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if overview.Totals[devicelimit.EventKicked] != 3 || overview.Totals[devicelimit.EventRejected] != 2 ||
		overview.Totals[devicelimit.EventChallenged] != 1 || overview.Totals[devicelimit.EventResolved] != 1 ||
		overview.Totals[devicelimit.EventNewDevice] != 1 {
		t.Fatalf("各类处置次数不对：%+v", overview.Totals)
	}
	if len(overview.Trend) != 7 || overview.ActiveDevices != 7 || overview.ActiveUsers != 1 || overview.ByClass[devicelimit.ClassMobile] != 4 {
		t.Fatalf("概览统计不对：trend=%d devices=%d users=%d byClass=%v", len(overview.Trend), overview.ActiveDevices, overview.ActiveUsers, overview.ByClass)
	}
	events, err := limits.Events(ctx, appID, devicelimit.EventQuery{Action: devicelimit.EventKicked})
	if err != nil || events.Total != 3 || events.Items[0].Account != "alice" || events.Items[0].TriggerName == "" {
		t.Fatalf("处置记录列表不对：%+v %v", events, err)
	}

	// ── 清台账后新设备频率从零算 ──
	if n, err := limits.ResetLedger(ctx, appID, user.ID, ""); err != nil || n != 7 {
		t.Fatalf("清台账：%d %v", n, err)
	}
}
