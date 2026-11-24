ALTER TABLE sale_payments ADD CONSTRAINT sale_payments_noncash_amount
    CHECK (method = 'cash' OR (amount > 0 AND cash_received IS NULL AND change_amount IS NULL));
