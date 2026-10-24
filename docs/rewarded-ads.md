# 激励广告

用户看完一段激励视频，按「奖励场景」发放会员 / 积分 / 经验 / 抽奖次数。
目前接入的平台是 **Apex**（ApexAdx，底层为 ToBid / WindMill 聚合，平台标识 `apex`）。2026-10 起取代灰鲸（`huijing`）：
两家回调协议相同，迁移 `000103_rewarded_ads_apex` 把已有配置的平台改成 `apex`，但 Security Key 是各平台各自签发的，
迁移后要在控制台填入 Apex 后台的 Security Key，并把回调地址换成下面的新路径。旧的 `/ads/huijing/callback` 已删除。

## 一次观看怎么才算数

一次观看由两方各确认一次，两方靠平台下发的 `transId` 对上：

| 一方 | 证明什么 | 怎么鉴别 |
|---|---|---|
| 平台服务端回调 | 这次观看真的发生了 | `sign = sha256(securityKey + ":" + transId)` |
| 客户端上报 | 这次是为哪个场景、哪个账号看的 | 网关用户令牌 |

只信客户端等于谁都能伪造一次观看；只信回调则不知道是为哪个场景看的，也无法拒绝
「拿别人的 transId 来领奖」。因此默认的 **双方确认**（`dual`）模式下，两方都到齐才发奖。

| 校验模式 | 发奖条件 | 用途 |
|---|---|---|
| `dual`（默认） | 回调验签通过 **且** 客户端已上报 | 生产 |
| `server` | 回调验签通过即发；场景取客户端上报、回调 `extrainfo` 里的 `aegisScene`，或广告位对应的第一个场景 | 客户端无法上报时 |
| `client` | 客户端上报即发，不验签 | **仅联调** |

两方到达的先后不确定，平台还会重试回调（无响应时每 200ms 重试 3 次）、客户端会轮询上报。
所以只有一个结算入口 `Repository.RecordRewardedAdView`，所有请求都落在 `(appid, trans_id)` 这一行上：

1. 先到的一方建行（`INSERT … ON CONFLICT DO NOTHING`），随后 `SELECT … FOR UPDATE` 锁行；
2. 每一方只补自己那一半的事实（回调：验签时间、广告位、奖励名、extra；上报：场景、设备、IP），从不覆盖另一方；
3. 条件满足时锁用户行、判日限额与冷却、同事务发奖；状态只有一次跃迁 `pending → granted / rejected`。

日限额与冷却在**锁住用户行之后**判定，同一用户的两次结算因此串行，不会被并发穿透。
`UNIQUE (appid, trans_id)` 是「一次观看只发一次」的最后一道保证。

## 用户标识

`/ads/rewarded` 下发的 `userId` 形如 `4829173056482917.1a2b3c4d5e6f7a8b`：对外用户 ID（与 `/me` 的 `id` 相同，见 [user-public-id.md](user-public-id.md)）+ HMAC 签名（密钥派生自 `SECURITY_MASTER_KEY`）。
切换到对外 ID 之前发出的主键形态（`42.…`）回调时照样认得。
客户端必须把它**原样**传给 SDK 的 `ApexAdxRewardRequest(placementId, userId, options)`。

平台的回调签名只覆盖 `transId`、不覆盖 `userId`。不带签名的话，能伪造一次回调的人就能把奖励记到任意账号头上；
带上签名后，回调里的 `userId` 只能是我们发出去的那一个。认不出的用户记为 `user_not_found`，不发奖。

## 接口

### 平台回调（服务端命名空间，无令牌）

```
GET /api/apps/{appKey}/ads/apex/callback?userId={{USER_ID}}&transId={{TRANS_ID}}&sign={{SIGN}}&extrainfo={{EXTRAINFO}}&placementId={{PLACEMENT_ID}}
→ {"isValid": true | false}
```

