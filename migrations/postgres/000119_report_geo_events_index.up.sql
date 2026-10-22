-- +migrate Up
-- 报表分析中心按应用与时间窗统计登录归属地。分区表的父表不支持 CONCURRENTLY；
-- 各分区按月切分、单个分区不大，普通建索引的锁很短。
CREATE INDEX IF NOT EXISTS idx_lge_app_time ON login_geo_events (app_id, created_at);
