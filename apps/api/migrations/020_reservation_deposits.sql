ALTER TABLE reservations ADD UNIQUE (tenant_id,store_id,id);

CREATE TABLE reservation_deposits (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    store_id uuid NOT NULL,
    reservation_id uuid NOT NULL,
    cash_session_id uuid NOT NULL,
    method text NOT NULL CHECK (method IN ('cash','transfer','debit','credit','qr')),
    amount numeric(18,2) NOT NULL CHECK (amount>0 AND amount <> 'NaN'::numeric),
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    idempotency_key text NOT NULL,
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,store_id,reservation_id),
    UNIQUE (tenant_id,idempotency_key),
    FOREIGN KEY (tenant_id,store_id,reservation_id) REFERENCES reservations(tenant_id,store_id,id),
    FOREIGN KEY (tenant_id,cash_session_id) REFERENCES cash_sessions(tenant_id,id),
    FOREIGN KEY (tenant_id,created_by) REFERENCES users(tenant_id,id)
);

CREATE FUNCTION reject_reservation_deposit_change() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION 'Las señas registradas son inmutables';
END;
$$;

CREATE TRIGGER reservation_deposits_immutable
    BEFORE UPDATE OR DELETE ON reservation_deposits
    FOR EACH ROW EXECUTE FUNCTION reject_reservation_deposit_change();

ALTER TABLE cash_movements ADD COLUMN reservation_deposit_id uuid,
    ADD FOREIGN KEY (tenant_id,reservation_deposit_id) REFERENCES reservation_deposits(tenant_id,id),
    ADD CHECK ((movement_type='reservation_deposit_cash')=(reservation_deposit_id IS NOT NULL)),
    ADD CHECK (movement_type<>'reservation_deposit_cash' OR (amount>0 AND sale_id IS NULL));
CREATE UNIQUE INDEX cash_reservation_deposit ON cash_movements(tenant_id,reservation_deposit_id)
    WHERE reservation_deposit_id IS NOT NULL;
