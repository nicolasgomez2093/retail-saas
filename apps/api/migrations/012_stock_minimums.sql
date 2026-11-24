INSERT INTO permissions(code) VALUES ('inventory:manage_minimums');
INSERT INTO role_permissions(role_code,permission_code) VALUES
    ('OWNER','inventory:manage_minimums'), ('STORE_MANAGER','inventory:manage_minimums');

CREATE TABLE stock_minimums (
    tenant_id uuid NOT NULL,
    location_id uuid NOT NULL,
    variant_id uuid NOT NULL,
    minimum integer NOT NULL CHECK (minimum >= 0),
    updated_by uuid NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (tenant_id,location_id,variant_id),
    FOREIGN KEY (tenant_id,location_id) REFERENCES stock_locations(tenant_id,id),
    FOREIGN KEY (tenant_id,variant_id) REFERENCES product_variants(tenant_id,id),
    FOREIGN KEY (tenant_id,updated_by) REFERENCES users(tenant_id,id)
);
