package service

import (
	"context"
	"testing"
	"time"

	securitydomain "aegis/internal/domain/security"
	"aegis/pkg/timeutil"
)

func ptrInt64(v int64) *int64 { return &v }

// TestRiskTemplatesAreValid 模板里的每一条规则与策略都必须过得了保存时的校验 ——
// 一键安装装出一半、剩下的报「表达式无法编译」是最尴尬的失败。
func TestRiskTemplatesAreValid(t *testing.T) {
	seen := map[string]bool{}
	for _, tpl := range securitydomain.RiskTemplates() {
		for _, rule := range tpl.Rules {
			key := securitydomain.TemplateRuleKey(tpl.Key, rule.Key)
			if seen[key] {
				t.Errorf("模板规则键重复：%s", key)
			}
			seen[key] = true
			if err := ValidateRuleInput(rule.Scene, rule.ConditionType, rule.ConditionData); err != nil {
				t.Errorf("模板 %s 规则 %s 校验失败：%v", tpl.Key, rule.Key, err)
			}
			if err := validateRuleMode("", rule.Decision); err != nil {
				t.Errorf("模板 %s 规则 %s 强制处置非法：%v", tpl.Key, rule.Key, err)
			}
		}
		for i := range tpl.Actions {
			a := tpl.Actions[i]
			if err := validateActionInput(a.Scene, a.Action, a.MinScore, a.MaxScore, a.BanDuration); err != nil {
				t.Errorf("模板 %s 策略 %d 校验失败：%v", tpl.Key, i, err)
			}
			for j := range tpl.Actions[:i] {
				b := tpl.Actions[j]
				if a.Scene == b.Scene && scoreRangesOverlap(a.MinScore, a.MaxScore, b.MinScore, b.MaxScore) {
					t.Errorf("模板 %s 策略区间互相重叠", tpl.Key)
				}
			}
		}
	}
}

// TestConditionCatalogHasDimension 每种条件都要归属一个已登记的维度
func TestConditionCatalogHasDimension(t *testing.T) {
	dims := map[string]bool{}
	for _, d := range securitydomain.DimensionValues() {
		dims[d] = true
	}
	for _, entry := range securitydomain.ConditionCatalogEntries() {
		if !dims[entry.Dimension] {
			t.Errorf("条件类型 %q 的维度 %q 未登记", entry.Value, entry.Dimension)
		}
	}
}

func TestAccountShape(t *testing.T) {
	cases := []struct{ in, kind, domain, phone string }{
		{"Foo@Mailinator.COM", "email", "mailinator.com", ""},
		{"+8613800138000", "phone", "", "13800138000"},
		{"13800138000", "phone", "", "13800138000"},
		{"zhangsan", "name", "", ""},
		{"", "", "", ""},
	}
	for _, c := range cases {
		kind, domain, phone := accountShape(c.in)
		if kind != c.kind || domain != c.domain || phone != c.phone {
			t.Errorf("accountShape(%q) = %q %q %q", c.in, kind, domain, phone)
		}
	}
}

func listEntry(id int64, listType, value, kind string) securitydomain.RiskListEntry {
	return securitydomain.RiskListEntry{ID: id, ListType: listType, Value: normalizeListValue(listType, value), Kind: kind}
}

