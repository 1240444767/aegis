-- +migrate Up
-- 抽奖的应用级设置（docs/lottery.md「至尊加成」）：目前只有每日运气的至尊加成，活动策略里没写时跟随这里。
-- 迁移器每次启动都会重跑全部 *.up.sql，语句必须可重复执行。

CREATE TABLE IF NOT EXISTS lottery_app_settings (
    appid BIGINT PRIMARY KEY REFERENCES apps(id) ON DELETE CASCADE,
    -- lottery.Supreme：enabled / scope / boost / floor / cap / tiers / title
    supreme JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_by VARCHAR(128) NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
