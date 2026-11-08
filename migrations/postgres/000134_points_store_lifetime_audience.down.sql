-- +migrate Down
UPDATE points_store_items SET audience = 'vip' WHERE audience = 'lifetime';
ALTER TABLE points_store_items DROP CONSTRAINT IF EXISTS ck_points_store_items_audience;
ALTER TABLE points_store_items
    ADD CONSTRAINT ck_points_store_items_audience CHECK (audience IN ('all','vip','non_vip'));
