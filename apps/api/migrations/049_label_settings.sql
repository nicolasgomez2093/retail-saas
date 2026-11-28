-- Whether printed shelf and garment labels carry the price. A commerce-wide choice made in Configuración.
ALTER TABLE tenants ADD COLUMN label_show_price boolean NOT NULL DEFAULT true;
