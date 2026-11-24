INSERT INTO permissions(code) VALUES ('users:manage_tenant'), ('users:manage_assigned');
INSERT INTO role_permissions(role_code, permission_code) VALUES
    ('OWNER', 'users:manage_tenant'),
    ('STORE_MANAGER', 'users:manage_assigned');

CREATE TABLE user_admin_events (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    actor_id uuid NOT NULL,
    user_id uuid NOT NULL,
    action text NOT NULL CHECK (action IN ('create','update')),
    before_data jsonb,
    after_data jsonb NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (tenant_id, actor_id) REFERENCES users(tenant_id, id),
    FOREIGN KEY (tenant_id, user_id) REFERENCES users(tenant_id, id)
);
