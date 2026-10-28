-- +migrate Down
ALTER TABLE vip_transactions DROP CONSTRAINT IF EXISTS fk_vip_transactions_tier;
DROP INDEX IF EXISTS idx_vip_transactions_tier;
ALTER TABLE vip_transactions DROP COLUMN IF EXISTS extra_features;
ALTER TABLE vip_transactions DROP COLUMN IF EXISTS tier_id;

ALTER TABLE vip_plans DROP CONSTRAINT IF EXISTS fk_vip_plans_tier;
DROP INDEX IF EXISTS idx_vip_plans_tier;
ALTER TABLE vip_plans DROP COLUMN IF EXISTS tier_id;

DROP TABLE IF EXISTS vip_tiers;
