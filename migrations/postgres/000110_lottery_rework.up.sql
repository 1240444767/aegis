-- +migrate Up
-- 抽奖系统重构。
--
-- 旧版的问题：抽奖链路不在一个事务里（扣了积分、减了库存，写记录失败时退不回来）；
-- 日限额是「先数历史行再写」，并发能穿透；「报名」与「种子承诺 / 上链」两套机制与抽奖本身
-- 没有接上（抽奖用的随机数从未用到承诺过的种子），只是看起来可验证。
--
-- 新版：
--   lottery_activities  免费 / 积分 / 视频（激励广告场景）三种次数、保底（连续未中 N 次必中）
--   lottery_prizes      奖品发的东西与积分商城同构（reward JSONB），kind 冗余一份便于统计
--   lottery_draws       每次抽奖一行，奖品存快照；request_id 幂等；人工发放的待处理
--   lottery_user_states 每人每个活动一行：连续未中次数。抽奖时锁这一行，同一个人的抽奖串行
--
-- 迁移器每次启动都会重跑全部 *.up.sql，因此所有语句都必须可重复执行；一次性的数据搬迁
-- 放在 DO 块里，以旧列是否还在为条件。全新安装时 000024 先建出旧形状，这里同样走一遍。

-- ── 活动 ──
ALTER TABLE lottery_activities
    ADD COLUMN IF NOT EXISTS rules TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS free_daily INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS points_cost BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS points_daily INTEGER NOT NULL DEFAULT 0,
    -- 激励广告场景的 key：那个场景发抽奖次数，看完进次数余额；空为不开放视频抽奖
    ADD COLUMN IF NOT EXISTS video_scene VARCHAR(32) NOT NULL DEFAULT '',
    -- 连续 pity_threshold 次没抽中时，这一次必中 pity_prize_id；0 为关闭
    ADD COLUMN IF NOT EXISTS pity_threshold INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS pity_prize_id BIGINT NULL,
    ADD COLUMN IF NOT EXISTS sort_order INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS created_by VARCHAR(128) NULL;

-- 开始 / 结束时间可以不填（长期活动）
ALTER TABLE lottery_activities ALTER COLUMN start_time DROP NOT NULL;
ALTER TABLE lottery_activities ALTER COLUMN end_time DROP NOT NULL;
UPDATE lottery_activities SET description = '' WHERE description IS NULL;
ALTER TABLE lottery_activities ALTER COLUMN description SET DEFAULT '';
ALTER TABLE lottery_activities ALTER COLUMN description SET NOT NULL;
ALTER TABLE lottery_activities ALTER COLUMN ui_mode SET DEFAULT 'grid';

-- ── 奖品 ──
ALTER TABLE lottery_prizes
    ADD COLUMN IF NOT EXISTS kind VARCHAR(32) NOT NULL DEFAULT 'none',
    -- 发的东西，取值形态由 internal/domain/lottery 的目录决定（与积分商城的权益同构）
    ADD COLUMN IF NOT EXISTS reward JSONB NOT NULL DEFAULT '{"type":"none"}'::jsonb;

-- ── 抽奖记录 ──
ALTER TABLE lottery_draws
    ADD COLUMN IF NOT EXISTS draw_no VARCHAR(40) NULL,
    ADD COLUMN IF NOT EXISTS appid BIGINT NULL,
    -- 奖品快照：奖品被改、被删之后，记录说的仍是当时抽中的那一份
    ADD COLUMN IF NOT EXISTS prize_name VARCHAR(64) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS prize_kind VARCHAR(32) NOT NULL DEFAULT 'none',
    ADD COLUMN IF NOT EXISTS reward JSONB NOT NULL DEFAULT '{"type":"none"}'::jsonb,
    ADD COLUMN IF NOT EXISTS position INTEGER NOT NULL DEFAULT 0,
    -- 用的哪一种次数：free / bonus / points
    ADD COLUMN IF NOT EXISTS source VARCHAR(16) NOT NULL DEFAULT 'free',
    ADD COLUMN IF NOT EXISTS points_cost BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS integral_transaction_no VARCHAR(64) NULL,
    -- 这一次是保底触发的
    ADD COLUMN IF NOT EXISTS pity BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS results JSONB NOT NULL DEFAULT '[]'::jsonb,
    -- 管理员处理人工发放时写给用户看的话
    ADD COLUMN IF NOT EXISTS note TEXT NULL,
    ADD COLUMN IF NOT EXISTS handled_by VARCHAR(128) NULL,
    ADD COLUMN IF NOT EXISTS handled_at TIMESTAMPTZ NULL,
    -- 客户端生成的幂等键：网络重试不会多抽一次
    ADD COLUMN IF NOT EXISTS request_id VARCHAR(64) NULL,
    ADD COLUMN IF NOT EXISTS device_id VARCHAR(128) NULL,
    ADD COLUMN IF NOT EXISTS client_ip VARCHAR(64) NULL,
    ADD COLUMN IF NOT EXISTS user_agent VARCHAR(256) NULL;

