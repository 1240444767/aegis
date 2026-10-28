# aegis-console 构建（Railpack）

`railpack-plan.json` 由 Railpack 生成，部署时用 Railpack 官方 BuildKit 前端执行：

```
docker build -f railpack-plan.json \
  --build-arg BUILDKIT_SYNTAX=ghcr.io/railwayapp/railpack-frontend:v0.43.0 \
  --secret type=env,id=BUN_CONFIG_REGISTRY ... .
```

走宿主机常驻的 BuildKit，计划里的缓存挂载（bun 安装缓存、`.next/cache`、`node_modules/.cache`、apt）
在两次构建之间保留，未变的层直接复用。Dokploy 的 Railpack 模式每次新建并删除临时 builder，缓存全丢，所以不用它。

依赖或构建方式变化（新增构建期环境变量、换包管理器等）时重新生成：

```
cd aegis-console
railpack prepare . --plan-out railpack-plan.json \
  --env BUN_CONFIG_REGISTRY --env NPM_CONFIG_REGISTRY \
  --env NEXT_PUBLIC_AEGIS_ENVIRONMENT --env NEXT_PUBLIC_AEGIS_CONSOLE_NAME
```

升级 Railpack 时同时改 Dokploy 里的 `BUILDKIT_SYNTAX` 版本号。
