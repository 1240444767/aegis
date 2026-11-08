// Package httpserve 让服务端自己终止 TLS：同一个端口号上，TCP 跑 HTTPS（HTTP/1.1 + HTTP/2），
// UDP 跑 HTTP/3（QUIC），并通过 Alt-Svc 告诉客户端可以升级到 HTTP/3。
//
// 浏览器、Cronet、curl --http3 这些客户端第一次总是走 TCP，看到 Alt-Svc 之后才在后台试 QUIC，
// 试通了下一个请求起就走 HTTP/3；UDP 被防火墙挡住时它们会自动退回 TCP —— 所以 TCP 监听不能省。
//
// WebSocket 不走 HTTP/3（quic-go 不支持 RFC 9220 的扩展 CONNECT），客户端会为 WebSocket 单独开 TCP 连接，
// 这是所有 HTTP/3 服务端的常态，不需要额外处理。
package httpserve

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/quic-go/quic-go/http3"
)

// Options 是一组 HTTPS + HTTP/3 监听的参数。
type Options struct {
	// Addr TCP 监听地址（HTTPS），如 ":8443"。
	Addr string
	// HTTP3Addr UDP 监听地址；留空表示不开 HTTP/3，只跑 HTTPS。
	HTTP3Addr string
	// TLS 来自 BuildTLSConfig 的配置（ALPN 为 h2 / http/1.1），HTTP/3 一侧会在它的副本上把 ALPN 换成 h3。
	TLS     *tls.Config
	Handler http.Handler

	// AltSvcPort Alt-Svc 里宣告的 UDP 端口。容器把宿主机 443 映射到容器 8443 时，这里要写 443 ——
	// 客户端拿到的是对外端口，写成容器内端口它就连不上了。0 表示取 HTTP3Addr 的端口。
	AltSvcPort   int
	AltSvcMaxAge time.Duration
	// HSTS Strict-Transport-Security 的完整取值；留空不发。
	HSTS string
	// Allow0RTT 接受 QUIC 0-RTT（早期数据）。早期数据可以被重放，所以即便开着，
	// 非幂等方法（POST/PUT/PATCH/DELETE…）在握手完成前到达也会回 425 Too Early，客户端会在握手后自动重发。
	Allow0RTT bool

	ReadHeaderTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	MaxHeaderBytes    int

	QUIC QUICOptions

	ErrorLog *log.Logger
}

// QUICOptions 传输层参数，零值即 quic-go 默认值。
type QUICOptions struct {
	MaxIdleTimeout        time.Duration
	KeepAlivePeriod       time.Duration
	MaxIncomingStreams    int64
	MaxIncomingUniStreams int64
	// HandshakeIdleTimeout 握手阶段的空闲超时（quic-go 默认 5s）。
	HandshakeIdleTimeout time.Duration
}

// Server 同时持有 HTTPS 与 HTTP/3 两个服务。
type Server struct {
	opts Options

	https *http.Server
	h3    *http3.Server

	tcpLn   net.Listener
	udpConn net.PacketConn

	altSvc      string
	advertising atomic.Bool

	shutdownOnce sync.Once
}

// New 只组装，不绑端口；端口在 Listen 里同步绑定，绑不上能在启动阶段直接报错。
func New(opts Options) (*Server, error) {
	if opts.TLS == nil {
		return nil, errors.New("httpserve: 缺少 TLS 配置")
	}
	if opts.Handler == nil {
		return nil, errors.New("httpserve: 缺少 Handler")
	}
	s := &Server{opts: opts}

	if opts.HTTP3Addr != "" {
		s.h3 = &http3.Server{
			Addr:           opts.HTTP3Addr,
			TLSConfig:      opts.TLS,
			Handler:        s.wrap(rejectUnsafeEarlyData(opts.Handler), false),
			MaxHeaderBytes: opts.MaxHeaderBytes,
			IdleTimeout:    opts.IdleTimeout,
			QUICConfig: &quic.Config{
				Allow0RTT:             opts.Allow0RTT,
				MaxIdleTimeout:        opts.QUIC.MaxIdleTimeout,
				KeepAlivePeriod:       opts.QUIC.KeepAlivePeriod,
				MaxIncomingStreams:    opts.QUIC.MaxIncomingStreams,
				MaxIncomingUniStreams: opts.QUIC.MaxIncomingUniStreams,
				HandshakeIdleTimeout:  opts.QUIC.HandshakeIdleTimeout,
			},
			ConnContext: func(ctx context.Context, c *quic.Conn) context.Context {
				return context.WithValue(ctx, quicConnKey{}, c)
			},
		}
	}

	s.https = &http.Server{
		Addr:              opts.Addr,
		Handler:           s.wrap(opts.Handler, true),
		TLSConfig:         opts.TLS,
		ReadHeaderTimeout: opts.ReadHeaderTimeout,
		ReadTimeout:       opts.ReadTimeout,
		WriteTimeout:      opts.WriteTimeout,
		IdleTimeout:       opts.IdleTimeout,
		MaxHeaderBytes:    opts.MaxHeaderBytes,
		ErrorLog:          opts.ErrorLog,
	}
	return s, nil
}

