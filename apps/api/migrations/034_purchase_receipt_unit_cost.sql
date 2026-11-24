ALTER TABLE purchase_receipt_items ADD COLUMN unit_cost numeric(18,2),
    ADD CHECK (unit_cost IS NULL OR (accepted_qty>0 AND unit_cost>=0 AND unit_cost<>'NaN'::numeric));
