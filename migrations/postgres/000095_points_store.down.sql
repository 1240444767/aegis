-- +migrate Down
DROP TABLE IF EXISTS user_feature_unlocks;
DROP TABLE IF EXISTS points_store_redemptions;
DROP TABLE IF EXISTS points_store_items;
DROP TABLE IF EXISTS points_store_settings;
