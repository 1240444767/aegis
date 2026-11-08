package service

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"aegis/internal/config"
	"aegis/internal/event"
	captchadomain "aegis/internal/domain/captcha"
	securitydomain "aegis/internal/domain/security"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"

	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	redislib "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

// 找回密码的端到端集成测试：真实 Postgres（全部迁移）+ miniredis，发信与短信换成记录验证码的假实现。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestPasswordRecoveryIntegration
func TestPasswordRecoveryIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	env := newRecoveryTestEnv(t, ctx, pool)
	const oldPassword = "Old-Passw0rd!2024"
	const newPassword = "Brand-New#Secret9"

	// ── 邮箱 + 手机、没开两步验证：两项都要验 ──
	alice := env.user(t, "alice", oldPassword, "Alice@Example.com", "13800138000")
	login, err := env.auth.PasswordLogin(ctx, env.appID, "alice", oldPassword, "dev-other", "旧手机", "10.0.0.2", "ua")
	if err != nil || login.RefreshToken == "" {
		t.Fatalf("准备一个其他设备上的登录失败：%+v / %v", login, err)
	}

	start, err := env.auth.StartPasswordRecovery(ctx, PasswordRecoveryStartInput{
		AppID: env.appID, Identifier: "alice@example.com", DeviceID: "dev-1", IP: "10.0.0.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if start.FlowToken == "" || start.Account != "a***e" || start.Ready || len(start.Requirements) != 2 {
		t.Fatalf("发起结果不对：%+v", start)
	}
	for _, ch := range start.Channels {
		if strings.Contains(ch.Masked, "alice") || strings.Contains(ch.Masked, "0013") {
			t.Fatalf("联系方式没有打码：%+v", ch)
		}
	}
	step := PasswordRecoveryStepInput{AppID: env.appID, FlowToken: start.FlowToken, DeviceID: "dev-1", IP: "10.0.0.1"}

	// 换设备继续被拒
	other := step
	other.DeviceID = "dev-x"
	if _, err := env.auth.PasswordRecoveryStatus(ctx, other); errCode(err) != errCodeRecoveryDeviceMismatch {
		t.Fatalf("换设备应被拒，得到 %v", err)
	}
	// 未登录发码要填完整联系方式，对不上算失败
	if _, err := env.auth.SendPasswordRecoveryCode(ctx, step, "email", ""); errCode(err) != errCodeRecoveryTargetMismatch {
		t.Fatalf("不填联系方式应被拒，得到 %v", err)
	}
	if _, err := env.auth.SendPasswordRecoveryCode(ctx, step, "email", "alice@example.org"); errCode(err) != errCodeRecoveryTargetMismatch {
		t.Fatalf("填错联系方式应被拒，得到 %v", err)
	}
	if len(env.mailer.codes) != 0 {
		t.Fatal("联系方式对不上时不应发出验证码")
	}
	if _, err := env.auth.SendPasswordRecoveryCode(ctx, step, "email", "ALICE@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := env.auth.SendPasswordRecoveryCode(ctx, step, "email", "alice@example.com"); errCode(err) != errCodeRecoveryResendTooSoon {
		t.Fatalf("一分钟内重发应被拒，得到 %v", err)
	}
	emailCode := env.mailer.last(t, "Alice@Example.com")

	// 设新密码之前必须验完
	if _, err := env.auth.CompletePasswordRecovery(ctx, PasswordRecoveryCompleteInput{PasswordRecoveryStepInput: step, NewPassword: newPassword}); errCode(err) != errCodeRecoveryNotVerified {
		t.Fatalf("未验证完成就设密码应被拒，得到 %v", err)
	}
	if _, err := env.auth.VerifyPasswordRecoveryFactor(ctx, step, "email", wrongCode(emailCode)); errCode(err) != errCodeRecoveryCodeInvalid {
		t.Fatalf("错误的邮箱验证码应被拒，得到 %v", err)
	}
	// 动态码不在这个账号的要求里
	if _, err := env.auth.VerifyPasswordRecoveryFactor(ctx, step, "totp", "123456"); errCode(err) != errCodeRecoveryFactorInvalid {
		t.Fatalf("不在要求里的方式应被拒，得到 %v", err)
	}
	view, err := env.auth.VerifyPasswordRecoveryFactor(ctx, step, "email", emailCode)
	if err != nil || view.Ready {
		t.Fatalf("邮箱验证后还差手机：%+v / %v", view, err)
	}
	if view.AttemptsLeft != passwordRecoveryMaxFailures-3 {
		t.Fatalf("前面错了三次（两次联系方式、一次验证码），剩余次数应为 %d，得到 %d", passwordRecoveryMaxFailures-3, view.AttemptsLeft)
	}
	if _, err := env.auth.SendPasswordRecoveryCode(ctx, step, "phone", "+86 138-0013-8000"); err != nil {
		t.Fatal(err)
	}
	view, err = env.auth.VerifyPasswordRecoveryFactor(ctx, step, "phone", env.sms.last(t, "13800138000"))
	if err != nil || !view.Ready {
		t.Fatalf("两项都验过后应可以设密码：%+v / %v", view, err)
	}

	// 密码不合格时流程保留
	if _, err := env.auth.CompletePasswordRecovery(ctx, PasswordRecoveryCompleteInput{PasswordRecoveryStepInput: step, NewPassword: oldPassword}); errCode(err) != 40006 {
		t.Fatalf("新旧密码相同应被拒，得到 %v", err)
	}
	done, err := env.auth.CompletePasswordRecovery(ctx, PasswordRecoveryCompleteInput{
		PasswordRecoveryStepInput: step, NewPassword: newPassword, Device: "新手机", Location: "上海",
	})
	if err != nil {
		t.Fatal(err)
	}
	if done.Account != "alice" || done.SignedOut < 1 || done.KeptCurrentSession {
		t.Fatalf("完成结果不对：%+v", done)
	}
	// 流程用完即作废
	if _, err := env.auth.CompletePasswordRecovery(ctx, PasswordRecoveryCompleteInput{PasswordRecoveryStepInput: step, NewPassword: newPassword + "x"}); errCode(err) != errCodeRecoveryFlowInvalid {
		t.Fatalf("流程应已作废，得到 %v", err)
	}
	// 其他设备的刷新令牌不能再换出新令牌
	if _, err := env.auth.Refresh(ctx, login.RefreshToken, "dev-other", "10.0.0.2", "ua"); err == nil {
		t.Fatal("找回之后旧的刷新令牌必须失效")
	}
	if _, err := env.auth.ValidateAccessToken(ctx, login.AccessToken); err == nil {
		t.Fatal("找回之后旧的访问令牌必须失效")
	}
	if _, err := env.auth.PasswordLogin(ctx, env.appID, "alice", oldPassword, "dev-1", "", "10.0.0.1", "ua"); err == nil {
		t.Fatal("旧密码不应再能登录")
	}
	if _, err := env.auth.PasswordLogin(ctx, env.appID, "alice", newPassword, "dev-1", "", "10.0.0.1", "ua"); err != nil {
		t.Fatalf("新密码应能登录：%v", err)
	}
	env.waitNotification(t, ctx, alice, "密码已重置")
	if notice := env.mailer.waitNotice(t, "Alice@Example.com"); notice.Account != "alice" || notice.Location != "上海" {
		t.Fatalf("告知信内容不对：%+v", notice)
	}

	// ── 邮箱 + 两步验证：动态码或恢复码 ──
	bob := env.user(t, "bob", oldPassword, "bob@example.com", "")
	secret := env.enableTOTP(t, ctx, bob)
	start, err = env.auth.StartPasswordRecovery(ctx, PasswordRecoveryStartInput{AppID: env.appID, Identifier: "bob", DeviceID: "dev-b"})
	if err != nil || !start.TOTP || len(start.Requirements) != 2 {
		t.Fatalf("两步验证账号的要求不对：%+v / %v", start, err)
	}
	step = PasswordRecoveryStepInput{AppID: env.appID, FlowToken: start.FlowToken, DeviceID: "dev-b"}
	if _, err := env.auth.VerifyPasswordRecoveryFactor(ctx, step, "totp", "000000"); errCode(err) != 40109 {
		t.Fatalf("错误的动态码应被拒，得到 %v", err)
	}
	code, _ := totp.GenerateCodeCustom(secret, time.Now().UTC(), totp.ValidateOpts{Period: 30, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
	if view, err = env.auth.VerifyPasswordRecoveryFactor(ctx, step, "totp", code); err != nil || view.Ready {
		t.Fatalf("动态码通过后还差邮箱：%+v / %v", view, err)
	}
	if _, err := env.auth.SendPasswordRecoveryCode(ctx, step, "email", "bob@example.com"); err != nil {
		t.Fatal(err)
	}
	if view, err = env.auth.VerifyPasswordRecoveryFactor(ctx, step, "email", env.mailer.last(t, "bob@example.com")); err != nil || !view.Ready {
		t.Fatalf("邮箱 + 动态码之后应可设密码：%+v / %v", view, err)
	}
	// 恢复码同样能顶替动态码
	if err := env.pg.ReplaceUserRecoveryCodes(ctx, env.appID, bob, []securitydomain.RecoveryCodeRecord{
		{UserID: bob, AppID: env.appID, CodeHash: hashRecoveryCode("AAAA-BBBB"), CodeHint: "BBBB", CreatedAt: time.Now()},
	}); err != nil {
		t.Fatal(err)
	}
	start, _ = env.auth.StartPasswordRecovery(ctx, PasswordRecoveryStartInput{AppID: env.appID, Identifier: "bob", DeviceID: "dev-b"})
	step.FlowToken = start.FlowToken
	if view, err = env.auth.VerifyPasswordRecoveryFactor(ctx, step, "recovery", "AAAA-BBBB"); err != nil || view.Requirements[1].By != "recovery" {
		t.Fatalf("恢复码应满足两步验证那一项：%+v / %v", view, err)
	}

	// ── 已登录发起：保留当前会话，其余下线 ──
	carol := env.user(t, "carol", oldPassword, "", "13900139000")
	current, err := env.auth.PasswordLogin(ctx, env.appID, "carol", oldPassword, "dev-c", "", "10.0.0.3", "ua")
	if err != nil {
		t.Fatal(err)
	}
	elsewhere, err := env.auth.PasswordLogin(ctx, env.appID, "carol", oldPassword, "dev-d", "", "10.0.0.4", "ua")
	if err != nil {
		t.Fatal(err)
	}
	session, err := env.auth.ValidateAccessToken(ctx, current.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	start, err = env.auth.StartSessionPasswordRecovery(ctx, session, "dev-c", "10.0.0.3")
	if err != nil || start.Mode != "session" || start.Account != "carol" {
		t.Fatalf("已登录发起结果不对：%+v / %v", start, err)
	}
	step = PasswordRecoveryStepInput{AppID: env.appID, FlowToken: start.FlowToken, DeviceID: "dev-c"}
	// 已登录时不必再填完整手机号
	if _, err := env.auth.SendPasswordRecoveryCode(ctx, step, "phone", ""); err != nil {
		t.Fatal(err)
	}
	if view, err = env.auth.VerifyPasswordRecoveryFactor(ctx, step, "phone", env.sms.last(t, "13900139000")); err != nil || !view.Ready {
		t.Fatalf("已登录只需一项：%+v / %v", view, err)
	}
	done, err = env.auth.CompletePasswordRecovery(ctx, PasswordRecoveryCompleteInput{PasswordRecoveryStepInput: step, NewPassword: newPassword})
	if err != nil || !done.KeptCurrentSession || done.SignedOut != 1 {
		t.Fatalf("应只下线另一台设备：%+v / %v", done, err)
	}
	if _, err := env.auth.ValidateAccessToken(ctx, current.AccessToken); err != nil {
		t.Fatalf("当前会话应保留：%v", err)
	}
	if _, err := env.auth.Refresh(ctx, current.RefreshToken, "dev-c", "10.0.0.3", "ua"); err != nil {
		t.Fatalf("当前会话的刷新令牌应保留：%v", err)
	}
	if _, err := env.auth.Refresh(ctx, elsewhere.RefreshToken, "dev-d", "10.0.0.4", "ua"); err == nil {
		t.Fatal("另一台设备的刷新令牌必须失效")
	}
	_ = carol

	// ── 刚换绑的联系方式不算数 ──
	dave := env.user(t, "dave", oldPassword, "dave@example.com", "")
	if err := env.sessions.MarkContactChanged(ctx, env.appID, dave, "email", ContactChangeCooldown); err != nil {
		t.Fatal(err)
	}
	if _, err := env.auth.StartPasswordRecovery(ctx, PasswordRecoveryStartInput{AppID: env.appID, Identifier: "dave"}); errCode(err) != errCodeRecoveryUnavailable ||
		!strings.Contains(err.Error(), "72 小时") {
		t.Fatalf("冷静期内应拒绝并说明还要等多久，得到 %v", err)
	}

	// ── 错满次数即作废 ──
	erin := env.user(t, "erin", oldPassword, "erin@example.com", "")
	start, _ = env.auth.StartPasswordRecovery(ctx, PasswordRecoveryStartInput{AppID: env.appID, Identifier: "erin"})
	step = PasswordRecoveryStepInput{AppID: env.appID, FlowToken: start.FlowToken}
	var lastErr error
	for i := 0; i < passwordRecoveryMaxFailures; i++ {
		_, lastErr = env.auth.SendPasswordRecoveryCode(ctx, step, "email", "nobody@example.com")
	}
	if errCode(lastErr) != errCodeRecoveryTooManyFailure {
		t.Fatalf("错满次数应作废流程，得到 %v", lastErr)
	}
	if _, err := env.auth.PasswordRecoveryStatus(ctx, step); errCode(err) != errCodeRecoveryFlowInvalid {
		t.Fatalf("作废后流程应不存在，得到 %v", err)
	}
	_ = erin

	// ── 一个邮箱挂在两个账号上：不替用户挑 ──
	env.user(t, "frank1", oldPassword, "shared@example.com", "")
	env.user(t, "frank2", oldPassword, "shared@example.com", "")
	if _, err := env.auth.StartPasswordRecovery(ctx, PasswordRecoveryStartInput{AppID: env.appID, Identifier: "shared@example.com"}); errCode(err) != 40986 {
		t.Fatalf("共用邮箱应要求改填账号名，得到 %v", err)
	}
	if _, err := env.auth.StartPasswordRecovery(ctx, PasswordRecoveryStartInput{AppID: env.appID, Identifier: "nobody-here"}); errCode(err) != errCodeRecoveryAccountMissing {
		t.Fatalf("不存在的账号应报未找到，得到 %v", err)
	}
}

type recoveryTestEnv struct {
	appID    int64
	pool     *pgxpool.Pool
	pg       *pgrepo.Repository
	sessions *redisrepo.SessionRepository
	auth     *AuthService
	security *SecurityService
	mailer   *fakeRecoveryMailer
	sms      *fakeSMSProvider
}

func newRecoveryTestEnv(t *testing.T, ctx context.Context, pool *pgxpool.Pool) *recoveryTestEnv {
	t.Helper()
	const appID = int64(31000)
	settings, _ := json.Marshal(map[string]any{
		"captcha": map[string]any{"sms": map[string]any{"provider": "aliyun", "signName": "测试", "templateId": "SMS_1"}},
	})
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name, settings) VALUES ($1, 'recovery-test', $2) ON CONFLICT DO NOTHING`, appID, settings); err != nil {
		t.Fatal(err)
	}
	mr := miniredis.RunT(t)
	rdb := redislib.NewClient(&redislib.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	cfg := config.Config{}
	cfg.JWT.Secret = "recovery-test-secret-recovery-test-secret"
	cfg.JWT.Issuer = "aegis-test"
	cfg.JWT.TTL = time.Hour
	cfg.JWT.RefreshTTL = 24 * time.Hour
	cfg.Security.MasterKey = "recovery-test-master-key"
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
	// 「密码已重置」站内信经统一投递
	auth.SetNotificationService(NewNotificationService(log, pg, sessions, nil))
	captcha := NewCaptchaService(cfg, log, redisrepo.NewCaptchaRepository(rdb, "t"))
	sms := &fakeSMSProvider{codes: map[string][]string{}}
	captcha.RegisterSMSProvider("aliyun", sms)
	mailer := &fakeRecoveryMailer{codes: map[string][]string{}, notices: map[string]PasswordResetNotice{}}
	auth.SetPasswordRecovery(nil, captcha, nil)
	auth.recoveryEmail = mailer
	return &recoveryTestEnv{appID: appID, pool: pool, pg: pg, sessions: sessions, auth: auth, security: security, mailer: mailer, sms: sms}
}

func (e *recoveryTestEnv) user(t *testing.T, account, password, email, phone string) int64 {
	t.Helper()
	ctx := context.Background()
	hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	var id int64
	if err := e.pool.QueryRow(ctx, `INSERT INTO users (appid, account, password_hash, enabled) VALUES ($1, $2, $3, TRUE) RETURNING id`,
		e.appID, account, string(hash)).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx, `INSERT INTO user_profiles (user_id, email, phone) VALUES ($1, $2, $3)`,
		id, email, phone); err != nil {
		t.Fatal(err)
	}
	return id
}

func (e *recoveryTestEnv) enableTOTP(t *testing.T, ctx context.Context, userID int64) string {
	t.Helper()
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "aegis-test", AccountName: "bob"})
	if err != nil {
		t.Fatal(err)
	}
	cipher, err := encryptSecret(e.security.currentEncryptionKey(), key.Secret())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := e.pg.UpsertUserTOTPSecret(ctx, securitydomain.TOTPSecretRecord{
		UserID: userID, AppID: e.appID, SecretCipher: cipher, Issuer: "aegis-test", AccountName: "bob",
		Enabled: true, EnabledAt: &now, CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	return key.Secret()
}

func (e *recoveryTestEnv) waitNotification(t *testing.T, ctx context.Context, userID int64, title string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		_ = e.pool.QueryRow(ctx, `SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND title = $2`, userID, title).Scan(&n)
		if n > 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("没有收到站内信「%s」", title)
}

// wrongCode 换掉最后一位，保证与真码不同。
func wrongCode(code string) string {
	last := code[len(code)-1]
	if last == '9' {
		return code[:len(code)-1] + "0"
	}
	return code[:len(code)-1] + string(last+1)
}

type fakeRecoveryMailer struct {
	mu      sync.Mutex
	codes   map[string][]string
	notices map[string]PasswordResetNotice
}

func (m *fakeRecoveryMailer) RecoveryMailReady(context.Context, int64) bool { return true }

func (m *fakeRecoveryMailer) SendRecoveryCode(_ context.Context, _ int64, email, code string, _ int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.codes[email] = append(m.codes[email], code)
	return nil
}

func (m *fakeRecoveryMailer) SendPasswordResetNotice(_ context.Context, _ int64, email string, notice PasswordResetNotice) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notices[email] = notice
	return nil
}

func (m *fakeRecoveryMailer) last(t *testing.T, email string) string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	codes := m.codes[email]
	if len(codes) == 0 {
		t.Fatalf("没有发往 %s 的验证码", email)
	}
	return codes[len(codes)-1]
}

func (m *fakeRecoveryMailer) waitNotice(t *testing.T, email string) PasswordResetNotice {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		notice, ok := m.notices[email]
		m.mu.Unlock()
		if ok {
			return notice
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("没有寄往 %s 的告知信", email)
	return PasswordResetNotice{}
}

type fakeSMSProvider struct {
	mu    sync.Mutex
	codes map[string][]string
}

func (p *fakeSMSProvider) Send(_ context.Context, phone, code string, _ *captchadomain.SMSProviderConfig) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.codes[phone] = append(p.codes[phone], code)
	return "req-1", nil
}

func (p *fakeSMSProvider) last(t *testing.T, phone string) string {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	codes := p.codes[phone]
	if len(codes) == 0 {
		t.Fatalf("没有发往 %s 的短信", phone)
	}
	return codes[len(codes)-1]
}
