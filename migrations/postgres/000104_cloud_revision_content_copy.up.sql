-- +migrate Up
-- 云存储修订的内容在数据库里留一份副本（不超过 1 MiB 的修订）。
-- 只存在存储桶里的内容会随存储一起丢：本地磁盘存储放在容器里时，每次重新部署都会被清空，
-- 修订行却还指向那些对象，读取就一直失败。存量修订在下次被读到时补写副本。
ALTER TABLE app_cloud_item_revisions ADD COLUMN IF NOT EXISTS content BYTEA NULL;
