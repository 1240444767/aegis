package mailtpl

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Theme 版式的设计令牌。内置版式与组件只从这里取颜色、圆角与字体，
// 自定义模板同样可以用 {{ theme.accent }} 这类变量对齐品牌。
type Theme struct {
	// Accent 强调色（按钮、验证码、链接），#rrggbb
	Accent string
	// Background 页面底色；Card 卡片底色
	Background string
	Card       string
	// Radius 卡片圆角（像素），按钮与内框按比例取
	Radius int
	// Font 字体预设：system / sans / serif / rounded
	Font string
	// LogoAlign 页眉对齐：left / center
	LogoAlign string
	// LogoHeight Logo 显示高度（像素）
	LogoHeight int
	// Header 页眉样式：plain 纯净 / bar 卡片顶部一道强调色 / banner 强调色页眉条
	Header string
}

// 字体预设。邮件客户端不会下载网络字体，因此每一档都是系统字体栈，按平台由近及远。
var fontStacks = map[string]string{
	"system":  `-apple-system,BlinkMacSystemFont,'Segoe UI','PingFang SC','Hiragino Sans GB','Microsoft YaHei',Roboto,'Helvetica Neue',Arial,sans-serif`,
	"sans":    `'Inter','IBM Plex Sans','Segoe UI Variable Text','PingFang SC','Microsoft YaHei UI',-apple-system,BlinkMacSystemFont,Arial,sans-serif`,
	"serif":   `'Iowan Old Style','Songti SC','Noto Serif SC','Source Han Serif SC',Georgia,'Times New Roman',serif`,
	"rounded": `'SF Pro Rounded','HarmonyOS Sans SC','PingFang SC','Nunito','Microsoft YaHei',-apple-system,Arial,sans-serif`,
}

const monoStack = `'SFMono-Regular','JetBrains Mono','Cascadia Code',Menlo,Consolas,'Liberation Mono',monospace`

// FontPresets 可选的字体预设，顺序即控制台的展示顺序。
func FontPresets() []string { return []string{"system", "sans", "serif", "rounded"} }

// HeaderStyles 可选的页眉样式。
func HeaderStyles() []string { return []string{"plain", "bar", "banner"} }

// DefaultTheme 内置主题：中性灰底、白卡片、深色按钮，与控制台的设计令牌一致。
func DefaultTheme() Theme {
	return Theme{
		Accent:     "#18181b",
		Background: "#f4f4f5",
		Card:       "#ffffff",
		Radius:     16,
		Font:       "system",
		LogoAlign:  "left",
		LogoHeight: 32,
		Header:     "plain",
	}
}

// Normalize 把非法值拉回默认值。
func (t Theme) Normalize() Theme {
	def := DefaultTheme()
	if !isHexColor(t.Accent) {
		t.Accent = def.Accent
	}
	if !isHexColor(t.Background) {
		t.Background = def.Background
	}
	if !isHexColor(t.Card) {
		t.Card = def.Card
	}
	if t.Radius < 0 || t.Radius > 32 {
		t.Radius = def.Radius
	}
	if _, ok := fontStacks[t.Font]; !ok {
		t.Font = def.Font
	}
	if t.LogoAlign != "center" {
		t.LogoAlign = "left"
	}
	if t.LogoHeight < 20 || t.LogoHeight > 72 {
		t.LogoHeight = def.LogoHeight
	}
	switch t.Header {
	case "plain", "bar", "banner":
	default:
		t.Header = def.Header
	}
	t.Accent = strings.ToLower(t.Accent)
	t.Background = strings.ToLower(t.Background)
	t.Card = strings.ToLower(t.Card)
	return t
}

// palette 由主题推导出的全部颜色。只在 Go 里算一次，模板与组件直接取用。
func (t Theme) palette() map[string]any {
	t = t.Normalize()
	accentText := readableOn(t.Accent)
	// 浅色强调色（例如亮黄）直接做文字颜色读不清，验证码与链接改用加深后的同色系
	accentStrong := t.Accent
	if luminance(t.Accent) > 0.55 {
		accentStrong = mix(t.Accent, "#000000", 0.45)
	}
	darkCard := luminance(t.Card) < 0.4
	text, muted, border, subtle := "#18181b", "#71717a", "#e4e4e7", "#f7f7f8"
	if darkCard {
		text, muted, border, subtle = "#f4f4f5", "#a1a1aa", "#3f3f46", mix(t.Card, "#ffffff", 0.06)
	} else {
		subtle = mix(t.Card, "#18181b", 0.025)
	}
	footText := "#71717a"
	if luminance(t.Background) < 0.4 {
		footText = "#a1a1aa"
	}
	innerRadius := int(math.Round(float64(t.Radius) * 0.75))
	return map[string]any{
		"accent":        t.Accent,
		"accent_text":   accentText,
		"accent_strong": accentStrong,
		"accent_tint":   mix(t.Card, t.Accent, 0.07),
		"accent_border": mix(t.Card, t.Accent, 0.18),
		"background":    t.Background,
		"card":          t.Card,
		"text":          text,
		"muted":         muted,
		"border":        border,
		"subtle":        subtle,
		"foot_text":     footText,
		"radius":        t.Radius,
		"inner_radius":  innerRadius,
		"button_radius": clamp(int(math.Round(float64(t.Radius)*0.6)), 0, 14),
		"font":          fontStacks[t.Font],
		"font_preset":   t.Font,
		"mono":          monoStack,
		"logo_align":    t.LogoAlign,
		"logo_height":   t.LogoHeight,
		"header":        t.Header,
	}
}

