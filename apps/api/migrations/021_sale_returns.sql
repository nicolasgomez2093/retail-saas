CREATE TABLE sale_returns (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    store_id uuid NOT NULL,
    sale_id uuid NOT NULL,
    reason text NOT NULL CHECK (char_length(btrim(reason)) BETWEEN 1 AND 300),
    reference_amount numeric(18,2) NOT NULL CHECK (reference_amount >= 0 AND reference_amount <> 'NaN'::numeric),
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    idempotency_key text NOT NULL,
    request_hash bytea NOT NULL CHECK (length(request_hash)=32),
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,store_id,sale_id,id),
    UNIQUE (tenant_id,idempotency_key),
    FOREIGN KEY (tenant_id,store_id,sale_id) REFERENCES sales(tenant_id,store_id,id),
    FOREIGN KEY (tenant_id,created_by) REFERENCES users(tenant_id,id)
);
CREATE INDEX sale_returns_history ON sale_returns(tenant_id,store_id,sale_id,created_at DESC,id DESC);

CREATE TABLE sale_return_items (
    tenant_id uuid NOT NULL,
    store_id uuid NOT NULL,
    sale_id uuid NOT NULL,
    return_id uuid NOT NULL,
    variant_id uuid NOT NULL,
    location_id uuid NOT NULL,
    location_name text NOT NULL,
    disposition text NOT NULL CHECK (disposition IN ('sellable','quarantine')),
    quantity integer NOT NULL CHECK (quantity > 0),
    PRIMARY KEY (tenant_id,return_id,location_id,variant_id),
    FOREIGN KEY (tenant_id,store_id,sale_id,return_id) REFERENCES sale_returns(tenant_id,store_id,sale_id,id),
    FOREIGN KEY (tenant_id,sale_id,variant_id) REFERENCES sale_items(tenant_id,sale_id,variant_id),
    FOREIGN KEY (tenant_id,store_id,location_id) REFERENCES stock_locations(tenant_id,store_id,id)
);
CREATE INDEX sale_return_items_by_sale ON sale_return_items(tenant_id,sale_id,variant_id);

ALTER TABLE stock_movements ADD COLUMN return_id uuid,
    ADD FOREIGN KEY (tenant_id,return_id) REFERENCES sale_returns(tenant_id,id),
    ADD CHECK ((movement_type='sale_return')=(return_id IS NOT NULL)),
    ADD CHECK (movement_type<>'sale_return' OR (quantity_delta>0 AND sale_id IS NOT NULL));
CREATE UNIQUE INDEX stock_sale_return_movement ON stock_movements(tenant_id,return_id,location_id,variant_id)
    WHERE return_id IS NOT NULL;

CREATE FUNCTION reject_sale_return_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'Las recepciones de devoluciones son inmutables';
END;
$$;

CREATE TRIGGER sale_returns_immutable
    BEFORE UPDATE OR DELETE ON sale_returns
    FOR EACH ROW EXECUTE FUNCTION reject_sale_return_change();

CREATE TRIGGER sale_return_items_immutable
    BEFORE UPDATE OR DELETE ON sale_return_items
    FOR EACH ROW EXECUTE FUNCTION reject_sale_return_change();
