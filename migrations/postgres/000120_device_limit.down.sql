-- +migrate Down
DROP TABLE IF EXISTS device_limit_events;
DROP TABLE IF EXISTS user_login_devices;
DROP TABLE IF EXISTS user_device_limit_overrides;
