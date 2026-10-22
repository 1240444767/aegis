# 崩溃报告

> 面包屑：[Aegis](../CLAUDE.md) › 崩溃报告

客户端崩溃后在崩溃屏上展示报告，用户点「发送此报告」才上报这**一份**。服务端只是收件箱：
免登录可用（崩溃往往发生在登录之前，或恰恰是登录本身崩了），带令牌时挂到账号上；
管理员在控制台按应用查看、标记已处理、删除。

## 隐私：只在用户逐份同意时上报

- **逐份同意。** 只有用户在崩溃屏上明确点了发送，客户端才调用上报接口。不做后台静默上报，
  也不把一次同意扩展成「以后的崩溃都自动发」。服务端没有任何开关能改变这一点 ——
  是否上报完全由客户端在用户操作后决定。
- **报告是用户看得见的那份。** 上报的 `report` 就是崩溃屏上展示给用户的纯文本，
  客户端不应在发送时另外夹带用户没看到的内容。
- **服务端另记的只有请求本身带来的东西**：客户端 IP（`pkg/clientip` 判定的真实地址）、
  由本地 GeoIP 库查到的国家 / 地区 / 城市（只查本地缓存与 mmdb，**不**触发远程 IP 接口）、
  User-Agent 与 `X-Device-*` 请求头里的设备信息。用于排查（同一机型、同一地区集中崩溃）与限频。
- **账号只来自令牌。** 客户端不能在请求体里指定把报告挂到谁名下；令牌无效按未登录处理。
  账号注销后报告保留、只解除归属（`ON DELETE SET NULL`）。
- 管理员可以随时删除单条或批量删除（真删，不进回收站）。

## 接口（App Protocol v1）

| 目录 key | 方法 | 路径 | 登录 | 说明 |
|---|---|---|---|---|
| `crashReport` | POST | `/api/v1/apps/{appKey}/crash-reports` | 可选 | 上报一份崩溃报告 |

与 `/ads/splash/events` 同一层（`OptionalAuth` + `AppGatewayTokenScope`）：带令牌时记 `user_id`，
令牌属于别的应用则拒绝，未登录照常收。三档安全等级（standard / signed / sealed）同样适用。

请求体（JSON）：

| 字段 | 必填 | 规则 |
|---|:--:|---|
| `reportId` | 是 | 客户端记录编号，8–64 位 `[A-Za-z0-9_-]`，同一应用内唯一 |
| `kind` | 是 | `java` / `native` / `anr` / `exit`（大小写不敏感，落库为小写） |
| `title` | 是 | 超过 512 字截断 |
| `report` | 是 | 完整纯文本报告，UTF-8 不超过 256 KB，超出返回 `40000` |
| `occurredAt` | 是 | RFC 3339；比服务器时间晚 5 分钟以上的按服务器时间记，早的不动（往往是下次启动才发） |
| `detail` | | 超过 4096 字截断 |
| `appVersion` `versionCode` `osVersion` `model` `abi` `processName` `source` | | `versionCode` 为非负整数；其余为展示字段，超长截断 |
| `deviceId` | | 缺省取 `X-Device-Id` 头 |

展示字段一律**截断而不是拒绝**：用户已经点了发送，因为某个字段长了一点就整份丢掉没有意义。
正文里的 NUL 与非法 UTF-8 序列（原生崩溃的寄存器转储里两样都可能有）在落库前去掉 / 替换。

响应 `data`：

```json
{ "id": "CR20261022K7Q2M9XA", "duplicate": false }
```

`id` 是对外编号（`CR` + 上报日期 + 8 位随机串），用户反馈时报这个号，控制台关键字能直接搜到。
同一 `reportId` 再报一次不新建，返回原来那一条的编号并 `duplicate: true`（HTTP 200）。

### 错误码

| 情形 | HTTP | code |
|---|:--:|:--:|
| 参数错误（缺字段、`reportId` 格式、未知 `kind`、`occurredAt` 不是 RFC 3339、`report` 超过 256 KB） | 400 | `40000` |
| 请求体超过 1 MB（在读取阶段就截停，不会整体读进内存） | 413 | `40000` |
| 限频 | 429 | `42900` |

参数错误与限频沿用全站通用码，客户端不必为这一条接口另记一套。`42900` 已登记进 `/config` 的错误目录（`RATE_LIMITED`）。

### 限频

