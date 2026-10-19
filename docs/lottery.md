# 抽奖

应用级抽奖活动：九宫格（8 个奖品）或转盘（2–12 个），每人每天的免费次数、看激励视频换次数、花积分抽，
以及「连续 N 次没抽中必中某个奖品」的保底。2026-10 重构（迁移 `000110_lottery_rework`），取代旧版的
报名 / 种子承诺 / 上链那一套。

## 结果只在服务端产生

客户端只说「用哪一种次数抽一次」：

```
POST /api/v1/apps/{appKey}/lottery/activities/{activityId}/draw
{"source": "free" | "bonus" | "points" | "", "requestId": "<uuid>"}
```

抽中哪一格、发什么、发多少，全部在**同一个数据库事务**里决定并到账（`Repository.DrawLottery`）：

1. 读活动（`FOR SHARE`，与「改奖盘」互斥）、判状态与时间窗；
2. 锁用户行 —— 今天用了几次、次数余额、积分余额、连续未中次数都是这个人的事实，同一个人的抽奖从这里开始串行；
3. `requestId` 回放：同一个键再来一次，原样返回第一次的记录（`replayed: true`）；
4. 判定用哪一种次数（`lottery.EvaluateChances` / `ResolveSource`，与活动页显示的剩余次数同一个函数）；
5. 扣次数：bonus 用条件 UPDATE（`balance > 0`）；积分在落记录之后按记录 ID 记账；
6. 选奖品：到了保底就直接给保底奖，否则在「有库存、权重 > 0」的奖品里用 `crypto/rand` 按权重抽；
7. 限量奖品用条件 UPDATE（`used < quantity`）扣库存，恰好被别人抽走最后一份时改发兜底奖；
8. 落记录（奖品存快照）→ 发奖（与积分商城、邀请同一套账本）→ 更新连续未中次数。

任何一步失败整笔回滚：免费次数、bonus、积分都没有花出去。不存在「客户端上报抽中了 X，服务端照着发」这条路。

## 三种次数

| 来源 | 是什么 | 限制 |
|---|---|---|
| `free` | 每人每天的免费次数（活动的 `freeDaily`） | 按应用时区的自然日 |
| `bonus` | 应用级的抽奖次数余额（`lottery_draw_grants`），任意活动可用 | 余额 |
| `points` | 每次消耗 `pointsCost` 积分 | `pointsDaily`（0 不限）与积分余额 |

`source` 留空时按 free → bonus → points 自动选；活动页的 `chances.next` 就是这个顺序的结论。

### 视频抽奖

视频抽奖不是第四种来源。活动的 `videoScene` 指向一个**激励广告场景**，那个场景发的是「抽奖次数」：

1. 活动页的 `chances.video` 给出场景、广告位、签名用户标识（与 `/ads/rewarded` 的 `userId` 相同）、今天还能看几次、冷却；
2. 客户端按激励广告的流程加载、播放、上报（见 [激励广告](rewarded-ads.md)）—— 看没看完由 Apex 的**服务端回调**说了算；
3. 结算后次数进 bonus，客户端再用 `source: "bonus"` 抽。

因此视频抽奖的每日次数、冷却都在激励广告的场景里配；保存活动时会核对场景存在且发「抽奖次数」。
广告看完但回调还没到时（`pending`），次数会在回调到达的那一刻补发，客户端下次进页面就能看到。

## 保底

`pityThreshold = N`、`pityPrizeId` 指向一个不是「谢谢参与」的奖品：连续 N−1 次没抽中（谢谢参与），
第 N 次必中保底奖（记录上 `pity: true`）。抽中任何奖品都会清零。活动页的 `pity.remaining` 是「最多再抽几次必中」，
保底奖抽光后不再返回 `pity`。连续未中次数存在 `lottery_user_states`，按人按活动。

## 奖品

奖品发的东西与积分商城同构（`reward` JSON），由目录 `lottery.Catalog()` 驱动：

| `kind` | 说明 |
|---|---|
| `none` | 谢谢参与 |
| `points` | 积分（流水分类 `lottery`，计入周期排行榜） |
| `experience` | 经验 |
| `vip_days` / `vip_plan` | 会员（渠道 `lottery`，不能选试用套餐） |
| `feature` | 功能解锁（天数，0 为永久；已永久拥有时记一笔说明） |
| `lottery_draws` | 抽奖次数（进 bonus） |
| `manual` | 人工发放：抽中后为 `pending`，管理员在记录里标记已发放并写一句话给用户 |

不含钱包余额。目录与发放分支由 `TestLotteryCatalogHasGrantBranch` 双向钉死。

奖盘约束（保存与上线时校验）：九宫格恰好 8 个、转盘 2–12 个；位置不重复；权重合计大于 0；
**至少一个不限库存的奖品**（否则奖池总有被抽空的一天）；最多一个兜底奖且必须不限库存；
库存不能改到已抽中数量以下。不限库存的奖品不维护 `used`（热门奖品行上没有锁竞争），抽中次数看统计。

