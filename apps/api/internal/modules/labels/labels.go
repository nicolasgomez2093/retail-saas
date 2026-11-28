// Package labels keeps the commerce-wide choices for printed labels.
package labels

import (
	"context"
	"errors"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/tenancy"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Settings struct {
	ShowPrice bool `json:"showPrice"`
}

type Service struct{ db *pgxpool.Pool }

func New(db *pgxpool.Pool) Service { return Service{db: db} }

// Get is readable by every active user of the commerce: whoever prints labels needs to know.
func (s Service) Get(ctx context.Context, actor auth.Identity) (Settings, error) {
	var result Settings
	err := s.db.QueryRow(ctx, `SELECT t.label_show_price FROM tenants t JOIN users u ON u.tenant_id=t.id
		WHERE t.id=$1 AND u.id=$2 AND t.status='active' AND u.status='active'`, actor.TenantID, actor.UserID).Scan(&result.ShowPrice)
	if errors.Is(err, pgx.ErrNoRows) {
		return result, auth.ErrForbidden
	}
	return result, err
}

// Save requires catalog:manage, like the rest of the label and catalog setup.
func (s Service) Save(ctx context.Context, actor auth.Identity, input Settings) (Settings, error) {
	err := pgx.BeginTxFunc(ctx, s.db, pgx.TxOptions{}, func(tx pgx.Tx) error {
		if err := tenancy.LockActive(ctx, tx, actor.TenantID); err != nil {
			return err
		}
		var allowed bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM user_roles ur JOIN role_permissions rp ON rp.role_code=ur.role_code
			JOIN users u ON (u.tenant_id,u.id)=(ur.tenant_id,ur.user_id)
			WHERE ur.tenant_id=$1 AND ur.user_id=$2 AND rp.permission_code='catalog:manage' AND u.status='active')`,
			actor.TenantID, actor.UserID).Scan(&allowed); err != nil {
			return err
		}
		if !allowed {
			return auth.ErrForbidden
		}
		_, err := tx.Exec(ctx, `UPDATE tenants SET label_show_price=$2 WHERE id=$1`, actor.TenantID, input.ShowPrice)
		return err
	})
	return input, err
}
