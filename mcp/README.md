# aegis-mcp

Aegis 管理端的 MCP 服务（Python）。AI 助手（Claude Desktop、Claude Code、Cursor 等）凭 **MCP 密钥**
以签发它的管理员身份查询与管理 Aegis：应用、用户、会员、积分、工单、内容、审计……

后端的密钥模型与判定规则见 [docs/mcp.md](../docs/mcp.md)。

## 1. 签发密钥

控制台 → 个人资料 → **MCP 密钥** → 签发密钥。默认只读、全部查看权限、90 天有效。
明文只显示一次，签发弹窗里同时给出照抄即可用的客户端配置。

密钥能做的 = 你此刻的权限 ∩ 密钥范围（权限点、应用、只读、来源 IP、每分钟次数）。

## 2. 接入客户端

需要 [uv](https://docs.astral.sh/uv/)。

**JSON 配置**（Claude Desktop、Cursor、Cherry Studio 等）：

```json
{
  "mcpServers": {
    "aegis": {
      "command": "uvx",
      "args": ["--from", "git+https://github.com/MiChongs/aegis#subdirectory=mcp", "aegis-mcp"],
      "env": {
        "AEGIS_BASE_URL": "https://aegis.example.com",
        "AEGIS_MCP_KEY": "aegis_mcp_..."
      }
    }
  }
}
```

仓库是私有的、或想用本地代码时，把 `--from` 换成本地路径：`"args": ["--from", "/path/to/aegis/mcp", "aegis-mcp"]`。

**Claude Code**：

```bash
claude mcp add aegis -e AEGIS_BASE_URL=https://aegis.example.com -e AEGIS_MCP_KEY=aegis_mcp_... -- uvx --from "git+https://github.com/MiChongs/aegis#subdirectory=mcp" aegis-mcp
```

`AEGIS_BASE_URL` 填后端地址或控制台地址都可以（控制台会把 `/api` 与 `/openapi.json` 反代到后端）。

## 3. HTTP 模式（团队共用一个服务）

```bash
AEGIS_BASE_URL=https://aegis.example.com aegis-mcp --transport streamable-http --host 0.0.0.0 --port 8765
```

每个客户端在请求头里带自己的密钥：`Authorization: Bearer aegis_mcp_...`（也认 `X-Aegis-MCP-Key`）。
设置了 `AEGIS_MCP_KEY` 时它是没带请求头的客户端的缺省密钥，对外暴露时不要设。
服务本身不做 TLS，放在反向代理后面。

## 环境变量

| 变量 | 说明 |
|---|---|
| `AEGIS_BASE_URL` | 必填，Aegis 根地址 |
| `AEGIS_MCP_KEY` | stdio 模式必填，`aegis_mcp_` 开头 |
| `AEGIS_TIMEOUT` | 单次请求超时秒数，默认 30 |
| `AEGIS_MAX_RESULT_CHARS` | 单个工具结果的最大字符数，超出截断，默认 60000 |
| `AEGIS_VERIFY_TLS` | 设为 `0` 跳过证书校验（仅自签名测试环境） |

## 工具

**通用层**，覆盖管理端全部约 970 条接口：

| 工具 | 作用 |
|---|---|
| `aegis_whoami` | 密钥代表谁、范围、是否只读、生效权限、可访问的应用 |
| `aegis_list_endpoints` | 按关键词 / 分组 / 方法查接口，逐条标出当前密钥能不能调、为什么 |
| `aegis_describe_endpoint` | 一条接口的路径参数、查询参数、请求体字段（来自 `/openapi.json`） |
| `aegis_call_api` | 调用任意一条管理端接口 |

**常用层**，带类型参数，模型不必先查目录：

| 分类 | 工具 |
|---|---|
| 应用 | `aegis_list_apps`、`aegis_get_app`、`aegis_app_stats`、`aegis_dashboard` |
| 用户 | `aegis_search_users`、`aegis_get_user`、`aegis_user_login_audits`、`aegis_ban_user`、`aegis_unban_user`、`aegis_revoke_user_sessions` |
| 积分与会员 | `aegis_user_points_ledger`、`aegis_adjust_points`、`aegis_vip_grant`（支持预览）、`aegis_vip_revoke` |
| 内容与通知 | `aegis_list_notices`、`aegis_create_notice`、`aegis_notify_users` |
| 工单 | `aegis_list_tickets`、`aegis_get_ticket`、`aegis_reply_ticket`、`aegis_set_ticket_status` |
| 审计与运维 | `aegis_audit_logs`、`aegis_crash_reports` |

写工具带 MCP 的 `destructive` / `read_only` 提示，客户端会据此要求确认。所有调用记入 Aegis 审计日志，
会话列为 `mcp:<密钥前缀>`，可用 `aegis_audit_logs(session_id=...)` 回查。

## 开发

```bash
uv venv && uv pip install -e ".[dev,http]"
.venv/bin/python -m pytest
```

测试用 `httpx.MockTransport` 模拟后端。对真实后端的端到端验证见 [docs/mcp.md](../docs/mcp.md#测法)。
