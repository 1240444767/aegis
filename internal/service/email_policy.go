package service

import (
	emaildomain "aegis/internal/domain/email"
	apperrors "aegis/pkg/errors"
	"context"
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

// EmailSceneCatalog 场景目录接口的出参。
type EmailSceneCatalog struct {
	Scenes     []emaildomain.Scene `json:"scenes"`
	Categories []EmailSceneGroup   `json:"categories"`
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
	return EmailSceneCatalog{Scenes: emaildomain.Scenes(), Categories: groups}
}

// ── 品牌 ──

// mailBrand 策略里的落款，渲染时叠到 emailLayout 上。
type mailBrand struct {
	Name         string
	LogoURL      string
	Accent       string
	SupportEmail string
	FooterText   string
}

func brandFromPolicy(policy emaildomain.Policy, scopeName string) mailBrand {
	return mailBrand{
		Name:         firstNonEmpty(policy.Brand.DisplayName, scopeName),
		LogoURL:      policy.Brand.LogoURL,
		Accent:       policy.Brand.AccentColor,
		SupportEmail: policy.Brand.SupportEmail,
		FooterText:   policy.Brand.FooterText,
	}
}

// brandFor 作用域的落款；凭证邮件这类自己拼正文的调用方用它补上 Logo 与强调色。
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
}

// ── 按场景合成 ──

// sceneMail 一封按场景渲染的信：正文积木由调用方给，文案来自目录与策略。
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
func composeScene(policy emaildomain.Policy, brand mailBrand, mail sceneMail) (string, string, string) {
	scene, ok := emaildomain.FindScene(mail.Scene)
	if !ok {
		scene, _ = emaildomain.FindScene(emaildomain.SceneCodeGeneric)
	}
	vars := map[string]string{"app": brand.Name}
	for key, value := range mail.Vars {
		vars[key] = value
	}
	copyText := policy.ResolveCopy(scene, vars)
	if mail.LeadDefault != "" && policy.Scenes[scene.Key].Lead == "" {
		copyText.Lead = mail.LeadDefault
	}
	layout := emailLayout{
		Eyebrow:    mail.Eyebrow,
		Title:      copyText.Title,
		Lead:       copyText.Lead,
		Preheader:  mail.Preheader,
		Blocks:     mail.Blocks,
		FooterNote: copyText.Footer,
	}
	brand.apply(&layout)
	html, text := renderEmail(layout)
	return copyText.Subject, html, text
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
			mailParagraph("请不要把这串数字转发给任何人。"),
		},
	}
}

// ── 预览 ──

// EmailPreviewRequest 控制台的模板预览：拿草稿策略渲染一个场景，不落库、不发信。
type EmailPreviewRequest struct {
	Scene  string
	Policy *emaildomain.Policy
}

// EmailPreview 预览结果。
type EmailPreview struct {
	Scene   string `json:"scene"`
	Subject string `json:"subject"`
	HTML    string `json:"html"`
	Text    string `json:"text"`
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
	sample := scene.SampleVars(brand.Name)
	subject, html, text := composeScene(policy, brand, sampleSceneMail(scene, sample, policy))
	return &EmailPreview{Scene: scene.Key, Subject: subject, HTML: html, Text: text}, nil
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
