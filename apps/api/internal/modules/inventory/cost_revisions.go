package inventory

import (
	"context"
	"errors"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/platform/money"

	"github.com/jackc/pgx/v5"
)

var ErrCostWithoutStock = errors.New("la variante no tiene unidades en esta sucursal: el costo se toma de la próxima compra")

type CostInput struct {
	StoreID string          `json:"storeId"`
	Reason  string          `json:"reason,omitempty"`
	Items   []CostInputItem `json:"items"`
}

type CostInputItem struct {
	VariantID string `json:"variantId"`
	UnitCost  string `json:"unitCost"`
}

type CostRevision struct {
	VariantID        string  `json:"variantId"`
	PreviousUnitCost *string `json:"previousUnitCost"`
	UnitCost         string  `json:"unitCost"`
	QuantityOnHand   int64   `json:"quantityOnHand"`
}

// SetCosts loads the unit cost of the units a variant has today in the
// branch, for stock that entered without a cost (initial stock, returns,
// adjustments). Later purchases keep averaging from this value. Every change
// is recorded in inventory_cost_revisions.
func (s Service) SetCosts(ctx context.Context, actor auth.Identity, input CostInput) ([]CostRevision, error) {
	input.Reason = strings.TrimSpace(input.Reason)
	if len(input.Items) == 0 || len(input.Items) > 100 || !utf8.ValidString(input.Reason) || utf8.RuneCountInString(input.Reason) > 300 ||
		strings.IndexFunc(input.Reason, unicode.IsControl) >= 0 {
		return nil, ErrInvalid
	}
	items := slices.Clone(input.Items)
	slices.SortFunc(items, func(a, b CostInputItem) int { return strings.Compare(a.VariantID, b.VariantID) })
	for i := range items {
		cost, valid := money.Parse(items[i].UnitCost)
		if !valid || !validID(items[i].VariantID) || (i > 0 && items[i-1].VariantID == items[i].VariantID) {
			return nil, ErrInvalid
		}
		items[i].UnitCost = money.Format(cost)
	}
	result := make([]CostRevision, 0, len(items))
	err := s.withStore(ctx, actor, input.StoreID, true, func(tx pgx.Tx, allowed access) error {
		if !allowed.setCosts {
			return auth.ErrForbidden
		}
		for _, item := range items {
			if err := LockCostVariant(ctx, tx, actor.TenantID, input.StoreID, item.VariantID); err != nil {
				return err
			}
			revision := CostRevision{VariantID: item.VariantID, UnitCost: item.UnitCost}
			err := tx.QueryRow(ctx, `SELECT c.quantity_on_hand,c.average_unit_cost::text FROM inventory_cost_balances c
				JOIN product_variants v ON (v.tenant_id,v.id)=(c.tenant_id,c.variant_id)
				WHERE c.tenant_id=$1 AND c.store_id=$2 AND c.variant_id=$3 FOR UPDATE OF c`,
				actor.TenantID, input.StoreID, item.VariantID).Scan(&revision.QuantityOnHand, &revision.PreviousUnitCost)
			if errors.Is(err, pgx.ErrNoRows) || err == nil && revision.QuantityOnHand <= 0 {
				return ErrCostWithoutStock
			}
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE inventory_cost_balances SET average_unit_cost=$4::text::numeric,status='known',updated_at=clock_timestamp()
				WHERE tenant_id=$1 AND store_id=$2 AND variant_id=$3`, actor.TenantID, input.StoreID, item.VariantID, item.UnitCost); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO inventory_cost_revisions(tenant_id,store_id,variant_id,previous_unit_cost,unit_cost,quantity_on_hand,reason,created_by)
				VALUES ($1,$2,$3,$4::text::numeric,$5::text::numeric,$6,$7,$8)`, actor.TenantID, input.StoreID, item.VariantID,
				revision.PreviousUnitCost, item.UnitCost, revision.QuantityOnHand, input.Reason, actor.UserID); err != nil {
				return err
			}
			result = append(result, revision)
		}
		return nil
	})
	return result, err
}
