package mailtpl

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
)

func sampleMessage() Message {
	return Message{
		BrandName:  "星河 <b>&</b> Co",
		Eyebrow:    "验证码",
		Title:      "登录验证码",
		Lead:       "输入验证码即可登录。",
		FooterNote: "不是您本人在登录的话，请立刻改密码。",
		Support:    "help@example.com",
		FooterText: "星河科技有限公司",
		Preheader:  "验证码 482913，10 分钟内有效",
		Blocks: []Block{
			{Kind: "code", Code: "482913", Note: "10 分钟内有效"},
			{Kind: "paragraph", Text: "请不要转发。"},
		},
		Vars: map[string]any{
			"code": "482913", "minutes": "10", "email": "lin@example.com",
			"reset_url": "https://app.example.com/reset?token=abc&email=lin@example.com",
			"details":   RowsBinding([]Row{{Label: "账号", Value: "lin"}, {Label: "IP 地址", Value: "203.0.113.9"}}),
		},
	}
}

func TestBuiltinBodiesRenderAndValidate(t *testing.T) {
	engine := New()
	ctx := context.Background()
	for _, key := range []string{"code_login", "code_register", "password_reset_link", "password_reset_notice", "profile_change", "welcome", "blocks"} {
		for _, header := range HeaderStyles() {
			theme := DefaultTheme()
			theme.Header = header
			if problems := engine.Validate(ctx, theme, sampleMessage(), Templates{BodyKey: key}); len(problems) > 0 {
				t.Errorf("%s/%s 严格校验失败：%+v", key, header, problems)
			}
		}
		result, err := engine.Render(ctx, DefaultTheme(), sampleMessage(), Templates{BodyKey: key})
		if err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		if !strings.Contains(result.HTML, "登录验证码") || result.Text == "" {
			t.Errorf("%s 渲染结果缺内容", key)
		}
	}
}

