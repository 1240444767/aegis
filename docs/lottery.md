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
6. 选奖品：按[出奖策略](#出奖策略)算出这个人这一次每个奖品的最终概率；到了保底且保底奖有资格时直接给它，
   否则按最终概率抽（gonum `sampleuv` 加权抽样，随机源是每次用 `crypto/rand` 播种的 ChaCha8）；
7. 占名额：设了全站每日限量的先占当天名额（`lottery_prize_daily` 条件 UPSERT），限量奖品再扣库存（`used < quantity`），
   任何一步恰好被别人抢光时改发兜底奖；
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

## 人群特权

活动可以配最多 10 条 `perks`（迁移 `000130`）：**谁**（人群 + 今天的条件）得到**什么**（效果）。特权只改写
「这个人此刻眼里的活动」（`PerkEffects.Personalize`），不新增次数来源、不改客户端契约，所以**老版本 App 不升级就生效** ——
它本来就照着服务端给的 `chances.next` 抽、照着 `video.remaining` 拦视频。活动页与抽奖事务调同一套函数（`perks.go`）。

| 字段 | 含义 |
|---|---|
| `audience` | `all` 全部 / `member` 会员（含永久）/ `lifetime` 永久会员 / `non_member` / `non_lifetime` / `ad_free` 免广告时长有效期内 |
| `signedToday` | 今天签到之后才生效 |
| `onlineMinutes` | 今天在线满 N 分钟之后才生效（`user_online_daily`，实时连接心跳累计） |
| `when` | 附加条件（expr，变量：画像的全部 `user.*`、`user.adFree`、`user.onlineMinutesToday`、`user.totalDraws`、`draw.hour` / `draw.weekday`），保存时编译 |
| `extraFree` | 每天多几次免费抽奖 |
| `adSkip` / `adSkipSlots` | 视频抽奖**免看广告**的概率（%）。今天的第 k 次视频抽奖（k = 1…场景每日上限；不限次时 1…`adSkipSlots`，默认 5）各自独立判定，**按顺序逐次解锁**（见下） |
| `pointsDiscount` / `extraPoints` | 积分抽奖省 N%（向上取整，至少 1 积分）/ 每天多几次（活动设了每日上限时） |
| `pityCut` | 保底提前 N 次（最少保留 2 次） |
| `winBoost` | 中奖奖品的权重 × 系数（1–3），公示概率随之重算 |

多条同时满足时叠加：次数相加（免费合计不超过 100），免看广告的概率按独立事件合并 1 − Π(1 − p)，折扣取最大，
保底提前相加，中奖加成相乘（封顶 3 倍）。

**免看广告按顺序逐次解锁**（`WalkAdSkip`）：第 k 次视频抽奖**轮到时**才看它免不免 —— 免看的那一次作为 1 次免费给出
（`perks.nextWaived`），用掉之后才轮到第 k+1 次；不免的那一次要看完广告才往下走。任何时刻最多只解锁「当前这一次」。
进度由今天看完并发放的视频次数（场景的 `todayCount`）与已用掉的免看次数（免费次数里超出「基础 + 额外免费」的部分）推出，
不另存状态。2026-10-26 修过一次：最初的实现一早就把全天判中的次数一次性并进免费次数（46 次视频 × 25% → 开页就多 8 次）。

**判定是确定的**：SHA-256(活动, 用户, 当天零点, k) 映射到 [0, 1) 与 p 比较。同一个人同一天刷新多少次、
抽奖事务里再算一次都是同一个结论；用户改不了输入，提前算出结论也换不来更多次数。
已解锁的免看次数从 `video.remaining` 里扣掉（App 据此拦视频按钮），激励广告场景自己的每日上限不变 ——
绕过 App 直接调激励广告接口的人最多多拿当天解锁的那几次。

客户端看到的：

- `chances.free.daily` / `remaining` 变大，`chances.video.remaining` 变小（判中的视频次数）、`points.cost` / `daily`、`pity.threshold` / `remaining` 按特权改写；
- 新字段 `perks`：`names`、`lines`（一项一句）、`waived`（今天已解锁的免看次数）、`nextWaived`（下一次视频抽奖免看）、`message`；
- **老客户端兼容**：顶部提示卡 `lifetime` 并入特权说明（`policy: "perk"`，原处理方式移到 `ownedPolicy`）——老 App 遇到不认识的 policy 时原样显示 `message`。

每次抽奖生效的特权写进 `trace.perks`（管理端「方案」里显示）。模拟接口按 `userId` 的真实身份，
或按 `perk`（`{member, lifetime, adFree, signedToday, onlineMinutesToday}`）给虚拟用户一个身份，保底提前与中奖加成计入模拟，结果多 `perks`。

```json
"perks": [
  {"name": "永久会员特权", "audience": "lifetime", "adSkip": 100, "extraFree": 1, "pointsDiscount": 20},
  {"name": "会员概率免广告", "audience": "member", "adSkip": 30},
  {"name": "签到送次数", "audience": "all", "signedToday": true, "extraFree": 1},
  {"name": "晚间加成", "audience": "all", "when": "draw.hour >= 20", "winBoost": 1.2}
]
```

## 保底

`pityThreshold = N`、`pityPrizeId` 指向一个不是「谢谢参与」的奖品：连续 N−1 次没抽中（谢谢参与），
第 N 次必中保底奖（记录上 `pity: true`）。抽中任何奖品都会清零。活动页的 `pity.remaining` 是「最多再抽几次必中」，
保底奖抽光后不再返回 `pity`。连续未中次数存在 `lottery_user_states`，按人按活动。

## 出奖策略

旧版只有「固定权重随机 + 硬保底」。现在活动可以配一份 `strategy`（迁移 `000124`），在基础权重上逐个维度乘系数，
算出**这个人、这一次**的最终概率。全部不配、奖品不设限量时与旧版完全相同。

| 维度 | 配置 | 作用 |
|---|---|---|
| 硬性资格 | 奖品的 `dailyLimit` / `userLimit` / `userDailyLimit`，库存 | 全站每天最多出几个、每人最多中几次、每人每天最多中几次；不满足的这一次权重为 0（`blocked`） |
| 设备聚集 | `risk.deviceAccounts` / `risk.ipAccounts` | 本活动当天同设备（同 IP）抽过奖的账号数超过上限时，这个人只出未中奖，保底也不触发 |
| 库存匀速释放 | `pacing.slack`（提前量，库存比例）/ `pacing.maxBoost` | 限量奖品按活动时间进度放量：已放量 = 库存 ×（进度 + 提前量）。超前时在提前量内线性降到 0，落后超过一个提前量时放大到 `maxBoost` 倍。需要结束时间 |
| 积分预算 | `budget.dailyPoints` | 本活动当天发出的积分（含折算）过半后，积分奖按剩余比例线性收紧；单个奖品超过剩余预算时不出。并发下是软上限 |
| 行为画像 | `behavior` | 见下节 |
| 中奖冷却 | `cooldown.draws` / `cooldown.factor` | 这个人最近 N 次里中过的奖品，权重 × factor |
| 自定义规则 | `rules[]` | expr 表达式为真时，给命中的奖品（类型 / 位置）乘系数，0 等于这一次剔除 |
| 软保底 | `softPity.start` / `softPity.step` | 连续未中达到 start 次起，每多一次未中，中奖总概率抬高 step 个百分点（中奖奖品之间比例不变，只缩小未中奖） |
| 硬保底 | `pityThreshold`（原有） | 到点直接给保底奖；保底奖这一次被硬性资格挡掉时退回常规抽取 |

每一次的完整方案（各奖品的基础权重、每个维度的系数与原因、最终概率、行为分明细、是否保底 / 改发兜底）
写进 `lottery_draws.trace`，管理端记录里点「方案」可看；客户端接口一律不返回。

### 概率控制层

各维度系数是连乘的，叠多了中奖率与发出去的价值都会失控。控制层（`strategy_control.go`，迁移 `000125`）在系数之后、
抽样之前按顺序把「这一次」的分布整体校正，每一步都有明确的数学含义：

| 步骤 | 配置 | 原理 |
|---|---|---|
| 运气平衡 | `luck.strength` | 每次抽奖是成功率不同的伯努利试验，中奖次数服从**泊松二项分布**。逐次累加均值 Σp、方差 Σp(1−p)、三阶矩 Σp(1−p)(1−2p)（`lottery_user_states.luck_*`），用带偏度修正的**精细正态近似（RNA）**求 mid-p 分位，再取 Φ⁻¹ 得 z。倒霉（z < 0）的在 logit 上加 −strength·z，走运的减，截断 ±3σ。期望按**控制层之前的公平中奖率**（`trace.fair`）累计，否则被补偿的人期望跟着涨，会来回过冲 |
| 波动模式 | `variance: "prd"` | **伪随机分布**：距上次中奖第 n 次的中奖率为 min(1, n·C)，C 由当前中奖率 p 二分反解（使 1/E[次数] = p）。长期中奖率不变，连败与连中都大幅减少；与软保底二选一 |
| 全站反馈 | `winRate.target` / `gain` / `prior` | 今日全站中奖率用 **Beta–二项共轭后验**估计：先验 Beta(κ·目标, κ·(1−目标))，后验均值 (k + κ·目标)/(n + κ)；样本少时收缩到目标、不乱调。校正量 = gain ×（logit(目标) − logit(后验)） |
| 积分节奏 | `budget.paced` | 允许已发 = 预算 ×（当天时间进度 + 5%），系数 exp(−3(已发/允许 − 1)) 截在 [0.05, 2]：超前收紧、落后放宽 |
| 护栏 | `guard.minWinRate` / `maxWinRate` / `maxValue` / `kindCaps` | 中奖率区间、每次期望价值（积分当量）上限、按奖品类型的单次占比上限，全部是线性不等式。取满足约束且与当前分布 **KL 散度最小**的分布（**I-投影**，最小鉴别信息原理）：解为指数族 p_i ∝ q_i·exp(−λ·a_i)，在凹对偶函数上做带有效集的**投影牛顿法**，KKT 条件满足即停；约束在可发奖品上不可行时 `trace.guardInfeasible` |

奖品的积分当量（`prize.value`，库列 `points_value`）：0 为按类型估算（积分按数量；经验 × `guard.experienceRate`（默认 0.1）；
抽奖次数 × `guard.drawValue`（默认积分抽奖价，再默认 10）；会员类奖品与已拥有时折算的积分同价（默认折算策略 + 活动单价）；
会员套餐按 30 天估）。人工发放与会员套餐建议显式设置。`trace` 多 `fair`（控制前中奖率）、`value`（这一次的期望价值）、`luck`（z）、`prd`。

同一奖盘 2000 人 × 60 次的模拟（`strategy_control_test.go`）：独立抽取最长连败 P99 为 13 次、每人中奖次数 P10–P90 为 21–31；
PRD 把连败 P99 压到 4、长期中奖率不变；运气平衡把中奖次数区间收窄到 23–29；两者叠加 24–28。

### 精确分析与审计

- **首中分布**（`markov.go`）：把「已连续未中 k 次」看成马尔可夫链的状态，每个状态的中奖率 h_k（风险函数）由同一套
  `PlanDraw` / `PityChoice` 给出，P(T > n) = Π(1 − h_k)，E[T]、方差、分位数都是精确值，尾部按几何分布补齐；
  由**更新报酬定理**，长期中奖率 = 1/E[T]。模拟接口返回 `analytic`，与蒙特卡洛结果互相印证（测试里两者偏差 < 1%）。
- **出奖审计**（`audit.go`，`GET /lottery/activities/{id}/audit?days=14`）：每次的概率都不同，普通卡方检验不适用。
  第 t 次是参数 p_t 的单次多项试验，合计 O 的期望 E = Σp_t、协方差 Σ = Σ(diag(p_t) − p_t p_tᵀ)；由多元中心极限定理
  T = (O − E)ᵀ Σ⁺ (O − E) → χ²(rank Σ)，Σ⁺ 用 SVD 求伪逆。另对每个奖品做泊松二项 z 检验，**Holm–Bonferroni** 校正，α = 0.01。
  硬保底与改发兜底的那几次不计入。用来证明线上出奖确实服从算法给出的概率（随机源、实现、数据都没出问题）。

### 行为画像

抽奖前（事务外，一条语句）取这个人在应用里的全部行为，按活动配置的权重加权成 0–1 的行为分：

| 信号 | 来源 | 映射 |
|---|---|---|
| 注册时长 | `users.created_at` | x / (x + half) |
| 近 7 / 30 天在线时长、近 30 天在线天数 | `user_online_daily`：实时连接心跳满 5 分钟与断开时按 UTC 自然日累计，单日封顶 86400 秒 | 同上 |
| 近 30 天登录天数、距上次活跃 | `login_audit_logs`、`user_online_daily`、`user_login_devices` | 距上次活跃为 half / (x + half) |
| 连续签到、近 30 天签到天数、累计签到 | `sign_stats`、`daily_signins`（连签只在昨天或今天签过时算） | x / (x + half) |
| 当前会员、永久会员、会员率 | `users.vip_expire_at` / `vip_lifetime_at`、`vip_transactions` | 会员率 = 累计会员天数 / 注册天数（永久会员为 1） |
| 累计付费、付费笔数 | `payment_orders`（status = paid） | x / (x + half) |
| 等级、近 30 天获得积分 | `users.experience` → `user_levels`、`integral_transactions` | 同上 |
| 邀请人数、近 30 天看完激励广告、兑换次数 | `invite_relations`、`app_rewarded_ad_views`、`points_store_redemptions` + `card_key_redemptions` | 同上 |
| 近 30 天风险分（负向） | `risk_assessments` 最高分 | 乘法惩罚 |

行为分 = 正向信号的加权平均 × Π(1 − s)^权重（负向信号不参与平均，否则「没有风险」会白送分数给新号）。
再折成中奖奖品的系数：`minFactor + (maxBoost − minFactor) × 行为分`，可限定只作用于某些奖品类型。
`signals` 不填时用目录里每个信号的推荐权重与半饱和值；控制台可逐项改。画像取不到时按空画像继续，不拦抽奖。

### 自定义规则

表达式用 [expr-lang/expr](https://expr-lang.org)，强类型环境，写错变量名在**保存时**就报错，执行有编译缓存：

- `user.*`：画像的全部字段（`registeredDays`、`onlineMinutes30d`、`signStreak`、`memberRate`、`paidAmount`、`level`、`invites`、`adViews30d`、`riskScore30d`、`signedToday`、`devices`……），
  本活动里的 `totalDraws`、`todayDraws`、`missStreak`、`winsTotal`、`winsToday`、`prizeWins`、`prizeWinsToday`，行为分 `score`
- `draw.*`：`source`、`hour` / `weekday`（应用时区）、`deviceAccounts`、`ipAccounts`
- `prize.*`：`id`、`kind`、`position`、`amount`、`weight`、`probability`（公示 %）、`limited`、`remaining`
- `activity.*`：`progress`（0–1）、`daysLeft`、`pointsIssuedToday`

```json
"strategy": {
  "softPity": {"start": 5, "step": 5},
  "pacing": {"slack": 0.05, "maxBoost": 1.5},
  "cooldown": {"draws": 5, "factor": 0.3},
  "budget": {"dailyPoints": 10000},
  "risk": {"deviceAccounts": 3, "ipAccounts": 0},
  "behavior": {"minFactor": 0.8, "maxBoost": 1.5, "kinds": ["points", "vip_days"],
               "signals": [{"key": "onlineMinutes30d", "weight": 2, "half": 900}]},
  "rules": [{"name": "连签一周", "when": "user.signStreak >= 7", "kinds": ["points"], "factor": 1.5}]
}
```

### 模拟

`POST /api/admin/apps/{appKey}/lottery/simulate` 让一批虚拟用户（同一画像）各自连续抽若干次，每一次走与线上同一套
`PlanDraw` / `PityChoice` / `Sample`；软保底、冷却、个人上限随每个人的进度演变，库存、每日限量与积分预算默认所有人共享
（`isolatedStock` 时各用一份）。返回实际中奖率对公示概率、首次中奖与最长连续未中的分位数（gonum `stat`）、保底触发次数、
各奖品出现比例与受限次数。`activity` 传控制台里未保存的编辑内容；`userId` 用真实用户的画像、已拥有的奖品与本活动进度，
并附他此刻再抽一次的方案（`current`）。规模上限：5000 人、每人 1000 次、合计 20 万次。不写库、不发奖。

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
| `vip_lifetime` | 永久会员（不挂套餐、不携带功能标识，与 `vip_days` 同口径；渠道 `lottery`）。要带功能的永久会员用 `vip_plan` 选永久套餐 |
| `feature_time` | 功能时长：`tag` + `amount`（分钟），进激励广告发的同一个功能时长账户（流水 ref_type `lottery`） |
| `ad_free` | 免广告时长（`amount` 天，多次抽中顺延，`user_ad_free_passes`）：有效期内 `/ads/policy` 的 `exempt` 为真（不放开屏、不受同意要求约束，不看策略的会员免除开关），功能时长一律免扣（看激励广告换时长的功能直接可用），并属于特权人群 `ad_free` |
| `manual` | 人工发放：抽中后为 `pending`，管理员在记录里标记已发放并写一句话给用户 |

后三档是抽奖独有的（积分商城、卡密里没有）。**老客户端兼容**：用户端的 `prizes[].kind` 与记录的 `prizeKind`
按最接近的已知类型下发（`vip_lifetime` → `vip_plan`，`feature_time` / `ad_free` → `feature`），老 App 的图标、配色与
「抽中后刷新会员与功能缓存」照常工作；真实类型放在新字段 `rewardType`。管理端接口一律是真实类型。

不含钱包余额。目录与发放分支由 `TestLotteryCatalogHasGrantBranch` 双向钉死。

奖盘约束（保存与上线时校验）：九宫格恰好 8 个、转盘 2–12 个；位置不重复；权重合计大于 0；
**至少一个不限库存的奖品**（否则奖池总有被抽空的一天）；最多一个兜底奖且必须不限库存；
库存不能改到已抽中数量以下。不限库存的奖品不维护 `used`（热门奖品行上没有锁竞争），抽中次数看统计。

用户端每个奖品带 `probability`：常规抽取下的公示概率（百分比，两位小数），不含保底与库存的影响。

## 已拥有的奖品（永久会员抽中会员）

永久会员抽中「会员 7 天」，照常记一段限时会员什么也没给他（功能取两条线的并集，永久那条已经全包）。
同类的还有：永久会员抽中他已含功能的套餐 / 功能、已永久解锁某功能的人再抽中这个功能。判据（`MemberCoverage.Owned`）走统一的
`vip.Holdings`（见 [会员权益折算](vip-conversion.md)「已拥有的判据」），与购买、积分商城、激励广告、卡密同一套：

| 奖品 | 何时算已拥有 |
|---|---|
| `vip_days` | 永久会员，且永久那条线免广告（看广告领到的永久会员可以不免，这时会员天数带来免广告，不算已拥有） |
| `vip_plan` | `vip.Holdings.CoversPlan`：套餐里启用中的功能都已永久拥有（永久开通或永久解锁），且免广告也已有；永久基础版抽中限时高级版不算 |
| `feature` | 已永久解锁这个功能，或永久会员的功能里有它 |
| `vip_lifetime` | 永久会员且免广告 |
| `ad_free` | 永久会员且免广告 |
| `feature_time` | 永久会员（会员一律免扣），或已永久解锁这个功能 |

积分、经验、抽奖次数、人工发放、谢谢参与从来不算。活动的 `lifetimePolicy` 决定怎么办（迁移 `000114`，已有活动默认 `points`）：

| `lifetimePolicy` | 行为 |
|---|---|
| `points`（默认） | 折算积分，单价与卡密、邀请、管理员发放同一套（应用的[会员折算策略](vip-conversion.md)）：会员按 `perVipDay`、限时功能与免广告按 `perFeatureDay`（按秒向上取整）、功能时长按 `perFeatureHour`、永久套餐 / 永久功能 / 永久会员给 `perLifetime`。活动的 `lifetimePointsPerDay` 大于 0 时覆盖会员与限时功能、免广告的每天单价，`lifetimePointsLifetime` 大于 0 时覆盖永久；两项默认 0 即完全跟随应用策略。单次封顶 1 亿 |
| `draws` | 改发 `lifetimeDraws` 次抽奖（默认 1） |
| `exclude` | 这个人的常规抽取里剔除这些奖品（权重按 0 算，公示概率随之重算）；保底 / 兜底恰好是这类奖品时按积分折算 —— 保底承诺的是「必中」 |
| `grant` | 照常发放（旧行为） |

奖品可单独设 `lifetimeReward`（积分 / 经验 / 抽奖次数），设了就不按公式折算（`grant` 时不生效）。
判定与发放都在抽奖事务里、锁用户行之后：抽奖与购买 / 兑换会员串行，不会出现「刚开通永久就抽到会员却没折算」。

### 无套利上限

积分能换到这个活动的次数时（积分抽奖价，或积分商城里卖「抽奖次数」，取单价最低的），一次抽奖的期望积分回报不能超过价格，
否则「用积分买次数、抽完再买」能稳赚（2026-10 线上出过：商城 10 积分一次，永久会员抽中月度会员折 300 积分，58 人几天刷走约 19 万积分）。
在这个人眼里的奖盘（剔除、特权加权之后的公示概率）上：

```
E = (P + O·c) / (1 − Q) ≤ 价格   ⇒   c ≤ (价格·(1 − Q) − P) / O
```

P 积分奖品的期望积分，O 已拥有、会改发成积分或抽奖次数的格子的概率，Q 抽奖次数奖品的期望次数，c 每次改发多少（次数按价格折成积分）。
改发（公式折的积分、奖品单独设的改发积分、改发的抽奖次数）超过上限 c 时按 c 发积分；c 为 0 时改发「谢谢参与」（`conversion.reward.type = none`，
说明「不再重复发放」，老客户端显示「已转为 谢谢参与」）。积分换不到次数（只有免费、看视频、卡密）时不封顶。规则在 `domain/lottery/economy.go`。

管理端活动带 `economy`（`price`、`memberReturn`、`lifetimeReturn`、`lifetimeCap`、`warnings`）：奖盘本身（不算折算）的期望回报就超过价格、
或对永久会员剔除之后超过价格、或抽一次期望再得一次以上时给出警告，控制台活动列表上显示。

记录上 `prizeKind` / `reward` 仍是抽中的那一格（统计按格子算），实际发的写在 `conversion`：

```json
"conversion": {"reason": "lifetime", "reward": {"type": "points", "amount": 70}, "label": "70 积分",
               "message": "你已是永久会员，「会员 7 天」已转为70 积分"}
```

`results` 是实际到账的流水（积分流水标题「抽奖奖品折算」，metadata 带 `converted`、`originalKind`）。
每次改发另记一条会员折算记录（`vip_conversions`，`kind = owned`、`source = lottery`，改发成积分时带积分流水号），
`conversion.conversionNo` 指向它：用户的 `/vip/conversions` 与控制台的折算视图里能看到抽奖折掉的东西。
活动页的奖品带 `swap`（`convert` + 改发的东西，或 `excluded`），页顶带 `lifetime` 提示（只在这个人有已拥有的奖品时出现）。
统计概览多 `converted`、`convertedPoints`，奖品分布多 `converted`。

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
| POST | `/api/admin/apps/{appKey}/lottery/simulate` | 出奖策略模拟（见上文「模拟」），附首中精确分析 `analytic` |
| GET | `/api/admin/apps/{appKey}/lottery/activities/{activityId}/audit?days=14` | 出奖审计（见上文「精确分析与审计」） |

活动的保存体：

```json
{
  "name": "幸运抽奖", "description": "", "rules": "", "uiMode": "grid", "status": "draft",
  "startTime": null, "endTime": null,
  "freeDaily": 1, "pointsCost": 0, "pointsDaily": 0, "videoScene": "lottery",
  "pityThreshold": 6, "pityPosition": 1, "sortOrder": 0,
  "lifetimePolicy": "points", "lifetimePointsPerDay": 0, "lifetimePointsLifetime": 0, "lifetimeDraws": 1,
  "prizes": [
    {"id": 0, "name": "", "reward": {"type": "none"}, "imageUrl": "", "quantity": -1, "weight": 40, "position": 0, "fallback": true},
    {"id": 0, "name": "2 积分", "reward": {"type": "points", "amount": 2}, "quantity": -1, "weight": 30, "position": 1},
    {"id": 0, "name": "会员 7 天", "reward": {"type": "vip_days", "amount": 7}, "quantity": 100, "weight": 2, "position": 2,
     "lifetimeReward": {"type": "points", "amount": 100}}
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
积分抽奖与每日上限、保底、限量奖品并发只出库存数、同一 requestId 并发只抽一次、会员 / 功能 / 人工发放、
已拥有的奖品（四种处理方式、单奖品改发、剔除后的概率与保底、已永久解锁的功能、统计）、删除与统计，
最后在旧表形状上跑 000110 + 000114 + 000124 + 000125 + 000130 两遍核对搬迁。

`TestLotteryPerksIntegration`：永久会员奖品（老客户端看到 `vip_plan`）与再抽中的折算、永久会员特权（4 次视频全部免看广告并进免费次数、
额外免费、积分 5 折与额外次数、保底提前、提示卡并入说明、trace 记特权）、签到条件、功能时长奖品、免广告时长（顺延、功能时长免扣、
开屏免除、`ad_free` 人群）、永久会员抽中免广告 / 功能时长的折算、模拟里的特权身份。单元测试在 `perks_test.go`（含免看广告判定的稳定性与比例校准）。

`TestLotteryStrategyIntegration`：12 人并发下全站每日限量不超发、个人上限、积分预算、同设备聚集、
行为画像整条查询（签到、会员率、邀请、在线时长累计）与规则、方案落库且不给用户端、模拟与单人预览、策略回读与非法规则。
策略、画像、规则、卡方抽样检验与模拟的单元测试在 `internal/domain/lottery/strategy_test.go`。

```
AEGIS_TEST_PG_DSN=postgres://... go test ./internal/service -run 'TestLotteryIntegration|TestLotteryStrategyIntegration|TestLotteryPerksIntegration'
```
