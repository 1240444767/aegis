-- +migrate Up
-- 激励广告的已拥有权益折算（docs/rewarded-ads.md「永久会员与已拥有的权益」）：
-- 永久会员看「领会员 / 功能时长」的广告时，已拥有的部分按场景的 lifetime 配置折算或改发，
-- 「配的是 A、发的是 B」记在 conversion 里（照常发放时为空）。场景的 lifetime 在配置的 JSONB 里，不用迁移。
--
-- 迁移器每次启动都会重跑全部 *.up.sql，语句必须可重复执行。
ALTER TABLE app_rewarded_ad_views ADD COLUMN IF NOT EXISTS conversion JSONB;
