ALTER TABLE products ADD COLUMN revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0);
ALTER TABLE product_variants ADD COLUMN revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0);

CREATE TABLE catalog_sensitive_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    actor_id uuid NOT NULL,
    variant_id uuid NOT NULL,
    action text NOT NULL CHECK (action = 'correct_variant'),
    reason text NOT NULL,
    before_data jsonb NOT NULL,
    after_data jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (tenant_id, actor_id) REFERENCES users(tenant_id, id),
    FOREIGN KEY (tenant_id, variant_id) REFERENCES product_variants(tenant_id, id)
);
