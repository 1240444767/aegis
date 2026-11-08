# HTTPS 与 HTTP/3（QUIC）

> 面包屑：[Aegis](../CLAUDE.md) › docs/http3

Aegis 可以自己终止 TLS：同一个端口号上，TCP 跑 HTTPS（HTTP/1.1 + HTTP/2），UDP 跑 HTTP/3（QUIC）。
实现在 `pkg/httpserve`，配置在 `internal/config/tls_server.go`，启动接线在 `internal/bootstrap/https.go`。

## 先判断要不要开

| 部署拓扑 | 怎么得到 HTTP/3 |
|---|---|
| Cloudflare 等 CDN 在前面（线上 `aegis.karpov.cn` 就是） | 在 CDN 面板打开 HTTP/3。客户端到边缘走 QUIC，回源照旧是明文 `HTTP_PORT`，**不用开 TLS_ENABLED** |
| Nginx / Caddy / PaaS 入口终止 TLS | 让入口开 HTTP/3（Nginx 1.25+ `listen 443 quic`，Caddy 默认开），Aegis 不动 |
| 公网流量直接打到 Aegis 进程（裸机、云主机直连、内网自建 CA） | 开 `TLS_ENABLED`，本文其余部分说的就是这种 |

明文 `HTTP_PORT` 在任何情况下都开着：回源、容器健康检查、内网调用、Hydra 令牌钩子都走它。

## 最小配置

```dotenv
TLS_CERT_FILE=/etc/aegis/tls/fullchain.pem
TLS_KEY_FILE=/etc/aegis/tls/privkey.pem
HTTPS_PORT=443
```

配了证书就自动开 HTTPS 与 HTTP/3（`TLS_ENABLED` 默认跟随证书是否配置，`HTTP3_ENABLED` 默认 true）。
证书文件被 certbot / acme.sh 续期替换后，30 秒内自动换上，不用重启；新文件读坏了继续用旧证书并打一条 Warn。

不想自己管证书就用 ACME（Let's Encrypt），与证书文件二选一：

```dotenv
TLS_AUTOCERT_DOMAINS=api.example.com
TLS_AUTOCERT_EMAIL=ops@example.com
# 容器里要挂卷，否则每次重启都重签，很快撞上签发频率限制
TLS_AUTOCERT_CACHE_DIR=.runtime/autocert
```

ACME 质询有两条路：HTTPS 端口对外就是 443 时走 TLS-ALPN-01；否则走明文端口上的 HTTP-01，
这要求公网 80 端口转到 `HTTP_PORT`。联调时把 `TLS_AUTOCERT_DIRECTORY_URL` 设成 Let's Encrypt staging。

全部变量见 `.env.example` 的「HTTPS 与 HTTP/3」一节，含义与默认值以 `tls_server.go` 顶部注释为准。

## 客户端怎么升级到 HTTP/3

客户端第一次总是走 TCP。HTTPS 响应里带着

```
Alt-Svc: h3=":443"; ma=86400
```

浏览器、Cronet、curl 这类支持 HTTP/3 的客户端看到它，会在后台试 QUIC（OkHttp 不支持 QUIC，始终停在 HTTP/2），试通了下一个请求起就走 HTTP/3；
UDP 被挡住时它们自动留在 TCP 上。所以 **TCP 监听永远不能省**，HTTP/3 只是加速通道。

Alt-Svc 宣告的是**对外**端口。容器把宿主机 443 映射到容器 8443 时必须告诉服务端：

```dotenv
HTTPS_PUBLIC_PORT=443        # 明文跳转与 Alt-Svc 都用它
# HTTP3_PUBLIC_PORT 默认跟随 HTTPS_PUBLIC_PORT（HTTP/3 与 HTTPS 同端口号时）
```

宣告了一个连不上的端口不会出错，只会让客户端白试一次再退回 TCP —— 表现是「开了 HTTP/3 但访问日志里一条 HTTP/3.0 都没有」。

## 安全取舍

- **0-RTT 默认关闭**（`HTTP3_ALLOW_0RTT=false`）。早期数据能被原样重放；开启后，握手完成前到达的
  非幂等请求（POST / PUT / PATCH / DELETE）一律回 `425 Too Early`（RFC 8470），客户端会在握手后自动重发。
  GET 也照样会被重放，所以有副作用的 GET 接口不要开 0-RTT。
- **HSTS** 默认不发（`HSTS_MAX_AGE=0`）。它一旦被浏览器记住就收不回来，确认整个域名长期只走 HTTPS 再开。
- **明文跳转** `HTTPS_REDIRECT_HTTP=true` 时 308 到 HTTPS（保留方法与请求体），`/healthz`、`/readyz` 例外。
  站在 CDN 后面、回源是明文时**不要开**，否则回源请求会被跳成死循环。
- TLS 最低版本默认 1.2，`TLS_MIN_VERSION=1.3` 可收紧；HTTP/3 本身总是 TLS 1.3。

## 已知边界

- **WebSocket 不走 HTTP/3**（quic-go 不支持 RFC 9220 的扩展 CONNECT）。实时在线等 WebSocket 接口由客户端单独开 TCP 连接，
  这是所有 HTTP/3 服务端的常态，客户端无需特殊处理。
- **ReadTimeout 只约束 TCP**。QUIC 没有逐请求的读超时，空闲连接由 `HTTP3_MAX_IDLE_TIMEOUT`（默认 30s）回收；
  上传接口的读截止时间（`middleware/upload_deadline.go`）在 HTTP/3 上同样生效。
- **封禁模式里需要劫持连接的那几种**（tarpit 等）在 HTTP/2 与 HTTP/3 上都会退回静默 403，与原先 HTTP/2 的行为一致。
- **UDP 缓冲区**：quic-go 启动时若打印 `failed to sufficiently increase receive buffer size`，
  在宿主机上执行 `sysctl -w net.core.rmem_max=7500000 net.core.wmem_max=7500000`（容器内改不了这两个参数）。
  不改也能跑，只是高吞吐时丢包多一些。
- **配置热更新**：TLS / HTTP/3 相关变量改了要重启进程（热更新会把它们列进 `restartRequired`）；证书文件本身是热加载的。

## Docker

`deploy/docker/docker-compose.yml` 的 `server` 已经映射了同一端口号的 TCP 与 UDP：

```yaml
ports:
  - "${HTTPS_PUBLIC_PORT:-8443}:8443/tcp"
  - "${HTTPS_PUBLIC_PORT:-8443}:8443/udp"   # 漏掉这一行：HTTPS 正常，但永远升级不到 HTTP/3
```

容器内固定监听 8443；证书放进 `TLS_CERT_DIR`（默认 `.runtime/tls`），`.env` 里写容器内路径
`/etc/aegis/tls/...`；ACME 缓存在 `aegis_autocert` 卷里。Zeabur 等 PaaS 一般只转发 HTTP，用它们自己的入口证书即可，不必开。

## 验证

```bash
curl -sI https://api.example.com/healthz | grep -i alt-svc     # 应看到 h3=":443"
curl --http3-only -sI https://api.example.com/healthz          # curl 8.x 编译了 HTTP/3 时
```

访问日志的 `proto` 字段是 `HTTP/3.0` 即说明客户端已经升级。自测用例：
`go test ./pkg/httpserve/ ./internal/bootstrap/ -run 'HTTP3|HTTPS|CertReloader|Redirect'`，
其中 `TestAPIAppServesGinOverHTTP3` 用真实 gin 路由跑了一遍 HTTP/2、HTTP/3 的 POST 与流式 Flush。
