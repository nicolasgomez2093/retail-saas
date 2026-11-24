package sales

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"retail-platform/api/internal/modules/auth"

	"github.com/jackc/pgx/v5"
)

var ErrReportInvalid = errors.New("verificá sucursal, fechas, búsqueda y paginación del reporte")

type SalesMarginFilter struct {
	From, To         string
	FromDate, ToDate string
	Query            string
	Limit, Offset    int
}

type SalesMarginLine struct {
	SaleID           string    `json:"saleId"`
	SaleNumber       string    `json:"saleNumber"`
	SoldAt           time.Time `json:"soldAt"`
	Currency         string    `json:"currency"`
	VariantID        string    `json:"variantId"`
	SKU              string    `json:"sku"`
	Product          string    `json:"product"`
	Quantity         int32     `json:"quantity"`
	NetSales         string    `json:"netSales"`
	CostStatus       string    `json:"costStatus"`
	UnitCostSnapshot *string   `json:"unitCostSnapshot"`
	CostOfGoods      *string   `json:"costOfGoods"`
	GrossProfit      *string   `json:"grossProfit"`
	GrossMarginPct   *string   `json:"grossMarginPercent"`
}

type SalesMarginPage struct {
	Items          []SalesMarginLine    `json:"items"`
	HasMore        bool                 `json:"hasMore"`
	Timezone       string               `json:"timezone"`
	Summary        []SalesMarginSummary `json:"summary"`
	TopVariants    []TopSoldVariant     `json:"topVariants"`
	PaymentTotals  []SalesPaymentTotal  `json:"paymentTotals"`
	DailyTotals    []SalesDailyTotal    `json:"dailyTotals"`
	CategoryTotals []SalesCategoryTotal `json:"categoryTotals"`
	Activity       []SalesActivity      `json:"activity"`
	TopProducts    []TopSoldProduct     `json:"topProducts"`
}

type SalesCategoryTotal struct {
	Date         string  `json:"date"`
	Currency     string  `json:"currency"`
	CategoryID   *string `json:"categoryId"`
	CategoryName *string `json:"categoryName"`
	NetSales     string  `json:"netSales"`
}

type SalesDailyTotal struct {
	Date      string `json:"date"`
	Currency  string `json:"currency"`
	SaleCount int64  `json:"saleCount"`
	UnitsSold int64  `json:"unitsSold"`
	NetSales  string `json:"netSales"`
}

type SalesPaymentTotal struct {
	Currency  string `json:"currency"`
	Method    string `json:"method"`
	Amount    string `json:"amount"`
	SaleCount int64  `json:"saleCount"`
}

type TopSoldVariant struct {
	VariantID string `json:"variantId"`
	SKU       string `json:"sku"`
	Product   string `json:"product"`
	Size      string `json:"size"`
	Color     string `json:"color"`
	UnitsSold int64  `json:"unitsSold"`
}

type SalesMarginSummary struct {
	Currency            string  `json:"currency"`
	LineCount           int64   `json:"lineCount"`
	UnknownCostLines    int64   `json:"unknownCostLines"`
	NetSales            string  `json:"netSales"`
	NetSalesWithCost    string  `json:"netSalesWithCost"`
	NetSalesWithoutCost string  `json:"netSalesWithoutCost"`
	CostOfGoodsWithCost *string `json:"costOfGoodsWithCost"`
	GrossProfitWithCost *string `json:"grossProfitWithCost"`
	GrossMarginWithCost *string `json:"grossMarginPercentWithCost"`
}

const salesMarginScope = `
	FROM sales s JOIN cash_sessions cs ON (cs.tenant_id,cs.id)=(s.tenant_id,s.cash_session_id)
	JOIN sale_items si ON (si.tenant_id,si.sale_id)=(s.tenant_id,s.id)
	WHERE s.tenant_id=$1 AND s.store_id=$2 AND ($3 OR cs.opened_by=$4)
	AND ($5::timestamptz IS NULL OR s.created_at >= $5)
	AND ($6::timestamptz IS NULL OR s.created_at < $6)
	AND ($7::text='' OR (s.created_at AT TIME ZONE $9)::date >= NULLIF($7,'')::date)
	AND ($8::text='' OR (s.created_at AT TIME ZONE $9)::date <= NULLIF($8,'')::date)
	AND ($10::text='' OR strpos(lower(si.sku),lower($10))>0 OR strpos(lower(si.product_name),lower($10))>0)`

func (s Service) SalesMarginReport(ctx context.Context, actor auth.Identity, storeID string, filter SalesMarginFilter) (SalesMarginPage, error) {
	if filter.Limit < 1 || filter.Limit > 100 || filter.Offset < 0 || filter.Offset > 100000 {
		return SalesMarginPage{}, ErrReportInvalid
	}
	var result SalesMarginPage
	limit := filter.Limit + 1
	err := s.readSalesMargin(ctx, actor, storeID, filter, &limit, &result, func(timezone string, rows pgx.Rows) error {
		result.Timezone = timezone
		items, err := pgx.CollectRows(rows, scanSalesMarginLine)
		result.Items = items
		if len(items) > filter.Limit {
			result.Items = items[:filter.Limit]
			result.HasMore = filter.Offset+filter.Limit <= 100000
		}
		return err
	})
	return result, err
}

