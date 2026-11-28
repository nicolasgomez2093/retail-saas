-- Public storefront: one per commerce, publishing the catalog, prices and availability of one branch.
-- Orders are not stored: the shopper sends the cart to the commerce WhatsApp.
INSERT INTO permissions(code) VALUES ('storefront:manage');
INSERT INTO role_permissions(role_code, permission_code) VALUES ('OWNER', 'storefront:manage');

CREATE TABLE storefronts (
    tenant_id uuid PRIMARY KEY REFERENCES tenants(id),
    enabled boolean NOT NULL DEFAULT false,
    store_id uuid,
    whatsapp text NOT NULL DEFAULT '' CHECK (whatsapp = '' OR whatsapp ~ '^[1-9][0-9]{7,14}$'),
    title text NOT NULL DEFAULT '' CHECK (char_length(title) <= 80),
    welcome text NOT NULL DEFAULT '' CHECK (char_length(welcome) <= 300),
    show_sold_out boolean NOT NULL DEFAULT true,
    updated_at timestamptz NOT NULL DEFAULT now(),
    updated_by uuid,
    FOREIGN KEY (tenant_id, store_id) REFERENCES stores(tenant_id, id),
    FOREIGN KEY (tenant_id, updated_by) REFERENCES users(tenant_id, id),
    -- A published storefront always has a branch and a WhatsApp number to receive orders.
    CHECK (NOT enabled OR (store_id IS NOT NULL AND whatsapp <> ''))
);

-- Every active product with a photo is published unless the commerce hides it.
ALTER TABLE products ADD COLUMN storefront_hidden boolean NOT NULL DEFAULT false;
