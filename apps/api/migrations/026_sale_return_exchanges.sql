CREATE TABLE sale_return_exchanges (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    store_id uuid NOT NULL,
    sale_id uuid NOT NULL,
    return_id uuid NOT NULL,
    location_id uuid NOT NULL,
    location_name text NOT NULL,
    total numeric(18,2) NOT NULL CHECK (total >= 0 AND total <> 'NaN'::numeric),
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    idempotency_key text NOT NULL,
    request_hash bytea NOT NULL CHECK (length(request_hash)=32),
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,store_id,sale_id,return_id),
    UNIQUE (tenant_id,idempotency_key),
    FOREIGN KEY (tenant_id,store_id,sale_id,return_id) REFERENCES sale_returns(tenant_id,store_id,sale_id,id),
    FOREIGN KEY (tenant_id,store_id,location_id) REFERENCES stock_locations(tenant_id,store_id,id),
    FOREIGN KEY (tenant_id,created_by) REFERENCES users(tenant_id,id)
);

CREATE TABLE sale_return_exchange_items (
    tenant_id uuid NOT NULL,
    exchange_id uuid NOT NULL,
    variant_id uuid NOT NULL,
    product_name text NOT NULL,
    sku text NOT NULL,
    size text NOT NULL,
    color text NOT NULL,
    quantity integer NOT NULL CHECK (quantity > 0),
    unit_price numeric(18,2) NOT NULL CHECK (unit_price >= 0 AND unit_price <> 'NaN'::numeric),
    PRIMARY KEY (tenant_id,exchange_id,variant_id),
    FOREIGN KEY (tenant_id,exchange_id) REFERENCES sale_return_exchanges(tenant_id,id),
    FOREIGN KEY (tenant_id,variant_id) REFERENCES product_variants(tenant_id,id)
);

ALTER TABLE stock_movements ADD COLUMN exchange_id uuid,
    ADD FOREIGN KEY (tenant_id,exchange_id) REFERENCES sale_return_exchanges(tenant_id,id),
    ADD CHECK ((movement_type='sale_exchange')=(exchange_id IS NOT NULL)),
    ADD CHECK (movement_type<>'sale_exchange' OR (quantity_delta<0 AND sale_id IS NOT NULL));
CREATE UNIQUE INDEX stock_sale_exchange_movement ON stock_movements(tenant_id,exchange_id,variant_id)
    WHERE exchange_id IS NOT NULL;

CREATE TRIGGER sale_return_exchanges_immutable
    BEFORE UPDATE OR DELETE ON sale_return_exchanges
    FOR EACH ROW EXECUTE FUNCTION reject_sale_return_change();

CREATE TRIGGER sale_return_exchange_items_immutable
    BEFORE UPDATE OR DELETE ON sale_return_exchange_items
    FOR EACH ROW EXECUTE FUNCTION reject_sale_return_change();