func (s Service) readSalesMargin(ctx context.Context, actor auth.Identity, storeID string, filter SalesMarginFilter, limit *int, page *SalesMarginPage, read func(string, pgx.Rows) error) error {
	if !validID(storeID) {
		return ErrReportInvalid
	}
	query := strings.TrimSpace(filter.Query)
	if !utf8.ValidString(query) || utf8.RuneCountInString(query) > 120 || strings.IndexFunc(query, unicode.IsControl) >= 0 {
		return ErrReportInvalid
	}
	useDates := filter.FromDate != "" || filter.ToDate != ""
	if useDates && (filter.From != "" || filter.To != "") {
		return ErrReportInvalid
	}
	for _, date := range []string{filter.FromDate, filter.ToDate} {
		if date != "" {
			day, err := time.Parse(time.DateOnly, date)
			if err != nil || day.Year() < 1 || day.Format(time.DateOnly) != date {
				return ErrReportInvalid
			}
		}
	}
	if filter.FromDate != "" && filter.ToDate != "" && filter.FromDate > filter.ToDate {
		return ErrReportInvalid
	}
	var from, to *time.Time
	if filter.From != "" {
		value, err := time.Parse(time.RFC3339Nano, filter.From)
		if err != nil {
			return ErrReportInvalid
		}
		from = &value
	}
	if filter.To != "" {
		value, err := time.Parse(time.RFC3339Nano, filter.To)
		if err != nil {
			return ErrReportInvalid
		}
		to = &value
	}
	if from != nil && to != nil && !from.Before(*to) {
		return ErrReportInvalid
	}

	return s.cash.ReadStoreSales(ctx, actor, storeID, func(tx pgx.Tx, supervise bool) error {
		var timezone string
		if err := tx.QueryRow(ctx, `SELECT t.timezone FROM stores st JOIN tenants t ON t.id=st.tenant_id
			WHERE st.tenant_id=$1 AND st.id=$2`, actor.TenantID, storeID).Scan(&timezone); err != nil {
			return err
		}
		args := []any{actor.TenantID, storeID, supervise, actor.UserID, from, to, filter.FromDate, filter.ToDate, timezone, query}
		if page != nil {
			var err error
			page.Summary, err = summarizeSalesMargin(ctx, tx, args)
			if err != nil {
				return err
			}
			page.TopVariants, err = topSoldVariants(ctx, tx, args)
			if err != nil {
				return err
			}
			page.PaymentTotals, err = summarizeSalesPayments(ctx, tx, args)
			if err != nil {
				return err
			}
			page.DailyTotals, err = summarizeSalesByDay(ctx, tx, args)
			if err != nil {
				return err
			}
			page.CategoryTotals, err = summarizeSalesCategories(ctx, tx, args)
			if err != nil {
				return err
			}
			page.Activity, err = summarizeSalesActivity(ctx, tx, args)
			if err != nil {
				return err
			}
			page.TopProducts, err = topSoldProducts(ctx, tx, args)
			if err != nil {
				return err
			}
		}
		rows, err := tx.Query(ctx, `SELECT s.id,s.sale_number::text,s.created_at,s.currency,si.variant_id,si.sku,si.product_name,
			si.quantity,(si.unit_price*si.quantity)::text,
			CASE WHEN si.unit_cost_snapshot IS NULL THEN 'unknown' ELSE 'known' END,
			si.unit_cost_snapshot::text,(si.unit_cost_snapshot*si.quantity)::text,
			(si.unit_price*si.quantity-si.unit_cost_snapshot*si.quantity)::text,
			CASE WHEN si.unit_cost_snapshot IS NOT NULL AND si.unit_price*si.quantity>0
				THEN round(((si.unit_price*si.quantity-si.unit_cost_snapshot*si.quantity)/(si.unit_price*si.quantity))*100,4)::text
				ELSE NULL END`+salesMarginScope+`
			ORDER BY s.created_at DESC,s.sale_number DESC,si.sku,si.variant_id LIMIT $11 OFFSET $12`, append(args, limit, filter.Offset)...)
		if err != nil {
			return err
		}
		defer rows.Close()
		return read(timezone, rows)
	})
}

// Category totals classify original sale lines using the current catalog.
func summarizeSalesCategories(ctx context.Context, tx pgx.Tx, args []any) ([]SalesCategoryTotal, error) {
	rows, err := tx.Query(ctx, `WITH lines AS (
		SELECT s.tenant_id,(s.created_at AT TIME ZONE $9)::date AS day,s.currency,si.variant_id,si.unit_price*si.quantity AS amount`+salesMarginScope+`
	)
	SELECT to_char(l.day,'YYYY-MM-DD'),l.currency,c.id::text,c.name,sum(l.amount)::text
	FROM lines l
	LEFT JOIN product_variants v ON (v.tenant_id,v.id)=(l.tenant_id,l.variant_id)
	LEFT JOIN products p ON (p.tenant_id,p.id)=(v.tenant_id,v.product_id)
	LEFT JOIN categories c ON (c.tenant_id,c.id)=(p.tenant_id,p.category_id)
	WHERE l.day IN (SELECT DISTINCT day FROM lines ORDER BY day DESC LIMIT 31)
	GROUP BY l.day,l.currency,c.id,c.name
	ORDER BY l.day DESC,l.currency,sum(l.amount) DESC,c.id`, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[SalesCategoryTotal])
}

