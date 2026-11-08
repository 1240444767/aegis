package email

import (
	"strings"
	"testing"
)

func TestMatchResetLinkComparesHostAndPathPrefix(t *testing.T) {
	allowed := []string{"https://app.example.com/reset", "http://localhost:3000"}
	cases := []struct {
		candidate string
		want      string
		ok        bool
	}{
		{"", "https://app.example.com/reset", true},
		{"https://app.example.com/reset", "https://app.example.com/reset", true},
		{"https://app.example.com/reset/confirm?x=1#y", "https://app.example.com/reset/confirm", true},
		{"https://APP.example.com/reset", "https://APP.example.com/reset", true},
		{"https://app.example.com/resetx", "", false},
		{"https://app.example.com.evil.io/reset", "", false},
		{"http://app.example.com/reset", "", false},
		{"https://evil.io/reset", "", false},
		{"http://localhost:3000/any/path", "http://localhost:3000/any/path", true},
		{"not a url", "", false},
	}
	for _, item := range cases {
		got, ok := MatchResetLink(allowed, item.candidate)
		if ok != item.ok || got != item.want {
			t.Errorf("MatchResetLink(%q) = %q, %v；期望 %q, %v", item.candidate, got, ok, item.want, item.ok)
		}
	}
	if _, ok := MatchResetLink(nil, ""); ok {
		t.Error("没有登记任何链接时不应放行")
	}
}

func TestValidatePolicyRejectsBadInput(t *testing.T) {
	cases := map[string]func(p *Policy){
		"位数":     func(p *Policy) { p.Code.Length = 3 },
		"每小时上限":  func(p *Policy) { p.Code.AddressHourly = 50; p.Code.AddressDaily = 10 },
		"Logo":   func(p *Policy) { p.Brand.LogoURL = "http://cdn.example.com/logo.png" },
		"强调色":    func(p *Policy) { p.Brand.AccentColor = "blue" },
		"客服邮箱":   func(p *Policy) { p.Brand.SupportEmail = "nope" },
		"未知场景":   func(p *Policy) { p.Scenes = map[string]SceneSetting{"bogus": {Title: "x"}} },
		"变量":     func(p *Policy) { p.Scenes = map[string]SceneSetting{SceneWelcome: {Title: "{{code}}"}} },
		"主题换行":   func(p *Policy) { p.Scenes = map[string]SceneSetting{SceneWelcome: {Subject: "a\nb"}} },
		"重置链接协议": func(p *Policy) { p.ResetLinks = []string{"http://app.example.com/reset"} },
		"重置链接参数": func(p *Policy) { p.ResetLinks = []string{"https://app.example.com/reset?a=1"} },
	}
	for name, mutate := range cases {
		policy := DefaultPolicy()
		mutate(&policy)
		if err := ValidatePolicy(policy); err == nil {
			t.Errorf("%s：应当校验失败", name)
		}
	}
	if err := ValidatePolicy(DefaultPolicy()); err != nil {
		t.Errorf("默认策略应当合法：%v", err)
	}
}

func TestNormalizePolicyKeepsOnlyMeaningfulOverrides(t *testing.T) {
	on := true
	policy := DefaultPolicy()
	// 位数、有效期、错误次数不允许为 0，越界的回到默认值；频次与冷却的 0 是「不限」，原样保留。
	// 老策略缺字段的情形由仓储层处理：它把 JSON 解到一份默认策略上，缺的字段本来就是默认值。
	policy.Code = CodePolicy{}
	policy.Scenes = map[string]SceneSetting{
		SceneWelcome:       {Enabled: &on},                  // 与默认（关）不同，保留
		SceneProfileChange: {Enabled: &on},                  // 与默认（开）相同，丢弃
		SceneCodeLogin:     {Enabled: &on, Title: "  登录  "}, // 不可关闭的场景开关丢弃，文案保留
		"bogus":            {Title: "x"},                    // 目录外的场景丢弃
	}
	normalized := NormalizePolicy(policy)
	def := DefaultPolicy().Code
	if normalized.Code != (CodePolicy{Length: def.Length, TTLMinutes: def.TTLMinutes, MaxAttempts: def.MaxAttempts}) {
		t.Errorf("越界的验证码规则应回到默认值、0 的频次保持不限：%+v", normalized.Code)
	}
	if len(normalized.Scenes) != 2 || normalized.Scenes[SceneWelcome].Enabled == nil ||
		normalized.Scenes[SceneCodeLogin].Enabled != nil || normalized.Scenes[SceneCodeLogin].Title != "登录" {
		t.Errorf("场景覆盖规整不对：%+v", normalized.Scenes)
	}
	if !normalized.SceneEnabled(SceneWelcome) || !normalized.SceneEnabled(SceneProfileChange) {
		t.Error("开关结论不对")
	}
}

func TestSceneCatalogIsWellFormed(t *testing.T) {
	seen := map[string]bool{}
	for _, scene := range Scenes() {
		if seen[scene.Key] {
			t.Errorf("场景键重复：%s", scene.Key)
		}
		seen[scene.Key] = true
		if scene.CategoryName == "" || scene.Name == "" {
			t.Errorf("场景 %s 缺名称或分组", scene.Key)
		}
		copyText := scene.Defaults
		for _, text := range []string{copyText.Subject, copyText.Title, copyText.Lead, copyText.Footer} {
			if strings.TrimSpace(text) == "" {
				t.Errorf("场景 %s 的默认文案有空段", scene.Key)
			}
			for _, match := range placeholderPattern.FindAllStringSubmatch(text, -1) {
				if !scene.HasVariable(match[1]) {
					t.Errorf("场景 %s 的默认文案用了未声明的变量 %s", scene.Key, match[1])
				}
			}
		}
		if scene.Category == SceneCategoryCode && !strings.Contains(copyText.Subject, "{{code}}") {
			t.Errorf("验证码场景 %s 的默认主题应以验证码开头", scene.Key)
		}
		// 默认文案渲染后不能残留占位符
		rendered := DefaultPolicy().ResolveCopy(scene, scene.SampleVars("星河"))
		if strings.Contains(rendered.Subject+rendered.Title+rendered.Lead+rendered.Footer, "{{") {
			t.Errorf("场景 %s 渲染后残留占位符：%+v", scene.Key, rendered)
		}
	}
	if CodeSceneForPurpose("SIGNUP").Key != SceneCodeRegister || CodeSceneForPurpose("whatever").Key != SceneCodeGeneric {
		t.Error("用途到场景的映射不对")
	}
}