func TestRiskListIndexMatch(t *testing.T) {
	now := timeutil.Now()
	past := now.Add(-time.Hour)
	expired := listEntry(9, securitydomain.ListTypeIP, "9.9.9.9", securitydomain.ListKindBlock)
	expired.ExpiresAt = &past
	appOnly := listEntry(10, securitydomain.ListTypeDevice, "dev-app", securitydomain.ListKindBlock)
	appOnly.AppID = ptrInt64(7)
	sceneOnly := listEntry(11, securitydomain.ListTypeDevice, "dev-scene", securitydomain.ListKindWatch)
	sceneOnly.Scenes = []string{securitydomain.SceneLottery}

	ix := buildRiskListIndex([]securitydomain.RiskListEntry{
		listEntry(1, securitydomain.ListTypeIP, "1.2.3.4", securitydomain.ListKindBlock),
		listEntry(2, securitydomain.ListTypeCIDR, "10.0.0.0/8", securitydomain.ListKindWatch),
		listEntry(3, securitydomain.ListTypeEmailDomain, "Mailinator.com", securitydomain.ListKindBlock),
		listEntry(4, securitydomain.ListTypeAccount, "Alice", securitydomain.ListKindAllow),
		listEntry(5, securitydomain.ListTypeUAKeyword, "python-requests", securitydomain.ListKindWatch),
		listEntry(6, securitydomain.ListTypePhonePrefix, "170", securitydomain.ListKindWatch),
		listEntry(7, securitydomain.ListTypeCountry, "kp", securitydomain.ListKindBlock),
		listEntry(8, securitydomain.ListTypeUser, "42", securitydomain.ListKindBlock),
		expired, appOnly, sceneOnly,
	})
	ids := func(subject riskListSubject) map[int64]bool {
		out := map[int64]bool{}
		for _, e := range ix.match(subject, now) {
			out[e.ID] = true
		}
		return out
	}

	got := ids(riskListSubject{AppID: 1, Scene: "login", IP: "1.2.3.4", Account: "alice"})
	if !got[1] || !got[4] || len(got) != 2 {
		t.Fatalf("IP 精确与账号大小写不敏感：%v", got)
	}
	got = ids(riskListSubject{AppID: 1, Scene: "login", IP: "10.20.30.40", EmailDomain: "x.mailinator.com", UserAgent: "Python-Requests/2.31"})
	if !got[2] || !got[3] || !got[5] {
		t.Fatalf("网段 / 子域名 / UA 关键词：%v", got)
	}
	got = ids(riskListSubject{AppID: 1, Scene: "login", Phone: "17012345678", Country: "KP", UserID: 42})
	if !got[6] || !got[7] || !got[8] {
		t.Fatalf("号段 / 国家 / 用户 ID：%v", got)
	}
	if got = ids(riskListSubject{AppID: 1, Scene: "login", IP: "9.9.9.9"}); len(got) != 0 {
		t.Fatalf("过期名单不应命中：%v", got)
	}
	if got = ids(riskListSubject{AppID: 1, Scene: "login", DeviceID: "dev-app"}); len(got) != 0 {
		t.Fatalf("其它应用的名单不应命中：%v", got)
	}
	if got = ids(riskListSubject{AppID: 7, Scene: "login", DeviceID: "dev-app"}); !got[10] {
		t.Fatalf("本应用名单应命中：%v", got)
	}
	if got = ids(riskListSubject{AppID: 1, Scene: "login", DeviceID: "dev-scene"}); len(got) != 0 {
		t.Fatalf("限定场景之外不应命中：%v", got)
	}
	if got = ids(riskListSubject{AppID: 1, Scene: "lottery", DeviceID: "dev-scene"}); !got[11] {
		t.Fatalf("限定场景之内应命中：%v", got)
	}
}

func TestValidateListValue(t *testing.T) {
	bad := map[string]string{
		securitydomain.ListTypeIP:          "300.1.1.1",
		securitydomain.ListTypeCIDR:        "0.0.0.0/0",
		securitydomain.ListTypeUser:        "abc",
		securitydomain.ListTypeEmailDomain: "nodot",
		securitydomain.ListTypePhonePrefix: "1a",
		securitydomain.ListTypeCountry:     "CHN",
		securitydomain.ListTypeUAKeyword:   "ab",
	}
	for listType, value := range bad {
		if err := validateListValue(listType, normalizeListValue(listType, value)); err == nil {
			t.Errorf("%s 值 %q 应被拒绝", listType, value)
		}
	}
	if got := normalizeListValue(securitydomain.ListTypeCIDR, "10.1.2.3/8"); got != "10.0.0.0/8" {
		t.Errorf("网段应规整为网络地址：%s", got)
	}
}

