-- +migrate Up
-- 邀请：邀请码（个人码 / 活动码）、邀请关系、邀请人的阶梯奖励。
--
--   invite_settings          应用的邀请配置，一个应用一行；没有这一行按关闭处理
--   invite_codes             邀请码。个人码一人一个、按应用配置的格式生成；活动码由运营创建，
--                            可以挂一个推广人、单独配置被邀请人奖励、限次数与有效期
--   invite_relations         邀请关系：一个用户在一个应用里只能被邀请一次（注册时填写或注册后限时补填）
--   invite_milestone_grants  邀请人累计邀请满 N 人的阶梯奖励，每档只发一次
--
-- 老系统的 user_profiles.invite_code（24 位全局唯一）与 parent_invite_account 保留：
-- 老的个人码仍能被填写（查不到新码时回落到它），绑定时把邀请人账号写回 parent_invite_account。
--
-- 迁移器每次启动都会重跑全部 *.up.sql，因此所有语句都必须可重复执行。

CREATE TABLE IF NOT EXISTS invite_settings (
    appid BIGINT PRIMARY KEY REFERENCES apps(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    -- 其余配置（表单、格式、奖励、限制、文案），形状由 internal/domain/invite 的 Settings 决定
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_by VARCHAR(128) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS invite_codes (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    -- 展示用的原样；比对一律按 UPPER(code)
    code VARCHAR(32) NOT NULL,
    -- personal 个人码 / campaign 活动码
    kind VARCHAR(16) NOT NULL,
    -- 个人码的主人；活动码的推广人（可空：纯渠道码没有邀请人奖励）
    owner_user_id BIGINT NULL REFERENCES users(id) ON DELETE CASCADE,
    title VARCHAR(64) NOT NULL DEFAULT '',
    note VARCHAR(256) NOT NULL DEFAULT '',
    -- 活动码单独配置的被邀请人奖励；NULL 为跟随应用默认
    rewards JSONB NULL,
    -- 可使用次数，NULL 为不限
    max_uses BIGINT NULL,
    used_count BIGINT NOT NULL DEFAULT 0,
    starts_at TIMESTAMPTZ NULL,
    ends_at TIMESTAMPTZ NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    -- 个人码是否由用户自定义（自定义过的不再随格式变化）
    custom BOOLEAN NOT NULL DEFAULT FALSE,
    created_by VARCHAR(128) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_invite_codes_kind CHECK (kind IN ('personal','campaign')),
    CONSTRAINT ck_invite_codes_status CHECK (status IN ('active','disabled')),
    CONSTRAINT ck_invite_codes_personal_owner CHECK (kind <> 'personal' OR owner_user_id IS NOT NULL),
    CONSTRAINT ck_invite_codes_uses CHECK (max_uses IS NULL OR max_uses >= 0),
    CONSTRAINT ck_invite_codes_window CHECK (ends_at IS NULL OR starts_at IS NULL OR ends_at > starts_at)
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_invite_codes_code ON invite_codes(appid, UPPER(code));
-- 一人一个个人码
CREATE UNIQUE INDEX IF NOT EXISTS uq_invite_codes_personal ON invite_codes(appid, owner_user_id) WHERE kind = 'personal';
CREATE INDEX IF NOT EXISTS idx_invite_codes_list ON invite_codes(appid, kind, created_at DESC);

CREATE TABLE IF NOT EXISTS invite_relations (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    invitee_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- 邀请人；纯渠道码为空
    inviter_id BIGINT NULL REFERENCES users(id) ON DELETE SET NULL,
    code_id BIGINT NULL REFERENCES invite_codes(id) ON DELETE SET NULL,
    -- 填写时的邀请码与类型（快照：码被删、被改之后记录仍说得清）
    code VARCHAR(32) NOT NULL,
    code_kind VARCHAR(16) NOT NULL,
    -- register 注册时填写 / bind 注册后补填
    source VARCHAR(16) NOT NULL,
    invitee_results JSONB NOT NULL DEFAULT '[]'::jsonb,
    -- none 无邀请人奖励 / pending 等待被邀请人达成条件 / granted 已发放 / limited 邀请人已达上限 / cancelled 已取消
    inviter_status VARCHAR(16) NOT NULL DEFAULT 'none',
    inviter_results JSONB NOT NULL DEFAULT '[]'::jsonb,
    inviter_granted_at TIMESTAMPTZ NULL,
    handled_by VARCHAR(128) NULL,
    device_id VARCHAR(128) NULL,
    client_ip VARCHAR(64) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_invite_relations_source CHECK (source IN ('register','bind')),
    CONSTRAINT ck_invite_relations_status CHECK (inviter_status IN ('none','pending','granted','limited','cancelled')),
    CONSTRAINT ck_invite_relations_self CHECK (inviter_id IS NULL OR inviter_id <> invitee_id)
);
-- 一个用户在一个应用里只能被邀请一次
CREATE UNIQUE INDEX IF NOT EXISTS uq_invite_relations_invitee ON invite_relations(appid, invitee_id);
-- 我邀请的人 / 邀请人的发放计数
CREATE INDEX IF NOT EXISTS idx_invite_relations_inviter ON invite_relations(appid, inviter_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_invite_relations_granted ON invite_relations(appid, inviter_id, inviter_granted_at)
    WHERE inviter_status = 'granted';
CREATE INDEX IF NOT EXISTS idx_invite_relations_code ON invite_relations(code_id, created_at DESC);
-- 设备 / IP 限制
CREATE INDEX IF NOT EXISTS idx_invite_relations_device ON invite_relations(appid, device_id) WHERE device_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_invite_relations_ip ON invite_relations(appid, client_ip, created_at) WHERE client_ip IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_invite_relations_created ON invite_relations(appid, created_at DESC);

CREATE TABLE IF NOT EXISTS invite_milestone_grants (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- 达成的档位（累计成功邀请人数）
    milestone INTEGER NOT NULL,
    results JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_invite_milestone_grants UNIQUE (appid, user_id, milestone)
);
