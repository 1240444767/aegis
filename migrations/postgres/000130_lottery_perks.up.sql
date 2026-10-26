-- +migrate Up
-- 抽奖：人群特权与三种新奖品（docs/lottery.md「人群特权」「奖品」）。
--
--   lottery_activities.perks   人群特权（会员 / 永久会员 / 免广告用户 / 签到 / 在线时长 …… → 额外免费次数、
--                              视频抽奖按概率免看广告、积分折扣、保底提前、中奖加成），空数组为没有
--   user_ad_free_passes        免广告时长（抽奖奖品 ad_free 发放）：有效期内免开屏广告、功能时长免扣
--
-- 新奖品 vip_lifetime / feature_time / ad_free 存在 lottery_prizes.reward 里，不用改列。
-- 迁移器每次启动都会重跑全部 *.up.sql，语句必须可重复执行。

ALTER TABLE lottery_activities
    ADD COLUMN IF NOT EXISTS perks JSONB NOT NULL DEFAULT '[]'::jsonb;

CREATE TABLE IF NOT EXISTS user_ad_free_passes (
    appid BIGINT NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    granted_days INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (appid, user_id)
);
