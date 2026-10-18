-- +migrate Down
DROP INDEX IF EXISTS idx_storage_objects_config_id;
ALTER TABLE storage_configs DROP COLUMN IF EXISTS redirect_config_id;
DROP TABLE IF EXISTS storage_migrations;
DROP TABLE IF EXISTS storage_pool_events;
DROP TABLE IF EXISTS storage_pool_members;
DROP TABLE IF EXISTS storage_pools;
