package config

import (
	"crypto/tls"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// TLSServerConfig 服务端自己终止 TLS 时的监听配置：HTTPS（TCP，HTTP/1.1 + HTTP/2）
// 与 HTTP/3（UDP，QUIC）共用同一套证书，默认也共用同一个端口号。
//
// 站在 Cloudflare / Nginx / PaaS 入口后面、由它们终止 TLS 时不需要开这个 ——
// 客户端到边缘的 HTTP/3 是边缘自己的事（Cloudflare 在面板里一键打开），回源仍是明文 HTTP_PORT。
// 只有公网流量直接打到本进程时才需要 TLS_ENABLED。详见 docs/http3.md。
//
// 相关环境变量：
//
//	TLS_ENABLED               bool；默认：配了证书文件或 ACME 域名就开
//	HTTPS_PORT                TCP 监听端口，默认 8443
//	HTTPS_PUBLIC_PORT         对外的 HTTPS 端口（端口映射时与 HTTPS_PORT 不同），默认等于 HTTPS_PORT；
//	                          明文跳转与 Alt-Svc 用的都是它
//	TLS_CERT_FILE / TLS_KEY_FILE  PEM 证书链与私钥；文件被续期替换后自动换上，不必重启
//	TLS_AUTOCERT_DOMAINS      逗号分隔；非空时改用 ACME 自动签发（与证书文件二选一）
//	TLS_AUTOCERT_EMAIL        ACME 账户邮箱（到期提醒）
//	TLS_AUTOCERT_CACHE_DIR    ACME 证书缓存目录，默认 .runtime/autocert；容器里要挂卷，否则每次重启都重签
//	TLS_AUTOCERT_DIRECTORY_URL  ACME 目录地址，留空 = Let's Encrypt 正式环境
//	TLS_MIN_VERSION           1.2 | 1.3，默认 1.2（HTTP/3 本身总是 TLS 1.3）
//	HTTPS_REDIRECT_HTTP       明文端口上的请求 308 跳到 HTTPS（/healthz、/readyz 除外），默认 false
//	HSTS_MAX_AGE              HTTPS 响应带 Strict-Transport-Security，0 = 不发，默认 0
//	HSTS_INCLUDE_SUBDOMAINS   bool，默认 false
//
//	HTTP3_ENABLED             bool，默认 true（只在 TLS 开启时生效）
//	HTTP3_PORT                UDP 监听端口，默认等于 HTTPS_PORT
//	HTTP3_PUBLIC_PORT         Alt-Svc 里宣告的 UDP 端口，默认：HTTP3_PORT 与 HTTPS_PORT 相同时取 HTTPS_PUBLIC_PORT，否则取 HTTP3_PORT
//	HTTP3_ALT_SVC_MAX_AGE     客户端记住「这里有 HTTP/3」多久，默认 24h
//	HTTP3_ALLOW_0RTT          接受 0-RTT 早期数据，默认 false；开启后非幂等请求在握手完成前到达仍回 425
//	HTTP3_MAX_IDLE_TIMEOUT    QUIC 连接空闲超时，默认 30s
//	HTTP3_KEEP_ALIVE_PERIOD   服务端主动保活间隔，默认 0（不发；移动端 NAT 映射短时可设 15s）
//	HTTP3_MAX_STREAMS         单连接并发请求流上限，默认 100
type TLSServerConfig struct {
	Enabled    bool
	Port       int
	PublicPort int

	CertFile string
	KeyFile  string

	AutocertDomains      []string
	AutocertEmail        string
	AutocertCacheDir     string
	AutocertDirectoryURL string

	MinVersion            uint16
	RedirectHTTP          bool
	HSTSMaxAge            time.Duration
	HSTSIncludeSubdomains bool

	HTTP3 HTTP3Config
}

// HTTP3Config QUIC / HTTP/3 监听参数。
type HTTP3Config struct {
	Enabled         bool
	Port            int
	PublicPort      int
	AltSvcMaxAge    time.Duration
	Allow0RTT       bool
	MaxIdleTimeout  time.Duration
	KeepAlivePeriod time.Duration
	MaxStreams      int64
}

// HSTSHeader Strict-Transport-Security 的取值；未开启时为空。
func (c TLSServerConfig) HSTSHeader() string {
	if c.HSTSMaxAge <= 0 {
		return ""
	}
	value := fmt.Sprintf("max-age=%d", int64(c.HSTSMaxAge/time.Second))
	if c.HSTSIncludeSubdomains {
		value += "; includeSubDomains"
	}
	return value
}

// UsesAutocert 是否走 ACME 自动签发。
func (c TLSServerConfig) UsesAutocert() bool { return len(c.AutocertDomains) > 0 }

func loadTLSServerConfig(v *viper.Viper) (TLSServerConfig, error) {
	cfg := TLSServerConfig{
		CertFile:              strings.TrimSpace(v.GetString("TLS_CERT_FILE")),
		KeyFile:               strings.TrimSpace(v.GetString("TLS_KEY_FILE")),
		AutocertDomains:       csvList(v.GetString("TLS_AUTOCERT_DOMAINS")),
		AutocertEmail:         strings.TrimSpace(v.GetString("TLS_AUTOCERT_EMAIL")),
		AutocertCacheDir:      strings.TrimSpace(v.GetString("TLS_AUTOCERT_CACHE_DIR")),
		AutocertDirectoryURL:  strings.TrimSpace(v.GetString("TLS_AUTOCERT_DIRECTORY_URL")),
		Port:                  v.GetInt("HTTPS_PORT"),
		PublicPort:            v.GetInt("HTTPS_PUBLIC_PORT"),
		RedirectHTTP:          getBool(v, "HTTPS_REDIRECT_HTTP", false),
		HSTSMaxAge:            getDuration(v, "HSTS_MAX_AGE", 0),
		HSTSIncludeSubdomains: getBool(v, "HSTS_INCLUDE_SUBDOMAINS", false),
		HTTP3: HTTP3Config{
			Enabled:         getBool(v, "HTTP3_ENABLED", true),
			Port:            v.GetInt("HTTP3_PORT"),
			PublicPort:      v.GetInt("HTTP3_PUBLIC_PORT"),
			AltSvcMaxAge:    getDuration(v, "HTTP3_ALT_SVC_MAX_AGE", 24*time.Hour),
			Allow0RTT:       getBool(v, "HTTP3_ALLOW_0RTT", false),
			MaxIdleTimeout:  getDuration(v, "HTTP3_MAX_IDLE_TIMEOUT", 30*time.Second),
			KeepAlivePeriod: getDuration(v, "HTTP3_KEEP_ALIVE_PERIOD", 0),
			MaxStreams:      v.GetInt64("HTTP3_MAX_STREAMS"),
		},
	}
	hasFiles := cfg.CertFile != "" || cfg.KeyFile != ""
	cfg.Enabled = getBool(v, "TLS_ENABLED", hasFiles || cfg.UsesAutocert())
	if !cfg.Enabled {
		// 关着的时候不校验其余字段：.env 里留着注释掉一半的证书路径很常见，不该因此起不来
		cfg.HTTP3.Enabled = false
		return cfg, nil
	}

	switch {
	case hasFiles && cfg.UsesAutocert():
		return cfg, errors.New("TLS_CERT_FILE/TLS_KEY_FILE 与 TLS_AUTOCERT_DOMAINS 只能二选一")
	case hasFiles && (cfg.CertFile == "" || cfg.KeyFile == ""):
		return cfg, errors.New("TLS_CERT_FILE 与 TLS_KEY_FILE 必须同时配置")
	case !hasFiles && !cfg.UsesAutocert():
		return cfg, errors.New("TLS_ENABLED=true 但没有配置证书：填 TLS_CERT_FILE/TLS_KEY_FILE 或 TLS_AUTOCERT_DOMAINS")
	}
	if cfg.UsesAutocert() && cfg.AutocertCacheDir == "" {
		cfg.AutocertCacheDir = ".runtime/autocert"
	}

	switch raw := strings.TrimSpace(v.GetString("TLS_MIN_VERSION")); raw {
	case "", "1.2":
		cfg.MinVersion = tls.VersionTLS12
	case "1.3":
		cfg.MinVersion = tls.VersionTLS13
	default:
		return cfg, fmt.Errorf("TLS_MIN_VERSION=%q 无效，只接受 1.2 或 1.3", raw)
	}

	if cfg.Port == 0 {
		cfg.Port = 8443
	}
	if cfg.PublicPort == 0 {
		cfg.PublicPort = cfg.Port
	}
	if cfg.HTTP3.Port == 0 {
		cfg.HTTP3.Port = cfg.Port
	}
	if cfg.HTTP3.PublicPort == 0 {
		if cfg.HTTP3.Port == cfg.Port {
			cfg.HTTP3.PublicPort = cfg.PublicPort
		} else {
			cfg.HTTP3.PublicPort = cfg.HTTP3.Port
		}
	}
	if cfg.HTTP3.MaxStreams <= 0 {
		cfg.HTTP3.MaxStreams = 100
	}
	for name, port := range map[string]int{"HTTPS_PORT": cfg.Port, "HTTPS_PUBLIC_PORT": cfg.PublicPort, "HTTP3_PORT": cfg.HTTP3.Port, "HTTP3_PUBLIC_PORT": cfg.HTTP3.PublicPort} {
		if port < 1 || port > 65535 {
			return cfg, fmt.Errorf("%s=%d 超出端口范围", name, port)
		}
	}
	return cfg, nil
}
