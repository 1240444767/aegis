-- +migrate Up
-- 报表分析中心按到账时刻统计已支付订单。
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_payment_orders_app_paid ON payment_orders (appid, paid_at) WHERE status = 'paid';
