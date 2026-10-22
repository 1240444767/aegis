# 登录设备数控制

> 面包屑：[Aegis](../CLAUDE.md) › docs/device-limit
> 代码：`internal/domain/devicelimit`（策略与判定，纯函数）· `internal/service/device_limit_service.go` ·
> `internal/transport/http/device_limit_handlers.go` · 控制台 `/device-limit`

一个账号同时能在几台设备上保持登录，超出时怎么处置。多个维度叠加判定：

| 维度 | 配置 | 说明 |
|---|---|---|
| 总数 | `maxDevices` | 0 表示不限总数 |
| 终端类别 | `classLimits.{mobile,desktop,web,other}` | 手机（Android / iOS / HarmonyOS）、电脑（Windows / macOS / Linux）、网页、未知平台各自的上限 |
| 会员档位 | `tiers[]` | 试用会员 / 会员 / 永久会员 / 持有某功能，按顺序第一条命中的生效；没配类别上限时沿用默认 |
| 单个用户 | 例外名单 | 豁免（不受任何限制），或另配上限；可设到期时间。优先于档位 |
| 新设备频率 | `newDevice.{windowDays,maxNew}` | 滚动窗口内首次出现的设备数，超出拒绝登录（防账号共享 / 被盗后到处登录） |
| 同设备 | `sameDevice` | `replace` 同一台设备再登录顶掉旧登录；`coexist` 保留，仍只算一台 |

生效上限的优先级：**单人例外 > 会员档位 > 默认**。策略存在 `apps.settings.deviceLimit`。
没有保存过新策略的应用按旧字段推导（`multiDeviceLogin=false` → 上限 1；`multiDeviceLoginNum>0` → 上限该值），
保存新策略时旧字段同步写回。

## 「设备」怎么算

设备不是会话。按刷新令牌族（一次登录）聚合，再按设备 Key 合并：

1. 客户端上报了设备标识（`X-Device-Id` 或请求体 `deviceId`）→ 用它
2. 否则用 UA 摘要（`ua:` + sha256 前 8 字节）→ 同一浏览器算一台
3. 连 UA 都没有 → 这次登录单独算一台（`s:` + 令牌族），不进台账，也不受新设备频率限制

访问令牌寿命短，过期了设备仍算在线；以仍可用的刷新令牌为准。
「最早登录」取该设备上仍有效的最早一次签发，「最近活动」取最近一次签发或刷新。

## 超限处置

| `overflow` | 行为 |
|---|---|
| `kick_oldest` | 自动下线最早登录的设备（默认） |
| `kick_idle` | 自动下线最久没有活动的设备 |
| `reject` | 拒绝这次登录（40997），已登录的不受影响；`rejectMessage` 可自定义，`{limit}` 占位 |
| `ask` | 不签发令牌，返回设备列表让用户选择下线哪台；客户端没声明支持时按 `askFallback` |

自动下线时先满足类别（在同类别里挑），再满足总数（在剩下的里挑）：只超了手机数时不会去踢电脑。
被下线的设备：访问令牌与刷新令牌都拉黑并记下原因，下次请求报 **40130**；
`notifyKicked` 开启时给用户发一条「账号安全」站内信；`/api/ws` 推 `session.revoked`
（`data.families` 是被撤销的令牌族，客户端与自己的比对，命中即提示并回到登录页）。

### 让用户选择（ask）

客户端在请求头带 `X-Device-Limit-Challenge: 1` 声明支持（Kotlin SDK：`Builder.deviceLimitChoice()`）。
登录结果：

```json
{
  "authenticationState": "device_limit_required",
  "deviceLimit": {
    "ticket": "…", "expiresAt": "…", "dimension": "total", "mustRemove": 1,
    "message": "同时登录的设备已达上限（2 台），请选择至少 1 台设备下线",
    "requirements": [{ "dimension": "total", "limit": 2, "current": 3, "mustRemove": 1 }],
    "devices": [{ "key": "…", "class": "mobile", "deviceInfo": { "name": "…" }, "loginAt": "…", "lastActiveAt": "…", "eligible": true }]
  }
}
```

