package inventory

import (
	"context"
	"strings"
	"unicode/utf8"

	"retail-platform/api/internal/modules/auth"

	"github.com/jackc/pgx/v5"
)

const ExistencePageSize = 25
const ExistenceLastPage = 4001

type ExistenceFilter struct {
	Query, Category, Brand, ProductStatus, VariantStatus, Balance, Location string
	LowOnly                                                                 bool
	Page                                                                    int
}

type ExistenceRow struct {
	ProductID         string  `json:"productId"`
	Product           string  `json:"product"`
	ProductStatus     string  `json:"productStatus"`
	VariantID         *string `json:"variantId"`
	VariantStatus     *string `json:"variantStatus"`
	SKU               string  `json:"sku"`
	Size              string  `json:"size"`
	Color             string  `json:"color"`
	OnHand            *int64  `json:"onHand"`
	Reserved          *int64  `json:"reserved"`
	Sellable          *int64  `json:"sellable"`
	LowStock          bool    `json:"lowStock"`
	RecordedLocations int64   `json:"recordedLocations"`
	Minimum           *int32  `json:"minimum"`
}

type ExistenceSummary struct {
	ProductsWithStock  int64 `json:"productsWithStock"`
	VariantsSellable   int64 `json:"variantsSellable"`
	VariantsZero       int64 `json:"variantsZero"`
	VariantsUnrecorded int64 `json:"variantsUnrecorded"`
	VariantsLow        int64 `json:"variantsLow"`
}

type ExistenceAccess struct {
	CanAdjust          bool `json:"canAdjust"`
	CanManageMinimums  bool `json:"canManageMinimums"`
	CanManageLocations bool `json:"canManageLocations"`
	CanTransfer        bool `json:"canTransfer"`
	CanCatalog         bool `json:"canCatalog"`
}

type ExistencePage struct {
	Items   []ExistenceRow   `json:"items"`
	Summary ExistenceSummary `json:"summary"`
	Access  ExistenceAccess  `json:"access"`
	HasMore bool             `json:"hasMore"`
}

type ExistenceLocation struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	Code           string  `json:"code"`
	Active         bool    `json:"active"`
	IsSellable     bool    `json:"isSellable"`
	OnHand         *int32  `json:"onHand"`
	Reserved       *int32  `json:"reserved"`
	Sellable       *int32  `json:"sellable"`
	Minimum        *int32  `json:"minimum"`
	LowStock       bool    `json:"lowStock"`
	Revision       *string `json:"revision"`
	MinimumVersion *string `json:"minimumVersion"`
}

type ExistenceDetail struct {
	Item      ExistenceRow        `json:"item"`
	Locations []ExistenceLocation `json:"locations"`
	Access    ExistenceAccess     `json:"access"`
}

func (a access) existenceAccess() ExistenceAccess {
	return ExistenceAccess{CanAdjust: a.adjust, CanManageMinimums: a.minimums, CanManageLocations: a.manage, CanTransfer: a.transfer, CanCatalog: a.catalog}
}

func (s Service) Existences(ctx context.Context, actor auth.Identity, storeID string, filter ExistenceFilter) (ExistencePage, error) {
	filter.Query = strings.TrimSpace(filter.Query)
	if !utf8.ValidString(filter.Query) || strings.ContainsRune(filter.Query, 0) || utf8.RuneCountInString(filter.Query) > 160 ||
		filter.Page < 1 || filter.Page > ExistenceLastPage ||
		!existenceStatus(filter.ProductStatus) || !existenceStatus(filter.VariantStatus) {
		return ExistencePage{}, ErrInvalid
	}
	for _, id := range []string{filter.Category, filter.Brand, filter.Location} {
		if id != "" && !validID(id) {
			return ExistencePage{}, ErrInvalid
		}
	}
	switch filter.Balance {
	case "", "positive", "zero", "unrecorded":
	default:
		return ExistencePage{}, ErrInvalid
	}
	var page ExistencePage
	err := s.withStore(ctx, actor, storeID, false, func(tx pgx.Tx, allowed access) error {
		if filter.Location != "" {
			if err := requireLocation(ctx, tx, actor.TenantID, storeID, filter.Location); err != nil {
				return err
			}
		}
		var err error
		page, err = queryExistences(ctx, tx, actor.TenantID, storeID, filter, "")
		page.Access = allowed.existenceAccess()
		return err
	})
	return page, err
}