-- ── 一次性搬迁：旧列还在时才做 ──
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_schema = current_schema() AND table_name = 'lottery_activities' AND column_name = 'cost_type') THEN
        -- 旧版 free + daily_limit=0 表示不限次免费，新版没有「不限」这一档，按每天 1 次搬过来
        UPDATE lottery_activities SET
            free_daily   = CASE WHEN cost_type = 'points' THEN 0 ELSE LEAST(GREATEST(daily_limit, 1), 100) END,
            points_cost  = CASE WHEN cost_type = 'points' THEN GREATEST(cost_amount, 0) ELSE 0 END,
            points_daily = CASE WHEN cost_type = 'points' THEN LEAST(GREATEST(daily_limit, 0), 1000) ELSE 0 END,
            ui_mode      = CASE WHEN ui_mode IN ('grid', 'wheel') THEN ui_mode ELSE 'grid' END,
            status       = CASE WHEN status IN ('draft', 'active', 'paused', 'ended') THEN status ELSE 'draft' END;
        ALTER TABLE lottery_activities
            DROP COLUMN IF EXISTS join_mode,
            DROP COLUMN IF EXISTS auto_join_rules,
            DROP COLUMN IF EXISTS cost_type,
            DROP COLUMN IF EXISTS cost_amount,
            DROP COLUMN IF EXISTS daily_limit,
            DROP COLUMN IF EXISTS total_limit,
            DROP COLUMN IF EXISTS seed_hash,
            DROP COLUMN IF EXISTS seed_value,
            DROP COLUMN IF EXISTS chain_tx_hash,
            DROP COLUMN IF EXISTS chain_network;
    END IF;

    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_schema = current_schema() AND table_name = 'lottery_prizes' AND column_name = 'value') THEN
        -- 旧类型 points / experience 带数字值的照搬；名字是「谢谢参与」一类的算未中；其余（item / coupon / custom）改人工发放
        UPDATE lottery_prizes SET
            kind = CASE
                WHEN type IN ('points', 'experience') AND value ~ '^[0-9]{1,9}$' AND value::bigint > 0 THEN type
                WHEN name LIKE '%谢谢%' OR name LIKE '%再接再厉%' OR name LIKE '%未中奖%' THEN 'none'
                ELSE 'manual' END;
        UPDATE lottery_prizes SET reward = CASE
                WHEN kind IN ('points', 'experience') THEN jsonb_build_object('type', kind, 'amount', value::bigint)
                WHEN kind = 'manual' THEN jsonb_build_object('type', 'manual', 'text', LEFT(COALESCE(NULLIF(value, ''), name), 64))
                ELSE '{"type":"none"}'::jsonb END;
        ALTER TABLE lottery_prizes
            DROP COLUMN IF EXISTS type,
            DROP COLUMN IF EXISTS value,
            DROP COLUMN IF EXISTS extra;
    END IF;

    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_schema = current_schema() AND table_name = 'lottery_draws' AND column_name = 'prize_snapshot') THEN
        UPDATE lottery_draws d SET
            appid      = a.appid,
            draw_no    = 'LTD' || to_char(d.created_at AT TIME ZONE 'UTC', 'YYYYMMDDHH24MISS') || lpad(d.id::text, 10, '0'),
            prize_name = LEFT(COALESCE(d.prize_snapshot->>'name', ''), 64),
            status     = CASE WHEN d.status = 'claimed' THEN 'fulfilled' ELSE 'granted' END,
            handled_at = CASE WHEN d.status = 'claimed' THEN d.claimed_at ELSE NULL END
        FROM lottery_activities a
        WHERE a.id = d.activity_id;
        UPDATE lottery_draws d SET
            prize_name = CASE WHEN d.prize_name = '' THEN LEFT(p.name, 64) ELSE d.prize_name END,
            prize_kind = p.kind,
            reward     = p.reward,
            position   = p.position,
            status     = CASE WHEN d.status = 'fulfilled' THEN 'fulfilled'
                              WHEN p.kind = 'manual' THEN 'pending' ELSE 'granted' END
        FROM lottery_prizes p
        WHERE p.id = d.prize_id;
        -- 用户已被删除的记录没有主人了（旧表没有外键，删用户时它们被留了下来）
        DELETE FROM lottery_draws d WHERE NOT EXISTS (SELECT 1 FROM users u WHERE u.id = d.user_id);
        ALTER TABLE lottery_draws
            DROP COLUMN IF EXISTS prize_snapshot,
            DROP COLUMN IF EXISTS draw_seed,
            DROP COLUMN IF EXISTS draw_proof,
            DROP COLUMN IF EXISTS claimed_at;
        ALTER TABLE lottery_draws ALTER COLUMN appid SET NOT NULL;
        ALTER TABLE lottery_draws ALTER COLUMN draw_no SET NOT NULL;
    END IF;
END $$;

ALTER TABLE lottery_draws ALTER COLUMN status SET DEFAULT 'granted';

