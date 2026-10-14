-- +migrate Up
-- 积分与经验余额的数据库兜底：不得为负，不得超过 2^53-1（JavaScript 能精确表示的最大整数）。
--
-- 应用层已在每条写入路径上按 pointdomain.ApplyDelta 校验；这里防的是绕过应用层的写入
-- 与将来新增却忘了校验的路径。NOT VALID：只约束此后的写入，不扫描、不拒绝存量数据，
-- 迁移在大表上也是瞬时的；存量若有越界值，可修正后再 VALIDATE CONSTRAINT。
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_users_integral_range') THEN
        ALTER TABLE users ADD CONSTRAINT chk_users_integral_range
            CHECK (integral >= 0 AND integral <= 9007199254740991) NOT VALID;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_users_experience_range') THEN
        ALTER TABLE users ADD CONSTRAINT chk_users_experience_range
            CHECK (experience >= 0 AND experience <= 9007199254740991) NOT VALID;
    END IF;
END $$;
