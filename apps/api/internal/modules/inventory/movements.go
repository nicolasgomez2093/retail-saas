package inventory

import (
	"context"
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"retail-platform/api/internal/modules/auth"

	"github.com/jackc/pgx/v5"
)

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

const movementFields = "id,location_id,variant_id,actor_id,sku_snapshot,movement_type,quantity_delta,quantity_before,quantity_after,reason,created_at,transfer_id,count_id,sale_id,return_id,exchange_id,purchase_receipt_id,adjustment_precondition"

var (
	ErrAdjustmentStale    = errors.New("el saldo cambió desde la consulta")
	ErrAdjustmentInactive = errors.New("el producto o la variante están inactivos")
)

const unrecorded = "unrecorded"

func validPrecondition(value string) bool {
	if value == unrecorded {
		return true
	}
	number, err := strconv.ParseInt(value, 10, 64)
	return err == nil && number >= 0 && strconv.FormatInt(number, 10) == value
}

func matchesVersion(expected string, actual *string) bool {
	if actual == nil {
		return expected == unrecorded
	}
	return expected == *actual
}

type Adjustment struct {
	StoreID          string  `json:"storeId"`
	LocationID       string  `json:"locationId"`
	VariantID        string  `json:"variantId"`
	Delta            int32   `json:"delta"`
	Reason           string  `json:"reason"`
	ExpectedRevision *string `json:"expectedRevision,omitempty"`
}

type Movement struct {
	ID                     string    `json:"id"`
	LocationID             string    `json:"locationId"`
	VariantID              string    `json:"variantId"`
	ActorID                string    `json:"actorId"`
	SKU                    string    `json:"sku"`
	Type                   string    `json:"type"`
	Delta                  int32     `json:"delta"`
	Before                 int32     `json:"before"`
	After                  int32     `json:"after"`
	Reason                 string    `json:"reason"`
	CreatedAt              time.Time `json:"createdAt"`
	TransferID             *string   `json:"transferId"`
	CountID                *string   `json:"countId"`
	SaleID                 *string   `json:"saleId"`
	ReturnID               *string   `json:"returnId"`
	PurchaseReceiptID      *string   `json:"purchaseReceiptId,omitempty"`
	ExchangeID             *string   `json:"exchangeId"`
	adjustmentPrecondition *string
}

