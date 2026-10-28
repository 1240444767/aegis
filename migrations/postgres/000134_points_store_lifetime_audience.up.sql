-- +migrate Up
-- 积分商城兑换对象新增 lifetime（仅永久会员，docs/points-store.md）。
-- 迁移器每次启动都会重跑全部 *.up.sql，语句必须可重复执行。

ALTER TABLE points_store_items DROP CONSTRAINT IF EXISTS ck_points_store_items_audience;
ALTER TABLE points_store_items
    ADD CONSTRAINT ck_points_store_items_audience CHECK (audience IN ('all','vip','non_vip','lifetime'));
