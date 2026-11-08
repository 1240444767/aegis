package service

import (
	emaildomain "aegis/internal/domain/email"
	"aegis/internal/mailtpl"
	"context"
	"fmt"
	"strings"

	"go.uber.org/zap"
)

/*
邮件内容的 Go 侧模型：业务代码只描述内容（一段话、一张字段表、一个验证码、一个按钮），
长什么样由模板服务决定（internal/mailtpl：Liquid + MJML）。

	emailLayout + mailBlock  →  mailtpl.Message + Block  →  内置或自定义模板  →  HTML 与纯文本

每一封信都走模板服务：验证码、重置、告知、欢迎、通知、凭证、注销告知、通道自检。
作用域的品牌、主题与自定义版式由 mailBrand.apply 叠到 emailLayout 上（见 email_policy.go）。

文案每句只说一次事：一封验证码邮件只需要回答这是什么码、多久失效、不是本人怎么办。
*/

/* ── 内容模型 ── */

// emailDetail 字段行：左标签右值。
type emailDetail struct {
	Label string
	Value string
}

// mailBlock 正文积木。
type mailBlock struct {
	Kind    string
	Text    string
	Title   string
	Label   string
	Code    string
	Note    string
	URL     string
	Details []emailDetail
}

// LastIndex 字段表最后一行的下标。
func (b mailBlock) LastIndex() int { return len(b.Details) - 1 }

func mailParagraph(text string) mailBlock {
	return mailBlock{Kind: "paragraph", Text: strings.TrimSpace(text)}
}

func mailCode(code string, expireMinutes int) mailBlock {
	note := ""
	if expireMinutes > 0 {
		note = fmt.Sprintf("%d 分钟内有效", expireMinutes)
	}
	return mailBlock{Kind: "code", Code: strings.TrimSpace(code), Note: note}
}

func mailDetails(details ...emailDetail) mailBlock {
	filtered := make([]emailDetail, 0, len(details))
	for _, item := range details {
		if strings.TrimSpace(item.Label) == "" && strings.TrimSpace(item.Value) == "" {
			continue
		}
		filtered = append(filtered, emailDetail{
			Label: strings.TrimSpace(item.Label),
			Value: strings.TrimSpace(item.Value),
		})
	}
	return mailBlock{Kind: "details", Details: filtered}
}

func mailNotice(title string, text string) mailBlock {
	return mailBlock{Kind: "notice", Title: strings.TrimSpace(title), Text: strings.TrimSpace(text)}
}

func mailButton(label string, url string) mailBlock {
	return mailBlock{Kind: "button", Label: strings.TrimSpace(label), URL: strings.TrimSpace(url)}
}

func mailLink(label string, url string) mailBlock {
	return mailBlock{Kind: "link", Label: strings.TrimSpace(label), URL: strings.TrimSpace(url)}
}

// emailLayout 一封信的全部内容。
//
// 语言是参数而不是两份模板：凭证邮件要按收件人的语言出具（含 <html lang>
// 与「请勿回复」那句话），而平台自身的验证码 / 密码重置邮件仍是中文。
type emailLayout struct {
	// Lang HTML 语言标记，留空为 zh-CN
	Lang string
	// AppName 品牌名，出现在页眉与页脚
	AppName string
	// Eyebrow 标题上方的小字分类，如「账号安全」「收据」
	Eyebrow string
	Title   string
	Lead    string
	// Preheader 收件箱预览行，留空则退回 Lead
	Preheader string
	Blocks    []mailBlock
	// FooterNote 卡内末尾那句「不是本人怎么办」
	FooterNote string
	// NoReplyNote 页脚提示，留空用中文默认句
	NoReplyNote string

	// 以下来自邮件策略（见 mailBrand.apply），全部可选
	LogoURL      string
	Accent       string
	SupportEmail string
	FooterText   string
	// Theme 主题设计令牌；零值即内置主题
	Theme mailtpl.Theme
	// LayoutSource 生效中的自定义版式，空即内置版式
	LayoutSource string
}

