CREATE TABLE customers (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    store_id uuid NOT NULL,
    name text NOT NULL CHECK (char_length(name) BETWEEN 1 AND 120),
    phone text NOT NULL DEFAULT '' CHECK (char_length(phone) <= 40),
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (tenant_id,store_id,id),
    FOREIGN KEY (tenant_id,store_id) REFERENCES stores(tenant_id,id),
    FOREIGN KEY (tenant_id,created_by) REFERENCES users(tenant_id,id)
);
CREATE INDEX customers_by_store_name ON customers(tenant_id,store_id,lower(name),id);

ALTER TABLE sales ADD UNIQUE (tenant_id,store_id,id);

CREATE TABLE receivables (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    store_id uuid NOT NULL,
    sale_id uuid NOT NULL,
    customer_id uuid NOT NULL,
    customer_name_snapshot text NOT NULL,
    original_amount numeric(18,2) NOT NULL CHECK (original_amount > 0 AND original_amount <> 'NaN'::numeric),
    outstanding_amount numeric(18,2) NOT NULL CHECK (outstanding_amount BETWEEN 0 AND original_amount),
    UNIQUE (tenant_id,store_id,sale_id),
    FOREIGN KEY (tenant_id,store_id,sale_id) REFERENCES sales(tenant_id,store_id,id),
    FOREIGN KEY (tenant_id,store_id,customer_id) REFERENCES customers(tenant_id,store_id,id)
);
