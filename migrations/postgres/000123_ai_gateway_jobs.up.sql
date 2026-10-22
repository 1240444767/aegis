-- +migrate Up
-- AI 中转异步任务（docs/ai-gateway.md「异步任务」）：生图、改图、转写这类一两分钟的调用，
-- 提交后立刻拿到任务编号，结果写在这里由客户端轮询取回 —— 不再让一个 HTTP 请求挂着等，
-- 中间任何一层反向代理的读超时（常见 60–100 秒）都会把它切断。
-- 任务与调用记录一一对应（编号相同），计费仍走调用记录；结果在完成后保留一段时间供取回。
CREATE TABLE IF NOT EXISTS ai_gateway_jobs (
    request_id   BIGINT       PRIMARY KEY REFERENCES ai_gateway_requests(id) ON DELETE CASCADE,
    request_no   VARCHAR(40)  NOT NULL,
    appid        BIGINT       NOT NULL,
    user_id      BIGINT       NOT NULL,
    endpoint     VARCHAR(32)  NOT NULL,
    status       VARCHAR(16)  NOT NULL DEFAULT 'in_progress',
    http_status  INT          NOT NULL DEFAULT 0,
    content_type VARCHAR(128) NOT NULL DEFAULT '',
    -- 交给客户端的响应体原样（JSON 或音频等二进制）。
    result       BYTEA,
    -- 失败时的错误信封：{message, type, code, aegis_code, status}。
    error        JSONB,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    finished_at  TIMESTAMPTZ,
    -- 进行中：超过即视为中断；已结束：超过即删除结果。
    expires_at   TIMESTAMPTZ  NOT NULL,
    CONSTRAINT ck_ai_gateway_jobs_status CHECK (status IN ('in_progress', 'completed', 'failed', 'canceled'))
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_ai_gateway_jobs_no ON ai_gateway_jobs (request_no);
CREATE INDEX IF NOT EXISTS idx_ai_gateway_jobs_expires ON ai_gateway_jobs (expires_at);
