ALTER TABLE receivables ADD UNIQUE (tenant_id,store_id,id);

CREATE TABLE receivable_payments (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    store_id uuid NOT NULL,
    receivable_id uuid NOT NULL,
    cash_session_id uuid NOT NULL,
    method text NOT NULL CHECK (method IN ('cash','transfer','debit','credit','qr')),
    amount numeric(18,2) NOT NULL CHECK (amount > 0 AND amount <> 'NaN'::numeric),
    balance_after numeric(18,2) NOT NULL CHECK (balance_after >= 0 AND balance_after <> 'NaN'::numeric),
    created_by uuid NOT NULL,
    idempotency_key text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,idempotency_key),
    FOREIGN KEY (tenant_id,store_id,receivable_id) REFERENCES receivables(tenant_id,store_id,id),
    FOREIGN KEY (tenant_id,cash_session_id) REFERENCES cash_sessions(tenant_id,id),
    FOREIGN KEY (tenant_id,created_by) REFERENCES users(tenant_id,id)
);
CREATE INDEX receivable_payments_history ON receivable_payments(tenant_id,store_id,receivable_id,created_at DESC,id DESC);

CREATE FUNCTION reject_receivable_payment_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'Los abonos registrados son inmutables';
END;
$$;

CREATE TRIGGER receivable_payments_immutable
    BEFORE UPDATE OR DELETE ON receivable_payments
    FOR EACH ROW EXECUTE FUNCTION reject_receivable_payment_change();

ALTER TABLE cash_movements ADD COLUMN receivable_payment_id uuid,
    ADD FOREIGN KEY (tenant_id,receivable_payment_id) REFERENCES receivable_payments(tenant_id,id),
    ADD CHECK ((movement_type='receivable_cash')=(receivable_payment_id IS NOT NULL)),
    ADD CHECK (movement_type<>'receivable_cash' OR (amount>0 AND sale_id IS NOT NULL));
CREATE UNIQUE INDEX cash_receivable_payment ON cash_movements(tenant_id,receivable_payment_id)
    WHERE receivable_payment_id IS NOT NULL;
