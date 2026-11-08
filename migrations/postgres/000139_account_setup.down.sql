-- +migrate Down
DROP TABLE IF EXISTS user_account_setup;
UPDATE app_auth_protocol_policies SET identifiers = array_remove(identifiers, 'id');
