package purchases

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/tenancy"
	"retail-platform/api/internal/platform/money"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrInvalid = errors.New("verificá los datos de la orden de compra")
var ErrNotFound = errors.New("sucursal u orden de compra inexistente")
var ErrSupplier = errors.New("elegí un proveedor activo del comercio")
var ErrVariant = errors.New("la orden contiene una variante que ya no está activa o disponible")
var ErrKey = errors.New("la clave de idempotencia ya se usó para otra orden de compra")
var ErrClosed = errors.New("el pedido ya está recibido o cancelado y no admite cambios")

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

type ItemInput struct {
	VariantID string  `json:"variantId"`
	Quantity  int32   `json:"quantity"`
	UnitCost  *string `json:"unitCost"`
}

type OrderInput struct {
	StoreID    string      `json:"storeId"`
	SupplierID string      `json:"supplierId"`
	Reference  string      `json:"reference,omitempty"`
	Items      []ItemInput `json:"items"`
}

type CancelInput struct {
	StoreID string `json:"storeId"`
	Reason  string `json:"reason"`
}

type Item struct {
	ItemInput
	Received  int32  `json:"received"`
	Rejected  int32  `json:"rejected"`
	ID        string `json:"id"`
	ProductID string `json:"productId"`
	Product   string `json:"product"`
	SKU       string `json:"sku"`
	Size      string `json:"size"`
	Color     string `json:"color"`
}

type Order struct {
	ID           string     `json:"id"`
	Number       int32      `json:"number"`
	StoreID      string     `json:"storeId"`
	SupplierID   string     `json:"supplierId"`
	SupplierName string     `json:"supplierName"`
	Reference    string     `json:"reference"`
	Currency     string     `json:"currency"`
	Status       string     `json:"status"`
	OrderedAt    time.Time  `json:"orderedAt"`
	CreatedBy    string     `json:"createdBy"`
	CancelledAt  *time.Time `json:"cancelledAt"`
	CancelReason string     `json:"cancelReason"`
	Items        []Item     `json:"items"`
}

type Page struct {
	Items     []Order      `json:"items"`
	HasMore   bool         `json:"hasMore"`
	CanManage bool         `json:"canManage"`
	Timezone  string       `json:"timezone"`
	Currency  string       `json:"currency"`
	Summary   OrderSummary `json:"summary"`
}

type OrderSummary struct {
	OrderCount    int64 `json:"orderCount"`
	OrderedUnits  int64 `json:"orderedUnits"`
	ReceivedUnits int64 `json:"receivedUnits"`
	PendingUnits  int64 `json:"pendingUnits"`
	// Orders still waiting for goods and orders fully received, for the
	// metrics at the top of Compras. Cancelled orders count in neither and
	// their remaining units are not pending.
	PendingOrders  int64 `json:"pendingOrders"`
	ReceivedOrders int64 `json:"receivedOrders"`
}

type ListFilter struct {
	SupplierID    string
	Status        string
	Query         string
	FromDate      string
	ToDate        string
	Limit, Offset int
}

type Service struct{ db *pgxpool.Pool }

func New(db *pgxpool.Pool) Service { return Service{db: db} }

const orderScope = ` FROM purchase_orders o WHERE o.tenant_id=$1 AND o.store_id=$2`

const orderListFilters = `
	AND ($3='' OR o.status=$3 OR ($3='pending' AND o.status IN ('OPEN','PARTIALLY_RECEIVED')))
	AND ($4='' OR strpos(lower(o.supplier_name),lower($4))>0 OR strpos(lower(o.reference),lower($4))>0
		OR strpos('oc-'||lpad(o.number::text,4,'0'),lower($4))>0 OR EXISTS (
		SELECT 1 FROM purchase_order_items i
		WHERE (i.tenant_id,i.purchase_order_id)=(o.tenant_id,o.id)
		AND (strpos(lower(i.sku),lower($4))>0 OR strpos(lower(i.product_name),lower($4))>0)
	))
	AND ($5::text='' OR (o.ordered_at AT TIME ZONE $7)::date >= NULLIF($5,'')::date)
	AND ($6::text='' OR (o.ordered_at AT TIME ZONE $7)::date <= NULLIF($6,'')::date)
	AND (NULLIF($8::text,'') IS NULL OR o.supplier_id=NULLIF($8,'')::uuid)`

