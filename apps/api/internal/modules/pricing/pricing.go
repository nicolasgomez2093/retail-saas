package pricing

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/tenancy"
	"retail-platform/api/internal/platform/money"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrInvalid = errors.New("verificá los datos de la lista o del precio")
var ErrNotFound = errors.New("lista, sucursal o variante inexistente")
var ErrNameTaken = errors.New("ya hay una lista con ese nombre")

type List struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Assignment struct {
	ID     string  `json:"id"`
	Code   string  `json:"code"`
	Name   string  `json:"name"`
	ListID *string `json:"listId"`
}

type Configuration struct {
	Currency string       `json:"currency"`
	Lists    []List       `json:"lists"`
	Stores   []Assignment `json:"stores"`
}

type Item struct {
	Amount *string `json:"amount"`
}

type Resolved struct {
	ListID   *string `json:"listId"`
	Amount   *string `json:"amount"`
	Currency string  `json:"currency"`
	Reason   string  `json:"reason"`
}

type Service struct{ db *pgxpool.Pool }

func New(db *pgxpool.Pool) Service { return Service{db: db} }

func (s Service) Configuration(ctx context.Context, actor auth.Identity) (Configuration, error) {
	var result Configuration
	err := s.manage(ctx, actor, false, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, "SELECT currency_code FROM tenants WHERE id=$1", actor.TenantID).Scan(&result.Currency); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, "SELECT id,name FROM price_lists WHERE tenant_id=$1 ORDER BY name,id", actor.TenantID)
		if err != nil {
			return err
		}
		result.Lists, err = pgx.CollectRows(rows, pgx.RowToStructByPos[List])
		if err != nil {
			return err
		}
		rows, err = tx.Query(ctx, `SELECT s.id,s.code,s.name,a.price_list_id FROM stores s
			LEFT JOIN store_price_list_assignments a ON (a.tenant_id,a.store_id)=(s.tenant_id,s.id)
			WHERE s.tenant_id=$1 AND s.status='active' ORDER BY s.code`, actor.TenantID)
		if err != nil {
			return err
		}
		result.Stores, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Assignment])
		return err
	})
	return result, err
}

func (s Service) Create(ctx context.Context, actor auth.Identity, name string) (List, error) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 120 || strings.ContainsRune(name, 0) {
		return List{}, ErrInvalid
	}
	list := List{Name: name}
	err := s.manage(ctx, actor, true, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, "INSERT INTO price_lists(tenant_id,name) VALUES ($1,$2) RETURNING id", actor.TenantID, name).Scan(&list.ID)
		var conflict *pgconn.PgError
		if errors.As(err, &conflict) && conflict.ConstraintName == "price_lists_tenant_name" {
			return ErrNameTaken
		}
		if err != nil {
			return err
		}
		return audit(ctx, tx, actor, "create_list", &list.ID, nil, nil, nil, list)
	})
	return list, err
}

func (s Service) Assignment(ctx context.Context, actor auth.Identity, storeID string, listID *string) error {
	if !validID(storeID) || listID != nil && !validID(*listID) {
		return ErrInvalid
	}
	return s.manage(ctx, actor, true, func(tx pgx.Tx) error {
		var prior *string
		err := tx.QueryRow(ctx, `SELECT a.price_list_id FROM stores s
			LEFT JOIN store_price_list_assignments a ON (a.tenant_id,a.store_id)=(s.tenant_id,s.id)
			WHERE s.tenant_id=$1 AND s.id=$2 AND s.status='active'`, actor.TenantID, storeID).Scan(&prior)
		if err != nil {
			return noRows(err)
		}
		if equal(prior, listID) {
			return nil
		}
		if listID == nil {
			_, err = tx.Exec(ctx, "DELETE FROM store_price_list_assignments WHERE tenant_id=$1 AND store_id=$2", actor.TenantID, storeID)
		} else {
			tag, writeErr := tx.Exec(ctx, `INSERT INTO store_price_list_assignments(tenant_id,store_id,price_list_id)
				SELECT tenant_id,$2,id FROM price_lists WHERE tenant_id=$1 AND id=$3
				ON CONFLICT (tenant_id,store_id) DO UPDATE SET price_list_id=excluded.price_list_id`,
				actor.TenantID, storeID, *listID)
			if writeErr == nil && tag.RowsAffected() == 0 {
				return ErrNotFound
			}
			err = writeErr
		}
		if err != nil {
			return err
		}
		return audit(ctx, tx, actor, "assign_list", listID, &storeID, nil,
			map[string]*string{"listId": prior}, map[string]*string{"listId": listID})
	})
}

func (s Service) GetItem(ctx context.Context, actor auth.Identity, listID, variantID string) (Item, error) {
	if !validID(listID) || !validID(variantID) {
		return Item{}, ErrInvalid
	}
	var item Item
	err := s.manage(ctx, actor, false, func(tx pgx.Tx) error {
		var err error
		item, err = readItem(ctx, tx, actor.TenantID, listID, variantID)
		return err
	})
	return item, err
}

