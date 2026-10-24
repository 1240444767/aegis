-- +migrate Down
ALTER TABLE app_rewarded_ad_views DROP COLUMN IF EXISTS conversion;
