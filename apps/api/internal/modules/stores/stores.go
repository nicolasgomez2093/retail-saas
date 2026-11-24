package stores

import (
	"context"

	"retail-platform/api/internal/modules/auth"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	ID   string `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

func List(ctx context.Context, db *pgxpool.Pool, identity auth.Identity) ([]Store, error) {
	// El permiso y las asignaciones se consultan en una sola instantánea.
	rows, err := db.Query(ctx, `
		WITH authorized AS (
			SELECT 1 FROM user_roles ur
			JOIN role_permissions rp ON rp.role_code=ur.role_code
			WHERE ur.tenant_id=$1 AND ur.user_id=$2 AND rp.permission_code='stores:read'
			LIMIT 1
		)
		SELECT assigned.id,assigned.code,assigned.name
		FROM authorized LEFT JOIN LATERAL (
			SELECT s.id,s.code,s.name FROM user_stores us
			JOIN stores s ON (s.tenant_id,s.id)=(us.tenant_id,us.store_id)
			WHERE us.tenant_id=$1 AND us.user_id=$2 AND s.status='active'
		) assigned ON true
		ORDER BY assigned.code`, identity.TenantID, identity.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Store{}
	permitted := false
	for rows.Next() {
		permitted = true
		var id, code, name *string
		if err := rows.Scan(&id, &code, &name); err != nil {
			return nil, err
		}
		if id != nil {
			result = append(result, Store{ID: *id, Code: *code, Name: *name})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !permitted {
		return nil, auth.ErrForbidden
	}
	return result, nil
}
