# 用户消息中心

> 面包屑：[Aegis](../CLAUDE.md) › 用户消息中心
>
> 状态：已实现（2026-10-19，后端与控制台）。Voyage App 按本文对接。与最初契约的偏离见文末「实现说明」。

应用用户的站内信：分类、跳转动作、完整的已读/未读/删除接口、按分类的推送偏好，
以及**实时推送**——新消息、已读、删除都经 `/api/ws` 推到该用户所有在线设备，多端状态一致。

旧接口（`/notifications`、`unread-count`、`read`、`read-batch`、`read-all`、`clear`、`DELETE /:id`）
路径与请求形状不变，响应只**加字段**，老客户端不受影响。旧实时事件 `notification.state.changed` 继续发。

## 分类（单一事实源：`internal/domain/notification/category.go`）

| key | 名称 | 归入的 `type` | 推送可关 |
|---|---|---|---|
| `system` | 系统通知 | `system` 与其余一切未登记的 type | 是 |
| `announcement` | 公告活动 | `announcement`、`campaign`、`marketing`，管理员群发默认 | 是 |
| `security` | 账号安全 | `security`、`geo_login`、`password_reset`、`two_factor`、`passkey` | **否** |
| `membership` | 会员与订单 | `vip`、`payment`、`order`、`wallet`、`refund`、`account_deletion` | 是 |
| `reward` | 积分与奖励 | `signin`、`auto_sign`、`points_store`、`invite`、`leaderboard`、`lottery`、`rewarded_ad`、`cardkey` | 是 |
| `ticket` | 工单与反馈 | `ticket`、`feedback` | 是 |

- 列 `notifications.category`：写入时由 `notification.CategoryOf(type)` 推出（大小写与首尾空白不敏感），
  调用方也可显式指定（必须是目录里的 key，否则 42231）。
- 分类顺序即客户端展示顺序；summary 与偏好接口都按这个顺序列出全部分类。
- 新增写入点时先在映射表登记 type：`TestNotificationWriterTypesAreCategorized` 扫描 `internal/` 全部源码里
  `DeliverInput{Type: ...}`、`SendUserNotification`、`NotifyUser`、`notifySecurityEvent` 的 type 字面量，
  没登记的 type 直接让测试失败（非字面量的透传参数要在测试里登记）。

### 各写入点

| 写入点 | type | 分类 | 动作 |
|---|---|---|---|
| 两步验证开启 / 关闭、Passkey 添加 / 移除（`security_service.go`） | `security` | security | route `security`「查看账号安全」 |
| 密码已重置（`auth_password_recovery.go`） | `security` | security | route `security`「查看账号安全」 |
| 异常登录提醒（`geo_risk_service.go`，Worker） | `security` | security | route `security`「查看账号安全」 |
| 账号注销各状态（`account_deletion_service.go`） | `account_deletion` | membership | route `security`「查看注销进度」 |
| 签到成功（`worker_event_service.go`，Worker） | `signin` | reward | route `signin`「查看签到」 |
| 自动签到成功 / 失败 / 暂停（`auto_sign_service.go`） | `auto_sign` | reward | route `signin`「查看签到」 |
| 自动签到测试通知（`/api/user-settings/auto-sign/test-notification`） | `auto_sign` | reward | — |
| 积分商城人工发放结果（`points_store_service.go`） | `points_store` | reward | route `points_store`「查看兑换记录」 |
| 邀请奖励、阶梯奖励（`invite_service.go`） | `invite` | reward | route `invite`「查看邀请」 |
| 排行榜奖励到账（`leaderboard_service.go`） | `leaderboard` | reward | route `leaderboard`「查看排行榜」 |
| 工单 / 反馈事件（NotifyHub 站内信渠道） | 渠道配置的 type，默认事件的 `ticket` | ticket | route `tickets`「查看工单」 |
| 远程函数 `aegis.notification.send` | 脚本传入，默认 `system` | 由 type 推出 | — |
| 管理员群发 | 管理员填写（控制台固定 `system`） | 管理员选择，默认 announcement | 管理员选择 |

