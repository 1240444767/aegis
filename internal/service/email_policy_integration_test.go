package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	emaildomain "aegis/internal/domain/email"
	userdomain "aegis/internal/domain/user"
	pgrepo "aegis/internal/repository/postgres"

	"github.com/alicebob/miniredis/v2"
	redislib "github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// 邮件系统的端到端集成测试：真实 Postgres（全部迁移）+ miniredis，发送器换成记录信件的假实现。
//
// AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestEmailPolicyIntegration
func TestEmailPolicyIntegration(t *testing.T) {
	ctx, pool := openTestDatabase(t)
	pg := pgrepo.New(pool)
	mr := miniredis.RunT(t)
	rdb := redislib.NewClient(&redislib.Options{Addr: mr.Addr()})
	svc := NewEmailService(zap.NewNop(), pg, rdb, "test", "master-key")
	outbox := &fakeMailOutbox{}
	svc.senders[emaildomain.ProviderSMTP] = outbox

	const appID, otherAppID int64 = 9101, 9102
	for _, id := range []int64{appID, otherAppID} {
		if _, err := pool.Exec(ctx, `INSERT INTO apps (id, name) VALUES ($1, $2) ON CONFLICT DO NOTHING`, id, fmt.Sprintf("星河%d", id)); err != nil {
			t.Fatal(err)
		}
	}
	saveChannel := func(scope int64, name string, shared bool) {
		t.Helper()
		if _, err := svc.Save(ctx, emaildomain.ConfigMutation{
			AppID: scope, Name: &name, Provider: ptrTo(emaildomain.ProviderSMTP), Enabled: ptrTo(true),
			IsDefault: ptrTo(true), Shared: ptrTo(shared),
			Settings: map[string]string{emaildomain.KeyFromAddress: "noreply@example.com"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	saveChannel(appID, "default", false)

	// ── 验证码：只存摘要，错满次数作废 ──
	if _, err := svc.SendCode(ctx, CodeRequest{AppID: appID, Email: "Alice@Example.com", Purpose: "login", IP: "10.0.0.1"}); err != nil {
		t.Fatal(err)
	}
	mail := outbox.last(t)
	code := regexp.MustCompile(`\b\d{6}\b`).FindString(mail.Subject)
	if code == "" || !strings.Contains(mail.Text, code) {
		t.Fatalf("验证码邮件里找不到 6 位码：%q", mail.Subject)
	}
	stored, _ := mr.Get(fmt.Sprintf("test:email:code:%d:login:alice@example.com", appID))
	if !strings.HasPrefix(stored, secretDigestPrefix) || strings.Contains(stored, code) {
		t.Fatalf("Redis 里应只有摘要，实际 %q", stored)
	}
	for i := 0; i < 4; i++ {
		if ok, err := svc.VerifyCode(ctx, appID, "alice@example.com", "000000", "login"); ok || err != nil {
			t.Fatalf("第 %d 次错码应返回 false 且无错误：%v %v", i+1, ok, err)
		}
	}
	if _, err := svc.VerifyCode(ctx, appID, "alice@example.com", "000000", "login"); errCode(err) != 42964 {
		t.Fatalf("错满 5 次应作废，得到 %v", err)
	}
	if ok, _ := svc.VerifyCode(ctx, appID, "alice@example.com", code, "login"); ok {
		t.Fatal("作废之后正确的码也不应通过")
	}

	// ── 冷却与频次 ──
	if _, err := svc.SendCode(ctx, CodeRequest{AppID: appID, Email: "alice@example.com", Purpose: "login"}); errCode(err) != 42961 {
		t.Fatalf("冷却期内重发应被拒，得到 %v", err)
	}
	policy := emaildomain.DefaultPolicy()
	policy.Code.ResendSeconds = 0
	policy.Code.AddressHourly = 2
	policy.Code.Length = 8
	policy.Brand = emaildomain.Brand{DisplayName: "星河通行证", AccentColor: "#2563EB", SupportEmail: "help@example.com", FooterText: "星河科技有限公司"}
	policy.Scenes = map[string]emaildomain.SceneSetting{
		emaildomain.SceneCodeRegister: {Subject: "【{{app}}】注册码 {{code}}"},
		emaildomain.SceneWelcome:      {Enabled: ptrTo(true), Lead: "{{name}}，欢迎加入。"},
	}
	bad := policy
	bad.Scenes = map[string]emaildomain.SceneSetting{emaildomain.SceneWelcome: {Subject: "{{code}}"}}
	if _, err := svc.SavePolicy(ctx, appID, bad, "root"); errCode(err) != 42292 {
		t.Fatalf("用了场景没有的变量应被拒，得到 %v", err)
	}
	view, err := svc.SavePolicy(ctx, appID, policy, "root")
	if err != nil {
		t.Fatal(err)
	}
	if !view.Configured || view.Policy.Brand.AccentColor != "#2563eb" || view.UpdatedBy != "root" {
		t.Fatalf("保存后的策略不对：%+v", view.PolicyRecord)
	}
	if _, err := svc.SendCode(ctx, CodeRequest{AppID: appID, Email: "bob@example.com", Purpose: "register"}); err != nil {
		t.Fatal(err)
	}
	mail = outbox.last(t)
	if !regexp.MustCompile(`^【星河通行证】注册码 \d{8}$`).MatchString(mail.Subject) {
		t.Fatalf("主题覆盖与 8 位码没有生效：%q", mail.Subject)
	}
	for _, want := range []string{"#2563eb", "help@example.com", "星河科技有限公司"} {
		if !strings.Contains(mail.HTML, want) {
			t.Errorf("品牌设置没进正文：缺 %q", want)
		}
	}
	if _, err := svc.SendCode(ctx, CodeRequest{AppID: appID, Email: "bob@example.com", Purpose: "register"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SendCode(ctx, CodeRequest{AppID: appID, Email: "bob@example.com", Purpose: "login"}); errCode(err) != 42962 {
		t.Fatalf("超过每小时上限应被拒，得到 %v", err)
	}

	// ── 欢迎信：场景开关 ──
	if err := svc.SendWelcomeEmail(ctx, appID, "carol@example.com", "小林", ""); err != nil {
		t.Fatal(err)
	}
	if mail = outbox.last(t); mail.To != "carol@example.com" || !strings.Contains(mail.Text, "小林，欢迎加入。") {
		t.Fatalf("欢迎信没有按覆盖的引导语发出：%+v", mail.Text)
	}
	before := outbox.count()
	if err := svc.SendWelcomeEmail(ctx, otherAppID, "carol@example.com", "小林", ""); err != nil {
		t.Fatal(err)
	}
	if outbox.count() != before {
		t.Fatal("欢迎信默认关闭，不应发出")
	}

	// ── 回执：硬退信自动加入抑制名单，下一封被拦 ──
	page, err := svc.ListDeliveries(ctx, emaildomain.DeliveryQuery{AppID: appID, Keyword: "bob@"})
	if err != nil || page.Total != 2 {
		t.Fatalf("应有 2 封发给 bob 的留痕：%+v %v", page, err)
	}
	bounced := page.Items[0]
	matched, err := svc.applyDeliveryEvent(ctx, pgrepo.EmailDeliveryStatusUpdate{
		AppID: appID, ProviderMessageID: bounced.ProviderMessageID, Event: emaildomain.EventBounce,
		Status: emaildomain.DeliveryStatusBounced, ErrorMessage: "550 mailbox not found",
	})
	if err != nil || !matched {
		t.Fatalf("退信回执应匹配：%v %v", matched, err)
	}
	if _, err := svc.SendCode(ctx, CodeRequest{AppID: appID, Email: "BOB@example.com", Purpose: "reset_password"}); errCode(err) != 42290 {
		t.Fatalf("被抑制的地址应拒绝投递，得到 %v", err)
	}
	detail, err := svc.DeliveryDetail(ctx, appID, bounced.ID)
	if err != nil || len(detail.Events) != 1 || detail.Events[0].Status != emaildomain.DeliveryStatusBounced || detail.Suppression == nil {
		t.Fatalf("投递详情不对：%+v %v", detail, err)
	}
	if _, err := svc.DeliveryDetail(ctx, otherAppID, bounced.ID); errCode(err) != 40469 {
		t.Fatalf("别的应用不能查这封信，得到 %v", err)
	}
	suppressions, err := svc.ListSuppressions(ctx, emaildomain.SuppressionQuery{AppID: appID})
	if err != nil || suppressions.Total != 1 || suppressions.Items[0].Source != emaildomain.SuppressionSourceWebhook {
		t.Fatalf("抑制名单不对：%+v %v", suppressions, err)
	}
	if err := svc.RemoveSuppression(ctx, otherAppID, suppressions.Items[0].ID); errCode(err) != 40470 {
		t.Fatalf("别的应用不能移出这条记录，得到 %v", err)
	}
	if err := svc.RemoveSuppression(ctx, appID, suppressions.Items[0].ID); err != nil {
		t.Fatal(err)
	}
	manual, err := svc.AddSuppression(ctx, emaildomain.SuppressionInput{AppID: appID, Address: " Dave@Example.com ", Note: "用户要求退订", Operator: "root"})
	if err != nil || manual.Address != "dave@example.com" || manual.Reason != emaildomain.SuppressionManual {
		t.Fatalf("手动加入不对：%+v %v", manual, err)
	}

	// ── 概览 ──
	overview, err := svc.DeliveryOverview(ctx, appID, 7, "Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	if len(overview.Trend) != 7 || overview.WindowTotal != overview.Total || overview.Bounced != 1 || overview.Suppressions != 1 {
		t.Fatalf("概览不对：%+v", overview)
	}
	if last := overview.Trend[6]; last.Date != time.Now().In(mustLocation(t, "Asia/Shanghai")).Format("2006-01-02") || last.Total != overview.Total {
		t.Fatalf("趋势最后一天应是今天且包含全部发信：%+v", last)
	}

	// ── 共享通道：留痕记在发信应用名下，平台回执仍能匹配 ──
	saveChannel(emaildomain.PlatformAppID, "platform", true)
	if _, err := svc.SendCode(ctx, CodeRequest{AppID: otherAppID, Email: "erin@example.com", Purpose: "login"}); err != nil {
		t.Fatal(err)
	}
	shared, err := svc.ListDeliveries(ctx, emaildomain.DeliveryQuery{AppID: otherAppID})
	if err != nil || shared.Total != 1 || !shared.Items[0].Inherited {
		t.Fatalf("借道平台通道的信应记在应用名下并标明：%+v %v", shared, err)
	}
	if matched, err := svc.applyDeliveryEvent(ctx, pgrepo.EmailDeliveryStatusUpdate{
		AppID: emaildomain.PlatformAppID, ProviderMessageID: shared.Items[0].ProviderMessageID,
		Event: emaildomain.EventDelivery, Status: emaildomain.DeliveryStatusDelivered, MarkDelivered: true,
	}); err != nil || !matched {
		t.Fatalf("平台通道的回执应匹配到借道发出的信：%v %v", matched, err)
	}
	if matched, _ := svc.applyDeliveryEvent(ctx, pgrepo.EmailDeliveryStatusUpdate{
		AppID: appID, ProviderMessageID: shared.Items[0].ProviderMessageID, Event: emaildomain.EventBounce,
		Status: emaildomain.DeliveryStatusBounced,
	}); matched {
		t.Fatal("别的应用的回执地址不应改到这封信")
	}

	// ── 重置链接：必须登记前缀，不存在的账号不发信 ──
	if _, err := svc.SendPasswordResetLink(ctx, ResetLinkRequest{AppID: appID, Email: "frank@example.com", ResetURL: "https://evil.example/steal"}); errCode(err) != 42291 {
		t.Fatalf("未登记链接时应被拒，得到 %v", err)
	}
	policy.ResetLinks = []string{"https://app.example.com/reset"}
	if _, err := svc.SavePolicy(ctx, appID, policy, "root"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SendPasswordResetLink(ctx, ResetLinkRequest{AppID: appID, Email: "frank@example.com", ResetURL: "https://app.example.com.evil.io/reset"}); errCode(err) != 42291 {
		t.Fatalf("前缀之外的链接应被拒，得到 %v", err)
	}
	before = outbox.count()
	result, err := svc.SendPasswordResetLink(ctx, ResetLinkRequest{AppID: appID, Email: "frank@example.com"})
	if err != nil || !result.Success || outbox.count() != before {
		t.Fatalf("不存在的账号应返回成功但不发信：%+v %v", result, err)
	}
	if _, err := pg.CreateUserWithProfile(ctx, appID, "frank", "x", userdomain.Profile{Email: "frank@example.com"}, userdomain.ProfileSecurityState{}); err != nil {
		t.Fatal(err)
	}
	result, err = svc.SendPasswordResetLink(ctx, ResetLinkRequest{AppID: appID, Email: "frank@example.com", ResetURL: "https://app.example.com/reset/confirm"})
	if err != nil || outbox.count() != before+1 || !strings.HasPrefix(result.ResetURL, "https://app.example.com/reset/confirm?token=") {
		t.Fatalf("真实账号应收到登记前缀内的链接：%+v %v", result, err)
	}
	if ok, err := svc.VerifyResetToken(ctx, appID, "frank@example.com", result.Token); !ok || err != nil {
		t.Fatalf("重置令牌应能验证：%v %v", ok, err)
	}

	// ── 自定义主题、版式与场景模板 ──
	broken := policy
	broken.Scenes = map[string]emaildomain.SceneSetting{
		emaildomain.SceneWelcome: {Enabled: ptrTo(true), Template: &emaildomain.SceneTemplate{Enabled: true, Body: "<mj-text>hi</mj-text>\n{% if name %}\n没闭合"}},
	}
	if _, err := svc.SavePolicy(ctx, appID, broken, "root"); errCode(err) != 42292 || !strings.Contains(err.Error(), "欢迎信") || !strings.Contains(err.Error(), "行") {
		t.Fatalf("坏模板应在保存时报出场景与行号，得到 %v", err)
	}
	broken.Scenes = nil
	broken.Layout = emaildomain.LayoutTemplate{Enabled: true, Source: "<mjml><mj-body></mj-body></mjml>"}
	if _, err := svc.SavePolicy(ctx, appID, broken, "root"); errCode(err) != 42292 {
		t.Fatalf("没有 {%% content %%} 的版式应被拒，得到 %v", err)
	}
	radius := 4
	policy.Theme = emaildomain.ThemeSettings{Background: "#0F172A", Radius: &radius, Font: "serif", Header: "banner"}
	policy.Layout = emaildomain.LayoutTemplate{Enabled: true, Source: `<mjml><mj-body background-color="{{ theme.background }}"><mj-section><mj-column>
<mj-text color="#ffffff">自定义版式·{{ brand.name }}</mj-text>
{% content %}
</mj-column></mj-section></mj-body></mjml>`}
	policy.Scenes[emaildomain.SceneWelcome] = emaildomain.SceneSetting{
		Enabled: ptrTo(true),
		Subject: "欢迎 {{ name | default: \"朋友\" }} 加入 {{ app }}",
		Template: &emaildomain.SceneTemplate{Enabled: true, Body: `{% markdown %}**{{ name }}**，欢迎！登录邮箱 {{ email | mask_email }}{% endmarkdown %}
{% button url: "https://app.example.com/start", label: "开始使用" %}`, Text: "{{ name }} 欢迎"},
	}
	if _, err := svc.SavePolicy(ctx, appID, policy, "root"); err != nil {
		t.Fatalf("合法的自定义模板应能保存：%v", err)
	}
	if err := svc.SendWelcomeEmail(ctx, appID, "gina@example.com", "阿吉", ""); err != nil {
		t.Fatal(err)
	}
	mail = outbox.last(t)
	if mail.Subject != "欢迎 阿吉 加入 星河通行证" || mail.Text != "阿吉 欢迎" {
		t.Fatalf("自定义主题行或纯文本不对：%q / %q", mail.Subject, mail.Text)
	}
	for _, want := range []string{"自定义版式·星河通行证", "<strong>阿吉</strong>", "g**a@example.com", "开始使用", "#0f172a"} {
		if !strings.Contains(mail.HTML, want) {
			t.Errorf("自定义模板的信缺 %q", want)
		}
	}
	// 没有自定义正文的场景同样套用自定义版式
	if _, err := svc.SendCode(ctx, CodeRequest{AppID: appID, Email: "hank@example.com", Purpose: "login"}); err != nil {
		t.Fatal(err)
	}
	if mail = outbox.last(t); !strings.Contains(mail.HTML, "自定义版式·星河通行证") || !strings.Contains(mail.HTML, "letter-spacing:10px") {
		t.Error("验证码邮件应套用自定义版式并保留内置的验证码组件")
	}

	// ── 预览：每个场景都渲染得出来 ──
	for _, scene := range emaildomain.Scenes() {
		preview, err := svc.Preview(ctx, appID, EmailPreviewRequest{Scene: scene.Key})
		if err != nil || preview.Subject == "" || !strings.Contains(preview.HTML, "星河通行证") || strings.Contains(preview.Subject, "{{") {
			t.Errorf("场景 %s 预览不对：%+v %v", scene.Key, preview, err)
		}
	}
}

// fakeMailOutbox 记录每封外发邮件的假发送器，替换 SMTP 那一档。
type fakeMailOutbox struct {
	mu    sync.Mutex
	sent  []emailOutbound
	index int
}

func (f *fakeMailOutbox) Provider() string { return emaildomain.ProviderSMTP }
func (f *fakeMailOutbox) Describe() emaildomain.ProviderMeta {
	return emaildomain.ProviderMeta{Provider: emaildomain.ProviderSMTP, Name: "SMTP"}
}
func (f *fakeMailOutbox) Validate(emaildomain.Config) error { return nil }
func (f *fakeMailOutbox) SupportsAttachments() bool         { return true }
func (f *fakeMailOutbox) Send(_ context.Context, _ *emaildomain.Config, out emailOutbound) (emailSendResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.index++
	f.sent = append(f.sent, out)
	return emailSendResult{ProviderMessageID: fmt.Sprintf("pm-%d", f.index), Status: emaildomain.DeliveryStatusSent}, nil
}

func (f *fakeMailOutbox) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sent)
}

func (f *fakeMailOutbox) last(t *testing.T) emailOutbound {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sent) == 0 {
		t.Fatal("还没有发出任何邮件")
	}
	return f.sent[len(f.sent)-1]
}
