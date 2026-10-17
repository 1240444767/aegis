package email

import (
	"fmt"
	"net/mail"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// 邮件策略：一个作用域（应用或平台）发信的全部规则。
//
// 通道（Config）回答「从哪儿发」，策略回答「发成什么样、什么时候发、发给谁不发」：
//
//	Brand        落款名、Logo、强调色、客服邮箱、页脚附加行
//	Code         验证码长度、有效期、重发冷却、错误次数、按地址与按 IP 的频次上限
//	Scenes       每个场景的开关与文案覆盖（主题 / 标题 / 引导句 / 页脚提示）
//	Suppression  硬退信与投诉是否自动加入抑制名单
//	ResetLinks   密码重置链接允许的地址前缀
//
// 没保存过策略的作用域按 DefaultPolicy 处理；新加的字段对老策略同样有合理取值（见 NormalizePolicy）。
type Policy struct {
	Brand       Brand                   `json:"brand"`
	Code        CodePolicy              `json:"code"`
	Scenes      map[string]SceneSetting `json:"scenes"`
	Suppression SuppressionPolicy       `json:"suppression"`
	ResetLinks  []string                `json:"resetLinks"`
}

// Brand 邮件落款。全部可选，留空沿用平台默认的样子。
type Brand struct {
	// DisplayName 邮件里的品牌名，留空用应用名（平台级用平台品牌名）
	DisplayName string `json:"displayName,omitempty"`
	// LogoURL 标题上方的 Logo，必须是 https 地址；邮件客户端不会加载 http 图片
	LogoURL string `json:"logoUrl,omitempty"`
	// AccentColor 按钮颜色，#RRGGBB；文字颜色按亮度自动取黑或白
	AccentColor string `json:"accentColor,omitempty"`
	// SupportEmail 页脚的客服邮箱。填了它，「直接回复不会有人看到」会改成指向这个地址
	SupportEmail string `json:"supportEmail,omitempty"`
	// FooterText 页脚附加的一行，例如公司名称或备案信息
	FooterText string `json:"footerText,omitempty"`
}

// CodePolicy 验证码规则。频次上限填 0 表示不限。
type CodePolicy struct {
	Length        int `json:"length"`
	TTLMinutes    int `json:"ttlMinutes"`
	ResendSeconds int `json:"resendSeconds"`
	MaxAttempts   int `json:"maxAttempts"`
	AddressHourly int `json:"addressHourly"`
	AddressDaily  int `json:"addressDaily"`
	IPHourly      int `json:"ipHourly"`
}

// SuppressionPolicy 投递回执触发的自动抑制。
//
// 硬退信说明地址不存在，继续往里发只会拉低发信域名的信誉；
// 投诉说明收件人把信标成了垃圾邮件，再发一封就是第二次投诉。
// 软退信（邮箱满、临时拒收）不在此列：它们不改投递主状态，也不触发抑制。
type SuppressionPolicy struct {
	HardBounce bool `json:"hardBounce"`
	Complaint  bool `json:"complaint"`
}

// SceneSetting 一个场景的开关与文案覆盖。文案留空即用目录里的默认文案。
type SceneSetting struct {
	// Enabled 只对可关闭的场景（Scene.Toggle）有意义；nil 表示按场景默认
	Enabled *bool  `json:"enabled,omitempty"`
	Subject string `json:"subject,omitempty"`
	Title   string `json:"title,omitempty"`
	Lead    string `json:"lead,omitempty"`
	Footer  string `json:"footer,omitempty"`
}

// IsZero 这一项既没改开关也没改文案。
func (s SceneSetting) IsZero() bool {
	return s.Enabled == nil && s.Subject == "" && s.Title == "" && s.Lead == "" && s.Footer == ""
}

// PolicyRecord 落库的策略与元数据。
type PolicyRecord struct {
	AppID      int64      `json:"appid"`
	Policy     Policy     `json:"policy"`
	Configured bool       `json:"configured"`
	UpdatedBy  string     `json:"updatedBy,omitempty"`
	UpdatedAt  *time.Time `json:"updatedAt,omitempty"`
}

// 验证码规则的取值范围。控制台的输入框与服务端校验共用这一组数。
const (
	CodeLengthMin, CodeLengthMax         = 4, 8
	CodeTTLMin, CodeTTLMax               = 1, 60
	CodeResendMax                        = 600
	CodeAttemptsMin, CodeAttemptsMax     = 1, 20
	CodeAddressHourlyMax                 = 100
	CodeAddressDailyMax                  = 500
	CodeIPHourlyMax                      = 1000
	ResetLinksMax                        = 10
	brandDisplayNameMax, brandFooterMax  = 40, 200
	sceneSubjectMax, sceneTitleMax       = 120, 60
	sceneLeadMax, sceneFooterMax         = 300, 300
	suppressionNoteMax                   = 200
	defaultCodeLength, defaultCodeTTL    = 6, 10
	defaultResendSeconds, defaultTries   = 60, 5
	defaultAddressHourly, defaultAddrDay = 10, 30
	defaultIPHourly                      = 60
)

// DefaultPolicy 没保存过策略时的取值。
func DefaultPolicy() Policy {
	return Policy{
		Code: CodePolicy{
			Length:        defaultCodeLength,
			TTLMinutes:    defaultCodeTTL,
			ResendSeconds: defaultResendSeconds,
			MaxAttempts:   defaultTries,
			AddressHourly: defaultAddressHourly,
			AddressDaily:  defaultAddrDay,
			IPHourly:      defaultIPHourly,
		},
		Scenes:      map[string]SceneSetting{},
		Suppression: SuppressionPolicy{HardBounce: true, Complaint: true},
		ResetLinks:  []string{},
	}
}

// PolicyLimits 下发给控制台的取值范围。
func PolicyLimits() map[string][2]int {
	return map[string][2]int{
		"length":        {CodeLengthMin, CodeLengthMax},
		"ttlMinutes":    {CodeTTLMin, CodeTTLMax},
		"resendSeconds": {0, CodeResendMax},
		"maxAttempts":   {CodeAttemptsMin, CodeAttemptsMax},
		"addressHourly": {0, CodeAddressHourlyMax},
		"addressDaily":  {0, CodeAddressDailyMax},
		"ipHourly":      {0, CodeIPHourlyMax},
		"resetLinks":    {0, ResetLinksMax},
	}
}

// NormalizePolicy 修剪空白、丢掉目录之外的场景与空覆盖、把越界的数值拉回默认。
//
// 读库时也走这一遍：老策略缺的字段（解出来是 0）落回默认值，而不是变成「验证码 0 位」。
func NormalizePolicy(p Policy) Policy {
	def := DefaultPolicy()
	p.Brand.DisplayName = strings.TrimSpace(p.Brand.DisplayName)
	p.Brand.LogoURL = strings.TrimSpace(p.Brand.LogoURL)
	p.Brand.AccentColor = strings.ToLower(strings.TrimSpace(p.Brand.AccentColor))
	p.Brand.SupportEmail = strings.TrimSpace(p.Brand.SupportEmail)
	p.Brand.FooterText = strings.TrimSpace(p.Brand.FooterText)

	fix := func(value *int, lo, hi, fallback int) {
		if *value < lo || *value > hi {
			*value = fallback
		}
	}
	fix(&p.Code.Length, CodeLengthMin, CodeLengthMax, def.Code.Length)
	fix(&p.Code.TTLMinutes, CodeTTLMin, CodeTTLMax, def.Code.TTLMinutes)
	fix(&p.Code.ResendSeconds, 0, CodeResendMax, def.Code.ResendSeconds)
	fix(&p.Code.MaxAttempts, CodeAttemptsMin, CodeAttemptsMax, def.Code.MaxAttempts)
	fix(&p.Code.AddressHourly, 0, CodeAddressHourlyMax, def.Code.AddressHourly)
	fix(&p.Code.AddressDaily, 0, CodeAddressDailyMax, def.Code.AddressDaily)
	fix(&p.Code.IPHourly, 0, CodeIPHourlyMax, def.Code.IPHourly)

	scenes := make(map[string]SceneSetting, len(p.Scenes))
	for key, setting := range p.Scenes {
		scene, ok := FindScene(key)
		if !ok {
			continue
		}
		setting.Subject = strings.TrimSpace(setting.Subject)
		setting.Title = strings.TrimSpace(setting.Title)
		setting.Lead = strings.TrimSpace(setting.Lead)
		setting.Footer = strings.TrimSpace(setting.Footer)
		// 开关只对可关闭的场景生效；与默认相同的开关不存，默认改了老策略跟着走
		if !scene.Toggle || (setting.Enabled != nil && *setting.Enabled == scene.DefaultOn) {
			setting.Enabled = nil
		}
		if setting.IsZero() {
			continue
		}
		scenes[scene.Key] = setting
	}
	p.Scenes = scenes

	links := make([]string, 0, len(p.ResetLinks))
	for _, link := range p.ResetLinks {
		link = strings.TrimSpace(link)
		if link != "" && !slices.Contains(links, link) {
			links = append(links, link)
		}
	}
	p.ResetLinks = links
	return p
}

var (
	accentColorPattern = regexp.MustCompile(`^#[0-9a-f]{6}$`)
	placeholderPattern = regexp.MustCompile(`\{\{\s*([a-zA-Z]+)\s*\}\}`)
)

// ValidatePolicy 在 NormalizePolicy 之后调用，返回第一条违规的中文说明。
//
// 数值越界在这里报错而不是静默拉回：管理员填了 99 位验证码却保存成功，
// 再打开发现变成了 6 位，那比一句「验证码长度须在 4 到 8 位之间」难理解得多。
func ValidatePolicy(raw Policy) error {
	checkRange := func(label string, value, lo, hi int) error {
		if value < lo || value > hi {
			return fmt.Errorf("%s须在 %d 到 %d 之间", label, lo, hi)
		}
		return nil
	}
	code := raw.Code
	for _, item := range []struct {
		label  string
		value  int
		lo, hi int
	}{
		{"验证码位数", code.Length, CodeLengthMin, CodeLengthMax},
		{"验证码有效期（分钟）", code.TTLMinutes, CodeTTLMin, CodeTTLMax},
		{"重发间隔（秒）", code.ResendSeconds, 0, CodeResendMax},
		{"错误次数上限", code.MaxAttempts, CodeAttemptsMin, CodeAttemptsMax},
		{"单个邮箱每小时上限", code.AddressHourly, 0, CodeAddressHourlyMax},
		{"单个邮箱每天上限", code.AddressDaily, 0, CodeAddressDailyMax},
		{"单个 IP 每小时上限", code.IPHourly, 0, CodeIPHourlyMax},
	} {
		if err := checkRange(item.label, item.value, item.lo, item.hi); err != nil {
			return err
		}
	}
	if code.AddressHourly > 0 && code.AddressDaily > 0 && code.AddressHourly > code.AddressDaily {
		return fmt.Errorf("单个邮箱每小时上限不能大于每天上限")
	}

	brand := raw.Brand
	if utf8.RuneCountInString(strings.TrimSpace(brand.DisplayName)) > brandDisplayNameMax {
		return fmt.Errorf("品牌名称不能超过 %d 个字", brandDisplayNameMax)
	}
	if logo := strings.TrimSpace(brand.LogoURL); logo != "" {
		parsed, err := url.Parse(logo)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
			return fmt.Errorf("Logo 地址须是 https 开头的完整地址")
		}
	}
	if color := strings.ToLower(strings.TrimSpace(brand.AccentColor)); color != "" && !accentColorPattern.MatchString(color) {
		return fmt.Errorf("强调色须是 #RRGGBB 格式")
	}
	if support := strings.TrimSpace(brand.SupportEmail); support != "" {
		if _, err := mail.ParseAddress(support); err != nil {
			return fmt.Errorf("客服邮箱格式错误")
		}
	}
	if utf8.RuneCountInString(strings.TrimSpace(brand.FooterText)) > brandFooterMax {
		return fmt.Errorf("页脚附加文字不能超过 %d 个字", brandFooterMax)
	}

	for key, setting := range raw.Scenes {
		scene, ok := FindScene(key)
		if !ok {
			return fmt.Errorf("未知的邮件场景：%s", key)
		}
		for _, field := range []struct {
			label string
			value string
			max   int
		}{
			{"主题", setting.Subject, sceneSubjectMax},
			{"标题", setting.Title, sceneTitleMax},
			{"引导语", setting.Lead, sceneLeadMax},
			{"页脚提示", setting.Footer, sceneFooterMax},
		} {
			value := strings.TrimSpace(field.value)
			if utf8.RuneCountInString(value) > field.max {
				return fmt.Errorf("「%s」的%s不能超过 %d 个字", scene.Name, field.label, field.max)
			}
			if strings.ContainsAny(value, "\r\n") && field.label == "主题" {
				return fmt.Errorf("「%s」的主题不能换行", scene.Name)
			}
			for _, match := range placeholderPattern.FindAllStringSubmatch(value, -1) {
				if !scene.HasVariable(match[1]) {
					return fmt.Errorf("「%s」的%s里用了这个场景没有的变量 {{%s}}", scene.Name, field.label, match[1])
				}
			}
		}
	}

	if len(raw.ResetLinks) > ResetLinksMax {
		return fmt.Errorf("重置链接最多登记 %d 条", ResetLinksMax)
	}
	for _, link := range raw.ResetLinks {
		link = strings.TrimSpace(link)
		if link == "" {
			continue
		}
		if err := validateResetLink(link); err != nil {
			return err
		}
	}
	return nil
}