func rule(id int64, scene, condType string, data map[string]any, score int) securitydomain.RiskRule {
	return securitydomain.RiskRule{
		ID: id, Name: condType, Scene: scene, ConditionType: condType, ConditionData: data, Score: score,
		IsActive: true, Mode: securitydomain.RuleModeEnforce, Dimension: securitydomain.ConditionDimension(condType, data),
	}
}

func botEnv(scene string) *RiskEvalEnv {
	env := newRiskEvalEnv(timeutil.Now())
	env.Scene = scene
	env.AppID = 1
	env.IP = "203.0.113.9"
	env.UAIsBot = true
	env.UALength = 0
	return env
}

func TestDecidePipeline(t *testing.T) {
	svc := &RiskService{}
	ctx := context.Background()
	bot := rule(1, securitydomain.SceneAll, securitydomain.CondUABot, map[string]any{}, 50)
	missing := rule(2, securitydomain.SceneAll, securitydomain.CondUAMissing, map[string]any{"min_length": 16}, 30)
	snap := &riskSnapshot{
		lists: buildRiskListIndex(nil),
		actions: []securitydomain.RiskAction{
			{ID: 1, Scene: securitydomain.SceneAll, MinScore: 41, MaxScore: new(79), Action: securitydomain.ActionReview, IsActive: true},
			{ID: 2, Scene: securitydomain.SceneAll, MinScore: 80, Action: securitydomain.ActionBlock, IsActive: true},
		},
	}

	d := svc.decide(ctx, snap, []securitydomain.RiskRule{bot, missing}, botEnv("login"))
	if d.total != 80 || d.action != securitydomain.ActionBlock || d.decisionBy != securitydomain.DecisionByPolicy {
		t.Fatalf("两条命中应 80 分拦截：%+v", d)
	}
	if d.dimensions[securitydomain.DimClient] != 80 {
		t.Fatalf("维度得分应归入客户端：%v", d.dimensions)
	}

	// 观察模式：留痕不计分
	observed := missing
	observed.Mode = securitydomain.RuleModeObserve
	d = svc.decide(ctx, snap, []securitydomain.RiskRule{bot, observed}, botEnv("login"))
	if d.total != 50 || d.action != securitydomain.ActionReview || len(d.matched) != 2 || !d.matched[1].Observe {
		t.Fatalf("观察模式不计分但留痕：%+v", d)
	}

	// 规则强制处置比分数策略更严时取强制
	forced := bot
	forced.Score = 5
	forced.Decision = securitydomain.ActionBlock
	d = svc.decide(ctx, snap, []securitydomain.RiskRule{forced}, botEnv("login"))
	if d.action != securitydomain.ActionBlock || d.decisionBy != securitydomain.DecisionByRule {
		t.Fatalf("强制处置应生效：%+v", d)
	}

	// 白名单放行，黑名单优先于白名单
	snap.lists = buildRiskListIndex([]securitydomain.RiskListEntry{
		listEntry(1, securitydomain.ListTypeIP, "203.0.113.9", securitydomain.ListKindAllow),
	})
	d = svc.decide(ctx, snap, []securitydomain.RiskRule{bot, missing}, botEnv("login"))
	if d.action != securitydomain.ActionPass || d.decisionBy != securitydomain.DecisionByAllowlist || d.total != 80 {
		t.Fatalf("白名单应放行且照记分数：%+v", d)
	}
	snap.lists = buildRiskListIndex([]securitydomain.RiskListEntry{
		listEntry(1, securitydomain.ListTypeIP, "203.0.113.9", securitydomain.ListKindAllow),
		listEntry(2, securitydomain.ListTypeCIDR, "203.0.113.0/24", securitydomain.ListKindBlock),
	})
	d = svc.decide(ctx, snap, nil, botEnv("login"))
	if d.action != securitydomain.ActionBlock || d.decisionBy != securitydomain.DecisionByBlocklist {
		t.Fatalf("黑名单应优先：%+v", d)
	}

	// 观察名单 → 环境变量 → 观察名单规则
	snap.lists = buildRiskListIndex([]securitydomain.RiskListEntry{
		listEntry(1, securitydomain.ListTypeIP, "203.0.113.9", securitydomain.ListKindWatch),
	})
	watch := rule(3, securitydomain.SceneAll, securitydomain.CondListWatch, map[string]any{"list_types": []any{"ip"}}, 45)
	d = svc.decide(ctx, snap, []securitydomain.RiskRule{watch}, botEnv("login"))
	if d.total != 45 || d.dimensions[securitydomain.DimList] != 45 {
		t.Fatalf("观察名单规则应命中：%+v", d)
	}
}

