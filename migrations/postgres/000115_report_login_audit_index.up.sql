-- +migrate Up
-- 报表分析中心按应用与时间窗扫描登录记录（登录趋势、活跃位图、设备分布）。
-- CONCURRENTLY 不能在事务里执行，本文件只能有这一条语句。
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_login_audit_app_time ON login_audit_logs (appid, created_at) INCLUDE (user_id, status, login_type);