会员与订单类目前没有服务端写入点（支付、会员开通不发站内信），分类先留着给管理员群发与后续写入点用。

## 消息形状（`notification.Item`）

```json
{
  "id": 1, "appid": 1, "userId": 2,
  "type": "leaderboard", "category": "reward", "categoryLabel": "积分与奖励",
  "title": "排行榜奖励已到账", "content": "你在 2026 年第 43 周积分周榜获得第 3 名。",
  "level": "info",
  "status": "unread", "read": false, "readAt": null,
  "action": { "type": "route", "target": "leaderboard", "label": "查看排行榜" },
  "image": null,
  "expiresAt": null,
  "metadata": {}, "createdAt": "...", "updatedAt": "..."
}
```

- `readAt`、`action`、`image`、`expiresAt` **恒在**，没有值时为 `null`（客户端按「字段恒在、可空」声明即可）；
  `metadata` 为空对象时省略。
- `status` 只有 `unread` / `read` 两个值（回执表里的 `delivered` 对外一律叫 `unread`），`read` 是它的布尔版。
- `level`：`info` | `success` | `warning` | `error`。写入时旧写法 `critical` / `danger` 归为 `error`、`warn` 归为 `warning`，其它值 40010。
- `action.type`：`route`（App 内页面，`target` 取下表的键，客户端不认识的键不显示按钮）或 `url`（`target` 为 https 地址，
  ≤ 2048 字符，不带账号密码与空白）。`label` 可空（≤ 16 字），空时客户端用默认文案。
- 路由键：`points`（积分中心）、`points_store`（积分商城）、`leaderboard`（排行榜）、`signin`（签到）、
  `membership`（会员）、`wallet`（钱包）、`security`（账号安全）、`invite`（邀请）、`tickets`（工单）、`profile`（个人资料）。
- `image`：https 地址（≤ 2048 字符），不合法 40010。
- `expiresAt`：必须晚于现在且不超过一年，否则 40010。
- 标题 ≤ 200 字、type ≤ 64 字符；正文为空时用标题兜底。

## 单一投递入口

`NotificationService.Deliver(ctx, notification.DeliverInput{AppID, UserID, Type, Category, Title, Content, Level,
Metadata, Action, Image, ExpiresAt}) (*notification.Item, error)`：校验 → 平台治理闸门 → 落库（消息 + 回执同一事务，返回完整一行）
→ 失效缓存 → 异步发实时事件 → 插件钩子 `notification.onCreated`（payload 加了 `id`、`category`）。

- `NotifyUser`、`SendUserNotification`、管理员群发、NotifyHub 站内信渠道都走它；其它服务只依赖接口
  `notificationDeliverer`（`Deliver` 一个方法），测试里可以换成假实现。
- `TestOnlyNotificationServiceCreatesUserNotifications` 扫描 `internal/` 全部非测试源码：除 `notification_service.go`
  外不得调用 `CreateUserNotification`。
- Worker 进程的 `NotificationService` 接的是 NATS 发布器（`NewNATSUserEventPublisher`），Worker 发出的签到、异常登录、
  自动签到、工单站内信同样经 API 实例实时推到设备。
- 签到提醒在 Worker 里投递失败只记日志、不让 NATS 消息重投（签到审计已经落库，重投会重复）。

## 用户端接口

网关 `/api/v1/apps/:appkey/notifications*`（需登录）与 `/api/notifications*`（JWT）挂同一组 handler。
响应外壳同其它接口（`{code, message, data}`），下文只写 `data`。

