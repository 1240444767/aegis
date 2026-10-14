# 实时在线统计（管理端加密长连接）

控制台「用户 › 在线会话」通过一条 WebSocket 实时接收在线快照、在线曲线与上下线动态，
取代旧版每 10 秒轮询三个统计接口的做法。旧的 REST 接口（`/api/admin/system/online/*`）
保留，供监控与脚本使用，读路径已改为按分数计数，不再每次全量清理。

- 端点：`GET /api/admin/system/online/live`（WebSocket）
- 权限：`system:admin:manage`（与旧的在线统计接口相同）
- 鉴权：与 `/api/ws` 相同，令牌放在子协议 `aegis.jwt.<accessToken>`，服务端只回显 `aegis`

## 谁能看到什么

| 内容 | 超级管理员 | 全局角色管理员 | 应用级管理员 |
|---|---|---|---|
| 全站总量、应用排行、分布 | 全部 | 全部 | 仅被分配的应用（总量为这些应用之和） |
| 单个应用的在线用户（含 IP、设备、连接明细） | 全部 | 全部 | 仅被分配的应用 |
| 控制台在线的管理员、管理员曲线 | 有 | 无 | 无 |
| 接入节点分布 | 有 | 无 | 无 |

服务端每 30 秒复核一次令牌与权限：会话被撤销、账号停用或权限收回时立即断开。

## 握手与加密

TLS 往往在反代、CDN 或 Next.js 同源代理那一跳终结，之后是明文。在线统计含用户 IP、
设备与地区，因此在 WSS 之上再加一层应用层加密，明文只出现在浏览器与 Aegis 进程里。

1. 服务端发明文文本帧：

   ```json
   {"t":"hello","v":1,"suite":"ECDH-P256/HKDF-SHA256/AES-256-GCM",
    "sid":"<uuid>","key":"<base64，服务端临时公钥，65 字节未压缩点>","salt":"<base64，32 字节>"}
   ```

2. 客户端生成 P-256 临时密钥对，回一个明文文本帧（10 秒内）：

   ```json
   {"t":"key","key":"<base64，客户端临时公钥，65 字节未压缩点>"}
   ```

3. 双方派生：
   - `shared = ECDH(P-256)`（32 字节）
   - `okm = HKDF-SHA256(ikm=shared, salt, info, 64)`，
     `info = "aegis/online-live/v1|" + sid + "|" + hex(SHA-256(访问令牌))`
   - `okm[0:32]` 为客户端→服务端密钥，`okm[32:64]` 为服务端→客户端密钥

   info 绑定令牌摘要：没有这名管理员令牌的中间人即使替换了双方公钥，也派生不出同一把密钥。

4. 之后只允许二进制帧，收到明文帧即以 4400 断开：

   | 偏移 | 长度 | 内容 |
   |---|---|---|
   | 0 | 1 | 版本，固定 1 |
   | 1 | 1 | 标志，保留，固定 0 |
   | 2 | 8 | 序号，大端 uint64，每个方向从 1 开始逐帧加一 |
   | 10 | n+16 | AES-256-GCM 密文与 16 字节标签 |

   `nonce = 方向前缀(4) + 序号(8)`，服务端发送用 `"S2C\0"`，客户端发送用 `"C2S\0"`；
   `AAD = 帧头 10 字节`。接收方要求序号恰好是上一帧加一，重放、乱序、丢帧一律断开。

临时密钥只存在于连接内存中（前向保密）。连接满 1 小时服务端以 4408 关闭，客户端立即重连换钥。

## 关闭码

| 码 | 含义 | 客户端处理 |
|---|---|---|
| 4400 | 握手失败或帧无法解密 | 停止重连，提示错误 |
| 4401 | 令牌失效、会话被撤销 | 停止重连，回登录 |
| 4403 | 权限被收回 | 停止重连，提示无权限 |
| 4408 | 到达最长寿命 | 立即重连 |
| 1013 | 客户端处理过慢 | 退避重连 |
| 其他 | 网络断开、服务重启 | 指数退避重连 |

## 消息（解密后的 JSON）

请求可带 `id`，响应以 `re` 原样回填。

### 客户端 → 服务端

| `t` | 字段 | 说明 |
|---|---|---|
| `sub` | `app`（0 为概览）、`feed`（是否接收动态） | 设置关注范围；服务端回 `sub` 并立即按新范围推一份快照 |
| `series` | `app`、`range`：`1h` / `6h` / `24h` / `7d` | 在线曲线 |
| `users` | `app`、`page`、`limit`（≤100）、`q` | 应用在线用户；`q` 匹配账号、昵称、用户 ID、IP 前缀、设备 ID |
| `admins` | — | 控制台在线的管理员（仅超管） |
| `ping` | `ts` | 测往返时延 |

### 服务端 → 客户端

**ready**（握手后第一帧）：`admin`（id、account、displayName、isSuperAdmin）、
`scope`（`all`、`apps`）、`node`、`serverTime`、`limits`（tickMs、retentionDays、lifetimeSec）。

**snapshot**（约每 2 秒，有新版本才推）：

```json
{"t":"snapshot","v":123,"at":"…",
 "totals":{"users":0,"connections":0,"apps":0},
 "apps":[{"appid":7,"name":"…","appKey":"…","users":0,"connections":0}],
 "dims":{"platform":[{"key":"Android","count":0}],"os":[],"appVersion":[],"country":[],"region":[],"device":[]},
 "focus":{"appid":7,"users":0,"connections":0,"dims":{}},
 "nodes":[{"serverId":"…","connections":0,"admins":0}],
 "admins":{"admins":0,"connections":0},
 "truncated":false}
```

分布里 `key` 为空串表示客户端未上报，`*` 表示其余项之和。分布每 10 秒重算一次，
超过 5 万条连接时只统计最近活跃的 5 万条并置 `truncated`。

**series**：`points` 为 `[时间(秒), 在线用户, 连接数, 在线管理员]` 数组，按 `step` 秒分桶取峰值。

**feed**（约每 0.5 秒一批）：`events` 为上下线事件（kind、appid、userId、account、ip、location、
platform、deviceName、serverId、at、duration、admin）；单批超过 100 条时其余只计入 `dropped`。

**users** / **admins** / **pong** / **sub** / **error**（`code`、`message`、`req`）。

## 数据来源

- 每条连接的元数据在建连时写入 Redis，包含会话里的设备信息（型号、平台、系统、版本）
  和本地 IP 库查到的国家、省份、城市。
- 快照：有订阅者的实例每拍抢锁，抢到的实例计算并写入 `realtime:snapshot`，其余实例直接读。
- 曲线：每分钟由一个实例写一次采样（全站 + 各应用），按 UTC 日分桶存 7 天。
- 动态：上下线由连接所在实例经 NATS `realtime.presence` 广播。
- 强制下线：撤销用户会话（`POST /api/admin/apps/:appkey/users/:userId/revoke-sessions`）与
  强制管理员下线时，经 NATS `realtime.control.disconnect` 断开对方已建立的实时连接（关闭码 4001）。
