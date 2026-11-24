package sales

import (
	"context"
	"time"

	"retail-platform/api/internal/modules/auth"

	"github.com/jackc/pgx/v5"
)

// CollectionsFilter selects the money that came in or went out in a period, optionally by method.
type CollectionsFilter struct {
	FromDate, ToDate string
	Method           string
	Limit, Offset    int
}

// CollectionsReport answers «how much did we collect and how»: each payment counts on the day it was
// received, with its real method. A fiado sale adds nothing until the customer pays it.
type CollectionsReport struct {
	Timezone string                  `json:"timezone"`
	Methods  []CollectionMethodTotal `json:"methods"`
	Sources  []CollectionSourceTotal `json:"sources"`
	Daily    []CollectionDailyTotal  `json:"daily"`
	Items    []CollectionEntry       `json:"items"`
	HasMore  bool                    `json:"hasMore"`
}

type CollectionMethodTotal struct {
	Currency  string `json:"currency"`
	Method    string `json:"method"`
	Collected string `json:"collected"`
	Refunded  string `json:"refunded"`
	Net       string `json:"net"`
	Count     int64  `json:"count"`
}

type CollectionSourceTotal struct {
	Currency string `json:"currency"`
	Source   string `json:"source"`
	Amount   string `json:"amount"`
	Count    int64  `json:"count"`
}

type CollectionDailyTotal struct {
	Date      string `json:"date"`
	Currency  string `json:"currency"`
	Collected string `json:"collected"`
	Refunded  string `json:"refunded"`
}

type CollectionEntry struct {
	Source     string    `json:"source"`
	Method     string    `json:"method"`
	Currency   string    `json:"currency"`
	Amount     string    `json:"amount"`
	HappenedAt time.Time `json:"happenedAt"`
	SaleID     *string   `json:"saleId"`
	SaleNumber *string   `json:"saleNumber"`
	Customer   *string   `json:"customer"`
}

// collectionLines: $1 tenant, $2 store, $3 supervise, $4 user, $5 from date, $6 to date, $7 timezone, $8 method.
// Sale payments follow the sale's cash session; every other movement follows its own session.
const collectionLines = `WITH money AS (
		SELECT 'sale' AS source,sp.method,sp.amount,s.created_at AS happened_at,s.currency,s.id AS sale_id,s.sale_number,
			NULL::text AS customer,s.cash_session_id AS session_id
		FROM sales s JOIN sale_payments sp ON (sp.tenant_id,sp.sale_id)=(s.tenant_id,s.id)
		WHERE s.tenant_id=$1 AND s.store_id=$2 AND sp.amount>0
		UNION ALL
		SELECT 'deposit',d.method,d.amount,d.created_at,t.currency_code,s.id,s.sale_number,b.customer_name,d.cash_session_id
		FROM reservation_deposits d JOIN reservations b ON (b.tenant_id,b.store_id,b.id)=(d.tenant_id,d.store_id,d.reservation_id)
		JOIN tenants t ON t.id=d.tenant_id
		LEFT JOIN sales s ON (s.tenant_id,s.store_id,s.id)=(b.tenant_id,b.store_id,b.sale_id)
		WHERE d.tenant_id=$1 AND d.store_id=$2
		UNION ALL
		SELECT 'receivable',p.method,p.amount,p.created_at,s.currency,s.id,s.sale_number,r.customer_name_snapshot,p.cash_session_id
		FROM receivable_payments p JOIN receivables r ON (r.tenant_id,r.store_id,r.id)=(p.tenant_id,p.store_id,p.receivable_id)
		JOIN sales s ON (s.tenant_id,s.store_id,s.id)=(r.tenant_id,r.store_id,r.sale_id)
		WHERE p.tenant_id=$1 AND p.store_id=$2
		UNION ALL
		SELECT 'exchange',cp.method,cp.amount,e.created_at,s.currency,s.id,s.sale_number,NULL,ch.cash_session_id
		FROM sale_return_exchange_charge_payments cp
		JOIN sale_return_exchange_charges ch ON (ch.tenant_id,ch.exchange_id)=(cp.tenant_id,cp.exchange_id)
		JOIN sale_return_exchanges e ON (e.tenant_id,e.id)=(ch.tenant_id,ch.exchange_id)
		JOIN sales s ON (s.tenant_id,s.store_id,s.id)=(e.tenant_id,e.store_id,e.sale_id)
		WHERE e.tenant_id=$1 AND e.store_id=$2
		UNION ALL
		SELECT 'refund',a.method,-a.amount,f.created_at,s.currency,s.id,s.sale_number,NULL,f.cash_session_id
		FROM sale_return_refund_allocations a JOIN sale_return_refunds f ON (f.tenant_id,f.id)=(a.tenant_id,a.refund_id)
		JOIN sales s ON (s.tenant_id,s.store_id,s.id)=(f.tenant_id,f.store_id,f.sale_id)
		WHERE f.tenant_id=$1 AND f.store_id=$2
	), scoped AS (
		SELECT m.* FROM money m JOIN cash_sessions cs ON (cs.tenant_id,cs.id)=($1,m.session_id)
		WHERE ($3 OR cs.opened_by=$4)
		AND ($5::text='' OR (m.happened_at AT TIME ZONE $7)::date >= NULLIF($5,'')::date)
		AND ($6::text='' OR (m.happened_at AT TIME ZONE $7)::date <= NULLIF($6,'')::date)
		AND ($8::text='' OR m.method=$8)
	)`

