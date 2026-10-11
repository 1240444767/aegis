-- +migrate Down
DROP TABLE IF EXISTS invite_milestone_grants;
DROP TABLE IF EXISTS invite_relations;
DROP TABLE IF EXISTS invite_codes;
DROP TABLE IF EXISTS invite_settings;
