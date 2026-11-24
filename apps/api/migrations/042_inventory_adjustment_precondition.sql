ALTER TABLE stock_movements ADD COLUMN adjustment_precondition text
    CHECK (adjustment_precondition IS NULL OR
           (movement_type='adjustment' AND adjustment_precondition ~ '^(unrecorded|0|[1-9][0-9]{0,18})$'));