-- 报名与种子承诺：新版没有这两个概念
DROP TABLE IF EXISTS lottery_participants;
DROP TABLE IF EXISTS lottery_seed_commitments;

-- ── 每人每个活动的状态 ──
CREATE TABLE IF NOT EXISTS lottery_user_states (
    activity_id BIGINT NOT NULL REFERENCES lottery_activities(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- 连续没抽中（谢谢参与）的次数，抽中任何奖品清零
    miss_streak INTEGER NOT NULL DEFAULT 0,
    total_draws BIGINT NOT NULL DEFAULT 0,
    last_draw_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (activity_id, user_id)
);

-- ── 约束（先删后加，可重复执行；外键用 NOT VALID，已有数据由上面的搬迁保证）──
ALTER TABLE lottery_activities DROP CONSTRAINT IF EXISTS ck_lottery_activities_ui_mode;
ALTER TABLE lottery_activities ADD CONSTRAINT ck_lottery_activities_ui_mode CHECK (ui_mode IN ('grid', 'wheel'));
ALTER TABLE lottery_activities DROP CONSTRAINT IF EXISTS ck_lottery_activities_status;
ALTER TABLE lottery_activities ADD CONSTRAINT ck_lottery_activities_status
    CHECK (status IN ('draft', 'active', 'paused', 'ended'));
ALTER TABLE lottery_activities DROP CONSTRAINT IF EXISTS ck_lottery_activities_counts;
ALTER TABLE lottery_activities ADD CONSTRAINT ck_lottery_activities_counts
    CHECK (free_daily >= 0 AND points_cost >= 0 AND points_daily >= 0 AND pity_threshold >= 0);
ALTER TABLE lottery_activities DROP CONSTRAINT IF EXISTS fk_lottery_activities_pity_prize;
ALTER TABLE lottery_activities ADD CONSTRAINT fk_lottery_activities_pity_prize
    FOREIGN KEY (pity_prize_id) REFERENCES lottery_prizes(id) ON DELETE SET NULL NOT VALID;

ALTER TABLE lottery_prizes DROP CONSTRAINT IF EXISTS ck_lottery_prizes_stock;
-- 库存不能抽成负数（兜底：抽奖在条件 UPDATE 里判库存，走到这里说明那段判定被改坏了）
ALTER TABLE lottery_prizes ADD CONSTRAINT ck_lottery_prizes_stock
    CHECK (weight >= 0 AND used >= 0 AND (quantity < 0 OR used <= quantity));

-- 删除奖品不删记录：记录里有快照
ALTER TABLE lottery_draws DROP CONSTRAINT IF EXISTS lottery_draws_prize_id_fkey;
ALTER TABLE lottery_draws ADD CONSTRAINT lottery_draws_prize_id_fkey
    FOREIGN KEY (prize_id) REFERENCES lottery_prizes(id) ON DELETE SET NULL NOT VALID;
ALTER TABLE lottery_draws DROP CONSTRAINT IF EXISTS fk_lottery_draws_user;
ALTER TABLE lottery_draws ADD CONSTRAINT fk_lottery_draws_user
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE NOT VALID;
ALTER TABLE lottery_draws DROP CONSTRAINT IF EXISTS fk_lottery_draws_app;
ALTER TABLE lottery_draws ADD CONSTRAINT fk_lottery_draws_app
    FOREIGN KEY (appid) REFERENCES apps(id) ON DELETE CASCADE NOT VALID;
ALTER TABLE lottery_draws DROP CONSTRAINT IF EXISTS ck_lottery_draws_status;
ALTER TABLE lottery_draws ADD CONSTRAINT ck_lottery_draws_status CHECK (status IN ('granted', 'pending', 'fulfilled'));
ALTER TABLE lottery_draws DROP CONSTRAINT IF EXISTS ck_lottery_draws_source;
ALTER TABLE lottery_draws ADD CONSTRAINT ck_lottery_draws_source CHECK (source IN ('free', 'bonus', 'points'));

-- ── 索引 ──
CREATE UNIQUE INDEX IF NOT EXISTS uq_lottery_draws_no ON lottery_draws(draw_no);
CREATE UNIQUE INDEX IF NOT EXISTS uq_lottery_draws_request
    ON lottery_draws(activity_id, user_id, request_id) WHERE request_id IS NOT NULL;
-- 今天用了几次免费 / 积分次数、我的记录
CREATE INDEX IF NOT EXISTS idx_lottery_draws_user_time ON lottery_draws(activity_id, user_id, created_at DESC);
-- 管理端记录列表与统计
CREATE INDEX IF NOT EXISTS idx_lottery_draws_app_time ON lottery_draws(appid, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_lottery_draws_activity_time ON lottery_draws(activity_id, created_at);
CREATE INDEX IF NOT EXISTS idx_lottery_draws_pending ON lottery_draws(appid, created_at) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS idx_lottery_activities_app_sort ON lottery_activities(appid, status, sort_order, id);
