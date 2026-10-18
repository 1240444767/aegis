-- +migrate Up
-- 激励广告平台从灰鲸（huijing）换成 Apex（apex）。两家都基于 ToBid，回调参数与签名算法相同，
-- 但 Security Key 是各平台各自签发的：迁移后要在控制台填入 Apex 后台的 Security Key，
-- 并把 Apex 后台的回调地址改成 /api/apps/{appKey}/ads/apex/callback。
UPDATE app_rewarded_ad_configs SET provider = 'apex' WHERE provider = 'huijing';
ALTER TABLE app_rewarded_ad_configs ALTER COLUMN provider SET DEFAULT 'apex';
