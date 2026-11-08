-- +migrate Up
-- 物品发放（docs/item-grant.md）：运营在控制台的画布上编排规则（触发器 → 筛选 → 分流 / 限额 → 发放 → 站内信），
-- 手动、定时或在用户注册 / 登录 / 签到 / 支付时实时执行。物品逐档复用各自账本的事务内入口。
--
-- 迁移器每次启动都会重跑全部 *.up.sql，语句必须可重复执行。

CREATE TABLE IF NOT EXISTS item_grant_rules (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL,
    name VARCHAR(64) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    -- draft / active / paused / archived
    status VARCHAR(16) NOT NULL DEFAULT 'draft',
    -- 从画布的触发器派生：manual / schedule / event
    trigger_kind VARCHAR(16) NOT NULL DEFAULT 'manual',
    trigger_event VARCHAR(32) NOT NULL DEFAULT '',
    cron VARCHAR(64) NOT NULL DEFAULT '',
    starts_at TIMESTAMPTZ NULL,
    ends_at TIMESTAMPTZ NULL,
    graph JSONB NOT NULL DEFAULT '{"nodes":[],"edges":[]}'::jsonb,
    version INTEGER NOT NULL DEFAULT 1,
    next_run_at TIMESTAMPTZ NULL,
    last_run_at TIMESTAMPTZ NULL,
    created_by VARCHAR(64) NOT NULL DEFAULT '',
    updated_by VARCHAR(64) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
ALTER TABLE item_grant_rules DROP CONSTRAINT IF EXISTS ck_item_grant_rules_status;
ALTER TABLE item_grant_rules ADD CONSTRAINT ck_item_grant_rules_status
    CHECK (status IN ('draft', 'active', 'paused', 'archived'));
CREATE INDEX IF NOT EXISTS idx_item_grant_rules_app ON item_grant_rules(appid, status);
-- 事件触发：按应用与事件找生效中的规则
CREATE INDEX IF NOT EXISTS idx_item_grant_rules_event
    ON item_grant_rules(appid, trigger_event) WHERE status = 'active' AND trigger_kind = 'event';
-- 定时执行：后台循环找到点的规则
CREATE INDEX IF NOT EXISTS idx_item_grant_rules_due
    ON item_grant_rules(next_run_at) WHERE status = 'active' AND trigger_kind = 'schedule';

CREATE TABLE IF NOT EXISTS item_grant_runs (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL,
    rule_id BIGINT NOT NULL REFERENCES item_grant_rules(id) ON DELETE CASCADE,
    rule_version INTEGER NOT NULL DEFAULT 1,
    -- manual / schedule
    trigger_kind VARCHAR(16) NOT NULL,
    -- running / succeeded / failed / cancelled
    status VARCHAR(16) NOT NULL DEFAULT 'running',
    graph JSONB NOT NULL,
    total BIGINT NOT NULL DEFAULT 0,
    processed BIGINT NOT NULL DEFAULT 0,
    granted BIGINT NOT NULL DEFAULT 0,
    skipped BIGINT NOT NULL DEFAULT 0,
    failed BIGINT NOT NULL DEFAULT 0,
    -- 已处理到的用户 ID（按 ID 升序逐批处理；进程重启后由别的实例从这里接着跑）
    cursor_user_id BIGINT NOT NULL DEFAULT 0,
    error TEXT NOT NULL DEFAULT '',
    operator VARCHAR(64) NOT NULL DEFAULT '',
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    heartbeat_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ NULL
);
ALTER TABLE item_grant_runs DROP CONSTRAINT IF EXISTS ck_item_grant_runs_status;
ALTER TABLE item_grant_runs ADD CONSTRAINT ck_item_grant_runs_status
    CHECK (status IN ('running', 'succeeded', 'failed', 'cancelled'));
CREATE INDEX IF NOT EXISTS idx_item_grant_runs_rule ON item_grant_runs(rule_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_item_grant_runs_app ON item_grant_runs(appid, id DESC);
-- 一条规则同一时刻只有一次在执行
CREATE UNIQUE INDEX IF NOT EXISTS uq_item_grant_runs_running ON item_grant_runs(rule_id) WHERE status = 'running';

CREATE TABLE IF NOT EXISTS item_grant_records (
    id BIGSERIAL PRIMARY KEY,
    record_no VARCHAR(40) NOT NULL,
    appid BIGINT NOT NULL,
    rule_id BIGINT NOT NULL REFERENCES item_grant_rules(id) ON DELETE CASCADE,
    rule_version INTEGER NOT NULL DEFAULT 1,
    run_id BIGINT NULL REFERENCES item_grant_runs(id) ON DELETE SET NULL,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    trigger_kind VARCHAR(16) NOT NULL,
    trigger_event VARCHAR(32) NOT NULL DEFAULT '',
    -- 同一条规则内的幂等键：run:<执行>:<用户>、event:<事件>:<事件键>
    dedupe_key VARCHAR(128) NOT NULL,
    -- granted / pending / fulfilled / failed
    status VARCHAR(16) NOT NULL,
    steps JSONB NOT NULL DEFAULT '[]'::jsonb,
    items JSONB NOT NULL DEFAULT '[]'::jsonb,
    results JSONB NOT NULL DEFAULT '[]'::jsonb,
    -- 通过了的限额节点：限额用量按它数
    limit_nodes TEXT[] NOT NULL DEFAULT '{}',
    event_data JSONB NULL,
    error TEXT NOT NULL DEFAULT '',
    note VARCHAR(200) NOT NULL DEFAULT '',
    handled_by VARCHAR(64) NOT NULL DEFAULT '',
    handled_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
ALTER TABLE item_grant_records DROP CONSTRAINT IF EXISTS ck_item_grant_records_status;
ALTER TABLE item_grant_records ADD CONSTRAINT ck_item_grant_records_status
    CHECK (status IN ('granted', 'pending', 'fulfilled', 'failed'));
CREATE UNIQUE INDEX IF NOT EXISTS uq_item_grant_records_no ON item_grant_records(record_no);
CREATE UNIQUE INDEX IF NOT EXISTS uq_item_grant_records_dedupe ON item_grant_records(rule_id, dedupe_key);
CREATE INDEX IF NOT EXISTS idx_item_grant_records_app ON item_grant_records(appid, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_item_grant_records_rule_user ON item_grant_records(rule_id, user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_item_grant_records_rule_time ON item_grant_records(rule_id, created_at);
CREATE INDEX IF NOT EXISTS idx_item_grant_records_user ON item_grant_records(appid, user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_item_grant_records_pending ON item_grant_records(appid, status) WHERE status IN ('pending', 'failed');
