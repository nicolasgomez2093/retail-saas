ALTER TABLE stock_counts
    ADD COLUMN draft_workflow boolean NOT NULL DEFAULT false,
    ADD COLUMN revision bigint NOT NULL DEFAULT 0 CHECK (revision >= 0),
    ADD COLUMN creator_snapshot text,
    ADD COLUMN closer_snapshot text,
    ADD COLUMN location_snapshot text,
    ADD COLUMN store_snapshot text,
    ADD COLUMN close_key text,
    ADD COLUMN close_fingerprint text,
    ADD CHECK ((close_key IS NULL) = (close_fingerprint IS NULL)),
    ADD CHECK (close_key IS NULL OR status = 'COMPLETED');

CREATE UNIQUE INDEX stock_count_close_keys ON stock_counts(tenant_id,close_key) WHERE close_key IS NOT NULL;

ALTER TABLE stock_count_items
    ADD COLUMN product_snapshot text,
    ADD COLUMN size_snapshot text,
    ADD COLUMN color_snapshot text,
    ADD COLUMN note text NOT NULL DEFAULT '' CHECK (char_length(note) <= 300);
