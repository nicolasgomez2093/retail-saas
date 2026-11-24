package inventory

import (
	"context"
	"errors"

	"retail-platform/api/internal/modules/auth"

	"github.com/jackc/pgx/v5"
)

var ErrMinimumInvalid = errors.New("verificá la ubicación, la variante y el mínimo. El mínimo debe ser un entero mayor o igual a cero")

var ErrMinimumStale = errors.New("el mínimo cambió desde la consulta")

const minimumVersionSQL = "m.version::text"

type MinimumInput struct {
	StoreID         string
	LocationID      string
	VariantID       string
	Minimum         *int32
	ExpectedVersion *string
}

func (s Service) SetMinimum(ctx context.Context, actor auth.Identity, input MinimumInput) error {
	if !validID(input.StoreID) || !validID(input.LocationID) || !validID(input.VariantID) ||
		(input.Minimum != nil && *input.Minimum < 0) ||
		input.ExpectedVersion != nil && *input.ExpectedVersion != unrecorded && !validID(*input.ExpectedVersion) {
		return ErrMinimumInvalid
	}
	return s.withStore(ctx, actor, input.StoreID, true, func(tx pgx.Tx, allowed access) error {
		if !allowed.minimums {
			return auth.ErrForbidden
		}
		if err := requireLocation(ctx, tx, actor.TenantID, input.StoreID, input.LocationID); err != nil {
			return err
		}
		var variant string
		err := tx.QueryRow(ctx, "SELECT id FROM product_variants WHERE tenant_id=$1 AND id=$2", actor.TenantID, input.VariantID).Scan(&variant)
		if err != nil {
			return mapNotFound(err)
		}
		if input.ExpectedVersion != nil {
			var version *string
			err := tx.QueryRow(ctx, `SELECT `+minimumVersionSQL+` FROM stock_minimums m
				WHERE tenant_id=$1 AND location_id=$2 AND variant_id=$3 FOR UPDATE`, actor.TenantID, input.LocationID, variant).Scan(&version)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			if !matchesVersion(*input.ExpectedVersion, version) {
				return ErrMinimumStale
			}
		}
		if input.Minimum == nil {
			_, err = tx.Exec(ctx, "DELETE FROM stock_minimums WHERE tenant_id=$1 AND location_id=$2 AND variant_id=$3",
				actor.TenantID, input.LocationID, variant)
		} else {
			_, err = tx.Exec(ctx, `INSERT INTO stock_minimums(tenant_id,location_id,variant_id,minimum,updated_by)
				VALUES ($1,$2,$3,$4,$5)
				ON CONFLICT (tenant_id,location_id,variant_id) DO UPDATE
				SET minimum=excluded.minimum,updated_by=excluded.updated_by,updated_at=clock_timestamp(),version=gen_random_uuid()
				WHERE stock_minimums.minimum<>excluded.minimum`,
				actor.TenantID, input.LocationID, variant, *input.Minimum, actor.UserID)
		}
		return err
	})
}
