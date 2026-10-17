-- +migrate Down
DROP TABLE IF EXISTS account_deletion_tombstones;
DROP TABLE IF EXISTS account_deletion_requests;
DROP TABLE IF EXISTS account_deletion_settings;
