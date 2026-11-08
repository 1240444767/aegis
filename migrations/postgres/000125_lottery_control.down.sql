-- +migrate Down
ALTER TABLE lottery_user_states
    DROP COLUMN IF EXISTS luck_expected,
    DROP COLUMN IF EXISTS luck_variance,
    DROP COLUMN IF EXISTS luck_skew;
ALTER TABLE lottery_prizes DROP COLUMN IF EXISTS points_value;