func validateResetLink(link string) error {
	parsed, err := url.Parse(link)
	if err != nil || parsed.Host == "" {
		return fmt.Errorf("重置链接 %s 不是完整的地址", link)
	}
	local := parsed.Hostname() == "localhost" || parsed.Hostname() == "127.0.0.1"
	if parsed.Scheme != "https" && !(parsed.Scheme == "http" && local) {
		return fmt.Errorf("重置链接 %s 须使用 https（本机调试可用 http://localhost）", link)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("重置链接 %s 不能带查询参数或锚点，令牌会自动拼在后面", link)
	}
	return nil
}

// MatchResetLink 判断调用方给的重置地址是否落在登记的前缀里，返回去掉查询参数后的地址。
//
// 比较的是协议、主机（含端口）与路径前缀：只比主机的话，同一域名下任何一个
// 能被用户控制的页面（论坛、短链）都能拿来收令牌。
func MatchResetLink(allowed []string, candidate string) (string, bool) {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		if len(allowed) == 0 {
			return "", false
		}
		return strings.TrimRight(allowed[0], "/"), true
	}
	target, err := url.Parse(candidate)
	if err != nil || target.Host == "" {
		return "", false
	}
	targetPath := "/" + strings.Trim(target.EscapedPath(), "/")
	for _, link := range allowed {
		base, err := url.Parse(strings.TrimSpace(link))
		if err != nil {
			continue
		}
		if !strings.EqualFold(base.Scheme, target.Scheme) || !strings.EqualFold(base.Host, target.Host) {
			continue
		}
		basePath := "/" + strings.Trim(base.EscapedPath(), "/")
		if basePath == "/" || targetPath == basePath || strings.HasPrefix(targetPath, basePath+"/") {
			target.RawQuery, target.Fragment = "", ""
			return strings.TrimRight(target.String(), "/"), true
		}
	}
	return "", false
}