func existenceStatus(status string) bool {
	return status == "" || status == "active" || status == "inactive"
}

func (s Service) Existence(ctx context.Context, actor auth.Identity, storeID, variantID string) (ExistenceDetail, error) {
	if !validID(variantID) {
		return ExistenceDetail{}, ErrInvalid
	}
	var detail ExistenceDetail
	err := s.withStore(ctx, actor, storeID, false, func(tx pgx.Tx, allowed access) error {
		page, err := queryExistences(ctx, tx, actor.TenantID, storeID, ExistenceFilter{Page: 1}, variantID)
		if err != nil {
			return err
		}
		if len(page.Items) == 0 {
			return ErrNotFound
		}
		detail.Item, detail.Access = page.Items[0], allowed.existenceAccess()
		active := detail.Item.ProductStatus == "active" && *detail.Item.VariantStatus == "active"
		rows, err := tx.Query(ctx, `SELECT l.id,l.name,l.code,l.status='active',l.is_sellable,
			b.quantity_on_hand,b.quantity_reserved,
			CASE WHEN b.variant_id IS NULL THEN NULL
				WHEN $4 AND l.status='active' AND l.is_sellable THEN b.quantity_on_hand-b.quantity_reserved ELSE 0 END,
			m.minimum,coalesce($4 AND l.status='active' AND coalesce(b.quantity_on_hand-b.quantity_reserved,0)<=m.minimum,false),
			b.revision::text,`+minimumVersionSQL+`
			FROM stock_locations l
			LEFT JOIN stock_balances b ON (b.tenant_id,b.location_id,b.variant_id)=(l.tenant_id,l.id,$3)
			LEFT JOIN stock_minimums m ON (m.tenant_id,m.location_id,m.variant_id)=(l.tenant_id,l.id,$3)
			WHERE l.tenant_id=$1 AND l.store_id=$2
			AND (l.status='active' OR b.variant_id IS NOT NULL OR m.variant_id IS NOT NULL)
			ORDER BY (l.status='active') DESC,l.code,l.id`, actor.TenantID, storeID, variantID, active)
		if err != nil {
			return err
		}
		detail.Locations, err = pgx.CollectRows(rows, pgx.RowToStructByPos[ExistenceLocation])
		return err
	})
	return detail, err
}

