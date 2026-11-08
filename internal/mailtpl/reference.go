package mailtpl

// 模板编辑器的参考目录：组件、过滤器、全局变量与内置模板源码。
// 控制台的变量面板、代码补全与「从内置模板开始」都来自这里，文档与实现因此不会漂移。

// ReferenceItem 一条可用的变量、组件或过滤器。
type ReferenceItem struct {
	Name        string `json:"name"`
	Syntax      string `json:"syntax"`
	Description string `json:"description"`
}

// Reference 模板语言参考。
type Reference struct {
	Globals    []ReferenceItem `json:"globals"`
	Theme      []ReferenceItem `json:"theme"`
	Components []ReferenceItem `json:"components"`
	Filters    []ReferenceItem `json:"filters"`
}

// Sources 内置模板源码。
type Sources struct {
	Layout string            `json:"layout"`
	Text   string            `json:"text"`
	Bodies map[string]string `json:"bodies"`
}

// BuiltinSources 全部内置模板的源码（控制台「从内置模板开始」用）。
func BuiltinSources() Sources {
	bodies := make(map[string]string, len(builtinBodies))
	for key, value := range builtinBodies {
		bodies[key] = value
	}
	return Sources{Layout: builtinLayout, Text: builtinText, Bodies: bodies}
}

