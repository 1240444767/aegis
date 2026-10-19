-- +migrate Down
DROP TABLE IF EXISTS notification_preferences;
DROP INDEX IF EXISTS idx_notifications_app_user_category_time;
ALTER TABLE notifications DROP COLUMN IF EXISTS expires_at;
ALTER TABLE notifications DROP COLUMN IF EXISTS image;
ALTER TABLE notifications DROP COLUMN IF EXISTS action;
ALTER TABLE notifications DROP COLUMN IF EXISTS category;