| 方法 路径 | 请求 | data |
|---|---|---|
| `GET /notifications` | query：`status=all\|unread\|read`、`category`、`page`、`limit`(≤100)；旧的 `type`、`level` 仍可用 | `{items, page, limit, total, totalPages, unread, categoryUnread}` |
| `GET /notifications/summary` | — | `{unread, categories:[{key, label, unread, total, latest: Item\|null}]}`（全部分类都列出，没有消息的 `total=0`、`latest=null`） |
| `GET /notifications/unread-count` | — | `{unread, categories: {<key>: n}}`（只含 n > 0 的分类） |
| `GET /notifications/:notificationId` | — | `Item` |
| `POST /notifications/read` | `{notificationId}` | `{notificationId, read: true, unread, categoryUnread}` |
| `POST /notifications/unread` | `{notificationId}` | `{notificationId, read: false, unread, categoryUnread}` |
| `POST /notifications/read-batch` | `{ids}`(≤200) | `{appid, userId, requested, updated, ids, unread, categoryUnread}` |
| `POST /notifications/read-all` | `{category?}`（不传或 `all` 即全部；请求体可空） | `{readAll: true, updated, category?, unread, categoryUnread}` |
| `DELETE /notifications/:notificationId` | — | `{appid, userId, deleted, clearedAll: false, unread, categoryUnread}` |
| `POST /notifications/delete-batch` | `{ids}`(≤200) | `{requested, deleted, ids（真正删掉的）, unread, categoryUnread}` |
| `POST /notifications/clear` | `{status?: all\|read\|unread, category?}`（旧的 `type`、`level` 仍可用） | `{appid, userId, deleted, status, category?, clearedAll: true, unread, categoryUnread}` |
| `GET /notifications/preferences` | — | `{categories:[{key, label, push, mutable}]}` |
| `PUT /notifications/preferences` | `{categories: {<key>: bool}}`（只改传了的分类） | 同 GET |

- `categoryUnread` 是 `{<key>: n}`，只含 n > 0 的分类，恒为对象（没有未读时为 `{}`）。
- 单条操作（详情、`read`、`unread`、`DELETE /:id`）遇到不存在、不属于本人或已过期的消息一律 **40455**；
  批量操作（`read-batch`、`delete-batch`）只处理本人未过期的那些，其余静默跳过（看 `updated` / `deleted`）。
- `ids` 去重并去掉非正数；空 40010，去重后超过 200 条 40010。
- 推送偏好表 `notification_preferences(appid, user_id, category, push, updated_at)`，没有行即开启。
  关闭只影响**提醒**（实时事件里 `silent = true`，客户端不弹横幅、不发系统通知），消息照常入库、照常计未读。
  PUT 里关闭 `mutable=false` 的分类 → 40347；把它设为 `true` 视为无操作；未知分类 → 42231；空对象 → 40010。
- 过期消息（`expires_at <= now()`）一律不出现、不计数；读写接口对它们按「不存在」处理。清空（`clear`）时顺带删掉
  命中条件的已过期消息，但不计入返回的 `deleted`。
- 列表的 `status` 只接受 `all` / `unread` / `read`（其它值 40010），`category` 未知 42231。

## 实时事件（`/api/ws`，信封同现有 `realtime.Event`：`{id, type, appid, userId, timestamp, data}`）

| type | data |
|---|---|
| `notification.created` | `{notification: Item, silent: bool, unread, categoryUnread}` |
| `notification.read` | `{ids: [...], all: bool, category?: string, read: true\|false, unread, categoryUnread}` |
| `notification.deleted` | `{ids: [...], cleared: bool, status?: string, category?: string, unread, categoryUnread}` |
| `notification.state.changed` | 旧事件照发：`{reason, refreshRequired: true, stateChangedAt, unread}` |

- 每个新事件后面紧跟一条 `notification.state.changed`（`reason`：`created`、`mark_read`、`mark_unread`、`mark_read_batch`、
  `mark_all_read`、`delete`、`delete_batch`、`clear`、`admin_delete`、`admin_delete_by_filter`）。
