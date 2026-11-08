# 积分排行榜

> 面包屑：[Aegis](../CLAUDE.md) › 积分排行榜
>
> 状态：**后端已实现**（2026-10-19，迁移 000107）。控制台、Voyage App 按本文对接；实现与最初契约的出入集中写在文末「实现说明」。

应用级的积分排行榜：总榜、日榜、周榜、月榜（另可开经验榜、等级榜），名次并列、隐私脱敏、
用户可匿名上榜、管理员可移出作弊者；周期榜到期**自动结算**成快照，按名次档位发奖励。

旧接口 `/leaderboard/summary|me|points/:type|signin/:type` 与 `/api/leaderboard/*` 保留兼容，
但**同样不再返回原始账号**（见「身份展示」）。新客户端只用本文的 `/leaderboard/boards*`。

## 榜单目录（单一事实源：`internal/domain/leaderboard/catalog.go`）

| key | 名称 | 周期 | 取值 | 默认 |
|---|---|---|---|---|
| `points_total` | 积分总榜 | `total` | `users.integral` 当前余额 | 开 |
| `points_daily` | 积分日榜 | `day` | 自然日内**获得**的积分 | 关 |
| `points_weekly` | 积分周榜 | `week` | ISO 周（周一 00:00 起）内获得的积分 | 开 |
| `points_monthly` | 积分月榜 | `month` | 自然月内获得的积分 | 开 |
| `experience` | 经验榜 | `total` | `users.experience` | 关 |
| `level` | 等级榜 | `total` | 当前等级，同级按经验 | 关 |

- 周期边界一律按**应用默认时区**（与积分商城 `limitPeriod` 同一口径，`pointstore.Windows` / `timeutil`）。
- 周期键：日 `2026-10-19`，周 `2026-W43`（ISO 年-周），月 `2026-10`，总榜 `total`。
- 「获得的积分」= 该周期内 `integral_transactions` 中 `amount > 0` 且 `status = 'completed'` 的流水之和，
  **排除**：退款/冲正类分类（积分商城取消退回、支付退款冲正等，实现时逐一列在目录里并加测试钉住）、
  排行榜奖励自身（分类 `leaderboard`，否则奖励滚雪球）；管理员调整（`admin_adjust`）是否计入由设置
  `countAdminAdjust` 决定，默认不计入。
- 分类目录（`leaderboard.IntegralCategories()`，逐一查过全部写 `integral_transactions` 的地方；
  `TestIntegralCategoriesCoverEveryWriter` 扫描仓储代码，新增分类不登记即失败）：

  | 分类 | 来源 | 周期榜 |
  |---|---|---|
  | `daily_signin` | 签到奖励 | 计入 |
  | `rewarded_ad` | 看广告赠送 | 计入 |
  | `card_key` | 卡密赠送 | 计入 |
  | `invite` | 邀请奖励 | 计入 |
  | `vip_bonus` | 会员套餐赠送 | 计入 |
  | `purchase` | 积分充值（花钱买的积分） | 计入 |
  | `admin_adjust` | 管理员调整；抽奖奖励与远程函数也经同一入口、落同一分类 | 见下 |
  | `points_store` | 积分兑换（扣减） | 不计入（负数） |
  | `points_store_refund` | 积分兑换取消退回 | **不计入** |
  | `refund` | 订单退款回收（扣减） | 不计入（负数） |
  | `leaderboard` | 排行榜奖励 | **不计入** |

  `admin_adjust` 再细分：带操作管理员的（`source_type = 'admin_manual'` 且 `source_id` 非空，即控制台里的调整）
  由 `countAdminAdjust` 决定；没有操作管理员的（抽奖奖励、远程函数 `points.add`）是用户自己挣来的，照常计入。
  未登记的分类按计入处理。口径的纯函数是 `leaderboard.Counts`，SQL 与它同一口径，集成测试逐类核对。
- 已知口径：订单退款时回收积分是一笔负数流水，周期榜只看正数，所以「买了积分又退款」那部分仍算在当期获得里。
- 取值 < `minValue`（默认 1）的人不上榜：没攒到积分的人不该挤在榜尾。

## 参与者

上榜的人 = 本应用 `enabled = true`、未被封禁（`enforceAccountBan` 同一判定，含限制登录档）、
未注销、不在「移出名单」里、取值 ≥ `minValue` 的用户。