同一设备（`deviceId`）、同一 IP **各自**每小时最多 20 份，任一维度超出即 `42900`。

- 顺序是「校验 → 去重 → 限频 → 落库」：已经收到的那一份重传先按重复返回，**不吃额度**。
  客户端没收到响应时会重发，那不该把它推到 429 上去。
- 计数直接数库（`created_at` 最近一小时），走两条部分索引；与意见反馈的限频同一取向，
  多实例部署下也准确，没有计数器与真实数据对不上的问题。删除报告即释放对应额度。
- 计数失败时放行：限频是保护措施，不该让它的故障丢掉用户同意发送的报告。
- 叠在防火墙的全局 IP 限流之上。

客户端收到 `42900` 时把报告留在本机，过一段时间（如下次启动）再发；同一 `reportId` 重发是安全的。

### 请求体大小

`report` 的上限是 256 KB，但它在 JSON 里要转义（换行、引号、制表符各多一字节），加上其余字段，
请求体上限按 4 倍留余量定为 1 MB。handler 在 `bind` 之前用 `http.MaxBytesReader` 读，超过上限读到 1 MB 就停。
signed / sealed 档的外层包装另受网关 `MaxRequestBytes`（8 MB）约束。

## 管理端

权限：查看（列表、摘要、含正文的详情）要 `app:read`，标记处理状态与删除要 `app:write`，均为应用作用域。
在 `internal/authz/routes.go` 里显式写出，不落进应用级兜底规则 —— 报告里带设备标识与 IP，权限改动应当一眼看见。

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/admin/apps/{appKey}/crash-reports` | 分页列表，**不带正文**。筛选：`kind` / `appVersion` / `keyword`（标题、详情、两种编号，`_` `%` 按字面匹配）/ `resolved` / `userId` / `deviceId` / `start` / `end` / `page` / `limit` |
| GET | `/api/admin/apps/{appKey}/crash-reports/facets` | 总数、未处理、近 24 小时，各类型数量与出现过的版本（按数量取前 50），供筛选下拉 |
| GET | `/api/admin/apps/{appKey}/crash-reports/{crashId}` | 详情：全部字段、报告正文、上报账号与其**对外用户 ID**（`userPublicId`） |
| POST | `/api/admin/apps/{appKey}/crash-reports/resolve` | `{ids, resolved}` 标记或取消「已处理」，返回 `{updated}`；已是目标状态的不重写处理人 |
| DELETE | `/api/admin/apps/{appKey}/crash-reports/{crashId}` | 删除单条，不存在返回 `40497` |
| POST | `/api/admin/apps/{appKey}/crash-reports/delete` | `{ids}` 批量删除，返回 `{deleted}` |

批量操作一次最多 200 条。管理端照全站惯例以主键寻址（`crashId` 即列表里的 `id`），对外编号是 `publicId`。

控制台入口：`/apps/{appKey}?tab=crash-reports`（概览分组下的「崩溃报告」），单页列表 + 筛选 + 详情抽屉，
正文等宽显示，可复制、可下载为 `.txt`。

## 存储

| 表 | 说明 |
|---|---|
| `app_crash_reports` | 一份报告一行；`UNIQUE (appid, report_id)` 去重，`UNIQUE (public_id)` 对外编号；`kind` 有 CHECK 约束 |

索引：`(appid, occurred_at DESC, id DESC)` 列表；`(appid, app_version)` 版本筛选与摘要；
`(appid, device_id, created_at DESC) WHERE device_id <> ''` 与 `(appid, client_ip, created_at DESC) WHERE client_ip <> ''` 限频。

迁移 `000121_crash_reports`。

## 代码位置

| 层 | 文件 |
|---|---|
| 领域 | `internal/domain/crashreport/types.go`（类型、上限、校验与截断） |
| 仓储 | `internal/repository/postgres/crash_report_repository.go` |
| 服务 | `internal/service/crash_report_service.go`（单元测试 `crash_report_service_test.go`，集成测试 `crash_report_integration_test.go`） |
| 传输 | `internal/transport/http/crash_report_handlers.go` |
| SDK | `sdk/kotlin/.../AegisApi.kt` 的 `submitCrashReport` |
| 控制台 | `aegis-console/src/components/apps/crash-reports/`、`src/lib/api/crash-reports.ts`、`src/lib/crash-report-hooks.ts` |
