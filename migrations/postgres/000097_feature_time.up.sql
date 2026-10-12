-- +migrate Up
-- 功能时长：按实际使用的秒数计量的功能权益（非会员看激励广告换「在线听歌 60 分钟」，按实际播放扣）。
--
-- 三张表一张账：
--   feature_time_accounts  余额（每个应用 × 用户 × 功能一行）
--   feature_time_ledger    流水：发放 earn、租出 lease、退回 refund，余额永远等于流水之和
--   feature_time_leases    租约：客户端先预扣一段再使用，停下时结算退回没用完的部分
--
-- 迁移器每次启动都会重跑全部 *.up.sql，因此所有语句都必须可重复执行。

-- ── 余额 ──
--
-- used_seconds 是实际用掉的：租出时加、退回时减（= 租出 − 退回）。
-- 只由发放增加、由租约扣减，扣减在锁住这一行之后判余额，余额不会被并发扣成负数。
CREATE TABLE IF NOT EXISTS feature_time_accounts (
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    feature VARCHAR(64) NOT NULL,
    balance_seconds BIGINT NOT NULL DEFAULT 0,
    earned_seconds BIGINT NOT NULL DEFAULT 0,
    used_seconds BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (appid, user_id, feature),
    -- 兜底：租约在锁住的行上判余额，走到这里说明那段判定被改坏了
    CONSTRAINT ck_feature_time_accounts_balance CHECK (balance_seconds >= 0),
    CONSTRAINT ck_feature_time_accounts_totals CHECK (earned_seconds >= 0 AND used_seconds >= 0)
);

-- ── 流水 ──
CREATE TABLE IF NOT EXISTS feature_time_ledger (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    feature VARCHAR(64) NOT NULL,
    delta_seconds BIGINT NOT NULL,
    balance_after BIGINT NOT NULL,
    kind VARCHAR(16) NOT NULL,
    -- 来源：rewarded_ad（观看记录 ID）/ lease（租约号）
    ref_type VARCHAR(32) NOT NULL DEFAULT '',
    ref_id VARCHAR(64) NOT NULL DEFAULT '',
    remark VARCHAR(256) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_feature_time_ledger_kind CHECK (kind IN ('earn','lease','refund'))
);
CREATE INDEX IF NOT EXISTS idx_feature_time_ledger_user ON feature_time_ledger(appid, user_id, feature, created_at DESC);

-- ── 租约 ──
--
-- 过期不靠定时任务：结算时比较 expires_at，过期的在第一次结算时写成 expired、不退款。
-- 从未结算的租约一直停在 open —— 那一段已经扣走，不影响任何人的余额。
CREATE TABLE IF NOT EXISTS feature_time_leases (
    id BIGSERIAL PRIMARY KEY,
    lease_no VARCHAR(40) NOT NULL,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    feature VARCHAR(64) NOT NULL,
    seconds INTEGER NOT NULL,
    used_seconds INTEGER NOT NULL DEFAULT 0,
    refunded_seconds INTEGER NOT NULL DEFAULT 0,
    status VARCHAR(16) NOT NULL DEFAULT 'open',
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    settled_at TIMESTAMPTZ NULL,
    CONSTRAINT ck_feature_time_leases_status CHECK (status IN ('open','settled','expired')),
    CONSTRAINT ck_feature_time_leases_seconds CHECK (seconds > 0 AND used_seconds >= 0 AND refunded_seconds >= 0
        AND used_seconds <= seconds AND refunded_seconds <= seconds)
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_feature_time_leases_no ON feature_time_leases(lease_no);
CREATE INDEX IF NOT EXISTS idx_feature_time_leases_user ON feature_time_leases(appid, user_id, created_at DESC);
