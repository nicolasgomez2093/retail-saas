package inventory

import (
	"context"

	"github.com/jackc/pgx/v5"
)

func ChangeReserved(ctx context.Context, tx pgx.Tx, tenantID, locationID, variantID string, delta int32) error {
	tag, err := tx.Exec(ctx, `UPDATE stock_balances
		SET quantity_reserved=(quantity_reserved::bigint+$4)::integer,updated_at=clock_timestamp()
		WHERE tenant_id=$1 AND location_id=$2 AND variant_id=$3
		AND quantity_reserved::bigint+$4 BETWEEN 0 AND quantity_on_hand`, tenantID, locationID, variantID, int64(delta))
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return ErrBalance
	}
	return nil
}
