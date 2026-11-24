package inventory

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/tenancy"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrInvalid = errors.New("verificá sucursal, ubicación, variante, cantidad y motivo")
var ErrNotFound = errors.New("sucursal, ubicación o variante inexistente")
var ErrLocationCode = errors.New("ya hay una ubicación con ese código en la sucursal")
var ErrBalance = errors.New("el ajuste supera el stock disponible o el límite de unidades")
var ErrKeyConflict = errors.New("la clave de idempotencia ya se usó para otro ajuste")

const locationFields = "id,store_id,code,name,type,is_sellable"

type Location struct {
	ID         string `json:"id"`
	StoreID    string `json:"storeId"`
	Code       string `json:"code"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	IsSellable bool   `json:"isSellable"`
}

type LocationInput struct {
	StoreID    string `json:"storeId"`
	Code       string `json:"code"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	IsSellable bool   `json:"isSellable"`
}

type Locations struct {
	Items             []Location `json:"items"`
	CanManage         bool       `json:"canManage"`
	CanAdjust         bool       `json:"canAdjust"`
	Timezone          string     `json:"timezone"`
	CanManageMinimums bool       `json:"canManageMinimums"`
}

type Balance struct {
	LocationID     string  `json:"locationId"`
	VariantID      string  `json:"variantId"`
	SKU            string  `json:"sku"`
	Product        string  `json:"product"`
	OnHand         int32   `json:"onHand"`
	Reserved       int32   `json:"reserved"`
	Available      int32   `json:"available"`
	Minimum        *int32  `json:"minimum"`
	LowStock       bool    `json:"lowStock"`
	Active         bool    `json:"active"`
	Currency       string  `json:"currency"`
	CostStatus     string  `json:"costStatus"`
	UnitCost       *string `json:"unitCost"`
	InventoryValue *string `json:"inventoryValue"`
}

type Service struct{ db *pgxpool.Pool }

func New(db *pgxpool.Pool) Service { return Service{db: db} }

func (s Service) Locations(ctx context.Context, actor auth.Identity, storeID string) (Locations, error) {
	var result Locations
	err := s.withStore(ctx, actor, storeID, false, func(tx pgx.Tx, allowed access) error {
		rows, err := tx.Query(ctx, "SELECT "+locationFields+" FROM stock_locations WHERE tenant_id=$1 AND store_id=$2 AND status='active' ORDER BY code",
			actor.TenantID, storeID)
		if err != nil {
			return err
		}
		result.Items, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Location])
		result.CanManage = allowed.manage
		result.CanAdjust = allowed.adjust
		result.Timezone = allowed.timezone
		result.CanManageMinimums = allowed.minimums
		return err
	})
	return result, err
}

func (s Service) CreateLocation(ctx context.Context, actor auth.Identity, input LocationInput) (Location, error) {
	input.Code, input.Name = strings.TrimSpace(input.Code), strings.TrimSpace(input.Name)
	if !validText(input.Code, 40) || !validText(input.Name, 160) ||
		(input.Type != "sales_floor" && input.Type != "warehouse") {
		return Location{}, ErrInvalid
	}
	var result Location
	err := s.withStore(ctx, actor, input.StoreID, true, func(tx pgx.Tx, allowed access) error {
		if !allowed.manage {
			return auth.ErrForbidden
		}
		err := tx.QueryRow(ctx, `INSERT INTO stock_locations(tenant_id,store_id,code,name,type,is_sellable)
			VALUES ($1,$2,$3,$4,$5,$6) RETURNING `+locationFields,
			actor.TenantID, input.StoreID, input.Code, input.Name, input.Type, input.IsSellable).
			Scan(&result.ID, &result.StoreID, &result.Code, &result.Name, &result.Type, &result.IsSellable)
		var conflict *pgconn.PgError
		if errors.As(err, &conflict) && conflict.ConstraintName == "stock_locations_store_code" {
			return ErrLocationCode
		}
		return err
	})
	return result, err
}

