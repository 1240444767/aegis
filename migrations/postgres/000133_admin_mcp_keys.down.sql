-- +migrate Down
DROP INDEX IF EXISTS idx_admin_mcp_keys_admin;
DROP INDEX IF EXISTS uq_admin_mcp_keys_hash;
DROP TABLE IF EXISTS admin_mcp_keys;
