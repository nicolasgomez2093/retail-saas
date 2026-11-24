package sales

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"regexp"
	"slices"
	"time"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/cash"
	"retail-platform/api/internal/modules/inventory"
	"retail-platform/api/internal/modules/pricing"
	"retail-platform/api/internal/modules/reservations"
	"retail-platform/api/internal/platform/money"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrInvalid = errors.New("verificá la caja, la ubicación, las variantes y los datos del pago")
var ErrNotFound = errors.New("venta o variante inexistente")
var ErrNoPrice = errors.New("hay una variante sin precio en esta sucursal")
var ErrPriceChanged = errors.New("cambió el precio de una variante. Revisá el carrito y confirmá nuevamente")
var ErrTender = errors.New("el efectivo recibido no cubre el importe en efectivo")
var ErrTotal = errors.New("la venta supera el límite de importe")
var ErrKey = errors.New("la clave de idempotencia ya se usó para otra venta")
var ErrLocation = errors.New("elegí una ubicación vendible de la sucursal")

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

type Input struct {
	StoreID         string         `json:"storeId"`
	SessionID       string         `json:"cashSessionId"`
	LocationID      string         `json:"locationId"`
	CashReceived    *string        `json:"cashReceived,omitempty"`
	Items           []ItemInput    `json:"items"`
	Payments        []PaymentInput `json:"payments,omitempty"`
	OnCredit        bool           `json:"onCredit,omitempty"`
	CustomerID      string         `json:"customerId,omitempty"`
	ReservationID   string         `json:"reservationId,omitempty"`
	ExpectedDeposit string         `json:"expectedDeposit,omitempty"`
}

type ItemInput struct {
	VariantID       string `json:"variantId"`
	Quantity        int32  `json:"quantity"`
	ExpectedPrice   string `json:"expectedUnitPrice"`
	DiscountPercent int32  `json:"discountPercent,omitempty"`
}

type Item struct {
	VariantID        string  `json:"variantId"`
	Product          string  `json:"product"`
	SKU              string  `json:"sku"`
	Size             string  `json:"size"`
	Color            string  `json:"color"`
	Quantity         int32   `json:"quantity"`
	UnitPrice        string  `json:"unitPrice"`
	Subtotal         string  `json:"subtotal"`
	UnitCostSnapshot *string `json:"unitCostSnapshot"`
	DiscountPercent  int32   `json:"discountPercent"`
	ListUnitPrice    *string `json:"listUnitPrice"`
}

type Sale struct {
	ID           string      `json:"id"`
	StoreID      string      `json:"storeId"`
	SessionID    string      `json:"cashSessionId"`
	LocationID   string      `json:"locationId"`
	Number       string      `json:"number"`
	Currency     string      `json:"currency"`
	Total        string      `json:"total"`
	CashReceived string      `json:"cashReceived"`
	Change       string      `json:"change"`
	CreatedBy    string      `json:"createdBy"`
	CreatedAt    time.Time   `json:"createdAt"`
	Items        []Item      `json:"items"`
	Payments     []Payment   `json:"payments"`
	Credit       *SaleCredit `json:"credit,omitempty"`
	Deposit      *Payment    `json:"deposit,omitempty"`
}

type Service struct{ cash cash.Service }

func New(db *pgxpool.Pool) Service { return Service{cash: cash.New(db)} }

