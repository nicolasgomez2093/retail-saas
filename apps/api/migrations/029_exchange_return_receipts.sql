ALTER TABLE sale_return_exchanges
    ADD UNIQUE (tenant_id,store_id,sale_id,id);

ALTER TABLE sale_returns
    ADD COLUMN source_exchange_id uuid,
    ADD COLUMN source_id uuid GENERATED ALWAYS AS (COALESCE(source_exchange_id,id)) STORED,
    ADD CHECK (source_exchange_id <> id),
    ADD UNIQUE (tenant_id,store_id,sale_id,id,source_id),
    ADD FOREIGN KEY (tenant_id,store_id,sale_id,source_exchange_id) REFERENCES sale_return_exchanges(tenant_id,store_id,sale_id,id);

ALTER TABLE sale_return_items
    ADD COLUMN source_exchange_id uuid,
    ADD COLUMN source_id uuid GENERATED ALWAYS AS (COALESCE(source_exchange_id,return_id)) STORED,
    ADD COLUMN original_variant_id uuid GENERATED ALWAYS AS (CASE WHEN source_exchange_id IS NULL THEN variant_id END) STORED,
    ADD CHECK (source_exchange_id <> return_id),
    DROP CONSTRAINT sale_return_items_tenant_id_sale_id_variant_id_fkey,
    ADD FOREIGN KEY (tenant_id,store_id,sale_id,return_id,source_id) REFERENCES sale_returns(tenant_id,store_id,sale_id,id,source_id),
    ADD FOREIGN KEY (tenant_id,sale_id,original_variant_id) REFERENCES sale_items(tenant_id,sale_id,variant_id),
    ADD FOREIGN KEY (tenant_id,source_exchange_id,variant_id) REFERENCES sale_return_exchange_items(tenant_id,exchange_id,variant_id);

CREATE INDEX sale_return_items_by_exchange ON sale_return_items(tenant_id,source_exchange_id,variant_id)
    WHERE source_exchange_id IS NOT NULL;
