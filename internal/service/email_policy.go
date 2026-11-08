package service

import (
	emaildomain "aegis/internal/domain/email"
	"aegis/internal/mailtpl"
	apperrors "aegis/pkg/errors"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"go.uber.org/zap"
)

// 邮件策略：品牌、验证码规则、场景开关与文案覆盖、退信抑制、重置链接白名单。
// 结构与校验在 domain/email/policy.go，场景目录在 domain/email/scenes.go。

// EmailPolicyView 策略接口的出参：策略本身 + 编辑器需要的目录与取值范围。
type EmailPolicyView struct {
	emaildomain.PolicyRecord
	// ScopeName 留空品牌名时邮件里实际用的名字
	ScopeName string             `json:"scopeName"`
	Defaults  emaildomain.Policy `json:"defaults"`
	Limits    map[string][2]int  `json:"limits"`
}

// GetPolicy 作用域的邮件策略。
func (s *EmailService) GetPolicy(ctx context.Context, appID int64) (*EmailPolicyView, error) {
	if err := s.ensureScope(ctx, appID); err != nil {
		return nil, err
	}
	record, err := s.pg.GetEmailPolicy(ctx, appID)
	if err != nil {
		return nil, err
	}
	scopeName, err := s.scopeName(ctx, appID)
	if err != nil {
		return nil, err
	}
	return &EmailPolicyView{
		PolicyRecord: *record,
		ScopeName:    scopeName,
		Defaults:     emaildomain.DefaultPolicy(),
		Limits:       emaildomain.PolicyLimits(),
	}, nil
}

// SavePolicy 校验并保存策略。
func (s *EmailService) SavePolicy(ctx context.Context, appID int64, policy emaildomain.Policy, operator string) (*EmailPolicyView, error) {
	if err := s.ensureScope(ctx, appID); err != nil {
		return nil, err
	}
	if err := emaildomain.ValidatePolicy(policy); err != nil {
		return nil, apperrors.New(42292, http.StatusUnprocessableEntity, err.Error())
	}
	scopeName, err := s.scopeName(ctx, appID)
	if err != nil {
		return nil, err
	}
	if err := validatePolicyTemplates(ctx, emaildomain.NormalizePolicy(policy), scopeName); err != nil {
		return nil, err
	}
	if _, err := s.pg.SaveEmailPolicy(ctx, appID, emaildomain.NormalizePolicy(policy), strings.TrimSpace(operator)); err != nil {
		return nil, err
	}
	return s.GetPolicy(ctx, appID)
}

// policyFor 发信链路取策略。读库失败时按默认策略继续发 —— 为一张策略表的抖动让验证码发不出去是本末倒置。
func (s *EmailService) policyFor(ctx context.Context, appID int64) emaildomain.Policy {
	if s.pg == nil {
		return emaildomain.DefaultPolicy()
	}
	record, err := s.pg.GetEmailPolicy(ctx, appID)
	if err != nil || record == nil {
		if err != nil {
			s.log.Warn("load email policy failed, using defaults", zap.Int64("appid", appID), zap.Error(err))
		}
		return emaildomain.DefaultPolicy()
	}
	return record.Policy
}

// SceneEnabled 可关闭的场景此刻是否开着。
func (s *EmailService) SceneEnabled(ctx context.Context, appID int64, sceneKey string) bool {
	return s.policyFor(ctx, appID).SceneEnabled(sceneKey)
}

// EmailSceneCatalog 场景目录接口的出参：场景、分组、内置模板源码、模板语言参考与主题可选值。
type EmailSceneCatalog struct {
	Scenes     []emaildomain.Scene `json:"scenes"`
	Categories []EmailSceneGroup   `json:"categories"`
	Templates  EmailBuiltinSources `json:"templates"`
	Reference  mailtpl.Reference   `json:"reference"`
	Theme      EmailThemeCatalog   `json:"theme"`
}

// EmailBuiltinSources 内置模板源码：版式、纯文本，以及每个场景的正文。
type EmailBuiltinSources struct {
	Layout string            `json:"layout"`
	Text   string            `json:"text"`
	Bodies map[string]string `json:"bodies"`
}

// EmailThemeCatalog 主题的默认值与可选值。
type EmailThemeCatalog struct {
	Defaults EmailThemeDefaults `json:"defaults"`
	Fonts    []string           `json:"fonts"`
	Headers  []string           `json:"headers"`
}

