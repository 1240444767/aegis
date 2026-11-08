# 版本管控

管理自己发布的客户端：客户端启动、回到前台、收到变更通知时，把包名、版本、签名证书等身份信息**加密上报**；
服务端按应用的管控规则给出一份 **Ed25519 签名**的裁决（放行 / 提醒 / 强制更新 / 停用），客户端验签后执行。
停用与强制更新还会在服务端执行：命中的客户端调用网关其余接口一律被拒。

上报、裁决、拦截与变更通知**都不依赖登录**；带令牌时只是额外记下用户、让「用户」条件可以生效。
上报与裁决走独立的加密通道，与应用的安全等级（standard / signed / sealed）无关。

代码：领域类型 `internal/domain/app/client_control.go`，判定引擎（纯函数）`internal/service/client_control_engine.go`，
密码学构造 `internal/service/client_control_crypto.go`，网关闸门 `internal/middleware/client_control_gate.go`，
接口 `internal/transport/http/client_control_handlers.go`，迁移 `000109_client_control`。

## 概念

| 概念 | 说明 |
|---|---|
| 裁决 | 一次上报的结果，`action` 为 `allow` 放行 / `warn` 提醒（可关闭）/ `force_update` 强制更新 / `block` 停用；后两者都让客户端不可用 |
| 规则 | 条件 + 结果 + 文案。`priority` 高者先匹配，同优先级先建的先匹配，**第一条命中的规则决定结果**；`allow` 规则用作例外（如给测试设备放行） |
| 条件 | 包名（可取反）、版本号区间（闭区间，0 不限）、版本名、签名证书（可取反）、平台、设备、用户、安装来源（可取反）、系统版本区间。条件之间为「且」，同一条件内多个取值为「或」，留空不参与判定；没有任何条件的规则命中所有客户端 |
| 生效时间 | 规则可设 `startsAt` / `endsAt`，窗口外视同停用 |
| 官方身份 | 设置里的官方包名、官方签名证书。没有规则命中时检查：包名不在官方清单 → 停用（`package_not_allowed`）；证书不在官方清单 → 停用（`signature_mismatch`）。清单留空表示不检查 |
| 策略版本 | `policyVersion`，设置或规则任何变更都加一；长轮询据此通知客户端重新校验 |
| 管控密钥 | `sign`：Ed25519，签裁决；`box`：X25519，客户端用它加密上报。私钥以主密钥加密保存，只在服务端使用 |

### 判定次序

1. 管控未启用 → `allow`，`reason=disabled`（仍然签名下发，客户端靠它解除此前的停用）
2. 规则逐条匹配，第一条命中的规则决定结果，`reason=rule`、`ruleId` 为该规则
3. 官方包名 → 官方签名检查（见上）
4. 默认 `allow`，`reason=default`

网关闸门只看得到请求头里的字段（见「服务端执行」）。规则用到闸门看不到的条件（安装来源、用户、系统版本等）、
而看得到的条件全部满足时，闸门判为 `allow`、`reason=undetermined`，不拦截，交给 `/client-control/check` 判定；
请求没带证书时闸门跳过官方签名检查。

## 客户端接入

### 1. 读 `/config`

`data.clientControl`：

```jsonc
{
  "protocol": "aegis-client-control-v1",
  "enabled": true,
  "policyVersion": 12,
  "recheckSeconds": 600,
  "watchEnabled": true,
  "signingKeyId": "ccs_…",                       // 当前签名密钥
  "signingKeys": [                               // 全部签名密钥，按创建先后排列
    { "keyId": "ccs_a", "publicKey": "<32 字节 base64url>", "status": "retired" },
    { "keyId": "ccs_b", "publicKey": "…", "status": "active",
      "endorsedBy": "ccs_a", "endorsement": "<64 字节 base64url>" }
  ],
  "boxKey": { "keyId": "cck_…", "publicKey": "<32 字节 base64url>",
              "signingKeyId": "ccs_b", "signature": "<64 字节 base64url>" },
  "checkPath": "/api/v1/apps/{appKey}/client-control/check",
  "watchPath": "/api/v1/apps/{appKey}/client-control/watch"
}
```

