-- +migrate Down
DROP TABLE IF EXISTS ai_gateway_requests;
DROP TABLE IF EXISTS ai_gateway_quota_usage;
DROP TABLE IF EXISTS ai_gateway_credit_ledger;
DROP TABLE IF EXISTS ai_gateway_accounts;
DROP TABLE IF EXISTS ai_gateway_keys;
DROP TABLE IF EXISTS ai_gateway_models;
DROP TABLE IF EXISTS ai_gateway_settings;