func (s Service) Confirm(ctx context.Context, actor auth.Identity, key string, input Input) (Sale, error) {
	if !keyPattern.MatchString(key) || !validID(input.StoreID) || !validID(input.SessionID) ||
		!validID(input.LocationID) || (input.ReservationID != "" && !validID(input.ReservationID)) || len(input.Items) == 0 || len(input.Items) > 100 {
		return Sale{}, ErrInvalid
	}
	if input.OnCredit && !validID(input.CustomerID) || !input.OnCredit && input.CustomerID != "" {
		return Sale{}, ErrCustomerRequired
	}
	var expectedDeposit int64
	if input.ExpectedDeposit != "" {
		var valid bool
		expectedDeposit, valid = money.Parse(input.ExpectedDeposit)
		if !valid || input.ReservationID == "" {
			return Sale{}, ErrInvalid
		}
		input.ExpectedDeposit = money.Format(expectedDeposit)
	}
	if err := normalizePaymentInput(&input); err != nil {
		return Sale{}, err
	}
	input.Items = slices.Clone(input.Items)
	slices.SortFunc(input.Items, func(a, b ItemInput) int { return cmp.Compare(a.VariantID, b.VariantID) })
	for i, item := range input.Items {
		cents, valid := money.Parse(item.ExpectedPrice)
		if !valid || item.DiscountPercent < 0 || item.DiscountPercent > 80 || !validID(item.VariantID) || item.Quantity <= 0 || (i > 0 && input.Items[i-1].VariantID == item.VariantID) {
			return Sale{}, ErrInvalid
		}
		input.Items[i].ExpectedPrice = money.Format(cents)
	}
	digestInput, err := json.Marshal(input)
	if err != nil {
		return Sale{}, err
	}
	digest := sha256.Sum256(digestInput)
	var result Sale
	err = s.cash.Checkout(ctx, actor, input.StoreID, input.SessionID, func(tx pgx.Tx, session cash.Session, currency string, _ bool) error {
		// Checkout locks the session. The key also needs serialization across different sessions.
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1, 0))",
			"retail:sale:"+actor.TenantID+":"+key); err != nil {
			return err
		}
		var id, createdBy string
		var prior []byte
		err := tx.QueryRow(ctx, "SELECT id,created_by,request_hash FROM sales WHERE tenant_id=$1 AND idempotency_key=$2",
			actor.TenantID, key).Scan(&id, &createdBy, &prior)
		if err == nil {
			if createdBy != actor.UserID || !bytes.Equal(prior, digest[:]) {
				return ErrKey
			}
			result, err = loadSale(ctx, tx, actor, input.StoreID, id, true)
			return err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if slices.ContainsFunc(input.Items, func(item ItemInput) bool { return item.DiscountPercent > 0 }) {
			allowed, err := cash.CanDiscount(ctx, tx, actor)
			if err != nil {
				return err
			}
			if !allowed {
				return auth.ErrForbidden
			}
		}
		if session.Status != "OPEN" {
			return cash.ErrSessionState
		}
		if err := inventory.RequireSellableLocation(ctx, tx, actor.TenantID, input.StoreID, input.LocationID); err != nil {
			if errors.Is(err, inventory.ErrNotFound) {
				return ErrLocation
			}
			return err
		}
		var depositAmount int64
		if input.ReservationID != "" {
			quantities := make([]reservations.Quantity, 0, len(input.Items))
			for _, item := range input.Items {
				quantities = append(quantities, reservations.Quantity{VariantID: item.VariantID, Quantity: item.Quantity})
			}
			deposit, err := reservations.LockForSale(ctx, tx, actor.TenantID, input.StoreID, input.ReservationID, input.LocationID,
				input.CustomerID, quantities)
			if err != nil {
				return err
			}
			if deposit != nil {
				depositAmount, _ = money.Parse(deposit.Amount)
			}
			if depositAmount != expectedDeposit {
				return reservations.ErrDepositChanged
			}
		}
		lines, total, err := quote(ctx, tx, actor, input)
		if err != nil {
			return err
		}
		if depositAmount > total {
			return reservations.ErrDepositExceedsTotal
		}
		payments, balance, err := applyPayments(input, total-depositAmount)
		if err != nil {
			return err
		}
		var customerName string
		if input.OnCredit {
			err = tx.QueryRow(ctx, `SELECT name FROM customers WHERE tenant_id=$1 AND store_id=$2 AND id=$3`,
				actor.TenantID, input.StoreID, input.CustomerID).Scan(&customerName)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrCustomerNotFound
			}
			if err != nil {
				return err
			}
		}
		var number int64
		err = tx.QueryRow(ctx, `INSERT INTO sale_sequences(tenant_id,store_id,next_number) VALUES ($1,$2,2)
			ON CONFLICT (tenant_id,store_id) DO UPDATE SET next_number=sale_sequences.next_number+1
			RETURNING next_number-1`, actor.TenantID, input.StoreID).Scan(&number)
		if err != nil {
			return err
		}
		err = tx.QueryRow(ctx, `INSERT INTO sales(tenant_id,store_id,cash_session_id,location_id,sale_number,currency,total,
			idempotency_key,request_hash,created_by,merchant_name_snapshot,store_code_snapshot,store_name_snapshot,
			store_address_snapshot,timezone_snapshot)
			SELECT r.tenant_id,r.id,$3,$4,$5,$6,$7::text::numeric,$8,$9,$10,t.name,r.code,r.name,r.address,t.timezone
			FROM stores r JOIN tenants t ON t.id=r.tenant_id WHERE r.tenant_id=$1 AND r.id=$2
			RETURNING id`, actor.TenantID, input.StoreID, input.SessionID, input.LocationID, number, currency,
			money.Format(total), key, digest[:], actor.UserID).Scan(&id)
		if err != nil {
			return err
		}
		if input.OnCredit {
			if _, err := tx.Exec(ctx, `INSERT INTO receivables(tenant_id,store_id,sale_id,customer_id,customer_name_snapshot,original_amount,outstanding_amount)
				VALUES ($1,$2,$3,$4,$5,$6::text::numeric,$6::text::numeric)`,
				actor.TenantID, input.StoreID, id, input.CustomerID, customerName, money.Format(balance)); err != nil {
				return err
			}
		}
		for _, line := range lines {
			_, err = tx.Exec(ctx, `INSERT INTO sale_items(tenant_id,sale_id,variant_id,product_name,sku,size,color,quantity,unit_price,unit_cost_snapshot,discount_percent,list_unit_price)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9::text::numeric,$10::text::numeric,$11,$12::text::numeric)`, actor.TenantID, id, line.VariantID, line.Product, line.SKU,
				line.Size, line.Color, line.Quantity, line.UnitPrice, line.UnitCostSnapshot, line.DiscountPercent, line.ListUnitPrice)
			if err != nil {
				return err
			}
			if input.ReservationID != "" {
				if err := inventory.ChangeReserved(ctx, tx, actor.TenantID, input.LocationID, line.VariantID, -line.Quantity); err != nil {
					return err
				}
			}
			if err := inventory.PostSaleMovement(ctx, tx, actor, id, input.LocationID, line.VariantID, line.SKU, line.Quantity); err != nil {
				return err
			}
		}
		for _, payment := range payments {
			if _, err := tx.Exec(ctx, `INSERT INTO sale_payments(tenant_id,sale_id,method,amount,cash_received,change_amount)
				VALUES ($1,$2,$3,$4::text::numeric,$5::text::numeric,$6::text::numeric)`,
				actor.TenantID, id, payment.Method, payment.Amount, payment.CashReceived, payment.change); err != nil {
				return err
			}
			if payment.Method == "cash" {
				amount, _ := money.Parse(payment.Amount)
				if err := cash.PostSaleCash(ctx, tx, actor, session, id, amount); err != nil {
					return err
				}
			}
		}
		if input.ReservationID != "" {
			if err := reservations.CompleteSale(ctx, tx, actor, input.StoreID, input.ReservationID, id); err != nil {
				return err
			}
		}
		result, err = loadSale(ctx, tx, actor, input.StoreID, id, true)
		return err
	})
	return result, err
}

