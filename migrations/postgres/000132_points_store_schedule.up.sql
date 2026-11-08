-- +migrate Up
-- 积分商城：补货周期、全站名额、每人累计上限、每日开放时段（docs/points-store.md）。
--
--   restock_period / restock_quantity / stock_window_start
--       每到新的自然日、ISO 周或自然月，剩余库存回到 restock_quantity。补货是惰性的：
--       stock 属于 stock_window_start 那个周期，跨周期后第一笔兑换在锁住的商品行上回写。
--   global_limit_period / global_limit_count
--       全站名额：所有人合计每个周期最多兑多少份，按兑换记录计数（取消的那笔让出名额），0 为不限。
--   user_total_limit
--       每人累计上限，与 limit_period / limit_count 叠加（每日 1 次、累计 10 次），0 为不限。
--   daily_open_minute / daily_close_minute
--       每日开放时段（应用默认时区，自零点起的分钟数），都为空时全天开放；开放晚于关闭表示跨零点。
--
-- 迁移器每次启动都会重跑全部 *.up.sql，语句必须可重复执行。

ALTER TABLE points_store_items ADD COLUMN IF NOT EXISTS restock_period VARCHAR(8) NOT NULL DEFAULT 'none';
ALTER TABLE points_store_items ADD COLUMN IF NOT EXISTS restock_quantity BIGINT NULL;
ALTER TABLE points_store_items ADD COLUMN IF NOT EXISTS stock_window_start TIMESTAMPTZ NULL;
ALTER TABLE points_store_items ADD COLUMN IF NOT EXISTS global_limit_period VARCHAR(8) NOT NULL DEFAULT 'day';
ALTER TABLE points_store_items ADD COLUMN IF NOT EXISTS global_limit_count INTEGER NOT NULL DEFAULT 0;
ALTER TABLE points_store_items ADD COLUMN IF NOT EXISTS user_total_limit INTEGER NOT NULL DEFAULT 0;
ALTER TABLE points_store_items ADD COLUMN IF NOT EXISTS daily_open_minute SMALLINT NULL;
ALTER TABLE points_store_items ADD COLUMN IF NOT EXISTS daily_close_minute SMALLINT NULL;

ALTER TABLE points_store_items DROP CONSTRAINT IF EXISTS ck_points_store_items_restock;
ALTER TABLE points_store_items ADD CONSTRAINT ck_points_store_items_restock CHECK (
    restock_period IN ('none','day','week','month')
    AND (restock_period = 'none' OR (restock_quantity IS NOT NULL AND restock_quantity > 0 AND stock IS NOT NULL))
);
ALTER TABLE points_store_items DROP CONSTRAINT IF EXISTS ck_points_store_items_global_limit;
ALTER TABLE points_store_items ADD CONSTRAINT ck_points_store_items_global_limit CHECK (
    global_limit_count >= 0 AND global_limit_period IN ('total','day','week','month')
);
ALTER TABLE points_store_items DROP CONSTRAINT IF EXISTS ck_points_store_items_user_total_limit;
ALTER TABLE points_store_items ADD CONSTRAINT ck_points_store_items_user_total_limit CHECK (user_total_limit >= 0);
ALTER TABLE points_store_items DROP CONSTRAINT IF EXISTS ck_points_store_items_daily_hours;
ALTER TABLE points_store_items ADD CONSTRAINT ck_points_store_items_daily_hours CHECK (
    (daily_open_minute IS NULL AND daily_close_minute IS NULL)
    OR (daily_open_minute BETWEEN 0 AND 1439 AND daily_close_minute BETWEEN 0 AND 1440
        AND daily_open_minute <> daily_close_minute % 1440)
);

-- 全站名额计数：某件商品在某个时刻之后被兑了几次（不含已取消）
CREATE INDEX IF NOT EXISTS idx_points_store_redemptions_item_time
    ON points_store_redemptions(item_id, created_at) WHERE status <> 'cancelled';
