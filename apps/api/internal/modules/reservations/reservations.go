package reservations

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

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrInvalid = errors.New("verificá el cliente, la ubicación, el vencimiento y las cantidades")
var ErrNotFound = errors.New("reserva, cliente, ubicación o variante inexistente")
var ErrState = errors.New("la reserva ya está cerrada o no está disponible para esta operación")
var ErrNotExpired = errors.New("la reserva todavía no alcanzó su fecha de vencimiento")
var ErrMismatch = errors.New("el cliente, la ubicación o los artículos no coinciden con la reserva")
var ErrKey = errors.New("la clave de idempotencia ya se usó para otra reserva")

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

type Quantity struct {
	VariantID string `json:"variantId"`
	Quantity  int32  `json:"quantity"`
}

type Input struct {
	StoreID    string     `json:"storeId"`
	LocationID string     `json:"locationId"`
	CustomerID string     `json:"customerId"`
	ExpiresAt  *time.Time `json:"expiresAt"`
	Items      []Quantity `json:"items"`
}

type Line struct {
	VariantID string `json:"variantId"`
	Quantity  int32  `json:"quantity"`
	Product   string `json:"product"`
	SKU       string `json:"sku"`
	Size      string `json:"size"`
	Color     string `json:"color"`
}

type Reservation struct {
	ID           string     `json:"id"`
	StoreID      string     `json:"storeId"`
	LocationID   string     `json:"locationId"`
	LocationName string     `json:"locationName"`
	CustomerID   string     `json:"customerId"`
	CustomerName string     `json:"customerName"`
	Status       string     `json:"status"`
	CreatedAt    time.Time  `json:"createdAt"`
	ExpiresAt    *time.Time `json:"expiresAt"`
	ClosedAt     *time.Time `json:"closedAt"`
	SaleID       *string    `json:"saleId"`
	Items        []Line     `json:"items"`
	Deposit      *Deposit   `json:"deposit"`
}

type Page struct {
	Items   []Reservation `json:"items"`
	HasMore bool          `json:"hasMore"`
}

type Service struct{ cash cash.Service }

func New(db *pgxpool.Pool) Service { return Service{cash: cash.New(db)} }

const selectReservation = `SELECT r.id,r.store_id,r.location_id,l.name,r.customer_id,r.customer_name,r.status,
	r.created_at,r.expires_at,r.closed_at,r.sale_id,
	(SELECT jsonb_agg(jsonb_build_object('variantId',i.variant_id,'quantity',i.quantity,'product',i.product_name,
		'sku',i.sku,'size',i.size,'color',i.color) ORDER BY i.variant_id)
	 FROM reservation_items i WHERE i.tenant_id=r.tenant_id AND i.reservation_id=r.id),
	CASE WHEN d.id IS NOT NULL THEN jsonb_build_object('id',d.id,'cashSessionId',d.cash_session_id,'method',d.method,
		'amount',d.amount::text,'createdBy',d.created_by,'createdAt',d.created_at) END
	FROM reservations r LEFT JOIN reservation_deposits d ON (d.tenant_id,d.store_id,d.reservation_id)=(r.tenant_id,r.store_id,r.id)
	JOIN stock_locations l ON (l.tenant_id,l.id)=(r.tenant_id,r.location_id)
	WHERE r.tenant_id=$1 AND r.store_id=$2`

