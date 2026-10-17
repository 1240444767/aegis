package service

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"aegis/internal/config"
	"aegis/internal/domain/accountdeletion"
	authdomain "aegis/internal/domain/auth"
	"aegis/internal/event"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"

	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pquerna/otp/totp"
	redislib "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// 账号注销的端到端集成测试：真实 Postgres（全部迁移）+ miniredis，发信与短信换成记录验证码的假实现。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestAccountDeletionIntegration
func TestAccountDeletionIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	env := newDeletionTestEnv(t, ctx, pool)
	const password = "Delete-Me#2026"

	alice := env.recovery.user(t, "alice", password, "Alice@Example.com", "13800138000")
	aliceSession := env.session(alice)

	// ── 未开放：页面返回 enabled=false，提交被拒 ──
	view, err := env.deletion.ClientView(ctx, aliceSession)
	if err != nil || view.Enabled {
		t.Fatalf("未开放时 enabled 应为 false：%+v / %v", view, err)
	}
	if _, err := env.deletion.Submit(ctx, AccountDeletionSubmitInput{Session: aliceSession, Agree: true, Password: password}); errCode(err) != errCodeDeletionDisabled {
		t.Fatalf("未开放时提交应被拒，得到 %v", err)
	}

	// ── 开放：冷静期 + 立即都开，登录即撤销，注销后 30 天不能重新注册 ──
	cfg := accountdeletion.DefaultConfig()
	cfg.AllowImmediate = true
	cfg.LoginDuringCooling = accountdeletion.LoginCancel
	cfg.ReregisterDays = 30
	bad := cfg
	bad.CoolingDays = 999
	if _, err := env.deletion.SaveSettings(ctx, env.appID, true, bad, "root"); errCode(err) != 40000 {
		t.Fatalf("非法配置应被拒，得到 %v", err)
	}
	if _, err := env.deletion.SaveSettings(ctx, env.appID, true, cfg, "root"); err != nil {
		t.Fatal(err)
	}

	view, err = env.deletion.ClientView(ctx, aliceSession)
	if err != nil {
		t.Fatal(err)
	}
	if !view.Enabled || len(view.Modes) != 2 || view.Modes[0].Mode != accountdeletion.ModeCooling || view.Blocked || !view.LoginCancels {
		t.Fatalf("注销页不对：%+v", view)
	}
	if len(view.Verification.Methods) != 3 || view.Verification.TOTP {
		t.Fatalf("密码、邮箱、手机任选其一：%+v", view.Verification)
	}
	for _, method := range view.Verification.Methods {
		if strings.Contains(method.Masked, "alice") || strings.Contains(method.Masked, "0013") {
			t.Fatalf("联系方式没有打码：%+v", method)
		}
	}

	// 没勾同意、没验证、密码错
	if _, err := env.deletion.Submit(ctx, AccountDeletionSubmitInput{Session: aliceSession, Password: password}); errCode(err) != 40000 {
		t.Fatalf("未同意须知应被拒，得到 %v", err)
	}
	if _, err := env.deletion.Submit(ctx, AccountDeletionSubmitInput{Session: aliceSession, Agree: true}); errCode(err) != errCodeDeletionVerifyRequired {
		t.Fatalf("没带验证应被拒，得到 %v", err)
	}
	if _, err := env.deletion.Submit(ctx, AccountDeletionSubmitInput{Session: aliceSession, Agree: true, Password: "wrong"}); errCode(err) != errCodeDeletionVerifyFailed {
		t.Fatalf("密码错误应被拒，得到 %v", err)
	}

	// ── 冷静期：提交后其他设备下线，再次提交被拒 ──
	if _, err := env.recovery.auth.PasswordLogin(ctx, env.appID, "alice", password, "dev-other", "旧手机", "10.0.0.2", "ua"); err != nil {
		t.Fatal(err)
	}
	submitted, err := env.deletion.Submit(ctx, AccountDeletionSubmitInput{
		Session: aliceSession, Agree: true, Password: password, ReasonCode: "privacy", ReasonText: "不想用了", IP: "10.0.0.1", Device: "新手机",
	})
	if err != nil {
		t.Fatal(err)
	}
	if submitted.Deleted || submitted.Request.Status != accountdeletion.StatusScheduled || submitted.Request.ScheduledAt == nil || submitted.SignedOut < 1 {
		t.Fatalf("冷静期提交结果不对：%+v", submitted)
	}
	if days := submitted.Request.ScheduledAt.Sub(time.Now()).Hours() / 24; days < 14.9 || days > 15.1 {
		t.Fatalf("冷静期应为 15 天，得到 %.2f 天", days)
	}
	if left, _ := env.sessions.ListUserSessions(ctx, env.appID, alice); len(left) != 0 {
		t.Fatalf("其他设备应当下线，还剩 %d 个会话", len(left))
	}
	if _, err := env.deletion.Submit(ctx, AccountDeletionSubmitInput{Session: aliceSession, Agree: true, Password: password}); errCode(err) != errCodeDeletionOpenExists {
		t.Fatalf("已有申请时再提交应被拒，得到 %v", err)
	}
	env.recovery.waitNotification(t, ctx, alice, "账号进入注销冷静期")

	// ── 冷静期内登录：按配置自动撤销，登录结果里带着结论 ──
	login, err := env.recovery.auth.PasswordLogin(ctx, env.appID, "alice", password, "dev-2", "平板", "10.0.0.3", "ua")
	if err != nil {
		t.Fatal(err)
	}
	if login.AccountDeletion == nil || !login.AccountDeletion.Cancelled {
		t.Fatalf("冷静期内登录应撤销注销：%+v", login.AccountDeletion)
	}
	if open, _ := env.pg.GetOpenAccountDeletionRequest(ctx, env.appID, alice); open != nil {
		t.Fatalf("撤销后不应再有进行中的申请：%+v", open)
	}

	// ── 邮箱验证码：一分钟内不能重发，码错算失败，码对通过 ──
	sent, err := env.deletion.SendCode(ctx, aliceSession, "email", "10.0.0.1")
	if err != nil || sent.Masked == "" {
		t.Fatalf("发邮箱验证码失败：%+v / %v", sent, err)
	}
	if _, err := env.deletion.SendCode(ctx, aliceSession, "email", "10.0.0.1"); errCode(err) != errCodeDeletionResendTooSoon {
		t.Fatalf("一分钟内重发应被拒，得到 %v", err)
	}
	code := env.mailer.last(t, "Alice@Example.com")
	if _, err := env.deletion.Submit(ctx, AccountDeletionSubmitInput{Session: aliceSession, Agree: true, Channel: "email", Code: wrongCode(code)}); errCode(err) != errCodeDeletionVerifyFailed {
		t.Fatalf("错误的邮箱验证码应被拒，得到 %v", err)
	}
	submitted, err = env.deletion.Submit(ctx, AccountDeletionSubmitInput{Session: aliceSession, Agree: true, Channel: "email", Code: code})
	if err != nil || submitted.Request.Status != accountdeletion.StatusScheduled {
		t.Fatalf("邮箱验证后进入冷静期：%+v / %v", submitted, err)
	}
	if _, err := env.deletion.Submit(ctx, AccountDeletionSubmitInput{Session: aliceSession, Agree: true, Channel: "email", Code: code}); errCode(err) != errCodeDeletionOpenExists {
		t.Fatalf("验证码只能用一次（且已有申请），得到 %v", err)
	}

	// 改期只能往后，改完提醒重算
	if _, err := env.deletion.Reschedule(ctx, env.appID, submitted.Request.ID, time.Now().Add(-time.Hour), false); errCode(err) != 40000 {
		t.Fatalf("改到过去应被拒，得到 %v", err)
	}
	rescheduled, err := env.deletion.Reschedule(ctx, env.appID, submitted.Request.ID, time.Now().Add(72*time.Hour), false)
	if err != nil || rescheduled.RemindAt == nil {
		t.Fatalf("改期失败：%+v / %v", rescheduled, err)
	}

	// ── 到期：后台循环先发提醒再执行 ──
	if _, err := pool.Exec(ctx, `UPDATE account_deletion_requests SET remind_at = NOW() - INTERVAL '1 minute' WHERE id = $1`, submitted.Request.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := env.deletion.RunDue(ctx); err != nil || n != 0 {
		t.Fatalf("还没到期不应执行：%d / %v", n, err)
	}
	env.recovery.waitNotification(t, ctx, alice, "账号即将注销")
	if _, err := pool.Exec(ctx, `UPDATE account_deletion_requests SET scheduled_at = NOW() - INTERVAL '1 minute' WHERE id = $1`, submitted.Request.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := env.deletion.RunDue(ctx); err != nil || n != 1 {
		t.Fatalf("到期应执行一条：%d / %v", n, err)
	}
	if user, _ := env.pg.GetUserByID(ctx, alice); user != nil {
		t.Fatal("到期后账号应当已删除")
	}
	done, err := env.deletion.GetRequest(ctx, env.appID, submitted.Request.ID)
	if err != nil || done.Status != accountdeletion.StatusCompleted || done.UserExists || done.Account != "alice" {
		t.Fatalf("申请应记为完成并保留账号名：%+v / %v", done, err)
	}

	// ── 重新注册限制：账号名、邮箱都被挡；解除后放行 ──
	_, err = env.recovery.auth.RegisterWithPassword(ctx, PasswordRegisterInput{AppID: env.appID, Account: "alice", Password: password, SuppressSession: true})
	if errCode(err) != errCodeDeletionReregister {
		t.Fatalf("注销 30 天内重新注册应被拒，得到 %v", err)
	}
	_, err = env.recovery.auth.RegisterWithPassword(ctx, PasswordRegisterInput{AppID: env.appID, Account: "alice2", Password: password,
		Profile: map[string]any{"email": "alice@EXAMPLE.com"}, SuppressSession: true})
	if errCode(err) != errCodeDeletionReregister {
		t.Fatalf("同一邮箱重新注册应被拒，得到 %v", err)
	}
	tombs, err := env.deletion.ListTombstones(ctx, env.appID, "", 1, 20)
	if err != nil || tombs.Total != 3 {
		t.Fatalf("应有账号、邮箱、手机三条限制：%+v / %v", tombs, err)
	}
	for _, item := range tombs.Items {
		if strings.Contains(item.Hint, "alice@") || strings.Contains(item.Hint, "13800138000") {
			t.Fatalf("限制记录不应存明文：%+v", item)
		}
		if item.Kind == accountdeletion.TombstoneAccount {
			if err := env.deletion.ReleaseTombstone(ctx, env.appID, item.ID); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, err := env.recovery.auth.RegisterWithPassword(ctx, PasswordRegisterInput{AppID: env.appID, Account: "alice", Password: password, SuppressSession: true}); err != nil {
		t.Fatalf("解除账号名限制后应能注册：%v", err)
	}
	if notice := env.mailer.lastNotice("Alice@Example.com"); notice.Kind != AccountDeletionNoticeCompleted {
		t.Fatalf("注销完成应当发告知信，最后一封是 %q", notice.Kind)
	}

	// ── 立即注销 + 两步验证 ──
	bob := env.recovery.user(t, "bob", password, "", "")
	secret := env.recovery.enableTOTP(t, ctx, bob)
	bobSession := env.session(bob)
	view, _ = env.deletion.ClientView(ctx, bobSession)
	if !view.Verification.TOTP || len(view.Verification.Methods) != 1 {
		t.Fatalf("bob 只有密码，并要求两步验证：%+v", view.Verification)
	}
	if _, err := env.deletion.Submit(ctx, AccountDeletionSubmitInput{Session: bobSession, Agree: true, Mode: "immediate", Password: password}); errCode(err) != errCodeDeletionVerifyRequired {
		t.Fatalf("缺动态码应被拒，得到 %v", err)
	}
	totpCode, _ := totp.GenerateCode(secret, time.Now())
	immediate, err := env.deletion.Submit(ctx, AccountDeletionSubmitInput{Session: bobSession, Agree: true, Mode: "immediate", Password: password, TOTPCode: totpCode})
	if err != nil || !immediate.Deleted || immediate.Request.Status != accountdeletion.StatusCompleted {
		t.Fatalf("立即注销应当场完成：%+v / %v", immediate, err)
	}
	if user, _ := env.pg.GetUserByID(ctx, bob); user != nil {
		t.Fatal("立即注销后账号应当已删除")
	}

	// ── 前置条件：有待支付订单时挡住 ──
	carol := env.recovery.user(t, "carol", password, "", "")
	if _, err := pool.Exec(ctx, `WITH c AS (INSERT INTO payment_configs (appid, payment_method, config_name) VALUES ($1, 'alipay', 'test') RETURNING id)
INSERT INTO payment_orders (appid, user_id, config_id, order_no, subject, amount, payment_method, provider_type, status)
SELECT $1, $2, c.id, 'D-1', '会员', 10, 'alipay', 'alipay', 'pending' FROM c`, env.appID, carol); err != nil {
		t.Fatal(err)
	}
	carolSession := env.session(carol)
	view, _ = env.deletion.ClientView(ctx, carolSession)
	if !view.Blocked {
		t.Fatalf("有待支付订单时应当 blocked：%+v", view.Checks)
	}
	if _, err := env.deletion.Submit(ctx, AccountDeletionSubmitInput{Session: carolSession, Agree: true, Password: password}); errCode(err) != errCodeDeletionBlocked {
		t.Fatalf("前置条件不满足应被拒，得到 %v", err)
	}

	// ── 失败锁定 ──
	var lockErr error
	dave := env.recovery.user(t, "dave", password, "", "")
	daveSession := env.session(dave)
	for i := 0; i < accountDeletionMaxFailures; i++ {
		_, lockErr = env.deletion.Submit(ctx, AccountDeletionSubmitInput{Session: daveSession, Agree: true, Password: "nope"})
	}
	if errCode(lockErr) != errCodeDeletionTooManyFailure {
		t.Fatalf("连续失败应当锁定，得到 %v", lockErr)
	}
	if _, err := env.deletion.Submit(ctx, AccountDeletionSubmitInput{Session: daveSession, Agree: true, Password: password}); errCode(err) != errCodeDeletionTooManyFailure {
		t.Fatalf("锁定期内密码正确也应被拒，得到 %v", err)
	}
	_ = env.sessions.ResetAccountDeletionCounter(ctx, "fail", env.appID, dave)

	// ── 人工审核：驳回要理由；批准后冷静期从批准时起算；管理员撤销 ──
	cfg.RequireReview = true
	cfg.LoginDuringCooling = accountdeletion.LoginKeep
	if _, err := env.deletion.SaveSettings(ctx, env.appID, true, cfg, "root"); err != nil {
		t.Fatal(err)
	}
	reviewing, err := env.deletion.Submit(ctx, AccountDeletionSubmitInput{Session: daveSession, Agree: true, Password: password})
	if err != nil || reviewing.Request.Status != accountdeletion.StatusReviewing || reviewing.Request.ScheduledAt != nil {
		t.Fatalf("需要审核时先待审核：%+v / %v", reviewing, err)
	}
	if _, err := env.deletion.Reject(ctx, env.appID, reviewing.Request.ID, "", "root"); errCode(err) != 40000 {
		t.Fatalf("驳回不填理由应被拒，得到 %v", err)
	}
	if _, err := env.deletion.Reject(ctx, env.appID, reviewing.Request.ID, "账号有未结清的纠纷", "root"); err != nil {
		t.Fatal(err)
	}
	view, _ = env.deletion.ClientView(ctx, daveSession)
	if view.Request == nil || view.Request.Status != accountdeletion.StatusRejected || view.Request.ReviewNote == "" || view.Request.CanCancel {
		t.Fatalf("驳回结论应当展示在注销页上：%+v", view.Request)
	}
	reviewing, err = env.deletion.Submit(ctx, AccountDeletionSubmitInput{Session: daveSession, Agree: true, Password: password})
	if err != nil {
		t.Fatalf("驳回后可以重新提交：%v", err)
	}
	approved, err := env.deletion.Approve(ctx, env.appID, reviewing.Request.ID, "", "root")
	if err != nil || approved.Status != accountdeletion.StatusScheduled || approved.ScheduledAt == nil || approved.ReviewedBy != "root" {
		t.Fatalf("批准后进入冷静期：%+v / %v", approved, err)
	}
	if _, err := env.deletion.Approve(ctx, env.appID, reviewing.Request.ID, "", "root"); errCode(err) != errCodeDeletionStateChanged {
		t.Fatalf("重复批准应被拒，得到 %v", err)
	}
	login, err = env.recovery.auth.PasswordLogin(ctx, env.appID, "dave", password, "dev-3", "手机", "10.0.0.4", "ua")
	if err != nil || login.AccountDeletion == nil || login.AccountDeletion.Cancelled || login.AccountDeletion.Status != accountdeletion.StatusScheduled {
		t.Fatalf("keep 配置下登录只提示不撤销：%+v / %v", login, err)
	}
	cancelled, err := env.deletion.AdminCancel(ctx, env.appID, reviewing.Request.ID, "用户来电要求保留", "root", true)
	if err != nil || cancelled.Status != accountdeletion.StatusCancelled || cancelled.CancelledBy != "admin:root" {
		t.Fatalf("管理员撤销：%+v / %v", cancelled, err)
	}

	// ── 管理员代发起立即注销：必须输入账号名确认 ──
	if _, err := env.deletion.AdminCreate(ctx, AccountDeletionAdminCreateInput{AppID: env.appID, UserID: dave, Mode: "immediate", Reason: "违规账号", Operator: "root"}); errCode(err) != 40000 {
		t.Fatalf("不确认账号名应被拒，得到 %v", err)
	}
	forced, err := env.deletion.AdminCreate(ctx, AccountDeletionAdminCreateInput{AppID: env.appID, UserID: dave, Mode: "immediate", Reason: "违规账号", Confirm: "dave", Operator: "root"})
	if err != nil || forced.Status != accountdeletion.StatusCompleted || forced.Source != accountdeletion.SourceAdmin {
		t.Fatalf("管理员立即注销：%+v / %v", forced, err)
	}

	// ── 列表与统计 ──
	page, err := env.deletion.ListRequests(ctx, accountdeletion.RequestQuery{AppID: env.appID, Status: accountdeletion.StatusCompleted})
	if err != nil || page.Total != 3 {
		t.Fatalf("已完成的申请应有 3 条：%+v / %v", page, err)
	}
	if page, _ := env.deletion.ListRequests(ctx, accountdeletion.RequestQuery{AppID: env.appID, Keyword: "dave"}); page.Total != 3 {
		t.Fatalf("按账号搜索 dave 应有 3 条，得到 %d", page.Total)
	}
	stats, err := env.deletion.Stats(ctx, env.appID, 7, "Asia/Shanghai")
	if err != nil || stats.Total != 6 || len(stats.Trend) != 7 || stats.Trend[6].Requested != 6 || stats.Trend[6].Completed != 3 {
		t.Fatalf("统计不对：%+v / %v", stats, err)
	}
	userView, err := env.deletion.UserView(ctx, env.appID, dave)
	if err != nil || userView.Open != nil || len(userView.History) != 3 {
		t.Fatalf("用户注销状况不对：%+v / %v", userView, err)
	}
}

type deletionTestEnv struct {
	appID    int64
	pg       *pgrepo.Repository
	sessions *redisrepo.SessionRepository
	deletion *AccountDeletionService
	mailer   *fakeDeletionMailer
	recovery *recoveryTestEnv
}

func (e *deletionTestEnv) session(userID int64) *authdomain.Session {
	return &authdomain.Session{AppID: e.appID, UserID: userID, TokenID: "current-" + time.Now().Format("150405.000"), IssuedAt: time.Now().UTC()}
}

func newDeletionTestEnv(t *testing.T, ctx context.Context, pool *pgxpool.Pool) *deletionTestEnv {
	t.Helper()
	const appID = int64(31200)
	settings, _ := json.Marshal(map[string]any{
		"captcha": map[string]any{"sms": map[string]any{"provider": "aliyun", "signName": "测试", "templateId": "SMS_1"}},
	})
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name, settings) VALUES ($1, 'deletion-test', $2) ON CONFLICT DO NOTHING`, appID, settings); err != nil {
		t.Fatal(err)
	}
	mr := miniredis.RunT(t)
	rdb := redislib.NewClient(&redislib.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	cfg := config.Config{}
	cfg.JWT.Secret = "deletion-test-secret-deletion-test-secret"
	cfg.JWT.Issuer = "aegis-test"
	cfg.JWT.TTL = time.Hour
	cfg.JWT.RefreshTTL = 24 * time.Hour
	cfg.Security.MasterKey = "deletion-test-master-key"
	cfg.Security.Modules.TOTPEnabled = true
	cfg.Security.Modules.RecoveryCodesEnabled = true
	cfg.Security.TOTP.Enabled = true
	cfg.Security.TOTP.Digits = 6
	cfg.Security.TOTP.Skew = 1
	cfg.Security.RecoveryCode.Enabled = true
	cfg.Captcha = config.NormalizeCaptchaConfig(config.CaptchaConfig{Enabled: true, SMS: config.SMSCaptchaConfig{Enabled: true}})

	log := zap.NewNop()
	pg := pgrepo.New(pool)
	sessions := redisrepo.NewSessionRepository(rdb, "t")
	app := NewAppService(log, pg, sessions)
	security := NewSecurityService(cfg, log, pg, sessions, app)
	publisher := event.NewPublisher(&captureJetStream{published: map[string][][]byte{}})
	auth := NewAuthService(cfg, log, pg, sessions, publisher, app, security)
	captcha := NewCaptchaService(cfg, log, redisrepo.NewCaptchaRepository(rdb, "t"))
	sms := &fakeSMSProvider{codes: map[string][]string{}}
	captcha.RegisterSMSProvider("aliyun", sms)
	users := NewUserService(log, pg, sessions, publisher, security)
	notifications := NewNotificationService(log, pg, sessions, nil)

	mailer := &fakeDeletionMailer{codes: map[string][]string{}, notices: map[string][]AccountDeletionNotice{}}
	deletion := NewAccountDeletionService(log, pg, sessions, cfg.Security.MasterKey)
	deletion.SetAccountServices(auth, users, security)
	deletion.SetVerificationChannels(nil, captcha, app)
	deletion.mailer = mailer
	deletion.SetNotifier(notifications)
	auth.SetAccountDeletion(deletion)

	recovery := &recoveryTestEnv{appID: appID, pool: pool, pg: pg, sessions: sessions, auth: auth, security: security, sms: sms}
	return &deletionTestEnv{appID: appID, pg: pg, sessions: sessions, deletion: deletion, mailer: mailer, recovery: recovery}
}

type fakeDeletionMailer struct {
	mu      sync.Mutex
	codes   map[string][]string
	notices map[string][]AccountDeletionNotice
}

func (m *fakeDeletionMailer) RecoveryMailReady(context.Context, int64) bool { return true }

func (m *fakeDeletionMailer) SendAccountDeletionCode(_ context.Context, _ int64, email, code string, _ int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.codes[email] = append(m.codes[email], code)
	return nil
}

func (m *fakeDeletionMailer) SendAccountDeletionNotice(_ context.Context, _ int64, email string, notice AccountDeletionNotice) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notices[email] = append(m.notices[email], notice)
	return nil
}

func (m *fakeDeletionMailer) last(t *testing.T, email string) string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	codes := m.codes[email]
	if len(codes) == 0 {
		t.Fatalf("没有发往 %s 的验证码", email)
	}
	return codes[len(codes)-1]
}

// lastNotice 等异步告知信落下来，返回最后一封。
func (m *fakeDeletionMailer) lastNotice(email string) AccountDeletionNotice {
	deadline := time.Now().Add(3 * time.Second)
	for {
		m.mu.Lock()
		items := m.notices[email]
		m.mu.Unlock()
		if len(items) > 0 && (items[len(items)-1].Kind == AccountDeletionNoticeCompleted || time.Now().After(deadline)) {
			return items[len(items)-1]
		}
		if time.Now().After(deadline) {
			return AccountDeletionNotice{}
		}
		time.Sleep(50 * time.Millisecond)
	}
}
