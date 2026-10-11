-- +migrate Up
-- 积分商城：用积分兑换会员、功能解锁、经验、余额、抽奖次数与人工发放的权益。
--
-- 三张表一张账：
--   points_store_items        商品（运营配的东西）
--   points_store_redemptions  兑换记录（扣积分、发了什么、人工发放的处理结果）
--   user_feature_unlocks      功能解锁账本 —— 与会员段并列的第三条线，不当会员也能单独拥有某个功能
--
-- 迁移器每次启动都会重跑全部 *.up.sql，因此所有语句都必须可重复执行。

-- ── 开关：一个应用一行，没有这一行按关闭处理 ──
CREATE TABLE IF NOT EXISTS points_store_settings (
    appid BIGINT PRIMARY KEY REFERENCES apps(id) ON DELETE CASCADE,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    updated_by VARCHAR(128) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ── 商品 ──
CREATE TABLE IF NOT EXISTS points_store_items (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    title VARCHAR(64) NOT NULL,
    subtitle VARCHAR(128) NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    -- 分类是运营自己起的名字（会员 / 功能 / 福利），客户端按它分组
    category VARCHAR(32) NOT NULL DEFAULT '',
    badge VARCHAR(16) NOT NULL DEFAULT '',
    cost BIGINT NOT NULL,
    original_cost BIGINT NULL,
    -- 权益清单，取值形态由 internal/domain/pointstore 的目录决定
    rewards JSONB NOT NULL DEFAULT '[]'::jsonb,
    -- 剩余库存，NULL 为不限。兑换时在锁住的行上减一，取消时加回
    stock BIGINT NULL,
    sold_count BIGINT NOT NULL DEFAULT 0,
    -- 每人限兑：limit_count 次 / limit_period，limit_count 为 0 不限
    limit_period VARCHAR(8) NOT NULL DEFAULT 'total',
    limit_count INTEGER NOT NULL DEFAULT 0,
    audience VARCHAR(16) NOT NULL DEFAULT 'all',
    min_level INTEGER NOT NULL DEFAULT 0,
    starts_at TIMESTAMPTZ NULL,
    ends_at TIMESTAMPTZ NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'active',
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_by VARCHAR(128) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_points_store_items_cost CHECK (cost > 0),
    CONSTRAINT ck_points_store_items_original_cost CHECK (original_cost IS NULL OR original_cost > cost),
    -- 库存不能减成负数（兜底：兑换在锁住的商品行上判库存，走到这里说明那段判定被改坏了）
    CONSTRAINT ck_points_store_items_stock CHECK (stock IS NULL OR stock >= 0),
    CONSTRAINT ck_points_store_items_limit CHECK (limit_count >= 0 AND limit_period IN ('total','day','week','month')),
    CONSTRAINT ck_points_store_items_audience CHECK (audience IN ('all','vip','non_vip')),
    CONSTRAINT ck_points_store_items_status CHECK (status IN ('active','inactive'))
);
CREATE INDEX IF NOT EXISTS idx_points_store_items_app ON points_store_items(appid, status, sort_order, id);

-- ── 兑换记录 ──
--
-- 标题、积分与权益都存快照：商品改价、改权益、被删除之后，旧记录说的仍是当时兑换的那一份。
CREATE TABLE IF NOT EXISTS points_store_redemptions (
    id BIGSERIAL PRIMARY KEY,
    redemption_no VARCHAR(40) NOT NULL,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    item_id BIGINT NULL REFERENCES points_store_items(id) ON DELETE SET NULL,
    item_title VARCHAR(64) NOT NULL,
    cost BIGINT NOT NULL,
    rewards JSONB NOT NULL DEFAULT '[]'::jsonb,
    results JSONB NOT NULL DEFAULT '[]'::jsonb,
    -- completed 已到账 / pending 待人工发放 / cancelled 已取消并退回积分
    status VARCHAR(16) NOT NULL,
    integral_transaction_no VARCHAR(64) NULL,
    refund_transaction_no VARCHAR(64) NULL,
    -- 客户端生成的幂等键：网络重试不会扣两次积分
    request_id VARCHAR(64) NULL,
    -- 管理员处理人工发放时写给用户看的话（兑换码、快递单号、取消原因）
    note TEXT NULL,
    handled_by VARCHAR(128) NULL,
    handled_at TIMESTAMPTZ NULL,
    device_id VARCHAR(128) NULL,
    client_ip VARCHAR(64) NULL,
    user_agent VARCHAR(256) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_points_store_redemptions_status CHECK (status IN ('completed','pending','cancelled'))
);
CREATE UNIQUE INDEX IF NOT EXISTS uq_points_store_redemptions_no ON points_store_redemptions(redemption_no);
CREATE UNIQUE INDEX IF NOT EXISTS uq_points_store_redemptions_request
    ON points_store_redemptions(appid, user_id, request_id) WHERE request_id IS NOT NULL;
-- 我的兑换记录
CREATE INDEX IF NOT EXISTS idx_points_store_redemptions_user ON points_store_redemptions(appid, user_id, created_at DESC);
-- 限兑计数：某人某件商品在某个时间之后兑了几次
CREATE INDEX IF NOT EXISTS idx_points_store_redemptions_item_user ON points_store_redemptions(item_id, user_id, created_at);
-- 管理端按状态筛选（待发放）
CREATE INDEX IF NOT EXISTS idx_points_store_redemptions_status ON points_store_redemptions(appid, status, created_at DESC);

-- ── 功能解锁 ──
--
-- 一行一次解锁。同一功能多次限时解锁时，发放时就把新的一段排在上一段之后（顺延），
-- 判定只需要「未收回且未结束」的行；ends_at 为 NULL 是永久解锁。
CREATE TABLE IF NOT EXISTS user_feature_unlocks (
    id BIGSERIAL PRIMARY KEY,
    appid BIGINT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    feature_tag VARCHAR(64) NOT NULL,
    -- points_store / admin_grant
    source VARCHAR(32) NOT NULL,
    -- 来源的单号（兑换单号），管理员授予为空
    source_ref VARCHAR(64) NULL,
    duration_days INTEGER NOT NULL DEFAULT 0,
    starts_at TIMESTAMPTZ NOT NULL,
    ends_at TIMESTAMPTZ NULL,
    revoked_at TIMESTAMPTZ NULL,
    revoked_by VARCHAR(128) NULL,
    revoke_reason VARCHAR(256) NULL,
    operator VARCHAR(128) NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_user_feature_unlocks_window CHECK (ends_at IS NULL OR ends_at > starts_at),
    CONSTRAINT ck_user_feature_unlocks_days CHECK (duration_days >= 0)
);
CREATE INDEX IF NOT EXISTS idx_user_feature_unlocks_user ON user_feature_unlocks(appid, user_id, feature_tag);