- **移出名单** `leaderboard_exclusions(appid, user_id, reason, operator, created_at)`：管理员手动移出
  （刷分、测试号）。移出对所有榜生效，含未结算的周期；已结算的快照不改写。
- 移出的人调用接口时 `me.excluded = true`，看不到自己的名次（`rank = null`），界面显示「你已被移出排行榜」。

## 名次

- **并列同名次**（竞赛排名 1,1,3）：`RANK() OVER (ORDER BY value DESC)`。等级榜按**经验**排名
  （等级由经验决定，同级比经验即全序），`value` 返回等级，`gapToPrev` 是经验差。
- 同名次内展示顺序：周期榜按「最后一次获得积分的时间」早者在前（先到先得），再按用户 ID；
  总榜按用户 ID。展示顺序不影响名次与奖励。
- 每个榜只展示前 `topLimit` 名（默认 100，10–500）；分页 `page`/`limit`（limit ≤ 50）只在这前
  `topLimit` 名内翻。榜外的人只在 `me` 里看到自己的名次（可以是 `rank > topLimit`）。
- 缓存：每个 (appid, board, periodKey) 的前 `topLimit` 名整体算一次放 Redis（总榜 60 秒、周期榜 60 秒），
  singleflight 防击穿，分页从缓存切；`me` 每次实时算（取值 + 比我大的人数 + 1），不进缓存。
  缓存键含 `periodKey / minValue / identity / countAdminAdjust / topLimit`，这些设置改了立即生效；
  移出名单、匿名偏好、昵称的变化最多晚 60 秒出现在列表里（`me` 是实时的）。
- 周期榜的聚合走新索引 `idx_integral_transactions_earned (appid, created_at) INCLUDE (user_id, amount, category,
  source_type, source_id) WHERE amount > 0 AND status = 'completed'`，只扫索引不回表。

## 身份展示（隐私）

接口**永远不返回原始账号**（账号可能是手机号、邮箱）。展示名规则（`leaderboard.DisplayName`，纯函数）：

1. 匿名上榜的人：`匿名用户`，头像为空，`userId` 不返回，`anonymous = true`；
2. 设置 `identity = nickname`（默认）且有昵称：昵称；
3. 否则：账号脱敏 —— 手机号 `138****5678`、邮箱 `ab***@q***.com`、其它 `首字符 + *** + 末两位`（长度 ≤ 3 时 `首字符 + **`）。

`identity = masked` 时一律走第 3 条（不显示昵称）。头像沿用用户资料的永久头像地址（不带账号做 gravatar 兜底：
那等于把邮箱的哈希公开给所有人）。
旧接口的 `account` 字段改为脱敏后的值（字段名不变，兼容老客户端）。

## 匿名上榜（用户偏好）

`leaderboard_preferences(appid, user_id, anonymous, updated_at)`。设置 `allowAnonymous = true`（默认）时
用户可开「匿名上榜」：名次照算、奖励照发，只是别人看到的是「匿名用户」。自己看自己那一行时
`isMe = true` 且显示自己的真实展示名（客户端可加「已匿名」标记）。`allowAnonymous = false` 时偏好保留但不生效。

## 设置 `leaderboard_settings`（一个应用一行，没有这一行按关闭处理）

```json
{
  "enabled": true,
  "boards": [ { "key": "points_total", "enabled": true }, ... ],   // 顺序即客户端标签顺序
  "topLimit": 100,
  "minValue": 1,
  "identity": "nickname",          // nickname | masked
  "allowAnonymous": true,
  "countAdminAdjust": false,
  "rewards": {                      // 仅周期榜可配；key 为榜单 key
    "points_weekly": {
      "enabled": true,
      "tiers": [
        { "rankFrom": 1, "rankTo": 1,  "rewards": [ { "type": "points", "amount": 500 } ] },
        { "rankFrom": 2, "rankTo": 3,  "rewards": [ { "type": "vip_days", "amount": 3 } ] },
        { "rankFrom": 4, "rankTo": 10, "rewards": [ { "type": "points", "amount": 100 } ] }
      ]
    }
  },
  "updatedAt": "...", "updatedBy": "..."
}
```

- 奖励权益**复用邀请的权益目录**（`invite.RewardCatalog()`：积分 / 会员套餐 / 会员天数 / 功能解锁 / 经验 / 余额 / 抽奖次数，
  不含人工发放），校验、描述、发放都走同一套（发放分支与目录双向钉死的测试照抄一份）。
  积分奖励的流水分类为 `leaderboard`。
