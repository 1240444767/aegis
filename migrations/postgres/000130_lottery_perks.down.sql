-- +migrate Down
DROP TABLE IF EXISTS user_ad_free_passes;
ALTER TABLE lottery_activities DROP COLUMN IF EXISTS perks;
