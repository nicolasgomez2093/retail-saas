CREATE TABLE tenants (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    slug text UNIQUE NOT NULL CHECK (slug ~ '^[a-z0-9]+(-[a-z0-9]+)*$'),
    name text NOT NULL,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    timezone text NOT NULL,
    currency_code text NOT NULL CHECK (currency_code ~ '^[A-Z]{3}$'),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE stores (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    code text NOT NULL,
    name text NOT NULL,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, code)
);

CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    email text NOT NULL CHECK (email = lower(btrim(email))),
    name text NOT NULL,
    password_hash text NOT NULL,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, email)
);

CREATE TABLE roles (
    code text PRIMARY KEY
);

CREATE TABLE permissions (
    code text PRIMARY KEY
);

CREATE TABLE role_permissions (
    role_code text REFERENCES roles(code),
    permission_code text REFERENCES permissions(code),
    PRIMARY KEY (role_code, permission_code)
);

CREATE TABLE user_roles (
    tenant_id uuid NOT NULL,
    user_id uuid NOT NULL,
    role_code text REFERENCES roles(code),
    PRIMARY KEY (tenant_id, user_id, role_code),
    FOREIGN KEY (tenant_id, user_id) REFERENCES users(tenant_id, id)
);

CREATE TABLE user_stores (
    tenant_id uuid NOT NULL,
    user_id uuid NOT NULL,
    store_id uuid NOT NULL,
    PRIMARY KEY (tenant_id, user_id, store_id),
    FOREIGN KEY (tenant_id, user_id) REFERENCES users(tenant_id, id),
    FOREIGN KEY (tenant_id, store_id) REFERENCES stores(tenant_id, id)
);

CREATE INDEX user_stores_by_store ON user_stores(tenant_id, store_id);

CREATE TABLE sessions (
    token_hash bytea PRIMARY KEY CHECK (octet_length(token_hash) = 32),
    tenant_id uuid NOT NULL,
    user_id uuid NOT NULL,
    expires_at timestamptz NOT NULL,
    FOREIGN KEY (tenant_id, user_id) REFERENCES users(tenant_id, id)
);

CREATE INDEX sessions_by_expiration ON sessions(expires_at);
CREATE INDEX sessions_by_user ON sessions(tenant_id, user_id);

INSERT INTO roles(code) VALUES ('OWNER'), ('STORE_MANAGER'), ('CASHIER'), ('INVENTORY_OPERATOR');
INSERT INTO permissions(code) VALUES ('stores:read');
INSERT INTO role_permissions(role_code, permission_code) SELECT code, 'stores:read' FROM roles;
