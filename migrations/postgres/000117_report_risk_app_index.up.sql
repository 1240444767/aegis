-- +migrate Up
-- 报表分析中心按应用与时间窗统计风控评估。
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_risk_assessments_app_time ON risk_assessments (app_id, created_at);