// Message 一封信的内容。所有字段都会以 snake_case 交给模板（见 buildBindings）。
type Message struct {
	Lang       string
	BrandName  string
	LogoURL    string
	Eyebrow    string
	Title      string
	Lead       string
	Preheader  string
	FooterNote string
	NoReply    string
	Support    string
	FooterText string
	Subject    string
	Blocks     []Block
	// Vars 场景变量（code、minutes、email、name、reset_url、details……），平铺在模板的顶层
	Vars map[string]any
}

// Block 正文积木：调用方只声明「要一段话 / 一个验证码 / 一张表」，长什么样是模板的事。
type Block struct {
	Kind  string // paragraph / code / details / notice / button / link
	Text  string
	Title string
	Label string
	Code  string
	Note  string
	URL   string
	Rows  []Row
}

// Row 字段表的一行。
type Row struct {
	Label string
	Value string
}

func (b Block) binding() map[string]any {
	rows := make([]any, 0, len(b.Rows))
	for _, row := range b.Rows {
		rows = append(rows, map[string]any{"label": row.Label, "value": row.Value})
	}
	return map[string]any{
		"kind": b.Kind, "text": b.Text, "title": b.Title, "label": b.Label,
		"code": b.Code, "note": b.Note, "url": b.URL, "rows": rows,
	}
}

// RowsBinding 把字段行转成模板可用的形态（给 Vars["details"] 用）。
func RowsBinding(rows []Row) []any {
	out := make([]any, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.Label) == "" && strings.TrimSpace(row.Value) == "" {
			continue
		}
		out = append(out, map[string]any{"label": row.Label, "value": row.Value})
	}
	return out
}

func buildBindings(theme Theme, msg Message) map[string]any {
	lang := firstNonEmpty(msg.Lang, "zh-CN")
	noReply := firstNonEmpty(msg.NoReply, "这是一封系统邮件，直接回复不会有人看到。")
	preheader := firstNonEmpty(msg.Preheader, msg.Lead, msg.Title)
	name := strings.TrimSpace(msg.BrandName)
	initial := ""
	if r, _ := utf8.DecodeRuneInString(name); r != utf8.RuneError {
		initial = strings.ToUpper(string(r))
	}
	logo := strings.TrimSpace(msg.LogoURL)
	if !strings.HasPrefix(logo, "https://") {
		logo = ""
	}
	blocks := make([]any, 0, len(msg.Blocks))
	for _, block := range msg.Blocks {
		blocks = append(blocks, block.binding())
	}
	bindings := map[string]any{}
	for key, value := range msg.Vars {
		bindings[key] = value
	}
	bindings["app"] = name
	bindings["brand"] = map[string]any{
		"name":          name,
		"initial":       initial,
		"logo_url":      logo,
		"support_email": strings.TrimSpace(msg.Support),
		"footer_text":   strings.TrimSpace(msg.FooterText),
	}
	bindings["theme"] = theme.palette()
	bindings["mail"] = map[string]any{
		"lang":        lang,
		"zh":          strings.HasPrefix(strings.ToLower(lang), "zh"),
		"subject":     strings.TrimSpace(msg.Subject),
		"eyebrow":     strings.TrimSpace(msg.Eyebrow),
		"title":       strings.TrimSpace(msg.Title),
		"lead":        strings.TrimSpace(msg.Lead),
		"preheader":   strings.TrimSpace(preheader),
		"footer_note": strings.TrimSpace(msg.FooterNote),
		"no_reply":    noReply,
	}
	bindings["blocks"] = blocks
	bindings["now"] = time.Now()
	return bindings
}

func cloneBindings(source map[string]any) map[string]any {
	out := make(map[string]any, len(source)+1)
	for key, value := range source {
		out[key] = value
	}
	return out
}

// ── 颜色 ──

func isHexColor(value string) bool {
	if len(value) != 7 || value[0] != '#' {
		return false
	}
	_, err := strconv.ParseUint(value[1:], 16, 32)
	return err == nil
}

func parseHex(value string) (float64, float64, float64) {
	n, err := strconv.ParseUint(strings.TrimPrefix(value, "#"), 16, 32)
	if err != nil {
		return 0, 0, 0
	}
	return float64(n >> 16 & 0xff), float64(n >> 8 & 0xff), float64(n & 0xff)
}

// luminance WCAG 相对亮度，0（黑）到 1（白）。
func luminance(hex string) float64 {
	r, g, b := parseHex(hex)
	channel := func(c float64) float64 {
		c /= 255
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(r) + 0.7152*channel(g) + 0.0722*channel(b)
}

// readableOn 在这个底色上读得清的文字颜色：比较黑白两种的对比度取高者。
func readableOn(hex string) string {
	l := luminance(hex)
	if (l+0.05)/(0.0+0.05) >= (1.0+0.05)/(l+0.05) {
		return "#18181b"
	}
	return "#ffffff"
}

// mix 按比例把 b 混进 a（weight 是 b 的占比）。
func mix(a, b string, weight float64) string {
	ar, ag, ab := parseHex(a)
	br, bg, bb := parseHex(b)
	blend := func(x, y float64) int { return int(math.Round(x*(1-weight) + y*weight)) }
	return fmt.Sprintf("#%02x%02x%02x", blend(ar, br), blend(ag, bg), blend(ab, bb))
}

func clamp(value, lo, hi int) int {
	if value < lo {
		return lo
	}
	if value > hi {
		return hi
	}
	return value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
