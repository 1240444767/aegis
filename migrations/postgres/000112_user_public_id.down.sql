-- +migrate Down
-- 回滚只拆掉约束与生成器，号本身保留在退役表里：再次上线时不会把发过的号发给别人。
DROP TRIGGER IF EXISTS trg_users_public_id_guard ON users;
DROP FUNCTION IF EXISTS aegis_users_public_id_guard();
INSERT INTO user_public_id_retired (public_id)
SELECT public_id FROM users WHERE public_id IS NOT NULL
ON CONFLICT DO NOTHING;
ALTER TABLE users DROP CONSTRAINT IF EXISTS ck_users_public_id_range;
DROP INDEX IF EXISTS uq_users_public_id;
ALTER TABLE users DROP COLUMN IF EXISTS public_id;
DROP FUNCTION IF EXISTS aegis_new_user_public_id();