func TestUserDataIsEscaped(t *testing.T) {
	result, err := New().Render(context.Background(), DefaultTheme(), sampleMessage(), Templates{BodyKey: "code_login"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.HTML, "<b>&</b>") {
		t.Error("品牌名里的标记没有被转义")
	}
	if !strings.Contains(result.HTML, "&lt;b&gt;") {
		t.Error("品牌名应当转义后照常显示")
	}
	if !strings.Contains(result.HTML, "482913") || !strings.Contains(result.HTML, "10 分钟内有效") {
		t.Error("验证码组件没有画出来")
	}
	if !strings.Contains(result.Text, "    482913") {
		t.Errorf("纯文本版缺验证码：\n%s", result.Text)
	}
}

func TestThemeAppliesEverywhere(t *testing.T) {
	theme := DefaultTheme()
	theme.Accent = "#2563EB"
	theme.Background = "#eef2ff"
	theme.Header = "bar"
	theme.Font = "serif"
	result, err := New().Render(context.Background(), theme, sampleMessage(), Templates{BodyKey: "password_reset_link"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"#2563eb", "#eef2ff", "4px solid #2563eb", "Songti SC", "设置新密码", "https://app.example.com/reset?token=abc&amp;email=lin@example.com"} {
		if !strings.Contains(result.HTML, want) {
			t.Errorf("渲染结果缺 %q", want)
		}
	}
}

func TestLightAccentGetsReadableText(t *testing.T) {
	palette := Theme{Accent: "#facc15"}.palette()
	if palette["accent_text"] != "#18181b" {
		t.Errorf("亮黄按钮上应当用深色文字，得到 %v", palette["accent_text"])
	}
	if palette["accent_strong"] == "#facc15" {
		t.Error("亮黄做文字色应当自动加深")
	}
	if (Theme{Accent: "#1e3a8a"}).palette()["accent_text"] != "#ffffff" {
		t.Error("深蓝按钮上应当用白字")
	}
}

func TestCustomBodyLayoutAndText(t *testing.T) {
	engine := New()
	ctx := context.Background()
	layout := `<mjml><mj-body background-color="{{ theme.background }}"><mj-section><mj-column>
<mj-text>自定义版式 {{ brand.name }}</mj-text>
{% content %}
</mj-column></mj-section></mj-body></mjml>`
	body := `{% if code != blank %}{% code note: "很快过期" %}{% endif %}
{% markdown %}
你好 **{{ email | mask_email }}**，[查看帮助](https://example.com/help)。
{% endmarkdown %}
{% notice title: "提示", text: "第一行
第二行", tone: "warning" %}`
	result, err := engine.Render(ctx, DefaultTheme(), sampleMessage(), Templates{Layout: layout, Body: body, Text: "码：{{ code }}"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"自定义版式", "很快过期", "l*n@example.com", "<strong>", "https://example.com/help", "第一行<br>第二行", "#f59e0b"} {
		if !strings.Contains(result.HTML, want) {
			t.Errorf("自定义模板结果缺 %q", want)
		}
	}
	if result.Text != "码：482913" {
		t.Errorf("自定义纯文本不对：%q", result.Text)
	}

	// 没写纯文本模板的自定义正文从 HTML 抓取
	result, err = engine.Render(ctx, DefaultTheme(), sampleMessage(), Templates{Body: `<mj-text>只有一行字</mj-text>`})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Text, "只有一行字") {
		t.Errorf("纯文本应从 HTML 抓取：%q", result.Text)
	}
}

func TestStandaloneMJMLAndHTMLFormats(t *testing.T) {
	engine := New()
	ctx := context.Background()
	result, err := engine.Render(ctx, DefaultTheme(), sampleMessage(), Templates{
		Body: `<mjml><mj-body><mj-section><mj-column><mj-text>独立文档 {{ code }}</mj-text></mj-column></mj-section></mj-body></mjml>`,
	})
	if err != nil || !strings.Contains(result.HTML, "独立文档 482913") || strings.Contains(result.HTML, "help@example.com") {
		t.Fatalf("完整 MJML 文档不应套版式：%v", err)
	}
	result, err = engine.Render(ctx, DefaultTheme(), sampleMessage(), Templates{
		Format: FormatHTML,
		Body:   `<html><head><style>.x{color:red}</style></head><body><p class="x">{{ brand.name }}</p></body></html>`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.HTML, `color:red`) || !strings.Contains(result.HTML, "&lt;b&gt;") {
		t.Errorf("HTML 格式应内联样式并转义变量：%s", result.HTML)
	}
}

func TestTemplateErrorsCarryLocation(t *testing.T) {
	engine := New()
	ctx := context.Background()
	_, err := engine.Render(ctx, DefaultTheme(), sampleMessage(), Templates{Body: "<mj-text>ok</mj-text>\n{% if code %}\n没有闭合"})
	var templateErr *TemplateError
	if !errors.As(err, &templateErr) || templateErr.Part != PartBody || templateErr.Line == 0 {
		t.Fatalf("没闭合的标签应报出位置，得到 %v", err)
	}
	_, err = engine.Render(ctx, DefaultTheme(), sampleMessage(), Templates{Body: `{% button label: "x" %}`})
	if !errors.As(err, &templateErr) || !strings.Contains(templateErr.Message, "url") {
		t.Fatalf("缺参数的组件应说明缺什么，得到 %v", err)
	}
	if problems := engine.Validate(ctx, DefaultTheme(), sampleMessage(), Templates{Body: `<mj-text><mj-bogus /></mj-text><mj-unknown></mj-unknown>`}); len(problems) == 0 {
		t.Error("未知的 MJML 标签应在严格校验里报出")
	}
}

func TestTemplatesCannotReadFilesOrRunAway(t *testing.T) {
	engine := New()
	ctx := context.Background()
	for _, body := range []string{`{% include "/etc/passwd" %}`, `{% render "../engine.go" %}`} {
		if _, err := engine.Render(ctx, DefaultTheme(), sampleMessage(), Templates{Body: body}); err == nil {
			t.Errorf("%s 应当失败", body)
		}
	}
	_, err := engine.Render(ctx, DefaultTheme(), sampleMessage(), Templates{Body: `{% for i in (1..200000) %}xxxxxxxxxx{% endfor %}`})
	var templateErr *TemplateError
	if !errors.As(err, &templateErr) || !strings.Contains(templateErr.Message, "KB") {
		t.Fatalf("失控的循环应被输出上限拦下，得到 %v", err)
	}
}

func TestCompiledHTMLIsCached(t *testing.T) {
	engine := New()
	var calls atomic.Int32
	real := engine.compile
	engine.compile = func(ctx context.Context, source string) (string, error) {
		calls.Add(1)
		return real(ctx, source)
	}
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if _, err := engine.Render(ctx, DefaultTheme(), sampleMessage(), Templates{BodyKey: "welcome"}); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Errorf("同一份源码应只编译一次，实际 %d 次", calls.Load())
	}
}

func TestFilters(t *testing.T) {
	engine := New()
	out, err := engine.RenderPlainWith(PartCopy, `{{ e | mask_email }} {{ p | mask_phone }} {{ m | minutes_text }} {{ t | datetime: "Asia/Shanghai", "01-02 15:04" }}`,
		map[string]any{"e": "alice@example.com", "p": "13800138000", "m": 90, "t": "2026-10-18T01:30:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if out != "a***e@example.com 138****8000 1 小时 30 分钟 10-18 09:30" {
		t.Errorf("过滤器结果不对：%q", out)
	}
}
