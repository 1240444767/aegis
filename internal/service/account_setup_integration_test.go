package service

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"aegis/internal/config"
	appdomain "aegis/internal/domain/app"
	authdomain "aegis/internal/domain/auth"
	authprotocol "aegis/internal/domain/authprotocol"
	oauthdomain "aegis/internal/domain/oauth"
	userdomain "aegis/internal/domain/user"
	"aegis/internal/event"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"

	"github.com/alicebob/miniredis/v2"
	redislib "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

// 账号初始化与多标识登录的端到端集成测试：真实 Postgres（全部迁移）+ miniredis + 假的 graph.qq.com。
//
// QQ 快速注册 → 状态（首次弹窗）→ 上报弹窗（横幅）→ 关掉横幅（静默）→ 用户名校验 →
// 密码不合规时用户名不落库 → 一次设好用户名与密码（会话里的账号名同步）→ 不能再自助改名 / 设密码 →
// 用户名、用户 ID、邮箱登录，旧的系统账号名登不上 → 关掉 ID 标识 → 一个邮箱挂两个账号按密码区分 →
// 管理员改名、要求重新设置 → 策略不覆盖时不提醒、策略校验。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55498/postgres go test ./internal/service -run TestAccountSetupIntegration
func TestAccountSetupIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	const appID = int64(32100)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'account-setup-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	mr := miniredis.RunT(t)
	rdb := redislib.NewClient(&redislib.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	cfg := config.Config{}
	cfg.JWT.Secret = "account-setup-test-secret-account-setup"
	cfg.JWT.Issuer = "aegis-test"
	cfg.JWT.TTL = time.Hour
	cfg.JWT.RefreshTTL = 24 * time.Hour
	cfg.Security.MasterKey = "account-setup-test-master-key"

	log := zap.NewNop()
	pg := pgrepo.New(pool)
	sessions := redisrepo.NewSessionRepository(rdb, "t")
	app := NewAppService(log, pg, sessions)
	auth := NewAuthService(cfg, log, pg, sessions, event.NewPublisher(&captureJetStream{published: map[string][][]byte{}}), app, nil)
	appOAuth := NewAppOAuthService(log, pg, cfg)
	auth.SetAppOAuthService(appOAuth)
	protocol := NewAuthProtocolService(log, pg, nil, "account-setup-root")
	qq := newFakeQQ()
	auth.http = qq.client()
	if _, err := appOAuth.Save(ctx, appID, oauthdomain.SaveInput{
		Provider: "qq", Kind: oauthdomain.KindQQ, DisplayName: "QQ", Enabled: true,
		ClientID: testQQAppID, ClientSecret: "0tgGbjeLo35sQkpf-test",
		RedirectURL: "https://aegis.test/api/auth/oauth2/callback?provider=qq",
		AuthURL:     "https://graph.qq.com/oauth2.0/authorize",
		TokenURL:    "https://graph.qq.com/oauth2.0/token",
		UserInfoURL: qqUserInfoURL,
		AllowLogin:  ptrTo(true), AllowRegister: ptrTo(true), AllowBind: ptrTo(true),
	}); err != nil {
		t.Fatalf("保存 QQ 渠道失败：%v", err)
	}
	expectCode := func(name string, err error, want int) {
		t.Helper()
		if errCode(err) != want {
			t.Fatalf("%s：期望错误码 %d，得到 %v", name, want, err)
		}
	}
	sessionOf := func(result *authdomain.LoginResult) *authdomain.Session {
		t.Helper()
		session, err := auth.ValidateAccessToken(ctx, result.AccessToken)
		if err != nil {
			t.Fatal(err)
		}
		return session
	}
	passwordUser := func(account, password, email string) int64 {
		t.Helper()
		hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO users (appid, account, password_hash, enabled) VALUES ($1, $2, $3, TRUE) RETURNING id`,
			appID, account, string(hash)).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO user_profiles (user_id, nickname, email) VALUES ($1, $2, $3)`,
			id, account, email); err != nil {
			t.Fatal(err)
		}
		return id
	}
	login := func(identifier, password string) (*authdomain.LoginResult, error) {
		return auth.PasswordLogin(ctx, appID, identifier, password, "dev-2", "Pixel", "10.0.0.2", "ua")
	}

	// ── QQ 快速注册：登记初始化，首次弹窗 ──
	qq.issue("tok-a", testQQAppID, "OPENID_A", "小明")
	registered, err := auth.NativeOAuthLogin(ctx, NativeOAuthLoginInput{
		AppID: appID, Provider: "qq", AccessToken: "tok-a",
		DeviceID: "dev-1", Device: "Pixel", IP: "10.0.0.1", UserAgent: "ua", AllowRegister: true,
	})
	if err != nil || registered.AccessToken == "" {
		t.Fatalf("QQ 快速注册失败：%v / %+v", err, registered)
	}
	session := sessionOf(registered)
	status, err := auth.AccountSetupStatus(ctx, session)
	if err != nil {
		t.Fatal(err)
	}
	if !status.Required || !status.AccountRequired || !status.PasswordRequired ||
		status.Prompt != userdomain.AccountSetupPromptDialog || status.Source != "oauth" || status.Provider != "qq" ||
		!strings.HasPrefix(status.Account, "qq_") || !status.Dismissible || len(status.Suggestions) == 0 {
		t.Fatalf("快速注册后应首次弹窗：%+v", status)
	}
	for _, suggestion := range status.Suggestions {
		if userdomain.ValidateUsername(suggestion) != nil || !strings.HasPrefix(suggestion, "小明") {
			t.Fatalf("建议的用户名应由昵称派生且合规：%q", suggestion)
		}
	}

	// ── 弹过一次窗之后只出横幅；关掉横幅后静默 ──
	status, err = auth.MarkAccountSetupPrompted(ctx, session)
	if err != nil || status.Prompt != userdomain.AccountSetupPromptBanner {
		t.Fatalf("上报弹窗后应出横幅：%v / %+v", err, status)
	}
	status, err = auth.DismissAccountSetup(ctx, session)
	if err != nil || status.Prompt != userdomain.AccountSetupPromptNone || status.SnoozedUntil == nil || !status.Required {
		t.Fatalf("关掉横幅后应静默但仍待设置：%v / %+v", err, status)
	}
	// 静默期过了重新出横幅（不会再弹窗）
	if _, err := pool.Exec(ctx, `UPDATE user_account_setup SET snoozed_until = NOW() - INTERVAL '1 minute' WHERE user_id = $1`, session.UserID); err != nil {
		t.Fatal(err)
	}
	if status, _ = auth.AccountSetupStatus(ctx, session); status.Prompt != userdomain.AccountSetupPromptBanner {
		t.Fatalf("静默期过后应重新出横幅：%+v", status)
	}

	// ── 用户名校验 ──
	aliceID := passwordUser("alice", "Alice#Pass2026", "shared@example.com")
	for name, want := range map[string]bool{"1234": false, "admin": false, "a@b.cn": false, "Alice": false, "ab": false, "ming_dev": true} {
		check, err := auth.CheckUsername(ctx, session, name)
		if err != nil || check.Available != want || (!want && check.Message == "") {
			t.Fatalf("检查用户名 %q：期望 %v，得到 %v / %+v", name, want, err, check)
		}
	}

	// ── 密码不合规：两项都不落库 ──
	_, err = auth.CompleteAccountSetup(ctx, session, AccountSetupInput{Account: "ming_dev", Password: "123"})
	if err == nil {
		t.Fatal("弱密码应被拒")
	}
	if status, _ = auth.AccountSetupStatus(ctx, session); !status.AccountRequired || status.Account == "ming_dev" {
		t.Fatalf("密码被拒时用户名不应改动：%+v", status)
	}
	_, err = auth.CompleteAccountSetup(ctx, session, AccountSetupInput{Account: "Alice"})
	expectCode("用户名忽略大小写撞名", err, errCodeUsernameTaken)

	// ── 一次设好 ──
	result, err := auth.CompleteAccountSetup(ctx, session, AccountSetupInput{Account: "ming_dev", Password: "Voyage#2026xq"})
	if err != nil {
		t.Fatalf("完成初始化失败：%v", err)
	}
	if !result.AccountChanged || !result.PasswordSet || result.Account != "ming_dev" || result.Status.Required ||
		result.Status.Prompt != userdomain.AccountSetupPromptNone || result.Status.CompletedAt == nil {
		t.Fatalf("完成后应不再提醒：%+v / %+v", result, result.Status)
	}
	if refreshed := sessionOf(registered); refreshed.Account != "ming_dev" {
		t.Fatalf("已签发会话里的账号名应同步：%q", refreshed.Account)
	}
	_, err = auth.CompleteAccountSetup(ctx, session, AccountSetupInput{Account: "ming_again"})
	expectCode("自助改名只能一次", err, errCodeAccountSetupDone)
	_, err = auth.CompleteAccountSetup(ctx, session, AccountSetupInput{Password: "Another#2026xq"})
	expectCode("已有密码不能再设", err, errCodePasswordAlreadySet)

	// ── 多标识登录 ──
	if _, err := login("ming_dev", "Voyage#2026xq"); err != nil {
		t.Fatalf("用户名登录失败：%v", err)
	}
	var publicID int64
	if err := pool.QueryRow(ctx, `SELECT public_id FROM users WHERE id = $1`, session.UserID).Scan(&publicID); err != nil {
		t.Fatal(err)
	}
	if _, err := login(strconv.FormatInt(publicID, 10), "Voyage#2026xq"); err != nil {
		t.Fatalf("用户 ID 登录失败：%v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE user_profiles SET email = 'Ming@Example.com' WHERE user_id = $1`, session.UserID); err != nil {
		t.Fatal(err)
	}
	if _, err := login("ming@example.com", "Voyage#2026xq"); err != nil {
		t.Fatalf("邮箱登录（忽略大小写）失败：%v", err)
	}
	_, err = login("qq_OPENID_A", "Voyage#2026xq")
	expectCode("旧的系统账号名", err, 40101)
	_, err = login(strconv.FormatInt(publicID, 10), "wrong-password")
	expectCode("用户 ID + 错误密码", err, 40101)

	// 接入策略关掉 ID 标识
	if _, err := protocol.UpdatePolicy(ctx, appID, authprotocol.PolicyPatch{Identifiers: []string{"username", "email"}}); err != nil {
		t.Fatal(err)
	}
	_, err = login(strconv.FormatInt(publicID, 10), "Voyage#2026xq")
	expectCode("关掉 ID 标识后", err, 40101)

	// 一个邮箱挂两个账号：密码决定是哪一个
	bobID := passwordUser("bob", "Bob#Pass2026", "shared@example.com")
	for password, wantID := range map[string]int64{"Alice#Pass2026": aliceID, "Bob#Pass2026": bobID} {
		res, err := login("shared@example.com", password)
		if err != nil || res.UserID != wantID {
			t.Fatalf("共享邮箱按密码区分：期望 %d，得到 %v / %+v", wantID, err, res)
		}
	}
	_, err = login("shared@example.com", "nobody")
	expectCode("共享邮箱 + 错误密码", err, 40101)

	// ── 管理员改名、要求重新设置 ──
	_, err = auth.AdminRenameUser(ctx, appID, session.UserID, "bob")
	expectCode("管理员改名撞名", err, errCodeUsernameTaken)
	renamed, err := auth.AdminRenameUser(ctx, appID, session.UserID, "ming.admin")
	if err != nil || renamed.Account != "ming.admin" {
		t.Fatalf("管理员改名失败：%v", err)
	}
	if refreshed := sessionOf(registered); refreshed.Account != "ming.admin" {
		t.Fatalf("管理员改名后会话账号名应同步：%q", refreshed.Account)
	}
	view, err := auth.AdminResetAccountSetup(ctx, appID, session.UserID, true)
	if err != nil || !view.Tracked || !view.AccountPending || !view.Required || view.CompletedAt != nil {
		t.Fatalf("要求重新设置后应待设置：%v / %+v", err, view)
	}
	if status, _ = auth.AccountSetupStatus(ctx, session); status.Prompt != userdomain.AccountSetupPromptDialog || !status.AccountRequired || status.PasswordRequired {
		t.Fatalf("重新设置应从首次弹窗开始、只缺用户名：%+v", status)
	}
	// 沿用当前名字：名字不变，只记为已设置
	result, err = auth.CompleteAccountSetup(ctx, session, AccountSetupInput{Account: "ming.admin"})
	if err != nil || result.AccountChanged || result.Status.Required {
		t.Fatalf("沿用当前用户名失败：%v / %+v", err, result)
	}

	// ── 策略 ──
	qq.issue("tok-b", testQQAppID, "OPENID_B", "")
	second, err := auth.NativeOAuthLogin(ctx, NativeOAuthLoginInput{
		AppID: appID, Provider: "qq", AccessToken: "tok-b",
		DeviceID: "dev-3", Device: "Pixel", IP: "10.0.0.3", UserAgent: "ua", AllowRegister: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	secondSession := sessionOf(second)
	if status, _ = auth.AccountSetupStatus(ctx, secondSession); len(status.Suggestions) == 0 || !strings.HasPrefix(status.Suggestions[0], "user_") {
		t.Fatalf("没有昵称时建议应退回 user_ 前缀：%+v", status.Suggestions)
	}
	_, err = auth.SaveAccountSetupPolicy(ctx, appID, appdomain.AccountSetupPolicy{Enabled: true})
	expectCode("开启却没选建号方式", err, errCodeAccountSetupPolicy)
	policyView, err := auth.SaveAccountSetupPolicy(ctx, appID, appdomain.AccountSetupPolicy{
		Enabled: true, Sources: []string{"sms", "cardkey"}, RequireAccount: true, RequirePassword: true,
		FirstPrompt: "banner", SnoozeHours: 0,
	})
	if err != nil || policyView.Policy.IsDefault || policyView.Stats.Tracked != 2 || policyView.Stats.Completed != 1 {
		t.Fatalf("保存策略失败：%v / %+v / %+v", err, policyView, policyView.Stats)
	}
	if status, _ = auth.AccountSetupStatus(ctx, secondSession); status.Required || status.Prompt != userdomain.AccountSetupPromptNone {
		t.Fatalf("策略不覆盖第三方时不应提醒：%+v", status)
	}
	if _, err := auth.SaveAccountSetupPolicy(ctx, appID, appdomain.AccountSetupPolicy{
		Enabled: true, Sources: []string{"oauth"}, RequireAccount: false, RequirePassword: true,
		FirstPrompt: "banner", SnoozeHours: 0,
	}); err != nil {
		t.Fatal(err)
	}
	status, _ = auth.AccountSetupStatus(ctx, secondSession)
	if !status.Required || status.AccountRequired || !status.PasswordRequired || status.Prompt != userdomain.AccountSetupPromptBanner || status.Dismissible {
		t.Fatalf("只要求密码、一律横幅、不许关闭：%+v", status)
	}
	if status, _ = auth.DismissAccountSetup(ctx, secondSession); status.Prompt != userdomain.AccountSetupPromptBanner {
		t.Fatalf("不许关闭时关横幅应无效：%+v", status)
	}
}
