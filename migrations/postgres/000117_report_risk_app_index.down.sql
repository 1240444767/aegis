-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_risk_assessments_app_time;
