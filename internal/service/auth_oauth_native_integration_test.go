package service

import (
	"strings"
	"testing"
	"time"

	"aegis/internal/config"
	authdomain "aegis/internal/domain/auth"
	oauthdomain "aegis/internal/domain/oauth"
	"aegis/internal/event"
	pgrepo "aegis/internal/repository/postgres"
	redisrepo "aegis/internal/repository/redis"

	"github.com/alicebob/miniredis/v2"
	redislib "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"golang.org/x/crypto/bcrypt"
)

// QQ 原生登录的端到端集成测试：真实 Postgres（全部迁移）+ miniredis + 假的 graph.qq.com。
//
// 走一遍「未绑定 → 关联凭证 → 快速注册 → 凭证不可复用 → 再次登录直达 → 已有账号登录后凭证绑定
// （资料不被第三方覆盖）→ 设置里令牌绑定 → 冲突（身份已被占 / 同渠道已绑别的）→ 别的应用的令牌 →
// 老客户端直接建号 → 账号名撞名加后缀 → 渠道关注册 → 应用关注册 → 解绑最后一个登录方式被拒」。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55471/postgres go test ./internal/service -run TestNativeOAuthIntegration
func TestNativeOAuthIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	const appID = int64(32000)
	if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, 'qq-native-test') ON CONFLICT DO NOTHING`, appID); err != nil {
		t.Fatal(err)
	}
	mr := miniredis.RunT(t)
	rdb := redislib.NewClient(&redislib.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })

	cfg := config.Config{}
	cfg.JWT.Secret = "qq-native-test-secret-qq-native-test-secret"
	cfg.JWT.Issuer = "aegis-test"
	cfg.JWT.TTL = time.Hour
	cfg.JWT.RefreshTTL = 24 * time.Hour
	cfg.Security.MasterKey = "qq-native-test-master-key"

	log := zap.NewNop()
	pg := pgrepo.New(pool)
	sessions := redisrepo.NewSessionRepository(rdb, "t")
	app := NewAppService(log, pg, sessions)
	auth := NewAuthService(cfg, log, pg, sessions, event.NewPublisher(&captureJetStream{published: map[string][][]byte{}}), app, nil)
	appOAuth := NewAppOAuthService(log, pg, cfg)
	auth.SetAppOAuthService(appOAuth)
	qq := newFakeQQ()
	auth.http = qq.client()

	saveQQ := func(allowRegister, allowBind bool) {
		t.Helper()
		if _, err := appOAuth.Save(ctx, appID, oauthdomain.SaveInput{
			Provider: "qq", Kind: oauthdomain.KindQQ, DisplayName: "QQ", Enabled: true,
			ClientID: testQQAppID, ClientSecret: "0tgGbjeLo35sQkpf-test",
			RedirectURL: "https://aegis.test/api/auth/oauth2/callback?provider=qq",
			AuthURL:     "https://graph.qq.com/oauth2.0/authorize",
			TokenURL:    "https://graph.qq.com/oauth2.0/token",
			UserInfoURL: qqUserInfoURL,
			AllowLogin:  ptrTo(true), AllowRegister: ptrTo(allowRegister), AllowBind: ptrTo(allowBind),
		}); err != nil {
			t.Fatalf("保存 QQ 渠道失败：%v", err)
		}
	}
	saveQQ(true, true)

	exchange := func(token string, deferRegistration bool) (*authdomain.LoginResult, error) {
		return auth.NativeOAuthLogin(ctx, NativeOAuthLoginInput{
			AppID: appID, Provider: "qq", AccessToken: token,
			DeviceID: "dev-1", Device: "Pixel", IP: "10.0.0.1", UserAgent: "ua",
			AllowRegister: true, DeferRegistration: deferRegistration,
		})
	}
	expectCode := func(name string, err error, want int) {
		t.Helper()
		if errCode(err) != want {
			t.Fatalf("%s：期望错误码 %d，得到 %v", name, want, err)
		}
	}
	passwordUser := func(account, password, nickname, phone string) int64 {
		t.Helper()
		hash, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO users (appid, account, password_hash, enabled) VALUES ($1, $2, $3, TRUE) RETURNING id`,
			appID, account, string(hash)).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `INSERT INTO user_profiles (user_id, nickname, phone, email) VALUES ($1, $2, $3, $4)`,
			id, nickname, phone, account+"@example.com"); err != nil {
			t.Fatal(err)
		}
		return id
	}

	qq.issue("tok-a", testQQAppID, "OPENID_A", "小明")
	qq.issue("tok-b", testQQAppID, "OPENID_B", "爱丽丝的 QQ")
	qq.issue("tok-c", testQQAppID, "OPENID_C", "第三个 QQ")
	qq.issue("tok-other", "100000001", "OPENID_X", "别的应用")

	// ── 未绑定：拿到关联凭证，不签发令牌 ──
	pending, err := exchange("tok-a", true)
	if err != nil {
		t.Fatal(err)
	}
	link := pending.OAuthLink
	if link == nil || pending.AccessToken != "" || pending.AuthenticationState != AuthStateOAuthLinkRequired ||
		link.Ticket == "" || !link.AllowRegister || !link.AllowBind || link.Nickname != "小明" ||
		!strings.HasPrefix(link.Avatar, "http://thirdqq.qlogo.cn/") || link.Provider != "qq" {
		t.Fatalf("未绑定时应返回关联凭证：%+v / %+v", pending, link)
	}

	// ── 快速注册 ──
	registered, err := auth.RegisterWithOAuthLink(ctx, OAuthLinkRegisterInput{AppID: appID, Ticket: link.Ticket, IP: "10.0.0.1", UserAgent: "ua"})
	if err != nil {
		t.Fatalf("快速注册失败：%v", err)
	}
	if registered.AccessToken == "" || registered.Account != "qq_OPENID_A" || registered.Provider != "qq" {
		t.Fatalf("快速注册结果不对：%+v", registered)
	}
	quickUser := registered.UserID
	profile, err := pg.GetUserProfileByUserID(ctx, quickUser)
	if err != nil || profile == nil || profile.Nickname != "小明" || !strings.Contains(profile.Avatar, "OPENID_A") || profile.InviteCode == "" {
		t.Fatalf("快速注册的档案不对：%+v / %v", profile, err)
	}
	if bound, _ := pg.FindOAuthBinding(ctx, appID, "qq", "OPENID_A"); bound != quickUser {
		t.Fatalf("快速注册后应已绑定，得到 %d", bound)
	}
	var unionID string
	if err := pool.QueryRow(ctx, `SELECT COALESCE(union_id,'') FROM oauth_bindings WHERE appid=$1 AND provider_user_id='OPENID_A'`, appID).Scan(&unionID); err != nil || unionID != "UID_OPENID_A" {
		t.Fatalf("unionid 应随绑定落库，得到 %q / %v", unionID, err)
	}

	// 凭证只能用一次
	_, err = auth.RegisterWithOAuthLink(ctx, OAuthLinkRegisterInput{AppID: appID, Ticket: link.Ticket})
	expectCode("凭证复用", err, errCodeOAuthLinkTicketInvalid)
	_, err = auth.RegisterWithOAuthLink(ctx, OAuthLinkRegisterInput{AppID: appID, Ticket: "not-a-ticket"})
	expectCode("伪造凭证", err, errCodeOAuthLinkTicketInvalid)

	// ── 已绑定：直接登录，不再发凭证 ──
	again, err := exchange("tok-a", true)
	if err != nil || again.OAuthLink != nil || again.AccessToken == "" || again.UserID != quickUser {
		t.Fatalf("已绑定时应直接登录同一账号：%+v / %v", again, err)
	}

	// ── 已有账号：登录后凭凭证绑定，资料不被第三方覆盖 ──
	alice := passwordUser("alice", "Alice-Passw0rd!9", "Alice", "13800138000")
	pending, err = exchange("tok-b", true)
	if err != nil || pending.OAuthLink == nil {
		t.Fatalf("爱丽丝的 QQ 未绑定，应返回凭证：%+v / %v", pending, err)
	}
	if _, err := auth.PasswordLogin(ctx, appID, "alice", "Alice-Passw0rd!9", "dev-1", "Pixel", "10.0.0.1", "ua"); err != nil {
		t.Fatalf("爱丽丝密码登录失败：%v", err)
	}
	binding, err := auth.BindOAuthLink(ctx, appID, alice, pending.OAuthLink.Ticket)
	if err != nil || binding.UserID != alice || binding.ProviderUserID != "OPENID_B" {
		t.Fatalf("凭证绑定失败：%+v / %v", binding, err)
	}
	_, err = auth.BindOAuthLink(ctx, appID, alice, pending.OAuthLink.Ticket)
	expectCode("绑定凭证复用", err, errCodeOAuthLinkTicketInvalid)
	viaQQ, err := exchange("tok-b", true)
	if err != nil || viaQQ.UserID != alice || viaQQ.Account != "alice" {
		t.Fatalf("绑定后用 QQ 应登录爱丽丝：%+v / %v", viaQQ, err)
	}
	aliceProfile, _ := pg.GetUserProfileByUserID(ctx, alice)
	if aliceProfile == nil || aliceProfile.Nickname != "Alice" || aliceProfile.Phone != "13800138000" ||
		aliceProfile.Email != "alice@example.com" || !strings.Contains(aliceProfile.Avatar, "OPENID_B") {
		t.Fatalf("QQ 登录不应覆盖已有资料（只补空头像）：%+v", aliceProfile)
	}

	// ── 设置里用令牌绑定 ──
	bob := passwordUser("bob", "Bob-Passw0rd!9", "Bob", "")
	_, err = auth.BindOAuthWithToken(ctx, appID, bob, "qq", "tok-a", "")
	expectCode("QQ 已绑别人", err, errCodeOAuthIdentityTaken)
	_, err = auth.BindOAuthWithToken(ctx, appID, alice, "qq", "tok-c", "")
	expectCode("同渠道已绑别的 QQ", err, errCodeOAuthProviderBound)
	_, err = auth.BindOAuthWithToken(ctx, appID, bob, "qq", "tok-other", "")
	expectCode("别的应用的令牌", err, errCodeOAuthTokenInvalid)
	_, err = auth.BindOAuthWithToken(ctx, appID, bob, "qq", "tok-c", "OPENID_A")
	expectCode("声称的 openid 不符", err, errCodeOAuthTokenInvalid)
	bobBinding, err := auth.BindOAuthWithToken(ctx, appID, bob, "qq", "tok-c", "")
	if err != nil || bobBinding.ProviderUserID != "OPENID_C" || bobBinding.Nickname != "第三个 QQ" {
		t.Fatalf("令牌绑定失败：%+v / %v", bobBinding, err)
	}
	// 同一个 QQ 重复绑定同一账号是幂等的
	if _, err := auth.BindOAuthWithToken(ctx, appID, bob, "qq", "tok-c", ""); err != nil {
		t.Fatalf("重复绑定同一个 QQ 应幂等：%v", err)
	}
	items, err := appOAuth.ListUserBindings(ctx, appID, bob)
	if err != nil || len(items) != 1 || items[0].Provider != "qq" || items[0].DisplayName != "QQ" {
		t.Fatalf("鲍勃的绑定列表不对：%+v / %v", items, err)
	}

	// ── 别的应用签出的令牌换会话 ──
	_, err = exchange("tok-other", true)
	expectCode("别的应用的令牌换会话", err, errCodeOAuthTokenInvalid)

	// ── 老客户端（不带 deferRegistration）：沿用自动建号 ──
	qq.issue("tok-d", testQQAppID, "OPENID_D", "老客户端")
	legacy, err := exchange("tok-d", false)
	if err != nil || legacy.OAuthLink != nil || legacy.AccessToken == "" || legacy.Account != "qq_OPENID_D" {
		t.Fatalf("老客户端应直接建号：%+v / %v", legacy, err)
	}

	// ── 账号名被占：加后缀 ──
	passwordUser("qq_OPENID_F", "Squat-Passw0rd!9", "占位", "")
	qq.issue("tok-f", testQQAppID, "OPENID_F", "撞名")
	pending, err = exchange("tok-f", true)
	if err != nil || pending.OAuthLink == nil {
		t.Fatalf("撞名用户应先拿凭证：%+v / %v", pending, err)
	}
	squatted, err := auth.RegisterWithOAuthLink(ctx, OAuthLinkRegisterInput{AppID: appID, Ticket: pending.OAuthLink.Ticket})
	if err != nil || !strings.HasPrefix(squatted.Account, "qq_OPENID_F_") || squatted.AccessToken == "" {
		t.Fatalf("撞名时应加后缀建号：%+v / %v", squatted, err)
	}

	// ── 渠道关闭自动注册：只能绑定 ──
	saveQQ(false, true)
	qq.issue("tok-e", testQQAppID, "OPENID_E", "关注册")
	pending, err = exchange("tok-e", true)
	if err != nil || pending.OAuthLink == nil || pending.OAuthLink.AllowRegister || !pending.OAuthLink.AllowBind {
		t.Fatalf("关注册时凭证应标明不能注册：%+v / %v", pending, err)
	}
	_, err = auth.RegisterWithOAuthLink(ctx, OAuthLinkRegisterInput{AppID: appID, Ticket: pending.OAuthLink.Ticket})
	expectCode("关注册时快速注册", err, 40393)
	_, err = exchange("tok-e", false)
	expectCode("关注册时老客户端", err, 40393)

	// ── 渠道不开放绑定：没得选，照旧建号 ──
	saveQQ(true, false)
	qq.issue("tok-g", testQQAppID, "OPENID_G", "不开放绑定")
	direct, err := exchange("tok-g", true)
	if err != nil || direct.OAuthLink != nil || direct.AccessToken == "" {
		t.Fatalf("不开放绑定时应直接建号：%+v / %v", direct, err)
	}
	_, err = auth.BindOAuthWithToken(ctx, appID, bob, "qq", "tok-e", "")
	expectCode("不开放绑定时令牌绑定", err, 40392)
	saveQQ(true, true)

	// ── 应用关闭注册：凭证标明不能注册，老客户端被注册开关挡住 ──
	if _, err := pool.Exec(ctx, `UPDATE apps SET register_status = FALSE WHERE id = $1`, appID); err != nil {
		t.Fatal(err)
	}
	mr.FlushAll()
	pending, err = exchange("tok-e", true)
	if err != nil || pending.OAuthLink == nil || pending.OAuthLink.AllowRegister {
		t.Fatalf("应用关注册时凭证应标明不能注册：%+v / %v", pending, err)
	}
	_, err = exchange("tok-e", false)
	expectCode("应用关注册时老客户端", err, 40312)
	if _, err := pool.Exec(ctx, `UPDATE apps SET register_status = TRUE WHERE id = $1`, appID); err != nil {
		t.Fatal(err)
	}
	mr.FlushAll()

	// ── 只靠 QQ 登录的账号不能解绑最后一个登录方式 ──
	if err := appOAuth.Unbind(ctx, appID, quickUser, "qq", false); errCode(err) != 40094 {
		t.Fatalf("无密码账号解绑最后一个第三方应被拒，得到 %v", err)
	}
	if err := appOAuth.Unbind(ctx, appID, bob, "qq", false); err != nil {
		t.Fatalf("有密码的账号应能解绑：%v", err)
	}
	if bound, _ := pg.FindOAuthBinding(ctx, appID, "qq", "OPENID_C"); bound != 0 {
		t.Fatalf("解绑后不应再有绑定，得到 %d", bound)
	}
}
