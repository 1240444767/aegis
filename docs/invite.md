# 邀请

> 面包屑：[Aegis](../CLAUDE.md) › 邀请

用户在**注册时**或**注册后限定时间内**（默认 72 小时）填写邀请码，被邀请人与邀请人按应用的配置各得奖励。
邀请码分两种：每个用户一个的**个人码**，和运营创建的**活动码**（渠道码 / 推广码）。

| 表 | 内容 |
|---|---|
| `invite_settings` | 开关与配置（`config` JSONB），一个应用一行；没有这一行按关闭处理 |
| `invite_codes` | 邀请码：个人码一人一个（`uq_invite_codes_personal`），比对按 `UPPER(code)`、应用内唯一 |
| `invite_relations` | 邀请关系：一个用户在一个应用里只能被邀请一次（`uq_invite_relations_invitee`） |
| `invite_milestone_grants` | 阶梯奖励的发放记录，每人每档一次 |

老系统的 `user_profiles.invite_code`（24 位、全局唯一）与 `parent_invite_account` 保留：
查不到新码时回落到老的个人码；绑定时把邀请人账号写回 `parent_invite_account`，读老字段的地方照样知道是谁邀请的。

## 配置

全部字段在 [`internal/domain/invite`](../internal/domain/invite/types.go) 的 `Config`；控制台的表单由
`GET /api/admin/apps/{appKey}/invite/catalog`（权益目录、各枚举、默认值与上限）驱动。

| 分组 | 字段 | 说明 |
|---|---|---|
| 注册表单 | `registerField` | `hidden` 不出现 / `optional` 选填 / `required` 必填（没有邀请码不能注册） |
| | `fieldLabel` `fieldPlaceholder` | 输入框的名称与提示 |
| | `bindWindowHours` | 注册后多少小时内可以补填，0 为只能注册时填，最多 720 |
| 个人码格式 | `codeLength` `codeCharset` `codePrefix` | 4–16 位（纯数字至少 6 位）；`alnum` / `digits` / `letters`，去掉 0 O 1 I；前缀最多 6 位 |
| | `allowCustomCode` | 允许用户把个人码改成自定义的 4–16 位字母数字 |
| 邀请人资格 | `inviterAudience` `inviterMinLevel` | `all` / `vip`；等级门槛。不满足时没有可用的个人码，他的码也填不进去 |
| 奖励 | `inviteeRewards` `inviterRewards` | 各最多 6 项，可为空（只奖励一方） |
| | `milestones` | 累计成功邀请满 N 人的阶梯，最多 10 档，每档只发一次 |
| | `inviterTrigger` `triggerSignInDays` | 邀请人奖励的发放时机：`bind` 填写即发 / `signin` 被邀请人累计签到满 N 天后发 |
| | `inviterDailyLimit` `inviterTotalLimit` | 邀请人每天 / 累计最多拿几次，0 不限。超出后邀请照样成立、被邀请人照拿，邀请人这次记 `limited` |
| | `notifyInviter` | 邀请人拿到奖励（含阶梯）时发站内信 |
| 防刷 | `deviceLimit` `ipDailyLimit` | 同一设备在应用内最多填写几次（默认 1）、同一 IP 每天最多几次；0 不限。另走风控 `invite` 场景 |
| 文案 | `rulesText` `shareTemplate` `landingUrl` | 活动规则；分享文案（`{code}` `{link}` `{app}`）；落地页（`{code}`） |

### 权益

[`invite/catalog.go`](../internal/domain/invite/catalog.go)：积分商城的六档（会员套餐、会员天数、功能解锁、经验、余额、抽奖次数，
同一套账本）加上**积分**。不含人工发放：邀请奖励在填写的同一个事务里就要到账。
`TestInviteCatalogHasGrantBranch` 把目录与发放分支双向钉死。会员渠道、功能解锁来源、钱包流水类型都是 `invite`，
积分与经验流水的分类是 `invite`。引用的套餐与功能在**保存配置时**核对（试用套餐不行），填写时再核一次。

## 能不能填写：一个纯函数

`invite.EvaluateBind(settings, facts)`，事实由仓储在锁住双方之后取齐。判据顺序即优先级：