type EmailThemeDefaults struct {
	Accent     string `json:"accent"`
	Background string `json:"background"`
	Card       string `json:"card"`
	Radius     int    `json:"radius"`
	Font       string `json:"font"`
	LogoAlign  string `json:"logoAlign"`
	LogoHeight int    `json:"logoHeight"`
	Header     string `json:"header"`
}

type EmailSceneGroup struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// SceneCatalog 全部场景与分组。静态目录。
func (s *EmailService) SceneCatalog() EmailSceneCatalog {
	groups := make([]EmailSceneGroup, 0, len(emaildomain.SceneCategoryOrder))
	for _, key := range emaildomain.SceneCategoryOrder {
		groups = append(groups, EmailSceneGroup{Key: key, Name: emaildomain.SceneCategoryNames[key]})
	}
	scenes := emaildomain.Scenes()
	bodies := make(map[string]string, len(scenes))
	for _, scene := range scenes {
		bodies[scene.Key] = mailtpl.BuiltinBody(scene.Key)
	}
	bodies["blocks"] = mailtpl.BuiltinBody("blocks")
	theme := mailtpl.DefaultTheme()
	return EmailSceneCatalog{
		Scenes:     scenes,
		Categories: groups,
		Templates:  EmailBuiltinSources{Layout: mailtpl.BuiltinLayout(), Text: mailtpl.BuiltinText(), Bodies: bodies},
		Reference:  mailtpl.TemplateReference(),
		Theme: EmailThemeCatalog{
			Defaults: EmailThemeDefaults{
				Accent: theme.Accent, Background: theme.Background, Card: theme.Card, Radius: theme.Radius,
				Font: theme.Font, LogoAlign: theme.LogoAlign, LogoHeight: theme.LogoHeight, Header: theme.Header,
			},
			Fonts:   mailtpl.FontPresets(),
			Headers: mailtpl.HeaderStyles(),
		},
	}
}

// ── 品牌 ──

// mailBrand 策略里的落款、主题与自定义版式，渲染时叠到 emailLayout 上。
type mailBrand struct {
	Name         string
	LogoURL      string
	Accent       string
	SupportEmail string
	FooterText   string
	Theme        mailtpl.Theme
	// Layout 生效中的自定义版式源码，空即内置版式
	Layout string
}

func brandFromPolicy(policy emaildomain.Policy, scopeName string) mailBrand {
	layout := ""
	if policy.Layout.Enabled {
		layout = policy.Layout.Source
	}
	return mailBrand{
		Name:         firstNonEmpty(policy.Brand.DisplayName, scopeName),
		LogoURL:      policy.Brand.LogoURL,
		Accent:       policy.Brand.AccentColor,
		SupportEmail: policy.Brand.SupportEmail,
		FooterText:   policy.Brand.FooterText,
		Theme:        themeFromPolicy(policy),
		Layout:       layout,
	}
}

// themeFromPolicy 策略里的主题设置叠到内置主题上。
func themeFromPolicy(policy emaildomain.Policy) mailtpl.Theme {
	theme := mailtpl.DefaultTheme()
	settings := policy.Theme
	if accent := strings.TrimSpace(policy.Brand.AccentColor); accent != "" {
		theme.Accent = accent
	}
	if settings.Background != "" {
		theme.Background = settings.Background
	}
	if settings.Card != "" {
		theme.Card = settings.Card
	}
	if settings.Radius != nil {
		theme.Radius = *settings.Radius
	}
	if settings.Font != "" {
		theme.Font = settings.Font
	}
	if settings.LogoAlign != "" {
		theme.LogoAlign = settings.LogoAlign
	}
	if settings.LogoHeight > 0 {
		theme.LogoHeight = settings.LogoHeight
	}
	if settings.Header != "" {
		theme.Header = settings.Header
	}
	return theme.Normalize()
}

// brandFor 作用域的落款；凭证邮件这类自己拼正文的调用方用它补上品牌、主题与版式。
func (s *EmailService) brandFor(ctx context.Context, appID int64) mailBrand {
	scopeName, err := s.scopeName(ctx, appID)
	if err != nil {
		scopeName = ""
	}
	return brandFromPolicy(s.policyFor(ctx, appID), scopeName)
}

