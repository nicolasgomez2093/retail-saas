CREATE TABLE categories (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    name text NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 120),
    UNIQUE (tenant_id,id)
);

CREATE UNIQUE INDEX categories_tenant_name ON categories(tenant_id,lower(name));

CREATE TABLE brands (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    name text NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 120),
    UNIQUE (tenant_id,id)
);

CREATE UNIQUE INDEX brands_tenant_name ON brands(tenant_id,lower(name));

ALTER TABLE products ADD COLUMN category_id uuid,
    ADD COLUMN brand_id uuid,
    ADD FOREIGN KEY (tenant_id,category_id) REFERENCES categories(tenant_id,id),
    ADD FOREIGN KEY (tenant_id,brand_id) REFERENCES brands(tenant_id,id);

CREATE INDEX products_by_category ON products(tenant_id,category_id,name,id) WHERE category_id IS NOT NULL;
CREATE INDEX products_by_brand ON products(tenant_id,brand_id,name,id) WHERE brand_id IS NOT NULL;
