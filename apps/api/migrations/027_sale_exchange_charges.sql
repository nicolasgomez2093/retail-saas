CREATE TABLE sale_return_exchange_charges (
    tenant_id uuid NOT NULL,
    exchange_id uuid NOT NULL,
    cash_session_id uuid NOT NULL,
    method text NOT NULL CHECK (method IN ('cash','transfer','debit','credit','qr')),
    amount numeric(18,2) NOT NULL CHECK (amount>0 AND amount <> 'NaN'::numeric),
    cash_received numeric(18,2) CHECK (cash_received>=0 AND cash_received <> 'NaN'::numeric),
    change_amount numeric(18,2) CHECK (change_amount>=0 AND change_amount <> 'NaN'::numeric),
    PRIMARY KEY (tenant_id,exchange_id),
    CHECK ((method='cash')=(cash_received IS NOT NULL)),
    CHECK ((method='cash')=(change_amount IS NOT NULL)),
    CHECK (method<>'cash' OR cash_received-change_amount=amount),
    FOREIGN KEY (tenant_id,exchange_id) REFERENCES sale_return_exchanges(tenant_id,id),
    FOREIGN KEY (tenant_id,cash_session_id) REFERENCES cash_sessions(tenant_id,id)
);

CREATE TRIGGER sale_return_exchange_charges_immutable
    BEFORE UPDATE OR DELETE ON sale_return_exchange_charges
    FOR EACH ROW EXECUTE FUNCTION reject_sale_return_change();

ALTER TABLE cash_movements ADD COLUMN exchange_id uuid,
    ADD FOREIGN KEY (tenant_id,exchange_id) REFERENCES sale_return_exchanges(tenant_id,id),
    ADD CHECK ((movement_type='sale_exchange_cash')=(exchange_id IS NOT NULL)),
    ADD CHECK (movement_type<>'sale_exchange_cash' OR (amount>0 AND sale_id IS NOT NULL));
CREATE UNIQUE INDEX cash_sale_exchange ON cash_movements(tenant_id,exchange_id) WHERE exchange_id IS NOT NULL;
