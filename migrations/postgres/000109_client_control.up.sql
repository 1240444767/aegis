-- +migrate Up
-- 版本管控：客户端加密上报身份，服务端按规则下发签名裁决（放行 / 提醒 / 强制更新 / 停用）。规则见 docs/client-control.md。
--
--   app_client_control_settings  应用的管控设置，一个应用一行；没有这一行按未启用处理
--   app_client_control_keys      管控密钥：sign = Ed25519 裁决签名密钥，box = X25519 上报加密密钥；私钥以主密钥加密保存。
--                                新签名密钥由上一把签名密钥背书（endorsement），客户端顺着背书链信任新密钥
--   app_client_control_rules     管控规则，priority 高者先匹配，第一条命中的规则决定结果
--   app_client_installs          每台设备上每个包名的最近一次上报与裁决，构成版本分布与设备清单
--   app_client_control_daily     按天的校验次数、去重设备数与停用数
--
-- 迁移器每次启动都会重跑全部 *.up.sql，因此所有语句都必须可重复执行。

CREATE TABLE IF NOT EXISTS app_client_control_settings (
    appid BIGINT PRIMARY KEY REFERENCES apps(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    policy_version BIGINT NOT NULL DEFAULT 1,
    allowed_packages TEXT[] NOT NULL DEFAULT '{}',
    allowed_certs TEXT[] NOT NULL DEFAULT '{}',
    verdict_ttl_seconds INTEGER NOT NULL DEFAULT 86400,
    recheck_seconds INTEGER NOT NULL DEFAULT 600,
    offline_policy VARCHAR(16) NOT NULL DEFAULT 'allow',
    watch_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    server_gate BOOLEAN NOT NULL DEFAULT TRUE,
    block_title VARCHAR(80) NOT NULL DEFAULT '',
    block_message TEXT NOT NULL DEFAULT '',
    updated_by VARCHAR(128) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS app_client_control_keys (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    key_id VARCHAR(64) NOT NULL UNIQUE,
    kind VARCHAR(8) NOT NULL,
    public_key BYTEA NOT NULL,
    private_key_cipher TEXT NOT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    endorsed_by VARCHAR(64) NOT NULL DEFAULT '',
    endorsement BYTEA NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    retired_at TIMESTAMPTZ NULL
);

-- 每个应用每种密钥同一时刻只有一把 active
CREATE UNIQUE INDEX IF NOT EXISTS uq_app_client_control_keys_active
    ON app_client_control_keys (appid, kind) WHERE status = 'active';
CREATE INDEX IF NOT EXISTS idx_app_client_control_keys_app
    ON app_client_control_keys (appid, kind, created_at);

CREATE TABLE IF NOT EXISTS app_client_control_rules (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    name VARCHAR(80) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    priority INTEGER NOT NULL DEFAULT 0,
    action VARCHAR(16) NOT NULL,
    conditions JSONB NOT NULL DEFAULT '{}'::jsonb,
    title VARCHAR(80) NOT NULL DEFAULT '',
    message TEXT NOT NULL DEFAULT '',
    action_url TEXT NOT NULL DEFAULT '',
    action_label VARCHAR(32) NOT NULL DEFAULT '',
    starts_at TIMESTAMPTZ NULL,
    ends_at TIMESTAMPTZ NULL,
    note TEXT NOT NULL DEFAULT '',
    hit_count BIGINT NOT NULL DEFAULT 0,
    last_hit_at TIMESTAMPTZ NULL,
    created_by VARCHAR(128) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_app_client_control_rules_app
    ON app_client_control_rules (appid, priority DESC, id);

CREATE TABLE IF NOT EXISTS app_client_installs (
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    device_id VARCHAR(128) NOT NULL,
    package_name VARCHAR(255) NOT NULL,
    version_name VARCHAR(64) NOT NULL DEFAULT '',
    version_code BIGINT NOT NULL DEFAULT 0,
    platform VARCHAR(16) NOT NULL DEFAULT '',
    cert_sha256 VARCHAR(64) NOT NULL DEFAULT '',
    installer VARCHAR(255) NOT NULL DEFAULT '',
    os_version INTEGER NOT NULL DEFAULT 0,
    model VARCHAR(128) NOT NULL DEFAULT '',
    brand VARCHAR(64) NOT NULL DEFAULT '',
    abi VARCHAR(32) NOT NULL DEFAULT '',
    user_id BIGINT NULL,
    ip VARCHAR(64) NOT NULL DEFAULT '',
    last_action VARCHAR(16) NOT NULL DEFAULT 'allow',
    last_reason VARCHAR(32) NOT NULL DEFAULT '',
    last_rule_id BIGINT NULL,
    report_count BIGINT NOT NULL DEFAULT 1,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (appid, device_id, package_name)
);

CREATE INDEX IF NOT EXISTS idx_app_client_installs_seen
    ON app_client_installs (appid, last_seen_at DESC);
CREATE INDEX IF NOT EXISTS idx_app_client_installs_version
    ON app_client_installs (appid, package_name, version_code);

CREATE TABLE IF NOT EXISTS app_client_control_daily (
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    day DATE NOT NULL,
    checks BIGINT NOT NULL DEFAULT 0,
    devices BIGINT NOT NULL DEFAULT 0,
    blocked BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (appid, day)
);
