-- +migrate Down
ALTER TABLE ai_gateway_models DROP COLUMN IF EXISTS capabilities;