func quote(ctx context.Context, tx pgx.Tx, actor auth.Identity, input Input) ([]Item, int64, error) {
	items := make([]Item, 0, len(input.Items))
	var total int64
	for _, item := range input.Items {
		price, err := pricing.ResolvePrice(ctx, tx, actor, input.StoreID, item.VariantID)
		if errors.Is(err, pricing.ErrNotFound) {
			return nil, 0, ErrNotFound
		}
		if err != nil {
			return nil, 0, err
		}
		if price.Amount == nil {
			return nil, 0, ErrNoPrice
		}
		if *price.Amount != item.ExpectedPrice {
			return nil, 0, ErrPriceChanged
		}
		unit, ok := money.Parse(*price.Amount)
		if !ok {
			return nil, 0, ErrTotal
		}
		// Round the discount down to the cent per unit to preserve the exact refund value.
		percent := int64(item.DiscountPercent)
		unit -= (unit/100)*percent + (unit%100)*percent/100
		if unit > (money.MaxCents-total)/int64(item.Quantity) {
			return nil, 0, ErrTotal
		}
		total += unit * int64(item.Quantity)
		costSnapshot, err := inventory.CostSnapshot(ctx, tx, actor.TenantID, input.StoreID, item.VariantID)
		if err != nil {
			return nil, 0, err
		}
		line := Item{VariantID: item.VariantID, Quantity: item.Quantity, UnitPrice: money.Format(unit), UnitCostSnapshot: costSnapshot, DiscountPercent: item.DiscountPercent}
		if item.DiscountPercent > 0 {
			line.ListUnitPrice = price.Amount
		}
		// Additional attributes (for example a jean length) are kept next to the
		// size in the snapshot so the ticket identifies the exact variant.
		err = tx.QueryRow(ctx, `SELECT p.name,v.sku,concat_ws(' · ',NULLIF(v.size,''),(SELECT string_agg(o->>'value',' · ' ORDER BY n)
			FROM jsonb_array_elements(v.options) WITH ORDINALITY AS x(o,n))),v.color FROM product_variants v
			JOIN products p ON (p.tenant_id,p.id)=(v.tenant_id,v.product_id) WHERE v.tenant_id=$1 AND v.id=$2`,
			actor.TenantID, item.VariantID).Scan(&line.Product, &line.SKU, &line.Size, &line.Color)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, line)
	}
	return items, total, nil
}

