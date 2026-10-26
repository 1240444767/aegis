-- +migrate Down
DELETE FROM feature_time_ledger WHERE kind IN ('convert','restore');
ALTER TABLE feature_time_ledger DROP CONSTRAINT IF EXISTS ck_feature_time_ledger_kind;
ALTER TABLE feature_time_ledger ADD CONSTRAINT ck_feature_time_ledger_kind
    CHECK (kind IN ('earn','lease','refund'));
DROP TABLE IF EXISTS vip_conversions;
DROP TABLE IF EXISTS vip_conversion_policies;