func (s Service) Create(ctx context.Context, actor auth.Identity, key string, input Input) (Reservation, error) {
	if !validID(input.StoreID) || !validID(input.LocationID) || !validID(input.CustomerID) ||
		!keyPattern.MatchString(key) || len(input.Items) == 0 || len(input.Items) > 100 {
		return Reservation{}, ErrInvalid
	}
	input.Items = slices.Clone(input.Items)
	slices.SortFunc(input.Items, func(a, b Quantity) int { return cmp.Compare(a.VariantID, b.VariantID) })
	for i, line := range input.Items {
		if !validID(line.VariantID) || line.Quantity <= 0 || (i > 0 && input.Items[i-1].VariantID == line.VariantID) {
			return Reservation{}, ErrInvalid
		}
	}
	if input.ExpiresAt != nil {
		at := input.ExpiresAt.UTC().Truncate(time.Microsecond)
		input.ExpiresAt = &at
	}
	body, err := json.Marshal(input)
	if err != nil {
		return Reservation{}, err
	}
	digest := sha256.Sum256(body)
	var result Reservation
	err = s.cash.WriteStoreSales(ctx, actor, input.StoreID, func(tx pgx.Tx, _ bool) error {
		if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "retail:reservation:"+actor.TenantID+":"+key); err != nil {
			return err
		}
		var id, creator string
		var prior []byte
		err := tx.QueryRow(ctx, "SELECT id,created_by,request_hash FROM reservations WHERE tenant_id=$1 AND idempotency_key=$2", actor.TenantID, key).
			Scan(&id, &creator, &prior)
		if err == nil {
			if creator != actor.UserID || !bytes.Equal(prior, digest[:]) {
				return ErrKey
			}
			result, err = read(ctx, tx, actor.TenantID, input.StoreID, id)
			return err
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err := inventory.RequireSellableLocation(ctx, tx, actor.TenantID, input.StoreID, input.LocationID); err != nil {
			if errors.Is(err, inventory.ErrNotFound) {
				return ErrNotFound
			}
			return err
		}
		var future bool
		if err := tx.QueryRow(ctx, "SELECT $1::timestamptz IS NULL OR $1 > clock_timestamp()", input.ExpiresAt).Scan(&future); err != nil {
			return err
		}
		if !future {
			return ErrInvalid
		}
		err = tx.QueryRow(ctx, `INSERT INTO reservations(tenant_id,store_id,location_id,customer_id,customer_name,expires_at,created_by,idempotency_key,request_hash)
			SELECT c.tenant_id,c.store_id,$3,c.id,c.name,$5,$6,$7,$8 FROM customers c
			WHERE c.tenant_id=$1 AND c.store_id=$2 AND c.id=$4 RETURNING id`,
			actor.TenantID, input.StoreID, input.LocationID, input.CustomerID, input.ExpiresAt, actor.UserID, key, digest[:]).Scan(&id)
		if err != nil {
			return notFound(err)
		}
		for _, line := range input.Items {
			tag, err := tx.Exec(ctx, `INSERT INTO reservation_items(tenant_id,reservation_id,variant_id,quantity,product_name,sku,size,color)
				SELECT v.tenant_id,$2,v.id,$4,p.name,v.sku,v.size,v.color FROM product_variants v
				JOIN products p ON (p.tenant_id,p.id)=(v.tenant_id,v.product_id)
				WHERE v.tenant_id=$1 AND v.id=$3 AND v.status='active' AND p.status='active'`,
				actor.TenantID, id, line.VariantID, line.Quantity)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return ErrNotFound
			}
			if err := inventory.ChangeReserved(ctx, tx, actor.TenantID, input.LocationID, line.VariantID, line.Quantity); err != nil {
				return err
			}
		}
		result, err = read(ctx, tx, actor.TenantID, input.StoreID, id)
		return err
	})
	return result, err
}

func (s Service) List(ctx context.Context, actor auth.Identity, storeID, status string, limit, offset int) (Page, error) {
	switch status {
	case "ACTIVE", "FULFILLED", "CANCELLED", "EXPIRED":
	default:
		return Page{}, ErrInvalid
	}
	if limit < 1 || limit > 50 || offset < 0 || offset > 100000 {
		return Page{}, ErrInvalid
	}
	var page Page
	err := s.cash.ReadStoreSales(ctx, actor, storeID, func(tx pgx.Tx, _ bool) error {
		rows, err := tx.Query(ctx, selectReservation+` AND r.status=$3 ORDER BY r.created_at DESC,r.id DESC LIMIT $4 OFFSET $5`,
			actor.TenantID, storeID, status, limit+1, offset)
		if err != nil {
			return err
		}
		page.Items, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Reservation, error) { return scan(row) })
		if len(page.Items) > limit {
			page.Items = page.Items[:limit]
			page.HasMore = offset+limit <= 100000
		}
		return err
	})
	return page, err
}

