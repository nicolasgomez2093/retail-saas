package sales

import (
	"context"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"retail-platform/api/internal/modules/auth"

	"github.com/jackc/pgx/v5"
)

type SaleSummary struct {
	ID        string    `json:"id"`
	Number    string    `json:"number"`
	Total     string    `json:"total"`
	Currency  string    `json:"currency"`
	CreatedAt time.Time `json:"createdAt"`
	// Customer is known for fiado sales and fulfilled reservations only.
	Customer *string `json:"customer"`
	Units    int64   `json:"units"`
	Products string  `json:"products"`
	Returns  int64   `json:"returns"`
	OnCredit bool    `json:"onCredit"`
	// Payments lists the methods charged at checkout, including a reservation deposit.
	Payments []string `json:"payments"`
}

type History struct {
	Items   []SaleSummary `json:"items"`
	HasMore bool          `json:"hasMore"`
}

type HistoryFilter struct {
	Number string
	// Query matches the customer, a product name, an exact SKU or an exact barcode.
	Query    string
	FromDate string
	ToDate   string
	// Method keeps the sales charged with it, as in the sales report.
	Method string
	Limit  int
	Offset int
}

type Ticket struct {
	Sale         Sale   `json:"sale"`
	MerchantName string `json:"merchantName"`
	StoreCode    string `json:"storeCode"`
	StoreName    string `json:"storeName"`
	StoreAddress string `json:"storeAddress"`
	Timezone     string `json:"timezone"`
}

