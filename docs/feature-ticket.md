# 权益凭证（POST /vip/feature-ticket）

服务端签名的「此人、此设备、此刻拥有某项会员权益」。客户端离线也能验，用于冷启动第一帧就要判定、
又不能信本地布尔值的功能（Voyage 的自定义启动动画 `custom_splash`）。

## 请求

`POST /api/v1/apps/:appkey/vip/feature-ticket`（需登录）

```json
{ "feature": "custom_splash", "digest": "<base64url，可空，≤128>", "nonce": "<base64url，16–128>" }
```

- 没有该权益：`40230 FEATURE_NOT_GRANTED`（HTTP 402）。客户端应删除本地旧凭证。
- 功能标识未登记：`40486`。版本管控未启用：`50391`。

## 响应 data（信封）

```json
{
  "ticket": "<base64url(ticketJSON)>",
  "signature": "<base64url>",
  "signingKeyId": "ccs_…",
  "keys": [{ "keyId": "ccs_…", "publicKey": "…", "endorsedBy": "", "endorsement": "" }]
}
```

ticketJSON：

| 字段 | 说明 |
| --- | --- |
| `v` | 恒为 1 |
| `appKey` | 应用 |
| `feature` | 功能标识（小写） |
| `userId` | 对外用户 ID（与 /me 一致） |
| `deviceId` | 会话登录时的设备；老会话没有时取请求头 `X-Device-Id` |
| `lifetime` | 签发时是永久会员 |
| `digest` | 请求里的 digest 原样 |
| `challenge` | 请求里的 nonce 原样 |
| `issuedAt` / `expiresAt` | Unix 秒。永久会员 30 天；限时会员 7 天且不超过会员到期 |

## 签名

```
签名：Ed25519(版本管控签名密钥, "aegis-feature-ticket-v1\n" ‖ ticketJSON)
背书：Ed25519(上一把签名密钥, "aegis-client-control-endorse-v1\n{appKey}\n{keyId}\n{publicKey}")
```

复用版本管控的签名密钥（见 client-control.md）。签名域前缀与裁决不同，同一把密钥签的裁决不能冒充凭证。
客户端只从**内置**的根公钥出发、沿信封 `keys` 的背书链验到 `signingKeyId`，不使用本地记下的密钥。
Kotlin SDK：`client.featureTickets.issue(feature, pinnedKeys, digest)` 与 `AegisFeatureTicketApi.verify/restore`。

## 接入自定义启动动画（Voyage）

1. 控制台「会员功能目录」新建标识 `custom_splash`（名称如「自定义启动动画」）。
2. 把它加到永久会员套餐的功能里。客户端还要求凭证 `lifetime = true`，限时套餐带了该标识也不放行。
