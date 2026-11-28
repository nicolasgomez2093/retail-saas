-- Look of the public storefront: the commerce logo (stored as PNG, at most 512 px) and its main color.
ALTER TABLE storefronts
    ADD COLUMN accent text NOT NULL DEFAULT '' CHECK (accent = '' OR accent ~ '^#[0-9a-f]{6}$'),
    ADD COLUMN logo_png bytea CHECK (logo_png IS NULL OR octet_length(logo_png) <= 1048576),
    ADD COLUMN logo_version integer NOT NULL DEFAULT 0;
