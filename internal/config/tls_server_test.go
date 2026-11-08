package config

import (
	"crypto/tls"
	"testing"
	"time"

	"github.com/spf13/viper"
)

func tlsViper(kv map[string]string) *viper.Viper {
	v := viper.New()
	for k, val := range kv {
		v.Set(k, val)
	}
	return v
}

func TestTLSServerDisabledByDefault(t *testing.T) {
	cfg, err := loadTLSServerConfig(tlsViper(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Enabled || cfg.HTTP3.Enabled {
		t.Fatalf("没配证书不应开启 TLS：%+v", cfg)
	}
}

func TestTLSServerDefaultsFromCertFiles(t *testing.T) {
	cfg, err := loadTLSServerConfig(tlsViper(map[string]string{
		"TLS_CERT_FILE":     "/etc/aegis/tls/fullchain.pem",
		"TLS_KEY_FILE":      "/etc/aegis/tls/privkey.pem",
		"HTTPS_PUBLIC_PORT": "443",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled || !cfg.HTTP3.Enabled {
		t.Fatal("配了证书应自动开启 HTTPS 与 HTTP/3")
	}
	if cfg.Port != 8443 || cfg.HTTP3.Port != 8443 || cfg.HTTP3.PublicPort != 443 {
		t.Fatalf("端口默认值不对：https=%d h3=%d h3public=%d", cfg.Port, cfg.HTTP3.Port, cfg.HTTP3.PublicPort)
	}
	if cfg.MinVersion != tls.VersionTLS12 || cfg.HTTP3.MaxStreams != 100 || cfg.HTTP3.Allow0RTT {
		t.Fatalf("默认值不对：%+v", cfg)
	}
	if cfg.HSTSHeader() != "" {
		t.Fatal("HSTS 默认不发")
	}
}

func TestTLSServerSeparateHTTP3Port(t *testing.T) {
	cfg, err := loadTLSServerConfig(tlsViper(map[string]string{
		"TLS_AUTOCERT_DOMAINS":    "api.example.com, auth.example.com",
		"HTTPS_PUBLIC_PORT":       "443",
		"HTTP3_PORT":              "9443",
		"HSTS_MAX_AGE":            "8760h",
		"HSTS_INCLUDE_SUBDOMAINS": "true",
		"TLS_MIN_VERSION":         "1.3",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HTTP3.PublicPort != 9443 {
		t.Fatalf("HTTP/3 单独端口时应宣告它自己，实际 %d", cfg.HTTP3.PublicPort)
	}
	if len(cfg.AutocertDomains) != 2 || cfg.AutocertCacheDir != ".runtime/autocert" {
		t.Fatalf("ACME 配置不对：%+v", cfg)
	}
	if cfg.MinVersion != tls.VersionTLS13 {
		t.Fatal("TLS_MIN_VERSION=1.3 未生效")
	}
	if got := cfg.HSTSHeader(); got != "max-age=31536000; includeSubDomains" {
		t.Fatalf("HSTS = %q", got)
	}
	if cfg.HTTP3.AltSvcMaxAge != 24*time.Hour {
		t.Fatalf("Alt-Svc ma 默认 24h，实际 %s", cfg.HTTP3.AltSvcMaxAge)
	}
}

func TestTLSServerRejectsBadConfig(t *testing.T) {
	cases := map[string]map[string]string{
		"开了却没证书":    {"TLS_ENABLED": "true"},
		"只有证书没私钥":   {"TLS_CERT_FILE": "a.pem"},
		"文件与ACME并存": {"TLS_CERT_FILE": "a.pem", "TLS_KEY_FILE": "b.pem", "TLS_AUTOCERT_DOMAINS": "x.com"},
		"TLS版本写错":   {"TLS_CERT_FILE": "a.pem", "TLS_KEY_FILE": "b.pem", "TLS_MIN_VERSION": "1.1"},
		"端口越界":      {"TLS_CERT_FILE": "a.pem", "TLS_KEY_FILE": "b.pem", "HTTP3_PORT": "70000"},
	}
	for name, kv := range cases {
		if _, err := loadTLSServerConfig(tlsViper(kv)); err == nil {
			t.Errorf("%s：应当报错", name)
		}
	}
	// 显式关掉时，留着的半截配置不该挡住启动
	if _, err := loadTLSServerConfig(tlsViper(map[string]string{"TLS_ENABLED": "false", "TLS_CERT_FILE": "a.pem"})); err != nil {
		t.Fatalf("TLS_ENABLED=false 时不应校验：%v", err)
	}
}
