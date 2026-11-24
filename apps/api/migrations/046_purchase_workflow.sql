-- Compras: datos del proveedor, número y referencia del pedido, cancelación
-- y motivo de las unidades no aceptadas en una recepción.

ALTER TABLE suppliers
    ADD COLUMN tax_id text NOT NULL DEFAULT '' CHECK (char_length(tax_id) <= 20),
    ADD COLUMN contact_name text NOT NULL DEFAULT '' CHECK (char_length(contact_name) <= 120);
-- Una identificación fiscal no puede repetirse dentro del comercio.
CREATE UNIQUE INDEX suppliers_tax_id_unique ON suppliers(tenant_id,tax_id) WHERE tax_id <> '';

ALTER TABLE purchase_orders
    ADD COLUMN number integer,
    ADD COLUMN reference text NOT NULL DEFAULT '' CHECK (char_length(reference) <= 60),
    ADD COLUMN cancelled_at timestamptz,
    ADD COLUMN cancelled_by uuid,
    ADD COLUMN cancel_reason text NOT NULL DEFAULT '' CHECK (char_length(cancel_reason) <= 300),
    ADD FOREIGN KEY (tenant_id,cancelled_by) REFERENCES users(tenant_id,id);

UPDATE purchase_orders o SET number=n.number
FROM (SELECT tenant_id,id,row_number() OVER (PARTITION BY tenant_id ORDER BY ordered_at,id) AS number FROM purchase_orders) n
WHERE (o.tenant_id,o.id)=(n.tenant_id,n.id);

ALTER TABLE purchase_orders
    ALTER COLUMN number SET NOT NULL,
    ADD CONSTRAINT purchase_orders_number_positive CHECK (number > 0),
    ADD CONSTRAINT purchase_orders_number_unique UNIQUE (tenant_id,number),
    DROP CONSTRAINT purchase_orders_status_check,
    ADD CONSTRAINT purchase_orders_status_check CHECK (status IN ('OPEN','PARTIALLY_RECEIVED','RECEIVED','CANCELLED')),
    ADD CONSTRAINT purchase_orders_cancellation CHECK ((status='CANCELLED') = (cancelled_at IS NOT NULL AND cancelled_by IS NOT NULL));

-- Las recepciones confirmadas son inmutables: las filas previas conservan
-- un motivo nulo y las nuevas lo informan siempre que haya unidades no aceptadas.
ALTER TABLE purchase_receipt_items
    ADD COLUMN rejection_reason text CHECK (rejection_reason IN ('damaged','wrong_item','excess','other')),
    ADD CONSTRAINT purchase_receipt_items_reason_needs_rejection CHECK (rejection_reason IS NULL OR rejected_qty > 0);
