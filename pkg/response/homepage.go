package response

import (
	"bytes"
	"html/template"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// ─────────────────────────────────────────────────────────────────────
// HTML 首页
//
// 面向两类访客：找控制台入口的管理员，和找接入地址的开发者。
// 所以首屏只放这两样东西，能力介绍压成一张表，不做卖点卡片。
// 纯 CSS、无脚本、无外链字体（CSP 为 default-src 'none'）。
// ─────────────────────────────────────────────────────────────────────

// HomePageData 首页可填充的动态数据。
type HomePageData struct {
	// Version 为空时不展示版本行。
	Version string
	// ConsoleURL 控制台地址；为空时不展示控制台入口（同源部署并没有 /console）。
	ConsoleURL string
	// DocsURL 开发者门户地址；为空时退回 /docs（由后端 302 到门户）。
	DocsURL string
	Uptime  string
}

// RenderHomepage 把首页 HTML 写入响应。
func RenderHomepage(c *gin.Context, data HomePageData) {
	setMetaHeaders(c)
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Header("Cache-Control", "public, max-age=60")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Referrer-Policy", "same-origin")
	c.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src data:; base-uri 'none'; form-action 'self'")

	view := homePageView{
		Data:    data,
		BaseURL: requestBaseURL(c),
		Year:    time.Now().Year(),
	}
	if view.Data.DocsURL == "" {
		view.Data.DocsURL = "/docs"
	}
	if view.Data.Uptime == "" {
		view.Data.Uptime = formatUptime(time.Since(processStart))
	}

	var buf bytes.Buffer
	if err := homePageTemplate.Execute(&buf, view); err != nil {
		c.Header("Content-Type", "text/plain; charset=utf-8")
		_, _ = c.Writer.WriteString("Aegis")
		return
	}
	c.Status(200)
	_, _ = c.Writer.Write(buf.Bytes())
}

type homePageView struct {
	Data    HomePageData
	BaseURL string
	Year    int
}

var processStart = time.Now()

// requestBaseURL 还原访客看到的站点地址。协议只信反代写入的 X-Forwarded-Proto
// 的第一段：这里只用于展示，判错的代价是页面上多一个 s 或少一个 s。
func requestBaseURL(c *gin.Context) string {
	req := c.Request
	if req == nil || req.Host == "" {
		return ""
	}
	scheme := "http"
	proto, _, _ := strings.Cut(req.Header.Get("X-Forwarded-Proto"), ",")
	if req.TLS != nil || strings.EqualFold(strings.TrimSpace(proto), "https") {
		scheme = "https"
	}
	return scheme + "://" + req.Host
}

// formatUptime 输出「3 天 4 小时」「5 小时 12 分钟」这种中文时长，只保留两级。
func formatUptime(d time.Duration) string {
	days := int(d / (24 * time.Hour))
	hours := int(d/time.Hour) % 24
	minutes := int(d/time.Minute) % 60
	switch {
	case days > 0:
		return strconv.Itoa(days) + " 天 " + strconv.Itoa(hours) + " 小时"
	case hours > 0:
		return strconv.Itoa(hours) + " 小时 " + strconv.Itoa(minutes) + " 分钟"
	case minutes > 0:
		return strconv.Itoa(minutes) + " 分钟"
	default:
		return "不到 1 分钟"
	}
}

var homePageTemplate = template.Must(template.New("home").Parse(homePageSource))

const homePageSource = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="description" content="Aegis 统一用户平台，为多个应用提供账号、登录、权限、会员与支付服务。">
  <meta name="theme-color" content="#f6f6f4" media="(prefers-color-scheme: light)">
  <meta name="theme-color" content="#111214" media="(prefers-color-scheme: dark)">
  <title>Aegis 统一用户平台</title>
  <style>
    :root {
      --bg: #f6f6f4; --surface: #ffffff; --surface-sunk: #efefec;
      --ink: #16181d; --body: #3b3f47; --muted: #6b7079;
      --line: #e3e4e0; --line-strong: #d3d5d0;
      --accent: #1d6b52; --on-ink: #ffffff;
    }
    @media (prefers-color-scheme: dark) {
      :root {
        --bg: #111214; --surface: #18191c; --surface-sunk: #1e2023;
        --ink: #ececea; --body: #c4c6c9; --muted: #8c9097;
        --line: #2a2c30; --line-strong: #383b40;
        --accent: #5cb894; --on-ink: #111214;
      }
    }
    *, *::before, *::after { box-sizing: border-box; }
    html { -webkit-text-size-adjust: 100%; }
    body { margin: 0; min-height: 100vh; display: flex; flex-direction: column; background: var(--bg); color: var(--body); font-family: -apple-system, BlinkMacSystemFont, "PingFang SC", "HarmonyOS Sans SC", "Microsoft YaHei UI", "Noto Sans CJK SC", "Segoe UI", sans-serif; font-size: 15px; line-height: 1.7; -webkit-font-smoothing: antialiased; }
    a { color: inherit; text-decoration: none; }
    a:focus-visible { outline: 2px solid var(--accent); outline-offset: 3px; border-radius: 4px; }
    .mono { font-family: "JetBrains Mono", "SF Mono", ui-monospace, Menlo, Consolas, monospace; font-size: 13px; }
    .wrap { width: 100%; max-width: 1080px; margin: 0 auto; padding: 0 32px; }

    .top { border-bottom: 1px solid var(--line); }
    .top .wrap { height: 64px; display: flex; align-items: center; gap: 32px; }
    .logo { display: inline-flex; align-items: center; gap: 10px; color: var(--ink); font-size: 17px; font-weight: 600; letter-spacing: -0.01em; }
    .logo-mark { width: 26px; height: 26px; display: inline-flex; align-items: center; justify-content: center; background: var(--ink); color: var(--bg); border-radius: 7px; }
    .logo-mark svg { width: 16px; height: 16px; display: block; }
    .nav { margin-left: auto; display: flex; align-items: center; gap: 28px; font-size: 14px; }
    .nav a { color: var(--muted); transition: color 120ms linear; }
    .nav a:hover { color: var(--ink); }
    .nav .nav-console { padding: 6px 14px; color: var(--ink); border: 1px solid var(--line-strong); border-radius: 8px; transition: background-color 120ms linear; }
    .nav .nav-console:hover { background: var(--surface); }

    .hero { padding: 96px 0 104px; }
    .hero .wrap { display: grid; grid-template-columns: minmax(0, 1fr) 400px; gap: 72px; align-items: center; }
    .hero h1 { margin: 0; color: var(--ink); font-size: clamp(32px, 4.2vw, 46px); font-weight: 650; line-height: 1.22; letter-spacing: -0.02em; }
    .hero-lead { margin: 22px 0 0; max-width: 30em; font-size: 17px; line-height: 1.8; text-wrap: pretty; }
    .hero-actions { margin-top: 36px; display: flex; flex-wrap: wrap; gap: 12px; }
    .btn { display: inline-flex; align-items: center; height: 42px; padding: 0 20px; font-size: 15px; font-weight: 500; border-radius: 8px; transition: background-color 120ms linear, border-color 120ms linear; }
    .btn-primary { background: var(--ink); color: var(--on-ink); }
    .btn-primary:hover { background: var(--body); }
    .btn-plain { color: var(--ink); border: 1px solid var(--line-strong); }
    .btn-plain:hover { background: var(--surface); }

    .panel { background: var(--surface); border: 1px solid var(--line); border-radius: 12px; }
    .panel-head { padding: 14px 20px; color: var(--ink); font-size: 14px; font-weight: 600; border-bottom: 1px solid var(--line); }
    .panel dl { margin: 0; padding: 4px 20px; }
    .panel .row { display: grid; grid-template-columns: 72px minmax(0, 1fr); gap: 16px; align-items: baseline; padding: 12px 0; }
    .panel .row + .row { border-top: 1px solid var(--line); }
    .panel dt { color: var(--muted); font-size: 13px; }
    .panel dd { margin: 0; color: var(--ink); overflow-wrap: anywhere; }
    .panel dd a { border-bottom: 1px solid var(--line-strong); transition: border-color 120ms linear; }
    .panel dd a:hover { border-color: var(--ink); }

    .caps { padding: 80px 0 96px; background: var(--surface); border-top: 1px solid var(--line); border-bottom: 1px solid var(--line); }
    .caps .wrap { display: grid; grid-template-columns: 240px minmax(0, 1fr); gap: 48px; }
    .caps h2 { margin: 0; color: var(--ink); font-size: 22px; font-weight: 600; letter-spacing: -0.01em; }
    .cap-list { margin: 0; padding: 0; list-style: none; border-top: 1px solid var(--ink); }
    .cap { display: grid; grid-template-columns: 160px minmax(0, 1fr); gap: 24px; padding: 18px 0; border-bottom: 1px solid var(--line); }
    .cap-name { color: var(--ink); font-weight: 600; }
    .cap-desc { margin: 0; }

    .foot { margin-top: auto; padding: 28px 0 36px; color: var(--muted); font-size: 13px; }
    .foot .wrap { display: flex; flex-wrap: wrap; align-items: center; gap: 12px 28px; }
    .foot-links { display: flex; flex-wrap: wrap; gap: 8px 24px; }
    .foot-links a { transition: color 120ms linear; }
    .foot-links a:hover { color: var(--ink); }
    .foot-copy { margin-left: auto; }

    @media (max-width: 920px) {
      .hero { padding: 64px 0 72px; }
      .hero .wrap { grid-template-columns: 1fr; gap: 48px; }
      .caps { padding: 56px 0 64px; }
      .caps .wrap { grid-template-columns: 1fr; gap: 24px; }
    }
    @media (max-width: 600px) {
      .wrap { padding: 0 20px; }
      .top .wrap { height: 56px; }
      .nav { gap: 20px; }
      .nav .nav-optional { display: none; }
      .hero { padding: 44px 0 56px; }
      .hero-lead { font-size: 16px; }
      .btn { flex: 1 1 0; justify-content: center; }
      .cap { grid-template-columns: 1fr; gap: 4px; padding: 16px 0; }
      .foot-copy { margin-left: 0; }
    }
  </style>
</head>
<body>
  <header class="top">
    <div class="wrap">
      <a class="logo" href="/" aria-label="Aegis 首页">
        <span class="logo-mark" aria-hidden="true"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3 19 6v5.5c0 4.3-2.9 7.6-7 9.5-4.1-1.9-7-5.2-7-9.5V6z"/><path d="m9 12 2.2 2.2L15.5 10"/></svg></span>
        <span>Aegis</span>
      </a>
      <nav class="nav" aria-label="主导航">
        <a class="nav-optional" href="/announcements">公告</a>
        <a class="nav-optional" href="/status">服务状态</a>
        <a href="{{.Data.DocsURL}}">开发文档</a>
        {{if .Data.ConsoleURL}}<a class="nav-console" href="{{.Data.ConsoleURL}}">控制台</a>{{end}}
      </nav>
    </div>
  </header>

  <main>
    <section class="hero">
      <div class="wrap">
        <div>
          <h1>统一用户平台</h1>
          <p class="hero-lead">Aegis 为多个应用集中提供账号、登录、权限、会员与支付服务。各应用的用户数据相互隔离，在同一个控制台中管理。</p>
          <div class="hero-actions">
            {{if .Data.ConsoleURL}}<a class="btn btn-primary" href="{{.Data.ConsoleURL}}">进入控制台</a>{{end}}
            <a class="btn {{if .Data.ConsoleURL}}btn-plain{{else}}btn-primary{{end}}" href="{{.Data.DocsURL}}">查看开发文档</a>
          </div>
        </div>
        <aside class="panel" aria-labelledby="access-title">
          <div class="panel-head" id="access-title">接入信息</div>
          <dl>
            {{if .BaseURL}}<div class="row"><dt>接口地址</dt><dd class="mono">{{.BaseURL}}</dd></div>{{end}}
            <div class="row"><dt>接口规范</dt><dd class="mono"><a href="/openapi.json">/openapi.json</a></dd></div>
            <div class="row"><dt>健康检查</dt><dd class="mono"><a href="/healthz">/healthz</a></dd></div>
            {{if .Data.Version}}<div class="row"><dt>服务版本</dt><dd class="mono">{{.Data.Version}}</dd></div>{{end}}
            <div class="row"><dt>已运行</dt><dd>{{.Data.Uptime}}</dd></div>
          </dl>
        </aside>
      </div>
    </section>

    <section class="caps" aria-labelledby="caps-title">
      <div class="wrap">
        <h2 id="caps-title">平台能力</h2>
        <ul class="cap-list">
          <li class="cap"><span class="cap-name">账号与登录</span><p class="cap-desc">密码、短信验证码、两步验证、通行密钥与第三方账号登录，支持找回密码与登录设备管理。</p></li>
          <li class="cap"><span class="cap-name">授权与单点登录</span><p class="cap-desc">对外提供 OAuth2 授权服务。控制台管理员可通过 LDAP、OIDC 或 SAML 登录。</p></li>
          <li class="cap"><span class="cap-name">应用与权限</span><p class="cap-desc">每个应用拥有独立的用户库与配置，按角色分配权限，支持组织与部门管理。</p></li>
          <li class="cap"><span class="cap-name">会员与支付</span><p class="cap-desc">会员、钱包、卡密与在线支付，以及签到、积分与排行榜。</p></li>
          <li class="cap"><span class="cap-name">运营与服务</span><p class="cap-desc">公告、站内通知、工单、版本发布与激励广告。</p></li>
          <li class="cap"><span class="cap-name">存储</span><p class="cap-desc">对象存储与用户云存储，可接入多家云服务商。</p></li>
          <li class="cap"><span class="cap-name">安全与审计</span><p class="cap-desc">应用级传输加密、防火墙、IP 与地区访问限制、风险识别与审计日志。</p></li>
        </ul>
      </div>
    </section>
  </main>

  <footer class="foot">
    <div class="wrap">
      <nav class="foot-links" aria-label="站点链接">
        <a href="/announcements">系统公告</a>
        <a href="/status">服务状态</a>
        <a href="/cons-env">网络环境检测</a>
        <a href="{{.Data.DocsURL}}">开发文档</a>
      </nav>
      <span class="foot-copy">© {{.Year}} Aegis</span>
    </div>
  </footer>
</body>
</html>`