- `read`：单条标已读 / 未读时 `ids` 是那一条；`read-batch` 时是**真正从未读变成已读**的那些；`read-all` 时 `ids=[]`、`all=true`
  （按分类时带 `category`）。没有任何变化的批量操作不发事件。
- `deleted`：单条与批量删除时 `ids` 是真正删掉的；`clear` 时 `ids=[]`、`cleared=true`，带 `status` 与（按分类时）`category`；
  管理员在控制台删除消息时，被删消息的主人也会收到 `deleted`（`ids` 为他的那几条）。
- `unread` / `categoryUnread` 是事件发出时的计数；极少数情况下计数查询失败，事件里不带这两个字段，客户端保留原值即可。
- 事件发给**该用户的全部连接**（含触发操作的那台设备，客户端按 id 幂等处理）。多实例经 NATS 已有通路。
- 发布是异步的、不阻塞请求；同时在发的事件任务最多 16 个（`notificationEventConcurrency`），群发上千人时不会占满数据库连接。
- 客户端重连后自行拉 `summary` 与第一页补齐，不做服务端补发。

## 缓存

- 列表缓存键含全部筛选条件（`status|category|type|level` + 页码 + 条数），换分类不会读到别的分类。
- 分类未读计数单独缓存（键挂在列表缓存前缀下，一次失效）；`unread-count` 读它。
- 两者寿命都是 60 秒，且不超过最早一条（未读 / 筛选范围内）消息的过期时间——消息一过期，计数与列表立刻跟着变。
- 任何写操作（含 Deliver）先失效缓存；写操作返回的计数直接查库，不回填缓存。

## 管理端

- 群发 `POST /api/admin/apps/:appkey/notifications/bulk` 新增可选字段：`category`（默认 `announcement`）、
  `action`（`{type, target, label?}`）、`image`、`expiresAt`（RFC 3339），校验同 Deliver（42231 / 42230 / 40010）。
  逐人 `Deliver`，同时落库 16 人；逐人的 `notification.onCreated` 插件钩子不发，整批发一次 `notification.onSent`。
- 列表 `GET /api/admin/apps/:appkey/notifications`、导出、按条件删除都加 `category` 筛选；列表项新增
  `category`、`categoryLabel`、`action`、`image`、`expiresAt`，`status` 的 `delivered` 也改叫 `unread`；
  CSV 导出新增 `category`、`expires_at` 两列。管理端列表**包含**已过期的消息。
- 控制台：
  - 用户列表批量操作「发通知」（`components/users/app-users/bulk-notify-dialog.tsx`）：标题、正文、分类（默认公告活动）、级别
    （普通 / 成功 / 提醒 / 重要）、跳转（无 / 应用内页面〔路由键下拉，中文名〕/ 链接）、按钮文案、配图地址、过期时间（1 / 7 / 30 天快捷）。
  - 应用详情审计区块里的「通知」表（`components/apps/app-audit-panel.tsx`）：分类筛选、分页，列出收件人、分类、级别、跳转、
    已读状态、过期时间（已过期标出）。
  - 分类、路由键、级别的中文名在 `lib/user-notification-meta.ts`，与后端目录一一对应。

## 错误码

| 码 | HTTP | 名称 | 场景 |
|---|---|---|---|
| 40455 | 404 | `NOTIFICATION_NOT_FOUND` 消息不存在 | 详情、标已读 / 未读、删除单条时不存在、不属于本人或已过期 |
| 40347 | 403 | `NOTIFICATION_PUSH_LOCKED` 该类消息不能关闭提醒 | 偏好里关闭账号安全类 |
| 42230 | 422 | `NOTIFICATION_ACTION_INVALID` 动作不合法 | 动作类型不是 route / url、路由键不在白名单、链接不是 https、按钮文案超长 |
| 42231 | 422 | `NOTIFICATION_CATEGORY_INVALID` 分类不存在 | 写入、筛选、read-all、clear、偏好里出现未知分类 |
| 40010 | 400 | 参数无效 | 标题为空或超长、级别未知、配图不是 https、过期时间不在未来一年内、ids 为空或超过 200、status 未知 |
| 40000 | 400 | 请求格式错误 | 请求体绑定失败、路径里的 ID 不是数字 |

