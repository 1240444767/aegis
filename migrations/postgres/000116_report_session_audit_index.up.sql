-- +migrate Up
-- 报表分析中心按应用与时间窗扫描会话事件（活跃位图的会话信号）。
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_session_audit_app_time ON session_audit_logs (appid, created_at) INCLUDE (user_id);
