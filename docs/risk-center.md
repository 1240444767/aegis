# 风控中心

一次评估的顺序固定（`internal/service/risk_engine.go` 的 `decide`，线上、模拟、重放共用）：

1. **装配环境**（`risk_env_populate.go`）：请求、客户端（UA）、频率（GCRA 限流 + HyperLogLog 扩散）、场景小时频次、设备档案、网络情报与 GeoIP、账号（注册时长、近 7 天高危次数、上一次归属地）。登录只带登录名时先认出用户，账号维度的计数与已登录场景归到同一主体。
2. **名单**（`risk_lists.go`，表 `risk_lists`）：黑名单直接拦、白名单直接放（黑白同时命中黑名单优先）、观察名单写进 `list_watch` 变量交给「观察名单命中」规则加分。十种名单类型：IP、网段、设备、账号、用户 ID、邮箱域名、手机号段、ASN / 运营商、国家、UA 关键词；可限定应用与场景、可设到期。
3. **规则**：只跑对该应用（`app_id` 为空 = 全平台）与该场景（`*` = 全部场景）生效的规则。每种条件归属一个**维度**（网络 / 设备 / 账号 / 行为 / 地理 / 客户端 / 名单 / 自定义），结果按维度汇总得分。`observe` 模式命中只留痕、不计分；`decision` 是命中即强制的处置。
4. **处置**：分数策略按「应用 + 场景 → 应用 + 全部 → 全平台 + 场景 → 全平台 + 全部」找第一层配了策略的；与规则强制处置取更严的一个。验证码：请求已通过图形验证码则放行；场景或应用接不了验证码则转人工复核。
5. **执行**：`ban` 当场落到对象上（`risk_enforce.go` 的 `enforce`）——账号走 `AccountBanService`（全部访问封禁）、设备与 IP 进黑名单、`firewall` 走 `IPBanService`（来源 `risk`）。每一项结果写进评估记录的 `enforcement`。其余留痕异步写库。

规则 / 策略 / 名单整体装进内存快照（15 秒有效，本实例写操作立即作废），评估热路径不查库。

## 业务接入

业务服务只调 `RiskService.Guard`（或嵌入 `riskHook` 后调 `riskGuard`）：拦截 / 封禁 → 403（默认 40398，可按业务指定），需要验证码 → 40015，其余放行；风控自身出错放行并记日志。

| 场景 | 接入点 |
|---|---|
| login | 密码登录（网关与 OAuth2 登录页共用 `authenticatePassword`）、卡密登录、管理员登录 |
| register / password_reset | `RegisterWithPassword` / `StartPasswordRecovery` |
| verify_code | 网关与旧接口的短信验证码发送（`guardVerifyCode`） |
| payment | `PaymentService.CreateOrder`（落单前，`extra.amount` 为金额） |
| invite / lottery / signin / cardkey / points_redeem / ad_reward | 各业务服务入口；自动签到不评估 |

验证码加验：客户端收到 40015 后取图形验证码、带 `captchaId` 重试。接入层校验通过后用 `service.WithRiskCaptchaPassed` 标记上下文，评估据此放行；应用没要求验证码时客户端带来的验证码也会被校验（`verifyRiskStepUpCaptcha`）。

## 复核与处置

- 复核驳回：按所选对象处置（默认设备 + IP）；通过：可加白、可撤销该记录触发的自动封禁（名单、账号封禁、防火墙封禁都按 `enforcement.refId` 撤回）。
- 人工处置 `POST /risk/dispose`：黑 / 白 / 观察 × 账号 / 设备 / IP / 防火墙 × 时长。账号与用户 ID 名单按应用作用域写，设备与 IP 是全平台的。
- 设备与 IP 档案上的标签与全平台名单双向同步：标签给人看，判定只认名单。

## 规则模板

`internal/domain/security/risk_templates.go`：基础防护 / 账号安全 / 营销反作弊 / 支付风控。装完即普通规则，`template_key` 只用来重复安装时跳过；可选观察模式安装、附带处置策略（区间冲突的跳过）。`TestRiskTemplatesAreValid` 保证模板里每条都过得了保存校验。

## 测试

```bash
go test ./internal/service -run 'Risk|Decide|ResolvePolicy|AccountShape|ValidateList'
# 端到端（真实 Postgres，测试会 DROP SCHEMA，别指向业务库）
docker exec aegis-postgres psql -U aegis -d aegis -c "CREATE DATABASE aegis_risk_test"
AEGIS_TEST_PG_DSN=<.env 的 DSN 把库名换成 aegis_risk_test> go test ./internal/service -run TestRiskCenterIntegration -count=1
```