// apply 把落款写进版式。AppName 只在调用方没给时覆盖：凭证邮件的品牌行有自己的取法。
func (b mailBrand) apply(layout *emailLayout) {
	if strings.TrimSpace(layout.AppName) == "" || b.Name != "" {
		layout.AppName = firstNonEmpty(b.Name, layout.AppName)
	}
	layout.LogoURL = b.LogoURL
	layout.Accent = b.Accent
	layout.SupportEmail = b.SupportEmail
	layout.FooterText = b.FooterText
	layout.Theme = b.Theme
	layout.LayoutSource = b.Layout
}

// ── 按场景合成 ──

// sceneMail 一封按场景渲染的信：正文积木由调用方给，文案来自目录与策略，模板来自内置正文或策略里的自定义模板。
type sceneMail struct {
	Scene     string
	Vars      map[string]string
	Blocks    []mailBlock
	Eyebrow   string
	Preheader string
	// LeadDefault 调用方对默认引导句的改写（例如带上「N 个设备已退出」）；
	// 管理员改写过引导句时不生效，以管理员的为准
	LeadDefault string
}

// composeScene 渲染一封场景邮件，返回主题、HTML 与纯文本。
//
// 文案（主题、标题、引导语、页脚提示）是 Liquid：默认文案里的 {{app}} {{code}} 与管理员写的
// {{ brand.name | upcase }} 走同一个引擎。自定义模板与文案都在保存时校验过，发信时仍出错就退回默认，
// 不让一封验证码因为模板问题发不出去。
func composeScene(policy emaildomain.Policy, brand mailBrand, mail sceneMail) (string, string, string) {
	prepared := prepareScene(policy, brand, mail)
	html, text := renderEmailWith(prepared.layout, prepared.subject, prepared.templates, prepared.vars)
	return prepared.subject, html, text
}

// preparedScene 场景邮件渲染前的全部输入。
type preparedScene struct {
	subject   string
	layout    emailLayout
	templates mailtpl.Templates
	vars      map[string]any
}

func prepareScene(policy emaildomain.Policy, brand mailBrand, mail sceneMail) preparedScene {
	scene, ok := emaildomain.FindScene(mail.Scene)
	if !ok {
		scene, _ = emaildomain.FindScene(emaildomain.SceneCodeGeneric)
	}
	vars := sceneVars(brand, mail)
	setting := policy.Scenes[scene.Key]
	engine := mailtpl.Default()
	renderCopy := func(override, fallback string) string {
		if override != "" {
			if out, err := engine.RenderPlainWith(mailtpl.PartCopy, override, vars); err == nil {
				return strings.TrimSpace(out)
			}
		}
		out, err := engine.RenderPlainWith(mailtpl.PartCopy, fallback, vars)
		if err != nil {
			return fallback
		}
		return strings.TrimSpace(out)
	}
	subject := strings.Join(strings.Fields(renderCopy(setting.Subject, scene.Defaults.Subject)), " ")
	lead := renderCopy(setting.Lead, scene.Defaults.Lead)
	if mail.LeadDefault != "" && setting.Lead == "" {
		lead = mail.LeadDefault
	}
	eyebrow := mail.Eyebrow
	if eyebrow == "" {
		eyebrow = scene.CategoryName
	}
	layout := emailLayout{
		Eyebrow:    eyebrow,
		Title:      renderCopy(setting.Title, scene.Defaults.Title),
		Lead:       lead,
		Preheader:  mail.Preheader,
		Blocks:     mail.Blocks,
		FooterNote: renderCopy(setting.Footer, scene.Defaults.Footer),
	}
	brand.apply(&layout)
	templates := mailtpl.Templates{BodyKey: scene.Key, Layout: layout.LayoutSource}
	if custom := setting.CustomTemplate(); custom != nil {
		templates.Body, templates.Format, templates.Text = custom.Body, custom.Format, custom.Text
	}
	return preparedScene{subject: subject, layout: layout, templates: templates, vars: vars}
}

// sceneVars 交给场景模板的变量：调用方给的场景变量，加上从积木里取出的字段表与链接。
func sceneVars(brand mailBrand, mail sceneMail) map[string]any {
	vars := map[string]any{"app": brand.Name}
	for key, value := range mail.Vars {
		vars[key] = value
	}
	for _, block := range mail.Blocks {
		switch block.Kind {
		case "details":
			if _, ok := vars["details"]; !ok {
				rows := make([]mailtpl.Row, 0, len(block.Details))
				for _, detail := range block.Details {
					rows = append(rows, mailtpl.Row{Label: detail.Label, Value: detail.Value})
				}
				vars["details"] = mailtpl.RowsBinding(rows)
			}
		case "button":
			if _, ok := vars["reset_url"]; !ok && block.URL != "" {
				vars["reset_url"] = block.URL
			}
		}
	}
	if _, ok := vars["details"]; !ok {
		vars["details"] = []any{}
	}
	return vars
}

