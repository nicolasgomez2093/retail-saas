CREATE TABLE catalog_attributes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id uuid NOT NULL REFERENCES tenants(id),
    kind text NOT NULL CHECK (kind IN ('sizes','colors','size-groups')),
    name text NOT NULL CHECK (char_length(btrim(name)) BETWEEN 1 AND 60),
    code text CHECK (char_length(btrim(code)) BETWEEN 1 AND 60),
    position integer NOT NULL DEFAULT 0 CHECK (position BETWEEN 0 AND 9999),
    status text NOT NULL DEFAULT 'active' CHECK (status IN ('active','inactive')),
    hex text CHECK (hex ~ '^#[0-9a-f]{6}$'),
    group_id uuid,
    group_kind text GENERATED ALWAYS AS ('size-groups'::text) STORED,
    revision bigint NOT NULL DEFAULT 1 CHECK (revision > 0),
    UNIQUE (tenant_id,kind,id),
    FOREIGN KEY (tenant_id,group_kind,group_id) REFERENCES catalog_attributes(tenant_id,kind,id),
    CHECK (group_id IS NULL OR kind = 'sizes'),
    CHECK (hex IS NULL OR kind = 'colors'),
    CHECK (kind <> 'colors' OR code IS NOT NULL),
    CHECK (kind <> 'size-groups' OR code IS NULL)
);

CREATE UNIQUE INDEX catalog_attributes_name_key ON catalog_attributes(tenant_id,kind,lower(btrim(name)));
CREATE UNIQUE INDEX catalog_attributes_code_key ON catalog_attributes(tenant_id,kind,lower(btrim(code))) WHERE code IS NOT NULL;

ALTER TABLE product_variants
    ADD COLUMN size_id uuid,
    ADD COLUMN color_id uuid,
    ADD COLUMN size_kind text NOT NULL DEFAULT 'sizes' CHECK (size_kind='sizes'),
    ADD COLUMN color_kind text NOT NULL DEFAULT 'colors' CHECK (color_kind='colors'),
    ADD FOREIGN KEY (tenant_id,size_kind,size_id) REFERENCES catalog_attributes(tenant_id,kind,id),
    ADD FOREIGN KEY (tenant_id,color_kind,color_id) REFERENCES catalog_attributes(tenant_id,kind,id);

-- Create tenant-local values only for spelling groups with a single original
-- representation. Case/spacing collisions stay unresolved for manual review.
WITH legacy AS (
    SELECT tenant_id, 'sizes' AS kind, size AS label FROM product_variants
    UNION ALL
    SELECT tenant_id, 'colors' AS kind, color AS label FROM product_variants
), unambiguous AS (
    SELECT tenant_id,kind,min(btrim(label)) AS label
    FROM legacy
    WHERE btrim(label) <> ''
    GROUP BY tenant_id,kind,lower(btrim(label))
    HAVING count(DISTINCT label COLLATE "C")=1 AND char_length(min(btrim(label)))<=60
)
INSERT INTO catalog_attributes(tenant_id,kind,name,code)
SELECT tenant_id,kind,label,CASE WHEN kind='colors' THEN lower(label) END FROM unambiguous;

UPDATE product_variants v SET size_id=a.id
FROM catalog_attributes a
WHERE a.tenant_id=v.tenant_id AND a.kind='sizes' AND a.name COLLATE "C"=btrim(v.size) COLLATE "C";

UPDATE product_variants v SET color_id=a.id
FROM catalog_attributes a
WHERE a.tenant_id=v.tenant_id AND a.kind='colors' AND a.name COLLATE "C"=btrim(v.color) COLLATE "C";

CREATE INDEX variants_pending_attributes ON product_variants(tenant_id,sku,id)
WHERE (size_id IS NULL AND size<>'') OR (color_id IS NULL AND color<>'');
