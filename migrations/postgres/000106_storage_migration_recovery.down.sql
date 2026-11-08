-- +migrate Down
ALTER TABLE storage_migrations DROP COLUMN IF EXISTS recovered_objects;
