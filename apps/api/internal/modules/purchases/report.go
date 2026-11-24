package purchases

import (
	"context"
	"time"

	"retail-platform/api/internal/modules/auth"

	"github.com/jackc/pgx/v5"
)

// ReportFilter selects the goods received in a period; orders count by the day they were placed.
type ReportFilter struct {
	FromDate, ToDate string
	Limit, Offset    int
}

// Report summarizes purchases by receipt date, valued at the unit cost captured on each receipt.
type Report struct {
	Timezone  string           `json:"timezone"`
	Currency  string           `json:"currency"`
	Summary   ReportSummary    `json:"summary"`
	Daily     []ReportDay      `json:"daily"`
	Suppliers []ReportSupplier `json:"suppliers"`
	Products  []ReportProduct  `json:"products"`
	Items     []ReportReceipt  `json:"items"`
	HasMore   bool             `json:"hasMore"`
}

type ReportSummary struct {
	ReceiptCount   int64  `json:"receiptCount"`
	ReceivedOrders int64  `json:"receivedOrders"`
	Units          int64  `json:"units"`
	UnitsWithCost  int64  `json:"unitsWithCost"`
	RejectedUnits  int64  `json:"rejectedUnits"`
	KnownCost      string `json:"knownCost"`
	PlacedOrders   int64  `json:"placedOrders"`
	OrderedUnits   int64  `json:"orderedUnits"`
	// PendingOrders is the current backlog, independent of the period.
	PendingOrders int64 `json:"pendingOrders"`
}

type ReportDay struct {
	Date         string `json:"date"`
	ReceiptCount int64  `json:"receiptCount"`
	Units        int64  `json:"units"`
	KnownCost    string `json:"knownCost"`
}

type ReportSupplier struct {
	SupplierID   string `json:"supplierId"`
	Supplier     string `json:"supplier"`
	ReceiptCount int64  `json:"receiptCount"`
	Units        int64  `json:"units"`
	KnownCost    string `json:"knownCost"`
}

type ReportProduct struct {
	ProductID string `json:"productId"`
	Product   string `json:"product"`
	Units     int64  `json:"units"`
	KnownCost string `json:"knownCost"`
}

type ReportReceipt struct {
	ReceiptID     string    `json:"receiptId"`
	OrderID       string    `json:"orderId"`
	OrderNumber   *int32    `json:"orderNumber"`
	Supplier      string    `json:"supplier"`
	LocationName  string    `json:"locationName"`
	ReceivedAt    time.Time `json:"receivedAt"`
	Units         int64     `json:"units"`
	RejectedUnits int64     `json:"rejectedUnits"`
	KnownCost     string    `json:"knownCost"`
	UnitsNoCost   int64     `json:"unitsWithoutCost"`
}

// receivedLines: $1 tenant, $2 store, $3 from date, $4 to date, $5 timezone.
const receivedLines = `WITH lines AS (
		SELECT r.id AS receipt_id,o.id AS order_id,o.number,o.supplier_id,o.supplier_name,r.location_name,r.received_at,
			(r.received_at AT TIME ZONE $5)::date AS day,v.product_id,oi.product_name,
			i.accepted_qty,i.rejected_qty,i.unit_cost,i.accepted_qty*i.unit_cost AS cost
		FROM purchase_receipts r
		JOIN purchase_orders o ON (o.tenant_id,o.store_id,o.id)=(r.tenant_id,r.store_id,r.purchase_order_id)
		JOIN purchase_receipt_items i ON (i.tenant_id,i.purchase_order_id,i.receipt_id)=(r.tenant_id,r.purchase_order_id,r.id)
		JOIN purchase_order_items oi ON (oi.tenant_id,oi.purchase_order_id,oi.id)=(i.tenant_id,i.purchase_order_id,i.order_item_id)
		JOIN product_variants v ON (v.tenant_id,v.id)=(oi.tenant_id,oi.variant_id)
		WHERE r.tenant_id=$1 AND r.store_id=$2
		AND ($3::text='' OR (r.received_at AT TIME ZONE $5)::date >= NULLIF($3,'')::date)
		AND ($4::text='' OR (r.received_at AT TIME ZONE $5)::date <= NULLIF($4,'')::date)
	)`