const selectOrders = `SELECT o.id,o.number,o.store_id,o.supplier_id,o.supplier_name,o.reference,o.currency,o.status,o.ordered_at,o.created_by,
	o.cancelled_at,o.cancel_reason,
	(SELECT jsonb_agg(jsonb_build_object('id',i.id,'variantId',i.variant_id,'productId',v.product_id,'quantity',i.ordered_qty,'unitCost',i.unit_cost::text,
		'product',i.product_name,'sku',i.sku,'size',i.size,'color',i.color,'received',COALESCE(received.quantity,0),
		'rejected',COALESCE(received.rejected,0)) ORDER BY i.sku,i.id)
	 FROM purchase_order_items i JOIN product_variants v ON (v.tenant_id,v.id)=(i.tenant_id,i.variant_id) CROSS JOIN LATERAL (
	    SELECT sum(r.accepted_qty) AS quantity,sum(r.rejected_qty) AS rejected FROM purchase_receipt_items r
	    WHERE (r.tenant_id,r.purchase_order_id,r.order_item_id)=(i.tenant_id,i.purchase_order_id,i.id)
	 ) received WHERE (i.tenant_id,i.purchase_order_id)=(o.tenant_id,o.id))` + orderScope

func (s Service) List(ctx context.Context, actor auth.Identity, storeID string, filter ListFilter) (Page, error) {
	if filter.Limit < 1 || filter.Limit > 50 || filter.Offset < 0 || filter.Offset > 100000 {
		return Page{}, ErrInvalid
	}
	filter, err := normalizeOrderFilter(storeID, filter)
	if err != nil {
		return Page{}, err
	}
	var page Page
	err = s.withStore(ctx, actor, storeID, false, func(tx pgx.Tx, access orderAccess) error {
		page.CanManage, page.Timezone, page.Currency = access.manage, access.timezone, access.currency
		var err error
		args := orderFilterArgs(actor.TenantID, storeID, access.timezone, filter)
		page.Summary, err = summarizeOrders(ctx, tx, args)
		if err != nil {
			return err
		}
		limit := filter.Limit + 1
		rows, err := queryOrderList(ctx, tx, args, &limit, filter.Offset)
		if err != nil {
			return err
		}
		page.Items, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Order, error) { return scanOrder(row) })
		if len(page.Items) > filter.Limit {
			page.Items = page.Items[:filter.Limit]
			page.HasMore = filter.Offset+filter.Limit <= 100000
		}
		return err
	})
	return page, err
}

func normalizeOrderFilter(storeID string, filter ListFilter) (ListFilter, error) {
	if !validID(storeID) || (filter.SupplierID != "" && !validID(filter.SupplierID)) {
		return ListFilter{}, ErrInvalid
	}
	switch filter.Status {
	case "", "pending", "OPEN", "PARTIALLY_RECEIVED", "RECEIVED", "CANCELLED":
	default:
		return ListFilter{}, ErrInvalid
	}
	filter.Query = strings.TrimSpace(filter.Query)
	if !utf8.ValidString(filter.Query) || utf8.RuneCountInString(filter.Query) > 120 || strings.IndexFunc(filter.Query, unicode.IsControl) >= 0 {
		return ListFilter{}, ErrInvalid
	}
	for _, date := range []string{filter.FromDate, filter.ToDate} {
		if date != "" {
			day, err := time.Parse(time.DateOnly, date)
			if err != nil || day.Year() < 1 || day.Format(time.DateOnly) != date {
				return ListFilter{}, ErrInvalid
			}
		}
	}
	if filter.FromDate != "" && filter.ToDate != "" && filter.FromDate > filter.ToDate {
		return ListFilter{}, ErrInvalid
	}
	return filter, nil
}

func orderFilterArgs(tenantID, storeID, timezone string, filter ListFilter) []any {
	return []any{tenantID, storeID, filter.Status, filter.Query, filter.FromDate, filter.ToDate, timezone, filter.SupplierID}
}

func queryOrderList(ctx context.Context, tx pgx.Tx, args []any, limit *int, offset int) (pgx.Rows, error) {
	return tx.Query(ctx, selectOrders+orderListFilters+" ORDER BY o.ordered_at DESC,o.id DESC LIMIT $9 OFFSET $10", append(args, limit, offset)...)
}

