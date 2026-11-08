-- +migrate Down
ALTER TABLE user_level_records DROP COLUMN IF EXISTS locked_level;
