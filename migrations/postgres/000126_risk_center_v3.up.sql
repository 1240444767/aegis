-- +migrate Up
-- 风控中心第三版：多维度评分、名单库、应用作用域、处置执行留痕。
--
--   risk_rules.app_id         NULL 为全平台规则，非空只对该应用生效
--   risk_rules.mode           enforce 计分 / observe 只观察（命中照记，不计分、不触发强制处置）
--   risk_rules.decision       命中即强制的处置（空为只计分），与分数策略取更严的一个
--   risk_rules.template_key   由规则模板一键安装时记下来源，重复安装据此跳过
--   risk_actions.app_id       NULL 为全平台策略；应用策略优先于全平台策略
--   risk_actions.ban_targets  封禁动作落到哪些对象：account / device / ip / firewall
--   risk_assessments.*        各维度得分、名单命中、判定来源、实际执行的处置
--   risk_lists                统一名单库：黑名单（拦截）/ 白名单（放行）/ 观察名单（加分）
--
-- 迁移器每次启动都会重跑，全部语句可重复执行。

ALTER TABLE risk_rules
    ADD COLUMN IF NOT EXISTS app_id BIGINT NULL,
    ADD COLUMN IF NOT EXISTS mode VARCHAR(16) NOT NULL DEFAULT 'enforce',
    ADD COLUMN IF NOT EXISTS decision VARCHAR(16) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS template_key VARCHAR(64) NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_risk_rules_app ON risk_rules (app_id, scene) WHERE is_active;

ALTER TABLE risk_actions
    ADD COLUMN IF NOT EXISTS app_id BIGINT NULL,
    ADD COLUMN IF NOT EXISTS ban_targets TEXT[] NOT NULL DEFAULT ARRAY['account','device']::TEXT[],
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

ALTER TABLE risk_assessments
    ADD COLUMN IF NOT EXISTS dimension_scores JSONB NOT NULL DEFAULT '{}'::jsonb,
    ADD COLUMN IF NOT EXISTS list_hits JSONB NOT NULL DEFAULT '[]'::jsonb,
    ADD COLUMN IF NOT EXISTS decision_by VARCHAR(16) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS enforcement JSONB NOT NULL DEFAULT '[]'::jsonb;

-- 账号画像：按应用 + 账号、按用户反查
CREATE INDEX IF NOT EXISTS idx_risk_assessments_app_account ON risk_assessments (app_id, account, created_at DESC) WHERE account <> '';
CREATE INDEX IF NOT EXISTS idx_risk_assessments_user ON risk_assessments (user_id, created_at DESC) WHERE user_id IS NOT NULL;
-- 待复核队列
CREATE INDEX IF NOT EXISTS idx_risk_assessments_pending ON risk_assessments (created_at DESC) WHERE action = 'review' AND NOT reviewed;

CREATE TABLE IF NOT EXISTS risk_lists (
    id           BIGSERIAL PRIMARY KEY,
    app_id       BIGINT NULL,
    list_type    VARCHAR(24) NOT NULL,
    value        VARCHAR(256) NOT NULL,
    kind         VARCHAR(16) NOT NULL,
    scenes       TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    reason       TEXT NOT NULL DEFAULT '',
    source       VARCHAR(32) NOT NULL DEFAULT 'manual',
    assessment_id BIGINT NULL,
    expires_at   TIMESTAMPTZ NULL,
    hit_count    BIGINT NOT NULL DEFAULT 0,
    last_hit_at  TIMESTAMPTZ NULL,
    created_by   BIGINT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT chk_risk_lists_kind CHECK (kind IN ('block', 'allow', 'watch'))
);
-- 同一作用域里一个对象只有一条名单记录：拉黑与加白互斥，改名单是改这一条
CREATE UNIQUE INDEX IF NOT EXISTS uq_risk_lists_entry ON risk_lists (COALESCE(app_id, 0), list_type, value);
CREATE INDEX IF NOT EXISTS idx_risk_lists_kind ON risk_lists (kind, list_type);
CREATE INDEX IF NOT EXISTS idx_risk_lists_expires ON risk_lists (expires_at) WHERE expires_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_risk_lists_assessment ON risk_lists (assessment_id) WHERE assessment_id IS NOT NULL;

-- 存量的人工封禁 / 受信标签并入名单库：此前「设备 / IP 标成封禁」只是一个变量，
-- 规则里没人引用它就不拦任何请求。并入之后它们是真正生效的名单。
-- 只搬一次（表注释作标记）：迁移器每次启动都重跑，管理员删掉的名单不能被下一次启动搬回来。
DO $$
BEGIN
    IF obj_description('risk_lists'::regclass, 'pg_class') IS DISTINCT FROM 'risk-lists:v3' THEN
        INSERT INTO risk_lists (list_type, value, kind, reason, source)
        SELECT 'ip', ip, CASE WHEN risk_tag = 'blocked' THEN 'block' ELSE 'allow' END,
               COALESCE(NULLIF(note, ''), '由 IP 风险库标签迁入'), 'migrated'
        FROM ip_risk_records WHERE risk_tag IN ('blocked', 'trusted')
        ON CONFLICT DO NOTHING;
        INSERT INTO risk_lists (list_type, value, kind, reason, source)
        SELECT 'device', device_id, CASE WHEN risk_tag = 'blocked' THEN 'block' ELSE 'allow' END,
               COALESCE(NULLIF(note, ''), '由设备指纹标签迁入'), 'migrated'
        FROM device_fingerprints WHERE risk_tag IN ('blocked', 'trusted')
        ON CONFLICT DO NOTHING;
        COMMENT ON TABLE risk_lists IS 'risk-lists:v3';
    END IF;
END $$;