func summarizeSalesByDay(ctx context.Context, tx pgx.Tx, args []any) ([]SalesDailyTotal, error) {
	rows, err := tx.Query(ctx, `WITH daily AS (
		SELECT (s.created_at AT TIME ZONE $9)::date AS day,s.currency,
			count(DISTINCT s.id) AS sale_count,sum(si.quantity) AS units_sold,sum(si.unit_price*si.quantity) AS net_sales`+salesMarginScope+`
		GROUP BY day,s.currency
	)
	SELECT to_char(day,'YYYY-MM-DD'),currency,sale_count,units_sold,net_sales::text
	FROM daily
	WHERE day IN (SELECT DISTINCT day FROM daily ORDER BY day DESC LIMIT 31)
	ORDER BY day DESC,currency`, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[SalesDailyTotal])
}

func summarizeSalesPayments(ctx context.Context, tx pgx.Tx, args []any) ([]SalesPaymentTotal, error) {
	rows, err := tx.Query(ctx, `WITH matching_sales AS (
		SELECT DISTINCT s.tenant_id,s.store_id,s.id,s.currency`+salesMarginScope+`
	)
	SELECT s.currency,payment.method,sum(payment.amount)::text,count(DISTINCT s.id)
	FROM matching_sales s `+saleDepositJoin+`
	LEFT JOIN receivables r ON (r.tenant_id,r.store_id,r.sale_id)=(s.tenant_id,s.store_id,s.id)
	CROSS JOIN LATERAL (
		SELECT sp.method,sp.amount FROM sale_payments sp WHERE (sp.tenant_id,sp.sale_id)=(s.tenant_id,s.id)
		UNION ALL SELECT d.method,d.amount WHERE d.amount IS NOT NULL
		UNION ALL SELECT 'on_credit',r.original_amount WHERE r.original_amount IS NOT NULL
	) payment
	WHERE payment.amount>0
	GROUP BY s.currency,payment.method ORDER BY s.currency,payment.method`, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[SalesPaymentTotal])
}

func topSoldVariants(ctx context.Context, tx pgx.Tx, args []any) ([]TopSoldVariant, error) {
	rows, err := tx.Query(ctx, `WITH ranked AS (
		SELECT si.variant_id,sum(si.quantity) AS units_sold`+salesMarginScope+`
		GROUP BY si.variant_id
		ORDER BY units_sold DESC,si.variant_id LIMIT 10
	)
	SELECT v.id,v.sku,p.name,v.size,v.color,ranked.units_sold
	FROM ranked JOIN product_variants v ON v.tenant_id=$1 AND v.id=ranked.variant_id
	JOIN products p ON p.tenant_id=v.tenant_id AND p.id=v.product_id
	ORDER BY ranked.units_sold DESC,ranked.variant_id`, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[TopSoldVariant])
}

func summarizeSalesMargin(ctx context.Context, tx pgx.Tx, args []any) ([]SalesMarginSummary, error) {
	rows, err := tx.Query(ctx, `WITH totals AS (
		SELECT s.currency,count(*) AS lines,count(*) FILTER (WHERE si.unit_cost_snapshot IS NULL) AS unknown_lines,
			sum(si.unit_price*si.quantity) AS net_sales,
			sum(si.unit_price*si.quantity) FILTER (WHERE si.unit_cost_snapshot IS NOT NULL) AS known_sales,
			sum(si.unit_price*si.quantity) FILTER (WHERE si.unit_cost_snapshot IS NULL) AS unknown_sales,
			sum(si.unit_cost_snapshot*si.quantity) AS cost`+salesMarginScope+`
		GROUP BY s.currency
	)
	SELECT currency,lines,unknown_lines,net_sales::text,coalesce(known_sales,0.00)::text,
		coalesce(unknown_sales,0.00)::text,cost::text,(known_sales-cost)::text,
		round((known_sales-cost)*100/nullif(known_sales,0),4)::text
	FROM totals ORDER BY currency`, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[SalesMarginSummary])
}

func scanSalesMarginLine(row pgx.CollectableRow) (SalesMarginLine, error) {
	var item SalesMarginLine
	err := row.Scan(&item.SaleID, &item.SaleNumber, &item.SoldAt, &item.Currency, &item.VariantID,
		&item.SKU, &item.Product, &item.Quantity, &item.NetSales, &item.CostStatus,
		&item.UnitCostSnapshot, &item.CostOfGoods, &item.GrossProfit, &item.GrossMarginPct)
	item.SoldAt = item.SoldAt.UTC()
	return item, err
}
