-- +migrate Down
DROP INDEX IF EXISTS idx_risk_assessments_user;
DROP TABLE IF EXISTS user_online_daily;
DROP INDEX IF EXISTS idx_lottery_draws_ip;
DROP INDEX IF EXISTS idx_lottery_draws_device;
DROP INDEX IF EXISTS idx_lottery_draws_user_prize;
DROP TABLE IF EXISTS lottery_prize_daily;
ALTER TABLE lottery_draws DROP COLUMN IF EXISTS trace;
ALTER TABLE lottery_prizes
    DROP COLUMN IF EXISTS daily_limit,
    DROP COLUMN IF EXISTS user_limit,
    DROP COLUMN IF EXISTS user_daily_limit;
ALTER TABLE lottery_activities DROP COLUMN IF EXISTS strategy;