func (s Service) Balances(ctx context.Context, actor auth.Identity, storeID, locationID string, lowOnly bool) ([]Balance, error) {
	if locationID != "" && !validID(locationID) {
		return nil, ErrInvalid
	}
	var result []Balance
	err := s.withStore(ctx, actor, storeID, false, func(tx pgx.Tx, _ access) error {
		if locationID != "" {
			if err := requireLocation(ctx, tx, actor.TenantID, storeID, locationID); err != nil {
				return err
			}
		}
		rows, err := tx.Query(ctx, `WITH locations AS (
				SELECT tenant_id,id,store_id,code FROM stock_locations
				WHERE tenant_id=$1 AND store_id=$2 AND status='active'
				AND (NULLIF($3,'') IS NULL OR id=NULLIF($3,'')::uuid)
			), tracked AS (
				SELECT b.tenant_id,b.location_id,b.variant_id FROM stock_balances b
				JOIN locations l ON (l.tenant_id,l.id)=(b.tenant_id,b.location_id)
				UNION
				SELECT m.tenant_id,m.location_id,m.variant_id FROM stock_minimums m
				JOIN locations l ON (l.tenant_id,l.id)=(m.tenant_id,m.location_id)
			), inventory AS (
				SELECT l.id AS location_id,v.id AS variant_id,v.sku,p.name,coalesce(b.quantity_on_hand,0) AS on_hand,
					coalesce(b.quantity_reserved,0) AS reserved,
					coalesce(b.quantity_on_hand-b.quantity_reserved,0) AS available,
					m.minimum,(p.status='active' AND v.status='active') AS active,l.code,ten.currency_code,
					coalesce(cb.status,'unknown') AS cost_status,cb.average_unit_cost::text AS unit_cost,
					CASE WHEN coalesce(b.quantity_on_hand,0)=0 THEN '0.00'
						WHEN cb.status='known' THEN (b.quantity_on_hand::numeric*cb.average_unit_cost)::text
						ELSE NULL END AS inventory_value
				FROM tracked tr JOIN locations l ON (l.tenant_id,l.id)=(tr.tenant_id,tr.location_id)
				JOIN product_variants v ON (v.tenant_id,v.id)=(tr.tenant_id,tr.variant_id)
				JOIN products p ON (p.tenant_id,p.id)=(v.tenant_id,v.product_id)
				JOIN tenants ten ON ten.id=l.tenant_id
				LEFT JOIN stock_balances b ON (b.tenant_id,b.location_id,b.variant_id)=(tr.tenant_id,tr.location_id,tr.variant_id)
				LEFT JOIN stock_minimums m ON (m.tenant_id,m.location_id,m.variant_id)=(tr.tenant_id,tr.location_id,tr.variant_id)
				LEFT JOIN inventory_cost_balances cb ON (cb.tenant_id,cb.store_id,cb.variant_id)=(tr.tenant_id,l.store_id,v.id)
			), alerts AS (
				SELECT *, coalesce(active AND available<=minimum,false) AS low_stock FROM inventory
			)
			SELECT location_id,variant_id,sku,name,on_hand,reserved,available,minimum,low_stock,active,currency_code,cost_status,unit_cost,inventory_value FROM alerts
			WHERE NOT $4 OR low_stock ORDER BY code,sku,variant_id`, actor.TenantID, storeID, locationID, lowOnly)
		if err != nil {
			return err
		}
		result, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Balance])
		return err
	})
	return result, err
}

type access struct {
	manage, adjust, transfer, minimums, catalog bool
	timezone                                    string
}

func (s Service) withStore(ctx context.Context, actor auth.Identity, storeID string, writing bool, operation func(pgx.Tx, access) error) error {
	if !validID(storeID) {
		return ErrInvalid
	}
	options := pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}
	if writing {
		options = pgx.TxOptions{}
	}
	return pgx.BeginTxFunc(ctx, s.db, options, func(tx pgx.Tx) error {
		if writing {
			if err := tenancy.LockActive(ctx, tx, actor.TenantID); err != nil {
				return err
			}
		}
		var readable bool
		var allowed access
		err := tx.QueryRow(ctx, `SELECT
			coalesce(bool_or(rp.permission_code='inventory:read'),false),
			coalesce(bool_or(rp.permission_code='locations:manage'),false),
			coalesce(bool_or(rp.permission_code='inventory:adjust'),false),
			coalesce(bool_or(rp.permission_code='inventory:transfer'),false),
			coalesce(bool_or(rp.permission_code='inventory:manage_minimums'),false),
			coalesce(bool_or(rp.permission_code IN ('catalog:read','catalog:manage')),false), t.timezone
			FROM user_stores us
			JOIN stores s ON (s.tenant_id,s.id)=(us.tenant_id,us.store_id)
			JOIN users u ON (u.tenant_id,u.id)=(us.tenant_id,us.user_id)
			JOIN tenants t ON t.id=u.tenant_id
			LEFT JOIN user_roles ur ON (ur.tenant_id,ur.user_id)=(us.tenant_id,us.user_id)
			LEFT JOIN role_permissions rp ON rp.role_code=ur.role_code
			WHERE us.tenant_id=$1 AND us.user_id=$2 AND us.store_id=$3 AND s.status='active' AND t.status='active' AND u.status='active'
			GROUP BY us.store_id,t.timezone`, actor.TenantID, actor.UserID, storeID).Scan(&readable, &allowed.manage, &allowed.adjust, &allowed.transfer, &allowed.minimums, &allowed.catalog, &allowed.timezone)
		if err != nil {
			return mapNotFound(err)
		}
		if !readable {
			return auth.ErrForbidden
		}
		return operation(tx, allowed)
	})
}

func requireLocation(ctx context.Context, tx pgx.Tx, tenantID, storeID, locationID string) error {
	var id string
	return mapNotFound(tx.QueryRow(ctx, `SELECT id FROM stock_locations
		WHERE tenant_id=$1 AND store_id=$2 AND id=$3 AND status='active'`, tenantID, storeID, locationID).Scan(&id))
}

func mapNotFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func validID(id string) bool {
	var uuid pgtype.UUID
	return len(id) == 36 && uuid.Scan(id) == nil && uuid.String() == id
}

func validText(value string, max int) bool {
	return value != "" && !strings.ContainsRune(value, 0) && utf8.ValidString(value) && utf8.RuneCountInString(value) <= max
}