// message 转成模板服务的内容模型。
func (layout emailLayout) message(subject string, vars map[string]any) mailtpl.Message {
	blocks := make([]mailtpl.Block, 0, len(layout.Blocks))
	for _, block := range layout.Blocks {
		rows := make([]mailtpl.Row, 0, len(block.Details))
		for _, detail := range block.Details {
			rows = append(rows, mailtpl.Row{Label: detail.Label, Value: detail.Value})
		}
		blocks = append(blocks, mailtpl.Block{
			Kind: block.Kind, Text: block.Text, Title: block.Title, Label: block.Label,
			Code: block.Code, Note: block.Note, URL: block.URL, Rows: rows,
		})
	}
	return mailtpl.Message{
		Lang:       layout.Lang,
		BrandName:  layout.AppName,
		LogoURL:    layout.LogoURL,
		Eyebrow:    layout.Eyebrow,
		Title:      layout.Title,
		Lead:       layout.Lead,
		Preheader:  layout.Preheader,
		FooterNote: layout.FooterNote,
		NoReply:    layout.NoReplyNote,
		Support:    layout.SupportEmail,
		FooterText: layout.FooterText,
		Subject:    subject,
		Blocks:     blocks,
		Vars:       vars,
	}
}

// theme 版式实际用的主题：策略主题叠上品牌强调色。
func (layout emailLayout) theme() mailtpl.Theme {
	theme := layout.Theme
	if theme == (mailtpl.Theme{}) {
		theme = mailtpl.DefaultTheme()
	}
	if accent := strings.TrimSpace(layout.Accent); accent != "" {
		theme.Accent = accent
	}
	return theme.Normalize()
}

/* ── 渲染 ── */

// renderEmail 用内置正文（按积木渲染）渲染一封信，返回 HTML 与纯文本。
func renderEmail(layout emailLayout) (string, string) {
	return renderEmailWith(layout, "", mailtpl.Templates{BodyKey: "blocks"}, nil)
}

// renderEmailWith 按给定的模板渲染。自定义模板在发信时出错（保存时已校验，出错多半是数据触发的）
// 不让这封信发不出去：依次退回内置正文、内置版式，并记一条 warn。
func renderEmailWith(layout emailLayout, subject string, tpl mailtpl.Templates, vars map[string]any) (string, string) {
	engine := mailtpl.Default()
	ctx := context.Background()
	message := layout.message(subject, vars)
	theme := layout.theme()
	if tpl.Layout == "" {
		tpl.Layout = layout.LayoutSource
	}
	attempts := []mailtpl.Templates{tpl}
	if tpl.Body != "" {
		attempts = append(attempts, mailtpl.Templates{Layout: tpl.Layout, BodyKey: tpl.BodyKey})
	}
	if tpl.Layout != "" {
		attempts = append(attempts, mailtpl.Templates{BodyKey: tpl.BodyKey})
	}
	for i, attempt := range attempts {
		result, err := engine.Render(ctx, theme, message, attempt)
		if err == nil {
			return result.HTML, result.Text
		}
		zap.L().Warn("render email template failed",
			zap.Int("attempt", i+1), zap.String("body", attempt.BodyKey), zap.Bool("customBody", attempt.Body != ""),
			zap.Bool("customLayout", attempt.Layout != ""), zap.Error(err))
	}
	return "", ""
}

// renderEmailLayoutWith 只要 HTML 的调用方用这个（通知、凭证）。
func renderEmailLayoutWith(layout emailLayout) string {
	html, _ := renderEmail(layout)
	return html
}

/* ── 验证码邮件 ── */

// emailPurposePresentation 一种验证码用途的默认文案（取自场景目录）。
type emailPurposePresentation struct {
	Noun   string
	Title  string
	Lead   string
	Footer string
}

// renderCodeMailContent 返回验证码邮件的主题、HTML 与纯文本（默认策略）。
//
// 主题把验证码放在最前面（「482913 是您的登录验证码」）：手机通知栏和
// 邮件列表都只显示开头十几个字，验证码放在那里往往不用点开邮件。
func renderCodeMailContent(appName string, configName string, purpose string, code string, expireMinutes int) (string, string, string) {
	return renderCodeMailWithPolicy(emaildomain.DefaultPolicy(), mailBrand{Name: appName}, configName, purpose, code, expireMinutes, "")
}

// renderCodeMailWithPolicy 按策略（文案、模板与品牌）渲染验证码邮件。
func renderCodeMailWithPolicy(policy emaildomain.Policy, brand mailBrand, configName string, purpose string, code string, expireMinutes int, email string) (string, string, string) {
	if normalizeEmailPurpose(purpose) == "test" {
		return renderChannelTestMail(brand, configName, code)
	}
	return composeScene(policy, brand, codeSceneMail(purpose, code, expireMinutes, email))
}

