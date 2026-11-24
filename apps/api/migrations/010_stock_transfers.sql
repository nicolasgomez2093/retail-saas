INSERT INTO permissions(code) VALUES ('inventory:transfer');
INSERT INTO role_permissions(role_code,permission_code) VALUES
    ('OWNER','inventory:transfer'), ('STORE_MANAGER','inventory:transfer'), ('INVENTORY_OPERATOR','inventory:transfer');

CREATE TABLE stock_transfers (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    origin_location_id uuid NOT NULL,
    destination_location_id uuid NOT NULL,
    status text NOT NULL DEFAULT 'DRAFT' CHECK (status IN ('DRAFT','DISPATCHED','RECEIVED','CANCELLED')),
    idempotency_key text NOT NULL,
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    dispatched_by uuid,
    dispatched_at timestamptz,
    received_by uuid,
    received_at timestamptz,
    cancelled_by uuid,
    cancelled_at timestamptz,
    receipt_reason text NOT NULL DEFAULT '',
    CHECK (origin_location_id <> destination_location_id),
    CHECK ((dispatched_by IS NULL) = (dispatched_at IS NULL)),
    CHECK ((received_by IS NULL) = (received_at IS NULL)),
    CHECK ((cancelled_by IS NULL) = (cancelled_at IS NULL)),
    CHECK ((status IN ('DISPATCHED','RECEIVED')) = (dispatched_at IS NOT NULL)),
    CHECK ((status = 'RECEIVED') = (received_at IS NOT NULL)),
    CHECK ((status = 'CANCELLED') = (cancelled_at IS NOT NULL)),
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,idempotency_key),
    FOREIGN KEY (tenant_id,origin_location_id) REFERENCES stock_locations(tenant_id,id),
    FOREIGN KEY (tenant_id,destination_location_id) REFERENCES stock_locations(tenant_id,id),
    FOREIGN KEY (tenant_id,created_by) REFERENCES users(tenant_id,id),
    FOREIGN KEY (tenant_id,dispatched_by) REFERENCES users(tenant_id,id),
    FOREIGN KEY (tenant_id,received_by) REFERENCES users(tenant_id,id),
    FOREIGN KEY (tenant_id,cancelled_by) REFERENCES users(tenant_id,id)
);

CREATE INDEX stock_transfers_by_origin ON stock_transfers(tenant_id,origin_location_id,created_at DESC);
CREATE INDEX stock_transfers_by_destination ON stock_transfers(tenant_id,destination_location_id,created_at DESC);

CREATE TABLE stock_transfer_items (
    tenant_id uuid NOT NULL,
    transfer_id uuid NOT NULL,
    variant_id uuid NOT NULL,
    sku_snapshot text NOT NULL,
    requested_quantity integer NOT NULL CHECK (requested_quantity > 0),
    received_quantity integer CHECK (received_quantity BETWEEN 0 AND requested_quantity),
    PRIMARY KEY (tenant_id,transfer_id,variant_id),
    FOREIGN KEY (tenant_id,transfer_id) REFERENCES stock_transfers(tenant_id,id),
    FOREIGN KEY (tenant_id,variant_id) REFERENCES product_variants(tenant_id,id)
);

ALTER TABLE stock_movements ADD COLUMN transfer_id uuid,
    ADD FOREIGN KEY (tenant_id,transfer_id) REFERENCES stock_transfers(tenant_id,id);

CREATE UNIQUE INDEX stock_movements_by_transfer ON stock_movements(tenant_id,transfer_id,variant_id,movement_type)
    WHERE transfer_id IS NOT NULL;
