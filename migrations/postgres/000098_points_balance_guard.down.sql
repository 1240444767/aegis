-- +migrate Down
ALTER TABLE users DROP CONSTRAINT IF EXISTS chk_users_experience_range;
ALTER TABLE users DROP CONSTRAINT IF EXISTS chk_users_integral_range;