func summarizeOrders(ctx context.Context, tx pgx.Tx, args []any) (OrderSummary, error) {
	var summary OrderSummary
	err := tx.QueryRow(ctx, `WITH matching AS (
		SELECT o.tenant_id,o.id,o.status`+orderScope+orderListFilters+`
	), units AS (
		SELECT coalesce(sum(i.ordered_qty),0) AS ordered,coalesce(sum(accepted.quantity),0)::bigint AS received,
			coalesce(sum(i.ordered_qty-coalesce(accepted.quantity,0)) FILTER (WHERE o.status<>'CANCELLED'),0)::bigint AS pending
		FROM matching o JOIN purchase_order_items i ON (i.tenant_id,i.purchase_order_id)=(o.tenant_id,o.id)
		CROSS JOIN LATERAL (
			SELECT sum(r.accepted_qty) AS quantity FROM purchase_receipt_items r
			WHERE (r.tenant_id,r.purchase_order_id,r.order_item_id)=(i.tenant_id,i.purchase_order_id,i.id)
		) accepted
	)
	SELECT (SELECT count(*) FROM matching),ordered,received,pending,
		(SELECT count(*) FROM matching WHERE status IN ('OPEN','PARTIALLY_RECEIVED')),
		(SELECT count(*) FROM matching WHERE status='RECEIVED') FROM units`,
		args...).
		Scan(&summary.OrderCount, &summary.OrderedUnits, &summary.ReceivedUnits, &summary.PendingUnits, &summary.PendingOrders, &summary.ReceivedOrders)
	return summary, err
}

func (s Service) Create(ctx context.Context, actor auth.Identity, key string, input OrderInput) (Order, error) {
	input.Reference = strings.TrimSpace(input.Reference)
	if !validID(input.StoreID) || !validID(input.SupplierID) || !keyPattern.MatchString(key) || len(input.Items) == 0 || len(input.Items) > 100 ||
		!validText(input.Reference, 60) {
		return Order{}, ErrInvalid
	}
	input.Items = slices.Clone(input.Items)
	slices.SortFunc(input.Items, func(a, b ItemInput) int { return cmp.Compare(a.VariantID, b.VariantID) })
	for i := range input.Items {
		line := &input.Items[i]
		if !validID(line.VariantID) || line.Quantity <= 0 || (i > 0 && input.Items[i-1].VariantID == line.VariantID) {
			return Order{}, ErrInvalid
		}
		if line.UnitCost != nil {
			cost, valid := money.Parse(*line.UnitCost)
			if !valid {
				return Order{}, ErrInvalid
			}
			normalized := money.Format(cost)
			line.UnitCost = &normalized
		}
	}
	body, err := json.Marshal(input)
	if err != nil {
		return Order{}, err
	}
	digest := sha256.Sum256(body)
	var result Order
	err = s.withStore(ctx, actor, input.StoreID, true, func(tx pgx.Tx, access orderAccess) error {
		var orderID, storeID, creator string
		var priorHash []byte
		err := tx.QueryRow(ctx, `SELECT id,store_id,created_by,request_hash FROM purchase_orders
			WHERE tenant_id=$1 AND idempotency_key=$2`, actor.TenantID, key).Scan(&orderID, &storeID, &creator, &priorHash)
		if err == nil {
			if storeID != input.StoreID || creator != actor.UserID || !bytes.Equal(priorHash, digest[:]) {
				return ErrKey
			}
			result, err = scanOrder(tx.QueryRow(ctx, selectOrders+" AND o.id=$3", actor.TenantID, storeID, orderID))
			return err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}

		// The tenant lock taken by withStore makes max(number)+1 safe.
		err = tx.QueryRow(ctx, `INSERT INTO purchase_orders(tenant_id,store_id,supplier_id,supplier_name,currency,created_by,idempotency_key,request_hash,number,reference)
			SELECT tenant_id,$2,id,name,$4,$5,$6,$7,
				(SELECT coalesce(max(number),0)+1 FROM purchase_orders WHERE tenant_id=$1),$8
			FROM suppliers WHERE tenant_id=$1 AND id=$3 AND status='active'
			RETURNING id`, actor.TenantID, input.StoreID, input.SupplierID, access.currency, actor.UserID, key, digest[:], input.Reference).Scan(&orderID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrSupplier
		}
		if err != nil {
			return err
		}
		for _, line := range input.Items {
			tag, err := tx.Exec(ctx, `INSERT INTO purchase_order_items(tenant_id,purchase_order_id,variant_id,ordered_qty,unit_cost,product_name,sku,size,color)
				SELECT v.tenant_id,$2,v.id,$4,$5::text::numeric,p.name,v.sku,v.size,v.color FROM product_variants v
				JOIN products p ON (p.tenant_id,p.id)=(v.tenant_id,v.product_id)
				WHERE v.tenant_id=$1 AND v.id=$3 AND v.status='active' AND p.status='active'`,
				actor.TenantID, orderID, line.VariantID, line.Quantity, line.UnitCost)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return ErrVariant
			}
		}
		result, err = scanOrder(tx.QueryRow(ctx, selectOrders+" AND o.id=$3", actor.TenantID, input.StoreID, orderID))
		return err
	})
	return result, err
}

