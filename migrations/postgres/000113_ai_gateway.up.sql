-- +migrate Up
-- ── AI 中转网关：面向应用用户的计费调用 ──
--
-- 应用的终端用户拿自己的 API 密钥（或登录会话）调 OpenAI 兼容接口，
-- 费用按模型单价 × 倍率计算，从免费额度、会员额度、赠送额度、积分、余额里依次扣。
--
-- 金额一律以「微点」整数存（1 点 = 1 积分定价 = 1,000,000 微点），
-- 不用浮点：一次几十 token 的调用只值零点零几个积分，四舍五入到积分会让
-- 小请求要么白用、要么多收一倍。积分与余额本身仍按各自的最小单位（积分、分）记账，
-- 不足一个单位的零头记在 ai_gateway_accounts 的 carry 上，下次凑整再扣。
--
-- 扣费分两步：调用前按估算**暂扣**（积分从 users.integral 移出、余额移进 user_wallets.frozen、
-- 额度记 reserved），调用成功后按实际用量**结算**并只写一条消费流水；调用失败整笔放回，
-- 积分与余额的流水里什么都不留。暂扣行由后台循环按 expires_at 回收，进程崩溃也不会吞钱。

-- 应用级网关设置。字段多且只在 Go 侧校验，整体放一袋 JSONB；缺省值以 Go 侧为准。
CREATE TABLE IF NOT EXISTS ai_gateway_settings (
    appid      BIGINT PRIMARY KEY REFERENCES apps(id) ON DELETE CASCADE,
    settings   JSONB       NOT NULL DEFAULT '{}'::jsonb,
    updated_by VARCHAR(64),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 模型与价格。**只有登记在这张表里的模型能被调用**：没定价的模型一律 404，
-- 不存在「忘了配价格于是免费用」的路径。价格单位是「点」：
-- token 类按每百万 token，图片按每张，语音合成按每百万字符，转写按每分钟。
CREATE TABLE IF NOT EXISTS ai_gateway_models (
    id                BIGSERIAL PRIMARY KEY,
    appid             BIGINT        NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    -- model 是客户端填的名字；upstream_model 是发给上游的名字，空串表示同名。
    model             VARCHAR(128)  NOT NULL,
    upstream_model    VARCHAR(128)  NOT NULL DEFAULT '',
    kind              VARCHAR(16)   NOT NULL,
    display_name      VARCHAR(128)  NOT NULL DEFAULT '',
    description       TEXT          NOT NULL DEFAULT '',
    -- config_id 非 0 时钉死一条 AI 通道（不回退），0 表示按应用的通道链路走。
    config_id         BIGINT        NOT NULL DEFAULT 0,
    enabled           BOOLEAN       NOT NULL DEFAULT TRUE,
    vip_only          BOOLEAN       NOT NULL DEFAULT FALSE,
    -- 全部价格为 0 的模型必须显式标记免费，否则保存时拒绝。
    free              BOOLEAN       NOT NULL DEFAULT FALSE,
    input_price       NUMERIC(20,6) NOT NULL DEFAULT 0,
    output_price      NUMERIC(20,6) NOT NULL DEFAULT 0,
    request_price     NUMERIC(20,6) NOT NULL DEFAULT 0,
    image_price       NUMERIC(20,6) NOT NULL DEFAULT 0,
    -- 按尺寸 / 质量覆盖单张价格：键为 "1024x1024"、"hd" 或 "hd:1024x1792"。
    image_prices      JSONB         NOT NULL DEFAULT '{}'::jsonb,
    char_price        NUMERIC(20,6) NOT NULL DEFAULT 0,
    minute_price      NUMERIC(20,6) NOT NULL DEFAULT 0,
    max_output_tokens INT           NOT NULL DEFAULT 0,
    sort_order        INT           NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_ai_gateway_models_prices CHECK (
        input_price >= 0 AND output_price >= 0 AND request_price >= 0 AND
        image_price >= 0 AND char_price >= 0 AND minute_price >= 0 AND max_output_tokens >= 0)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_ai_gateway_models_app_model
    ON ai_gateway_models (appid, model);

-- 用户 API 密钥。只存 sha256，明文只在创建时返回一次。
-- 删除是软删（status = revoked）：调用记录里的 key_id 仍能对上，散列也不会被复用。
CREATE TABLE IF NOT EXISTS ai_gateway_keys (
    id           BIGSERIAL PRIMARY KEY,
    appid        BIGINT      NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    user_id      BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         VARCHAR(64) NOT NULL DEFAULT '',
    key_hash     CHAR(64)    NOT NULL,
    key_hint     VARCHAR(32) NOT NULL,
    status       VARCHAR(16) NOT NULL DEFAULT 'active',
    -- 可用模型白名单，空数组表示不限。
    models       TEXT[]      NOT NULL DEFAULT '{}',
    -- 累计消费上限（微点），0 表示不限；reserved 是这把密钥名下正在暂扣的部分。
    spend_limit  BIGINT      NOT NULL DEFAULT 0,
    spent        BIGINT      NOT NULL DEFAULT 0,
    reserved     BIGINT      NOT NULL DEFAULT 0,
    expires_at   TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    last_used_ip VARCHAR(64),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_ai_gateway_keys_status CHECK (status IN ('active', 'disabled', 'revoked')),
    CONSTRAINT ck_ai_gateway_keys_amounts CHECK (spend_limit >= 0 AND spent >= 0 AND reserved >= 0)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_ai_gateway_keys_hash ON ai_gateway_keys (key_hash);
CREATE INDEX IF NOT EXISTS idx_ai_gateway_keys_user ON ai_gateway_keys (appid, user_id, id DESC);

-- 每个用户一行：赠送额度余额、积分与余额的不足一单位零头、累计消费。
-- 网关的每次暂扣与结算都先锁这一行（锁序 users → ai_gateway_accounts → user_wallets）。
CREATE TABLE IF NOT EXISTS ai_gateway_accounts (
    user_id        BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    appid          BIGINT      NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    credit         BIGINT      NOT NULL DEFAULT 0,
    credit_frozen  BIGINT      NOT NULL DEFAULT 0,
    -- 积分零头（微点）与余额零头（微分），都小于一个单位。
    integral_carry BIGINT      NOT NULL DEFAULT 0,
    wallet_carry   BIGINT      NOT NULL DEFAULT 0,
    total_spent    BIGINT      NOT NULL DEFAULT 0,
    total_requests BIGINT      NOT NULL DEFAULT 0,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_ai_gateway_accounts_amounts CHECK (
        credit >= 0 AND credit_frozen >= 0 AND total_spent >= 0 AND total_requests >= 0 AND
        integral_carry >= 0 AND integral_carry < 1000000 AND
        wallet_carry >= 0 AND wallet_carry < 1000000)
);

CREATE INDEX IF NOT EXISTS idx_ai_gateway_accounts_app ON ai_gateway_accounts (appid);

-- 赠送额度流水：看广告、管理员发放、退款进来，调用消费出去。暂扣不记流水，只记结算。
CREATE TABLE IF NOT EXISTS ai_gateway_credit_ledger (
    id            BIGSERIAL PRIMARY KEY,
    appid         BIGINT       NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    user_id       BIGINT       NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind          VARCHAR(16)  NOT NULL,
    amount        BIGINT       NOT NULL,
    balance_after BIGINT       NOT NULL,
    ref_type      VARCHAR(32)  NOT NULL DEFAULT '',
    ref_id        VARCHAR(64)  NOT NULL DEFAULT '',
    remark        VARCHAR(200) NOT NULL DEFAULT '',
    operator      VARCHAR(64),
    -- 管理员调整的请求编号：同一编号只生效一次。
    request_id    VARCHAR(64),
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_ai_gateway_credit_ledger_kind CHECK (kind IN ('grant', 'consume', 'refund', 'adjust'))
);

CREATE INDEX IF NOT EXISTS idx_ai_gateway_credit_ledger_user ON ai_gateway_credit_ledger (user_id, id DESC);
CREATE UNIQUE INDEX IF NOT EXISTS uq_ai_gateway_credit_ledger_request
    ON ai_gateway_credit_ledger (appid, request_id) WHERE request_id IS NOT NULL;

-- 周期额度（每日免费、会员每日、会员每月）与每日消费上限的用量。
-- period_key 按应用设置的时区取日历日或月；换周期即换一行，旧行留作统计。
CREATE TABLE IF NOT EXISTS ai_gateway_quota_usage (
    user_id    BIGINT      NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    appid      BIGINT      NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    source     VARCHAR(16) NOT NULL,
    period_key VARCHAR(16) NOT NULL,
    used       BIGINT      NOT NULL DEFAULT 0,
    reserved   BIGINT      NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (user_id, source, period_key),
    CONSTRAINT ck_ai_gateway_quota_usage_amounts CHECK (used >= 0 AND reserved >= 0)
);

-- 调用记录：一次请求一行，从暂扣到结算都在这一行上推进。
-- price 是暂扣那一刻的价格快照：调用进行中管理员改价，按下单时的价格结算。
CREATE TABLE IF NOT EXISTS ai_gateway_requests (
    id               BIGSERIAL PRIMARY KEY,
    request_no       VARCHAR(40)   NOT NULL,
    appid            BIGINT        NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    user_id          BIGINT        NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    key_id           BIGINT        NOT NULL DEFAULT 0,
    key_hint         VARCHAR(32)   NOT NULL DEFAULT '',
    auth_kind        VARCHAR(16)   NOT NULL,
    endpoint         VARCHAR(32)   NOT NULL,
    model            VARCHAR(128)  NOT NULL,
    upstream_model   VARCHAR(128)  NOT NULL DEFAULT '',
    config_id        BIGINT        NOT NULL DEFAULT 0,
    provider         VARCHAR(32)   NOT NULL DEFAULT '',
    stream           BOOLEAN       NOT NULL DEFAULT FALSE,
    status           VARCHAR(16)   NOT NULL,
    price            JSONB         NOT NULL DEFAULT '{}'::jsonb,
    factor           NUMERIC(12,6) NOT NULL DEFAULT 1,
    estimate         BIGINT        NOT NULL DEFAULT 0,
    reserved         BIGINT        NOT NULL DEFAULT 0,
    holds            JSONB         NOT NULL DEFAULT '[]'::jsonb,
    -- 暂扣那一刻的计费口径（扣费顺序、余额折算、时区、额度上限、会员与等级）。
    billing          JSONB         NOT NULL DEFAULT '{}'::jsonb,
    clamp_max_tokens INT           NOT NULL DEFAULT 0,
    input_tokens     BIGINT        NOT NULL DEFAULT 0,
    output_tokens    BIGINT        NOT NULL DEFAULT 0,
    images           BIGINT        NOT NULL DEFAULT 0,
    characters       BIGINT        NOT NULL DEFAULT 0,
    audio_ms         BIGINT        NOT NULL DEFAULT 0,
    usage_estimated  BOOLEAN       NOT NULL DEFAULT FALSE,
    -- cost 计价结果（含倍率）；charged 实际扣到的；shortfall 实际用量超出暂扣且余额不够补的部分。
    cost             BIGINT        NOT NULL DEFAULT 0,
    charged          BIGINT        NOT NULL DEFAULT 0,
    shortfall        BIGINT        NOT NULL DEFAULT 0,
    charges          JSONB         NOT NULL DEFAULT '[]'::jsonb,
    integral_txn_no  VARCHAR(64),
    wallet_txn_no    VARCHAR(64),
    idempotency_key  VARCHAR(128),
    error_code       VARCHAR(64)   NOT NULL DEFAULT '',
    error_message    TEXT          NOT NULL DEFAULT '',
    client_ip        VARCHAR(64),
    user_agent       VARCHAR(256),
    latency_ms       INT           NOT NULL DEFAULT 0,
    refund_reason    VARCHAR(200),
    refunded_by      VARCHAR(64),
    refunded_at      TIMESTAMPTZ,
    created_at       TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    expires_at       TIMESTAMPTZ   NOT NULL,
    settled_at       TIMESTAMPTZ,
    CONSTRAINT ck_ai_gateway_requests_status CHECK (
        status IN ('reserved', 'succeeded', 'aborted', 'failed', 'expired', 'refunded')),
    CONSTRAINT ck_ai_gateway_requests_amounts CHECK (
        estimate >= 0 AND reserved >= 0 AND cost >= 0 AND charged >= 0 AND shortfall >= 0)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_ai_gateway_requests_no ON ai_gateway_requests (request_no);
CREATE INDEX IF NOT EXISTS idx_ai_gateway_requests_user ON ai_gateway_requests (appid, user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ai_gateway_requests_app ON ai_gateway_requests (appid, created_at DESC);
-- 后台回收与并发计数只看暂扣中的行。
CREATE INDEX IF NOT EXISTS idx_ai_gateway_requests_reserved
    ON ai_gateway_requests (user_id, expires_at) WHERE status = 'reserved';
-- 幂等键：失败的请求不占位，同一个键可以重试；进行中与成功的不能重复。
CREATE UNIQUE INDEX IF NOT EXISTS uq_ai_gateway_requests_idempotency
    ON ai_gateway_requests (user_id, idempotency_key)
    WHERE idempotency_key IS NOT NULL AND status NOT IN ('failed', 'expired');
