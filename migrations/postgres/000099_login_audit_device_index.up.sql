-- +migrate Up
-- 设备目录（发布中心选择内测、排除设备）按应用与设备 ID 聚合登录记录。
-- CONCURRENTLY：登录记录表可能很大，普通建索引会在构建期间挡住所有登录写入。
-- 本文件只能有这一条语句：CONCURRENTLY 不能在事务块里执行，多条语句会被合成一个隐式事务。
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_login_audit_app_device_time ON login_audit_logs (appid, device_id, created_at DESC) WHERE device_id IS NOT NULL AND device_id <> '';
