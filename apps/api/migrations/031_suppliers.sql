INSERT INTO permissions(code) VALUES ('suppliers:read'), ('suppliers:manage');
INSERT INTO role_permissions(role_code,permission_code) VALUES
    ('OWNER','suppliers:read'), ('OWNER','suppliers:manage'), ('STORE_MANAGER','suppliers:read');

CREATE TABLE suppliers (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    name text NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 160),
    email text NOT NULL DEFAULT '' CHECK (char_length(email) <= 254),
    phone text NOT NULL DEFAULT '' CHECK (char_length(phone) <= 40),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','inactive')),
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id,id),
    FOREIGN KEY (tenant_id,created_by) REFERENCES users(tenant_id,id)
);

CREATE INDEX suppliers_by_name ON suppliers(tenant_id,lower(name),id);
