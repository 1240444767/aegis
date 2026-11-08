-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_login_audit_app_time;