// SceneEnabled 可关闭的场景此刻是否开着。
func (p Policy) SceneEnabled(key string) bool {
	scene, ok := FindScene(key)
	if !ok {
		return true
	}
	if !scene.Toggle {
		return true
	}
	if setting, ok := p.Scenes[scene.Key]; ok && setting.Enabled != nil {
		return *setting.Enabled
	}
	return scene.DefaultOn
}

// ── 抑制名单 ──

// 抑制原因与来源。
const (
	SuppressionBounce    = "bounce"
	SuppressionComplaint = "complaint"
	SuppressionManual    = "manual"

	SuppressionSourceWebhook = "webhook"
	SuppressionSourceAdmin   = "admin"
)

// Suppression 一条「不再往这个地址发信」的记录。
type Suppression struct {
	ID         int64      `json:"id"`
	AppID      int64      `json:"appid"`
	Address    string     `json:"address"`
	Reason     string     `json:"reason"`
	Source     string     `json:"source"`
	Note       string     `json:"note,omitempty"`
	DeliveryID int64      `json:"deliveryId,omitempty"`
	CreatedBy  string     `json:"createdBy,omitempty"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	UpdatedAt  time.Time  `json:"updatedAt"`
}

// SuppressionQuery 抑制名单分页筛选。
type SuppressionQuery struct {
	AppID    int64
	Reason   string
	Keyword  string
	Page     int
	PageSize int
}

type SuppressionPage struct {
	Items    []Suppression `json:"items"`
	Total    int64         `json:"total"`
	Page     int           `json:"page"`
	PageSize int           `json:"pageSize"`
}

// SuppressionInput 管理员手动添加。
type SuppressionInput struct {
	AppID     int64
	Address   string
	Reason    string
	Note      string
	ExpiresAt *time.Time
	Operator  string
}

// ValidateSuppressionInput 校验并规整手动添加的抑制记录。
func ValidateSuppressionInput(input *SuppressionInput) error {
	input.Address = strings.ToLower(strings.TrimSpace(input.Address))
	if _, err := mail.ParseAddress(input.Address); err != nil || !strings.Contains(input.Address, "@") {
		return fmt.Errorf("邮箱地址格式错误")
	}
	switch input.Reason {
	case "":
		input.Reason = SuppressionManual
	case SuppressionBounce, SuppressionComplaint, SuppressionManual:
	default:
		return fmt.Errorf("未知的抑制原因")
	}
	input.Note = strings.TrimSpace(input.Note)
	if utf8.RuneCountInString(input.Note) > suppressionNoteMax {
		return fmt.Errorf("备注不能超过 %d 个字", suppressionNoteMax)
	}
	if input.ExpiresAt != nil && !input.ExpiresAt.After(time.Now()) {
		return fmt.Errorf("到期时间须晚于现在")
	}
	return nil
}

// ── 投递事件 ──

// DeliveryEvent 一封信在服务商侧经历的一次事件（来自投递回执）。
type DeliveryEvent struct {
	ID         int64          `json:"id"`
	DeliveryID int64          `json:"deliveryId"`
	Event      string         `json:"event"`
	Status     string         `json:"status,omitempty"`
	Detail     string         `json:"detail,omitempty"`
	Payload    map[string]any `json:"payload,omitempty"`
	OccurredAt time.Time      `json:"occurredAt"`
}

// DeliveryDetail 一封信的留痕与事件时间线。
type DeliveryDetail struct {
	Delivery    Delivery        `json:"delivery"`
	Events      []DeliveryEvent `json:"events"`
	Suppression *Suppression    `json:"suppression,omitempty"`
}

// DeliveryEventOutcome 回执落地后的结果，服务层据此决定要不要自动抑制。
type DeliveryEventOutcome struct {
	Matched    bool
	DeliveryID int64
	AppID      int64
	ToAddress  string
	Status     string
}

// ── 概览 ──

// DeliveryTrendPoint 一天的发信量。
type DeliveryTrendPoint struct {
	Date      string `json:"date"`
	Total     int64  `json:"total"`
	Delivered int64  `json:"delivered"`
	Failed    int64  `json:"failed"`
	Bounced   int64  `json:"bounced"`
}

// DeliveryCount 按某一维度的计数。
type DeliveryCount struct {
	Key   string `json:"key"`
	Count int64  `json:"count"`
}

// DeliveryOverview 概览页的全部数字。累计值不受时间窗口影响，其余按所选天数。
type DeliveryOverview struct {
	DeliveryStats
	Days         int                  `json:"days"`
	WindowTotal  int64                `json:"windowTotal"`
	Trend        []DeliveryTrendPoint `json:"trend"`
	ByPurpose    []DeliveryCount      `json:"byPurpose"`
	ByStatus     []DeliveryCount      `json:"byStatus"`
	Suppressions int64                `json:"suppressions"`
	// FailedLast24h 最近 24 小时发送失败的封数，概览顶部据此提示
	FailedLast24h int64 `json:"failedLast24h"`
}
