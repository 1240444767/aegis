-- +migrate Up
-- 登录设备数控制。策略本身存在 apps.settings.deviceLimit，规则见 docs/device-limit.md。
--
--   user_device_limit_overrides  单个用户的例外（豁免或另配上限），优先于会员档位与默认上限
--   user_login_devices           设备台账：用户登录过的每台设备一行，新设备频率与「常用设备」以它为准
--   device_limit_events          处置记录：自动下线、拒绝登录、请用户选择、管理员下线等
--
-- 迁移器每次启动都会重跑全部 *.up.sql，因此所有语句都必须可重复执行。

CREATE TABLE IF NOT EXISTS user_device_limit_overrides (
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    exempt BOOLEAN NOT NULL DEFAULT FALSE,
    max_devices INTEGER NOT NULL DEFAULT 0,
    class_limits JSONB NOT NULL DEFAULT '{}'::jsonb,
    note TEXT NOT NULL DEFAULT '',
    expires_at TIMESTAMPTZ NULL,
    updated_by VARCHAR(128) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (appid, user_id)
);

CREATE TABLE IF NOT EXISTS user_login_devices (
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    device_key VARCHAR(160) NOT NULL,
    platform VARCHAR(16) NOT NULL DEFAULT '',
    model VARCHAR(64) NOT NULL DEFAULT '',
    name VARCHAR(128) NOT NULL DEFAULT '',
    extra JSONB NULL,
    first_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_ip VARCHAR(64) NOT NULL DEFAULT '',
    login_count BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (appid, user_id, device_key)
);

-- 新设备频率：按用户数窗口内首次出现的设备
CREATE INDEX IF NOT EXISTS idx_user_login_devices_first_seen
    ON user_login_devices (appid, user_id, first_seen_at DESC);
-- 概览：窗口内活跃设备
CREATE INDEX IF NOT EXISTS idx_user_login_devices_last_seen
    ON user_login_devices (appid, last_seen_at DESC);

CREATE TABLE IF NOT EXISTS device_limit_events (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    action VARCHAR(24) NOT NULL,
    dimension VARCHAR(16) NOT NULL DEFAULT '',
    device_key VARCHAR(160) NOT NULL DEFAULT '',
    device_name VARCHAR(128) NOT NULL DEFAULT '',
    platform VARCHAR(16) NOT NULL DEFAULT '',
    device_class VARCHAR(16) NOT NULL DEFAULT '',
    trigger_name VARCHAR(128) NOT NULL DEFAULT '',
    ip VARCHAR(64) NOT NULL DEFAULT '',
    limit_value INTEGER NOT NULL DEFAULT 0,
    detail JSONB NOT NULL DEFAULT '{}'::jsonb,
    operator VARCHAR(128) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_device_limit_events_app
    ON device_limit_events (appid, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_device_limit_events_user
    ON device_limit_events (appid, user_id, created_at DESC);
