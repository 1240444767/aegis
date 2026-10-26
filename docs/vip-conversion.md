# 会员权益折算

> 面包屑：[Aegis](../CLAUDE.md) › 会员权益折算

永久会员（`users.vip_lifetime_at`）与限时会员、功能解锁、功能时长、免广告时长是并列的几条线。
有两种情况会让一份权益对用户「拿到了等于没拿到」，统一按应用的**折算策略**处理：

| 情况 | 例子 | 默认处理 |
|---|---|---|
| 已拥有的发放 | 永久会员收到「会员 7 天」、他已包含的套餐、永久会员已包含的功能解锁、已永久解锁的功能 | 折成积分，不记会员段 |
| 开通永久时的存量 | 开通前买的月卡还剩 20 天、限时解锁的导出功能、在线听歌余额、免广告时长 | 按剩余时间折成积分，就此结束 |

规则在 `internal/domain/vip/conversion.go`（纯函数），落账在 `internal/repository/postgres/vip_conversion_repository.go`。

## 「已拥有」的判据

只有一个入口：`vip.Holdings`（永久身份、永久开通的功能并集、已永久解锁的功能）。

- 会员天数 / 套餐：永久会员，且这段会员带的功能都已在永久开通里（`LifetimeIncludes`）。带了永久会员没有的功能（永久基础版再拿限时高级版）**不算**已拥有，照常发放。
- 功能解锁：已永久解锁该功能，或永久会员已包含该功能。
- 功能时长：永久会员（会员一律免扣），或已永久解锁该功能。
- 免广告时长：永久会员。

购买（40378）、积分商城（不可兑换）、抽奖（活动的已拥有处理方式）、激励广告（场景的 `lifetime`）都问同一个判据。

## 策略

`GET/PUT /api/admin/apps/:appkey/vip/conversion-policy`，没配过的应用按默认值：

| 字段 | 默认 | 说明 |
|---|---|---|
| `owned` | `convert` | 已拥有的发放：`convert` 折算为积分 / `grant` 照常发放 |
| `upgrade` | `convert` | 开通永久时的存量：`convert` 折算并结束 / `keep` 保留到原到期时间 |
| `perVipDay` | 10 | 会员每天折多少积分（按秒、向上取整） |
| `perFeatureDay` | 5 | 限时功能解锁、免广告每天折多少积分 |
| `perFeatureHour` | 10 | 功能使用时长每小时折多少积分 |
| `perLifetime` | 3650 | 已包含的永久套餐、已永久解锁的功能再被发放时给的固定积分 |
| `restoreOnRevoke` | `true` | 永久会员被取消或退款时，把开通时折掉的存量还回、收回折给的积分 |

GET 同时返回 `ownedModes`、`upgradeModes`（处理方式目录）与 `defaults`。

## 哪些入口按策略折算

| 入口 | 已拥有的发放 | 开通永久的存量 |
|---|---|---|
| 管理员发放 / 远程函数 `vip.grant`、`vip.grantLifetime` | 按策略；管理员可以这一次选「照常发放」（`ownedPolicy: "grant"`） | 按策略；管理员可以这一次指定（`upgradePolicy: "convert" \| "keep"`） |
| 卡密、邀请奖励、排行榜奖励 | 按策略（会员与功能解锁） | 按策略 |
| 余额购买、在线支付、积分商城、试用 | 下单前就拦（40378 / 不可兑换 / 不可领），不会走到 | 按策略 |
| 抽奖、激励广告 | 活动 / 场景自己的处理方式（改发积分、次数、隐藏等） | 按策略 |

开通永久的折算发生在永久开通的同一个事务里（`grantLifetimeVipTx` 末尾），所有入口自动生效。

策略为 `grant` 时，已永久解锁的功能再被发放没有可记的：卡密报 40983（卡不被核销），邀请 / 排行榜记一笔「已永久拥有」。

## 开通永久时怎么折

- 限时会员段：功能都已在永久开通里的那几段被折掉 —— 正在走的截在此刻，还没轮到的整段作废；排在它们后面、没被覆盖的段（永久基础版之后另买的高级版）整体前移，不留空档。`users.vip_expire_at` 随之更新。没有开通记录的那截到期时间（老系统写进 users 的）一并折掉。
- 限时功能解锁：功能已在永久开通里的作废（`revoke_reason = lifetime_convert`）。
- 功能时长：余额清零，流水记 `convert`。
- 免广告时长：到期时间收到此刻。

## 记录

- `vip_conversions`：每次折算一行 —— `kind`（`owned` / `upgrade`）、`source`（触发的渠道）、`items`（折掉的每一项与积分）、`points`、积分流水号、触发的那笔永久开通、`status`（`applied` / `restored`）。
- 会员账本：已拥有的发放记一行**原渠道、0 天**的说明；开通永久折掉存量记一行 `lifetime_convert` 渠道、负时长的说明。两者都带 `conversion` 摘要（`conversionNo`、`kind`、`points`、`label`、`message`），不是会员段，不贡献任何权益。
- 积分流水分类 `vip_convert`，不计入积分排行榜。
- 发放结果（卡密、邀请、排行榜）的 `results[]` 里，被折算的那一项 `detail` 是折算说明，另带 `conversion`。

## 取消永久会员

管理员取消永久会员、或永久套餐订单退款后，用户不再是永久会员时（另有永久开通则不动），按 `restoreOnRevoke`：

- 折掉的会员时长按原功能各还一段（`lifetime_convert` 渠道、正时长，按秒精确），功能解锁、功能时长、免广告各自还回。
- 折给的积分收回，余额不足时收到 0 为止，差额记在 `restore.pointsShortfall`。
- 折算记录标为 `restored`。

已拥有的发放（owned）折出的积分不收回：那一笔本来就什么也没给。

## 接口

| 接口 | 说明 |
|---|---|
| `GET /api/admin/apps/:appkey/vip/conversion-policy` | 策略、处理方式目录、默认值 |
| `PUT /api/admin/apps/:appkey/vip/conversion-policy` | 整体覆盖保存 |
| `GET /api/admin/apps/:appkey/vip/conversions?userId=&page=&limit=` | 折算记录 |
| `POST /api/admin/apps/:appkey/vip/grant/preview` | 发放前预览（请求体同 `/vip/grant`）：`owned`（会不会折、折多少）与 `upgrade`（存量会怎样） |
| `POST /api/admin/apps/:appkey/vip/grant` | 新增 `ownedPolicy`、`upgradePolicy` |
| `GET /vip/conversions`（网关）、`GET /api/vip/conversions` | 用户自己的折算记录 |
| `GET /vip/plans` | 永久套餐新增 `upgradePreview`（`mode`、`items`、`points`、`message`）：当前用户开通后存量会怎样，没有存量时不出现 |
| `GET /vip/transactions` | 记录新增 `conversion` 摘要 |

## 数据

迁移 `000131_vip_conversion`：`vip_conversion_policies`、`vip_conversions`，`feature_time_ledger.kind` 增加 `convert` / `restore`。

## 测法

`TestVipConversionIntegration`（嵌入式 PG：`AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55441/postgres go test ./internal/service -run TestVipConversionIntegration`），
纯函数 `internal/domain/vip/conversion_test.go`。
