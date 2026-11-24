CREATE TABLE sale_return_refund_charge_allocations (
    tenant_id uuid NOT NULL,
    refund_id uuid NOT NULL,
    method text NOT NULL,
    exchange_id uuid NOT NULL,
    amount numeric(18,2) NOT NULL CHECK (amount > 0 AND amount <> 'NaN'::numeric),
    PRIMARY KEY (tenant_id,refund_id,method,exchange_id),
    FOREIGN KEY (tenant_id,refund_id,method) REFERENCES sale_return_refund_allocations(tenant_id,refund_id,method),
    FOREIGN KEY (tenant_id,exchange_id,method) REFERENCES sale_return_exchange_charge_payments(tenant_id,exchange_id,method)
);

CREATE INDEX sale_return_refund_charge_allocations_by_exchange
    ON sale_return_refund_charge_allocations(tenant_id,exchange_id,method);

CREATE TRIGGER sale_return_refund_charge_allocations_immutable
    BEFORE UPDATE OR DELETE ON sale_return_refund_charge_allocations
    FOR EACH ROW EXECUTE FUNCTION reject_sale_return_change();
