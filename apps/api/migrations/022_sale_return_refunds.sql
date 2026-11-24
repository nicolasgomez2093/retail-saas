CREATE TABLE sale_return_refunds (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    store_id uuid NOT NULL,
    sale_id uuid NOT NULL,
    return_id uuid NOT NULL,
    cash_session_id uuid NOT NULL,
    amount numeric(18,2) NOT NULL CHECK (amount > 0 AND amount <> 'NaN'::numeric),
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    idempotency_key text NOT NULL,
    UNIQUE (tenant_id,id),
    UNIQUE (tenant_id,idempotency_key),
    UNIQUE (tenant_id,store_id,sale_id,return_id),
    FOREIGN KEY (tenant_id,store_id,sale_id,return_id) REFERENCES sale_returns(tenant_id,store_id,sale_id,id),
    FOREIGN KEY (tenant_id,cash_session_id) REFERENCES cash_sessions(tenant_id,id),
    FOREIGN KEY (tenant_id,created_by) REFERENCES users(tenant_id,id)
);

CREATE TRIGGER sale_return_refunds_immutable
    BEFORE UPDATE OR DELETE ON sale_return_refunds
    FOR EACH ROW EXECUTE FUNCTION reject_sale_return_change();

ALTER TABLE cash_movements ADD COLUMN return_refund_id uuid,
    ADD FOREIGN KEY (tenant_id,return_refund_id) REFERENCES sale_return_refunds(tenant_id,id),
    ADD CHECK ((movement_type='sale_refund_cash')=(return_refund_id IS NOT NULL)),
    ADD CHECK (movement_type<>'sale_refund_cash' OR (amount<0 AND sale_id IS NOT NULL));
CREATE UNIQUE INDEX cash_return_refund ON cash_movements(tenant_id,return_refund_id)
    WHERE return_refund_id IS NOT NULL;