`/config` 本身不签名，因此**客户端不能直接信任这里的公钥**：

- 客户端内置（或首次使用时固定保存）一把签名公钥，控制台「密钥」页给出当前公钥；
- 顺着背书链信任后来的密钥：`endorsement` 是 `endorsedBy` 指向的密钥对下面这串字节的签名，被信任的密钥背书的密钥同样被信任：

  ```text
  aegis-client-control-endorse-v1\n{appKey}\n{keyId}\n{publicKeyBase64Url}
  ```

- `boxKey.signature` 是 `boxKey.signingKeyId` 对下面这串字节的签名，验过才用它加密：

  ```text
  aegis-client-control-box-v1\n{appKey}\n{keyId}\n{publicKeyBase64Url}
  ```

全部编码都是**无 padding 的 base64url**，字段之间为 `\n`，末尾没有换行。

### 2. 上报：`POST /client-control/check`

免登录；带 Bearer 时记下用户。明文是 JSON：

```jsonc
{
  "packageName": "com.example.app",
  "versionName": "1.4.0",
  "versionCode": 140,
  "platform": "android",                // android / ios / harmony / windows / macos / linux
  "certSha256": "<64 位小写十六进制>",   // 签名证书 SHA-256；服务端也接受带冒号或大写的写法
  "installer": "com.android.vending",    // 安装来源，取不到留空
  "deviceId": "…",                       // 稳定设备 ID；为空时设备清单里记作 anon-<hash>，裁决的 subject.deviceId 仍是空串
  "osVersion": 34,                       // 系统主版本（Android 填 SDK_INT）
  "model": "…", "brand": "…", "abi": "arm64-v8a",
  "policyVersion": 11                    // 客户端当前持有的裁决对应的策略版本，仅用于统计
}
```

加密（每次请求都换新的临时密钥对与 nonce）：

```text
nonce    = 24 字节随机数                       → nonceB64
shared   = X25519(客户端临时私钥, boxKey.publicKey)
AAD      = "aegis-client-control-v1\n{appKey}\n{boxKeyId}\nrequest\n{timestamp}\n{nonceB64}"
salt     = SHA-256("aegis-client-control-v1:{appKey}:{boxKeyId}")
key      = HKDF-SHA256(IKM=shared, salt, info=AAD, L=32)
payload  = XChaCha20-Poly1305(key, nonce, 明文, AAD)
```

请求体：

```json
{ "keyId": "cck_…", "clientKey": "<临时公钥 base64url>", "nonce": "<nonceB64>",
  "timestamp": 1760000000, "payload": "<密文 base64url>" }
```

`timestamp` 是 Unix 秒，与服务端相差须在 ±10 分钟内，先用 `/config` 的 `serverTime` 校准。
这个请求本身仍按应用的安全等级包装（signed 档签名、sealed 档再加一层），SDK 自动处理。

### 3. 解开响应

响应 `data`：`{ "keyId", "nonce": "<responseNonceB64>", "payload": "<密文>" }`。

```text
responseKey = SHA-256(key ‖ "aegis-client-control-response-v1")
AAD         = "aegis-client-control-v1\n{appKey}\n{boxKeyId}\nresponse\n{requestNonceB64}\n{responseNonceB64}"
明文        = XChaCha20-Poly1305-Open(responseKey, responseNonce, payload, AAD)
```

明文：`{ "verdict": "<裁决 JSON 原始字节的 base64url>", "signature": "<64 字节 base64url>", "signingKeyId": "ccs_…" }`。
签名消息是 `"aegis-client-control-verdict-v1\n"` 后接裁决原始字节。**原样保存解码后的那串字节**，
不要解析后再序列化 —— 字段顺序或空白一变，签名就对不上了。

裁决：

