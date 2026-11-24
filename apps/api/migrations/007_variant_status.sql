ALTER TABLE product_variants
    ADD COLUMN status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','inactive'));
