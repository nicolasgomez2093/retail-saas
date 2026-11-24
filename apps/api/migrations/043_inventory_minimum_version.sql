ALTER TABLE stock_minimums ADD COLUMN version uuid NOT NULL DEFAULT gen_random_uuid();
