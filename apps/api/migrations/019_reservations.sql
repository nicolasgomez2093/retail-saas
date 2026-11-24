ALTER TABLE stock_locations ADD UNIQUE (tenant_id,store_id,id);

CREATE TABLE reservations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    store_id uuid NOT NULL,
    customer_id uuid NOT NULL,
    customer_name text NOT NULL,
    location_id uuid NOT NULL,
    status text NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','FULFILLED','CANCELLED','EXPIRED')),
    expires_at timestamptz,
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    closed_by uuid,
    closed_at timestamptz,
    sale_id uuid,
    idempotency_key text NOT NULL,
    request_hash bytea NOT NULL CHECK (length(request_hash)=32),
    CHECK ((status = 'ACTIVE') = (closed_by IS NULL AND closed_at IS NULL)),
    CHECK ((closed_by IS NULL) = (closed_at IS NULL)),
    CHECK ((status = 'FULFILLED') = (sale_id IS NOT NULL)),
    CHECK (status <> 'EXPIRED' OR (expires_at IS NOT NULL AND closed_at >= expires_at)),
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,idempotency_key),
    UNIQUE (tenant_id,store_id,sale_id),
    FOREIGN KEY (tenant_id,store_id,customer_id) REFERENCES customers(tenant_id,store_id,id),
    FOREIGN KEY (tenant_id,store_id,location_id) REFERENCES stock_locations(tenant_id,store_id,id),
    FOREIGN KEY (tenant_id,store_id,sale_id) REFERENCES sales(tenant_id,store_id,id),
    FOREIGN KEY (tenant_id,created_by) REFERENCES users(tenant_id,id),
    FOREIGN KEY (tenant_id,closed_by) REFERENCES users(tenant_id,id)
);
CREATE INDEX reservations_by_store ON reservations(tenant_id,store_id,status,created_at DESC,id DESC);

CREATE TABLE reservation_items (
    tenant_id uuid NOT NULL,
    reservation_id uuid NOT NULL,
    variant_id uuid NOT NULL,
    quantity integer NOT NULL CHECK (quantity > 0),
    product_name text NOT NULL,
    sku text NOT NULL,
    size text NOT NULL,
    color text NOT NULL,
    PRIMARY KEY (tenant_id,reservation_id,variant_id),
    FOREIGN KEY (tenant_id,reservation_id) REFERENCES reservations(tenant_id,id),
    FOREIGN KEY (tenant_id,variant_id) REFERENCES product_variants(tenant_id,id)
);