- 档位：`rankFrom ≤ rankTo ≤ topLimit`，档位之间不重叠，最多 10 档，每档 1–6 项权益（形状同邀请）。
- 并列时按名次落档：两个人并列第 3，两人都拿第 3 名所在档的奖励。
- 总榜（`total` 周期）不能配奖励。

## 周期结算与快照

周期榜（日/周/月）每个周期结束后自动结算，**无论是否配奖励都生成快照**（供「往期榜单」查看）：

- `leaderboard_snapshots(id, appid, board, period_key, period_start, period_end, status, participants, reward_tiers jsonb,
  settled_by, settled_at, created_at)`，`UNIQUE(appid, board, period_key)`；`status`：`settling` → `settled`
  （快照行、全部条目与置为 `settled` 在同一个事务里，外面只看得到 `settled`）。`reward_tiers` 是结算时生效的档位，
  `settled_by` 为 `system`（后台循环）或手动结算的管理员账号。
- `leaderboard_snapshot_entries(snapshot_id, appid, user_id, rank, position, value, display_name, avatar, anonymous, level_name,
  rewards jsonb, reward_status, reward_results jsonb, reward_error, granted_at)`，主键 `(snapshot_id, user_id)`；
  `position` 是展示顺序，`rewards` 是结算时按档位冻结的「应发权益」（发放读它），`user_id` 不挂外键
  （注销后往期榜照旧，待发的奖励在发放时记 `skipped`）；
  只存前 `topLimit` 名；`reward_status`：`none`（没落在档位里）/ `pending` / `granted` / `failed` / `skipped`（被移出或已注销）。
  展示名、头像在结算那一刻按当时的规则冻结（之后改昵称、改匿名不影响往期榜）。
- 结算循环：API 进程后台循环（照账号注销 `Start` 的写法），每分钟一轮：
  1. 对每个开启的应用、每个开启的周期榜，看**上一个周期**（只看紧挨着的上一个，不补历史）是否已过
     结束时刻 + 宽限 5 分钟且还没有快照 → `INSERT ... ON CONFLICT DO NOTHING` 抢到的实例负责生成快照；
  2. 认领 `reward_status = 'pending'` 的条目（`FOR UPDATE SKIP LOCKED`，每轮至多 50 份），每人一个事务发奖励
     （发放在保存点里，失败回滚到保存点再记 `failed` + 原因，行锁一直握着），
     幂等键 `LB:<snapshotId>:<userId>`（钱包流水幂等键、功能解锁来源引用、积分流水 extra）；可由管理员重试；
  3. 发放成功给用户发站内信（「你在 2026 年第 43 周积分周榜获得第 3 名，奖励已到账」），站内信失败不回滚奖励。
- 奖励按**结算那一刻的设置**快照到条目里（`reward_results` 先写入「应发什么」，发完补流水号），之后改设置不影响已结算的周期。
- 管理员可对某个已结束但未结算的周期手动触发结算（同一入口，幂等），可重试 `failed` 条目。手动结算与重试后
  会唤醒循环立即跑一轮。
- 发放时再核一次：用户已注销 → `skipped`（原因「账号已注销」），已被移出 → `skipped`（「已被移出排行榜」）；
  套餐被删 / 功能停用 → `failed`（「奖励配置已失效…」）。
- 账本来源：积分分类与 `source_type` 为 `leaderboard`，会员渠道 `leaderboard`、功能解锁来源 `leaderboard`、
  钱包流水类型 `leaderboard`（不计入累计充值）。站内信类型 `leaderboard`。

## 用户端接口（应用网关，需登录）

响应外壳与现有接口一致（`{code, message, data}`）。下文只写 `data`。