// The listing deliberately uses one set-based query. It distinguishes a
// missing balance from zero and aggregates saleable units only from active,
// sellable locations, as required by RequireSellableLocation.
func queryExistences(ctx context.Context, tx pgx.Tx, tenantID, storeID string, f ExistenceFilter, variantID string) (ExistencePage, error) {
	var page ExistencePage
	err := tx.QueryRow(ctx, `WITH scope_locations AS MATERIALIZED (
		SELECT id,status,is_sellable FROM stock_locations WHERE tenant_id=$1 AND store_id=$2
			AND (NULLIF($3,'') IS NULL OR id=NULLIF($3,'')::uuid)
	), candidates AS MATERIALIZED (
		SELECT p.id AS product_id,p.name AS product,p.status AS product_status,v.id AS variant_id,
			v.status AS variant_status,coalesce(v.sku,'') AS sku,coalesce(v.size,'') AS size,coalesce(v.color,'') AS color
		FROM products p LEFT JOIN product_variants v ON (v.tenant_id,v.product_id)=(p.tenant_id,p.id)
		WHERE p.tenant_id=$1 AND ($4='' OR strpos(lower(p.name),lower($4))>0 OR strpos(lower(v.sku),lower($4))>0)
			AND (NULLIF($5,'') IS NULL OR p.category_id=NULLIF($5,'')::uuid)
			AND (NULLIF($6,'') IS NULL OR p.brand_id=NULLIF($6,'')::uuid)
			AND ($7='' OR p.status=$7) AND ($8='' OR v.status=$8)
			AND (NULLIF($12,'') IS NULL OR v.id=NULLIF($12,'')::uuid)
	), tracked AS (
		SELECT b.location_id,b.variant_id FROM stock_balances b JOIN scope_locations l ON l.id=b.location_id WHERE b.tenant_id=$1 AND (NULLIF($12,'') IS NULL OR b.variant_id=NULLIF($12,'')::uuid)
		UNION
		SELECT m.location_id,m.variant_id FROM stock_minimums m JOIN scope_locations l ON l.id=m.location_id WHERE m.tenant_id=$1 AND (NULLIF($12,'') IS NULL OR m.variant_id=NULLIF($12,'')::uuid)
	), balances AS (
		SELECT t.variant_id,count(b.variant_id) recorded,sum(b.quantity_on_hand) on_hand,sum(b.quantity_reserved) reserved,
			CASE WHEN $3<>'' THEN max(m.minimum) END minimum,
			sum(CASE WHEN b.variant_id IS NULL THEN NULL
				WHEN c.product_status='active' AND c.variant_status='active' AND l.status='active' AND l.is_sellable
				THEN b.quantity_on_hand-b.quantity_reserved ELSE 0 END) sellable,
			bool_or(coalesce(c.product_status='active' AND c.variant_status='active' AND l.status='active'
				AND coalesce(b.quantity_on_hand-b.quantity_reserved,0)<=m.minimum,false)) low_stock
		FROM tracked t JOIN scope_locations l ON l.id=t.location_id JOIN candidates c ON c.variant_id=t.variant_id
		LEFT JOIN stock_balances b ON (b.tenant_id,b.location_id,b.variant_id)=($1,t.location_id,t.variant_id)
		LEFT JOIN stock_minimums m ON (m.tenant_id,m.location_id,m.variant_id)=($1,t.location_id,t.variant_id)
		GROUP BY t.variant_id
	), matches AS MATERIALIZED (
		SELECT c.*,b.on_hand,b.reserved,b.sellable,b.minimum,coalesce(b.recorded,0) recorded,coalesce(b.low_stock,false) low_stock
		FROM candidates c LEFT JOIN balances b ON b.variant_id=c.variant_id
		WHERE (NOT $9 OR b.low_stock) AND ($10='' OR
			(c.variant_id IS NOT NULL AND ($10='positive' AND b.sellable>0 OR $10='zero' AND b.sellable=0 OR $10='unrecorded' AND b.on_hand IS NULL)))
	)
	SELECT (SELECT jsonb_build_object(
		'productsWithStock',count(DISTINCT product_id) FILTER (WHERE on_hand>0),
		'variantsSellable',count(*) FILTER (WHERE sellable>0),
		'variantsZero',count(*) FILTER (WHERE sellable=0),
		'variantsUnrecorded',count(*) FILTER (WHERE variant_id IS NOT NULL AND on_hand IS NULL),
		'variantsLow',count(*) FILTER (WHERE low_stock)
	) FROM matches),
	(SELECT coalesce(jsonb_agg(jsonb_build_object(
			'productId',product_id,'product',product,'productStatus',product_status,'variantId',variant_id,'variantStatus',variant_status,
			'sku',sku,'size',size,'color',color,'onHand',on_hand,'reserved',reserved,'sellable',sellable,
			'lowStock',low_stock,'recordedLocations',recorded,'minimum',minimum
		) ORDER BY product,product_id,sku,variant_id),'[]'::jsonb)
		FROM (SELECT * FROM matches ORDER BY product,product_id,sku,variant_id LIMIT 26 OFFSET $11) ordered)`,
		tenantID, storeID, f.Location, f.Query, f.Category, f.Brand, f.ProductStatus, f.VariantStatus, f.LowOnly, f.Balance, (f.Page-1)*ExistencePageSize, variantID).
		Scan(&page.Summary, &page.Items)
	if len(page.Items) > ExistencePageSize {
		page.Items = page.Items[:ExistencePageSize]
		page.HasMore = f.Page < ExistenceLastPage
	}
	return page, err
}
