// Package mailtpl 是平台唯一的邮件模板服务：Liquid 负责数据与逻辑，MJML 负责版式。
//
// 为什么是这两样：
//
//   - MJML 是响应式邮件的事实标准。邮件客户端的排版能力停留在 2007 年（Outlook 用 Word 引擎，
//     不认 flex、不认 div 上的 max-width），手写能在各家客户端都不散架的表格 HTML 极易出错；
//     MJML 把「一栏、一个按钮、一张表」编译成经过大量客户端验证的表格结构，并自动内联样式。
//   - Liquid 是 Shopify、Klaviyo、Customer.io 等邮件平台通用的模板语言：条件、循环、过滤器齐全，
//     且**不能执行任意代码** —— 管理员写的模板只能读到我们交给它的数据。
//
// 一封信的渲染顺序：
//
//	正文（body，Liquid → MJML 片段）→ 塞进版式（layout，Liquid → 完整 MJML）→ MJML 编译 → HTML
//	纯文本（text，Liquid）单独渲染；没有给纯文本模板的自定义正文由 HTML 抓取补齐
//
// HTML 引擎开了自动转义：`{{ name }}` 一律按 HTML 转义输出，昵称、应用名之类的用户数据不可能注入标记；
// 确实要输出原样 HTML 时写 `{{ x | safe }}`。主题、纯文本、主题行用的是不转义的引擎。
package mailtpl

import (
	"bytes"
	"container/list"
	"context"
	"crypto/sha256"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"sync"
	"time"

	"github.com/Boostport/mjml-go"
	"github.com/osteele/liquid"
	"github.com/osteele/liquid/render"
	"github.com/osteele/liquid/values"
	"github.com/vanng822/go-premailer/premailer"
)

//go:embed templates
var builtinFS embed.FS

// 渲染上限。模板由管理员编写，但仍按不可信输入对待：一个写错的循环不该拖垮发信。
const (
	maxTemplateBytes = 64 << 10  // 单个模板源码
	maxOutputBytes   = 512 << 10 // 单次 Liquid 渲染的输出
	renderTimeout    = 3 * time.Second
	cacheEntries     = 256
)

// 正文格式。
const (
	FormatMJML = "mjml" // MJML 片段（放进版式）或完整 MJML 文档（以 <mjml 开头时不套版式）
	FormatHTML = "html" // 完整 HTML 文档，不套版式，样式由 premailer 内联
)

// Part 模板的组成部分，出错时用来告诉管理员是哪一段写错了。
const (
	PartLayout  = "layout"
	PartBody    = "body"
	PartText    = "text"
	PartSubject = "subject"
	PartCopy    = "copy"
)

// TemplateError 带位置的模板错误。
type TemplateError struct {
	Part    string `json:"part"`
	Line    int    `json:"line,omitempty"`
	Message string `json:"message"`
}

func (e *TemplateError) Error() string {
	label := map[string]string{
		PartLayout: "版式", PartBody: "正文", PartText: "纯文本", PartSubject: "主题", PartCopy: "文案",
	}[e.Part]
	if label == "" {
		label = e.Part
	}
	if e.Line > 0 {
		return fmt.Sprintf("%s第 %d 行：%s", label, e.Line, e.Message)
	}
	return label + "：" + e.Message
}

// Templates 一封信用到的模板。字段留空即用内置模板。
type Templates struct {
	// Layout 自定义版式（MJML + Liquid），用 {% content %} 标出正文位置
	Layout string
	// Body 自定义正文；留空时用 BodyKey 指定的内置正文
	Body string
	// BodyKey 内置正文名：场景键，或 "blocks"（按内容积木逐个渲染）
	BodyKey string
	// Format 自定义正文的格式，mjml（默认）或 html
	Format string
	// Text 自定义纯文本模板；留空时内置正文用内置纯文本，自定义正文从 HTML 抓取
	Text string
}

// Result 渲染结果。
type Result struct {
	HTML string
	Text string
}

// Engine 模板服务。并发安全。
type Engine struct {
	html *liquid.Engine // 自动转义：正文与版式
	text *liquid.Engine // 不转义：纯文本、主题、文案

	mu    sync.Mutex
	cache map[[32]byte]*list.Element
	order *list.List

	// compile 可替换，测试里用来计数
	compile func(ctx context.Context, source string) (string, error)
}

type cacheEntry struct {
	key  [32]byte
	html string
}

