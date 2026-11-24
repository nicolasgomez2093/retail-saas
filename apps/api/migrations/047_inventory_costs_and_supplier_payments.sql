-- Costo cargado a mano para el stock actual de una variante y pagos a
-- proveedores por pedido de compra.

-- Cada carga de costo queda registrada: quién, cuándo, el costo anterior y
-- las unidades a las que se aplicó. El costo vigente sigue en
-- inventory_cost_balances.
CREATE TABLE inventory_cost_revisions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    store_id uuid NOT NULL,
    variant_id uuid NOT NULL,
    previous_unit_cost numeric(18,2),
    unit_cost numeric(18,2) NOT NULL CHECK (unit_cost >= 0 AND unit_cost <> 'NaN'::numeric),
    quantity_on_hand bigint NOT NULL CHECK (quantity_on_hand > 0),
    reason text NOT NULL DEFAULT '' CHECK (char_length(reason) <= 300),
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    FOREIGN KEY (tenant_id,store_id) REFERENCES stores(tenant_id,id),
    FOREIGN KEY (tenant_id,variant_id) REFERENCES product_variants(tenant_id,id),
    FOREIGN KEY (tenant_id,created_by) REFERENCES users(tenant_id,id)
);
CREATE INDEX inventory_cost_revisions_by_variant ON inventory_cost_revisions(tenant_id,store_id,variant_id,created_at DESC);

-- Una transferencia entre ubicaciones de la misma sucursal no cambia el
-- costo: son las mismas unidades. Antes dejaba el costo como desconocido.
CREATE OR REPLACE FUNCTION invalidate_unknown_inventory_cost() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.quantity_delta>0 AND NEW.movement_type<>'purchase_receipt' AND NOT (
        NEW.movement_type='transfer' AND EXISTS (
            SELECT 1 FROM stock_transfers t
            JOIN stock_locations o ON (o.tenant_id,o.id)=(t.tenant_id,t.origin_location_id)
            JOIN stock_locations d ON (d.tenant_id,d.id)=(t.tenant_id,t.destination_location_id)
            WHERE t.tenant_id=NEW.tenant_id AND t.id=NEW.transfer_id AND o.store_id=d.store_id
        )
    ) THEN
        UPDATE inventory_cost_balances c SET status='unknown',average_unit_cost=NULL,updated_at=clock_timestamp()
        WHERE c.tenant_id=NEW.tenant_id AND c.variant_id=NEW.variant_id
          AND c.store_id=(SELECT store_id FROM stock_locations WHERE tenant_id=NEW.tenant_id AND id=NEW.location_id);
    END IF;
    RETURN NEW;
END;
$$;

-- Pagos al proveedor de un pedido. No se borran: un pago cargado por error
-- se anula con motivo y deja de sumar.
CREATE TABLE purchase_order_payments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    store_id uuid NOT NULL,
    purchase_order_id uuid NOT NULL,
    method text NOT NULL CHECK (method IN ('cash','transfer','check','card','other')),
    amount numeric(18,2) NOT NULL CHECK (amount > 0 AND amount <> 'NaN'::numeric),
    paid_on date NOT NULL,
    -- Fecha de cobro de un cheque (diferido o no).
    check_due_on date CHECK (check_due_on IS NULL OR method='check'),
    reference text NOT NULL DEFAULT '' CHECK (char_length(reference) <= 80),
    note text NOT NULL DEFAULT '' CHECK (char_length(note) <= 300),
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    voided_at timestamptz,
    voided_by uuid,
    void_reason text NOT NULL DEFAULT '' CHECK (char_length(void_reason) <= 300),
    idempotency_key text NOT NULL,
    request_hash bytea NOT NULL CHECK (length(request_hash)=32),
    UNIQUE (tenant_id,idempotency_key),
    CHECK ((voided_at IS NULL) = (voided_by IS NULL)),
    FOREIGN KEY (tenant_id,store_id,purchase_order_id) REFERENCES purchase_orders(tenant_id,store_id,id),
    FOREIGN KEY (tenant_id,created_by) REFERENCES users(tenant_id,id),
    FOREIGN KEY (tenant_id,voided_by) REFERENCES users(tenant_id,id)
);
CREATE INDEX purchase_order_payments_by_order ON purchase_order_payments(tenant_id,purchase_order_id,paid_on,created_at);
