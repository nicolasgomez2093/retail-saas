CREATE TABLE sale_sequences (
    tenant_id uuid NOT NULL,
    store_id uuid NOT NULL,
    next_number bigint NOT NULL CHECK (next_number >= 2),
    PRIMARY KEY (tenant_id,store_id),
    FOREIGN KEY (tenant_id,store_id) REFERENCES stores(tenant_id,id)
);

CREATE TABLE sales (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    store_id uuid NOT NULL,
    cash_session_id uuid NOT NULL,
    location_id uuid NOT NULL,
    sale_number bigint NOT NULL CHECK (sale_number > 0),
    currency text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
    total numeric(18,2) NOT NULL CHECK (total >= 0 AND total <> 'NaN'::numeric),
    idempotency_key text NOT NULL,
    request_hash bytea NOT NULL CHECK (length(request_hash)=32),
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,idempotency_key),
    UNIQUE (tenant_id,store_id,sale_number),
    FOREIGN KEY (tenant_id,store_id) REFERENCES stores(tenant_id,id),
    FOREIGN KEY (tenant_id,cash_session_id) REFERENCES cash_sessions(tenant_id,id),
    FOREIGN KEY (tenant_id,location_id) REFERENCES stock_locations(tenant_id,id),
    FOREIGN KEY (tenant_id,created_by) REFERENCES users(tenant_id,id)
);

CREATE TABLE sale_items (
    tenant_id uuid NOT NULL,
    sale_id uuid NOT NULL,
    variant_id uuid NOT NULL,
    product_name text NOT NULL,
    sku text NOT NULL,
    size text NOT NULL,
    color text NOT NULL,
    quantity integer NOT NULL CHECK (quantity > 0),
    unit_price numeric(18,2) NOT NULL CHECK (unit_price >= 0 AND unit_price <> 'NaN'::numeric),
    PRIMARY KEY (tenant_id,sale_id,variant_id),
    FOREIGN KEY (tenant_id,sale_id) REFERENCES sales(tenant_id,id),
    FOREIGN KEY (tenant_id,variant_id) REFERENCES product_variants(tenant_id,id)
);

CREATE TABLE sale_payments (
    tenant_id uuid NOT NULL,
    sale_id uuid NOT NULL,
    method text NOT NULL,
    amount numeric(18,2) NOT NULL CHECK (amount >= 0 AND amount <> 'NaN'::numeric),
    cash_received numeric(18,2) CHECK (cash_received >= 0 AND cash_received <> 'NaN'::numeric),
    change_amount numeric(18,2) CHECK (change_amount >= 0 AND change_amount <> 'NaN'::numeric),
    PRIMARY KEY (tenant_id,sale_id,method),
    CHECK (method<>'cash' OR (cash_received IS NOT NULL AND change_amount IS NOT NULL AND cash_received-change_amount=amount)),
    FOREIGN KEY (tenant_id,sale_id) REFERENCES sales(tenant_id,id)
);

ALTER TABLE stock_movements ADD COLUMN sale_id uuid,
    ADD FOREIGN KEY (tenant_id,sale_id) REFERENCES sales(tenant_id,id);

CREATE UNIQUE INDEX stock_sale_movement ON stock_movements(tenant_id,sale_id,variant_id)
    WHERE sale_id IS NOT NULL AND movement_type='sale';

ALTER TABLE cash_movements ADD COLUMN sale_id uuid,
    ADD FOREIGN KEY (tenant_id,sale_id) REFERENCES sales(tenant_id,id);

CREATE UNIQUE INDEX cash_sale_movement ON cash_movements(tenant_id,sale_id)
    WHERE sale_id IS NOT NULL AND movement_type='sale_cash';