// New 构造模板服务。
func New() *Engine {
	e := &Engine{
		html:  liquid.NewEngine(),
		text:  liquid.NewEngine(),
		cache: map[[32]byte]*list.Element{},
		order: list.New(),
	}
	e.html.SetAutoEscapeReplacer(render.HtmlEscaper)
	for _, engine := range []*liquid.Engine{e.html, e.text} {
		registerFilters(engine)
		// {% include %} / {% render %} 默认会去读磁盘上的文件。组件是 Go 实现的标签（见 components.go），
		// 不需要片段文件，因此换成一律「不存在」的存储：管理员的模板读不到服务器上的任何文件
		engine.RegisterTemplateStore(denyStore{})
	}
	registerComponents(e.html)
	e.compile = compileMJML
	return e
}

var (
	defaultOnce sync.Once
	defaultEng  *Engine
)

// Default 进程内共享的模板服务。
func Default() *Engine {
	defaultOnce.Do(func() { defaultEng = New() })
	return defaultEng
}

// Render 渲染一封信。
func (e *Engine) Render(ctx context.Context, theme Theme, msg Message, tpl Templates) (Result, error) {
	bindings := buildBindings(theme, msg)
	custom := strings.TrimSpace(tpl.Body) != ""
	if custom && normalizeFormat(tpl.Format) == FormatHTML {
		html, err := e.renderHTMLDocument(tpl.Body, bindings)
		if err != nil {
			return Result{}, err
		}
		text, err := e.renderText(tpl.Text, custom, bindings, html)
		if err != nil {
			return Result{}, err
		}
		return Result{HTML: html, Text: text}, nil
	}
	document, err := e.assemble(bindings, tpl)
	if err != nil {
		return Result{}, err
	}
	html, err := e.compileCached(ctx, document)
	if err != nil {
		return Result{}, err
	}
	text, err := e.renderText(tpl.Text, custom, bindings, html)
	if err != nil {
		return Result{}, err
	}
	return Result{HTML: html, Text: text}, nil
}

// Validate 按严格模式检查一套模板：Liquid 语法、渲染、MJML 结构，返回全部问题（保存时用）。
func (e *Engine) Validate(ctx context.Context, theme Theme, msg Message, tpl Templates) []TemplateError {
	bindings := buildBindings(theme, msg)
	collect := func(err error) []TemplateError {
		var templateErr *TemplateError
		if errors.As(err, &templateErr) {
			return []TemplateError{*templateErr}
		}
		return []TemplateError{{Part: PartBody, Message: err.Error()}}
	}
	if strings.TrimSpace(tpl.Text) != "" {
		if _, err := e.renderLiquid(e.text, PartText, tpl.Text, bindings); err != nil {
			return collect(err)
		}
	}
	if strings.TrimSpace(tpl.Body) != "" && normalizeFormat(tpl.Format) == FormatHTML {
		if _, err := e.renderLiquid(e.html, PartBody, tpl.Body, bindings); err != nil {
			return collect(err)
		}
		return nil
	}
	document, err := e.assemble(bindings, tpl)
	if err != nil {
		return collect(err)
	}
	problems := ValidateMJML(ctx, document)
	if len(problems) > 0 {
		// MJML 报的行号是拼好的整份文档的行号；正文与版式各自的行号对不上，只保留信息本身
		for i := range problems {
			problems[i].Line = 0
		}
	}
	return problems
}

// assemble 渲染正文并套进版式，产出完整的 MJML 文档。
func (e *Engine) assemble(bindings map[string]any, tpl Templates) (string, error) {
	bodySource := tpl.Body
	if strings.TrimSpace(bodySource) == "" {
		bodySource = BuiltinBody(tpl.BodyKey)
	}
	body, err := e.renderLiquid(e.html, PartBody, bodySource, bindings)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(strings.TrimSpace(strings.ToLower(body)), "<mjml") {
		return body, nil
	}
	layoutSource := tpl.Layout
	if strings.TrimSpace(layoutSource) == "" {
		layoutSource = BuiltinLayout()
	}
	withContent := cloneBindings(bindings)
	// SafeValue：{{ content }} 原样输出，不被自动转义
	withContent["content"] = values.SafeValue{Value: body}
	return e.renderLiquid(e.html, PartLayout, layoutSource, withContent)
}

// RenderPlain 渲染一段不转义的 Liquid（主题、文案）。
func (e *Engine) RenderPlain(part string, source string, theme Theme, msg Message) (string, error) {
	return e.renderLiquid(e.text, part, source, buildBindings(theme, msg))
}

// RenderPlainWith 用给定的变量渲染一段不转义的 Liquid。
func (e *Engine) RenderPlainWith(part string, source string, vars map[string]any) (string, error) {
	return e.renderLiquid(e.text, part, source, vars)
}

