package httpserve

import (
	"net"
	"net/http"
	"strconv"
	"strings"
)

// RedirectToHTTPS 把明文请求 308 到 HTTPS（保留方法与请求体），exempt 命中的路径照常交给 next ——
// 容器健康检查、内网探针走的就是明文端口，跳走了它们就会判服务挂掉。
//
// httpsPort 是对外的 HTTPS 端口；443 时跳转地址里不带端口。
func RedirectToHTTPS(httpsPort int, exempt func(path string) bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if exempt != nil && exempt(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		host := r.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		if host == "" {
			next.ServeHTTP(w, r)
			return
		}
		if strings.Contains(host, ":") {
			host = "[" + host + "]"
		}
		if httpsPort != 0 && httpsPort != 443 {
			host += ":" + strconv.Itoa(httpsPort)
		}
		http.Redirect(w, r, "https://"+host+r.URL.RequestURI(), http.StatusPermanentRedirect)
	})
}