func (s Service) Collections(ctx context.Context, actor auth.Identity, storeID string, filter CollectionsFilter) (CollectionsReport, error) {
	if !validID(storeID) || filter.Limit < 1 || filter.Limit > 100 || filter.Offset < 0 || filter.Offset > 100000 ||
		filter.Method == "on_credit" || !validSaleMethod(filter.Method) || !validDateRange(filter.FromDate, filter.ToDate) {
		return CollectionsReport{}, ErrReportInvalid
	}
	var result CollectionsReport
	err := s.cash.ReadStoreSales(ctx, actor, storeID, func(tx pgx.Tx, supervise bool) error {
		if err := tx.QueryRow(ctx, `SELECT t.timezone FROM tenants t WHERE t.id=$1`, actor.TenantID).Scan(&result.Timezone); err != nil {
			return err
		}
		args := []any{actor.TenantID, storeID, supervise, actor.UserID, filter.FromDate, filter.ToDate, result.Timezone, filter.Method}
		rows, err := tx.Query(ctx, collectionLines+`
			SELECT currency,method,COALESCE(sum(amount) FILTER (WHERE amount>0),0.00)::text,
				COALESCE(-sum(amount) FILTER (WHERE amount<0),0.00)::text,sum(amount)::text,count(*)
			FROM scoped GROUP BY currency,method ORDER BY currency,sum(amount) DESC,method`, args...)
		if err != nil {
			return err
		}
		if result.Methods, err = pgx.CollectRows(rows, pgx.RowToStructByPos[CollectionMethodTotal]); err != nil {
			return err
		}
		rows, err = tx.Query(ctx, collectionLines+`
			SELECT currency,source,sum(amount)::text,count(*)
			FROM scoped GROUP BY currency,source ORDER BY currency,source`, args...)
		if err != nil {
			return err
		}
		if result.Sources, err = pgx.CollectRows(rows, pgx.RowToStructByPos[CollectionSourceTotal]); err != nil {
			return err
		}
		rows, err = tx.Query(ctx, collectionLines+`, daily AS (
				SELECT (happened_at AT TIME ZONE $7)::date AS day,currency,
					COALESCE(sum(amount) FILTER (WHERE amount>0),0.00) AS collected,COALESCE(-sum(amount) FILTER (WHERE amount<0),0.00) AS refunded
				FROM scoped GROUP BY day,currency
			)
			SELECT to_char(day,'YYYY-MM-DD'),currency,collected::text,refunded::text FROM daily
			WHERE day IN (SELECT DISTINCT day FROM daily ORDER BY day DESC LIMIT 31)
			ORDER BY day DESC,currency`, args...)
		if err != nil {
			return err
		}
		if result.Daily, err = pgx.CollectRows(rows, pgx.RowToStructByPos[CollectionDailyTotal]); err != nil {
			return err
		}
		rows, err = tx.Query(ctx, collectionLines+`
			SELECT source,method,currency,amount::text,happened_at,sale_id::text,sale_number::text,customer
			FROM scoped ORDER BY happened_at DESC,sale_number DESC NULLS LAST,source,method LIMIT $9 OFFSET $10`,
			append(args, filter.Limit+1, filter.Offset)...)
		if err != nil {
			return err
		}
		result.Items, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (CollectionEntry, error) {
			entry, err := pgx.RowToStructByPos[CollectionEntry](row)
			entry.HappenedAt = entry.HappenedAt.UTC()
			return entry, err
		})
		if len(result.Items) > filter.Limit {
			result.Items = result.Items[:filter.Limit]
			result.HasMore = filter.Offset+filter.Limit <= 100000
		}
		return err
	})
	return result, err
}

func validDateRange(from, to string) bool {
	for _, date := range []string{from, to} {
		if date != "" {
			day, err := time.Parse(time.DateOnly, date)
			if err != nil || day.Year() < 1 || day.Format(time.DateOnly) != date {
				return false
			}
		}
	}
	return from == "" || to == "" || from <= to
}
