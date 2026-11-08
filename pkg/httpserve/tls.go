package httpserve

import (
	"crypto/tls"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

// CertSource 是服务端证书的来源：证书文件对，或 ACME 自动签发，二选一。
type CertSource struct {
	CertFile string
	KeyFile  string

	// AutocertDomains 非空时改用 ACME 自动签发（Let's Encrypt 等），只为这些域名签。
	AutocertDomains  []string
	AutocertCacheDir string
	AutocertEmail    string
	// AutocertDirectoryURL 留空用 Let's Encrypt 正式环境；联调时填 staging 地址，免得撞签发频率限制。
	AutocertDirectoryURL string
}

// TLSBundle 是建好的 TLS 配置，外加 ACME 管理器（仅自动签发时非 nil）。
//
// 管理器要单独交出去：HTTP-01 质询走明文端口，明文监听要用 Manager.HTTPHandler 包一层。
type TLSBundle struct {
	Config  *tls.Config
	Manager *autocert.Manager
	Reload  *CertReloader
}

// BuildTLSConfig 按证书来源建出 TCP（h2 + http/1.1）那一侧的 TLS 配置。
// HTTP/3 那一侧由 http3.ConfigureTLSConfig 在它的基础上换 ALPN，证书来源是同一个。
func BuildTLSConfig(src CertSource, minVersion uint16) (*TLSBundle, error) {
	if minVersion == 0 {
		minVersion = tls.VersionTLS12
	}
	conf := &tls.Config{
		MinVersion: minVersion,
		NextProtos: []string{"h2", "http/1.1"},
	}
	bundle := &TLSBundle{Config: conf}

	switch {
	case len(src.AutocertDomains) > 0:
		mgr := &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(src.AutocertDomains...),
			Cache:      autocert.DirCache(src.AutocertCacheDir),
			Email:      src.AutocertEmail,
		}
		if src.AutocertDirectoryURL != "" {
			mgr.Client = &acme.Client{DirectoryURL: src.AutocertDirectoryURL}
		}
		conf.GetCertificate = mgr.GetCertificate
		// TLS-ALPN-01 质询靠这个 ALPN 进来；只有 HTTPS 端口对外就是 443 时它才走得通，
		// 否则 ACME 会退到明文端口上的 HTTP-01。
		conf.NextProtos = append(conf.NextProtos, acme.ALPNProto)
		bundle.Manager = mgr
	case src.CertFile != "" && src.KeyFile != "":
		reloader, err := NewCertReloader(src.CertFile, src.KeyFile)
		if err != nil {
			return nil, err
		}
		conf.GetCertificate = reloader.GetCertificate
		bundle.Reload = reloader
	default:
		return nil, errors.New("httpserve: TLS 需要证书文件对或 ACME 域名")
	}
	return bundle, nil
}

// certCheckInterval 两次检查证书文件是否变更的最小间隔。
// 握手是热路径，不能每次都 stat；证书续期是按天算的，半分钟的延迟无所谓。
const certCheckInterval = 30 * time.Second

// CertReloader 从磁盘加载证书，文件被替换（certbot / acme.sh 续期）后自动换上新证书，不必重启。
//
// 新文件读坏了（比如续期脚本只写了一半）就继续用旧证书，等下一轮再试 —— 握手失败比证书晚换半分钟严重得多。
type CertReloader struct {
	certFile string
	keyFile  string

	cert      atomic.Pointer[tls.Certificate]
	nextCheck atomic.Int64

	mu       sync.Mutex
	certMod  time.Time
	keyMod   time.Time
	onReload func(error)
}

// NewCertReloader 立即加载一次；首次加载失败直接返回错误，好让启动失败而不是带着坏配置跑起来。
func NewCertReloader(certFile, keyFile string) (*CertReloader, error) {
	r := &CertReloader{certFile: certFile, keyFile: keyFile}
	if err := r.load(); err != nil {
		return nil, err
	}
	return r, nil
}

// OnReload 设置换证书（成功或失败）后的回调，用于打日志。
func (r *CertReloader) OnReload(fn func(error)) {
	r.mu.Lock()
	r.onReload = fn
	r.mu.Unlock()
}

// GetCertificate 实现 tls.Config.GetCertificate。
func (r *CertReloader) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	now := time.Now().UnixNano()
	if next := r.nextCheck.Load(); now >= next && r.nextCheck.CompareAndSwap(next, now+int64(certCheckInterval)) {
		r.maybeReload()
	}
	return r.cert.Load(), nil
}

// Current 当前在用的证书。
func (r *CertReloader) Current() *tls.Certificate { return r.cert.Load() }

func (r *CertReloader) maybeReload() {
	certInfo, err1 := os.Stat(r.certFile)
	keyInfo, err2 := os.Stat(r.keyFile)
	if err1 != nil || err2 != nil {
		return
	}
	r.mu.Lock()
	changed := !certInfo.ModTime().Equal(r.certMod) || !keyInfo.ModTime().Equal(r.keyMod)
	cb := r.onReload
	r.mu.Unlock()
	if !changed {
		return
	}
	err := r.load()
	if cb != nil {
		cb(err)
	}
}

func (r *CertReloader) load() error {
	certInfo, err := os.Stat(r.certFile)
	if err != nil {
		return fmt.Errorf("httpserve: 读取证书 %s: %w", r.certFile, err)
	}
	keyInfo, err := os.Stat(r.keyFile)
	if err != nil {
		return fmt.Errorf("httpserve: 读取私钥 %s: %w", r.keyFile, err)
	}
	pair, err := tls.LoadX509KeyPair(r.certFile, r.keyFile)

	r.mu.Lock()
	defer r.mu.Unlock()
	// 读坏了也记下这次的修改时间：同一份坏文件不必每半分钟重读一遍，等它再被改动时再试
	r.certMod, r.keyMod = certInfo.ModTime(), keyInfo.ModTime()
	if err != nil {
		return fmt.Errorf("httpserve: 加载证书: %w", err)
	}
	r.cert.Store(&pair)
	return nil
}
