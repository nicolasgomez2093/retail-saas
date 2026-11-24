package customers

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/cash"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrInvalid = errors.New("verificá el nombre, el teléfono y la búsqueda de clientes")

var ErrNotFound = errors.New("cliente inexistente en la sucursal")

type Customer struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Phone string `json:"phone"`
}

type Input struct {
	StoreID string `json:"storeId"`
	Name    string `json:"name"`
	Phone   string `json:"phone"`
}

type Balance struct {
	Currency string `json:"currency"`
	Amount   string `json:"amount"`
}

type Summary struct {
	CustomerID   string     `json:"customerId"`
	LastPurchase *time.Time `json:"lastPurchase"`
	Balances     []Balance  `json:"balances"`
}

type Page struct {
	Items     []Customer `json:"items"`
	HasMore   bool       `json:"hasMore"`
	Summaries []Summary  `json:"summaries,omitempty"`
	Timezone  string     `json:"timezone,omitempty"`
}

type Service struct{ cash cash.Service }

func New(db *pgxpool.Pool) Service { return Service{cash: cash.New(db)} }

func (s Service) Create(ctx context.Context, actor auth.Identity, input Input) (Customer, error) {
	name, phone := strings.TrimSpace(input.Name), strings.TrimSpace(input.Phone)
	if name == "" || !validText(name, 120) || !validText(phone, 40) {
		return Customer{}, ErrInvalid
	}
	var customer Customer
	err := s.cash.WriteStoreSales(ctx, actor, input.StoreID, func(tx pgx.Tx, _ bool) error {
		return tx.QueryRow(ctx, `INSERT INTO customers(tenant_id,store_id,name,phone,created_by)
			VALUES ($1,$2,$3,$4,$5) RETURNING id,name,phone`,
			actor.TenantID, input.StoreID, name, phone, actor.UserID).Scan(&customer.ID, &customer.Name, &customer.Phone)
	})
	return customer, err
}

func (s Service) Update(ctx context.Context, actor auth.Identity, id string, input Input) (Customer, error) {
	name, phone := strings.TrimSpace(input.Name), strings.TrimSpace(input.Phone)
	var customerID pgtype.UUID
	if customerID.Scan(id) != nil || !customerID.Valid || name == "" || !validText(name, 120) || !validText(phone, 40) {
		return Customer{}, ErrInvalid
	}
	var customer Customer
	err := s.cash.WriteStoreSales(ctx, actor, input.StoreID, func(tx pgx.Tx, _ bool) error {
		err := tx.QueryRow(ctx, `UPDATE customers SET name=$4, phone=$5
			WHERE tenant_id=$1 AND store_id=$2 AND id=$3 RETURNING id,name,phone`,
			actor.TenantID, input.StoreID, customerID, name, phone).Scan(&customer.ID, &customer.Name, &customer.Phone)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	})
	return customer, err
}

func (s Service) List(ctx context.Context, actor auth.Identity, storeID, query string, limit, offset int, includeSummary bool) (Page, error) {
	query = strings.TrimSpace(query)
	if !validText(query, 120) || limit < 1 || limit > 100 || offset < 0 || offset > 100000 {
		return Page{}, ErrInvalid
	}
	var page Page
	err := s.cash.ReadStoreSales(ctx, actor, storeID, func(tx pgx.Tx, supervise bool) error {
		rows, err := tx.Query(ctx, `SELECT id,name,phone FROM customers
			WHERE tenant_id=$1 AND store_id=$2 AND (strpos(lower(name),lower($3)) > 0 OR strpos(phone,$3) > 0)
			ORDER BY lower(name),id LIMIT $4 OFFSET $5`, actor.TenantID, storeID, query, limit+1, offset)
		if err != nil {
			return err
		}
		page.Items, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Customer])
		if len(page.Items) > limit {
			page.Items = page.Items[:limit]
			page.HasMore = offset+limit <= 100000
		}
		if err == nil && includeSummary && len(page.Items) > 0 {
			page.Summaries, err = customerSummaries(ctx, tx, actor, storeID, supervise, page.Items)
			if err == nil {
				err = tx.QueryRow(ctx, "SELECT timezone FROM tenants WHERE id=$1", actor.TenantID).Scan(&page.Timezone)
			}
		}
		return err
	})
	return page, err
}

func customerSummaries(ctx context.Context, tx pgx.Tx, actor auth.Identity, storeID string, supervise bool, items []Customer) ([]Summary, error) {
	ids := make([]pgtype.UUID, 0, len(items))
	for _, item := range items {
		var id pgtype.UUID
		if err := id.Scan(item.ID); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	rows, err := tx.Query(ctx, `WITH linked AS (
		SELECT customer_id,sale_id,outstanding_amount AS balance FROM receivables
		WHERE tenant_id=$1 AND store_id=$2 AND customer_id=ANY($5)
		UNION ALL
		SELECT customer_id,sale_id,NULL FROM reservations
		WHERE tenant_id=$1 AND store_id=$2 AND customer_id=ANY($5) AND sale_id IS NOT NULL
	), visible AS (
		SELECT linked.customer_id,s.created_at,s.currency,linked.balance
		FROM linked JOIN sales s ON s.tenant_id=$1 AND s.store_id=$2 AND s.id=linked.sale_id
		JOIN cash_sessions cs ON (cs.tenant_id,cs.id)=(s.tenant_id,s.cash_session_id)
		WHERE $3 OR cs.opened_by=$4
	), last_purchase AS (
		SELECT customer_id,max(created_at) AS bought_at FROM visible GROUP BY customer_id
	), totals AS (
		SELECT customer_id,currency,sum(balance)::text AS amount
		FROM visible WHERE balance>0 GROUP BY customer_id,currency
	), balances AS (
		SELECT customer_id,jsonb_agg(jsonb_build_object('currency',currency,'amount',amount) ORDER BY currency) AS amounts
		FROM totals GROUP BY customer_id
	)
	SELECT c.id::text,p.bought_at,coalesce(b.amounts,'[]'::jsonb)
	FROM customers c
	LEFT JOIN last_purchase p ON p.customer_id=c.id
	LEFT JOIN balances b ON b.customer_id=c.id
	WHERE c.tenant_id=$1 AND c.store_id=$2 AND c.id=ANY($5)`, actor.TenantID, storeID, supervise, actor.UserID, ids)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Summary])
}

func validText(value string, limit int) bool {
	return utf8.RuneCountInString(value) <= limit && strings.IndexFunc(value, unicode.IsControl) == -1
}
