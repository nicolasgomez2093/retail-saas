INSERT INTO permissions(code) VALUES ('cash:operate'), ('cash:supervise'), ('cash:manage_registers');
INSERT INTO role_permissions(role_code,permission_code) VALUES
    ('OWNER','cash:operate'), ('STORE_MANAGER','cash:operate'), ('CASHIER','cash:operate'),
    ('OWNER','cash:supervise'), ('STORE_MANAGER','cash:supervise'), ('OWNER','cash:manage_registers');

CREATE TABLE cash_registers (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    store_id uuid NOT NULL,
    code text NOT NULL,
    name text NOT NULL,
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','inactive')),
    UNIQUE (tenant_id,id),
    FOREIGN KEY (tenant_id,store_id) REFERENCES stores(tenant_id,id)
);

CREATE UNIQUE INDEX cash_registers_store_code ON cash_registers(tenant_id,store_id,lower(code));

CREATE TABLE cash_sessions (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    cash_register_id uuid NOT NULL,
    opened_by uuid NOT NULL,
    opened_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    opening_cash numeric(18,2) NOT NULL CHECK (opening_cash >= 0 AND opening_cash <> 'NaN'::numeric),
    open_key text NOT NULL,
    status text NOT NULL DEFAULT 'OPEN' CHECK (status IN ('OPEN','CLOSED')),
    closed_by uuid,
    closed_at timestamptz,
    expected_cash numeric(18,2) CHECK (expected_cash >= 0 AND expected_cash <> 'NaN'::numeric),
    declared_cash numeric(18,2) CHECK (declared_cash >= 0 AND declared_cash <> 'NaN'::numeric),
    difference numeric(18,2),
    CHECK ((status='CLOSED') = (closed_by IS NOT NULL)),
    CHECK ((status='CLOSED') = (closed_at IS NOT NULL)),
    CHECK ((status='CLOSED') = (expected_cash IS NOT NULL)),
    CHECK ((status='CLOSED') = (declared_cash IS NOT NULL)),
    CHECK ((status='CLOSED') = (difference IS NOT NULL)),
    CHECK (difference=declared_cash-expected_cash),
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,open_key),
    FOREIGN KEY (tenant_id,cash_register_id) REFERENCES cash_registers(tenant_id,id),
    FOREIGN KEY (tenant_id,opened_by) REFERENCES users(tenant_id,id),
    FOREIGN KEY (tenant_id,closed_by) REFERENCES users(tenant_id,id)
);

CREATE UNIQUE INDEX cash_sessions_one_open ON cash_sessions(tenant_id,cash_register_id) WHERE status='OPEN';
CREATE INDEX cash_sessions_by_register ON cash_sessions(tenant_id,cash_register_id,opened_at DESC,id DESC);

CREATE TABLE cash_movements (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    cash_session_id uuid NOT NULL,
    actor_id uuid NOT NULL,
    movement_type text NOT NULL,
    amount numeric(18,2) NOT NULL CHECK (amount <> 0 AND amount <> 'NaN'::numeric),
    reason text NOT NULL CHECK (char_length(btrim(reason)) > 0),
    idempotency_key text,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    CHECK (movement_type <> 'manual_in' OR amount > 0),
    CHECK (movement_type <> 'manual_out' OR amount < 0),
    UNIQUE (tenant_id,idempotency_key),
    FOREIGN KEY (tenant_id,cash_session_id) REFERENCES cash_sessions(tenant_id,id),
    FOREIGN KEY (tenant_id,actor_id) REFERENCES users(tenant_id,id)
);

CREATE INDEX cash_movements_by_session ON cash_movements(tenant_id,cash_session_id,created_at DESC,id DESC);

CREATE FUNCTION reject_cash_movement_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'Los movimientos de caja son inmutables';
END;
$$;

CREATE TRIGGER cash_movements_immutable
    BEFORE UPDATE OR DELETE ON cash_movements
    FOR EACH ROW EXECUTE FUNCTION reject_cash_movement_change();
