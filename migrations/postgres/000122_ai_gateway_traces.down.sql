-- +migrate Down
DROP TABLE IF EXISTS ai_gateway_request_traces;
DROP INDEX IF EXISTS idx_ai_gateway_requests_config;
ALTER TABLE ai_gateway_requests
    DROP COLUMN IF EXISTS attempts,
    DROP COLUMN IF EXISTS first_token_ms,
    DROP COLUMN IF EXISTS finish_reason,
    DROP COLUMN IF EXISTS upstream_request_id,
    DROP COLUMN IF EXISTS request_bytes,
    DROP COLUMN IF EXISTS response_bytes;
