-- +migrate Up
-- 抽奖：抽中已拥有的奖品怎么办（永久会员抽中会员天数、已永久解锁的人抽中同一功能）。
--
--   lottery_activities.lifetime_policy           points（折算积分）/ draws（改发抽奖次数）/ exclude（不参与抽取）/ grant（照常发放）
--   lottery_activities.lifetime_points_per_day   折算积分时每天会员 / 功能折多少积分
--   lottery_activities.lifetime_points_lifetime  永久套餐 / 永久功能折多少积分
--   lottery_activities.lifetime_draws            改发抽奖次数时发几次
--   lottery_prizes.lifetime_reward               单个奖品的改发（积分 / 经验 / 抽奖次数），空为按活动的方式
--   lottery_draws.conversion                     这一次改发了什么（抽中的那一格仍记在 reward 上）
--
-- 已有活动按「折算积分、每天 10 积分、永久 3650 积分」处理：之前永久会员抽中会员什么也拿不到。
-- 迁移器每次启动都会重跑，全部语句可重复执行。

ALTER TABLE lottery_activities
    ADD COLUMN IF NOT EXISTS lifetime_policy VARCHAR(16) NOT NULL DEFAULT 'points',
    ADD COLUMN IF NOT EXISTS lifetime_points_per_day BIGINT NOT NULL DEFAULT 10,
    ADD COLUMN IF NOT EXISTS lifetime_points_lifetime BIGINT NOT NULL DEFAULT 3650,
    ADD COLUMN IF NOT EXISTS lifetime_draws BIGINT NOT NULL DEFAULT 1;

ALTER TABLE lottery_prizes
    ADD COLUMN IF NOT EXISTS lifetime_reward JSONB NULL;

ALTER TABLE lottery_draws
    ADD COLUMN IF NOT EXISTS conversion JSONB NULL;

-- 统计「改发了多少次」
CREATE INDEX IF NOT EXISTS idx_lottery_draws_converted
    ON lottery_draws (activity_id, created_at) WHERE conversion IS NOT NULL;
