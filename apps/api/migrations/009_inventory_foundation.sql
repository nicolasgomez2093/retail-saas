INSERT INTO permissions(code) VALUES ('inventory:read'), ('inventory:adjust'), ('locations:manage');
INSERT INTO role_permissions(role_code, permission_code) SELECT code, 'inventory:read' FROM roles;
INSERT INTO role_permissions(role_code, permission_code) VALUES
    ('OWNER','inventory:adjust'), ('STORE_MANAGER','inventory:adjust'),
    ('INVENTORY_OPERATOR','inventory:adjust'), ('OWNER','locations:manage');

CREATE TABLE stock_locations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    store_id uuid NOT NULL,
    code text NOT NULL,
    name text NOT NULL,
    type text NOT NULL CHECK (type IN ('sales_floor','warehouse')),
    is_sellable boolean NOT NULL DEFAULT false,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','inactive')),
    UNIQUE (tenant_id,id),
    FOREIGN KEY (tenant_id,store_id) REFERENCES stores(tenant_id,id)
);

CREATE UNIQUE INDEX stock_locations_store_code ON stock_locations(tenant_id,store_id,lower(code));

CREATE TABLE stock_balances (
    tenant_id uuid NOT NULL,
    location_id uuid NOT NULL,
    variant_id uuid NOT NULL,
    quantity_on_hand integer NOT NULL DEFAULT 0 CHECK (quantity_on_hand >= 0),
    quantity_reserved integer NOT NULL DEFAULT 0 CHECK (quantity_reserved >= 0 AND quantity_reserved <= quantity_on_hand),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id,location_id,variant_id),
    FOREIGN KEY (tenant_id,location_id) REFERENCES stock_locations(tenant_id,id),
    FOREIGN KEY (tenant_id,variant_id) REFERENCES product_variants(tenant_id,id)
);

CREATE TABLE stock_movements (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    location_id uuid NOT NULL,
    variant_id uuid NOT NULL,
    actor_id uuid NOT NULL,
    sku_snapshot text NOT NULL,
    movement_type text NOT NULL,
    quantity_delta integer NOT NULL CHECK (quantity_delta <> 0),
    quantity_before integer NOT NULL CHECK (quantity_before >= 0),
    quantity_after integer NOT NULL CHECK (quantity_after >= 0),
    reason text NOT NULL CHECK (char_length(btrim(reason)) > 0),
    idempotency_key text,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (quantity_before::bigint + quantity_delta = quantity_after),
    UNIQUE (tenant_id,idempotency_key),
    FOREIGN KEY (tenant_id,location_id) REFERENCES stock_locations(tenant_id,id),
    FOREIGN KEY (tenant_id,variant_id) REFERENCES product_variants(tenant_id,id),
    FOREIGN KEY (tenant_id,actor_id) REFERENCES users(tenant_id,id)
);

CREATE INDEX stock_movements_by_location ON stock_movements(tenant_id,location_id,created_at DESC,id DESC);

CREATE FUNCTION reject_stock_movement_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'Los movimientos de stock son inmutables';
END;
$$;

CREATE TRIGGER stock_movements_immutable
    BEFORE UPDATE OR DELETE ON stock_movements
    FOR EACH ROW EXECUTE FUNCTION reject_stock_movement_change();
