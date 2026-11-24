ALTER TABLE stores ADD COLUMN address text NOT NULL DEFAULT '';

INSERT INTO permissions(code) VALUES ('stores:manage');
INSERT INTO role_permissions(role_code, permission_code) VALUES ('OWNER', 'stores:manage');

CREATE TABLE store_admin_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    actor_id uuid NOT NULL,
    store_id uuid NOT NULL,
    action text NOT NULL CHECK (action IN ('create','update')),
    before_data jsonb,
    after_data jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (tenant_id, actor_id) REFERENCES users(tenant_id, id),
    FOREIGN KEY (tenant_id, store_id) REFERENCES stores(tenant_id, id)
);
