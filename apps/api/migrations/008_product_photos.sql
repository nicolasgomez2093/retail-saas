CREATE TABLE product_images (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL,
    product_id uuid NOT NULL,
    width integer NOT NULL CHECK (width BETWEEN 1 AND 4096),
    height integer NOT NULL CHECK (height BETWEEN 1 AND 4096),
    is_primary boolean NOT NULL DEFAULT false,
    jpeg_data bytea NOT NULL CHECK (octet_length(jpeg_data) BETWEEN 1 AND 5242880),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (tenant_id,product_id) REFERENCES products(tenant_id,id)
);

CREATE INDEX product_images_by_product ON product_images(tenant_id,product_id,created_at,id);
CREATE UNIQUE INDEX product_images_one_primary ON product_images(tenant_id,product_id) WHERE is_primary;
