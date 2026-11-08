package httpserve

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
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/quic-go/quic-go/http3"
)

// writeSelfSigned 在 dir 下写一对 127.0.0.1 的自签证书，返回证书池与文件路径。
func writeSelfSigned(t *testing.T, dir, cn string) (*x509.CertPool, string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPath, keyPath := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return pool, certPath, keyPath
}

func startServer(t *testing.T, opts Options) (*Server, *x509.CertPool) {
	t.Helper()
	pool, certPath, keyPath := writeSelfSigned(t, t.TempDir(), "aegis-test")
	bundle, err := BuildTLSConfig(CertSource{CertFile: certPath, KeyFile: keyPath}, 0)
	if err != nil {
		t.Fatal(err)
	}
	opts.TLS = bundle.Config
	if opts.Addr == "" {
		opts.Addr = "127.0.0.1:0"
	}
	srv, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Listen(); err != nil {
		t.Fatal(err)
	}
	srv.Serve(func(err error) { t.Errorf("serve: %v", err) })
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	return srv, pool
}

func echoProto() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, r.Method+" "+r.Proto)
	})
}

func TestHTTPSAdvertisesAndServesHTTP3(t *testing.T) {
	srv, pool := startServer(t, Options{
		HTTP3Addr:    "127.0.0.1:0",
		Handler:      echoProto(),
		AltSvcMaxAge: time.Hour,
		HSTS:         "max-age=63072000",
	})
	tcpAddr, udpAddr := srv.Addrs()

	// TCP：HTTP/2，带 Alt-Svc 与 HSTS
	h2 := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}, ForceAttemptHTTP2: true}}
	resp, err := h2.Get("https://" + tcpAddr.String() + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.ProtoMajor != 2 {
		t.Fatalf("TCP 侧应协商出 HTTP/2，实际 %s", resp.Proto)
	}
	wantAltSvc := `h3=":` + strings.Split(udpAddr.String(), ":")[1] + `"; ma=3600`
	if got := resp.Header.Get("Alt-Svc"); got != wantAltSvc {
		t.Fatalf("Alt-Svc = %q，期望 %q", got, wantAltSvc)
	}
	if resp.Header.Get("Strict-Transport-Security") == "" {
		t.Fatal("缺少 HSTS")
	}
	if string(body) != "GET HTTP/2.0" {
		t.Fatalf("body = %q", body)
	}

	// UDP：HTTP/3，GET 与 POST 都能通
	h3 := &http.Client{Transport: &http3.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}}
	defer h3.Transport.(*http3.Transport).Close()
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		req, _ := http.NewRequest(method, "https://"+udpAddr.String()+"/", strings.NewReader("x"))
		resp, err := h3.Do(req)
		if err != nil {
			t.Fatalf("HTTP/3 %s: %v", method, err)
		}
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if resp.ProtoMajor != 3 || string(body) != method+" HTTP/3.0" {
			t.Fatalf("HTTP/3 %s: proto=%s body=%q", method, resp.Proto, body)
		}
		if resp.Header.Get("Alt-Svc") != "" {
			t.Fatal("HTTP/3 响应不必再宣告 Alt-Svc")
		}
	}
}

func TestHTTPSOnlyHasNoAltSvc(t *testing.T) {
	srv, pool := startServer(t, Options{Handler: echoProto()})
	tcpAddr, udpAddr := srv.Addrs()
	if udpAddr != nil || srv.HTTP3Enabled() {
		t.Fatal("未配置 HTTP3Addr 不应监听 UDP")
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}}}
	resp, err := client.Get("https://" + tcpAddr.String() + "/")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.Header.Get("Alt-Svc") != "" {
		t.Fatal("没开 HTTP/3 却宣告了 Alt-Svc")
	}
}

func TestAltSvcUsesAdvertisedPort(t *testing.T) {
	srv, _ := startServer(t, Options{HTTP3Addr: "127.0.0.1:0", AltSvcPort: 443, Handler: echoProto()})
	if got := srv.AltSvc(); got != `h3=":443"; ma=86400` {
		t.Fatalf("Alt-Svc = %q", got)
	}
}

func TestShutdownStopsAdvertising(t *testing.T) {
	srv, _ := startServer(t, Options{HTTP3Addr: "127.0.0.1:0", Handler: echoProto()})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	srv.https.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Header().Get("Alt-Svc") != "" {
		t.Fatal("关闭后仍在宣告 HTTP/3")
	}
}

func TestCertReloaderPicksUpRenewal(t *testing.T) {
	dir := t.TempDir()
	_, certPath, keyPath := writeSelfSigned(t, dir, "first")
	r, err := NewCertReloader(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	first := r.Current()

	// 续期：同一路径写入新证书，修改时间往后挪，跳过节流窗口
	writeSelfSigned(t, dir, "second")
	later := time.Now().Add(time.Minute)
	_ = os.Chtimes(certPath, later, later)
	_ = os.Chtimes(keyPath, later, later)
	r.nextCheck.Store(0)
	got, _ := r.GetCertificate(nil)
	if got == first {
		t.Fatal("证书文件更新后没有换上新证书")
	}

	// 写坏的文件：继续用当前证书
	_ = os.WriteFile(certPath, []byte("garbage"), 0o600)
	evenLater := later.Add(time.Minute)
	_ = os.Chtimes(certPath, evenLater, evenLater)
	r.nextCheck.Store(0)
	if kept, _ := r.GetCertificate(nil); kept != got {
		t.Fatal("新证书读坏时应保留旧证书")
	}
}

func TestRedirectToHTTPS(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) })
	h := RedirectToHTTPS(8443, func(p string) bool { return p == "/healthz" }, ok)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "http://api.example.com:8088/api/x?a=1", nil))
	if rec.Code != http.StatusPermanentRedirect || rec.Header().Get("Location") != "https://api.example.com:8443/api/x?a=1" {
		t.Fatalf("code=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://api.example.com/healthz", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("健康检查不应被跳转，code=%d", rec.Code)
	}

	rec = httptest.NewRecorder()
	RedirectToHTTPS(443, nil, ok).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://api.example.com/", nil))
	if rec.Header().Get("Location") != "https://api.example.com/" {
		t.Fatalf("443 不应出现在跳转地址里：%q", rec.Header().Get("Location"))
	}
}

func TestEarlyDataGuardPassesNonQUICRequests(t *testing.T) {
	h := rejectUnsafeEarlyData(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("code=%d", rec.Code)
	}
}
