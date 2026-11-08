-- +migrate Down
ALTER TABLE email_deliveries DROP COLUMN IF EXISTS inherited;
DROP TABLE IF EXISTS email_delivery_events;
DROP TABLE IF EXISTS email_suppressions;
DROP TABLE IF EXISTS email_policies;