// Check 只检查 Liquid 语法，不渲染（保存文案时用）。
func (e *Engine) Check(part string, source string) error {
	if len(source) > maxTemplateBytes {
		return &TemplateError{Part: part, Message: fmt.Sprintf("模板超过 %d KB", maxTemplateBytes>>10)}
	}
	if _, err := e.html.ParseString(source); err != nil {
		return liquidError(part, err)
	}
	return nil
}

func (e *Engine) renderText(source string, custom bool, bindings map[string]any, html string) (string, error) {
	if strings.TrimSpace(source) != "" {
		text, err := e.renderLiquid(e.text, PartText, source, bindings)
		return collapseBlankLines(text), err
	}
	if custom {
		return HTMLToText(html), nil
	}
	text, err := e.renderLiquid(e.text, PartText, BuiltinText(), bindings)
	return collapseBlankLines(text), err
}

func (e *Engine) renderHTMLDocument(source string, bindings map[string]any) (string, error) {
	html, err := e.renderLiquid(e.html, PartBody, source, bindings)
	if err != nil {
		return "", err
	}
	// 自定义 HTML 多半把样式写在 <style> 里，而邮件客户端普遍只认内联样式
	if pm, err := premailer.NewPremailerFromString(html, premailer.NewOptions(
		premailer.WithRemoveClasses(false),
		premailer.WithKeepBangImportant(true),
	)); err == nil {
		if inlined, err := pm.Transform(); err == nil {
			return inlined, nil
		}
	}
	return html, nil
}

// renderLiquid 解析并渲染一段 Liquid，带输出上限与超时。
func (e *Engine) renderLiquid(engine *liquid.Engine, part string, source string, bindings map[string]any) (string, error) {
	if len(source) > maxTemplateBytes {
		return "", &TemplateError{Part: part, Message: fmt.Sprintf("模板超过 %d KB", maxTemplateBytes>>10)}
	}
	template, err := engine.ParseString(source)
	if err != nil {
		return "", liquidError(part, err)
	}
	type outcome struct {
		out string
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		writer := &limitedBuffer{limit: maxOutputBytes}
		renderErr := template.FRender(writer, bindings)
		done <- outcome{out: writer.String(), err: renderErr}
	}()
	select {
	case result := <-done:
		if result.err != nil {
			if errors.Is(result.err, errOutputTooLarge) {
				return "", &TemplateError{Part: part, Message: fmt.Sprintf("渲染结果超过 %d KB，检查是否有失控的循环", maxOutputBytes>>10)}
			}
			return "", liquidError(part, result.err)
		}
		return result.out, nil
	case <-time.After(renderTimeout):
		return "", &TemplateError{Part: part, Message: "渲染超时，检查是否有失控的循环"}
	}
}

