-- +migrate Down
ALTER TABLE app_rewarded_ad_configs ALTER COLUMN provider SET DEFAULT 'huijing';
UPDATE app_rewarded_ad_configs SET provider = 'huijing' WHERE provider = 'apex';
