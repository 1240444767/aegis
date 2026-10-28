-- +migrate Up
-- 每日运气的管理员干预（docs/lottery.md「运气控制」）：按人或全站，重抽 / 定档 / 偏移，带生效时间窗，可撤销。
-- 迁移器每次启动都会重跑全部 *.up.sql，语句必须可重复执行。

CREATE TABLE IF NOT EXISTS lottery_fortune_controls (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    activity_id BIGINT NOT NULL REFERENCES lottery_activities(id) ON DELETE CASCADE,
    -- 为空是全站
    user_id BIGINT NULL REFERENCES users(id) ON DELETE CASCADE,
    kind VARCHAR(16) NOT NULL,
    -- pin：定在的 z；shift：加减的量
    z DOUBLE PRECISION NOT NULL DEFAULT 0,
    level VARCHAR(16) NOT NULL DEFAULT '',
    -- reroll：加进哈希的随机盐
    salt VARCHAR(64) NOT NULL DEFAULT '',
    starts_at TIMESTAMPTZ NOT NULL,
    ends_at TIMESTAMPTZ NOT NULL,
    note TEXT NOT NULL DEFAULT '',
    operator VARCHAR(128) NOT NULL DEFAULT '',
    revoked_at TIMESTAMPTZ NULL,
    revoked_by VARCHAR(128) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_lottery_fortune_controls_kind CHECK (kind IN ('reroll', 'pin', 'shift')),
    CONSTRAINT ck_lottery_fortune_controls_window CHECK (ends_at > starts_at)
);

-- 抽奖与活动页：这个活动里与某人相关（全站或本人）、尚未撤销、还没到期的干预
CREATE INDEX IF NOT EXISTS idx_lottery_fortune_controls_lookup
    ON lottery_fortune_controls (activity_id, user_id, ends_at)
    WHERE revoked_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_lottery_fortune_controls_history
    ON lottery_fortune_controls (activity_id, created_at DESC);