func (s Service) Report(ctx context.Context, actor auth.Identity, storeID string, filter ReportFilter) (Report, error) {
	if filter.Limit < 1 || filter.Limit > 50 || filter.Offset < 0 || filter.Offset > 100000 {
		return Report{}, ErrInvalid
	}
	if _, err := normalizeOrderFilter(storeID, ListFilter{FromDate: filter.FromDate, ToDate: filter.ToDate}); err != nil {
		return Report{}, err
	}
	var report Report
	err := s.withStore(ctx, actor, storeID, false, func(tx pgx.Tx, access orderAccess) error {
		report.Timezone, report.Currency = access.timezone, access.currency
		args := []any{actor.TenantID, storeID, filter.FromDate, filter.ToDate, access.timezone}
		err := tx.QueryRow(ctx, receivedLines+`, placed AS (
				SELECT count(DISTINCT o.id) AS orders,coalesce(sum(oi.ordered_qty),0) AS units
				FROM purchase_orders o JOIN purchase_order_items oi ON (oi.tenant_id,oi.purchase_order_id)=(o.tenant_id,o.id)
				WHERE o.tenant_id=$1 AND o.store_id=$2
				AND ($3::text='' OR (o.ordered_at AT TIME ZONE $5)::date >= NULLIF($3,'')::date)
				AND ($4::text='' OR (o.ordered_at AT TIME ZONE $5)::date <= NULLIF($4,'')::date)
			)
			SELECT count(DISTINCT receipt_id),count(DISTINCT order_id),coalesce(sum(accepted_qty),0),
				coalesce(sum(accepted_qty) FILTER (WHERE unit_cost IS NOT NULL),0),coalesce(sum(rejected_qty),0),
				coalesce(sum(cost),0.00)::text,(SELECT orders FROM placed),(SELECT units FROM placed),
				(SELECT count(*) FROM purchase_orders o WHERE o.tenant_id=$1 AND o.store_id=$2 AND o.status IN ('OPEN','PARTIALLY_RECEIVED'))
			FROM lines`, args...).Scan(&report.Summary.ReceiptCount, &report.Summary.ReceivedOrders, &report.Summary.Units,
			&report.Summary.UnitsWithCost, &report.Summary.RejectedUnits, &report.Summary.KnownCost, &report.Summary.PlacedOrders,
			&report.Summary.OrderedUnits, &report.Summary.PendingOrders)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, receivedLines+`
			SELECT to_char(day,'YYYY-MM-DD'),count(DISTINCT receipt_id),sum(accepted_qty),coalesce(sum(cost),0.00)::text
			FROM lines WHERE day IN (SELECT DISTINCT day FROM lines ORDER BY day DESC LIMIT 31)
			GROUP BY day ORDER BY day DESC`, args...)
		if err != nil {
			return err
		}
		if report.Daily, err = pgx.CollectRows(rows, pgx.RowToStructByPos[ReportDay]); err != nil {
			return err
		}
		rows, err = tx.Query(ctx, receivedLines+`
			SELECT supplier_id::text,supplier_name,count(DISTINCT receipt_id),sum(accepted_qty),coalesce(sum(cost),0.00)::text
			FROM lines GROUP BY supplier_id,supplier_name
			ORDER BY coalesce(sum(cost),0) DESC,sum(accepted_qty) DESC,supplier_id LIMIT 10`, args...)
		if err != nil {
			return err
		}
		if report.Suppliers, err = pgx.CollectRows(rows, pgx.RowToStructByPos[ReportSupplier]); err != nil {
			return err
		}
		rows, err = tx.Query(ctx, receivedLines+`
			SELECT l.product_id::text,p.name,sum(l.accepted_qty),coalesce(sum(l.cost),0.00)::text
			FROM lines l JOIN products p ON (p.tenant_id,p.id)=($1,l.product_id)
			GROUP BY l.product_id,p.name HAVING sum(l.accepted_qty)>0
			ORDER BY sum(l.accepted_qty) DESC,coalesce(sum(l.cost),0) DESC,l.product_id LIMIT 10`, args...)
		if err != nil {
			return err
		}
		if report.Products, err = pgx.CollectRows(rows, pgx.RowToStructByPos[ReportProduct]); err != nil {
			return err
		}
		rows, err = tx.Query(ctx, receivedLines+`
			SELECT receipt_id::text,order_id::text,number,supplier_name,location_name,received_at,sum(accepted_qty),sum(rejected_qty),
				coalesce(sum(cost),0.00)::text,coalesce(sum(accepted_qty) FILTER (WHERE unit_cost IS NULL),0)
			FROM lines GROUP BY receipt_id,order_id,number,supplier_name,location_name,received_at
			ORDER BY received_at DESC,receipt_id DESC LIMIT $6 OFFSET $7`, append(args, filter.Limit+1, filter.Offset)...)
		if err != nil {
			return err
		}
		report.Items, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (ReportReceipt, error) {
			item, err := pgx.RowToStructByPos[ReportReceipt](row)
			item.ReceivedAt = item.ReceivedAt.UTC()
			return item, err
		})
		if len(report.Items) > filter.Limit {
			report.Items = report.Items[:filter.Limit]
			report.HasMore = filter.Offset+filter.Limit <= 100000
		}
		return err
	})
	return report, err
}