### `GET /leaderboard/boards`
```json
{
  "enabled": true,
  "boards": [
    {
      "key": "points_weekly", "label": "积分周榜", "metric": "points", "period": "week",
      "periodKey": "2026-W43", "periodLabel": "本周", "periodStart": "RFC3339", "periodEnd": "RFC3339",
      "valueUnit": "积分",
      "rewards": [ { "rankFrom": 1, "rankTo": 1, "label": "第 1 名", "rewards": [ { "type": "points", "label": "积分", "detail": "500 点" } ] } ]
    }
  ],
  "me": { "anonymous": false, "allowAnonymous": true, "excluded": false }
}
```
未开启时 `enabled = false`、`boards = []`（不报错，客户端显示「排行榜暂未开放」）。
`rewards` 用邀请的 `DescribeRewards` 描述（积分的 `detail` 实际是「积分 500 点」）；没配奖励时为空数组。
总榜、经验榜、等级榜的 `periodKey` 为 `total`、`periodLabel` 为「累计」、`periodStart / periodEnd` 为 null；
周期榜当前周期的 `periodLabel` 为「今天 / 本周 / 本月」。

### `GET /leaderboard/boards/:key?page=1&limit=20`
```json
{
  "board": { ...同上一项... },
  "total": 1234,                 // 参与者人数（取值 ≥ minValue 的全部，不限 topLimit）
  "topLimit": 100,
  "page": 1, "limit": 20, "totalPages": 5,   // totalPages 按 min(total, topLimit) 算
  "items": [
    { "rank": 1, "userId": 10086, "name": "小风车", "avatar": "https://...", "value": 1200,
      "anonymous": false, "isMe": false, "levelName": "Lv.5 旅人" }
  ],
  "me": { "rank": 37, "value": 260, "gapToPrev": 15, "inTop": true, "anonymous": false, "excluded": false,
          "name": "小风车", "avatar": "...", "levelName": "..." },   // 取值 < minValue、被移出、被封禁时 rank = null、value 为实际值
  "updatedAt": "RFC3339"         // 这份榜单缓存生成的时刻
}
```
- `gapToPrev`：距上一名（名次更靠前的那个取值）还差多少；第 1 名或 `rank = null` 时为 0。
- 匿名的人 `userId` 省略、`avatar` 为空、`name = "匿名用户"`；`isMe` 那一行例外（见匿名上榜）。
- `levelName` 只有经验榜、等级榜返回。
- 榜单 key 不存在或未开启：错误「排行榜不存在」（404 系码）。排行榜整体未开启：错误「排行榜暂未开放」。

### `GET /leaderboard/boards/:key/periods?limit=12`
往期列表（只有周期榜）：
```json
{ "items": [ { "periodKey": "2026-W42", "periodLabel": "2026 年第 42 周", "periodStart": "...", "periodEnd": "...",
               "participants": 980, "settledAt": "...",
               "my": { "rank": 5, "value": 820, "rewardStatus": "granted" } } ] }
```
`my` 只在我上过这一期快照时出现。

### `GET /leaderboard/boards/:key/periods/:periodKey?page=1&limit=20`
往期榜单：`board`、`period`（同上一项的一条）、`total / page / limit / totalPages`、`items`（同实时榜的行，取自快照；
匿名按结算时冻结的标记处理）、`me`（我在这一期的 `rank/value/rewardStatus/rewards`，没上榜为 null）、
`rewards`（该期结算时的档位描述）。期号格式不对 → 40000；没有这一期的快照 → 40453。

### `GET /leaderboard/rewards?page=1&limit=20`
我的榜单奖励记录：`items: [{ board, boardLabel, periodKey, periodLabel, rank, value, status, rewards:[{type,label,detail,transactionNo?,expireAt?}], grantedAt }]`，
`total / page / limit / totalPages`。只列落在档位里的（`status ≠ none`），不看排行榜此刻是否开启。

### `GET /leaderboard/preference` / `PUT /leaderboard/preference`
返回 `{ "anonymous": true, "allowAnonymous": true }`；PUT 请求体 `{ "anonymous": true }`（必填）。
`allowAnonymous = false` 时 PUT `anonymous=true` 返回 40359「当前应用不允许匿名上榜」（关掉总是允许）。

### 限流
新路由同样挂排行榜专用限流（每用户每分钟 60 次，超限 429 / 42900）。

Kotlin SDK（`engagement`）：`leaderboardBoards()` / `leaderboardBoard(key, page, limit)` / `leaderboardPeriods(key, limit)` /
`leaderboardPeriod(key, periodKey, page, limit)` / `leaderboardRewards(page, limit)` / `leaderboardPreference()` /
`updateLeaderboardPreference(anonymous)`。

## 管理端接口（`/api/admin/apps/:appkey/leaderboard/*`）

