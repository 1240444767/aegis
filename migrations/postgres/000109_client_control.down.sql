-- +migrate Down
DROP TABLE IF EXISTS app_client_control_daily;
DROP TABLE IF EXISTS app_client_installs;
DROP TABLE IF EXISTS app_client_control_rules;
DROP TABLE IF EXISTS app_client_control_keys;
DROP TABLE IF EXISTS app_client_control_settings;
