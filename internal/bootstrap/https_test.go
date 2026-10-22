package bootstrap

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"aegis/internal/config"

	"github.com/gin-gonic/gin"
	"github.com/quic-go/quic-go/http3"
	"go.uber.org/zap"
)

func writeTestCert(t *testing.T) (*x509.CertPool, string, string) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "aegis"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, _ := x509.MarshalECPrivateKey(key)
	dir := t.TempDir()
	certPath, keyPath := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	_ = os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	_ = os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600)
	leaf, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return pool, certPath, keyPath
}

// 真实的 gin 路由经 APIApp.Serve 跑在 HTTPS 与 HTTP/3 上：流式输出能逐块 Flush，请求体能读到，
// 明文端口按配置 308 到 HTTPS、健康检查不跳。
func TestAPIAppServesGinOverHTTP3(t *testing.T) {
	gin.SetMode(gin.TestMode)
	pool, certPath, keyPath := writeTestCert(t)

	router := gin.New()
	router.GET("/healthz", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	router.POST("/echo", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		c.String(http.StatusOK, c.Request.Proto+" "+string(body))
	})
	router.GET("/stream", func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		for i := 0; i < 3; i++ {
			_, _ = io.WriteString(c.Writer, "data: tick\n\n")
			c.Writer.Flush()
		}
	})

	plainLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_ = plainLn.Close()

	app := &APIApp{
		Config: config.Config{TLSServer: config.TLSServerConfig{
			Enabled:      true,
			CertFile:     certPath,
			KeyFile:      keyPath,
			MinVersion:   tls.VersionTLS12,
			RedirectHTTP: true,
			PublicPort:   8443,
			HSTSMaxAge:   time.Hour,
			HTTP3:        config.HTTP3Config{Enabled: true, AltSvcMaxAge: time.Hour, MaxIdleTimeout: 30 * time.Second, MaxStreams: 100},
		}},
		Logger: zap.NewNop(),
		Router: router,
		Server: &http.Server{Addr: plainLn.Addr().String(), Handler: router, ReadHeaderTimeout: 5 * time.Second},
	}
	if err := app.Serve(func(err error) { t.Errorf("serve: %v", err) }); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = app.Secure.Shutdown(ctx)
		_ = app.Server.Shutdown(ctx)
	})
	// 监听在 ":0"（所有网卡），连的时候用证书里写的 127.0.0.1
	tcpAddr, udpAddr := app.Secure.Addrs()
	_, tcpPort, _ := net.SplitHostPort(tcpAddr.String())
	_, udpPort, _ := net.SplitHostPort(udpAddr.String())

	// HTTPS（TCP）：HTTP/2 + Alt-Svc + HSTS
	h2 := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}, ForceAttemptHTTP2: true}}
	resp, err := h2.Get("https://127.0.0.1:" + tcpPort + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.ProtoMajor != 2 || resp.Header.Get("Alt-Svc") != `h3=":`+udpPort+`"; ma=3600` || resp.Header.Get("Strict-Transport-Security") != "max-age=3600" {
		t.Fatalf("HTTPS 响应不对：proto=%s alt-svc=%q hsts=%q", resp.Proto, resp.Header.Get("Alt-Svc"), resp.Header.Get("Strict-Transport-Security"))
	}

	// HTTP/3（UDP）
	tr := &http3.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}
	defer tr.Close()
	h3 := &http.Client{Transport: tr, Timeout: 5 * time.Second}
	base := "https://127.0.0.1:" + udpPort

	resp, err = h3.Post(base+"/echo", "text/plain", strings.NewReader("hello"))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if string(body) != "HTTP/3.0 hello" {
		t.Fatalf("HTTP/3 POST body = %q", body)
	}

	resp, err = h3.Get(base + "/stream")
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if strings.Count(string(body), "data: tick") != 3 || resp.Header.Get("Strict-Transport-Security") == "" {
		t.Fatalf("HTTP/3 流式响应不对：%q", body)
	}

	// 明文端口：跳转 HTTPS，健康检查例外
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	plainBase := "http://" + app.Server.Addr
	var plainResp *http.Response
	for i := 0; i < 50; i++ { // ListenAndServe 在后台起，稍等它绑上端口
		if plainResp, err = noFollow.Get(plainBase + "/echo"); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	_ = plainResp.Body.Close()
	if plainResp.StatusCode != http.StatusPermanentRedirect || plainResp.Header.Get("Location") != "https://127.0.0.1:8443/echo" {
		t.Fatalf("明文跳转不对：%d %q", plainResp.StatusCode, plainResp.Header.Get("Location"))
	}
	plainResp, err = noFollow.Get(plainBase + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	_ = plainResp.Body.Close()
	if plainResp.StatusCode != http.StatusOK {
		t.Fatalf("健康检查被跳转了：%d", plainResp.StatusCode)
	}
}
