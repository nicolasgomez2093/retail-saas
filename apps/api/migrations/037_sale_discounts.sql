ALTER TABLE sale_items
    ADD COLUMN discount_percent smallint NOT NULL DEFAULT 0 CHECK (discount_percent BETWEEN 0 AND 80),
    ADD COLUMN list_unit_price numeric(18,2),
    ADD CHECK (
        (discount_percent = 0 AND list_unit_price IS NULL)
        OR (discount_percent > 0 AND list_unit_price IS NOT NULL AND list_unit_price >= unit_price AND list_unit_price <> 'NaN'::numeric)
    );
