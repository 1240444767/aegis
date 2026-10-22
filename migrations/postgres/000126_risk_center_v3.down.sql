-- +migrate Down
DROP TABLE IF EXISTS risk_lists;
DROP INDEX IF EXISTS idx_risk_assessments_pending;
DROP INDEX IF EXISTS idx_risk_assessments_user;
DROP INDEX IF EXISTS idx_risk_assessments_app_account;
ALTER TABLE risk_assessments
    DROP COLUMN IF EXISTS dimension_scores,
    DROP COLUMN IF EXISTS list_hits,
    DROP COLUMN IF EXISTS decision_by,
    DROP COLUMN IF EXISTS enforcement;
ALTER TABLE risk_actions
    DROP COLUMN IF EXISTS app_id,
    DROP COLUMN IF EXISTS ban_targets,
    DROP COLUMN IF EXISTS updated_at;
DROP INDEX IF EXISTS idx_risk_rules_app;
ALTER TABLE risk_rules
    DROP COLUMN IF EXISTS app_id,
    DROP COLUMN IF EXISTS mode,
    DROP COLUMN IF EXISTS decision,
    DROP COLUMN IF EXISTS template_key;
