-- +migrate Up
-- 卡密系统重构。
--
-- 000081 的卡只能「一码一人一次」，批次生成之后什么都改不了，发出去的卡也分不清哪些已经交给了渠道。
-- 这一版补上运营真正要用的几件事：
--
--   活动码        一个码可以被多人各用一次（max_uses / use_count），用于群发福利、直播口令
--   领取规则      每人限兑张数、仅会员 / 仅非会员、生效时间（starts_at 之前不能用）
--   卡面          字符集可选（防混淆 / 纯数字 / 纯字母），也可以导入现成的卡面（source = imported）
--   提卡          distributed_at：一张卡交给渠道之后打上标记，下一次提卡不会再拿到它
--   面值与渠道    face_value / channel：统计里回答「这一批值多少钱、卖到哪去了」
--   自助解绑      self_unbind_limit：授权卡用户在客户端里每 30 天能自己解绑几次设备
--   解绑流水      card_key_device_unbinds：自助与管理员解绑都记一笔，限次与排障都靠它
--
-- 迁移器每次启动都会重跑全部 *.up.sql，因此所有语句都必须可重复执行
-- （约束先 DROP IF EXISTS 再 ADD；回填只处理尚未回填的行）。

-- ── 批次 ──
ALTER TABLE card_key_batches
    ADD COLUMN IF NOT EXISTS charset VARCHAR(16) NOT NULL DEFAULT 'safe',
    ADD COLUMN IF NOT EXISTS source VARCHAR(16) NOT NULL DEFAULT 'generated',
    ADD COLUMN IF NOT EXISTS max_uses INTEGER NOT NULL DEFAULT 1,
    -- 同一个人在这一批里最多兑换几张；0 为不限
    ADD COLUMN IF NOT EXISTS per_user_limit INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS audience VARCHAR(16) NOT NULL DEFAULT 'all',
    ADD COLUMN IF NOT EXISTS starts_at TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS channel VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS face_value NUMERIC(12, 2) NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS self_unbind_limit INTEGER NOT NULL DEFAULT 0;

ALTER TABLE card_key_batches DROP CONSTRAINT IF EXISTS ck_card_key_batches_charset;
ALTER TABLE card_key_batches
    ADD CONSTRAINT ck_card_key_batches_charset CHECK (charset IN ('safe', 'digits', 'letters'));

ALTER TABLE card_key_batches DROP CONSTRAINT IF EXISTS ck_card_key_batches_source;
ALTER TABLE card_key_batches
    ADD CONSTRAINT ck_card_key_batches_source CHECK (source IN ('generated', 'imported'));

ALTER TABLE card_key_batches DROP CONSTRAINT IF EXISTS ck_card_key_batches_audience;
ALTER TABLE card_key_batches
    ADD CONSTRAINT ck_card_key_batches_audience CHECK (audience IN ('all', 'vip', 'non_vip'));

ALTER TABLE card_key_batches DROP CONSTRAINT IF EXISTS ck_card_key_batches_max_uses;
ALTER TABLE card_key_batches
    ADD CONSTRAINT ck_card_key_batches_max_uses CHECK (max_uses BETWEEN 1 AND 1000000);

ALTER TABLE card_key_batches DROP CONSTRAINT IF EXISTS ck_card_key_batches_limits;
ALTER TABLE card_key_batches
    ADD CONSTRAINT ck_card_key_batches_limits
    CHECK (per_user_limit BETWEEN 0 AND 100000 AND self_unbind_limit BETWEEN 0 AND 100 AND face_value >= 0);

CREATE INDEX IF NOT EXISTS idx_card_key_batches_channel
    ON card_key_batches(appid, channel) WHERE channel <> '';

-- ── 单卡 ──
ALTER TABLE card_keys
    ADD COLUMN IF NOT EXISTS max_uses INTEGER NOT NULL DEFAULT 1,
    ADD COLUMN IF NOT EXISTS use_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS distributed_at TIMESTAMPTZ NULL,
    ADD COLUMN IF NOT EXISTS distribution_note VARCHAR(128) NOT NULL DEFAULT '';

-- 回填已用次数：老数据里「用过」只体现为状态与绑定账号，没有计数。
-- 只处理 use_count = 0 的行，因此重跑不会把新数据改回去。
UPDATE card_keys c
SET use_count = GREATEST(
        (SELECT COUNT(*) FROM card_key_redemptions x WHERE x.card_key_id = c.id),
        CASE WHEN c.status IN ('used', 'active') OR c.bound_user_id IS NOT NULL THEN 1 ELSE 0 END)
WHERE c.use_count = 0
  AND (c.status IN ('used', 'active') OR c.bound_user_id IS NOT NULL
       OR EXISTS (SELECT 1 FROM card_key_redemptions x WHERE x.card_key_id = c.id));

ALTER TABLE card_keys DROP CONSTRAINT IF EXISTS ck_card_keys_uses;
ALTER TABLE card_keys
    ADD CONSTRAINT ck_card_keys_uses CHECK (max_uses BETWEEN 1 AND 1000000 AND use_count BETWEEN 0 AND max_uses);

-- 「还没交给渠道的未使用卡」是提卡的查询条件，也是列表里最常用的一档筛选。
CREATE INDEX IF NOT EXISTS idx_card_keys_batch_undistributed
    ON card_keys(batch_id, id) WHERE status = 'unused' AND distributed_at IS NULL;

-- ── 核销流水 ──
--
-- 一码一用的唯一约束放宽成「一码一人一次」：活动码要被多人使用，
-- 而同一个人重复兑换同一个码仍然由这条约束兜住（并发抢占被改坏时，表现是重复发奖）。
ALTER TABLE card_key_redemptions DROP CONSTRAINT IF EXISTS uq_card_key_redemptions_card;
ALTER TABLE card_key_redemptions DROP CONSTRAINT IF EXISTS uq_card_key_redemptions_card_user;
ALTER TABLE card_key_redemptions
    ADD CONSTRAINT uq_card_key_redemptions_card_user UNIQUE (card_key_id, user_id);

-- 每人限兑按批次数行数
CREATE INDEX IF NOT EXISTS idx_card_key_redemptions_batch_user
    ON card_key_redemptions(batch_id, user_id);

-- ── 设备解绑流水 ──
CREATE TABLE IF NOT EXISTS card_key_device_unbinds (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL,
    card_key_id BIGINT NOT NULL REFERENCES card_keys(id) ON DELETE CASCADE,
    device_id VARCHAR(128) NOT NULL,
    device_name VARCHAR(128) NOT NULL DEFAULT '',
    -- self 用户在客户端自助解绑 / admin 管理员在控制台解绑
    source VARCHAR(16) NOT NULL DEFAULT 'admin',
    operator VARCHAR(128) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

ALTER TABLE card_key_device_unbinds DROP CONSTRAINT IF EXISTS ck_card_key_device_unbinds_source;
ALTER TABLE card_key_device_unbinds
    ADD CONSTRAINT ck_card_key_device_unbinds_source CHECK (source IN ('self', 'admin'));

CREATE INDEX IF NOT EXISTS idx_card_key_device_unbinds_card
    ON card_key_device_unbinds(card_key_id, created_at DESC);