// HTTP3Enabled 是否开了 HTTP/3。
func (s *Server) HTTP3Enabled() bool { return s.h3 != nil }

// AltSvc 宣告的 Alt-Svc 值（Listen 之后才有；未开 HTTP/3 时为空）。
func (s *Server) AltSvc() string { return s.altSvc }

// Listen 同步绑定 TCP 与 UDP 端口。
func (s *Server) Listen() error {
	ln, err := net.Listen("tcp", s.opts.Addr)
	if err != nil {
		return fmt.Errorf("httpserve: 监听 HTTPS %s: %w", s.opts.Addr, err)
	}
	s.tcpLn = ln
	if s.h3 != nil {
		conn, err := net.ListenPacket("udp", s.opts.HTTP3Addr)
		if err != nil {
			_ = ln.Close()
			return fmt.Errorf("httpserve: 监听 HTTP/3 %s/udp: %w", s.opts.HTTP3Addr, err)
		}
		s.udpConn = conn
		port := s.opts.AltSvcPort
		if port == 0 {
			port = conn.LocalAddr().(*net.UDPAddr).Port
		}
		maxAge := s.opts.AltSvcMaxAge
		if maxAge <= 0 {
			maxAge = 24 * time.Hour
		}
		s.altSvc = fmt.Sprintf(`h3=":%d"; ma=%d`, port, int64(maxAge/time.Second))
	}
	return nil
}

// Addrs 实际绑定的 TCP / UDP 地址（Listen 之后才有；端口写 0 时用来拿系统分配的端口）。
func (s *Server) Addrs() (tcp, udp net.Addr) {
	if s.tcpLn != nil {
		tcp = s.tcpLn.Addr()
	}
	if s.udpConn != nil {
		udp = s.udpConn.LocalAddr()
	}
	return tcp, udp
}

// Serve 在后台跑起两个服务；任一服务异常退出时调用 onError（正常关闭不会回调）。
// 调用前必须先 Listen。
func (s *Server) Serve(onError func(error)) {
	if s.tcpLn == nil {
		onError(errors.New("httpserve: Serve 之前没有 Listen"))
		return
	}
	go func() {
		if err := s.https.ServeTLS(s.tcpLn, "", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			onError(fmt.Errorf("https serve failed: %w", err))
		}
	}()
	if s.h3 != nil {
		s.advertising.Store(true)
		go func() {
			err := s.h3.Serve(s.udpConn)
			// HTTP/3 挂了就别再宣告，免得客户端一直去试一个不存在的 QUIC 端点
			s.advertising.Store(false)
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				onError(fmt.Errorf("http3 serve failed: %w", err))
			}
		}()
	}
}

// Shutdown 优雅关闭：先停止宣告 HTTP/3，HTTP/3 发 GOAWAY 等存量请求结束，HTTPS 同时排空。
func (s *Server) Shutdown(ctx context.Context) error {
	var errs []error
	s.shutdownOnce.Do(func() {
		s.advertising.Store(false)
		var wg sync.WaitGroup
		var mu sync.Mutex
		collect := func(err error) {
			if err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, context.Canceled) {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}
		if s.h3 != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				collect(s.h3.Shutdown(ctx))
				// http3.Server.Serve 不负责关传进来的 PacketConn
				if s.udpConn != nil {
					collect(s.udpConn.Close())
				}
			}()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			collect(s.https.Shutdown(ctx))
			// Serve 从没跑起来时 Shutdown 不会关监听，这里兜底；已关过的再关一次只会报错，忽略
			if s.tcpLn != nil {
				_ = s.tcpLn.Close()
			}
		}()
		wg.Wait()
	})
	return errors.Join(errs...)
}

// wrap 给响应统一加安全相关的头。tcp=true 时额外宣告 HTTP/3。
func (s *Server) wrap(next http.Handler, tcp bool) http.Handler {
	hsts := s.opts.HSTS
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		if tcp && s.altSvc != "" && s.advertising.Load() {
			h.Set("Alt-Svc", s.altSvc)
		}
		if hsts != "" {
			h.Set("Strict-Transport-Security", hsts)
		}
		next.ServeHTTP(w, r)
	})
}

type quicConnKey struct{}

// rejectUnsafeEarlyData 按 RFC 8470：握手完成前（0-RTT 早期数据里）到达的非幂等请求回 425，
// 早期数据能被中间人原样重放，下单、扣积分这类请求不能在那时执行。
func rejectUnsafeEarlyData(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isSafeMethod(r.Method) {
			if conn, ok := r.Context().Value(quicConnKey{}).(*quic.Conn); ok && inEarlyData(conn) {
				w.WriteHeader(http.StatusTooEarly)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func inEarlyData(conn *quic.Conn) bool {
	if !conn.ConnectionState().Used0RTT {
		return false
	}
	select {
	case <-conn.HandshakeComplete():
		return false
	default:
		return true
	}
}

func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	}
	return false
}