用户端每个奖品带 `probability`：常规抽取下的公示概率（百分比，两位小数），不含保底与库存的影响。

## 接口

### 客户端（App Protocol v1，需登录）

| 目录 key | 方法 | 路径 | 说明 |
|---|---|---|---|
| `lotteryActivities` | GET | `/lottery` | 进行中的活动（含未开始的）；没有时为空列表 |
| `lotteryState` | GET | `/lottery/activities/{activityId}` | 活动与奖品、`chances`、`pity` |
| `lotteryDraw` | POST | `/lottery/activities/{activityId}/draw` | 抽一次，返回 `draw`（含 `position`）、抽完后的 `chances` 与 `pity` |
| `lotteryDraws` | GET | `/lottery/draws?activityId=&page=&limit=` | 我的抽奖记录 |

错误码：`40402` 活动不存在或已下线 / `40923` 未开始 / `40924` 已结束 / `42913` 今日免费次数已用完 /
`42914` 今日积分抽奖次数已用完 / `42915` 没有可用次数 / `40307` 积分不足 / `40928` 该方式未开放 /
`40929` 奖品配置已失效（整笔已回滚） / `40925` 同一 requestId 正在处理 / `40348` 风控拦截（场景 `lottery`）。

抽中积分或花了积分时向用户的其它在线设备推 `points.changed`；抽中人工发放的奖品发站内信（类型 `lottery`，跳转路由 `lottery`）。

### 管理端（`app:read` / `app:write`）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/admin/apps/{appKey}/lottery/catalog` | 奖品目录、界面形态、状态、可选的会员套餐 / 功能 / 发抽奖次数的激励广告场景 |
| GET / POST | `/api/admin/apps/{appKey}/lottery/activities` | 活动列表（含奖品与汇总）/ 新建（连同奖品） |
| GET / PUT / DELETE | `/api/admin/apps/{appKey}/lottery/activities/{activityId}` | 详情 / 整体更新活动与奖品 / 删除（有记录时 `40927`，改为结束） |
| PUT | `/api/admin/apps/{appKey}/lottery/activities/{activityId}/status` | `draft` / `active` / `paused` / `ended`；上线前按当前奖盘再校验一次 |
| GET | `/api/admin/apps/{appKey}/lottery/activities/{activityId}/stats?days=14` | 概览、按天趋势、来源与奖品分布 |
| GET | `/api/admin/apps/{appKey}/lottery/draws` | 记录（activityId / userId / status / source / winning / keyword / start / end） |
| POST | `/api/admin/apps/{appKey}/lottery/draws/{drawId}/fulfill` | 人工发放的奖品标记已发放（`{note}` 写进站内信；重复标记 `40926`） |

活动的保存体：

```json
{
  "name": "幸运抽奖", "description": "", "rules": "", "uiMode": "grid", "status": "draft",
  "startTime": null, "endTime": null,
  "freeDaily": 1, "pointsCost": 0, "pointsDaily": 0, "videoScene": "lottery",
  "pityThreshold": 6, "pityPosition": 1, "sortOrder": 0,
  "prizes": [
    {"id": 0, "name": "", "reward": {"type": "none"}, "imageUrl": "", "quantity": -1, "weight": 40, "position": 0, "fallback": true},
    {"id": 0, "name": "2 积分", "reward": {"type": "points", "amount": 2}, "quantity": -1, "weight": 30, "position": 1}
  ]
}
```

奖品按 `id` 对上：带 id 的更新（保留已抽中数量），不带的新建，原有而这次没带的删除（记录有快照，不受影响）。
保底奖按位置（`pityPosition`）指定，名字留空时按奖品内容生成（「30 积分」「会员 7 天」）。

## 迁移

`000110_lottery_rework` 在旧列还在时一次性搬迁（迁移器每次启动都重跑全部 `*.up.sql`，所以搬迁放在以旧列为条件的 DO 块里）：
旧 `cost_type=free` 的活动搬成每天 `max(daily_limit, 1)` 次免费（新版没有「不限次免费」）、`points` 的搬成积分抽奖；
旧奖品 `points` / `experience` 且值为数字的照搬，名字像「谢谢参与」的搬成 `none`，其余搬成 `manual`；
旧记录补单号与快照，`claimed` 记为已发放，用户已被删除的记录清掉。`lottery_participants` 与 `lottery_seed_commitments` 删除。

## 验证

`TestLotteryIntegration`（真实 Postgres）：保存校验、草稿不可见、免费次数与幂等回放、视频抽奖（client 模式的激励广告发次数）、
积分抽奖与每日上限、保底、限量奖品并发只出库存数、同一 requestId 并发只抽一次、会员 / 功能 / 人工发放、删除与统计，
最后在旧表形状上跑 000110 两遍核对搬迁。

```
AEGIS_TEST_PG_DSN=postgres://... go test ./internal/service -run TestLotteryIntegration
```
