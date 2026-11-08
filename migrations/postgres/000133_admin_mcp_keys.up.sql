-- +migrate Up
-- MCP 密钥（docs/mcp.md）：管理员给 MCP 服务端（AI 助手）签发的长期凭证。
--
-- 一把密钥恒代表签发它的那个管理员，能做的事是**两层交集**：
--   1. 管理员本人此刻的权限（角色、临时权限、显式拒绝照常判定，撤了角色密钥随之失效）；
--   2. 密钥自己的范围：permissions（权限点，支持 `ticket:*` 结尾通配，`*` 为全部）、
--      app_ids（空为不限应用）、read_only（只允许 GET / HEAD）、allow_ips（CIDR，空为不限）。
--
-- 明文只在签发与轮换时返回一次，库里只存 SHA-256；key_prefix 只供列表上辨认是哪一把。
-- 迁移器每次启动都会重跑全部 *.up.sql，语句必须可重复执行。

CREATE TABLE IF NOT EXISTS admin_mcp_keys (
    id BIGSERIAL PRIMARY KEY,
    admin_id BIGINT NOT NULL REFERENCES admin_accounts(id) ON DELETE CASCADE,
    name VARCHAR(96) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    key_prefix VARCHAR(24) NOT NULL,
    key_hash BYTEA NOT NULL,
    permissions TEXT[] NOT NULL DEFAULT '{}',
    app_ids BIGINT[] NOT NULL DEFAULT '{}',
    read_only BOOLEAN NOT NULL DEFAULT TRUE,
    allow_ips TEXT[] NOT NULL DEFAULT '{}',
    rate_limit_per_minute INTEGER NOT NULL DEFAULT 120,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    expires_at TIMESTAMPTZ NULL,
    last_used_at TIMESTAMPTZ NULL,
    last_used_ip VARCHAR(64) NOT NULL DEFAULT '',
    use_count BIGINT NOT NULL DEFAULT 0,
    rotated_at TIMESTAMPTZ NULL,
    revoked_at TIMESTAMPTZ NULL,
    revoked_by BIGINT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_admin_mcp_keys_status CHECK (status IN ('active', 'disabled', 'revoked')),
    CONSTRAINT ck_admin_mcp_keys_rate_limit CHECK (rate_limit_per_minute BETWEEN 0 AND 6000)
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_admin_mcp_keys_hash ON admin_mcp_keys(key_hash);
CREATE INDEX IF NOT EXISTS idx_admin_mcp_keys_admin ON admin_mcp_keys(admin_id, created_at DESC);
