-- +migrate Up
-- 邮件系统补全：策略、抑制名单、投递事件时间线，以及共享通道发出的信记在发信应用名下。
--
--   email_policies          一个作用域一行（appid 为 NULL 即平台级）；没有这一行按默认策略处理
--   email_suppressions      不再往这些地址发信：硬退信 / 投诉由回执自动加入，也可手动添加
--   email_delivery_events   投递回执的逐条事件，详情页据此画时间线
--
-- 迁移器每次启动都会重跑全部 *.up.sql，因此所有语句都必须可重复执行。

CREATE TABLE IF NOT EXISTS email_policies (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NULL REFERENCES apps(id) ON DELETE CASCADE,
    -- 形状由 internal/domain/email 的 Policy 决定
    policy JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_by VARCHAR(128) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- 与 app_email_configs 同一条约定：COALESCE(appid, 0) 让平台级那一行也受唯一约束
CREATE UNIQUE INDEX IF NOT EXISTS uq_email_policies_scope ON email_policies (COALESCE(appid, 0));

CREATE TABLE IF NOT EXISTS email_suppressions (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NULL REFERENCES apps(id) ON DELETE CASCADE,
    -- 一律小写落库，比较时不再区分大小写
    address VARCHAR(320) NOT NULL,
    -- bounce 硬退信 / complaint 投诉 / manual 手动
    reason VARCHAR(16) NOT NULL,
    -- webhook 回执自动加入 / admin 管理员添加
    source VARCHAR(16) NOT NULL,
    note TEXT NOT NULL DEFAULT '',
    delivery_id BIGINT NULL REFERENCES email_deliveries(id) ON DELETE SET NULL,
    created_by VARCHAR(128) NOT NULL DEFAULT '',
    -- 为空表示长期有效
    expires_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_email_suppressions_scope_address
    ON email_suppressions (COALESCE(appid, 0), address);
CREATE INDEX IF NOT EXISTS idx_email_suppressions_created
    ON email_suppressions (COALESCE(appid, 0), created_at DESC);

CREATE TABLE IF NOT EXISTS email_delivery_events (
    id BIGSERIAL PRIMARY KEY,
    delivery_id BIGINT NOT NULL REFERENCES email_deliveries(id) ON DELETE CASCADE,
    event VARCHAR(32) NOT NULL,
    -- 这次事件之后的主状态
    status VARCHAR(16) NOT NULL DEFAULT '',
    detail TEXT NOT NULL DEFAULT '',
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_email_delivery_events_delivery
    ON email_delivery_events (delivery_id, occurred_at);

-- 借用平台共享通道发出的信此前记在平台名下，应用的投递记录里看不到自己的信。
-- 现在记在发信应用名下，用这一列标明走的是共享通道；回执按通道归属匹配（见 ApplyEmailDeliveryEvent）。
ALTER TABLE email_deliveries ADD COLUMN IF NOT EXISTS inherited BOOLEAN NOT NULL DEFAULT FALSE;
