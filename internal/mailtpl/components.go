package mailtpl

import (
	"bytes"
	"fmt"
	"html"
	"strconv"
	"strings"

	"github.com/osteele/liquid"
	"github.com/osteele/liquid/render"
	"github.com/osteele/liquid/values"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

/*
内置组件：写在 MJML 栏（mj-column）里的 Liquid 标签，按主题画出邮件里常用的几种元素。

	{% code %}                                   验证码，默认取 code 与 minutes
	{% button url: reset_url, label: "设置新密码" %}  按钮（Outlook 也是圆角实心按钮）
	{% details %}                                字段表，默认取 details
	{% notice title: "注意", text: "……" %}        提示框
	{% link url: reset_url, label: "复制链接" %}    长链接（按钮打不开时的备用）
	{% paragraph text: "……" %}                    一段正文（换行保留）
	{% markdown %}**加粗**、[链接](https://…){% endmarkdown %}
	{% image src: "https://…", alt: "", width: 520 %}
	{% divider %}  {% spacer height: 24 %}
	{% blocks %}                                 按内容积木逐个渲染（内置正文用它）
	{% content %}                                版式里正文的位置（等价于 {{ content }}）

组件是 Go 实现而不是 Liquid 片段：片段的作用域是隔离的，拿不到主题；
而组件直接从当前作用域读 theme，管理员写组件时不必再传一遍颜色。
参数值是 Liquid 表达式（变量、字符串、过滤器都可以），输出时一律按 HTML 转义。
*/

type componentFunc func(ctx render.Context, args componentArgs, theme map[string]any) (string, error)

var components = map[string]componentFunc{
	"code":      renderCodeComponent,
	"button":    renderButtonComponent,
	"details":   renderDetailsComponent,
	"notice":    renderNoticeComponent,
	"link":      renderLinkComponent,
	"paragraph": renderParagraphComponent,
	"image":     renderImageComponent,
	"divider":   renderDividerComponent,
	"spacer":    renderSpacerComponent,
	"blocks":    renderBlocksComponent,
}

func registerComponents(engine *liquid.Engine) {
	for name, fn := range components {
		fn := fn
		engine.RegisterTag(name, func(ctx render.Context) (string, error) {
			args, err := parseComponentArgs(ctx)
			if err != nil {
				return "", err
			}
			return fn(ctx, args, themeOf(ctx))
		})
	}
	engine.RegisterTag("content", func(ctx render.Context) (string, error) {
		switch value := ctx.Get("content").(type) {
		case values.SafeValue:
			return fmt.Sprint(value.Value), nil
		case string:
			return value, nil
		case nil:
			return "", nil
		default:
			return fmt.Sprint(value), nil
		}
	})
	engine.RegisterBlock("markdown", func(ctx render.Context) (string, error) {
		inner, err := ctx.InnerString()
		if err != nil {
			return "", err
		}
		return renderMarkdownComponent(inner, themeOf(ctx))
	})
}

// ── 参数 ──

type componentArgs map[string]any

func (a componentArgs) str(key string, fallback string) string {
	value, ok := a[key]
	if !ok || value == nil {
		return fallback
	}
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" {
		return fallback
	}
	return text
}

func (a componentArgs) has(key string) bool {
	_, ok := a[key]
	return ok
}

func (a componentArgs) num(key string, fallback int) int {
	value, ok := a[key]
	if !ok {
		return fallback
	}
	switch typed := value.(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	default:
		if parsed, err := strconv.Atoi(strings.TrimSpace(fmt.Sprint(value))); err == nil {
			return parsed
		}
	}
	return fallback
}

// parseComponentArgs 解析 `key: 表达式, key: 表达式`。逗号与冒号只在引号、括号之外生效。
func parseComponentArgs(ctx render.Context) (componentArgs, error) {
	source := strings.TrimSpace(ctx.TagArgs())
	args := componentArgs{}
	if source == "" {
		return args, nil
	}
	for _, part := range splitTopLevel(source, ',') {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		pieces := splitTopLevel(part, ':')
		if len(pieces) < 2 {
			return nil, fmt.Errorf("%s 的参数要写成 名称: 值，例如 url: reset_url（收到 %q）", ctx.TagName(), part)
		}
		key := strings.TrimSpace(pieces[0])
		expression := strings.TrimSpace(strings.Join(pieces[1:], ":"))
		value, err := ctx.EvaluateString(expression)
		if err != nil {
			return nil, fmt.Errorf("%s 的参数 %s 无法求值：%v", ctx.TagName(), key, err)
		}
		args[key] = value
	}
	return args, nil
}

