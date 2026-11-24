ALTER TABLE barcodes ADD COLUMN is_internal boolean NOT NULL DEFAULT false;
CREATE UNIQUE INDEX barcodes_one_internal ON barcodes(tenant_id,variant_id) WHERE is_internal;

CREATE TABLE catalog_identifier_sequences (
    tenant_id uuid PRIMARY KEY REFERENCES tenants(id),
    sku_prefix text NOT NULL DEFAULT '' CHECK (sku_prefix ~ '^([A-Z][A-Z0-9_-]{0,11})?$'),
    sku_width integer NOT NULL DEFAULT 6 CHECK (sku_width BETWEEN 4 AND 12),
    sku_number bigint NOT NULL DEFAULT 0 CHECK (sku_number BETWEEN 0 AND 999999999999),
    barcode_number bigint NOT NULL DEFAULT 0 CHECK (barcode_number BETWEEN 0 AND 9999999999),
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0)
);

CREATE TABLE catalog_identifier_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    kind text NOT NULL CHECK (kind IN ('sku','barcode')),
    value text NOT NULL CHECK (length(value)>0),
    normalized text GENERATED ALWAYS AS (CASE WHEN kind='sku' THEN lower(value) ELSE value END) STORED,
    variant_id uuid NOT NULL,
    action text NOT NULL CHECK (action IN ('retired','reused')),
    is_internal boolean NOT NULL DEFAULT false,
    actor_id uuid,
    reason text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    FOREIGN KEY (tenant_id,variant_id) REFERENCES product_variants(tenant_id,id),
    FOREIGN KEY (tenant_id,actor_id) REFERENCES users(tenant_id,id)
);
CREATE INDEX catalog_identifiers_retired ON catalog_identifier_events(tenant_id,kind,normalized)
    WHERE action='retired';
CREATE INDEX catalog_identifier_history ON catalog_identifier_events(tenant_id,kind,normalized,created_at DESC,id);

-- Recover identifiers still observable before this migration. The observation
-- of a historical SKU is not evidence of its original removal date or actor.
WITH history AS (
    SELECT tenant_id,variant_id,sku AS value FROM sale_items
    UNION SELECT tenant_id,variant_id,sku FROM sale_return_exchange_items
    UNION SELECT tenant_id,variant_id,sku FROM reservation_items
    UNION SELECT tenant_id,variant_id,sku FROM purchase_order_items
    UNION SELECT tenant_id,variant_id,sku_snapshot FROM stock_count_items
    UNION SELECT tenant_id,variant_id,sku_snapshot FROM stock_transfer_items
    UNION SELECT tenant_id,variant_id,sku_snapshot FROM stock_movements
    UNION SELECT tenant_id,variant_id,before_data->>'sku' FROM catalog_sensitive_events
)
INSERT INTO catalog_identifier_events(tenant_id,kind,value,variant_id,action,reason)
SELECT DISTINCT ON (h.tenant_id,h.variant_id,lower(h.value)) h.tenant_id,'sku',h.value,h.variant_id,'retired','Importado de un snapshot o una auditoría anterior'
FROM history h JOIN product_variants v ON (v.tenant_id,v.id)=(h.tenant_id,h.variant_id)
WHERE h.value<>'' AND lower(h.value)<>lower(v.sku)
ORDER BY h.tenant_id,h.variant_id,lower(h.value),h.value;

INSERT INTO catalog_identifier_events(tenant_id,kind,value,variant_id,action,reason)
SELECT DISTINCT e.tenant_id,'barcode',past.code,e.variant_id,'retired','Importado de una auditoría anterior'
FROM catalog_sensitive_events e CROSS JOIN LATERAL jsonb_array_elements_text(e.before_data->'barcodes') AS past(code)
WHERE past.code<>'' AND NOT EXISTS (
    SELECT 1 FROM barcodes b WHERE (b.tenant_id,b.variant_id,b.code)=(e.tenant_id,e.variant_id,past.code)
);

CREATE FUNCTION reject_catalog_identifier_event_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'El historial de identificadores es inmutable';
END;
$$;
CREATE TRIGGER catalog_identifier_events_immutable
BEFORE UPDATE OR DELETE ON catalog_identifier_events
FOR EACH ROW EXECUTE FUNCTION reject_catalog_identifier_event_change();

CREATE TABLE catalog_variant_creations (
    tenant_id uuid NOT NULL,
    idempotency_key text NOT NULL,
    actor_id uuid NOT NULL,
    variant_id uuid NOT NULL,
    request_hash bytea NOT NULL CHECK (length(request_hash)=32),
    response jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id,idempotency_key),
    FOREIGN KEY (tenant_id,actor_id) REFERENCES users(tenant_id,id),
    FOREIGN KEY (tenant_id,variant_id) REFERENCES product_variants(tenant_id,id)
);
