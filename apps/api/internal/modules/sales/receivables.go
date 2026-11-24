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

var ErrCustomerRequired = errors.New("elegí un cliente para confirmar la venta fiada")
var ErrCustomerNotFound = errors.New("no encontramos el cliente en esta sucursal")
var ErrCreditBalance = errors.New("la venta fiada debe dejar un saldo pendiente")
var ErrReceivableSearch = errors.New("ingresá un nombre de cliente o número de venta de hasta 120 caracteres")

type SaleCredit struct {
	ID             string `json:"id"`
	CustomerID     string `json:"customerId"`
	CustomerName   string `json:"customerName"`
	OriginalAmount string `json:"originalAmount"`
}

type Receivable struct {
	SaleCredit
	SaleID            string    `json:"saleId"`
	SaleNumber        string    `json:"saleNumber"`
	Currency          string    `json:"currency"`
	OutstandingAmount string    `json:"outstandingAmount"`
	CreatedAt         time.Time `json:"createdAt"`
}

type ReceivablePage struct {
	Items          []Receivable              `json:"items"`
	HasMore        bool                      `json:"hasMore"`
	Summary        []ReceivableSummary       `json:"summary"`
	RecentPayments []RecentReceivablePayment `json:"recentPayments"`
}

type RecentReceivablePayment struct {
	ID           string    `json:"id"`
	SaleID       string    `json:"saleId"`
	SaleNumber   string    `json:"saleNumber"`
	CustomerName string    `json:"customerName"`
	Currency     string    `json:"currency"`
	Method       string    `json:"method"`
	Amount       string    `json:"amount"`
	CreatedAt    time.Time `json:"createdAt"`
}

type ReceivableFilter struct {
	Status     string
	Query      string
	CustomerID string
}

type ReceivableSummary struct {
	Currency         string                      `json:"currency"`
	PendingCount     int64                       `json:"pendingCount"`
	SettledCount     int64                       `json:"settledCount"`
	Outstanding      string                      `json:"outstanding"`
	Collected        string                      `json:"collected"`
	DebtCredits      string                      `json:"debtCredits"`
	PaymentsByMethod []ReceivableMethodTotal     `json:"paymentsByMethod"`
	TopDebtors       []ReceivableCustomerBalance `json:"topDebtors"`
}

type ReceivableCustomerBalance struct {
	CustomerID   string `json:"customerId"`
	CustomerName string `json:"customerName"`
	PendingCount int64  `json:"pendingCount"`
	Outstanding  string `json:"outstanding"`
}

type ReceivableMethodTotal struct {
	Method       string `json:"method"`
	PaymentCount int64  `json:"paymentCount"`
	Amount       string `json:"amount"`
}

const receivableScope = `
	FROM receivables r
	JOIN sales s ON (s.tenant_id,s.store_id,s.id)=(r.tenant_id,r.store_id,r.sale_id)
	JOIN cash_sessions cs ON (cs.tenant_id,cs.id)=(s.tenant_id,s.cash_session_id)
	JOIN customers c ON (c.tenant_id,c.store_id,c.id)=(r.tenant_id,r.store_id,r.customer_id)
	WHERE r.tenant_id=$1 AND r.store_id=$2 AND ($3 OR cs.opened_by=$4)
	AND ($5='' OR s.sale_number::text=ltrim($5,'0') OR strpos(lower(c.name),lower($5))>0
		OR strpos(lower(r.customer_name_snapshot),lower($5))>0)
	AND (NULLIF($6,'') IS NULL OR r.customer_id=NULLIF($6,'')::uuid)`

func (s Service) Receivables(ctx context.Context, actor auth.Identity, storeID string, filter ReceivableFilter, limit, offset int) (ReceivablePage, error) {
	if limit < 1 || limit > 100 || offset < 0 || offset > 100000 {
		return ReceivablePage{}, ErrInvalid
	}
	var result ReceivablePage
	rowLimit := limit + 1
	err := s.readReceivables(ctx, actor, storeID, filter, &rowLimit, offset, &result, func(rows pgx.Rows) error {
		var err error
		result.Items, err = pgx.CollectRows(rows, scanReceivable)
		if len(result.Items) > limit {
			result.Items = result.Items[:limit]
			result.HasMore = offset+limit <= 100000
		}
		return err
	})
	return result, err
}

