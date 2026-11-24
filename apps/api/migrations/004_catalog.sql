INSERT INTO permissions(code) VALUES ('catalog:read'), ('catalog:manage');
INSERT INTO role_permissions(role_code, permission_code) SELECT code, 'catalog:read' FROM roles;
INSERT INTO role_permissions(role_code, permission_code) VALUES ('OWNER', 'catalog:manage');

CREATE TABLE products (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    name text NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 160),
    description text NOT NULL DEFAULT '',
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','inactive')),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id)
);

CREATE INDEX products_by_name ON products(tenant_id, name, id);

CREATE TABLE product_variants (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    product_id uuid NOT NULL,
    sku text NOT NULL,
    size text NOT NULL DEFAULT '',
    color text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    FOREIGN KEY (tenant_id, product_id) REFERENCES products(tenant_id, id)
);

CREATE UNIQUE INDEX variants_tenant_sku ON product_variants(tenant_id, lower(sku));
CREATE INDEX variants_by_product ON product_variants(tenant_id, product_id, sku);

CREATE TABLE barcodes (
    tenant_id uuid NOT NULL,
    variant_id uuid NOT NULL,
    code text NOT NULL CHECK (code ~ '^[A-Za-z0-9][A-Za-z0-9_.-]{0,79}$'),
    is_primary boolean NOT NULL DEFAULT false,
    PRIMARY KEY (tenant_id, code),
    FOREIGN KEY (tenant_id, variant_id) REFERENCES product_variants(tenant_id, id)
);

CREATE INDEX barcodes_by_variant ON barcodes(tenant_id, variant_id);
CREATE UNIQUE INDEX barcodes_one_primary ON barcodes(tenant_id, variant_id) WHERE is_primary;
