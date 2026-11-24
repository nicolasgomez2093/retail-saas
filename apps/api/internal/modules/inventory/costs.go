package inventory

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// LockCostVariant serializes cost reads and stock movements for one store/variant.
func LockCostVariant(ctx context.Context, tx pgx.Tx, tenantID, storeID, variantID string) error {
	_, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "retail:inventory-cost:"+tenantID+":"+storeID+":"+variantID)
	return err
}

func CostSnapshot(ctx context.Context, tx pgx.Tx, tenantID, storeID, variantID string) (*string, error) {
	if err := LockCostVariant(ctx, tx, tenantID, storeID, variantID); err != nil {
		return nil, err
	}
	var cost *string
	err := tx.QueryRow(ctx, `SELECT average_unit_cost::text FROM inventory_cost_balances
		WHERE tenant_id=$1 AND store_id=$2 AND variant_id=$3 AND status='known'`, tenantID, storeID, variantID).Scan(&cost)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	return cost, err
}

func RecordPurchaseCost(ctx context.Context, tx pgx.Tx, tenantID, storeID, variantID string, accepted int32, unitCost *string) error {
	_, err := tx.Exec(ctx, `UPDATE inventory_cost_balances SET
		average_unit_cost=CASE
			WHEN $4::text IS NULL THEN NULL
			WHEN status='known' AND quantity_on_hand>$5 THEN
				(((quantity_on_hand-$5)::numeric*average_unit_cost)+($5::numeric*$4::text::numeric))/quantity_on_hand
			WHEN quantity_on_hand=$5 THEN $4::text::numeric
			ELSE NULL END,
		status=CASE
			WHEN $4::text IS NULL THEN 'unknown'
			WHEN status='known' AND quantity_on_hand>$5 THEN 'known'
			WHEN quantity_on_hand=$5 THEN 'known'
			ELSE 'unknown' END,
		updated_at=clock_timestamp()
		WHERE tenant_id=$1 AND store_id=$2 AND variant_id=$3 AND quantity_on_hand >= $5`, tenantID, storeID, variantID, unitCost, accepted)
	return err
}