// TemplateReference 模板语言参考目录。
func TemplateReference() Reference {
	return Reference{
		Globals: []ReferenceItem{
			{Name: "app", Syntax: "{{ app }}", Description: "品牌名（同 brand.name）"},
			{Name: "brand.name", Syntax: "{{ brand.name }}", Description: "品牌名"},
			{Name: "brand.initial", Syntax: "{{ brand.initial }}", Description: "品牌名首字，没有 Logo 时做徽标"},
			{Name: "brand.logo_url", Syntax: "{{ brand.logo_url }}", Description: "Logo 地址，未设置时为空"},
			{Name: "brand.support_email", Syntax: "{{ brand.support_email }}", Description: "客服邮箱"},
			{Name: "brand.footer_text", Syntax: "{{ brand.footer_text }}", Description: "页脚附加文字"},
			{Name: "mail.title", Syntax: "{{ mail.title }}", Description: "大标题（场景文案）"},
			{Name: "mail.lead", Syntax: "{{ mail.lead }}", Description: "引导语（场景文案）"},
			{Name: "mail.footer_note", Syntax: "{{ mail.footer_note }}", Description: "页脚提示（场景文案）"},
			{Name: "mail.eyebrow", Syntax: "{{ mail.eyebrow }}", Description: "标题上方的小字分类"},
			{Name: "mail.subject", Syntax: "{{ mail.subject }}", Description: "邮件主题"},
			{Name: "mail.preheader", Syntax: "{{ mail.preheader }}", Description: "收件箱预览行"},
			{Name: "mail.no_reply", Syntax: "{{ mail.no_reply }}", Description: "「请勿回复」提示"},
			{Name: "mail.lang", Syntax: "{{ mail.lang }}", Description: "语言标记，例如 zh-CN"},
			{Name: "blocks", Syntax: "{% for block in blocks %}…{% endfor %}", Description: "内容积木列表：kind、text、title、label、code、note、url、rows"},
			{Name: "now", Syntax: "{{ now | datetime: \"Asia/Shanghai\" }}", Description: "渲染时刻"},
			{Name: "content", Syntax: "{% content %}", Description: "仅版式可用：正文插入的位置"},
		},
		Theme: []ReferenceItem{
			{Name: "theme.accent", Syntax: "{{ theme.accent }}", Description: "强调色"},
			{Name: "theme.accent_text", Syntax: "{{ theme.accent_text }}", Description: "强调色上的文字颜色（自动取黑或白）"},
			{Name: "theme.accent_strong", Syntax: "{{ theme.accent_strong }}", Description: "做文字用的强调色（浅色强调色会自动加深）"},
			{Name: "theme.accent_tint", Syntax: "{{ theme.accent_tint }}", Description: "强调色的浅底"},
			{Name: "theme.background", Syntax: "{{ theme.background }}", Description: "页面底色"},
			{Name: "theme.card", Syntax: "{{ theme.card }}", Description: "卡片底色"},
			{Name: "theme.text", Syntax: "{{ theme.text }}", Description: "正文颜色"},
			{Name: "theme.muted", Syntax: "{{ theme.muted }}", Description: "次要文字颜色"},
			{Name: "theme.border", Syntax: "{{ theme.border }}", Description: "描边颜色"},
			{Name: "theme.subtle", Syntax: "{{ theme.subtle }}", Description: "弱底色（字段表、提示框）"},
			{Name: "theme.radius", Syntax: "{{ theme.radius }}", Description: "卡片圆角（像素）"},
			{Name: "theme.inner_radius", Syntax: "{{ theme.inner_radius }}", Description: "内框圆角"},
			{Name: "theme.button_radius", Syntax: "{{ theme.button_radius }}", Description: "按钮圆角"},
			{Name: "theme.font", Syntax: "{{ theme.font }}", Description: "正文字体栈"},
			{Name: "theme.mono", Syntax: "{{ theme.mono }}", Description: "等宽字体栈"},
			{Name: "theme.header", Syntax: "{{ theme.header }}", Description: "页眉样式：plain / bar / banner"},
		},
		Components: []ReferenceItem{
			{Name: "code", Syntax: "{% code %}", Description: "验证码框，默认取 code 与 minutes；可传 value、note"},
			{Name: "button", Syntax: "{% button url: reset_url, label: \"设置新密码\" %}", Description: "按钮，可传 align: \"center\""},
			{Name: "details", Syntax: "{% details %}", Description: "字段表，默认取 details；可传 rows"},
			{Name: "notice", Syntax: "{% notice title: \"注意\", text: \"…\", tone: \"warning\" %}", Description: "提示框，tone 可选 accent / warning / danger"},
			{Name: "link", Syntax: "{% link url: reset_url, label: \"复制链接\" %}", Description: "长链接（按钮打不开时备用）"},
			{Name: "paragraph", Syntax: "{% paragraph text: \"…\", tone: \"muted\" %}", Description: "一段正文，保留换行"},
			{Name: "markdown", Syntax: "{% markdown %}**加粗** 与 [链接](https://example.com){% endmarkdown %}", Description: "用 Markdown 写一段正文"},
			{Name: "image", Syntax: "{% image src: \"https://…\", alt: \"\", width: 520 %}", Description: "图片，src 须为 https，可传 href"},
			{Name: "divider", Syntax: "{% divider %}", Description: "分隔线"},
			{Name: "spacer", Syntax: "{% spacer height: 24 %}", Description: "留白"},
			{Name: "blocks", Syntax: "{% blocks %}", Description: "按内容积木逐个渲染"},
		},
		Filters: []ReferenceItem{
			{Name: "mask_email", Syntax: "{{ email | mask_email }}", Description: "邮箱打码 a***e@example.com"},
			{Name: "mask_phone", Syntax: "{{ phone | mask_phone }}", Description: "手机号打码 138****8000"},
			{Name: "datetime", Syntax: "{{ now | datetime: \"Asia/Shanghai\", \"2006-01-02 15:04\" }}", Description: "按时区与 Go 布局格式化时间"},
			{Name: "minutes_text", Syntax: "{{ minutes | minutes_text }}", Description: "90 → 1 小时 30 分钟"},
			{Name: "default", Syntax: "{{ name | default: \"朋友\" }}", Description: "空值时的默认值"},
			{Name: "safe", Syntax: "{{ html | safe }}", Description: "原样输出，不做 HTML 转义（谨慎使用）"},
			{Name: "upcase / downcase", Syntax: "{{ code | upcase }}", Description: "大小写转换"},
			{Name: "truncate", Syntax: "{{ text | truncate: 40 }}", Description: "截断"},
			{Name: "replace", Syntax: "{{ text | replace: \"a\", \"b\" }}", Description: "替换"},
			{Name: "date", Syntax: "{{ now | date: \"%Y-%m-%d\" }}", Description: "strftime 风格的日期格式化"},
		},
	}
}
