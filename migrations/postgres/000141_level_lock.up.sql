-- +migrate Up
-- 等级锁定（docs/levels.md「强制升级」）：管理员强制升级时可以把等级锁定为保底。
-- 「重算并允许降级」从这一级起判定，不会把人降到它以下；0 = 没有锁定。
ALTER TABLE user_level_records ADD COLUMN IF NOT EXISTS locked_level INTEGER NOT NULL DEFAULT 0;
