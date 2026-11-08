-- +migrate Down
ALTER TABLE lottery_draws
    DROP COLUMN IF EXISTS ticket_lot_id,
    DROP COLUMN IF EXISTS ticket_origin,
    DROP COLUMN IF EXISTS ticket_paid;
DROP TABLE IF EXISTS lottery_draw_ledger;
DROP TABLE IF EXISTS lottery_draw_lots;