func TestDecideCaptchaDowngrade(t *testing.T) {
	svc := &RiskService{}
	ctx := context.Background()
	snap := &riskSnapshot{
		lists: buildRiskListIndex(nil),
		actions: []securitydomain.RiskAction{
			{ID: 1, Scene: securitydomain.SceneAll, MinScore: 1, Action: securitydomain.ActionCaptcha, IsActive: true},
		},
	}
	bot := rule(1, securitydomain.SceneAll, securitydomain.CondUABot, map[string]any{}, 50)

	// 抽奖场景接不了验证码 → 复核
	if d := svc.decide(ctx, snap, []securitydomain.RiskRule{bot}, botEnv(securitydomain.SceneLottery)); d.action != securitydomain.ActionReview {
		t.Fatalf("不支持验证码的场景应转复核：%s", d.action)
	}
	// 登录场景、应用有验证码 → 要验证码；带着验证码 → 放行
	svc.SetCaptchaProbe(func(context.Context, int64) bool { return true })
	if d := svc.decide(ctx, snap, []securitydomain.RiskRule{bot}, botEnv(securitydomain.SceneLogin)); d.action != securitydomain.ActionCaptcha {
		t.Fatalf("登录应要求验证码：%s", d.action)
	}
	env := botEnv(securitydomain.SceneLogin)
	env.CaptchaPassed = true
	if d := svc.decide(ctx, snap, []securitydomain.RiskRule{bot}, env); d.action != securitydomain.ActionPass {
		t.Fatalf("通过验证码应放行：%s", d.action)
	}
	if err := GuardError(&securitydomain.RiskEvalResult{Action: securitydomain.ActionCaptcha}); !IsRiskRejection(err) {
		t.Fatalf("验证码处置应翻译成 40015：%v", err)
	}
}

func TestResolvePolicyTiers(t *testing.T) {
	app := ptrInt64(5)
	snap := &riskSnapshot{actions: []securitydomain.RiskAction{
		{ID: 1, Scene: securitydomain.SceneAll, MinScore: 50, Action: securitydomain.ActionBlock, IsActive: true},
		{ID: 2, Scene: securitydomain.SceneLogin, MinScore: 50, Action: securitydomain.ActionReview, IsActive: true},
		{ID: 3, AppID: app, Scene: securitydomain.SceneLogin, MinScore: 50, MaxScore: new(70), Action: securitydomain.ActionCaptcha, IsActive: true},
	}}
	if p := resolvePolicy(snap, 5, securitydomain.SceneLogin, 60); p == nil || p.ID != 3 {
		t.Fatalf("应用场景策略优先：%+v", p)
	}
	if p := resolvePolicy(snap, 5, securitydomain.SceneLogin, 90); p != nil {
		t.Fatalf("应用层配了策略，区间外不应掉到全平台：%+v", p)
	}
	if p := resolvePolicy(snap, 6, securitydomain.SceneLogin, 60); p == nil || p.ID != 2 {
		t.Fatalf("其它应用走全平台场景策略：%+v", p)
	}
	if p := resolvePolicy(snap, 6, securitydomain.SceneLottery, 60); p == nil || p.ID != 1 {
		t.Fatalf("无场景策略时走全部场景：%+v", p)
	}
}
