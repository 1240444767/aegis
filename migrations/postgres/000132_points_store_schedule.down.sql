-- +migrate Down
DROP INDEX IF EXISTS idx_points_store_redemptions_item_time;
ALTER TABLE points_store_items DROP CONSTRAINT IF EXISTS ck_points_store_items_daily_hours;
ALTER TABLE points_store_items DROP CONSTRAINT IF EXISTS ck_points_store_items_user_total_limit;
ALTER TABLE points_store_items DROP CONSTRAINT IF EXISTS ck_points_store_items_global_limit;
ALTER TABLE points_store_items DROP CONSTRAINT IF EXISTS ck_points_store_items_restock;
ALTER TABLE points_store_items DROP COLUMN IF EXISTS daily_close_minute;
ALTER TABLE points_store_items DROP COLUMN IF EXISTS daily_open_minute;
ALTER TABLE points_store_items DROP COLUMN IF EXISTS user_total_limit;
ALTER TABLE points_store_items DROP COLUMN IF EXISTS global_limit_count;
ALTER TABLE points_store_items DROP COLUMN IF EXISTS global_limit_period;
ALTER TABLE points_store_items DROP COLUMN IF EXISTS stock_window_start;
ALTER TABLE points_store_items DROP COLUMN IF EXISTS restock_quantity;
ALTER TABLE points_store_items DROP COLUMN IF EXISTS restock_period;