参数宏与 [Apex 服务端回调文档](https://doc.apexadx.com/server/reward-callback)一致，平台替换成实际值后 GET 调用；
`user_id` / `USER_ID` 等写法也认，`rewardAmount` / `rewardName` / `networkId` 有就记下。控制台复制出的地址已带好这串宏。
`extrainfo` 是客户端放进 SDK options 的内容（App 放 `aegisScene`，Apex 自己会再加一个 `BUSTRANSID`）。
`isValid` 回答「这次观看算不算数」：验签不过、应用没开、用户认不出、被限额挡下都是 `false`；
`dual` 模式下等客户端上报时是 `true`。响应不套本项目的信封 —— 对方只认它自己的格式。

它不在网关 `/api/v1/apps/*` 下：平台服务器没有用户令牌，也无法做 signed / sealed 包装。

### 客户端（App Protocol v1，需登录）

| 目录 key | 方法 | 路径 | 说明 |
|---|---|---|---|
| `rewardedAdStatus` | GET | `/ads/rewarded` | 是否启用、各场景剩余次数与冷却、传给 SDK 的 `userId` |
| `rewardedAdClaim` | POST | `/ads/rewarded/claim` | 上报 `{scene, transId, placementId?, verified?, errorCode?, deviceId?}` |
| `rewardedAdRecords` | GET | `/ads/rewarded/records` | 我的观看记录 |

上报的返回体 `status`：`granted` 已到账；`rejected` 看了但不发（`reason`：`daily_limit` / `scene_daily_limit` /
`cooldown` / `scene_unavailable` / `user_mismatch` / `client_unverified` / `user_not_found` / `disabled`）；
`pending` 还在等平台回调 —— 用同一个 `transId` 再上报一次即可轮询，奖励只会发一次。

只有「这次上报本身不成立」才是错误码：`40350` 未启用、`40351` 场景不存在或已停用、`40352` 这次观看属于另一个账号。

### 管理端（`app:read` / `app:write`，落在应用级兜底规则里）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET / PUT | `/api/admin/apps/{appKey}/rewarded-ads/config` | 配置；GET 附带权益目录与回调地址，Security Key 只回「配没配」 |
| GET | `/api/admin/apps/{appKey}/rewarded-ads/views` | 观看记录（status / scene / keyword / start / end / userId） |
| POST | `/api/admin/apps/{appKey}/rewarded-ads/views/{viewId}/grant` | 补发：不等另一方、不看限额，一条只能补发一次（`40930`） |
| GET | `/api/admin/apps/{appKey}/rewarded-ads/stats?days=14` | 概览与按天趋势（缺失日期补零） |

## 配置

存放在 `app_rewarded_ad_configs`（一应用一行），场景以 JSONB 整体保存。Security Key 以 AES-GCM 密文落库，
派生自 `SECURITY_MASTER_KEY`，留空表示不修改。开启开关前会挡下「开了也发不出去」的配置：
没有启用中的场景，或非 `client` 模式却没有 Security Key。

权益复用卡密的权益目录与数据形态，但只开放 `vip_plan` / `vip_days` / `integral` / `experience` / `lottery_draws`
（余额是真钱、设备位挂在授权卡上，都不该由广告发），另加广告独有的 `feature_minutes`（功能时长，带 `feature`，
按实际使用扣，见 [功能时长](feature-time.md)）。目录与发放分支由 `TestRewardedAdCatalogHasGrantBranch` 双向钉死。
会员发放走同一套账本，渠道为 `ad_reward`。永久会员看到的已拥有权益按场景配置折算，见下文「永久会员与已拥有的权益」。`vip_plan` 不能选永久套餐：广告可以每天反复看，
永久会员看一次就到头了，之后每次观看只是白发套餐附赠的积分。

## 看广告赠送的会员权益

每个场景有一份**会员权益**（`scene.membership`），与套餐一样**跟随当前配置**：改了它，已经领到、仍在期内的会员随即按新的权益算。

| 字段 | 作用 |
|---|---|
| `features` | 「会员天数」这一档带哪些功能标识（必须是本应用功能目录里的）。「会员套餐」那一档的功能跟随所选套餐，不看这里；不送会员天数却配了功能的场景保存时报错 |
| `adFree` | 这个场景送出的会员（两档都算）是否免广告：免除广告服务的同意要求、不展示开屏广告。不填按免广告，与这项配置出现之前一致 |

判定时，看广告领的会员段（渠道 `ad_reward`）按开通记录 metadata 里的 `scene` 找到场景、取它现在的会员权益
（`vip.Segment.AdTerms`）；场景被删除后回落到开通那一刻的快照（`vip_transactions.features` 与 `metadata.adFree`）。
会员是否免广告（`Entitlement.adFree`）取仍生效各段的「或」：只要有一段免广告就免 —— 付费、试用、兑换、管理员发放的会员一律免广告，
所以只有全部会员期都来自「不免广告」的场景时才是 false。`/vip/status`、服务端会员校验与广告服务策略都读这一个结论，
广告服务的会员免除看的是它而不是「是不是会员」，否则「看一次广告换一天不看广告」会让同意要求形同虚设。

客户端的场景列表带 `membership`，`rewardSummary` 也会补上一句会员权益（如「会员天数 1 天（会员可用 AI 对话，会员期间免广告）」，
只列启用中的功能）。

## 永久会员与已拥有的权益

永久会员看一次「领 1 天会员」的广告，照常记一段限时会员什么也没给他（功能取两条线的并集，永久那条已经全包了）。
每个场景有一份 `scene.lifetime`，决定**已拥有**的权益怎么办。判定在结算事务里、锁住用户行之后做（与购买 / 兑换会员串行），
客户端列表与结算用同一个纯函数 `rewardedad.Resolve`，两边说的一定一样。

什么算已拥有（`rewardedad.Coverage.Owned`，与抽奖同一套数据）：

| 权益 | 已拥有的条件 |
|---|---|
| `vip_days` | 永久会员，且场景会员权益里的功能都已在永久开通里（多出来的功能是升级，照发） |
| `vip_plan` | `vip.LifetimeIncludes`：与购买入口同一个判据 |
| `feature_minutes` | 永久会员（会员使用时免扣，余额永远用不上），或已永久解锁该功能 |

积分、经验、抽奖次数、AI 额度从来不算已拥有；场景里不是已拥有的那几项照常发放。

| `policy` | 处理 |
|---|---|
| `convert`（默认，不填即此） | 按价值折算：会员每天 `perVipDay`（会员天数按天数、套餐按套餐天数）、功能时长每 60 分钟 `perFeatureHour`（按比例向上取整），折成 `target`（`integral` / `ai_credit` / `experience` / `lottery_draws`）；两项都不填时为 10 / 10、折成积分。套餐附赠的积分照发。各档不超过该档的单次上限 |
| `replace` | 改发一组固定权益 `rewards`（只能是上面四档） |
| `hide` | 对已拥有的用户不出现在 `/ads/rewarded` 列表里；仍有观看到达（列表是旧的、抽奖视频场景等直接上报）时按 `convert` 折算，不拒发 |
| `grant` | 照常发放（这项配置出现之前的行为） |

结果：观看记录多一列 `conversion`（迁移 000129）记「配的是 A、发的是 B」，`claim` 返回体带 `conversion`，`message` 换成它的说明；
`/ads/rewarded` 的场景对这个人带 `lifetime` 提示，`rewardSummary` 改写成折算后实际会发的。统计多了 `converted`（按天、按场景）、
`totalConverted` 与 `convertedIntegral`。

**不需要升级客户端。** 已发布的 App 只认原有字段，所以：

- 场景的 `rewards` / `membership` 仍按配置原样下发（老 App 拿 `rewards` 找「发听歌时长」的场景、判断领完要不要刷新会员缓存），折算只体现在 `rewardSummary` 里；
- 折算说明作为 `results` 的第一行（`type: "conversion"`，`detail` 即说明）：老 App 的领取弹窗与观看记录只逐行展示 `results`，不读 `message`；
- 新字段（`lifetime`、`conversion`）都是可选的，老 App 解析时忽略。

## Apex 后台怎么配

1. 在 Apex 后台的激励视频广告位上开启服务端回调，回调地址填控制台「激励广告 → 配置 → 回调地址」复制出的那一条（已带参数宏）；
2. 把 Apex 后台给出的 Security Key 填进控制台同一处（两边必须一致，签名是 `sha256(SecurityKey + ":" + transId)`）；
3. 控制台里「平台应用 ID」填 Apex 应用 ID（只作核对展示），场景的「广告位 ID」填该广告位的 ID。

Voyage 的正式 Apex 应用 ID 是 `84371`（包名 `com.cityray.aihrplus.vgjx`）：「平台应用 ID」填它，Security Key 与场景广告位都用这个应用下的。
Voyage 的所有构建都用这个应用，不再使用 Apex 的测试应用（`68758`）与测试广告位，测试广告位也不要填进配置。
