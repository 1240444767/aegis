-- +migrate Up
-- 用户消息中心：分类、跳转动作、配图、过期时间，以及按分类的推送偏好。规则见 docs/notification-center.md。
--
--   notifications.category      分类 key（internal/domain/notification/category.go），按 type 回填
--   notifications.action        跳转动作 {type: route|url, target, label}，可空
--   notifications.image         配图地址（https），可空
--   notifications.expires_at    过期时间，可空；过期的消息不出现在列表、不计未读
--   notification_preferences    用户按分类的提醒开关；没有行即开启
--
-- 迁移器每次启动都会重跑全部 *.up.sql，因此所有语句都必须可重复执行：
-- category 先加成可空列，只回填仍为 NULL 的行，再补默认值与 NOT NULL —— 重跑时不会覆盖写入时显式指定的分类。

ALTER TABLE notifications ADD COLUMN IF NOT EXISTS category VARCHAR(32) NULL;
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS action JSONB NULL;
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS image TEXT NULL;
ALTER TABLE notifications ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ NULL;

-- 回填：与 notification.CategoryOf 的映射表一致；另按 metadata 认出历史上用 system / security 写入的
-- 自动签到与账号注销消息（新写入已改用 auto_sign / account_deletion 这两个 type）。
UPDATE notifications
SET category = CASE
    WHEN metadata->>'module' = 'account_deletion' THEN 'membership'
    WHEN metadata->>'scene' IN ('auto_sign', 'auto_sign_test') THEN 'reward'
    WHEN LOWER(type) IN ('announcement', 'campaign', 'marketing') THEN 'announcement'
    WHEN LOWER(type) IN ('security', 'geo_login', 'password_reset', 'two_factor', 'passkey') THEN 'security'
    WHEN LOWER(type) IN ('vip', 'payment', 'order', 'wallet', 'refund', 'account_deletion') THEN 'membership'
    WHEN LOWER(type) IN ('signin', 'auto_sign', 'points_store', 'invite', 'leaderboard', 'lottery', 'rewarded_ad', 'cardkey') THEN 'reward'
    WHEN LOWER(type) IN ('ticket', 'feedback') THEN 'ticket'
    ELSE 'system'
END
WHERE category IS NULL;

ALTER TABLE notifications ALTER COLUMN category SET DEFAULT 'system';
ALTER TABLE notifications ALTER COLUMN category SET NOT NULL;

-- 列表与按分类计数：(appid, user_id, category, created_at DESC)
CREATE INDEX IF NOT EXISTS idx_notifications_app_user_category_time
    ON notifications (appid, user_id, category, created_at DESC);

CREATE TABLE IF NOT EXISTS notification_preferences (
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    category VARCHAR(32) NOT NULL,
    push BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (appid, user_id, category)
);