```jsonc
{
  "v": 1,
  "appKey": "demo_app",
  "policyVersion": 12,
  "issuedAt": 1760000000,            // 服务端时间，Unix 秒
  "expiresAt": 1760086400,           // 放行裁决的离线有效期截止
  "challenge": "<本次请求的 nonceB64>",
  "subject": { "packageName": "…", "versionCode": 140, "certSha256": "…", "deviceId": "…" },
  "enabled": true,
  "action": "block",                 // allow / warn / force_update / block
  "reason": "rule",                  // disabled / rule / package_not_allowed / signature_mismatch / default
  "ruleId": 7,
  "title": "当前版本已停用",
  "message": "…",
  "actionUrl": "https://…",          // 可空；强制更新时为空表示用应用内的更新入口
  "actionLabel": "前往下载",
  "recheckSeconds": 600,
  "watchEnabled": true,
  "offlinePolicy": "allow"           // 放行裁决过期且连不上服务端时：allow 继续用 / block 停用
}
```

### 4. 客户端应当如何执行

| 规则 | 原因 |
|---|---|
| 只接受签名有效、`challenge` 等于本次请求 nonce、`appKey` 与 `subject`（包名、版本号、证书）都与自身一致的裁决 | 旧的放行裁决、发给别的包或别的版本的裁决都不能被拿来顶替 |
| 裁决原始字节与签名一起落盘；启动时先读、先验签、再执行，然后再联网校验 | 断网、重启都不会让已下发的停用失效 |
| `block` / `force_update` 一旦生效，**只有一份新的、验签通过的非停用裁决才能解除**；`expiresAt` 对它们不起作用 | 改系统时间、断网、清缓存都不能把停用变回可用 |
| 落盘的裁决与当前版本号或证书不一致时忽略它，立即联网校验 | 用户更新了应用之后，旧版本的停用不该继续生效 |
| 停用页放在界面最外层，不提供关闭；同时停掉后台播放、下载等仍在运行的任务 | 对话框或单个页面挡不住其他入口 |
| `warn` 可关闭；同一 `ruleId` + `policyVersion` 只提醒一次 | 提醒不是停用 |
| 前台时每 `recheckSeconds` 复查一次；`watchEnabled` 时另开长轮询（见下） | 规则变更后尽快生效，未登录也一样 |
| 任一网关接口返回 `40329` / `40326`（或响应头带 `X-Aegis-Client-Control`）时立即重新校验 | 服务端已经判定停用，客户端应马上展示 |

### 5. 变更通知：`GET /client-control/watch?since={policyVersion}&wait={秒}`

免登录的长轮询。`since` 是客户端已知的策略版本；`wait` 取 0–25，缺省 25。
策略版本与 `since` 不同时立即返回，否则最多挂起 `wait` 秒：

```json
{ "policyVersion": 13, "changed": true, "watchEnabled": true, "retryAfter": 0 }
```

`changed=true` 时立即调 `/client-control/check`。`watchEnabled=false` 时停止长轮询，`retryAfter` 秒后再试。
客户端的读超时要比 `wait` 长（建议 `wait + 10` 秒）。已登录的客户端还会在实时通道上收到 `client_control.changed`（`data.policyVersion`）。

### 6. 服务端执行

SDK 随每个请求附带身份请求头（与 `X-Device-*` 一样，非 ASCII 值按 UTF-8 百分号编码）：

| 请求头 | 内容 |
|---|---|
| `X-App-Package` | 包名 |
| `X-App-Version-Code` | 版本号 |
| `X-App-Version` | 版本名 |
| `X-App-Cert` | 签名证书 SHA-256 |

另读 `X-Device-Platform`、`X-Device-Id`。`X-Device-OS-Version` 是系统版本名（如 `14`），口径与规则里的 API 级别不同，闸门不读它：系统版本条件在闸门上按未知字段处理。应用启用了管控且打开「服务端执行」时，
带 `X-App-Package` 的网关请求命中停用 / 强制更新即返回 HTTP 403：

```json
{ "code": 40329, "message": "当前版本已停止服务……" }
```

