ALTER TABLE sales ADD COLUMN merchant_name_snapshot text,
    ADD COLUMN store_code_snapshot text,
    ADD COLUMN store_name_snapshot text,
    ADD COLUMN store_address_snapshot text,
    ADD COLUMN timezone_snapshot text;

UPDATE sales s
SET merchant_name_snapshot=t.name, store_code_snapshot=r.code, store_name_snapshot=r.name,
    store_address_snapshot=r.address, timezone_snapshot=t.timezone
FROM stores r JOIN tenants t ON t.id=r.tenant_id
WHERE (s.tenant_id,s.store_id)=(r.tenant_id,r.id);

ALTER TABLE sales ALTER COLUMN merchant_name_snapshot SET NOT NULL,
    ALTER COLUMN store_code_snapshot SET NOT NULL,
    ALTER COLUMN store_name_snapshot SET NOT NULL,
    ALTER COLUMN store_address_snapshot SET NOT NULL,
    ALTER COLUMN timezone_snapshot SET NOT NULL;
