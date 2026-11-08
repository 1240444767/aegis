# aegis-console 构建（Railpack）

`railpack-plan.json` 由 Railpack 生成，部署时用 Railpack 官方 BuildKit 前端执行：

```
docker build -f railpack-plan.json \
  --build-arg BUILDKIT_SYNTAX=ghcr.io/railwayapp/railpack-frontend:v0.43.0 \
  --secret type=env,id=BUN_CONFIG_REGISTRY ... .
```

走宿主机常驻的 BuildKit，计划里的缓存挂载（bun 安装缓存、`.next/cache`、`node_modules/.cache`、apt）
在两次构建之间保留，未变的层直接复用。Dokploy 的 Railpack 模式每次新建并删除临时 builder，缓存全丢，所以不用它。

`AEGIS_API_BACKEND` 在 next build 时被写进 routes-manifest.json 的 rewrites，必须作为构建密钥传入，否则 /api/* 会反代到默认的 127.0.0.1:8088（500）。

依赖或构建方式变化（新增构建期环境变量、换包管理器等）时重新生成：

```
cd aegis-console
railpack prepare . --plan-out railpack-plan.json \
  --env NEXT_PUBLIC_AEGIS_ENVIRONMENT --env NEXT_PUBLIC_AEGIS_CONSOLE_NAME --env AEGIS_API_BACKEND
```

升级 Railpack 时同时改 Dokploy 里的 `BUILDKIT_SYNTAX` 版本号。

## 出站代理

部署服务器的 IPv4 到部分 CDN 网段（Cloudflare 104.16.0.0/16、阿里 CDN、华为云等）不稳定，IPv6 正常。
install / build 步骤的 `variables` 里设了 `HTTPS_PROXY=http://172.31.0.1:8888`：宿主机 docker0 上的 tinyproxy，
经宿主机公网 IPv6 出站。重新生成计划后要把这三项（HTTPS_PROXY / HTTP_PROXY / NO_PROXY）加回去。
