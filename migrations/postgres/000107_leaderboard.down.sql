-- +migrate Down
DROP INDEX IF EXISTS idx_integral_transactions_earned;
DROP TABLE IF EXISTS leaderboard_snapshot_entries;
DROP TABLE IF EXISTS leaderboard_snapshots;
DROP TABLE IF EXISTS leaderboard_exclusions;
DROP TABLE IF EXISTS leaderboard_preferences;
DROP TABLE IF EXISTS leaderboard_settings;