// Get returns one order of a store, for the detail view and deep links.
func (s Service) Get(ctx context.Context, actor auth.Identity, storeID, orderID string) (Order, error) {
	if !validID(storeID) || !validID(orderID) {
		return Order{}, ErrInvalid
	}
	var result Order
	err := s.withStore(ctx, actor, storeID, false, func(tx pgx.Tx, _ orderAccess) error {
		var err error
		result, err = scanOrder(tx.QueryRow(ctx, selectOrders+" AND o.id=$3", actor.TenantID, storeID, orderID))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	})
	return result, err
}

// Cancel closes an order that is still waiting for goods. Units already
// received stay in stock and in the history; the remaining units stop being
// pending. Cancelling twice returns the cancelled order unchanged.
func (s Service) Cancel(ctx context.Context, actor auth.Identity, orderID string, input CancelInput) (Order, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if !validID(orderID) || !validID(input.StoreID) || !validText(input.Reason, 300) {
		return Order{}, ErrInvalid
	}
	var result Order
	err := s.withStore(ctx, actor, input.StoreID, true, func(tx pgx.Tx, _ orderAccess) error {
		var status string
		err := tx.QueryRow(ctx, "SELECT status FROM purchase_orders WHERE tenant_id=$1 AND store_id=$2 AND id=$3 FOR UPDATE",
			actor.TenantID, input.StoreID, orderID).Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		switch status {
		case "RECEIVED":
			return ErrClosed
		case "OPEN", "PARTIALLY_RECEIVED":
			if _, err := tx.Exec(ctx, `UPDATE purchase_orders SET status='CANCELLED',cancelled_at=clock_timestamp(),cancelled_by=$4,cancel_reason=$5
				WHERE tenant_id=$1 AND store_id=$2 AND id=$3`, actor.TenantID, input.StoreID, orderID, actor.UserID, input.Reason); err != nil {
				return err
			}
		}
		result, err = scanOrder(tx.QueryRow(ctx, selectOrders+" AND o.id=$3", actor.TenantID, input.StoreID, orderID))
		return err
	})
	return result, err
}

func validText(value string, limit int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= limit && strings.IndexFunc(value, unicode.IsControl) == -1
}

type orderAccess struct {
	manage             bool
	currency, timezone string
}

func (s Service) withStore(ctx context.Context, actor auth.Identity, storeID string, writing bool, operation func(pgx.Tx, orderAccess) error) error {
	options := pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}
	if writing {
		options = pgx.TxOptions{}
	}
	return pgx.BeginTxFunc(ctx, s.db, options, func(tx pgx.Tx) error {
		if writing {
			// The tenant lock also serializes supplier/catalog writes and order retries.
			if err := tenancy.LockActive(ctx, tx, actor.TenantID); err != nil {
				return err
			}
		}
		var access orderAccess
		var read bool
		err := tx.QueryRow(ctx, `SELECT COALESCE(bool_or(rp.permission_code='purchases:read'),false),
			COALESCE(bool_or(rp.permission_code='purchases:manage'),false),t.currency_code,t.timezone
			FROM user_stores us
			JOIN stores s ON (s.tenant_id,s.id)=(us.tenant_id,us.store_id)
			JOIN users u ON (u.tenant_id,u.id)=(us.tenant_id,us.user_id)
			JOIN tenants t ON t.id=us.tenant_id
			LEFT JOIN user_roles ur ON (ur.tenant_id,ur.user_id)=(us.tenant_id,us.user_id)
			LEFT JOIN role_permissions rp ON rp.role_code=ur.role_code
			WHERE us.tenant_id=$1 AND us.user_id=$2 AND us.store_id=$3 AND s.status='active' AND u.status='active' AND t.status='active'
			GROUP BY t.currency_code,t.timezone`, actor.TenantID, actor.UserID, storeID).Scan(&read, &access.manage, &access.currency, &access.timezone)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if !access.manage && (writing || !read) {
			return auth.ErrForbidden
		}
		return operation(tx, access)
	})
}

func scanOrder(row pgx.Row) (Order, error) {
	var result Order
	err := row.Scan(&result.ID, &result.Number, &result.StoreID, &result.SupplierID, &result.SupplierName, &result.Reference, &result.Currency,
		&result.Status, &result.OrderedAt, &result.CreatedBy, &result.CancelledAt, &result.CancelReason, &result.Items)
	result.OrderedAt = result.OrderedAt.UTC()
	if result.CancelledAt != nil {
		at := result.CancelledAt.UTC()
		result.CancelledAt = &at
	}
	return result, err
}

func validID(id string) bool {
	var uuid pgtype.UUID
	return len(id) == 36 && uuid.Scan(id) == nil && uuid.String() == id
}