func (s Service) readReceivables(ctx context.Context, actor auth.Identity, storeID string, filter ReceivableFilter, limit *int, offset int, page *ReceivablePage, read func(pgx.Rows) error) error {
	if !validID(storeID) || (filter.Status != "pending" && filter.Status != "settled") ||
		(filter.CustomerID != "" && !validID(filter.CustomerID)) {
		return ErrInvalid
	}
	query := strings.TrimSpace(filter.Query)
	if !utf8.ValidString(query) || utf8.RuneCountInString(query) > 120 || strings.IndexFunc(query, unicode.IsControl) >= 0 {
		return ErrReceivableSearch
	}
	return s.cash.ReadStoreSales(ctx, actor, storeID, func(tx pgx.Tx, supervise bool) error {
		args := []any{actor.TenantID, storeID, supervise, actor.UserID, query, filter.CustomerID}
		if page != nil {
			var err error
			page.Summary, err = summarizeReceivables(ctx, tx, args)
			if err != nil {
				return err
			}
			page.RecentPayments, err = recentReceivablePayments(ctx, tx, args)
			if err != nil {
				return err
			}
		}
		rows, err := tx.Query(ctx, `SELECT r.id,r.customer_id,r.customer_name_snapshot,r.original_amount::text,
			s.id,s.sale_number::text,s.currency,r.outstanding_amount::text,s.created_at`+receivableScope+`
			AND (r.outstanding_amount>0)=$7
			ORDER BY s.sale_number DESC LIMIT $8 OFFSET $9`, append(args, filter.Status == "pending", limit, offset)...)
		if err != nil {
			return err
		}
		defer rows.Close()
		return read(rows)
	})
}

func scanReceivable(row pgx.CollectableRow) (Receivable, error) {
	var item Receivable
	err := row.Scan(&item.ID, &item.CustomerID, &item.CustomerName, &item.OriginalAmount,
		&item.SaleID, &item.SaleNumber, &item.Currency, &item.OutstandingAmount, &item.CreatedAt)
	item.CreatedAt = item.CreatedAt.UTC()
	return item, err
}

func recentReceivablePayments(ctx context.Context, tx pgx.Tx, args []any) ([]RecentReceivablePayment, error) {
	rows, err := tx.Query(ctx, `WITH scoped AS (
		SELECT r.tenant_id,r.store_id,r.id,r.sale_id,s.sale_number,c.name AS customer_name,s.currency`+receivableScope+`
	)
	SELECT p.id,r.sale_id,r.sale_number::text,r.customer_name,r.currency,p.method,p.amount::text,p.created_at
	FROM scoped r JOIN receivable_payments p
		ON (p.tenant_id,p.store_id,p.receivable_id)=(r.tenant_id,r.store_id,r.id)
	ORDER BY p.created_at DESC,p.id DESC LIMIT 10`, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (RecentReceivablePayment, error) {
		payment, err := pgx.RowToStructByPos[RecentReceivablePayment](row)
		payment.CreatedAt = payment.CreatedAt.UTC()
		return payment, err
	})
}

func summarizeReceivables(ctx context.Context, tx pgx.Tx, args []any) ([]ReceivableSummary, error) {
	rows, err := tx.Query(ctx, `WITH scoped AS (
		SELECT r.tenant_id,r.store_id,r.id,r.sale_id,r.outstanding_amount,s.currency,r.customer_id,c.name AS customer_name`+receivableScope+`
	), balances AS (
		SELECT r.currency,count(*) FILTER (WHERE r.outstanding_amount>0) AS pending_count,
			count(*) FILTER (WHERE r.outstanding_amount=0) AS settled_count,
			sum(r.outstanding_amount) AS outstanding,coalesce(sum(credited.amount),0.00) AS debt_credits
		FROM scoped r
		CROSS JOIN LATERAL (
			SELECT sum(c.amount) AS amount FROM sale_return_debt_credits c
			WHERE (c.tenant_id,c.store_id,c.sale_id)=(r.tenant_id,r.store_id,r.sale_id)
		) credited
		GROUP BY r.currency
	), methods AS (
		SELECT r.currency,p.method,count(*) AS payment_count,sum(p.amount) AS amount
		FROM scoped r JOIN receivable_payments p
			ON (p.tenant_id,p.store_id,p.receivable_id)=(r.tenant_id,r.store_id,r.id)
		GROUP BY r.currency,p.method
	), payments AS (
		SELECT currency,sum(amount) AS collected,
			jsonb_agg(jsonb_build_object('method',method,'paymentCount',payment_count,'amount',amount::text) ORDER BY method) AS breakdown
		FROM methods GROUP BY currency
	), customer_balances AS (
		SELECT currency,customer_id,customer_name,count(*) AS pending_count,sum(outstanding_amount) AS outstanding
		FROM scoped WHERE outstanding_amount>0
		GROUP BY currency,customer_id,customer_name
	), ranked_customers AS (
		SELECT *,row_number() OVER (PARTITION BY currency ORDER BY outstanding DESC,lower(customer_name),customer_id) AS position
		FROM customer_balances
	), debtors AS (
		SELECT currency,jsonb_agg(jsonb_build_object('customerId',customer_id,'customerName',customer_name,
			'pendingCount',pending_count,'outstanding',outstanding::text) ORDER BY position) AS breakdown
		FROM ranked_customers WHERE position<=10 GROUP BY currency
	)
	SELECT b.currency,b.pending_count,b.settled_count,b.outstanding::text,
		coalesce(p.collected,0.00)::text,b.debt_credits::text,coalesce(p.breakdown,'[]'::jsonb),coalesce(d.breakdown,'[]'::jsonb)
	FROM balances b LEFT JOIN payments p ON p.currency=b.currency
	LEFT JOIN debtors d ON d.currency=b.currency
	ORDER BY b.currency`, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[ReceivableSummary])
}
