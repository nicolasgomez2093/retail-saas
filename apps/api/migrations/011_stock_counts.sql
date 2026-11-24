ALTER TABLE stock_balances ADD COLUMN revision bigint NOT NULL DEFAULT 0 CHECK (revision >= 0);

CREATE FUNCTION increment_stock_balance_revision() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    NEW.revision := OLD.revision + 1;
    RETURN NEW;
END;
$$;

CREATE TRIGGER stock_balance_revision
    BEFORE UPDATE OF quantity_on_hand, quantity_reserved ON stock_balances
    FOR EACH ROW EXECUTE FUNCTION increment_stock_balance_revision();

CREATE TABLE stock_counts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    location_id uuid NOT NULL,
    status text NOT NULL DEFAULT 'DRAFT' CHECK (status IN ('DRAFT','COMPLETED','CANCELLED')),
    idempotency_key text NOT NULL,
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    completed_by uuid,
    completed_at timestamptz,
    cancelled_by uuid,
    cancelled_at timestamptz,
    note text NOT NULL DEFAULT '',
    CHECK ((completed_by IS NULL) = (completed_at IS NULL)),
    CHECK ((cancelled_by IS NULL) = (cancelled_at IS NULL)),
    CHECK ((status = 'COMPLETED') = (completed_at IS NOT NULL)),
    CHECK ((status = 'CANCELLED') = (cancelled_at IS NOT NULL)),
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,idempotency_key),
    FOREIGN KEY (tenant_id,location_id) REFERENCES stock_locations(tenant_id,id),
    FOREIGN KEY (tenant_id,created_by) REFERENCES users(tenant_id,id),
    FOREIGN KEY (tenant_id,completed_by) REFERENCES users(tenant_id,id),
    FOREIGN KEY (tenant_id,cancelled_by) REFERENCES users(tenant_id,id)
);

CREATE INDEX stock_counts_by_location ON stock_counts(tenant_id,location_id,created_at DESC,id DESC);

CREATE TABLE stock_count_items (
    tenant_id uuid NOT NULL,
    count_id uuid NOT NULL,
    variant_id uuid NOT NULL,
    sku_snapshot text NOT NULL,
    expected_quantity integer NOT NULL CHECK (expected_quantity >= 0),
    expected_revision bigint,
    counted_quantity integer CHECK (counted_quantity >= 0),
    PRIMARY KEY (tenant_id,count_id,variant_id),
    FOREIGN KEY (tenant_id,count_id) REFERENCES stock_counts(tenant_id,id),
    FOREIGN KEY (tenant_id,variant_id) REFERENCES product_variants(tenant_id,id)
);

ALTER TABLE stock_movements ADD COLUMN count_id uuid,
    ADD FOREIGN KEY (tenant_id,count_id) REFERENCES stock_counts(tenant_id,id);

CREATE UNIQUE INDEX stock_movements_by_count ON stock_movements(tenant_id,count_id,variant_id)
    WHERE count_id IS NOT NULL;
