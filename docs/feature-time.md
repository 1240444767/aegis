# 功能时长

> 面包屑：[Aegis](../CLAUDE.md) › 功能时长

按**实际使用的秒数**计量的功能权益。典型场景是在线听歌（功能标识 `online_music`）：非会员看一次激励广告换
60 分钟，按实际播放的时长扣，暂停期间不扣；会员免扣。时长记在服务端，单次广告给多少分钟由控制台的激励广告场景配置。
能力是通用的，任意功能标识都能用。

## 与会员、功能解锁的区别

| | 回答的问题 | 怎么消耗 |
|---|---|---|
| 会员 / 功能解锁 | 这段时间里能不能用 | 到期即止，用多用少都一样 |
| 功能时长 | 还能用多久 | 只在真正使用时扣，余额不过期 |

**免扣**：当前是会员（`Entitlement.IsVIP`，含试用、永久），**或**权益里带这个功能（`Entitlement.HasFeature`，
会员段的功能与功能解锁都算）。免扣时租约接口不扣余额、不建租约，余额原样保留，会员到期后接着用。
判定是纯函数 `featuretime.Exempt`，每次租约都重新判一次 —— 会员到期的下一次续租就开始扣。

## 概念

- **余额**：每个（应用，用户，功能）一行秒数余额（`feature_time_accounts`）。只由发放增加（目前只有激励广告），
  由租约扣减；每一次变动都记一笔流水（`feature_time_ledger`：`earn` / `lease` / `refund`），余额永远等于流水之和。
  `usedSeconds` 是实际用掉的：租出减去退回。
- **租约**：客户端开始使用前向服务端**预扣**一段（≤ 600 秒），这段立刻从余额里扣走；用完再租下一段；
  停止使用时结算，没用完的退回余额。客户端永远拿不到「没扣就能用」的路径，不结算只会让用户自己吃亏。
- **过期**：租约在租出 2 小时后过期，过期的租约结算时不退款。不需要定时任务：结算时比较 `expires_at`，
  在第一次结算时把状态写成 `expired`。从未结算的租约一直停在 `open`，那一段已经扣走，不影响任何人的余额。

并发：所有改余额的路径都先锁住余额行，判余额与扣减在同一把锁里；结算先锁租约行再动余额行，租出只锁余额行，
两条路径不会互相等死。

## 发放：激励广告的「功能时长」

激励广告场景新增一档权益 `feature_minutes`（「功能时长」，单位分钟，1–1440），带功能标识字段 `feature`：

```json
{"type": "feature_minutes", "amount": 60, "feature": "online_music"}
```

- **只开放给激励广告**：卡密、积分商城、邀请各有自己的权益目录，都不含它（`TestRewardCatalogOnlyOffersAllowedTypes`、
  pointstore / invite 的目录测试钉住）。常量是 `rewardedad.RewardFeatureMinutes`，取值形态 `cardkey.ValueFeature`
  （`Reward.Feature` + `Reward.Amount`）；目录与发放分支仍由 `TestRewardedAdCatalogHasGrantBranch` 双向钉死。
- 一个场景最多一项功能时长；保存场景时核对 `feature` 必须是本应用功能目录里登记的标识（停用的也可以选）。
- 发放与其它权益同事务：余额 += `amount × 60`，流水记一条 `earn`（`ref_type = rewarded_ad`，`ref_id` 为观看记录 ID）。
  发放结果形如 `{"type":"feature_minutes","label":"功能时长","detail":"在线听歌 60 分钟（余额 95 分钟）"}`，
  余额按整分钟向下取整。
- `GET /ads/rewarded` 的 `scenes[].rewards[]` 原样带出 `feature`，`rewardSummary` 说成「在线听歌 60 分钟」
  （功能名取启用中的功能目录，认不出时用标识本身）。

## 用户端接口

应用由 appkey 决定，用户由访问令牌决定，与其它 `/me` 类接口一致。功能标识须合 `FeatureTagPattern`
（小写字母开头，可含数字、点、下划线、连字符，2–64 位），不合报 40000；不要求登记在功能目录里。

| 接口 | 说明 |
|---|---|
| `GET /features/time/{feature}` | 余额与免扣状态 |
| `POST /features/time/{feature}/leases` | 开始使用前租一段 |
| `POST /features/time/leases/{leaseNo}/settle` | 停止使用时结算 |

