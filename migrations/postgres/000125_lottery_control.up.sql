-- +migrate Up
-- 抽奖概率控制层：护栏用的奖品积分当量、运气平衡用的每人累计期望。
--
--   lottery_prizes.points_value        奖品的积分当量，0 为按类型估算（人工发放、会员套餐建议设置）。
--                                      不叫 value：000110 以「lottery_prizes.value 存在」判断旧表形状，重名会让它每次启动都重做搬迁
--   lottery_user_states.luck_expected  这个人在本活动里历次「公平中奖率」之和（应得的中奖次数）
--   lottery_user_states.luck_variance  历次 p(1−p) 之和
--   lottery_user_states.luck_skew      历次 p(1−p)(1−2p) 之和（泊松二项的三阶中心矩，偏度修正用）
--
-- 迁移器每次启动都会重跑，全部语句可重复执行。

ALTER TABLE lottery_prizes
    ADD COLUMN IF NOT EXISTS points_value BIGINT NOT NULL DEFAULT 0;

ALTER TABLE lottery_user_states
    ADD COLUMN IF NOT EXISTS luck_expected DOUBLE PRECISION NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS luck_variance DOUBLE PRECISION NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS luck_skew DOUBLE PRECISION NOT NULL DEFAULT 0;
