ALTER TABLE product_variants ADD COLUMN batch_created boolean NOT NULL DEFAULT false;

CREATE TABLE catalog_variant_batches (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    product_id uuid NOT NULL,
    actor_id uuid NOT NULL,
    idempotency_key text NOT NULL CHECK (idempotency_key ~ '^[A-Za-z0-9_.-]{1,128}$'),
    request_hash bytea NOT NULL CHECK (length(request_hash)=32),
    response jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id,idempotency_key),
    FOREIGN KEY (tenant_id,product_id) REFERENCES products(tenant_id,id),
    FOREIGN KEY (tenant_id,actor_id) REFERENCES users(tenant_id,id)
);

-- The original result also serves as the tombstone. It must survive retries
-- and cannot be deleted by an application cleanup.
CREATE FUNCTION reject_catalog_variant_batch_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'El resultado confirmado de un lote de variantes es inmutable';
END;
$$;
CREATE TRIGGER catalog_variant_batches_immutable
BEFORE UPDATE OR DELETE ON catalog_variant_batches
FOR EACH ROW EXECUTE FUNCTION reject_catalog_variant_batch_change();
