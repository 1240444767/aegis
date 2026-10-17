-- +migrate Up
-- 账号注销：应用级配置、注销申请（立即注销 / 冷静期 / 人工审核）与「注销后限期不能再注册」的墓碑。
--
--   account_deletion_settings    应用的注销配置，一个应用一行；没有这一行按关闭处理（App 里不出现入口）
--   account_deletion_requests    注销申请。user_id 不挂外键：账号删掉之后这一行要留下来，
--                                它是「谁、什么时候、为什么注销、谁批的」的唯一记录
--   account_deletion_tombstones  注销完成后按应用配置留下的标识摘要（账号名、邮箱、手机号），
--                                到期前同一标识不能再注册；只存 HMAC 摘要与打码提示，不存原文
--
-- 迁移器每次启动都会重跑全部 *.up.sql，因此所有语句都必须可重复执行。

CREATE TABLE IF NOT EXISTS account_deletion_settings (
    appid BIGINT PRIMARY KEY REFERENCES apps(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    -- 其余配置（方式、冷静期、验证、前置条件、原因、文案），形状由 internal/domain/accountdeletion 的 Config 决定
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_by VARCHAR(128) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS account_deletion_requests (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL,
    -- 提交那一刻的账号名与昵称：账号删掉之后列表上仍要认得出是谁
    account VARCHAR(255) NOT NULL DEFAULT '',
    nickname VARCHAR(255) NOT NULL DEFAULT '',
    -- immediate 立即注销 / cooling 冷静期满后注销
    mode VARCHAR(16) NOT NULL,
    -- reviewing 待审核 / scheduled 冷静期中 / processing 执行中 / completed 已注销 /
    -- cancelled 已撤销 / rejected 已驳回 / failed 执行失败（可重试）
    status VARCHAR(16) NOT NULL,
    -- user 用户在 App 里提交 / admin 管理员在控制台发起
    source VARCHAR(16) NOT NULL DEFAULT 'user',
    reason_code VARCHAR(64) NOT NULL DEFAULT '',
    reason_text TEXT NOT NULL DEFAULT '',
    cooling_days INT NOT NULL DEFAULT 0,
    -- 到点执行的时刻；待审核时为空（批准时才定）
    scheduled_at TIMESTAMPTZ NULL,
    -- 到期提醒：remind_at 到了且 reminded_at 为空时发一次
    remind_at TIMESTAMPTZ NULL,
    reminded_at TIMESTAMPTZ NULL,
    reviewed_at TIMESTAMPTZ NULL,
    reviewed_by VARCHAR(128) NOT NULL DEFAULT '',
    review_note TEXT NOT NULL DEFAULT '',
    cancelled_at TIMESTAMPTZ NULL,
    -- user 本人撤销 / login 冷静期内登录自动撤销 / admin:<账号> 管理员撤销
    cancelled_by VARCHAR(128) NOT NULL DEFAULT '',
    cancel_reason TEXT NOT NULL DEFAULT '',
    completed_at TIMESTAMPTZ NULL,
    failure TEXT NOT NULL DEFAULT '',
    attempts INT NOT NULL DEFAULT 0,
    -- 提交时通过的验证方式（password / email / phone / totp / recovery / session）
    verified_by JSONB NOT NULL DEFAULT '[]'::jsonb,
    -- 管理员发起时的操作人
    operator VARCHAR(128) NOT NULL DEFAULT '',
    ip VARCHAR(64) NOT NULL DEFAULT '',
    device_id VARCHAR(128) NOT NULL DEFAULT '',
    device VARCHAR(255) NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    -- 提交时的账号概况（打码后的邮箱手机号、注册时间、会员、余额、前置条件结论），只用于展示
    snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_account_deletion_mode CHECK (mode IN ('immediate','cooling')),
    CONSTRAINT ck_account_deletion_status CHECK (status IN ('reviewing','scheduled','processing','completed','cancelled','rejected','failed')),
    CONSTRAINT ck_account_deletion_source CHECK (source IN ('user','admin'))
);

-- 一个账号同时只能有一条进行中的申请（失败的也算：管理员重试或撤销之前不能再提交）
CREATE UNIQUE INDEX IF NOT EXISTS uq_account_deletion_open
    ON account_deletion_requests(appid, user_id) WHERE status IN ('reviewing','scheduled','processing','failed');
CREATE INDEX IF NOT EXISTS idx_account_deletion_due
    ON account_deletion_requests(scheduled_at) WHERE status = 'scheduled';
CREATE INDEX IF NOT EXISTS idx_account_deletion_remind
    ON account_deletion_requests(remind_at) WHERE status = 'scheduled' AND reminded_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_account_deletion_app_created
    ON account_deletion_requests(appid, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_account_deletion_app_status
    ON account_deletion_requests(appid, status, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_account_deletion_user
    ON account_deletion_requests(appid, user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS account_deletion_tombstones (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    request_id BIGINT NULL REFERENCES account_deletion_requests(id) ON DELETE SET NULL,
    -- account / email / phone
    kind VARCHAR(16) NOT NULL,
    -- HMAC-SHA256(appid:kind:规范化后的值)，十六进制
    digest CHAR(64) NOT NULL,
    -- 打码后的样子，控制台用来认
    hint VARCHAR(128) NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_account_deletion_tombstone_kind CHECK (kind IN ('account','email','phone'))
);

CREATE INDEX IF NOT EXISTS idx_account_deletion_tombstones_lookup
    ON account_deletion_tombstones(appid, digest, expires_at);
CREATE INDEX IF NOT EXISTS idx_account_deletion_tombstones_app
    ON account_deletion_tombstones(appid, created_at DESC);
