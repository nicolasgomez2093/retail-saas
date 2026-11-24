-- Additional variant attributes beyond size and color (for example the
-- length of a jean or the presentation of a perfume). Existing variants keep
-- an empty list, so their identity, sales and inventory are unchanged.
ALTER TABLE product_variants
    ADD COLUMN options jsonb NOT NULL DEFAULT '[]'::jsonb
    CHECK (jsonb_typeof(options) = 'array' AND jsonb_array_length(options) <= 4);

-- Quick product creation writes product, variants, identifiers, prices and
-- initial stock in one transaction. The confirmed result is kept so a retry
-- with the same key returns it without writing anything again.
CREATE TABLE catalog_quick_creations (
    tenant_id uuid NOT NULL,
    idempotency_key text NOT NULL CHECK (idempotency_key ~ '^[A-Za-z0-9_.-]{1,128}$'),
    actor_id uuid NOT NULL,
    product_id uuid NOT NULL,
    request_hash bytea NOT NULL CHECK (length(request_hash) = 32),
    response jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, idempotency_key),
    FOREIGN KEY (tenant_id, product_id) REFERENCES products(tenant_id, id),
    FOREIGN KEY (tenant_id, actor_id) REFERENCES users(tenant_id, id)
);

CREATE FUNCTION reject_catalog_quick_creation_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'El resultado confirmado de un alta rápida es inmutable';
END;
$$;
CREATE TRIGGER catalog_quick_creations_immutable
BEFORE UPDATE OR DELETE ON catalog_quick_creations
FOR EACH ROW EXECUTE FUNCTION reject_catalog_quick_creation_change();