func (s Service) History(ctx context.Context, actor auth.Identity, storeID string, filter HistoryFilter) (History, error) {
	if !validID(storeID) || filter.Limit < 1 || filter.Limit > 100 || filter.Offset < 0 || filter.Offset > 100000 {
		return History{}, ErrInvalid
	}
	var number *int64
	if search := strings.TrimSpace(filter.Number); search != "" {
		for _, letter := range search {
			if letter < '0' || letter > '9' {
				return History{}, ErrInvalid
			}
		}
		value, err := strconv.ParseInt(search, 10, 64)
		if err != nil || value < 1 {
			return History{}, ErrInvalid
		}
		number = &value
	}
	filter.Query = strings.TrimSpace(filter.Query)
	if !utf8.ValidString(filter.Query) || utf8.RuneCountInString(filter.Query) > 120 || strings.IndexFunc(filter.Query, unicode.IsControl) >= 0 {
		return History{}, ErrInvalid
	}
	for _, day := range []string{filter.FromDate, filter.ToDate} {
		if parsed, err := time.Parse(time.DateOnly, day); day != "" && (err != nil || parsed.Format(time.DateOnly) != day) {
			return History{}, ErrInvalid
		}
	}
	if (filter.FromDate != "" && filter.ToDate != "" && filter.FromDate > filter.ToDate) || !validSaleMethod(filter.Method) {
		return History{}, ErrInvalid
	}
	var result History
	err := s.cash.ReadStoreSales(ctx, actor, storeID, func(tx pgx.Tx, supervise bool) error {
		rows, err := tx.Query(ctx, `SELECT s.id,s.sale_number::text,s.total::text,s.currency,s.created_at,COALESCE(rc.name,vc.name),
			(SELECT COALESCE(sum(i.quantity),0) FROM sale_items i WHERE (i.tenant_id,i.sale_id)=(s.tenant_id,s.id)),
			(SELECT COALESCE(string_agg(DISTINCT i.product_name,', '),'') FROM sale_items i WHERE (i.tenant_id,i.sale_id)=(s.tenant_id,s.id)),
			(SELECT count(*) FROM sale_returns sr WHERE (sr.tenant_id,sr.store_id,sr.sale_id)=(s.tenant_id,s.store_id,s.id)),
			r.id IS NOT NULL,
			(SELECT COALESCE(array_agg(DISTINCT m.method ORDER BY m.method),'{}') FROM (
				SELECT sp.method FROM sale_payments sp WHERE (sp.tenant_id,sp.sale_id)=(s.tenant_id,s.id) AND sp.amount>0
				UNION ALL SELECT d.method FROM reservations bk JOIN reservation_deposits d ON (d.tenant_id,d.store_id,d.reservation_id)=(bk.tenant_id,bk.store_id,bk.id)
				WHERE (bk.tenant_id,bk.store_id,bk.sale_id)=(s.tenant_id,s.store_id,s.id)) m)
			FROM sales s JOIN cash_sessions cs ON (cs.tenant_id,cs.id)=(s.tenant_id,s.cash_session_id)
			JOIN tenants t ON t.id=s.tenant_id
			LEFT JOIN receivables r ON (r.tenant_id,r.store_id,r.sale_id)=(s.tenant_id,s.store_id,s.id)
			LEFT JOIN customers rc ON (rc.tenant_id,rc.id)=(r.tenant_id,r.customer_id)
			LEFT JOIN reservations v ON (v.tenant_id,v.store_id,v.sale_id)=(s.tenant_id,s.store_id,s.id)
			LEFT JOIN customers vc ON (vc.tenant_id,vc.id)=(v.tenant_id,v.customer_id)
			WHERE s.tenant_id=$1 AND s.store_id=$2 AND ($3 OR cs.opened_by=$4)
			AND ($5::bigint IS NULL OR s.sale_number=$5)
			AND ($8='' OR strpos(lower(COALESCE(rc.name,vc.name,'')),lower($8))>0 OR EXISTS (
				SELECT 1 FROM sale_items i WHERE (i.tenant_id,i.sale_id)=(s.tenant_id,s.id) AND (strpos(lower(i.product_name),lower($8))>0
				OR lower(i.sku)=lower($8) OR EXISTS (SELECT 1 FROM barcodes b WHERE (b.tenant_id,b.variant_id)=(i.tenant_id,i.variant_id) AND b.code=$8))))
			AND ($9='' OR (s.created_at AT TIME ZONE t.timezone)::date >= NULLIF($9,'')::date)
			AND ($10='' OR (s.created_at AT TIME ZONE t.timezone)::date <= NULLIF($10,'')::date)
			AND `+salePaidWith("$11")+`
			ORDER BY s.sale_number DESC LIMIT $6 OFFSET $7`,
			actor.TenantID, storeID, supervise, actor.UserID, number, filter.Limit+1, filter.Offset, filter.Query, filter.FromDate, filter.ToDate, filter.Method)
		if err != nil {
			return err
		}
		result.Items, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (SaleSummary, error) {
			var item SaleSummary
			err := row.Scan(&item.ID, &item.Number, &item.Total, &item.Currency, &item.CreatedAt, &item.Customer, &item.Units, &item.Products, &item.Returns, &item.OnCredit, &item.Payments)
			item.CreatedAt = item.CreatedAt.UTC()
			return item, err
		})
		if len(result.Items) > filter.Limit {
			result.Items = result.Items[:filter.Limit]
			result.HasMore = filter.Offset+filter.Limit <= 100000
		}
		return err
	})
	return result, err
}

func (s Service) Ticket(ctx context.Context, actor auth.Identity, storeID, id string) (Ticket, error) {
	if !validID(id) || !validID(storeID) {
		return Ticket{}, ErrInvalid
	}
	var ticket Ticket
	err := s.cash.ReadStoreSales(ctx, actor, storeID, func(tx pgx.Tx, supervise bool) error {
		var err error
		ticket.Sale, err = loadSale(ctx, tx, actor, storeID, id, supervise)
		if err != nil {
			return err
		}
		return tx.QueryRow(ctx, `SELECT merchant_name_snapshot,store_code_snapshot,store_name_snapshot,
			store_address_snapshot,timezone_snapshot FROM sales WHERE tenant_id=$1 AND store_id=$2 AND id=$3`,
			actor.TenantID, storeID, id).Scan(&ticket.MerchantName, &ticket.StoreCode, &ticket.StoreName, &ticket.StoreAddress, &ticket.Timezone)
	})
	return ticket, err
}
