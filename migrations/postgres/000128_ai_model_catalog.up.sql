-- +migrate Up
-- 公开模型目录（docs/ai-gateway.md「模型目录」）：models.dev、OpenRouter、LiteLLM 合并后的整份快照，
-- 全平台共用一行。各实例读它建内存索引；刷新由抢到租约（refreshing_until）的实例做，其余实例只读。
CREATE TABLE IF NOT EXISTS ai_model_catalog (
    id               SMALLINT    PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    entries          JSONB       NOT NULL DEFAULT '[]'::jsonb,
    sources          JSONB       NOT NULL DEFAULT '[]'::jsonb,
    entry_count      INT         NOT NULL DEFAULT 0,
    fetched_at       TIMESTAMPTZ,
    refreshing_until TIMESTAMPTZ
);
INSERT INTO ai_model_catalog (id) VALUES (1) ON CONFLICT DO NOTHING;
