package sales

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// SalesActivity nets sales with the returns and exchange replacements registered in the period.
// Returns are valued at the price actually charged (sale line or exchange line) and dated when they happen.
type SalesActivity struct {
	Currency       string  `json:"currency"`
	SaleCount      int64   `json:"saleCount"`
	UnitsSold      int64   `json:"unitsSold"`
	GrossSales     string  `json:"grossSales"`
	AverageTicket  *string `json:"averageTicket"`
	ReturnCount    int64   `json:"returnCount"`
	ReturnedUnits  int64   `json:"returnedUnits"`
	ReturnedAmount string  `json:"returnedAmount"`
	ExchangeCount  int64   `json:"exchangeCount"`
	ExchangedUnits int64   `json:"exchangedUnits"`
	ExchangeAmount string  `json:"exchangeAmount"`
	NetUnits       int64   `json:"netUnits"`
	NetSales       string  `json:"netSales"`
}

type TopSoldProduct struct {
	Currency      string `json:"currency"`
	ProductID     string `json:"productId"`
	Product       string `json:"product"`
	UnitsSold     int64  `json:"unitsSold"`
	ReturnedUnits int64  `json:"returnedUnits"`
	NetUnits      int64  `json:"netUnits"`
	NetSales      string `json:"netSales"`
}

// commercialLines uses the same arguments as salesMarginScope. Each operation is filtered by its own date;
// the method filter applies to the original sale, so its returns and exchanges follow it.
var commercialLines = `WITH lines AS (
		SELECT 'sale' AS kind,s.id AS sale_id,s.id AS operation_id,s.currency,si.variant_id,si.product_name,si.sku,
			si.quantity AS units,si.unit_price*si.quantity AS amount,s.created_at AS happened_at
		FROM sales s JOIN cash_sessions cs ON (cs.tenant_id,cs.id)=(s.tenant_id,s.cash_session_id)
		JOIN sale_items si ON (si.tenant_id,si.sale_id)=(s.tenant_id,s.id)
		WHERE s.tenant_id=$1 AND s.store_id=$2 AND ($3 OR cs.opened_by=$4)
		UNION ALL
		SELECT 'return',s.id,r.id,s.currency,ri.variant_id,COALESCE(ei.product_name,si.product_name),COALESCE(ei.sku,si.sku),
			-ri.quantity,-ri.quantity*COALESCE(ei.unit_price,si.unit_price),r.created_at
		FROM sale_returns r JOIN sales s ON (s.tenant_id,s.store_id,s.id)=(r.tenant_id,r.store_id,r.sale_id)
		JOIN cash_sessions cs ON (cs.tenant_id,cs.id)=(s.tenant_id,s.cash_session_id)
		JOIN sale_return_items ri ON (ri.tenant_id,ri.return_id)=(r.tenant_id,r.id)
		LEFT JOIN sale_items si ON (si.tenant_id,si.sale_id,si.variant_id)=(ri.tenant_id,ri.sale_id,ri.original_variant_id)
		LEFT JOIN sale_return_exchange_items ei ON (ei.tenant_id,ei.exchange_id,ei.variant_id)=(ri.tenant_id,ri.source_exchange_id,ri.variant_id)
		WHERE r.tenant_id=$1 AND r.store_id=$2 AND ($3 OR cs.opened_by=$4)
		UNION ALL
		SELECT 'exchange',s.id,e.id,s.currency,ei.variant_id,ei.product_name,ei.sku,
			ei.quantity,ei.quantity*ei.unit_price,e.created_at
		FROM sale_return_exchanges e JOIN sales s ON (s.tenant_id,s.store_id,s.id)=(e.tenant_id,e.store_id,e.sale_id)
		JOIN cash_sessions cs ON (cs.tenant_id,cs.id)=(s.tenant_id,s.cash_session_id)
		JOIN sale_return_exchange_items ei ON (ei.tenant_id,ei.exchange_id)=(e.tenant_id,e.id)
		WHERE e.tenant_id=$1 AND e.store_id=$2 AND ($3 OR cs.opened_by=$4)
	), scoped AS (
		SELECT * FROM lines
		WHERE ($5::timestamptz IS NULL OR happened_at >= $5)
		AND ($6::timestamptz IS NULL OR happened_at < $6)
		AND ($7::text='' OR (happened_at AT TIME ZONE $9)::date >= NULLIF($7,'')::date)
		AND ($8::text='' OR (happened_at AT TIME ZONE $9)::date <= NULLIF($8,'')::date)
		AND ($10::text='' OR strpos(lower(sku),lower($10))>0 OR strpos(lower(product_name),lower($10))>0)
		AND ($11::text='' OR sale_id IN (SELECT s.id FROM sales s WHERE s.tenant_id=$1 AND s.store_id=$2 AND ` + salePaidWith("$11") + `))
	)`

func summarizeSalesActivity(ctx context.Context, tx pgx.Tx, args []any) ([]SalesActivity, error) {
	rows, err := tx.Query(ctx, commercialLines+`
	SELECT currency,
		count(DISTINCT operation_id) FILTER (WHERE kind='sale'),
		COALESCE(sum(units) FILTER (WHERE kind='sale'),0),
		COALESCE(sum(amount) FILTER (WHERE kind='sale'),0.00)::text,
		round(sum(amount) FILTER (WHERE kind='sale')/nullif(count(DISTINCT operation_id) FILTER (WHERE kind='sale'),0),2)::text,
		count(DISTINCT operation_id) FILTER (WHERE kind='return'),
		COALESCE(-sum(units) FILTER (WHERE kind='return'),0),
		COALESCE(-sum(amount) FILTER (WHERE kind='return'),0.00)::text,
		count(DISTINCT operation_id) FILTER (WHERE kind='exchange'),
		COALESCE(sum(units) FILTER (WHERE kind='exchange'),0),
		COALESCE(sum(amount) FILTER (WHERE kind='exchange'),0.00)::text,
		sum(units),sum(amount)::text
	FROM scoped GROUP BY currency ORDER BY currency`, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[SalesActivity])
}

// topSoldProducts ranks products by units kept by customers: sold and replacements minus returns.
func topSoldProducts(ctx context.Context, tx pgx.Tx, args []any) ([]TopSoldProduct, error) {
	rows, err := tx.Query(ctx, commercialLines+`
	SELECT l.currency,p.id::text,p.name,
		COALESCE(sum(l.units) FILTER (WHERE l.kind<>'return'),0),
		COALESCE(-sum(l.units) FILTER (WHERE l.kind='return'),0),
		sum(l.units),sum(l.amount)::text
	FROM scoped l JOIN product_variants v ON (v.tenant_id,v.id)=($1,l.variant_id)
	JOIN products p ON (p.tenant_id,p.id)=(v.tenant_id,v.product_id)
	GROUP BY l.currency,p.id,p.name
	HAVING sum(l.units)>0
	ORDER BY sum(l.units) DESC,sum(l.amount) DESC,p.id
	LIMIT 10`, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[TopSoldProduct])
}
