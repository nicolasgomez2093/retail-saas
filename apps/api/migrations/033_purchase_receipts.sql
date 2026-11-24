ALTER TABLE purchase_orders DROP CONSTRAINT purchase_orders_status_check,
    ADD CHECK (status IN ('OPEN','PARTIALLY_RECEIVED','RECEIVED'));

CREATE TABLE purchase_receipts (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    store_id uuid NOT NULL,
    purchase_order_id uuid NOT NULL,
    location_id uuid NOT NULL,
    location_name text NOT NULL,
    note text NOT NULL CHECK (char_length(note)<=300),
    received_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    received_by uuid NOT NULL,
    idempotency_key text NOT NULL,
    request_hash bytea NOT NULL CHECK (length(request_hash)=32),
    UNIQUE (tenant_id,id,location_id),
    UNIQUE (tenant_id,purchase_order_id,id),
    UNIQUE (tenant_id,idempotency_key),
    FOREIGN KEY (tenant_id,store_id,purchase_order_id) REFERENCES purchase_orders(tenant_id,store_id,id),
    FOREIGN KEY (tenant_id,store_id,location_id) REFERENCES stock_locations(tenant_id,store_id,id),
    FOREIGN KEY (tenant_id,received_by) REFERENCES users(tenant_id,id)
);
CREATE INDEX purchase_receipts_by_order ON purchase_receipts(tenant_id,purchase_order_id,received_at DESC,id DESC);

CREATE TABLE purchase_receipt_items (
    tenant_id uuid NOT NULL,
    purchase_order_id uuid NOT NULL,
    receipt_id uuid NOT NULL,
    order_item_id uuid NOT NULL,
    accepted_qty integer NOT NULL CHECK (accepted_qty>=0),
    rejected_qty integer NOT NULL CHECK (rejected_qty>=0),
    CHECK (accepted_qty>0 OR rejected_qty>0),
    PRIMARY KEY (tenant_id,purchase_order_id,receipt_id,order_item_id),
    FOREIGN KEY (tenant_id,purchase_order_id,receipt_id) REFERENCES purchase_receipts(tenant_id,purchase_order_id,id),
    FOREIGN KEY (tenant_id,purchase_order_id,order_item_id) REFERENCES purchase_order_items(tenant_id,purchase_order_id,id)
);
CREATE INDEX purchase_receipt_items_by_line ON purchase_receipt_items(tenant_id,purchase_order_id,order_item_id);

CREATE FUNCTION reject_purchase_receipt_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'Las recepciones de compra son inmutables';
END;
$$;

CREATE TRIGGER purchase_receipts_immutable BEFORE UPDATE OR DELETE ON purchase_receipts
    FOR EACH ROW EXECUTE FUNCTION reject_purchase_receipt_change();
CREATE TRIGGER purchase_receipt_items_immutable BEFORE UPDATE OR DELETE ON purchase_receipt_items
    FOR EACH ROW EXECUTE FUNCTION reject_purchase_receipt_change();

ALTER TABLE stock_movements ADD COLUMN purchase_receipt_id uuid,
    ADD FOREIGN KEY (tenant_id,purchase_receipt_id,location_id) REFERENCES purchase_receipts(tenant_id,id,location_id),
    ADD CHECK ((movement_type='purchase_receipt')=(purchase_receipt_id IS NOT NULL)),
    ADD CHECK (movement_type<>'purchase_receipt' OR quantity_delta>0);
CREATE UNIQUE INDEX stock_movements_by_purchase_receipt ON stock_movements(tenant_id,purchase_receipt_id,variant_id)
    WHERE purchase_receipt_id IS NOT NULL;