// compileCached MJML 编译很重（约 50ms），按最终源码的摘要缓存：
// 同一份通知、同一次预览不会重复编译。验证码这类每封都不同的信缓存不命中，照常编译。
func (e *Engine) compileCached(ctx context.Context, source string) (string, error) {
	if len(source) > 4*maxTemplateBytes {
		return "", &TemplateError{Part: PartLayout, Message: "邮件源码过大"}
	}
	key := sha256.Sum256([]byte(source))
	e.mu.Lock()
	if element, ok := e.cache[key]; ok {
		e.order.MoveToFront(element)
		html := element.Value.(*cacheEntry).html
		e.mu.Unlock()
		return html, nil
	}
	e.mu.Unlock()

	html, err := e.compile(ctx, source)
	if err != nil {
		return "", err
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.cache[key]; !ok {
		e.cache[key] = e.order.PushFront(&cacheEntry{key: key, html: html})
		for e.order.Len() > cacheEntries {
			oldest := e.order.Back()
			e.order.Remove(oldest)
			delete(e.cache, oldest.Value.(*cacheEntry).key)
		}
	}
	return html, nil
}

func compileMJML(ctx context.Context, source string) (string, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	html, err := mjml.ToHTML(ctx, source, mjml.WithMinify(true), mjml.WithValidationLevel(mjml.Soft))
	if err != nil {
		var mjmlErr mjml.Error
		if errors.As(err, &mjmlErr) && len(mjmlErr.Details) > 0 {
			detail := mjmlErr.Details[0]
			message := detail.Message
			if detail.TagName != "" {
				message = "<" + detail.TagName + "> " + message
			}
			return "", &TemplateError{Part: PartLayout, Line: detail.Line, Message: message}
		}
		return "", &TemplateError{Part: PartLayout, Message: "MJML 编译失败：" + err.Error()}
	}
	return html, nil
}

// ValidateMJML 严格校验一份完整的 MJML 文档，返回全部问题（保存模板时用）。
func ValidateMJML(ctx context.Context, source string) []TemplateError {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	_, err := mjml.ToHTML(ctx, source, mjml.WithValidationLevel(mjml.Strict))
	if err == nil {
		return nil
	}
	var mjmlErr mjml.Error
	if errors.As(err, &mjmlErr) && len(mjmlErr.Details) > 0 {
		out := make([]TemplateError, 0, len(mjmlErr.Details))
		for _, detail := range mjmlErr.Details {
			message := detail.Message
			if detail.TagName != "" {
				message = "<" + detail.TagName + "> " + message
			}
			out = append(out, TemplateError{Part: PartLayout, Line: detail.Line, Message: message})
		}
		return out
	}
	return []TemplateError{{Part: PartLayout, Message: err.Error()}}
}

// Warmup 预热 MJML 运行时（首次编译要实例化 wasm 模块）。启动后在后台调用一次即可。
func (e *Engine) Warmup(ctx context.Context) {
	_, _ = e.Render(ctx, DefaultTheme(), Message{BrandName: "Aegis", Title: "warmup"}, Templates{BodyKey: "blocks"})
}

func liquidError(part string, err error) error {
	var located interface {
		LineNumber() int
		Cause() error
	}
	if errors.As(err, &located) {
		message := err.Error()
		if cause := located.Cause(); cause != nil {
			message = cause.Error()
		}
		return &TemplateError{Part: part, Line: located.LineNumber(), Message: translateLiquidMessage(message)}
	}
	return &TemplateError{Part: part, Message: translateLiquidMessage(err.Error())}
}

// translateLiquidMessage 把最常见的几类 Liquid 报错换成中文，其余原样保留（带上英文更好搜）。
func translateLiquidMessage(message string) string {
	switch {
	case strings.Contains(message, "undefined tag"):
		return "未知的标签：" + message
	case strings.Contains(message, "undefined filter"):
		return "未知的过滤器：" + message
	case strings.Contains(message, "unterminated"), strings.Contains(message, "not terminated"):
		return "标签没有闭合：" + message
	case strings.Contains(message, "syntax error"):
		return "语法错误：" + message
	}
	return message
}

func normalizeFormat(format string) string {
	if strings.EqualFold(strings.TrimSpace(format), FormatHTML) {
		return FormatHTML
	}
	return FormatMJML
}

func collapseBlankLines(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	out := make([]string, 0, len(lines))
	blank := 0
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if line == "" {
			blank++
			if blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// ── 输出上限 ──

var errOutputTooLarge = errors.New("mail template output too large")

type limitedBuffer struct {
	bytes.Buffer
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.limit {
		return 0, errOutputTooLarge
	}
	return b.Buffer.Write(p)
}

func (b *limitedBuffer) WriteString(s string) (int, error) {
	if b.Len()+len(s) > b.limit {
		return 0, errOutputTooLarge
	}
	return b.Buffer.WriteString(s)
}

var _ io.Writer = (*limitedBuffer)(nil)

// ── 内置模板 ──

// denyStore 拒绝一切片段读取。
type denyStore struct{}

func (denyStore) ReadTemplate(string) ([]byte, error) { return nil, fs.ErrNotExist }

func mustRead(name string) string {
	data, err := builtinFS.ReadFile("templates/" + name)
	if err != nil {
		panic("mailtpl: 内置模板缺失 " + name + ": " + err.Error())
	}
	return string(data)
}

var (
	builtinLayout = mustRead("layout.mjml.liquid")
	builtinText   = mustRead("text.liquid")
	builtinBodies = func() map[string]string {
		bodies := map[string]string{}
		entries, err := builtinFS.ReadDir("templates/bodies")
		if err != nil {
			panic("mailtpl: 内置正文目录缺失: " + err.Error())
		}
		for _, entry := range entries {
			name := strings.TrimSuffix(entry.Name(), ".mjml.liquid")
			bodies[name] = mustRead("bodies/" + entry.Name())
		}
		return bodies
	}()
)

// BuiltinLayout 内置版式源码。
func BuiltinLayout() string { return builtinLayout }

// BuiltinText 内置纯文本模板源码。
func BuiltinText() string { return builtinText }

// BuiltinBody 内置正文源码。场景键可以是具体场景，也可以是场景族（code_ 前缀的验证码共用一份）。
func BuiltinBody(key string) string {
	key = strings.TrimSpace(key)
	if body, ok := builtinBodies[key]; ok {
		return body
	}
	if strings.HasPrefix(key, "code_") {
		return builtinBodies["code"]
	}
	return builtinBodies["blocks"]
}
