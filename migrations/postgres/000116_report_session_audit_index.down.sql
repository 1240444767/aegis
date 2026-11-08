-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_session_audit_app_time;
