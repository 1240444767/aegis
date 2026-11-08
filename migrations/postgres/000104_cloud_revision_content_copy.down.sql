-- +migrate Down
ALTER TABLE app_cloud_item_revisions DROP COLUMN IF EXISTS content;