// renderChannelTestMail 通道自检邮件。
//
// 它不是验证码 —— 这串数字不写 Redis，验不了任何东西，只是让操作者把邮件里的
// 内容和控制台返回的值对一眼，确认中途没有被改写。写成「验证码」会让收到的人
// 以为账号上正在发生什么。自检信同样带上品牌、主题与自定义版式：管理员试发时顺带看到实际效果。
func renderChannelTestMail(brand mailBrand, configName string, code string) (string, string, string) {
	layout := emailLayout{
		Eyebrow:   "通道自检",
		Title:     "邮件通道工作正常",
		Lead:      "这封信由控制台手动触发，能收到就说明这条通道是通的。",
		Preheader: "通道自检邮件，收到即代表投递正常",
		Blocks: []mailBlock{
			mailDetails(
				emailDetail{Label: "发送通道", Value: fallbackEmailValue(configName, "默认配置")},
				emailDetail{Label: "校验串", Value: strings.TrimSpace(code)},
			),
			mailParagraph("控制台上显示的校验串与这里一致，说明正文在投递途中没有被改写。"),
		},
		FooterNote: "这封信不涉及任何账号操作，看完可以直接删除。",
	}
	brand.apply(&layout)
	html, text := renderEmail(layout)
	return fmt.Sprintf("%s 邮件通道自检", layout.AppName), html, text
}

func normalizeEmailPurpose(purpose string) string {
	return strings.ToLower(strings.TrimSpace(purpose))
}

// getEmailPurposePresentation 用途对应的默认文案，取自场景目录（domain/email/scenes.go）。
func getEmailPurposePresentation(purpose string) emailPurposePresentation {
	scene := emaildomain.CodeSceneForPurpose(purpose)
	return emailPurposePresentation{
		Noun:   strings.TrimPrefix(scene.Defaults.Subject, "{{code}} 是您的"),
		Title:  scene.Defaults.Title,
		Lead:   scene.Defaults.Lead,
		Footer: scene.Defaults.Footer,
	}
}

/* ── 密码重置 / 欢迎 / 资料变更 ── */

// passwordResetBlocks 重置链接邮件的积木（纯文本版与没有专属正文时用）；
// 网页版由内置正文 bodies/password_reset_link 画，两边措辞一致。
func passwordResetBlocks(resetURL string, expireMinutes int) []mailBlock {
	if strings.TrimSpace(resetURL) == "" {
		// 这是配置问题，不是收件人能处理的事，所以直说缺哪一项
		return []mailBlock{
			mailNotice("重置入口尚未配置", "这个应用还没有登记重置链接（resetBaseURL），暂时无法自助重置，请联系管理员。"),
		}
	}
	return []mailBlock{
		mailParagraph(fmt.Sprintf("点下面的按钮设置新密码，链接 %d 分钟后失效。", expireMinutes)),
		mailButton("设置新密码", resetURL),
		mailLink("按钮打不开的话，复制下面的链接到浏览器打开", resetURL),
	}
}

func welcomeLead(appName string, userName string) string {
	if name := strings.TrimSpace(userName); name != "" {
		return fmt.Sprintf("%s，您在 %s 的账号已经可以用了。", name, strings.TrimSpace(appName))
	}
	return fmt.Sprintf("您在 %s 的账号已经可以用了。", strings.TrimSpace(appName))
}

/* ── HTML → 纯文本（仅用于正文由外部给的一段 HTML） ── */

// htmlToPlainText 从 HTML 正文提取纯文本备份。模板渲染的信自带纯文本版，走不到这里。
func htmlToPlainText(source string) string { return mailtpl.HTMLToText(source) }

/* ── 小工具 ── */

func fallbackEmailValue(value string, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func describeProfileChangeField(field string) string {
	switch strings.ToLower(strings.TrimSpace(field)) {
	case "email":
		return "邮箱地址"
	case "phone":
		return "手机号码"
	default:
		return "资料信息"
	}
}

func maskProfileChangeNotificationValue(field string, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "未设置"
	}
	switch strings.ToLower(strings.TrimSpace(field)) {
	case "email":
		return maskEmailForNotification(value)
	case "phone":
		return maskPhoneForNotification(value)
	default:
		return value
	}
}

func maskEmailForNotification(value string) string {
	parts := strings.Split(value, "@")
	if len(parts) != 2 {
		return value
	}
	local := parts[0]
	if len(local) <= 1 {
		return "*@" + parts[1]
	}
	if len(local) == 2 {
		return local[:1] + "*@" + parts[1]
	}
	return local[:1] + strings.Repeat("*", len(local)-2) + local[len(local)-1:] + "@" + parts[1]
}

func maskPhoneForNotification(value string) string {
	if len(value) <= 7 {
		return "***"
	}
	return value[:3] + "****" + value[len(value)-4:]
}
