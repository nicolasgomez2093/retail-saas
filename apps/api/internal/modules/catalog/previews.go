package catalog

import (
	"context"

	"retail-platform/api/internal/modules/auth"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

type Preview struct {
	ProductID     string  `json:"productId"`
	PhotoID       *string `json:"photoId"`
	Currency      *string `json:"currency"`
	MinPrice      *string `json:"minPrice"`
	MaxPrice      *string `json:"maxPrice"`
	PricePartial  bool    `json:"pricePartial"`
	StockReadable bool    `json:"stockReadable"`
	Available     *int64  `json:"available"`
}

func listPreviews(ctx context.Context, tx pgx.Tx, actor auth.Identity, store pgtype.UUID, products []Product) ([]Preview, error) {
	ids := make([]pgtype.UUID, 0, len(products))
	for _, product := range products {
		id, _ := parseID(product.ID)
		ids = append(ids, id)
	}
	rows, err := tx.Query(ctx, `WITH access AS (
		SELECT s.id,t.currency_code,
			EXISTS (SELECT 1 FROM user_roles ur JOIN role_permissions rp ON rp.role_code=ur.role_code
				WHERE ur.tenant_id=$1 AND ur.user_id=$2 AND rp.permission_code='pricing:read') AS price_read,
			EXISTS (SELECT 1 FROM user_roles ur JOIN role_permissions rp ON rp.role_code=ur.role_code
				WHERE ur.tenant_id=$1 AND ur.user_id=$2 AND rp.permission_code='inventory:read') AS stock_read
		FROM user_stores us JOIN stores s ON (s.tenant_id,s.id)=(us.tenant_id,us.store_id)
		JOIN tenants t ON t.id=s.tenant_id
		JOIN users u ON (u.tenant_id,u.id)=(us.tenant_id,us.user_id)
		WHERE us.tenant_id=$1 AND us.user_id=$2 AND us.store_id=$3
			AND s.status='active' AND u.status='active' AND t.status='active'
	)
	SELECT p.id::text,photo.id::text,CASE WHEN a.price_read AND p.status='active' THEN a.currency_code END,
		price.minimum::text,price.maximum::text,coalesce(price.partial,false),coalesce(a.stock_read AND p.status='active',false),stock.available
	FROM products p
	LEFT JOIN access a ON true
	LEFT JOIN LATERAL (
		SELECT id FROM product_images WHERE tenant_id=p.tenant_id AND product_id=p.id
		ORDER BY is_primary DESC,created_at,id LIMIT 1
	) photo ON true
	LEFT JOIN LATERAL (
		SELECT min(i.amount) AS minimum,max(i.amount) AS maximum,
			count(i.amount)>0 AND count(i.amount)<count(*) AS partial
		FROM product_variants v
		LEFT JOIN store_price_list_assignments l ON l.tenant_id=v.tenant_id AND l.store_id=a.id
		LEFT JOIN price_list_items i ON (i.tenant_id,i.price_list_id,i.variant_id)=(l.tenant_id,l.price_list_id,v.id)
		WHERE a.price_read AND p.status='active' AND v.status='active'
			AND (v.tenant_id,v.product_id)=(p.tenant_id,p.id)
	) price ON true
	LEFT JOIN LATERAL (
		SELECT sum(b.quantity_on_hand-b.quantity_reserved) AS available
		FROM product_variants v
		JOIN stock_balances b ON (b.tenant_id,b.variant_id)=(v.tenant_id,v.id)
		JOIN stock_locations sl ON (sl.tenant_id,sl.id)=(b.tenant_id,b.location_id)
		WHERE a.stock_read AND sl.store_id=a.id AND sl.status='active' AND sl.is_sellable
			AND p.status='active' AND v.status='active' AND (v.tenant_id,v.product_id)=(p.tenant_id,p.id)
	) stock ON true
	WHERE p.tenant_id=$1 AND p.id=ANY($4)`, actor.TenantID, actor.UserID, store, ids)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Preview])
}
