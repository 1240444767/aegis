-- +migrate Up
-- 会员权益的统一折算（docs/vip-conversion.md）：
--
--   vip_conversion_policies  应用的折算策略：已拥有的权益被发放时、开通永久会员时被覆盖的存量，各怎么办、按什么价折成积分
--   vip_conversions          每一次折算：折掉了什么、给了多少积分、触发它的那一笔、是否已因取消永久会员还回
--
-- 会员账本（vip_transactions）另留一行说明：已拥有发放是 0 天的原渠道行，开通永久折掉存量是 lifetime_convert 渠道的负时长行。
-- 两种都不满足会员段的条件（正时长或永久），不贡献任何权益。
--
-- 功能时长流水新增 convert（折掉，delta 为负）与 restore（还回，delta 为正）。
-- 迁移器每次启动都会重跑全部 *.up.sql，语句必须可重复执行。

CREATE TABLE IF NOT EXISTS vip_conversion_policies (
    appid BIGINT PRIMARY KEY REFERENCES apps(id) ON DELETE CASCADE,
    owned_policy VARCHAR(16) NOT NULL DEFAULT 'convert',
    upgrade_policy VARCHAR(16) NOT NULL DEFAULT 'convert',
    per_vip_day BIGINT NOT NULL DEFAULT 10,
    per_feature_day BIGINT NOT NULL DEFAULT 5,
    per_feature_hour BIGINT NOT NULL DEFAULT 10,
    per_lifetime BIGINT NOT NULL DEFAULT 3650,
    restore_on_revoke BOOLEAN NOT NULL DEFAULT TRUE,
    updated_by VARCHAR(128) NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS vip_conversions (
    id BIGSERIAL PRIMARY KEY,
    conversion_no VARCHAR(40) NOT NULL,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- owned（发放时已拥有）/ upgrade（开通永久时折算存量）
    kind VARCHAR(16) NOT NULL,
    -- 触发折算的发放渠道
    source VARCHAR(32) NOT NULL DEFAULT '',
    reason VARCHAR(16) NOT NULL DEFAULT '',
    items JSONB NOT NULL DEFAULT '[]'::jsonb,
    points BIGINT NOT NULL DEFAULT 0,
    integral_transaction_no VARCHAR(40) NULL,
    trigger_transaction_no VARCHAR(40) NULL,
    vip_transaction_no VARCHAR(40) NULL,
    message VARCHAR(512) NOT NULL DEFAULT '',
    -- applied / restored
    status VARCHAR(16) NOT NULL DEFAULT 'applied',
    restored_at TIMESTAMPTZ NULL,
    restore JSONB NULL,
    operator VARCHAR(128) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_vip_conversions_no UNIQUE (conversion_no)
);

CREATE INDEX IF NOT EXISTS idx_vip_conversions_user
    ON vip_conversions(appid, user_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_vip_conversions_app
    ON vip_conversions(appid, id DESC);

ALTER TABLE feature_time_ledger DROP CONSTRAINT IF EXISTS ck_feature_time_ledger_kind;
ALTER TABLE feature_time_ledger ADD CONSTRAINT ck_feature_time_ledger_kind
    CHECK (kind IN ('earn','lease','refund','convert','restore'));
