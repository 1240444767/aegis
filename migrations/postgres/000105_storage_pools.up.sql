-- +migrate Up
-- 存储池：把多个平台级存储配置并成一个写入目标，按策略选成员，满了或出错换下一个。
CREATE TABLE IF NOT EXISTS storage_pools (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(64) NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    strategy VARCHAR(16) NOT NULL DEFAULT 'priority'
        CHECK (strategy IN ('priority', 'round_robin', 'weighted', 'least_used')),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    read_fallback BOOLEAN NOT NULL DEFAULT TRUE,
    max_attempts INT NOT NULL DEFAULT 3 CHECK (max_attempts BETWEEN 1 AND 10),
    warn_percent INT NOT NULL DEFAULT 85 CHECK (warn_percent BETWEEN 1 AND 100),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS uk_storage_pools_name ON storage_pools (name);
-- 至多一个默认池。
CREATE UNIQUE INDEX IF NOT EXISTS uk_storage_pools_default ON storage_pools (is_default) WHERE is_default;

CREATE TABLE IF NOT EXISTS storage_pool_members (
    pool_id BIGINT NOT NULL REFERENCES storage_pools(id) ON DELETE CASCADE,
    config_id BIGINT NOT NULL REFERENCES storage_configs(id) ON DELETE CASCADE,
    priority INT NOT NULL DEFAULT 0,
    weight INT NOT NULL DEFAULT 1 CHECK (weight BETWEEN 1 AND 1000),
    -- 写入上限，0 为不限。
    capacity_bytes BIGINT NOT NULL DEFAULT 0 CHECK (capacity_bytes >= 0),
    mode VARCHAR(16) NOT NULL DEFAULT 'active' CHECK (mode IN ('active', 'standby', 'readonly')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (pool_id, config_id)
);
CREATE INDEX IF NOT EXISTS idx_storage_pool_members_config ON storage_pool_members (config_id);

-- 运行事件：切换、退避、恢复、容量预警、迁移进度。只追加，按时间倒序看。
CREATE TABLE IF NOT EXISTS storage_pool_events (
    id BIGSERIAL PRIMARY KEY,
    pool_id BIGINT NULL REFERENCES storage_pools(id) ON DELETE CASCADE,
    config_id BIGINT NULL REFERENCES storage_configs(id) ON DELETE CASCADE,
    level VARCHAR(8) NOT NULL DEFAULT 'info' CHECK (level IN ('info', 'warn', 'error')),
    kind VARCHAR(32) NOT NULL,
    message TEXT NOT NULL,
    detail JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_storage_pool_events_time ON storage_pool_events (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_storage_pool_events_pool ON storage_pool_events (pool_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_storage_pool_events_config ON storage_pool_events (config_id, created_at DESC);

-- 迁移任务：把一个配置里已索引的对象复制到另一个配置，键不变。
CREATE TABLE IF NOT EXISTS storage_migrations (
    id BIGSERIAL PRIMARY KEY,
    source_config_id BIGINT NOT NULL REFERENCES storage_configs(id) ON DELETE CASCADE,
    target_config_id BIGINT NOT NULL REFERENCES storage_configs(id) ON DELETE CASCADE,
    status VARCHAR(16) NOT NULL DEFAULT 'pending'
        CHECK (status IN ('pending', 'running', 'paused', 'completed', 'failed', 'cancelled')),
    delete_source BOOLEAN NOT NULL DEFAULT FALSE,
    set_redirect BOOLEAN NOT NULL DEFAULT TRUE,
    total_objects BIGINT NOT NULL DEFAULT 0,
    total_bytes BIGINT NOT NULL DEFAULT 0,
    copied_objects BIGINT NOT NULL DEFAULT 0,
    copied_bytes BIGINT NOT NULL DEFAULT 0,
    skipped_objects BIGINT NOT NULL DEFAULT 0,
    failed_objects BIGINT NOT NULL DEFAULT 0,
    -- 断点：storage_objects.id 的游标，重启后从这里继续。
    last_object_id BIGINT NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    created_by VARCHAR(128) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at TIMESTAMPTZ NULL,
    finished_at TIMESTAMPTZ NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT ck_storage_migrations_distinct CHECK (source_config_id <> target_config_id)
);
CREATE INDEX IF NOT EXISTS idx_storage_migrations_status ON storage_migrations (status);

-- 迁移后的读取转向：引用里记的仍是源配置，读不到时去这里读。
ALTER TABLE storage_configs ADD COLUMN IF NOT EXISTS redirect_config_id BIGINT NULL
    REFERENCES storage_configs(id) ON DELETE SET NULL;

-- 迁移按 id 游标扫一个配置的对象；容量按配置汇总。
CREATE INDEX IF NOT EXISTS idx_storage_objects_config_id ON storage_objects (config_id, id);