| reason | 码 | 含义 |
|---|---|---|
| `disabled` | 40363 | 未开启 |
| `already_bound` | 40365 | 已填写过 |
| `window_closed` | 40364 | 超过补填时限（或只能注册时填写） |
| `code_not_found` / `code_disabled` / `code_not_started` / `code_expired` / `code_exhausted` | 40366 | 码本身不可用 |
| `self_invite` / `cycle` | 40367 | 自己的码 / 对方是自己邀请来的 |
| `inviter_ineligible` | 40366 | 个人码的主人不满足邀请人资格 |
| `reward_unavailable` | 40355 | 配置的套餐或功能已失效 |
| `device_limited` / `ip_limited` | 40368 | 设备 / IP 次数已满（风控拦截也是 40368） |

另有 40369（注册必填却没填）、40356（不允许自定义）、40357（自己不满足邀请人资格，换码时）、40948（码已被占用）。
管理端：40446 码不存在、40447 关系不存在、40914 邀请人奖励不在等待中。

## 填写：一个事务

查码 → **按 ID 升序锁住被邀请人与邀请人** → 锁码行并重读 → 判定 → 记关系 → 码的使用次数加一 →
发被邀请人奖励 → 写回 `parent_invite_account` → 条件已达成时结算邀请人。

- 两个人一起按 ID 升序锁：A 填 B 的码、B 同时填 A 的码时不会互相等死，后到的一方看到「对方是我邀请来的」（`cycle`）。
- 码行锁让 `maxUses` 在并发下准确；邀请人的行锁让每日 / 累计上限与阶梯计数准确。
- 任何一项发不出去整笔回滚。已拥有的会员与功能按[会员权益折算](vip-conversion.md)策略折成积分；策略为照常发放时已永久拥有的功能不算失败，结果里记「已永久拥有」。

邀请人奖励的状态：`none`（纯渠道码，没有邀请人）、`pending`（等被邀请人签满 N 天）、`granted`（算一次成功邀请）、
`limited`（达到上限）、`cancelled`（管理员取消）。签到落库后 `SignInService` 回调 `InviteService.OnSignIn`
（API 与 Worker 都接了，自动签到也算），在后台结算。

## 注册

`/config` 的 `auth.invite`：`enabled`、`field`（未开启时恒为 `hidden`）、`label`、`placeholder`、`maxLength`、`bindWindowHours`。
`POST /auth/register` 多一个 `inviteCode`：

1. 建号**之前**核对：必填却没填 40369，填了却不能用 40366 / 40367；
2. 建号之后填写，结论在响应的 `invite`（`bound`、`message`、`rewards`、`inviter`）。注册不因此失败。

短信注册对已有手机号是直接登录：注册时间超过 10 分钟的账号按「补填」的规则走（时限照样适用）。

## 接口

用户端：

```
GET  /api/v1/apps/{appKey}/invite            邀请页：code、shareText、shareLink、双方奖励、milestones、stats、bind
GET  /api/v1/apps/{appKey}/invite/invitees   我邀请的人
POST /api/v1/apps/{appKey}/invite/bind       { code, deviceId }  补填
PUT  /api/v1/apps/{appKey}/invite/code       { code }  自定义；为空则重新生成（旧码立即失效）
POST /api/v1/apps/{appKey}/invite/preview    { code }  注册页预览，不需要登录；不能用时 valid=false + reason
```

未开启时 `/invite` 回 `enabled=false`，不报错。奖励到账后向双方推送 `invite.changed`。

管理端：`/api/admin/apps/{appKey}/invite/{catalog,settings,codes,relations,stats}`；活动码可指定一个或按格式批量生成
（最多 500 个，可另给前缀）、挂推广人（拿邀请人奖励）、单独配置被邀请人奖励（`rewards` 为 null 时跟随默认）、
限次数与有效期；个人码可以停用不能删除。等待中的邀请人奖励可以立即发放（不看条件与上限）或取消。

Kotlin SDK：`auth.register(..., inviteCode)`、`auth.previewInvite`、`engagement.invite / invitees / bindInvite / updateInviteCode`，
`AuthCapability.invite`。

## 验证

`TestInviteIntegration`（真实 Postgres）走完开关、引用核对、个人码格式、注册前核对、双方奖励、重复 / 设备 / 自己 / 下级、
阶梯、每日上限、补填时限、签到满 N 天、管理员取消与发放、活动码（覆盖奖励、次数、批量、推广人）、自定义与占用、
老系统个人码、并发抢次数、互填不死锁、预览、邀请人资格、统计：

```
AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55434/postgres go test ./internal/service -run TestInviteIntegration
```
