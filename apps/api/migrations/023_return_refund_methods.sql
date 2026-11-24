ALTER TABLE sale_return_refunds
    ADD COLUMN method text NOT NULL DEFAULT 'cash' CHECK (method IN ('cash','transfer','debit','credit','qr'));

ALTER TABLE sale_return_refunds ALTER COLUMN method DROP DEFAULT;
