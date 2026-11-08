-- +migrate Up
-- 等级体系（docs/levels.md）：每个应用一套，在控制台的画布上编排（体系设置 → 等级 → 晋级条件 → 等级 …，
-- 等级上挂达成奖励与等级权益）。草稿与已发布分开存，发布时留一份版本快照（可回滚）。
-- 没有行的应用使用默认体系：由旧版全站等级表 user_levels 原样转换（重构前后等级一致）。user_levels 保留只读。
--
--   level_schemes          每个应用一行：草稿画布、已发布画布与版本号
--   level_scheme_versions  每次发布的画布快照
--   user_level_events      升级（与管理员调级）记录；升级通知的发送状态
--   user_level_rewards     达成奖励：每人每个奖励节点一行（唯一键 reward_key = 等级:节点），自动发放或等用户领取
--   level_jobs             后台任务：按新体系重算全体用户的等级
--
-- 迁移器每次启动都会重跑全部 *.up.sql，语句必须可重复执行。

CREATE TABLE IF NOT EXISTS level_schemes (
    appid BIGINT PRIMARY KEY,
    draft_graph JSONB NOT NULL DEFAULT '{"nodes":[],"edges":[]}'::jsonb,
    -- 草稿的修订号：保存时比对，防止两个人同时编辑互相覆盖
    draft_revision INTEGER NOT NULL DEFAULT 1,
    draft_updated_by VARCHAR(64) NOT NULL DEFAULT '',
    draft_updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    published_graph JSONB NULL,
    published_version INTEGER NOT NULL DEFAULT 0,
    published_by VARCHAR(64) NOT NULL DEFAULT '',
    published_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS level_scheme_versions (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL,
    version INTEGER NOT NULL,
    graph JSONB NOT NULL,
    -- 发布时的摘要：级数、最高门槛、奖励 / 权益 / 晋级条件个数
    summary JSONB NOT NULL DEFAULT '{}'::jsonb,
    note VARCHAR(200) NOT NULL DEFAULT '',
    published_by VARCHAR(64) NOT NULL DEFAULT '',
    published_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_level_scheme_versions UNIQUE (appid, version)
);

CREATE TABLE IF NOT EXISTS user_level_events (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    from_level INTEGER NOT NULL,
    to_level INTEGER NOT NULL,
    -- 判定前记录里的等级（0 = 之前没有记录）：升级通知据此列出这次经过的达成奖励
    floor_level INTEGER NOT NULL DEFAULT 0,
    experience BIGINT NOT NULL DEFAULT 0,
    -- experience 经验变化 / sync 查询等级时补判（晋级条件后来满足）/ recalc 重算任务 / admin 管理员调级
    reason VARCHAR(16) NOT NULL,
    scheme_version INTEGER NOT NULL DEFAULT 0,
    operator VARCHAR(64) NOT NULL DEFAULT '',
    note VARCHAR(200) NOT NULL DEFAULT '',
    -- none 不通知 / pending 待发 / sending 发送中 / sent / failed
    notify_status VARCHAR(16) NOT NULL DEFAULT 'none',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_user_level_events_app ON user_level_events(appid, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_user_level_events_user ON user_level_events(appid, user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_user_level_events_notify ON user_level_events(id) WHERE notify_status = 'pending';

CREATE TABLE IF NOT EXISTS user_level_rewards (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    level INTEGER NOT NULL,
    node_id VARCHAR(64) NOT NULL,
    reward_key VARCHAR(100) NOT NULL,
    label VARCHAR(64) NOT NULL DEFAULT '',
    items JSONB NOT NULL DEFAULT '[]'::jsonb,
    results JSONB NOT NULL DEFAULT '[]'::jsonb,
    -- auto 自动到账 / manual 用户领取
    claim_mode VARCHAR(8) NOT NULL DEFAULT 'auto',
    -- queued 待发放 / claimable 待领取 / granted 已到账 / pending 人工物品待处理 / failed 发放失败
    status VARCHAR(16) NOT NULL,
    error TEXT NOT NULL DEFAULT '',
    attempts INTEGER NOT NULL DEFAULT 0,
    scheme_version INTEGER NOT NULL DEFAULT 0,
    -- 来源：level_up 升级 / backfill 补发
    origin VARCHAR(16) NOT NULL DEFAULT 'level_up',
    granted_at TIMESTAMPTZ NULL,
    handled_by VARCHAR(64) NOT NULL DEFAULT '',
    handled_note VARCHAR(200) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_user_level_rewards UNIQUE (appid, user_id, reward_key)
);
ALTER TABLE user_level_rewards DROP CONSTRAINT IF EXISTS ck_user_level_rewards_status;
ALTER TABLE user_level_rewards ADD CONSTRAINT ck_user_level_rewards_status
    CHECK (status IN ('queued', 'claimable', 'granted', 'pending', 'failed'));
CREATE INDEX IF NOT EXISTS idx_user_level_rewards_app ON user_level_rewards(appid, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_user_level_rewards_user ON user_level_rewards(appid, user_id, level);
CREATE INDEX IF NOT EXISTS idx_user_level_rewards_queued ON user_level_rewards(id) WHERE status = 'queued';

CREATE TABLE IF NOT EXISTS level_jobs (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL,
    -- recalc 按当前体系重算全体用户
    kind VARCHAR(16) NOT NULL,
    params JSONB NOT NULL DEFAULT '{}'::jsonb,
    -- running / succeeded / failed / cancelled
    status VARCHAR(16) NOT NULL DEFAULT 'running',
    scheme_version INTEGER NOT NULL DEFAULT 0,
    total BIGINT NOT NULL DEFAULT 0,
    processed BIGINT NOT NULL DEFAULT 0,
    changed BIGINT NOT NULL DEFAULT 0,
    cursor_id BIGINT NOT NULL DEFAULT 0,
    message TEXT NOT NULL DEFAULT '',
    created_by VARCHAR(64) NOT NULL DEFAULT '',
    heartbeat_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ NULL
);
-- 一个应用同时只有一个任务在跑
CREATE UNIQUE INDEX IF NOT EXISTS uq_level_jobs_running ON level_jobs(appid) WHERE status = 'running';
CREATE INDEX IF NOT EXISTS idx_level_jobs_app ON level_jobs(appid, created_at DESC);

-- 等级记录按应用排名（排行榜、人群筛选都改读这张表）
CREATE INDEX IF NOT EXISTS idx_user_level_records_app_level ON user_level_records(appid, current_level);