`:appkey` 与积分商城、邀请的管理路由同一写法（应用 ID 或 appKey）。权限与积分商城同级：GET 为 `app:read`，
其余为 `app:write`（走 `/api/admin/apps/:appkey/*` 的通用规则）。全部登记了审计目录。响应外壳同上，下文只写 `data`。

| 方法与路径 | 请求 | `data` |
|---|---|---|
| `GET settings` | — | `{ settings, catalog, rewardCatalog, identities, rewardStatuses, integralCategories, defaults, limits }` |
| `PUT settings` | 设置体（见下） | 保存后的 `settings` |
| `GET boards/:key` | `?periodKey=&page=1&limit=20`（limit ≤ 100） | `{ board, source, snapshot?, total, page, limit, totalPages, items[AdminEntry] }` |
| `GET exclusions` | `?keyword=&page=1&limit=20` | `{ items[Exclusion], total, page, limit, totalPages }` |
| `POST exclusions` | `{ "userIds": [1,2], "reason": "刷分" }`（1–100 人，原因 ≤ 200 字） | `{ added, missing: [userId] }` |
| `DELETE exclusions/:userId` | — | `{ removed: true }`；不在名单里 → 40454 |
| `GET snapshots` | `?board=&page=1&limit=20` | `{ items[Snapshot（含 stats）], total, page, limit, totalPages }` |
| `GET snapshots/:snapshotId/entries` | `?status=&page=1&limit=20` | `{ snapshot（含 stats）, entries: { items[SnapshotEntry], total, page, limit, totalPages } }` |
| `POST snapshots/:snapshotId/retry` | — | `{ retried }`（置回 pending 的条数） |
| `POST settle` | `{ "board": "points_weekly", "periodKey": "2026-W42" }` | `{ snapshot, created }`；未结束 → 40922 |
| `GET stats` | — | `{ enabled, boards[BoardStats], exclusions, pendingRewards, failedRewards }` |

**设置体**（`PUT settings`，GET 返回的 `settings` 同形，另有 `appid / updatedBy / updatedAt / configured`）：
`enabled`、`boards[{key, enabled}]`（顺序即标签顺序；缺的榜补在末尾并关闭）、`topLimit`（10–500，0 取 100）、
`minValue`（0–1e9，不传为 1）、`identity`（`nickname | masked`）、`allowAnonymous`（不传为 true）、`countAdminAdjust`、
`rewards: { <boardKey>: { enabled, tiers: [{ rankFrom, rankTo, rewards: [{ type, amount, money, refId, tag }] }] } }`
（权益形状同邀请：`money` 为字符串；只有周期榜能配；试用套餐、未登记功能、重叠档位、超出 topLimit 等 → 40000，message 说明哪一项）。

**目录**：`catalog[{ key, label, metric, period, valueUnit, description, defaultEnabled }]`；
`rewardCatalog` 即邀请的 `invite.RewardCatalog()`（与 `GET invite/catalog` 的 `rewards` 同形）；
`integralCategories[{ category, label, rule: counted | excluded | admin }]`；
`limits{ minTopLimit, maxTopLimit, maxMinValue, maxTiers, maxRewardsInTier, maxReasonLength, maxExclusions }`。

**AdminEntry**（管理端榜单的一行）：`rank`（被移出的人为 null）、`userId`、`account`（真实账号）、`nickname`、`name`（按展示身份算的名字）、
`avatar`、`value`、`anonymous`（用户的匿名偏好）、`excluded`、`levelName?`、`lastAt?`（周期榜最后一次获得积分的时刻）。
`source = "live"` 时实时算：**不限 topLimit**、被移出的人也列出来；`total` 是参与者人数（不含被移出的），`totalPages` 按全部行算。
给了 `periodKey` 且这一期已结算时 `source = "snapshot"`，行取自快照（`account` 关联当前账号，已注销的为空串，`excluded` 恒为 false），
`snapshot` 为该期快照；给了期号但未结算时按那个周期实时算（结算前预览）。

**Exclusion**：`appid, userId, account, nickname, reason, operator, createdAt`。

**Snapshot**：`id, appid, board, periodKey, periodStart, periodEnd, status, participants, tiers[{rankFrom, rankTo, rewards}], settledBy, settledAt, createdAt,
stats{ entries, rewarded, pending, granted, failed, skipped }`（应发 = rewarded = pending + granted + failed + skipped）。

