-- +migrate Up
-- 迁移时源对象已丢失、但数据库里留有字节副本（头像副本、云存储修订内容）的，
-- 从副本写回目标存储；这里记这样找回的个数（同时计入 copied_objects）。
ALTER TABLE storage_migrations ADD COLUMN IF NOT EXISTS recovered_objects BIGINT NOT NULL DEFAULT 0;