func (s Service) Release(ctx context.Context, actor auth.Identity, storeID, id, status string) (Reservation, error) {
	if !validID(id) || (status != "CANCELLED" && status != "EXPIRED") {
		return Reservation{}, ErrInvalid
	}
	var result Reservation
	err := s.cash.WriteStoreSales(ctx, actor, storeID, func(tx pgx.Tx, _ bool) error {
		state, elapsed, err := lock(ctx, tx, actor.TenantID, storeID, id)
		if err != nil {
			return err
		}
		result, err = read(ctx, tx, actor.TenantID, storeID, id)
		if err != nil || state == status {
			return err
		}
		if state != "ACTIVE" {
			return ErrState
		}
		if status == "EXPIRED" && !elapsed {
			return ErrNotExpired
		}
		for _, line := range result.Items {
			if err := inventory.ChangeReserved(ctx, tx, actor.TenantID, result.LocationID, line.VariantID, -line.Quantity); err != nil {
				return err
			}
		}
		if err := finish(ctx, tx, actor, storeID, id, status, nil); err != nil {
			return err
		}
		result, err = read(ctx, tx, actor.TenantID, storeID, id)
		return err
	})
	return result, err
}

func LockForSale(ctx context.Context, tx pgx.Tx, tenantID, storeID, id, locationID, creditCustomer string, lines []Quantity) (*Deposit, error) {
	state, elapsed, err := lock(ctx, tx, tenantID, storeID, id)
	if err != nil {
		return nil, err
	}
	if state != "ACTIVE" || elapsed {
		return nil, ErrState
	}
	reservation, err := read(ctx, tx, tenantID, storeID, id)
	if err != nil {
		return nil, err
	}
	if locationID != reservation.LocationID || (creditCustomer != "" && creditCustomer != reservation.CustomerID) || len(lines) != len(reservation.Items) {
		return nil, ErrMismatch
	}
	for i, line := range lines {
		if line.VariantID != reservation.Items[i].VariantID || line.Quantity != reservation.Items[i].Quantity {
			return nil, ErrMismatch
		}
	}
	return reservation.Deposit, nil
}

func CompleteSale(ctx context.Context, tx pgx.Tx, actor auth.Identity, storeID, id, saleID string) error {
	return finish(ctx, tx, actor, storeID, id, "FULFILLED", &saleID)
}

func finish(ctx context.Context, tx pgx.Tx, actor auth.Identity, storeID, id, status string, saleID *string) error {
	_, err := tx.Exec(ctx, `UPDATE reservations SET status=$4,closed_at=clock_timestamp(),closed_by=$5,sale_id=$6
		WHERE tenant_id=$1 AND store_id=$2 AND id=$3`, actor.TenantID, storeID, id, status, actor.UserID, saleID)
	return err
}

func lock(ctx context.Context, tx pgx.Tx, tenantID, storeID, id string) (string, bool, error) {
	var state string
	var elapsed bool
	err := tx.QueryRow(ctx, `SELECT status,coalesce(expires_at <= clock_timestamp(),false)
		FROM reservations WHERE tenant_id=$1 AND store_id=$2 AND id=$3 FOR UPDATE`, tenantID, storeID, id).Scan(&state, &elapsed)
	return state, elapsed, notFound(err)
}

func read(ctx context.Context, tx pgx.Tx, tenantID, storeID, id string) (Reservation, error) {
	return scan(tx.QueryRow(ctx, selectReservation+" AND r.id=$3", tenantID, storeID, id))
}

func scan(row pgx.Row) (Reservation, error) {
	var result Reservation
	err := row.Scan(&result.ID, &result.StoreID, &result.LocationID, &result.LocationName, &result.CustomerID, &result.CustomerName, &result.Status,
		&result.CreatedAt, &result.ExpiresAt, &result.ClosedAt, &result.SaleID, &result.Items, &result.Deposit)
	return result, notFound(err)
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func validID(id string) bool {
	var uuid pgtype.UUID
	return len(id) == 36 && uuid.Scan(id) == nil && uuid.String() == id
}