func (s Service) Adjust(ctx context.Context, actor auth.Identity, key string, input Adjustment) (Movement, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if !validID(input.LocationID) || !validID(input.VariantID) || !keyPattern.MatchString(key) ||
		input.Delta == 0 || !validText(input.Reason, 300) ||
		input.ExpectedRevision != nil && !validPrecondition(*input.ExpectedRevision) {
		return Movement{}, ErrInvalid
	}
	var result Movement
	err := s.withStore(ctx, actor, input.StoreID, true, func(tx pgx.Tx, allowed access) error {
		if !allowed.adjust {
			return auth.ErrForbidden
		}
		if err := requireLocation(ctx, tx, actor.TenantID, input.StoreID, input.LocationID); err != nil {
			return err
		}
		prior, err := scanMovement(tx.QueryRow(ctx, "SELECT "+movementFields+" FROM stock_movements WHERE tenant_id=$1 AND idempotency_key=$2",
			actor.TenantID, key))
		if err == nil {
			if prior.ActorID != actor.UserID || prior.LocationID != input.LocationID || prior.VariantID != input.VariantID ||
				prior.Delta != input.Delta || prior.Reason != input.Reason || prior.Type != "adjustment" || !sameAdjustmentVersion(prior.adjustmentPrecondition, input.ExpectedRevision) {
				return ErrKeyConflict
			}
			result = prior
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var sku string
		var active bool
		err = tx.QueryRow(ctx, `SELECT v.sku,v.status='active' AND p.status='active'
			FROM product_variants v JOIN products p ON (p.tenant_id,p.id)=(v.tenant_id,v.product_id)
			WHERE v.tenant_id=$1 AND v.id=$2`, actor.TenantID, input.VariantID).Scan(&sku, &active)
		if err != nil {
			return mapNotFound(err)
		}
		if input.ExpectedRevision != nil && !active {
			return ErrAdjustmentInactive
		}
		result, err = postMovement(ctx, tx, actor, Movement{
			LocationID: input.LocationID, VariantID: input.VariantID, SKU: sku, Type: "adjustment",
			Delta: input.Delta, Reason: input.Reason, adjustmentPrecondition: input.ExpectedRevision,
		}, &key)
		return err
	})
	return result, err
}

func sameAdjustmentVersion(a, b *string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func (s Service) AdjustmentResult(ctx context.Context, actor auth.Identity, storeID, key string) (Movement, error) {
	if !keyPattern.MatchString(key) {
		return Movement{}, ErrInvalid
	}
	var result Movement
	err := s.withStore(ctx, actor, storeID, false, func(tx pgx.Tx, allowed access) error {
		if !allowed.adjust {
			return auth.ErrForbidden
		}
		var err error
		result, err = scanMovement(tx.QueryRow(ctx, "SELECT "+movementFields+` FROM stock_movements m
			WHERE tenant_id=$1 AND actor_id=$2 AND idempotency_key=$3 AND movement_type='adjustment'
			AND EXISTS (SELECT 1 FROM stock_locations l WHERE l.tenant_id=m.tenant_id AND l.id=m.location_id AND l.store_id=$4)`,
			actor.TenantID, actor.UserID, key, storeID))
		return mapNotFound(err)
	})
	return result, err
}

func postMovement(ctx context.Context, tx pgx.Tx, actor auth.Identity, movement Movement, key *string) (Movement, error) {
	var storeID string
	if err := tx.QueryRow(ctx, "SELECT store_id FROM stock_locations WHERE tenant_id=$1 AND id=$2", actor.TenantID, movement.LocationID).Scan(&storeID); err != nil {
		return Movement{}, mapNotFound(err)
	}
	if err := LockCostVariant(ctx, tx, actor.TenantID, storeID, movement.VariantID); err != nil {
		return Movement{}, err
	}
	if movement.adjustmentPrecondition != nil {
		var revision *string
		err := tx.QueryRow(ctx, `SELECT revision::text FROM stock_balances
			WHERE tenant_id=$1 AND location_id=$2 AND variant_id=$3 FOR UPDATE`,
			actor.TenantID, movement.LocationID, movement.VariantID).Scan(&revision)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return Movement{}, err
		}
		if !matchesVersion(*movement.adjustmentPrecondition, revision) {
			return Movement{}, ErrAdjustmentStale
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO stock_balances(tenant_id,location_id,variant_id)
		VALUES ($1,$2,$3) ON CONFLICT DO NOTHING`, actor.TenantID, movement.LocationID, movement.VariantID); err != nil {
		return Movement{}, err
	}
	var reserved int32
	err := tx.QueryRow(ctx, `SELECT quantity_on_hand,quantity_reserved FROM stock_balances
		WHERE tenant_id=$1 AND location_id=$2 AND variant_id=$3 FOR UPDATE`,
		actor.TenantID, movement.LocationID, movement.VariantID).Scan(&movement.Before, &reserved)
	if err != nil {
		return Movement{}, err
	}
	after := int64(movement.Before) + int64(movement.Delta)
	if after < int64(reserved) || after > math.MaxInt32 {
		return Movement{}, ErrBalance
	}
	if _, err := tx.Exec(ctx, `UPDATE stock_balances SET quantity_on_hand=$4,updated_at=clock_timestamp()
		WHERE tenant_id=$1 AND location_id=$2 AND variant_id=$3`, actor.TenantID, movement.LocationID,
		movement.VariantID, int32(after)); err != nil {
		return Movement{}, err
	}
	return scanMovement(tx.QueryRow(ctx, `INSERT INTO stock_movements(tenant_id,location_id,variant_id,actor_id,
		sku_snapshot,movement_type,quantity_delta,quantity_before,quantity_after,reason,idempotency_key,created_at,transfer_id,count_id,sale_id,return_id,exchange_id,purchase_receipt_id,adjustment_precondition)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,clock_timestamp(),$12,$13,$14,$15,$16,$17,$18) RETURNING `+movementFields,
		actor.TenantID, movement.LocationID, movement.VariantID, actor.UserID, movement.SKU, movement.Type,
		movement.Delta, movement.Before, int32(after), movement.Reason, key, movement.TransferID, movement.CountID, movement.SaleID, movement.ReturnID, movement.ExchangeID, movement.PurchaseReceiptID, movement.adjustmentPrecondition))
}

func PostPurchaseReceiptMovement(ctx context.Context, tx pgx.Tx, actor auth.Identity, receiptID, storeID, locationID, variantID, sku string, accepted int32, unitCost *string) error {
	if accepted <= 0 {
		return ErrInvalid
	}
	_, err := postMovement(ctx, tx, actor, Movement{
		LocationID: locationID, VariantID: variantID, SKU: sku, Type: "purchase_receipt",
		Delta: accepted, Reason: "Recepción de compra", PurchaseReceiptID: &receiptID,
	}, nil)
	if err != nil {
		return err
	}
	return RecordPurchaseCost(ctx, tx, actor.TenantID, storeID, variantID, accepted, unitCost)
}

func (s Service) Movements(ctx context.Context, actor auth.Identity, storeID, locationID string) ([]Movement, error) {
	if !validID(locationID) {
		return nil, ErrInvalid
	}
	var result []Movement
	err := s.withStore(ctx, actor, storeID, false, func(tx pgx.Tx, _ access) error {
		if err := requireLocation(ctx, tx, actor.TenantID, storeID, locationID); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, "SELECT "+movementFields+` FROM stock_movements
			WHERE tenant_id=$1 AND location_id=$2 ORDER BY created_at DESC,id DESC LIMIT 50`, actor.TenantID, locationID)
		if err != nil {
			return err
		}
		result, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Movement, error) { return scanMovement(row) })
		return err
	})
	return result, err
}

func scanMovement(row pgx.Row) (Movement, error) {
	var item Movement
	err := row.Scan(&item.ID, &item.LocationID, &item.VariantID, &item.ActorID, &item.SKU, &item.Type,
		&item.Delta, &item.Before, &item.After, &item.Reason, &item.CreatedAt, &item.TransferID, &item.CountID, &item.SaleID, &item.ReturnID, &item.ExchangeID, &item.PurchaseReceiptID, &item.adjustmentPrecondition)
	item.CreatedAt = item.CreatedAt.UTC()
	return item, err
}

// InitialStockReason identifies the movements registered while creating a
// product, so they can be told apart from later manual adjustments.
const InitialStockReason = "Stock inicial · alta de producto"

// PostInitialStock records the first units of a variant created in the same
// transaction. The caller must hold the tenant write lock and have verified
// inventory:adjust for the location's store. Idempotency belongs to the
// enclosing operation, which stores its confirmed result.
func PostInitialStock(ctx context.Context, tx pgx.Tx, actor auth.Identity, locationID, variantID, sku string, quantity int32) (Movement, error) {
	if quantity <= 0 {
		return Movement{}, ErrInvalid
	}
	return postMovement(ctx, tx, actor, Movement{
		LocationID: locationID, VariantID: variantID, SKU: sku, Type: "adjustment",
		Delta: quantity, Reason: InitialStockReason,
	}, nil)
}
