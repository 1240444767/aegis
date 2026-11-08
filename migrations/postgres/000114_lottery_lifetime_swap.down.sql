-- +migrate Down
DROP INDEX IF EXISTS idx_lottery_draws_converted;
ALTER TABLE lottery_draws DROP COLUMN IF EXISTS conversion;
ALTER TABLE lottery_prizes DROP COLUMN IF EXISTS lifetime_reward;
ALTER TABLE lottery_activities
    DROP COLUMN IF EXISTS lifetime_policy,
    DROP COLUMN IF EXISTS lifetime_points_per_day,
    DROP COLUMN IF EXISTS lifetime_points_lifetime,
    DROP COLUMN IF EXISTS lifetime_draws;
