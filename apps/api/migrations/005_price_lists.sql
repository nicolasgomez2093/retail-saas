INSERT INTO permissions(code) VALUES ('pricing:read'), ('pricing:manage');
INSERT INTO role_permissions(role_code, permission_code) SELECT code, 'pricing:read' FROM roles;
INSERT INTO role_permissions(role_code, permission_code) VALUES ('OWNER','pricing:manage');

CREATE TABLE price_lists (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    name text NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 120),
    UNIQUE (tenant_id, id)
);

CREATE UNIQUE INDEX price_lists_tenant_name ON price_lists(tenant_id,lower(name));

CREATE TABLE price_list_items (
    tenant_id uuid NOT NULL,
    price_list_id uuid NOT NULL,
    variant_id uuid NOT NULL,
    amount numeric(18,2) NOT NULL CHECK (amount >= 0 AND amount <> 'NaN'::numeric),
    PRIMARY KEY (tenant_id,price_list_id,variant_id),
    FOREIGN KEY (tenant_id,price_list_id) REFERENCES price_lists(tenant_id,id),
    FOREIGN KEY (tenant_id,variant_id) REFERENCES product_variants(tenant_id,id)
);

CREATE INDEX price_items_by_variant ON price_list_items(tenant_id,variant_id);

CREATE TABLE store_price_list_assignments (
    tenant_id uuid NOT NULL,
    store_id uuid NOT NULL,
    price_list_id uuid NOT NULL,
    PRIMARY KEY (tenant_id,store_id),
    FOREIGN KEY (tenant_id,store_id) REFERENCES stores(tenant_id,id),
    FOREIGN KEY (tenant_id,price_list_id) REFERENCES price_lists(tenant_id,id)
);

CREATE TABLE price_admin_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    actor_id uuid NOT NULL,
    action text NOT NULL CHECK (action IN ('create_list','set_price','assign_list')),
    list_id uuid,
    store_id uuid,
    variant_id uuid,
    before_data jsonb,
    after_data jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (tenant_id,actor_id) REFERENCES users(tenant_id,id),
    FOREIGN KEY (tenant_id,list_id) REFERENCES price_lists(tenant_id,id),
    FOREIGN KEY (tenant_id,store_id) REFERENCES stores(tenant_id,id),
    FOREIGN KEY (tenant_id,variant_id) REFERENCES product_variants(tenant_id,id)
);
