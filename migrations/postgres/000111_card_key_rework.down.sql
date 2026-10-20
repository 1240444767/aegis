-- +migrate Down
-- 活动码被多人用过之后，「一码一行流水」的旧约束已经装不回去：先删掉同一张卡的多余流水再恢复。
DROP TABLE IF EXISTS card_key_device_unbinds;
DROP INDEX IF EXISTS idx_card_key_redemptions_batch_user;
ALTER TABLE card_key_redemptions DROP CONSTRAINT IF EXISTS uq_card_key_redemptions_card_user;
DELETE FROM card_key_redemptions x
USING card_key_redemptions y
WHERE x.card_key_id = y.card_key_id AND x.id > y.id;
ALTER TABLE card_key_redemptions DROP CONSTRAINT IF EXISTS uq_card_key_redemptions_card;
ALTER TABLE card_key_redemptions ADD CONSTRAINT uq_card_key_redemptions_card UNIQUE (card_key_id);

DROP INDEX IF EXISTS idx_card_keys_batch_undistributed;
ALTER TABLE card_keys DROP CONSTRAINT IF EXISTS ck_card_keys_uses;
ALTER TABLE card_keys
    DROP COLUMN IF EXISTS max_uses,
    DROP COLUMN IF EXISTS use_count,
    DROP COLUMN IF EXISTS distributed_at,
    DROP COLUMN IF EXISTS distribution_note;

DROP INDEX IF EXISTS idx_card_key_batches_channel;
ALTER TABLE card_key_batches DROP CONSTRAINT IF EXISTS ck_card_key_batches_charset;
ALTER TABLE card_key_batches DROP CONSTRAINT IF EXISTS ck_card_key_batches_source;
ALTER TABLE card_key_batches DROP CONSTRAINT IF EXISTS ck_card_key_batches_audience;
ALTER TABLE card_key_batches DROP CONSTRAINT IF EXISTS ck_card_key_batches_max_uses;
ALTER TABLE card_key_batches DROP CONSTRAINT IF EXISTS ck_card_key_batches_limits;
ALTER TABLE card_key_batches
    DROP COLUMN IF EXISTS charset,
    DROP COLUMN IF EXISTS source,
    DROP COLUMN IF EXISTS max_uses,
    DROP COLUMN IF EXISTS per_user_limit,
    DROP COLUMN IF EXISTS audience,
    DROP COLUMN IF EXISTS starts_at,
    DROP COLUMN IF EXISTS channel,
    DROP COLUMN IF EXISTS face_value,
    DROP COLUMN IF EXISTS self_unbind_limit;