### `GET /features/time/{feature}`

```json
{"feature":"online_music","name":"在线听歌","balanceSeconds":3600,"exempt":false,"earnedSeconds":7200,"usedSeconds":3600}
```

`name` 取功能目录里的名称，没有则为空串。

### `POST /features/time/{feature}/leases`

请求 `{"seconds": 300}`：1–600，超过 600 按 600 截断，小于 1 报 40000。

| 情形 | 响应 |
|---|---|
| 免扣 | 200 `{"exempt":true,"leaseNo":"","seconds":0,"balanceSeconds":<当前余额>,"expiresAt":""}` |
| 余额为 0 | 403，code `40328`，「使用时长已用完」 |
| 其它 | 扣 `min(seconds, 余额)`：200 `{"exempt":false,"leaseNo":"FTL…","seconds":300,"balanceSeconds":3300,"expiresAt":"2026-10-12T12:00:00Z"}` |

### `POST /features/time/leases/{leaseNo}/settle`

请求 `{"usedSeconds": 120}`，夹到 `[0, 租约秒数]`。

```json
{"leaseNo":"FTL…","feature":"online_music","status":"settled","seconds":300,"usedSeconds":120,"refundedSeconds":180,"balanceSeconds":3480}
```

- `status = settled`：退回 `seconds − usedSeconds`。
- `status = expired`：过期后才结算，`refundedSeconds = 0`。
- 已结算的再结算一次**幂等**：原样返回第一次的结论，`balanceSeconds` 为当前余额。网络重试不会退两次。
- 租约不存在或不属于当前用户：404，code `40406`（两者不区分，免得能拿租约号探测别人的记录）。

## 客户端用法

1. 进入功能前 `GET /features/time/{feature}`：`exempt` 为真直接用；否则 `balanceSeconds` 为 0 时引导看激励广告。
2. 开始使用时租一段（如 300 秒），在本地按实际使用计时；快用完时租下一段，再结算上一段（`usedSeconds` 为整段）。
3. 暂停、退出、切走时结算当前租约，`usedSeconds` 为本段实际用掉的秒数。
4. 租约返回 `exempt = true` 时不结算（没有租约号）；收到 40328 时停止使用并提示看广告。

## 错误码

| code | HTTP | 说明 |
|---|---|---|
| `40000` | 400 | 功能标识无效，或 `seconds` 小于 1 |
| `40328` | 403 | 使用时长已用完（`FEATURE_TIME_EXHAUSTED`） |
| `40406` | 404 | 租约不存在或不属于当前用户（`FEATURE_TIME_LEASE_NOT_FOUND`） |

## 数据

迁移 `000097_feature_time`：

| 表 | 说明 |
|---|---|
| `feature_time_accounts` | 主键 `(appid, user_id, feature)`；`balance_seconds`（CHECK ≥ 0）、`earned_seconds`、`used_seconds` |
| `feature_time_ledger` | `delta_seconds`、`balance_after`、`kind`（`earn` / `lease` / `refund`）、`ref_type`、`ref_id`、`remark` |
| `feature_time_leases` | `lease_no` 唯一；`seconds`、`used_seconds`、`refunded_seconds`、`status`（`open` / `settled` / `expired`）、`expires_at`、`settled_at` |

## 上线

1. 在控制台的功能目录里登记功能标识（如 `online_music`，名称「在线听歌」）。
2. 激励广告里加一个场景，权益选「功能时长」、选功能、填分钟数。
3. 客户端按上面的用法接入（Kotlin SDK：`featureTime` / `leaseFeatureTime` / `settleFeatureTimeLease`）。

## 代码位置

| 层 | 文件 |
|---|---|
| 领域 | `internal/domain/featuretime/types.go`（租约上限与有效期、结算与免扣判定） |
| 仓储 | `internal/repository/postgres/feature_time_repository.go`；发放分支在 `rewarded_ad_settle_repository.go` |
| 服务 | `internal/service/feature_time_service.go` |
| 接口 | `internal/transport/http/feature_time_handlers.go` |
| 测试 | `internal/domain/featuretime/types_test.go`；集成测试 `internal/service/feature_time_integration_test.go`（`TestFeatureTimeIntegration`，需 `AEGIS_TEST_PG_DSN`） |
