-- +migrate Up
-- 积分排行榜：设置、匿名偏好、移出名单、周期榜的结算快照与快照条目。规则见 docs/leaderboard.md。
--
--   leaderboard_settings          应用的排行榜配置，一个应用一行；没有这一行按关闭处理
--   leaderboard_preferences       用户的「匿名上榜」偏好
--   leaderboard_exclusions        管理员移出排行榜的人（刷分、测试号），对所有榜生效，含未结算的周期
--   leaderboard_snapshots         周期榜（日 / 周 / 月）每期结束后的结算快照，(appid, board, period_key) 唯一，
--                                 多实例同时结算时靠 ON CONFLICT DO NOTHING 抢占
--   leaderboard_snapshot_entries  快照里的前 topLimit 名：展示名、头像、匿名在结算那一刻冻结；
--                                 奖励按结算时的档位写入，由后台循环 SKIP LOCKED 认领发放
--
-- 迁移器每次启动都会重跑全部 *.up.sql，因此所有语句都必须可重复执行。

CREATE TABLE IF NOT EXISTS leaderboard_settings (
    appid BIGINT PRIMARY KEY REFERENCES apps(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    -- 其余配置（榜单、展示名次、门槛、展示身份、匿名、奖励档位），形状由 internal/domain/leaderboard 的 Config 决定
    config JSONB NOT NULL DEFAULT '{}'::jsonb,
    updated_by VARCHAR(128) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS leaderboard_preferences (
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    anonymous BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (appid, user_id)
);

CREATE TABLE IF NOT EXISTS leaderboard_exclusions (
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    reason VARCHAR(256) NOT NULL DEFAULT '',
    operator VARCHAR(128) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (appid, user_id)
);

CREATE INDEX IF NOT EXISTS idx_leaderboard_exclusions_app_time
    ON leaderboard_exclusions (appid, created_at DESC);

CREATE TABLE IF NOT EXISTS leaderboard_snapshots (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    board VARCHAR(32) NOT NULL,
    -- 日 2026-10-19 / 周 2026-W43 / 月 2026-10
    period_key VARCHAR(16) NOT NULL,
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    -- settling 结算中（与条目写入同一个事务，提交后即为 settled）/ settled 已结算
    status VARCHAR(16) NOT NULL DEFAULT 'settling',
    -- 参与者人数（取值 ≥ minValue 的全部，不限 topLimit）
    participants BIGINT NOT NULL DEFAULT 0,
    -- 结算时生效的奖励档位（之后改设置不影响这一期）
    reward_tiers JSONB NOT NULL DEFAULT '[]'::jsonb,
    -- system 为后台循环，其余为手动结算的管理员
    settled_by VARCHAR(128) NOT NULL DEFAULT 'system',
    settled_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT uq_leaderboard_snapshots_period UNIQUE (appid, board, period_key),
    CONSTRAINT chk_leaderboard_snapshots_status CHECK (status IN ('settling', 'settled'))
);

CREATE INDEX IF NOT EXISTS idx_leaderboard_snapshots_board_time
    ON leaderboard_snapshots (appid, board, period_end DESC);

CREATE TABLE IF NOT EXISTS leaderboard_snapshot_entries (
    snapshot_id BIGINT NOT NULL REFERENCES leaderboard_snapshots(id) ON DELETE CASCADE,
    appid BIGINT NOT NULL,
    -- 不挂外键：用户注销后往期榜单照旧（展示名已冻结），待发的奖励在发放时记为 skipped
    user_id BIGINT NOT NULL,
    -- 名次（并列同名次）与展示顺序（同名次内先到先得，再按用户 ID）
    rank INTEGER NOT NULL,
    position INTEGER NOT NULL,
    value BIGINT NOT NULL,
    display_name VARCHAR(128) NOT NULL DEFAULT '',
    -- 头像原值（storage:// 引用或外链），出网时再翻成永久地址；匿名的人为空
    avatar TEXT NOT NULL DEFAULT '',
    anonymous BOOLEAN NOT NULL DEFAULT FALSE,
    level_name VARCHAR(64) NOT NULL DEFAULT '',
    -- 应发的权益（结算时按档位冻结）
    rewards JSONB NOT NULL DEFAULT '[]'::jsonb,
    -- none / pending / granted / failed / skipped
    reward_status VARCHAR(16) NOT NULL DEFAULT 'none',
    -- 先写入「应发什么」，发放后补上流水号
    reward_results JSONB NOT NULL DEFAULT '[]'::jsonb,
    reward_error TEXT NOT NULL DEFAULT '',
    granted_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (snapshot_id, user_id),
    CONSTRAINT chk_leaderboard_entries_reward_status
        CHECK (reward_status IN ('none', 'pending', 'granted', 'failed', 'skipped'))
);

CREATE INDEX IF NOT EXISTS idx_leaderboard_entries_position
    ON leaderboard_snapshot_entries (snapshot_id, position);

-- 我的奖励记录 / 往期列表里的「我的名次」
CREATE INDEX IF NOT EXISTS idx_leaderboard_entries_user
    ON leaderboard_snapshot_entries (appid, user_id, snapshot_id DESC);

-- 结算循环认领待发放的条目
CREATE INDEX IF NOT EXISTS idx_leaderboard_entries_pending
    ON leaderboard_snapshot_entries (snapshot_id, rank)
    WHERE reward_status = 'pending';

-- 周期榜「获得的积分」：按应用与时间范围扫正向的已完成流水，按人求和。
-- INCLUDE 让这条聚合只走索引（不回表）；部分索引只收 amount > 0 的那一半流水。
CREATE INDEX IF NOT EXISTS idx_integral_transactions_earned
    ON integral_transactions (appid, created_at)
    INCLUDE (user_id, amount, category, source_type, source_id)
    WHERE amount > 0 AND status = 'completed';