强制更新为 `40326`。响应头 `X-Aegis-Client-Control: block | force_update`、`X-Aegis-Client-Control-Version: {policyVersion}`。
以下路径不拦，被停用的客户端仍要靠它们取裁决和下载更新：`/config`、`/auth/oauth/callback`、`/client-control/*`、`/releases*`。
不带 `X-App-Package` 的请求（官网服务端、旧版客户端）不经过闸门。

## 管理端接口（`/api/admin/apps/{appKey}/client-control`，权限 `version:read` / `version:write`）

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| `GET` | `` | read | 概览：设置、24 小时 / 7 天 / 30 天活跃设备、7 天停用数、包名与版本数、规则数、当前密钥、按结果分布、按天统计 |
| `GET` `PUT` | `/settings` | read / write | 设置；`PUT` 体为 `ClientControlSettingsPatch`，字段缺省表示不修改 |
| `GET` | `/keys` | read | 全部管控密钥（公钥、状态、背书） |
| `POST` | `/keys/{kind}/rotate` | write | 轮换：`sign` 新签名密钥由旧密钥背书；`box` 旧加密密钥退役后保留 7 天，过期返回 `40457`，客户端届时重拉 config |
| `GET` `POST` | `/rules` | read / write | 规则列表（按匹配顺序）/ 新建 |
| `POST` | `/rules/reorder` | write | `{ "ruleIds": [...] }` 按匹配顺序排列，第一个最先匹配 |
| `PUT` `DELETE` | `/rules/{ruleId}` | write | 整体更新 / 删除 |
| `POST` | `/rules/{ruleId}/enable`、`/rules/{ruleId}/disable` | write | 启用 / 停用 |
| `GET` | `/versions?days=30` | read | 版本分布：按包名 + 版本号 + 证书 + 平台聚合，`decision` 是按当前规则重新判定的结果 |
| `GET` | `/devices?keyword&packageName&versionCode&certSha256&action&days&page&limit` | read | 设备清单（每台设备每个包名一条，记录最近一次上报与裁决） |
| `DELETE` | `/devices/{deviceId}?packageName=` | write | 删除一条设备记录（不影响规则，设备下次上报会重新出现） |
| `POST` | `/simulate` | read | 以给定身份模拟判定，返回逐条解释 `trace` |
| `POST` | `/refresh` | write | 策略版本加一，正在长轮询的客户端立即重新校验 |

设置字段：

| 字段 | 默认 | 说明 |
|---|---|---|
| `enabled` | `false` | 管控总开关 |
| `allowedPackages` | `[]` | 官方包名 |
| `allowedCerts` | `[]` | 官方签名证书 SHA-256 |
| `verdictTtlSeconds` | `86400` | 放行裁决的离线有效期 |
| `recheckSeconds` | `600` | 前台复查间隔 |
| `offlinePolicy` | `allow` | 放行裁决过期且连不上服务端时的处理 |
| `watchEnabled` | `true` | 是否开放长轮询 |
| `serverGate` | `true` | 服务端执行 |
| `blockTitle` / `blockMessage` | 空 | 停用规则没写文案时用的默认文案 |

规则字段见 `ClientControlRule` / `ClientControlRuleInput` 的 json tag。

## 错误码

| 业务码 | 含义 | 处理 |
|---|---|---|
| `40000` | 管控请求或载荷格式无效、载荷认证失败 | 核对 AAD、HKDF 盐与 nonce |
| `40071` | 时间戳超出 ±10 分钟 | 用 `/config` 的 `serverTime` 校准 |
| `40970` | nonce 已使用 | 每次请求换新 nonce |
| `40457` | 管控加密密钥不存在或已失效 | 强制刷新 `/config`，用新的 `boxKey` 重新加密后重试一次 |
| `40329` | 当前客户端已停用（网关闸门） | 立即调 `/client-control/check` 并展示停用页 |
| `40326` | 当前版本需要更新（网关闸门） | 立即调 `/client-control/check` 并引导更新 |
| `50391` | 版本管控未启用（服务端未部署） | 沿用已保存的裁决 |
| `40456` | 规则不存在（管理端） | — |
| `40459` | 设备记录不存在（管理端删除设备记录） | — |
