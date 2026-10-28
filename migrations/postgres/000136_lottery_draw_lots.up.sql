-- +migrate Up
-- 抽奖次数分批记账（docs/lottery.md「抽奖次数」）。
--
-- 以前 lottery_draw_grants.balance 是一个不分来源的整数：看视频送的、积分换的、卡密充的混在一起，
-- 用掉的是哪一次、还剩的是从哪来的都说不清。现在每次发放是一批（lottery_draw_lots），每次增减一条流水
-- （lottery_draw_ledger）；lottery_draw_grants 仍是这个人全部批次的汇总，抽奖时锁它这一行。
--
-- 扣除顺序：今日免费 → 赠送的批次（paid = false，先到先用）→ 兑换的批次（paid = true，先到先用）→ 积分。
--
-- 迁移器每次启动都会重跑全部 *.up.sql，语句必须可重复执行。

CREATE TABLE IF NOT EXISTS lottery_draw_lots (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- rewarded_ad / points_store / cardkey / invite / leaderboard / lottery / admin / legacy
    origin VARCHAR(32) NOT NULL,
    -- 用积分或钱换来的：排在赠送的之后用
    paid BOOLEAN NOT NULL DEFAULT FALSE,
    amount INTEGER NOT NULL,
    remaining INTEGER NOT NULL,
    -- 来源单号（兑换单号、卡号、抽奖单号、激励广告 transId…）
    ref VARCHAR(64) NOT NULL DEFAULT '',
    title VARCHAR(128) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
ALTER TABLE lottery_draw_lots DROP CONSTRAINT IF EXISTS ck_lottery_draw_lots_remaining;
ALTER TABLE lottery_draw_lots ADD CONSTRAINT ck_lottery_draw_lots_remaining
    CHECK (amount > 0 AND remaining >= 0 AND remaining <= amount);
-- 按使用顺序取下一批（只看还有剩余的）
CREATE INDEX IF NOT EXISTS idx_lottery_draw_lots_open
    ON lottery_draw_lots(appid, user_id, paid, id) WHERE remaining > 0;
CREATE INDEX IF NOT EXISTS idx_lottery_draw_lots_origin ON lottery_draw_lots(appid, origin, created_at);

CREATE TABLE IF NOT EXISTS lottery_draw_ledger (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    lot_id BIGINT NULL REFERENCES lottery_draw_lots(id) ON DELETE SET NULL,
    -- grant / consume / deduct
    kind VARCHAR(16) NOT NULL,
    origin VARCHAR(32) NOT NULL,
    paid BOOLEAN NOT NULL DEFAULT FALSE,
    delta INTEGER NOT NULL,
    balance_after BIGINT NOT NULL,
    ref VARCHAR(64) NOT NULL DEFAULT '',
    title VARCHAR(128) NOT NULL DEFAULT '',
    operator VARCHAR(128) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
ALTER TABLE lottery_draw_ledger DROP CONSTRAINT IF EXISTS ck_lottery_draw_ledger_kind;
ALTER TABLE lottery_draw_ledger ADD CONSTRAINT ck_lottery_draw_ledger_kind
    CHECK (kind IN ('grant', 'consume', 'deduct') AND delta <> 0);
CREATE INDEX IF NOT EXISTS idx_lottery_draw_ledger_user ON lottery_draw_ledger(appid, user_id, id DESC);
CREATE INDEX IF NOT EXISTS idx_lottery_draw_ledger_app ON lottery_draw_ledger(appid, created_at DESC);

-- 抽奖记录：source = bonus 时用的是哪一批
ALTER TABLE lottery_draws
    ADD COLUMN IF NOT EXISTS ticket_lot_id BIGINT NULL,
    ADD COLUMN IF NOT EXISTS ticket_origin VARCHAR(32) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS ticket_paid BOOLEAN NOT NULL DEFAULT FALSE;

-- 分批记账之前的余额：差多少补一批 legacy（赠送档）。可重复执行 —— 已对齐的人差额为 0，不会再补。
-- 抽奖与发放时还会在事务里再对一次（lotteryReconcileLotsTx），滚动发布期间旧版本写进汇总的次数也接得上。
WITH inserted AS (
    INSERT INTO lottery_draw_lots (appid, user_id, origin, paid, amount, remaining, title, created_at)
    SELECT g.appid, g.user_id, 'legacy', FALSE, g.balance - COALESCE(l.open, 0), g.balance - COALESCE(l.open, 0),
           '早期抽奖次数', g.created_at
    FROM lottery_draw_grants g
    LEFT JOIN (
        SELECT appid, user_id, SUM(remaining) AS open
        FROM lottery_draw_lots WHERE remaining > 0 GROUP BY appid, user_id
    ) l ON l.appid = g.appid AND l.user_id = g.user_id
    WHERE g.balance - COALESCE(l.open, 0) > 0
    RETURNING id, appid, user_id, amount, created_at
)
INSERT INTO lottery_draw_ledger (appid, user_id, lot_id, kind, origin, paid, delta, balance_after, title, created_at)
SELECT i.appid, i.user_id, i.id, 'grant', 'legacy', FALSE, i.amount, g.balance, '早期抽奖次数', i.created_at
FROM inserted i
JOIN lottery_draw_grants g ON g.appid = i.appid AND g.user_id = i.user_id;