// codeSceneMail 验证码邮件的场景描述。
func codeSceneMail(purpose string, code string, expireMinutes int, email string) sceneMail {
	scene := emaildomain.CodeSceneForPurpose(purpose)
	trimmed := strings.TrimSpace(code)
	return sceneMail{
		Scene: scene.Key,
		Vars: map[string]string{
			"code":    trimmed,
			"minutes": strconv.Itoa(expireMinutes),
			"email":   strings.TrimSpace(email),
		},
		Preheader: fmt.Sprintf("验证码 %s，%d 分钟内有效", trimmed, expireMinutes),
		Blocks: []mailBlock{
			mailCode(trimmed, expireMinutes),
			mailParagraph("请不要把这串数字转发给任何人，包括自称客服的人。"),
		},
	}
}

// ── 预览 ──

// EmailPreviewRequest 控制台的模板预览：拿草稿策略渲染一个场景，不落库、不发信。
type EmailPreviewRequest struct {
	Scene  string
	Policy *emaildomain.Policy
}

// EmailPreview 预览结果。模板有问题时 HTML 可能为空，Problems 给出每条问题的位置（编辑器据此标红）。
type EmailPreview struct {
	Scene    string                  `json:"scene"`
	Subject  string                  `json:"subject"`
	HTML     string                  `json:"html"`
	Text     string                  `json:"text"`
	Problems []mailtpl.TemplateError `json:"problems"`
}

// Preview 用草稿策略（没给就用已保存的）渲染一个场景的示例邮件。
func (s *EmailService) Preview(ctx context.Context, appID int64, request EmailPreviewRequest) (*EmailPreview, error) {
	if err := s.ensureScope(ctx, appID); err != nil {
		return nil, err
	}
	scene, ok := emaildomain.FindScene(request.Scene)
	if !ok {
		return nil, apperrors.New(40466, http.StatusNotFound, "邮件场景不存在")
	}
	policy := s.policyFor(ctx, appID)
	if request.Policy != nil {
		if err := emaildomain.ValidatePolicy(*request.Policy); err != nil {
			return nil, apperrors.New(42292, http.StatusUnprocessableEntity, err.Error())
		}
		policy = emaildomain.NormalizePolicy(*request.Policy)
	}
	scopeName, err := s.scopeName(ctx, appID)
	if err != nil {
		return nil, err
	}
	brand := brandFromPolicy(policy, scopeName)
	prepared := prepareScene(policy, brand, sampleSceneMail(scene, scene.SampleVars(brand.Name), policy))
	theme, message := prepared.layout.theme(), prepared.layout.message(prepared.subject, prepared.vars)
	preview := &EmailPreview{Scene: scene.Key, Subject: prepared.subject, Problems: []mailtpl.TemplateError{}}
	// 预览不走发信时的「出错退回默认」：管理员正在改的就是这份模板，问题要原样告诉他
	engine := mailtpl.Default()
	result, err := engine.Render(ctx, theme, message, prepared.templates)
	if err != nil {
		var templateErr *mailtpl.TemplateError
		if errors.As(err, &templateErr) {
			preview.Problems = append(preview.Problems, *templateErr)
			return preview, nil
		}
		return nil, err
	}
	preview.HTML, preview.Text = result.HTML, result.Text
	// 用了自定义模板时再按 MJML 严格模式过一遍：能渲染出来的模板也可能有标签放错位置
	if prepared.templates.Body != "" || prepared.templates.Layout != "" {
		preview.Problems = append(preview.Problems, engine.Validate(ctx, theme, message, prepared.templates)...)
	}
	return preview, nil
}

