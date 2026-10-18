# 存储中心

> 面包屑：[Aegis](../CLAUDE.md) › docs/storage

所有上传（用户上传、头像、云存储、工单附件、横幅、发布包）都经 `StorageService.UploadForApp`
写到一个**存储配置**或一个**存储池**；读取永远按对象记录里的 `config_id` 去原处读。
控制台入口：`/storage`。

## 提供商

| provider | SDK / 协议 | 直传 | 删除 | CORS | 备注 |
|---|---|---|---|---|---|
| `r2` | aws-sdk-go-v2（S3 API） | ✓ | ✓ | ✓ | 填账户 ID 即可，端点自动拼；可选 API 令牌读真实用量 |
| `s3` | aws-sdk-go-v2 | ✓ | ✓ | ✓ | 任意 S3 兼容端点 |
| `minio` | minio-go | ✓ | ✓ | | |
| `aliyun_oss` / `tencent_cos` / `qiniu_kodo` | 各家官方 SDK | OSS、COS ✓ | ✓ | | |
| `webdav` / `onedrive` / `dropbox` / `google_drive` / `azure_blob` | | | ✓ | | |
| `local` | 本地磁盘 | | ✓ | | 只能是平台级；放在容器里**必须挂持久卷** |

S3 与 R2 共用 `s3StorageProvider` 内核（`storage_provider_impl.go`），差异由构造时注入：端点解析、
SDK 选项、HTTP 客户端。两者都在 `StorageService` 上常驻，按配置指纹缓存 SDK 客户端。

### Cloudflare R2

- 端点：`https://<account>.r2.cloudflarestorage.com`，欧盟 / FedRAMP 管辖区是 `<account>.eu.` / `<account>.fedramp.`；
  地区固定 `auto`，路径风格访问。走出海网关（`newStorageOutboundTransport`）。
- **校验和**：aws-sdk-go-v2 s3 ≥ 1.73 默认给请求附 CRC32 尾部（`aws-chunked` + `x-amz-trailer`），
  分片上传器 transfermanager 还有自己的一份设置。R2 两处都改成 `WhenRequired`，见
  `TestR2ProviderSpeaksS3ToAccountEndpoint`（用记录型 Transport 断言请求形态，不出网）。
- **公开访问**：S3 端点只认签名请求。`base_url` 填自定义域名或 `r2.dev` 子域即可发直链；
  不填时公开链接给预签名 GET（≤ 7 天），永久地址给不出来 —— 所以「公开 + 不代理 + 无域名」保存时拒绝（42284）。
- **真实用量**：可选 `api_token`（Account Analytics 读权限），后台每 5 分钟经 GraphQL
  `r2StorageAdaptiveGroups` 取最新的 `payloadSize + metadataSize`；受管辖区限制的桶名带 `eu_` 前缀。
  与索引用量取大者（分析按小时采样会滞后，索引看不到不经 Aegis 写入的对象）。
- **CORS**：浏览器直传要求桶允许控制台来源 PUT，控制台「一键配置 CORS」调 `PutBucketCors`。

## 写入目标解析（`resolveWriteTarget`）

1. 指定了名称：先按存储配置名（应用级优先、平台级其次），找不到再按**池名**；
2. 没指定：应用有自己的存储配置就用应用的；否则走**默认池**；没有默认池再用平台默认配置。

云存储配置页的「存储」下拉里同时列出配置与池（`provider = "pool"`）。

## 存储池

一个池 = 多个**平台级**存储配置 + 选择策略。成员属性：优先级（小的先用）、权重、容量上限（0 不限）、
角色（`active` / `standby` 备用，所有 active 都不可写时才用 / `readonly` 只读排空）。

| 策略 | 行为 |
|---|---|
| `priority` | 按优先级写满一个再用下一个 |
| `round_robin` | 在可写成员间轮流 |
| `weighted` | 按权重无放回随机（Efraimidis–Spirakis） |
| `least_used` | 占用比例低的先写（不限容量按 1 PiB 折算） |

一次写入（`uploadToPool`）：

1. 内容做成可重读的（能 Seek 的记起点，否则落临时文件，不进内存）；
2. 按策略排出成员顺序，跳过停用、只读、退避中、放不下的成员；
3. 依次尝试，至多 `max_attempts` 个。**只有存储后端的错误**（`storageBackendError`：网络、鉴权、对方 5xx）
   才换下一个成员；规则、参数、治理类错误换谁都一样，直接返回；
4. 成功：就地累加用量，越过预警线 / 写满记事件；有成员失败过则记一条「切换」事件；
5. 一个都没试成：全因容量 → **50780 / 507**；否则 **50383 / 503**。

### 退避

健康记录按存储配置记（`storagePoolEngine`，进程内）。连续失败第 n 次冷却 `10s·2^(n-1)`，封顶 15 分钟，
±20% 抖动。冷却期满即半开放行一次；后台每 30 秒对冷却期满的成员做一次 `HealthCheck`，成功即恢复，
不必等真实写入去撞。控制台可以手动「恢复写入」清除退避。

### 用量与预警