func (s Service) SetItem(ctx context.Context, actor auth.Identity, listID, variantID string, amount *string) (Item, error) {
	if !validID(listID) || !validID(variantID) {
		return Item{}, ErrInvalid
	}
	if amount != nil {
		cents, valid := money.Parse(*amount)
		if !valid {
			return Item{}, ErrInvalid
		}
		normalized := money.Format(cents)
		amount = &normalized
	}
	result := Item{Amount: amount}
	err := s.manage(ctx, actor, true, func(tx pgx.Tx) error {
		prior, err := readItem(ctx, tx, actor.TenantID, listID, variantID)
		if err != nil {
			return err
		}
		if equal(prior.Amount, amount) {
			return nil
		}
		if amount == nil {
			_, err = tx.Exec(ctx, "DELETE FROM price_list_items WHERE tenant_id=$1 AND price_list_id=$2 AND variant_id=$3", actor.TenantID, listID, variantID)
		} else {
			_, err = tx.Exec(ctx, `INSERT INTO price_list_items(tenant_id,price_list_id,variant_id,amount)
				VALUES ($1,$2,$3,$4::text::numeric)
				ON CONFLICT (tenant_id,price_list_id,variant_id) DO UPDATE SET amount=excluded.amount`,
				actor.TenantID, listID, variantID, *amount)
		}
		if err != nil {
			return err
		}
		return audit(ctx, tx, actor, "set_price", &listID, nil, &variantID, prior, result)
	})
	return result, err
}

func (s Service) Resolve(ctx context.Context, actor auth.Identity, storeID, variantID string) (Resolved, error) {
	return ResolvePrice(ctx, s.db, actor, storeID, variantID)
}

func ResolvePrice(ctx context.Context, db interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, actor auth.Identity, storeID, variantID string) (Resolved, error) {
	if !validID(storeID) || !validID(variantID) {
		return Resolved{}, ErrInvalid
	}
	var result Resolved
	err := db.QueryRow(ctx, `SELECT a.price_list_id,i.amount::text,t.currency_code,
		CASE WHEN a.price_list_id IS NULL THEN 'no_list'
		     WHEN i.amount IS NULL THEN 'no_price' ELSE 'configured' END
		FROM user_stores us
		JOIN stores s ON (s.tenant_id,s.id)=(us.tenant_id,us.store_id)
		JOIN users u ON (u.tenant_id,u.id)=(us.tenant_id,us.user_id)
		JOIN tenants t ON t.id=s.tenant_id
		JOIN product_variants v ON v.tenant_id=s.tenant_id AND v.id=$4
		JOIN products p ON (p.tenant_id,p.id)=(v.tenant_id,v.product_id)
		LEFT JOIN store_price_list_assignments a ON (a.tenant_id,a.store_id)=(s.tenant_id,s.id)
		LEFT JOIN price_list_items i ON (i.tenant_id,i.price_list_id,i.variant_id)=(a.tenant_id,a.price_list_id,v.id)
		WHERE us.tenant_id=$1 AND us.user_id=$2 AND us.store_id=$3
		  AND s.status='active' AND t.status='active' AND u.status='active' AND p.status='active' AND v.status='active'
		  AND EXISTS(SELECT 1 FROM user_roles ur JOIN role_permissions rp ON rp.role_code=ur.role_code
			WHERE (ur.tenant_id,ur.user_id)=(us.tenant_id,us.user_id) AND rp.permission_code='pricing:read')`,
		actor.TenantID, actor.UserID, storeID, variantID).Scan(&result.ListID, &result.Amount, &result.Currency, &result.Reason)
	return result, noRows(err)
}

func (s Service) manage(ctx context.Context, actor auth.Identity, writing bool, operation func(pgx.Tx) error) error {
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
		var allowed bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(
			SELECT 1 FROM user_roles ur JOIN role_permissions rp ON rp.role_code=ur.role_code
			JOIN users u ON (u.tenant_id,u.id)=(ur.tenant_id,ur.user_id)
			JOIN tenants t ON t.id=u.tenant_id
			WHERE ur.tenant_id=$1 AND ur.user_id=$2 AND rp.permission_code='pricing:manage'
			AND u.status='active' AND t.status='active')`, actor.TenantID, actor.UserID).Scan(&allowed); err != nil {
			return err
		}
		if !allowed {
			return auth.ErrForbidden
		}
		return operation(tx)
	})
}

func readItem(ctx context.Context, tx pgx.Tx, tenantID, listID, variantID string) (Item, error) {
	var item Item
	err := tx.QueryRow(ctx, `SELECT i.amount::text
		FROM price_lists l JOIN product_variants v ON v.tenant_id=l.tenant_id AND v.id=$3
		LEFT JOIN price_list_items i ON (i.tenant_id,i.price_list_id,i.variant_id)=(l.tenant_id,l.id,v.id)
		WHERE l.tenant_id=$1 AND l.id=$2`, tenantID, listID, variantID).Scan(&item.Amount)
	return item, noRows(err)
}

func audit(ctx context.Context, tx pgx.Tx, actor auth.Identity, action string, listID, storeID, variantID *string, before, after any) error {
	_, err := tx.Exec(ctx, `INSERT INTO price_admin_events(tenant_id,actor_id,action,list_id,store_id,variant_id,before_data,after_data)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, actor.TenantID, actor.UserID, action, listID, storeID, variantID, before, after)
	return err
}

func equal(a, b *string) bool { return a == nil && b == nil || a != nil && b != nil && *a == *b }
func noRows(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func validID(id string) bool {
	var uuid pgtype.UUID
	return len(id) == 36 && uuid.Scan(id) == nil && uuid.String() == id
}