没有令牌。用户勾好后 `POST /auth/device-limit/resolve {ticket, deviceKeys}`，成功返回与登录相同的结果。
凭证 5 分钟有效，用过即作废（40496）；勾得不够不再追问，直接 40997。
`eligible=false` 的设备勾了也不解决问题（例如只超了手机数时的电脑）。
登录现场（方式、设备、IP）取自凭证，不取自 resolve 请求；账号与应用状态在 resolve 时重新判定。

## 执行点

只有一个：`AuthService.issueSession`，全部新登录（密码、短信、卡密、第三方、Passkey、二次验证、扫码）
经 `finalizeLogin` 走到这里。

- `Admit`：同一用户加 Redis 锁（10 秒，最多等 3 秒，拿不到就不加锁继续）→ 取在线设备、会员身份、例外、台账 → `devicelimit.Plan` → 拒绝 / 选择页 / 放行
- 签发
- `Commit`：下线被顶掉与被选中的设备、写台账、记处置、提醒 → 解锁

刷新令牌延续的是已有设备，不经过关卡，只异步更新台账的最后出现时间。
OAuth2 授权服务器（Hydra）签发的令牌不是 Aegis 会话，不在此列。
基础设施出错（Redis、数据库、会员判定）一律放行并记 warn。

## 错误码

| 码 | HTTP | 含义 | 客户端 |
|---|---|---|---|
| 40130 | 401 | 账号已在其他设备登录，本设备被挤下线 | 不要刷新，清登录态，提示后回登录页 |
| 40131 | 401 | 本设备在设备管理里被移除（用户自己或管理员） | 同上 |
| 40997 | 409 | 设备数已达上限，拒绝登录 | 提示先在已登录的设备上退出 |
| 40998 | 409 | 窗口内新设备过多，拒绝登录 | 引导在常用设备上登录 |
| 40496 | 404 | 选择页凭证无效或已过期 | 回登录页重新登录 |

鉴权中间件把其它令牌失效都报成 40100（客户端据此刷新），只有 40130 / 40131 原样透出。

## 接口

网关（`/api/v1/apps/{appKey}`）：

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/auth/device-limit/resolve` | 选择页完成登录 |
| GET | `/me/devices` | 我的登录设备（本机在前）、生效上限、各类别占用 |
| POST | `/me/devices/remove` | 下线我的其他设备 `{deviceKeys}`，不能下线本机 |

管理端（`/api/admin/apps/:appkey/device-limit`，读 `app:user:read`、改策略 `app:write`、其余写 `app:user:write`）：

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `` | 概览：`days`、`tz`（浏览器时区，按它划分自然日） |
| GET / PUT | `/policy` | 策略 |
| POST | `/simulate` | 模拟一次登录：草稿策略 + 真实用户或虚构的会员身份与设备 |
| GET | `/events` | 处置记录：`action`、`dimension`、`userId`、`keyword`、`from`、`to` |
| GET | `/overrides` | 例外名单 |
| PUT / DELETE | `/overrides/:userId` | 设置 / 删除例外 |
| GET | `/users/:userId` | 用户的生效上限、在线设备、台账、例外、最近处置 |
| POST | `/users/:userId/remove` | 下线指定设备 `{deviceKeys}`（40131） |
| POST | `/users/:userId/enforce` | 按当前策略收回超出的设备（调低上限后立即生效） |
| DELETE | `/users/:userId/ledger` | 清空台账（`key` 只删一台），新设备频率从零算 |

## 数据

- `user_device_limit_overrides`：单人例外，主键 (appid, user_id)
- `user_login_devices`：设备台账，主键 (appid, user_id, device_key)，登录时写、刷新时更新最后出现
- `device_limit_events`：处置记录（kicked / rejected / challenged / resolved / new_device / user_removed / admin_removed / admin_enforced），异步写

## 测试

- 判定：`go test ./internal/domain/devicelimit/`
- 端到端（真实 Postgres + miniredis）：
  `AEGIS_TEST_PG_DSN=postgres://…/空库 go test ./internal/service -run TestDeviceLimitIntegration`
  （测试会清空 schema，必须指向专用的空库）
