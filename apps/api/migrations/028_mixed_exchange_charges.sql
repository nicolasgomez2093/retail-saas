ALTER TABLE sale_return_exchange_charges ALTER COLUMN method DROP NOT NULL,
    ADD CHECK (method IS NOT NULL OR (cash_received IS NULL AND change_amount IS NULL));

CREATE TABLE sale_return_exchange_charge_payments (
    tenant_id uuid NOT NULL,
    exchange_id uuid NOT NULL,
    method text NOT NULL CHECK (method IN ('cash','transfer','debit','credit','qr')),
    amount numeric(18,2) NOT NULL CHECK (amount>0 AND amount <> 'NaN'::numeric),
    cash_received numeric(18,2) CHECK (cash_received>=0 AND cash_received <> 'NaN'::numeric),
    change_amount numeric(18,2) CHECK (change_amount>=0 AND change_amount <> 'NaN'::numeric),
    PRIMARY KEY (tenant_id,exchange_id,method),
    CHECK ((method='cash')=(cash_received IS NOT NULL)),
    CHECK ((method='cash')=(change_amount IS NOT NULL)),
    CHECK (method<>'cash' OR cash_received-change_amount=amount),
    FOREIGN KEY (tenant_id,exchange_id) REFERENCES sale_return_exchange_charges(tenant_id,exchange_id)
);

INSERT INTO sale_return_exchange_charge_payments(tenant_id,exchange_id,method,amount,cash_received,change_amount)
SELECT tenant_id,exchange_id,method,amount,cash_received,change_amount FROM sale_return_exchange_charges;

CREATE TRIGGER sale_return_exchange_charge_payments_immutable
    BEFORE UPDATE OR DELETE ON sale_return_exchange_charge_payments
    FOR EACH ROW EXECUTE FUNCTION reject_sale_return_change();