// validatePolicyTemplates 保存前把策略里的每一段 Liquid 与 MJML 都跑一遍：
// 文案的语法、自定义版式、每个启用的场景模板（用示例数据渲染并按 MJML 严格模式校验）。
func validatePolicyTemplates(ctx context.Context, policy emaildomain.Policy, scopeName string) error {
	engine := mailtpl.Default()
	brand := brandFromPolicy(policy, scopeName)
	for _, scene := range emaildomain.Scenes() {
		setting, ok := policy.Scenes[scene.Key]
		if !ok && !policy.Layout.Enabled {
			continue
		}
		for _, field := range []struct{ label, value string }{
			{"主题", setting.Subject}, {"标题", setting.Title}, {"引导语", setting.Lead}, {"页脚提示", setting.Footer},
		} {
			if field.value == "" {
				continue
			}
			if err := engine.Check(mailtpl.PartCopy, field.value); err != nil {
				return templateAppError(scene.Name+"的"+field.label, err)
			}
		}
		// 自定义版式影响每一个场景；只开了版式时也要逐个场景过一遍
		if setting.CustomTemplate() == nil && !policy.Layout.Enabled {
			continue
		}
		prepared := prepareScene(policy, brand, sampleSceneMail(scene, scene.SampleVars(brand.Name), policy))
		problems := engine.Validate(ctx, prepared.layout.theme(), prepared.layout.message(prepared.subject, prepared.vars), prepared.templates)
		if len(problems) > 0 {
			return templateAppError(scene.Name, &problems[0])
		}
	}
	if policy.Layout.Enabled {
		// 没有专属正文的信（通知、凭证、注销告知）也套这份版式
		sample := emailLayout{
			Title: "示例通知", Lead: "这是一封示例通知。", Blocks: []mailBlock{mailParagraph("正文内容"), mailNotice("提示", "说明文字")},
		}
		brand.apply(&sample)
		problems := engine.Validate(ctx, sample.theme(), sample.message("示例", nil),
			mailtpl.Templates{Layout: sample.LayoutSource, BodyKey: "blocks"})
		if len(problems) > 0 {
			return templateAppError("自定义版式", &problems[0])
		}
	}
	return nil
}

// templateAppError 把模板错误包成带场景名的 42292。
func templateAppError(owner string, err error) error {
	return apperrors.New(42292, http.StatusUnprocessableEntity, "「"+owner+"」"+err.Error())
}

// sampleSceneMail 每个场景的示例正文，与真实发信用的积木一致。
func sampleSceneMail(scene emaildomain.Scene, vars map[string]string, policy emaildomain.Policy) sceneMail {
	if scene.Category == emaildomain.SceneCategoryCode {
		// 示例码的位数跟着策略走，管理员改了位数能在预览里直接看到
		digits := "48291327"
		length := policy.Code.Length
		if length <= 0 || length > len(digits) {
			length = 6
		}
		code := digits[:length]
		purpose := ""
		if len(scene.Purposes) > 0 {
			purpose = scene.Purposes[0]
		}
		mail := codeSceneMail(purpose, code, policy.Code.TTLMinutes, vars["email"])
		mail.Scene = scene.Key
		return mail
	}
	mail := sceneMail{Scene: scene.Key, Vars: vars}
	switch scene.Key {
	case emaildomain.ScenePasswordResetLink:
		link := "https://example.com/reset?token=8f3c2a&email=" + vars["email"]
		mail.Blocks = passwordResetBlocks(link, passwordResetTTLMinutes)
		mail.Preheader = fmt.Sprintf("重置密码的链接 %d 分钟内有效", passwordResetTTLMinutes)
	case emaildomain.ScenePasswordResetNotice:
		mail.Blocks = []mailBlock{mailDetails(
			emailDetail{Label: "账号", Value: vars["account"]},
			emailDetail{Label: "时间", Value: "2026-10-18 09:30:12"},
			emailDetail{Label: "设备", Value: "iPhone 17 Pro"},
			emailDetail{Label: "IP 地址", Value: "203.0.113.24"},
			emailDetail{Label: "位置", Value: "上海"},
		)}
	case emaildomain.SceneProfileChange:
		mail.Blocks = []mailBlock{mailDetails(
			emailDetail{Label: "变更前", Value: "l***n@example.com"},
			emailDetail{Label: "变更后", Value: "n***w@example.com"},
		)}
	case emaildomain.SceneWelcome:
		mail.Blocks = []mailBlock{mailDetails(emailDetail{Label: "登录邮箱", Value: vars["email"]})}
		mail.LeadDefault = welcomeLead(vars["app"], vars["name"])
	}
	return mail
}
