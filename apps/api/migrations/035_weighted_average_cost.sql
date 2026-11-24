ALTER TABLE sale_items ADD COLUMN unit_cost_snapshot numeric(18,2),
    ADD CHECK (unit_cost_snapshot IS NULL OR (unit_cost_snapshot>=0 AND unit_cost_snapshot<>'NaN'::numeric));

CREATE TABLE inventory_cost_balances (
    tenant_id uuid NOT NULL,
    store_id uuid NOT NULL,
    variant_id uuid NOT NULL,
    quantity_on_hand bigint NOT NULL DEFAULT 0 CHECK (quantity_on_hand>=0),
    average_unit_cost numeric(18,2) CHECK (average_unit_cost IS NULL OR (average_unit_cost>=0 AND average_unit_cost<>'NaN'::numeric)),
    status text NOT NULL DEFAULT 'unknown' CHECK (status IN ('known','unknown')),
    updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
    PRIMARY KEY (tenant_id,store_id,variant_id),
    CHECK ((status='known')=(average_unit_cost IS NOT NULL)),
    FOREIGN KEY (tenant_id,store_id) REFERENCES stores(tenant_id,id),
    FOREIGN KEY (tenant_id,variant_id) REFERENCES product_variants(tenant_id,id)
);

INSERT INTO inventory_cost_balances(tenant_id,store_id,variant_id,quantity_on_hand)
SELECT b.tenant_id,l.store_id,b.variant_id,sum(b.quantity_on_hand)
FROM stock_balances b JOIN stock_locations l ON (l.tenant_id,l.id)=(b.tenant_id,b.location_id)
GROUP BY b.tenant_id,l.store_id,b.variant_id
HAVING sum(b.quantity_on_hand)>0;

CREATE FUNCTION sync_inventory_cost_quantity() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
    v_store_id uuid;
    v_delta integer;
BEGIN
    SELECT store_id INTO v_store_id FROM stock_locations
    WHERE tenant_id=NEW.tenant_id AND id=NEW.location_id;
    v_delta := NEW.quantity_on_hand - CASE WHEN TG_OP='INSERT' THEN 0 ELSE OLD.quantity_on_hand END;
    INSERT INTO inventory_cost_balances(tenant_id,store_id,variant_id,quantity_on_hand)
    VALUES (NEW.tenant_id,v_store_id,NEW.variant_id,NEW.quantity_on_hand)
    ON CONFLICT (tenant_id,store_id,variant_id) DO UPDATE
    SET quantity_on_hand=inventory_cost_balances.quantity_on_hand+v_delta,
        status=CASE WHEN inventory_cost_balances.quantity_on_hand+v_delta=0 THEN 'unknown' ELSE inventory_cost_balances.status END,
        average_unit_cost=CASE WHEN inventory_cost_balances.quantity_on_hand+v_delta=0 THEN NULL ELSE inventory_cost_balances.average_unit_cost END,
        updated_at=clock_timestamp();
    RETURN NEW;
END;
$$;

CREATE TRIGGER stock_balance_cost_quantity
    AFTER INSERT OR UPDATE OF quantity_on_hand ON stock_balances
    FOR EACH ROW EXECUTE FUNCTION sync_inventory_cost_quantity();

CREATE FUNCTION invalidate_unknown_inventory_cost() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.quantity_delta>0 AND NEW.movement_type<>'purchase_receipt' THEN
        UPDATE inventory_cost_balances c SET status='unknown',average_unit_cost=NULL,updated_at=clock_timestamp()
        WHERE c.tenant_id=NEW.tenant_id AND c.variant_id=NEW.variant_id
          AND c.store_id=(SELECT store_id FROM stock_locations WHERE tenant_id=NEW.tenant_id AND id=NEW.location_id);
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER stock_movement_unknown_cost
    AFTER INSERT ON stock_movements
    FOR EACH ROW EXECUTE FUNCTION invalidate_unknown_inventory_cost();