func splitTopLevel(source string, sep rune) []string {
	var parts []string
	var quote rune
	depth := 0
	start := 0
	for i, r := range source {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '"' || r == '\'':
			quote = r
		case r == '(' || r == '[':
			depth++
		case r == ')' || r == ']':
			depth--
		case r == sep && depth == 0:
			parts = append(parts, source[start:i])
			start = i + 1
			if sep == ':' {
				// 冒号只切第一处：值里可能有 https://
				parts = append(parts, source[start:])
				return parts
			}
		}
	}
	return append(parts, source[start:])
}

func themeOf(ctx render.Context) map[string]any {
	if theme, ok := ctx.Get("theme").(map[string]any); ok {
		return theme
	}
	return DefaultTheme().palette()
}

func themeValue(theme map[string]any, key string) string {
	return fmt.Sprint(theme[key])
}

func esc(value string) string { return html.EscapeString(value) }

// escMultiline 转义并保留换行。
func escMultiline(value string) string {
	return strings.ReplaceAll(esc(strings.TrimSpace(value)), "\n", "<br />")
}

// ── 组件 ──

func renderCodeComponent(ctx render.Context, args componentArgs, theme map[string]any) (string, error) {
	code := args.str("value", fmt.Sprint(valueOr(ctx.Get("code"), "")))
	note := args.str("note", "")
	if !args.has("note") {
		if minutes := strings.TrimSpace(fmt.Sprint(valueOr(ctx.Get("minutes"), ""))); minutes != "" && minutes != "0" {
			note = minutes + " 分钟内有效"
		}
	}
	return renderCodeBox(code, note, theme), nil
}

func renderCodeBox(code, note string, theme map[string]any) string {
	var b strings.Builder
	radius := themeValue(theme, "inner_radius")
	tint := themeValue(theme, "accent_tint")
	notePart := ""
	if note != "" {
		notePart = fmt.Sprintf(`<div style="margin-top:10px;font-size:13px;line-height:20px;color:%s;letter-spacing:0;">%s</div>`,
			themeValue(theme, "muted"), esc(note))
	}
	// 验证码框用 mj-table 画：Outlook 也认 td 背景；圆角在支持的客户端上生效
	fmt.Fprintf(&b, `<mj-table padding="4px 0 24px 0" cellpadding="0" cellspacing="0" width="100%%">
<tr><td align="center" bgcolor="%[1]s" style="background-color:%[1]s;border:1px solid %[2]s;border-radius:%[3]spx;padding:24px 16px;">
<div style="font-family:%[4]s;font-size:34px;line-height:40px;font-weight:700;letter-spacing:10px;padding-left:10px;color:%[5]s;">%[6]s</div>%[7]s
</td></tr>
</mj-table>`, tint, themeValue(theme, "accent_border"), radius, themeValue(theme, "mono"), themeValue(theme, "accent_strong"), esc(code), notePart)
	return b.String()
}

func renderButtonComponent(_ render.Context, args componentArgs, theme map[string]any) (string, error) {
	url := args.str("url", "")
	label := args.str("label", "打开")
	if url == "" {
		return "", fmt.Errorf("button 需要 url 参数")
	}
	align := args.str("align", "left")
	if align != "center" && align != "right" {
		align = "left"
	}
	return renderButton(label, url, align, theme), nil
}

func renderButton(label, url, align string, theme map[string]any) string {
	return fmt.Sprintf(`<mj-button href="%s" align="%s" background-color="%s" color="%s" border-radius="%spx" font-size="15px" font-weight="600" line-height="20px" inner-padding="14px 28px" padding="4px 0 24px 0">%s</mj-button>`,
		esc(url), align, themeValue(theme, "accent"), themeValue(theme, "accent_text"), themeValue(theme, "button_radius"), esc(label))
}

func renderDetailsComponent(ctx render.Context, args componentArgs, theme map[string]any) (string, error) {
	rows := args["rows"]
	if !args.has("rows") {
		rows = ctx.Get("details")
	}
	return renderDetails(toRows(rows), theme), nil
}

func toRows(value any) []Row {
	items, ok := value.([]any)
	if !ok {
		if typed, ok := value.([]map[string]any); ok {
			for _, item := range typed {
				items = append(items, item)
			}
		}
	}
	rows := make([]Row, 0, len(items))
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		rows = append(rows, Row{Label: fmt.Sprint(valueOr(entry["label"], "")), Value: fmt.Sprint(valueOr(entry["value"], ""))})
	}
	return rows
}

