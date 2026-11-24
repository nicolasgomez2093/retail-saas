package tenancy

import (
	"context"
	"errors"

	"retail-platform/api/internal/modules/auth"

	"github.com/jackc/pgx/v5"
)

func LockActive(ctx context.Context, tx pgx.Tx, tenantID string) error {
	var id string
	err := tx.QueryRow(ctx, "SELECT id FROM tenants WHERE id=$1 AND status='active' FOR NO KEY UPDATE", tenantID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return auth.ErrForbidden
	}
	return err
}
