-- +migrate Down
DROP TABLE IF EXISTS level_jobs;
DROP TABLE IF EXISTS user_level_rewards;
DROP TABLE IF EXISTS user_level_events;
DROP TABLE IF EXISTS level_scheme_versions;
DROP TABLE IF EXISTS level_schemes;
DROP INDEX IF EXISTS idx_user_level_records_app_level;
