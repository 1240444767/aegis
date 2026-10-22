-- +migrate Up
-- AI 中转调用追踪（docs/ai-gateway.md「调用追踪」）：
-- 记录行上加列表要显示的摘要，详情（每一次上游尝试、请求与响应内容摘要）另放一张表。
ALTER TABLE ai_gateway_requests
    ADD COLUMN IF NOT EXISTS attempts            SMALLINT     NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS first_token_ms      INT          NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS finish_reason       VARCHAR(32)  NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS upstream_request_id VARCHAR(128) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS request_bytes       BIGINT       NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS response_bytes      BIGINT       NOT NULL DEFAULT 0;

-- 控制台按通道筛选。
CREATE INDEX IF NOT EXISTS idx_ai_gateway_requests_config ON ai_gateway_requests (appid, config_id, created_at DESC);

CREATE TABLE IF NOT EXISTS ai_gateway_request_traces (
    request_id BIGINT      PRIMARY KEY REFERENCES ai_gateway_requests(id) ON DELETE CASCADE,
    -- 每一次上游尝试：通道、URL（不含查询串）、HTTP 状态、上游请求编号、首包与首字耗时、字节数、限流余量、错误。
    attempts   JSONB       NOT NULL DEFAULT '[]'::jsonb,
    -- 请求与响应的摘要：参数原样、长文本截断、base64 换占位；设置 redactContent 时不留正文。
    request    JSONB       NOT NULL DEFAULT '{}'::jsonb,
    response   JSONB       NOT NULL DEFAULT '{}'::jsonb,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