四个新码都登记在网关错误目录（`auth_protocol_catalog.go` 的 `gatewayErrors`）。

## 迁移 000108_notification_center

- `notifications` 加列：`category VARCHAR(32) NOT NULL DEFAULT 'system'`、`action JSONB NULL`、`image TEXT NULL`、`expires_at TIMESTAMPTZ NULL`；
  索引 `idx_notifications_app_user_category_time (appid, user_id, category, created_at DESC)`。
- 新表 `notification_preferences`（主键 `(appid, user_id, category)`，随应用与用户级联删除）。
- 回填：`category` 先加成可空列，只回填仍为 `NULL` 的行，再补默认值与 `NOT NULL`——迁移器每次启动重跑也不会覆盖写入时显式指定的分类。
  按上面的映射表回填，另外认出两种历史写法：`metadata.module = 'account_deletion'`（旧的账号注销消息用的是 `security`）→ membership，
  `metadata.scene in ('auto_sign', 'auto_sign_test')`（旧的自动签到消息用的是 `system`）→ reward。

## 测试

- `internal/domain/notification/category_test.go`：分类目录与顺序、type 映射、偏好判定、级别归一、动作校验、可空字段输出 null。
- `internal/service/notification_writers_test.go`：只有 NotificationService 调 `CreateUserNotification`；全部写入点的 type 都已登记。
- `internal/service/notify_provider_local_test.go`：工单站内信带 `tickets` 动作。
- `TestNotificationCenterIntegration`（真实 Postgres + miniredis + 假的实时发布器）：回填与重跑、Deliver 校验与完整 Item、
  created 事件内容与 silent、偏好、分类 / 状态过滤与过期排除、缓存键含分类、按分类未读、summary、详情与越权、标已读 / 未读、
  批量已读、按分类全部已读、批量删除、按分类清空、管理员群发与列表。

```bash
# 只能指向一次性的测试库：openTestDatabase 会 DROP SCHEMA public
AEGIS_TEST_PG_DSN=postgres://aegis:<密码>@127.0.0.1:15432/aegis_notification_test?sslmode=disable \
  go test ./internal/service -run TestNotificationCenterIntegration
```

## 实现说明（与最初契约的差异）

- **两个写入点换了 type**：账号注销由 `security` 改为 `account_deletion`（契约把账号注销归入会员与订单，原 type 会落进账号安全），
  自动签到由 `system` 改为 `auto_sign`（契约把它归入积分与奖励）。按 type 筛选的老客户端会看到这两类消息的 type 变了。
  账号注销的动作给的是 `security`（注销入口在账号安全页），不是 `membership`。
- **单条写操作的 404**：契约只写了详情返回「消息不存在」；按「读写接口对过期消息按不存在处理」，`read`、`unread`、`DELETE /:id`
  遇到不存在 / 别人的 / 过期的消息也返回 40455（旧实现静默成功）。
- **`clear` 的 `status`** 除契约的 `all` / `read` 外仍接受 `unread`（旧接口就支持）。
- **`unread-count` 的 `categories`** 与 `categoryUnread` 同样只含 n > 0 的分类。
- **`readAt` 恒在**（旧实现为空时省略），`status` 不再出现 `delivered`。
- **级别**：契约只列了四个值；旧控制台发的是 `critical`，因此写入时归一而不是报错。
- **管理员删除也推 `notification.deleted`**（契约没写，多端一致需要）。
- **`created` 插件钩子**：契约要求 Deliver 发钩子；群发时为避免一次群发触发上千次钩子，逐人钩子不发，保留原有的整批 `notification.onSent`。
- 配图与过期时间不合法用 40010（参数类），没有单独的码。