**SnapshotEntry**：`snapshotId, rank, position, userId, value, displayName, anonymous, levelName?, rewards[权益], rewardStatus,
rewardResults[{ type, label, detail, transactionNo?, expireAt? }], rewardError?, grantedAt?, account, avatar`。`status` 筛选取值
`pending | granted | failed | skipped | none`。

**BoardStats**：`key, label, enabled, periodKey, periodLabel, participants, top[AdminEntry]（本期前 3）, lastSettledAt`。

## 错误码

| 码 | HTTP | 含义 | 出现在 |
|---|---|---|---|
| 40358 | 403 | 排行榜暂未开放 | 用户端读榜、往期（`/leaderboard/boards` 不报错，回 `enabled=false`） |
| 40359 | 403 | 当前应用不允许匿名上榜 | `PUT /leaderboard/preference` |
| 40452 | 404 | 排行榜不存在（key 不存在或该榜未开启） | 用户端；管理端 key 不存在 |
| 40453 | 404 | 该期榜单不存在或尚未结算 | 往期详情；管理端结算明细与重试（快照不存在） |
| 40454 | 404 | 该用户不在移出名单中 | `DELETE exclusions/:userId` |
| 40922 | 409 | 这一期还没有结束，不能结算 | `POST settle` |
| 40000 | 400 | 参数不合法：设置与档位校验、期号格式、总榜不结算、移出人数与原因长度 | 管理端 / 用户端 |
| 40401 | 404 | 用户不存在（要移出的用户全都不存在） | `POST exclusions` |

用户端的码登记在网关错误目录（`auth_protocol_catalog.go`）。

## 三端落点

- 后端：`internal/domain/leaderboard/`（目录、周期、展示名、档位校验、积分口径，纯函数带单测）、
  `internal/repository/postgres/leaderboard_repository.go`（取数、快照、发放认领）与 `leaderboard_grant.go`（发放分支，
  `TestLeaderboardCatalogHasGrantBranch` 与目录双向钉死）、`internal/service/leaderboard_service.go`（含后台循环，
  在 `APIApp.Start / Close` 里启停）、`internal/transport/http/leaderboard_boards_handlers.go`、迁移 `000107_leaderboard`，
  集成测试 `TestLeaderboardIntegration`：

  ```
  AEGIS_TEST_PG_DSN=postgres://user:pass@127.0.0.1:15432/<独立的测试库> go test ./internal/service -run TestLeaderboardIntegration
  ```

  测试会 `DROP SCHEMA public`，**不要**指向开发库。
- SDK：`sdk/kotlin` 与 Voyage `aegis-sdk/` 同步加方法（CRLF）。
- 控制台：应用详情「用户运营 → 排行榜」：概览、设置、奖励档位（复用 `RewardListEditor`）、移出名单、结算记录。
- Voyage App：积分中心入口「排行榜」→ 排行榜页（榜单切换、周期与倒计时、前三名领奖台、列表、置底「我的名次」、往期、奖励说明、匿名开关）。

## 实现说明（与最初契约的出入）

- 管理端路径参数是 `:appkey`（与积分商城、邀请一致），不是 `:appid`。
- 快照表多了 `reward_tiers`、`settled_by`；条目表多了 `appid`、`position`、`level_name`、`rewards`（应发权益）、
  `created_at / updated_at`，主键 `(snapshot_id, user_id)` 即契约的唯一约束。
- 「管理员调整」只指带操作管理员的 `admin_adjust`；抽奖奖励与远程函数落在同一分类但没有操作管理员，照常计入（见「榜单目录」的分类表）。
- 等级榜按经验排名，`gapToPrev` 是经验差。
- 用户端 `me` 在被封禁、冻结时也是 `rank = null`（`excluded` 仍为 false）。
- 管理端实时榜不限 topLimit，并把被移出的人列出来（rank 为 null）；给了未结算的期号时实时预览。
- 旧接口：`account` 一律脱敏（在 `PointsService.GetRankings` 出口处理，`/api/points/rankings` 等同样生效）。
  旧榜按封禁的投影排除：原来只看 `users.enabled`（永久封禁），现在同时排除 `disabled_end_time` 未到的临时冻结；
  旧榜不看移出名单。顺带修掉旧等级榜「我的名次」的 SQL：CTE 名 `current_user` 是保留字，这个查询此前一跑就 42601。
- 移出名单、匿名偏好、昵称的变化在实时榜列表上最多晚 60 秒（缓存），`me` 实时。