容量按 `storage_objects` 汇总（含回收站里的，它们在清除前仍占空间），60 秒缓存，写入后就地累加。
后台每 5 分钟重算一次并检查各池成员：超过 `warn_percent` 记 `near_full`，写满记 `full`。
同一成员同一种事件 10 分钟内只记一条；事件保留 30 天。

## 读取：转向与池内回退（`openStoredObject`）

1. 配置启用时先在原处读；
2. 找不到（或配置已停用）且设了 `redirect_config_id`，去转向的配置读（至多 4 跳）；
3. 还找不到，且原配置在开了 `read_fallback` 的池里，去池里其他成员读同一个键。

只有「对象不存在」才往下找（`isStorageObjectNotFound`：本地 / 网盘的 404、`os.ErrNotExist`、
S3 的 `NoSuchKey` / `NotFound`、OSS / COS / Azure 各自的不存在错误）；网络、鉴权故障原样返回。
`DeleteObject` 同样跟着转向删目标配置里的对象与索引，免得修订裁剪在目标存储里留孤儿。

## 迁移

把一个配置里**已索引**的对象逐个复制到另一个平台级配置，键不变（`storage_migration.go`）。

- 复制成功：索引行改记到目标配置（文件管理、用量跟着走）；可选删除源对象。
- 完成后设读取转向：散落在各处的 `storage://` 引用、云存储修订、头像记录都不用改。
  **删除源对象必须同时开启转向**，否则那些引用就读不到了。
- 断点：按 `storage_objects.id` 游标推进，进程重启从游标继续；并发 4，单对象 30 分钟超时。
  整批全失败（多半是目标不可用）就停在 `failed`，可「继续」。复制失败的对象索引留在源配置，
  再建一个迁移任务就只会扫到它们。

## 上传规则（`storage_rules.go`）

规则按 `config_id` / `app_id` 叠加（为空即全部），可用 `uploaderTypes` 限定只管 `user` 或 `admin`
（免得「只许图片」把云存储的 JSON、发布包一起拦掉）。在 `uploadWithConfig` 里、魔数识别之后执行。

| 类型 | rule_data | 拦截 |
|---|---|---|
| `upload_limit` | `{maxBytes}` | 41300 / 413 |
| `file_type` | `{mode: allow\|deny, extensions, mimeTypes}`（`image/*` 通配） | 41500 / 415 |
| `path_pattern` | `{mode, patterns}`（`*` 不跨目录，`**` 跨任意层） | 42278 / 422 |
| `quota` | `{maxBytes, maxFiles}`（按应用，可再限定存储） | 41301 / 413 |

## CDN 与防盗链（`storage_cdn.go`）

- 公开直链：配了 CDN 域名就用 `协议://域名/对象键`；开了签名附 A 型鉴权 `auth_key`
  （阿里云 / 腾讯云等通用）。永久地址不签名（签了就不永久），改发代理地址。
- 代理下载：IP 白名单（IP / CIDR）、Referer 黑白名单（域名、`*.域名`、完整地址；**空 Referer 放行**）、
  缓存时长覆盖 `Cache-Control`。设置 30 秒缓存，保存即失效。

## 文件管理

「永久删除」与回收站清理会**真正删除存储里的对象**（经 `StorageService.DeleteObject`）；
删除失败的对象索引保留在回收站，可再试，不会变成桶里没人知道的孤儿。用量快照每天采集一次，
控制台也能手动采集。

## 接口

平台级，前缀 `/api/admin/platform/storage`，读要 `platform:storage:read`，写要 `platform:storage:write`：

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/overview` | 全部平台级配置（密钥打码）+ 用量 + 健康 + 能力 + 所在池；全部池；最近迁移；默认写入目标 |
| GET | `/events?poolId=&configId=&level=&limit=` | 运行事件 |
| POST | `/usage/collect` | 立即采集用量快照 |
| GET / POST | `/pools` | 列表 / 新建 |
| GET / PUT / DELETE | `/pools/:poolId` | 详情 / 整体替换（含成员）/ 删除 |
| POST | `/pools/:poolId/simulate` | `{size}` → 成员顺序与每个成员会不会被尝试 |
| POST | `/configs/:configId/probe` | 深度检测：连通 → 写入探测对象 → 读回比对 → 删除 |
| POST | `/configs/:configId/reset-health` | 清除退避 |
| GET / PUT | `/configs/:configId/cors` | 读 / 写桶 CORS（`{origins}`） |
| DELETE | `/configs/:configId/redirect` | 撤销读取转向 |
| GET / POST | `/migrations` | 列表 / 新建（`{sourceConfigId, targetConfigId, deleteSource, setRedirect}`） |
| GET | `/migrations/:migrationId` | 进度 |
| POST | `/migrations/:migrationId/{pause,resume,cancel}` | 控制 |

## 测试

- 单元：`go test ./internal/service -run 'TestBackoff|TestPlan|TestR2|TestDecodeR2|TestCDN|TestNormalizeAndEnforce|TestRewindable'`
- 集成（真实 PG + 三个本地存储，一个故意坏掉）：
  `AEGIS_TEST_PG_DSN=postgres://postgres@127.0.0.1:55432/postgres go test ./internal/service -run TestStoragePoolIntegration`
  覆盖故障切换与退避、写满换成员、507、上传规则、迁移 + 转向（源停用后旧永久地址照样可读）、永久删除真删文件。
