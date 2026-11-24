CREATE TABLE sale_return_debt_credits (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    store_id uuid NOT NULL,
    sale_id uuid NOT NULL,
    return_id uuid NOT NULL,
    amount numeric(18,2) NOT NULL CHECK (amount>0 AND amount <> 'NaN'::numeric),
    balance_after numeric(18,2) NOT NULL CHECK (balance_after>=0 AND balance_after <> 'NaN'::numeric),
    created_by uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    idempotency_key text NOT NULL,
    UNIQUE (tenant_id,idempotency_key),
    FOREIGN KEY (tenant_id,store_id,sale_id,return_id) REFERENCES sale_returns(tenant_id,store_id,sale_id,id),
    FOREIGN KEY (tenant_id,store_id,sale_id) REFERENCES receivables(tenant_id,store_id,sale_id),
    FOREIGN KEY (tenant_id,created_by) REFERENCES users(tenant_id,id)
);

CREATE INDEX sale_return_debt_credits_by_receipt ON sale_return_debt_credits(tenant_id,store_id,sale_id,return_id,created_at,id);
CREATE INDEX sale_return_debt_credits_by_sale ON sale_return_debt_credits(tenant_id,store_id,sale_id,created_at DESC,id DESC);

CREATE TRIGGER sale_return_debt_credits_immutable
    BEFORE UPDATE OR DELETE ON sale_return_debt_credits
    FOR EACH ROW EXECUTE FUNCTION reject_sale_return_change();
