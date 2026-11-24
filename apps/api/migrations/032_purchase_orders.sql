INSERT INTO permissions(code) VALUES ('purchases:read'), ('purchases:manage');
INSERT INTO role_permissions(role_code,permission_code) VALUES
    ('OWNER','purchases:read'), ('OWNER','purchases:manage'),
    ('STORE_MANAGER','purchases:read'), ('STORE_MANAGER','purchases:manage');

CREATE TABLE purchase_orders (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    store_id uuid NOT NULL,
    supplier_id uuid NOT NULL,
    supplier_name text NOT NULL,
    currency char(3) NOT NULL,
    status text NOT NULL DEFAULT 'OPEN' CHECK (status='OPEN'),
    ordered_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    created_by uuid NOT NULL,
    idempotency_key text NOT NULL,
    request_hash bytea NOT NULL CHECK (length(request_hash)=32),
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,store_id,id),
    UNIQUE (tenant_id,idempotency_key),
    FOREIGN KEY (tenant_id,store_id) REFERENCES stores(tenant_id,id),
    FOREIGN KEY (tenant_id,supplier_id) REFERENCES suppliers(tenant_id,id),
    FOREIGN KEY (tenant_id,created_by) REFERENCES users(tenant_id,id)
);
CREATE INDEX purchase_orders_by_store ON purchase_orders(tenant_id,store_id,ordered_at DESC,id DESC);

CREATE TABLE purchase_order_items (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    purchase_order_id uuid NOT NULL,
    variant_id uuid NOT NULL,
    product_name text NOT NULL,
    sku text NOT NULL,
    size text NOT NULL,
    color text NOT NULL,
    ordered_qty integer NOT NULL CHECK (ordered_qty > 0),
    unit_cost numeric(18,2) CHECK (unit_cost>=0 AND unit_cost<>'NaN'::numeric),
    UNIQUE (tenant_id,purchase_order_id,id),
    UNIQUE (tenant_id,purchase_order_id,variant_id),
    FOREIGN KEY (tenant_id,purchase_order_id) REFERENCES purchase_orders(tenant_id,id),
    FOREIGN KEY (tenant_id,variant_id) REFERENCES product_variants(tenant_id,id)
);