func (s Service) Sale(ctx context.Context, actor auth.Identity, storeID, id string) (Sale, error) {
	if !validID(id) || !validID(storeID) {
		return Sale{}, ErrInvalid
	}
	var result Sale
	err := s.cash.ReadStoreSales(ctx, actor, storeID, func(tx pgx.Tx, supervise bool) error {
		var err error
		result, err = loadSale(ctx, tx, actor, storeID, id, supervise)
		return err
	})
	return result, err
}

const saleDepositJoin = `LEFT JOIN reservations booked ON (booked.tenant_id,booked.store_id,booked.sale_id)=(s.tenant_id,s.store_id,s.id)
	LEFT JOIN reservation_deposits d ON (d.tenant_id,d.store_id,d.reservation_id)=(booked.tenant_id,booked.store_id,booked.id)`

func loadSale(ctx context.Context, tx pgx.Tx, actor auth.Identity, storeID, id string, supervise bool) (Sale, error) {
	var sale Sale
	var credit SaleCredit
	var deposit Payment
	err := tx.QueryRow(ctx, `SELECT s.id,s.store_id,s.cash_session_id,s.location_id,s.sale_number::text,s.currency,s.total::text,
		COALESCE(p.cash_received,0.00)::text,COALESCE(p.change_amount,0.00)::text,s.created_by,s.created_at,
		COALESCE(r.id::text,''),COALESCE(r.customer_id::text,''),COALESCE(r.customer_name_snapshot,''),COALESCE(r.original_amount,0.00)::text,
		COALESCE(d.method,''),COALESCE(d.amount,0.00)::text
		FROM sales s
		JOIN cash_sessions cs ON (cs.tenant_id,cs.id)=(s.tenant_id,s.cash_session_id)
		LEFT JOIN sale_payments p ON (p.tenant_id,p.sale_id)=(s.tenant_id,s.id) AND p.method='cash'
		LEFT JOIN receivables r ON (r.tenant_id,r.store_id,r.sale_id)=(s.tenant_id,s.store_id,s.id)
		`+saleDepositJoin+`
		WHERE s.tenant_id=$1 AND s.store_id=$2 AND s.id=$3 AND ($5 OR cs.opened_by=$4)`,
		actor.TenantID, storeID, id, actor.UserID, supervise).Scan(&sale.ID, &sale.StoreID, &sale.SessionID, &sale.LocationID,
		&sale.Number, &sale.Currency, &sale.Total, &sale.CashReceived, &sale.Change, &sale.CreatedBy, &sale.CreatedAt,
		&credit.ID, &credit.CustomerID, &credit.CustomerName, &credit.OriginalAmount, &deposit.Method, &deposit.Amount)
	if errors.Is(err, pgx.ErrNoRows) {
		return Sale{}, ErrNotFound
	}
	if err != nil {
		return Sale{}, err
	}
	if credit.ID != "" {
		sale.Credit = &credit
	}
	if deposit.Method != "" {
		sale.Deposit = &deposit
	}
	sale.CreatedAt = sale.CreatedAt.UTC()
	rows, err := tx.Query(ctx, `SELECT variant_id,product_name,sku,size,color,quantity,unit_price::text,(unit_price*quantity)::text,unit_cost_snapshot::text,discount_percent,list_unit_price::text
		FROM sale_items WHERE tenant_id=$1 AND sale_id=$2 ORDER BY variant_id`, actor.TenantID, id)
	if err != nil {
		return Sale{}, err
	}
	sale.Items, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Item])
	if err != nil {
		return Sale{}, err
	}
	rows, err = tx.Query(ctx, `SELECT method,amount::text FROM sale_payments
		WHERE tenant_id=$1 AND sale_id=$2 ORDER BY method`, actor.TenantID, id)
	if err != nil {
		return Sale{}, err
	}
	sale.Payments, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Payment])
	return sale, err
}

func validID(id string) bool {
	var uuid pgtype.UUID
	return len(id) == 36 && uuid.Scan(id) == nil && uuid.String() == id
}