func renderDetails(rows []Row, theme map[string]any) string {
	if len(rows) == 0 {
		return ""
	}
	var b strings.Builder
	border := themeValue(theme, "border")
	fmt.Fprintf(&b, `<mj-table padding="0 0 24px 0" cellpadding="0" cellspacing="0" width="100%%" font-size="14px" line-height="22px" color="%s">`, themeValue(theme, "text"))
	fmt.Fprintf(&b, `<tr><td colspan="2" bgcolor="%[1]s" style="background-color:%[1]s;border-radius:%[2]spx;padding:4px 20px;">`, themeValue(theme, "subtle"), themeValue(theme, "inner_radius"))
	b.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="border-collapse:collapse;">`)
	for i, row := range rows {
		line := ""
		if i < len(rows)-1 {
			line = "border-bottom:1px solid " + border + ";"
		}
		fmt.Fprintf(&b, `<tr><td valign="top" style="padding:12px 16px 12px 0;%s color:%s;white-space:nowrap;width:30%%;">%s</td><td valign="top" align="right" style="padding:12px 0;%s text-align:right;word-break:break-all;font-weight:500;">%s</td></tr>`,
			line, themeValue(theme, "muted"), esc(row.Label), line, esc(valueText(row.Value)))
	}
	b.WriteString(`</table></td></tr></mj-table>`)
	return b.String()
}

func valueText(value string) string {
	if strings.TrimSpace(value) == "" {
		return "—"
	}
	return value
}

func renderNoticeComponent(_ render.Context, args componentArgs, theme map[string]any) (string, error) {
	return renderNotice(args.str("title", ""), args.str("text", ""), args.str("tone", "accent"), theme), nil
}

func renderNotice(title, text, tone string, theme map[string]any) string {
	bar := themeValue(theme, "accent")
	if tone == "warning" {
		bar = "#f59e0b"
	} else if tone == "danger" {
		bar = "#ef4444"
	}
	titlePart := ""
	if title != "" {
		titlePart = fmt.Sprintf(`<div style="font-weight:600;margin-bottom:4px;color:%s;">%s</div>`, themeValue(theme, "text"), esc(title))
	}
	return fmt.Sprintf(`<mj-table padding="0 0 24px 0" cellpadding="0" cellspacing="0" width="100%%">
<tr><td bgcolor="%[1]s" style="background-color:%[1]s;border-left:3px solid %[2]s;border-radius:%[3]spx;padding:14px 18px;font-size:14px;line-height:22px;color:%[4]s;">%[5]s%[6]s</td></tr>
</mj-table>`, themeValue(theme, "subtle"), bar, themeValue(theme, "inner_radius"), themeValue(theme, "muted"), titlePart, escMultiline(text))
}

func renderLinkComponent(_ render.Context, args componentArgs, theme map[string]any) (string, error) {
	url := args.str("url", "")
	if url == "" {
		return "", fmt.Errorf("link 需要 url 参数")
	}
	return renderLink(args.str("label", ""), url, theme), nil
}

func renderLink(label, url string, theme map[string]any) string {
	labelPart := ""
	if label != "" {
		labelPart = esc(label) + "<br />"
	}
	return fmt.Sprintf(`<mj-text padding="0 0 24px 0" font-size="12px" line-height="20px" color="%s">%s<a href="%s" style="color:%s;font-family:%s;word-break:break-all;text-decoration:none;">%s</a></mj-text>`,
		themeValue(theme, "muted"), labelPart, esc(url), themeValue(theme, "accent_strong"), themeValue(theme, "mono"), esc(url))
}

func renderParagraphComponent(_ render.Context, args componentArgs, theme map[string]any) (string, error) {
	return renderParagraph(args.str("text", ""), args.str("tone", ""), theme), nil
}

func renderParagraph(text, tone string, theme map[string]any) string {
	color := themeValue(theme, "text")
	size := "15px"
	if tone == "muted" {
		color, size = themeValue(theme, "muted"), "14px"
	}
	return fmt.Sprintf(`<mj-text padding="0 0 18px 0" font-size="%s" line-height="1.75" color="%s">%s</mj-text>`, size, color, escMultiline(text))
}

func renderImageComponent(_ render.Context, args componentArgs, theme map[string]any) (string, error) {
	src := args.str("src", "")
	if !strings.HasPrefix(src, "https://") {
		return "", fmt.Errorf("image 的 src 必须是 https 地址")
	}
	width := clamp(args.num("width", 0), 0, 600)
	widthAttr := ""
	if width > 0 {
		widthAttr = fmt.Sprintf(` width="%dpx"`, width)
	}
	href := ""
	if link := args.str("href", ""); link != "" {
		href = fmt.Sprintf(` href="%s"`, esc(link))
	}
	return fmt.Sprintf(`<mj-image src="%s" alt="%s"%s%s border-radius="%spx" padding="0 0 24px 0" />`,
		esc(src), esc(args.str("alt", "")), widthAttr, href, themeValue(theme, "inner_radius")), nil
}

func renderDividerComponent(_ render.Context, _ componentArgs, theme map[string]any) (string, error) {
	return fmt.Sprintf(`<mj-divider border-color="%s" border-width="1px" padding="4px 0 24px 0" />`, themeValue(theme, "border")), nil
}

func renderSpacerComponent(_ render.Context, args componentArgs, _ map[string]any) (string, error) {
	return fmt.Sprintf(`<mj-spacer height="%dpx" />`, clamp(args.num("height", 16), 0, 160)), nil
}

func renderBlocksComponent(ctx render.Context, args componentArgs, theme map[string]any) (string, error) {
	source := args["blocks"]
	if !args.has("blocks") {
		source = ctx.Get("blocks")
	}
	items, _ := source.([]any)
	var b strings.Builder
	for _, item := range items {
		block, ok := item.(map[string]any)
		if !ok {
			continue
		}
		get := func(key string) string { return strings.TrimSpace(fmt.Sprint(valueOr(block[key], ""))) }
		switch get("kind") {
		case "paragraph":
			b.WriteString(renderParagraph(get("text"), "", theme))
		case "code":
			b.WriteString(renderCodeBox(get("code"), get("note"), theme))
		case "details":
			b.WriteString(renderDetails(toRows(block["rows"]), theme))
		case "notice":
			b.WriteString(renderNotice(get("title"), get("text"), "accent", theme))
		case "button":
			if url := get("url"); url != "" {
				b.WriteString(renderButton(get("label"), url, "left", theme))
			}
		case "link":
			if url := get("url"); url != "" {
				b.WriteString(renderLink(get("label"), url, theme))
			}
		}
		b.WriteString("\n")
	}
	return b.String(), nil
}

var markdown = goldmark.New(goldmark.WithExtensions(extension.Linkify, extension.Strikethrough))

// renderMarkdownComponent 正文里的一段 Markdown。块内的 {{ 变量 }} 已经按 HTML 转义；
// goldmark 默认不放行原始 HTML，因此管理员写的 Markdown 只会产出标准的段落、列表、链接与强调。
func renderMarkdownComponent(source string, theme map[string]any) (string, error) {
	var out bytes.Buffer
	if err := markdown.Convert([]byte(html.UnescapeString(dedent(source))), &out); err != nil {
		return "", err
	}
	rendered := out.String()
	accent := themeValue(theme, "accent_strong")
	// 邮件客户端不认外部样式，给常用元素补上内联样式
	replacer := strings.NewReplacer(
		"<p>", `<p style="margin:0 0 14px 0;">`,
		"<ul>", `<ul style="margin:0 0 14px 0;padding-left:22px;">`,
		"<ol>", `<ol style="margin:0 0 14px 0;padding-left:22px;">`,
		"<li>", `<li style="margin:0 0 6px 0;">`,
		"<a href=", `<a style="color:`+accent+`;text-decoration:underline;" href=`,
		"<code>", `<code style="font-family:`+themeValue(theme, "mono")+`;font-size:13px;background-color:`+themeValue(theme, "subtle")+`;padding:2px 5px;border-radius:4px;">`,
		"<blockquote>", `<blockquote style="margin:0 0 14px 0;padding:2px 0 2px 14px;border-left:3px solid `+themeValue(theme, "border")+`;color:`+themeValue(theme, "muted")+`;">`,
		"<h1>", `<h1 style="margin:0 0 12px 0;font-size:20px;line-height:28px;">`,
		"<h2>", `<h2 style="margin:0 0 12px 0;font-size:18px;line-height:26px;">`,
		"<h3>", `<h3 style="margin:0 0 10px 0;font-size:16px;line-height:24px;">`,
	)
	return fmt.Sprintf(`<mj-text padding="0 0 10px 0" font-size="15px" line-height="1.75" color="%s">%s</mj-text>`,
		themeValue(theme, "text"), replacer.Replace(rendered)), nil
}

// dedent 去掉 Markdown 块的公共缩进：模板里块内容通常整体缩进，原样交给 Markdown 会变成代码块。
func dedent(source string) string {
	lines := strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n")
	indent := -1
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		width := len(line) - len(strings.TrimLeft(line, " \t"))
		if indent < 0 || width < indent {
			indent = width
		}
	}
	if indent <= 0 {
		return strings.TrimSpace(source)
	}
	for i, line := range lines {
		if len(line) >= indent {
			lines[i] = line[indent:]
		} else {
			lines[i] = strings.TrimSpace(line)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func valueOr(value any, fallback any) any {
	if value == nil {
		return fallback
	}
	return value
}
