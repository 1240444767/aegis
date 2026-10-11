# 自动签到

> 面包屑：[Aegis](../CLAUDE.md) › 自动签到

到了用户设定的时刻，服务端替用户签到，奖励与手动签到完全一致（同一条 `SignInForUser`），
签到记录的来源为 `auto`。

## 两层开关

| 层 | 存在哪 | 谁改 | 决定什么 |
|---|---|---|---|
| 开放策略 | `apps.settings.autoSign` | 管理员（控制台「签到 → 自动签到」） | 应用开不开放、开放给谁 |
| 个人设置 | `user_settings`（`category = autoSign`） | 用户 | 开不开、几点签、失败重试与通知 |

两层分开放：管理员收紧范围时不改任何人的个人设置，资格恢复后原来的时刻接着生效。

### 开放策略

| 字段 | 说明 |
|---|---|
| `enabled` | 是否开放。**未配置即不开放** |
| `audience` | `all` 所有用户 / `vip` 有效会员（含永久） / `lifetime` 仅永久会员 / `feature` 持有指定功能 |
| `includeTrial` | `audience = vip` 时试用会员算不算 |
| `featureTag` | `audience = feature` 时的功能标识，保存时核对必须已登记。会员套餐所含与功能解锁（如积分商城兑换）都算 |

判定是纯函数 `appdomain.EvaluateAutoSign`，开启校验、排班、执行与状态接口四处共用：

| reason | 说明 |
|---|---|
| `disabled` | 应用未开放（或全局 `AUTO_SIGN_ENABLED` 关闭） |
| `vip_required` | 仅限会员 |
| `trial_excluded` | 试用会员不在开放范围内 |
| `lifetime_required` | 仅限永久会员 |
| `feature_required` | 需要持有指定功能，`message` 带功能名 |

### 个人设置

`enabled`、`time`（HH:MM，按状态里的 `timezone`）、`retryOnFail`、`maxRetries`（0–10，失败后最多再试几次，
间隔 `AUTO_SIGN_RETRY_DELAY`）、`notifyOnSuccess`、`notifyOnFail`、`disableLocationTracking`。
写入前经 `MergeAutoSignSettings` 清洗：只改传了的字段，类型或取值不对报 40000。

**首次取用户信息（`GET /me`）时**，各类个人设置的默认值落库（`EnsureDefaultUserSettings`，
只补缺的分类，已有的不动）；Redis 标记 30 天内不再重复。新增设置分类时调高 `settingsInitVersion`。

## 用户端

| 接口 | 说明 |
|---|---|
| `GET /me/auto-sign` | 状态：`available`、`reason`、`message`、`active`（已开启且有资格）、`settings`、`access`（开放策略与功能名）、`timezone`、`nextRunAt`、`lastRun` |
| `PUT /me/auto-sign` | 改设置（字段同上，全部可选），立即重排，返回新状态 |

**只拦「从关到开」**：没有资格时开启返回 40353（未开放）或 40354（不满足开放对象），`message` 与状态里的同一句话。
已经开着、因失去资格而暂停的用户仍可改时刻、改通知、关掉。`PUT /me/settings` 的 `autoSign` 分类走同一套校验。

`lastRun.status`：`success`、`already_signed`（到点时已手动签过）、`retrying`（失败，等重试）、
`failed`（重试用完，次日再签）、`suspended`（到点时已失去资格，暂停）。

站内信（`type = system`，`metadata.scene = auto_sign`）：签到成功（`notifyOnSuccess`）；
重试用完仍失败、到点时失去资格（`notifyOnFail`）。管理员关闭开放不逐个通知。

## 调度

排班是 Redis 有序集合 `autosign:schedule`，分值为下一次执行时刻。API 与 Worker 都跑调度循环，
到期条目先 `ZREM` 认领，认领到的实例才执行，不会签两次、发两条通知；认领后中断的由周期重建补回。

- 改个人设置：`SyncUserSchedule` 按新时刻覆盖排班；
- 改开放策略：后台 `RebuildApp` 重排本应用；
- 每 `AUTO_SIGN_REBUILD_INTERVAL`：全量重建，补上资格恢复的人、撤下失去资格的人，已在排班上的不动（不打乱重试）；
- 到点时再判一次资格：失去资格记 `suspended`、通知一次、撤下排班，个人设置保持开启。

## 管理端

`GET / PUT /api/admin/apps/:appkey/auto-sign`：策略 + 使用情况（开启人数、今日自动签到次数、近 14 天趋势）。

## 验证

`TestAutoSignIntegration`（真实 Postgres + miniredis）走完「首次初始化 → 未开放 → 仅会员 → 签到与通知 →
已签 → 到期暂停 → 指定功能 → 关闭 → 统计」；判定、设置清洗在 `internal/domain/{app,user}/autosign_test.go`。
