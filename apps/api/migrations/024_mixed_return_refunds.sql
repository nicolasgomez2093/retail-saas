ALTER TABLE sale_return_refunds ALTER COLUMN method DROP NOT NULL;

CREATE TABLE sale_return_refund_allocations (
    tenant_id uuid NOT NULL,
    refund_id uuid NOT NULL,
    method text NOT NULL CHECK (method IN ('cash','transfer','debit','credit','qr')),
    amount numeric(18,2) NOT NULL CHECK (amount > 0 AND amount <> 'NaN'::numeric),
    PRIMARY KEY (tenant_id,refund_id,method),
    FOREIGN KEY (tenant_id,refund_id) REFERENCES sale_return_refunds(tenant_id,id)
);

INSERT INTO sale_return_refund_allocations(tenant_id,refund_id,method,amount)
SELECT tenant_id,id,method,amount FROM sale_return_refunds;

CREATE TRIGGER sale_return_refund_allocations_immutable
    BEFORE UPDATE OR DELETE ON sale_return_refund_allocations
    FOR EACH ROW EXECUTE FUNCTION reject_sale_return_change();
