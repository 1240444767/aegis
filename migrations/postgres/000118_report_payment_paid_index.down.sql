-- +migrate Down
DROP INDEX CONCURRENTLY IF EXISTS idx_payment_orders_app_paid;
