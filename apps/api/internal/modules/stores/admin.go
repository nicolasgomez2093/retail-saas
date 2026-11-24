package stores

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

var ErrNotFound = errors.New("sucursal inexistente")
var ErrCodeTaken = errors.New("el código ya está registrado")
var ErrInvalid = errors.New("verificá código, nombre, domicilio y estado de la sucursal")

type ManagedStore struct {
	Store
	Address string `json:"address"`
	Status  string `json:"status"`
}

type StoreChanges struct {
	Code    *string `json:"code"`
	Name    *string `json:"name"`
	Address *string `json:"address"`
	Status  *string `json:"status"`
}

type StoreInput struct {
	Code    string `json:"code"`
	Name    string `json:"name"`
	Address string `json:"address"`
}

const fields = "id,code,name,address,status"

func requireManagement(ctx context.Context, tx pgx.Tx, actor auth.Identity) error {
	var allowed bool
	err := tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM user_roles ur
			JOIN role_permissions rp ON rp.role_code=ur.role_code
			JOIN users u ON (u.tenant_id,u.id)=(ur.tenant_id,ur.user_id)
			JOIN tenants t ON t.id=u.tenant_id
			WHERE ur.tenant_id=$1 AND ur.user_id=$2 AND rp.permission_code='stores:manage'
			  AND u.status='active' AND t.status='active'
		)`, actor.TenantID, actor.UserID).Scan(&allowed)
	if err == nil && !allowed {
		return auth.ErrForbidden
	}
	return err
}

func ListManaged(ctx context.Context, db *pgxpool.Pool, actor auth.Identity) ([]ManagedStore, error) {
	var list []ManagedStore
	err := pgx.BeginTxFunc(ctx, db, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		if err := requireManagement(ctx, tx, actor); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, "SELECT "+fields+" FROM stores WHERE tenant_id=$1 ORDER BY code", actor.TenantID)
		if err != nil {
			return err
		}
		list, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (ManagedStore, error) { return readStore(row) })
		return err
	})
	return list, err
}

func Create(ctx context.Context, db *pgxpool.Pool, actor auth.Identity, input StoreInput) (ManagedStore, error) {
	code, name, address := strings.TrimSpace(input.Code), strings.TrimSpace(input.Name), strings.TrimSpace(input.Address)
	if !validField(code, 40, true) || !validField(name, 160, true) || !validField(address, 300, false) {
		return ManagedStore{}, ErrInvalid
	}
	var store ManagedStore
	err := write(ctx, db, actor, func(tx pgx.Tx) error {
		var err error
		store, err = readStore(tx.QueryRow(ctx, `INSERT INTO stores(tenant_id,code,name,address)
			VALUES ($1,$2,$3,$4) RETURNING `+fields, actor.TenantID, code, name, address))
		if err != nil {
			return storeError(err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO user_stores(tenant_id,user_id,store_id) VALUES ($1,$2,$3)`,
			actor.TenantID, actor.UserID, store.ID); err != nil {
			return err
		}
		return auditStore(ctx, tx, actor, "create", nil, store)
	})
	return store, err
}

func Update(ctx context.Context, db *pgxpool.Pool, actor auth.Identity, id string, input StoreChanges) (ManagedStore, error) {
	var uuid pgtype.UUID
	if len(id) != 36 || uuid.Scan(id) != nil || uuid.String() != id ||
		(input.Code == nil && input.Name == nil && input.Address == nil && input.Status == nil) {
		return ManagedStore{}, ErrInvalid
	}
	var store ManagedStore
	err := write(ctx, db, actor, func(tx pgx.Tx) error {
		before, err := readStore(tx.QueryRow(ctx, "SELECT "+fields+" FROM stores WHERE tenant_id=$1 AND id=$2", actor.TenantID, uuid))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		store = before
		for _, field := range []struct{ target, value *string }{
			{&store.Code, input.Code}, {&store.Name, input.Name},
			{&store.Address, input.Address}, {&store.Status, input.Status},
		} {
			if field.value != nil {
				*field.target = strings.TrimSpace(*field.value)
			}
		}
		if !validField(store.Code, 40, true) || !validField(store.Name, 160, true) ||
			!validField(store.Address, 300, false) || (store.Status != "active" && store.Status != "inactive") {
			return ErrInvalid
		}
		if store == before {
			return nil
		}
		if _, err := tx.Exec(ctx, "UPDATE stores SET code=$3,name=$4,address=$5,status=$6 WHERE tenant_id=$1 AND id=$2",
			actor.TenantID, uuid, store.Code, store.Name, store.Address, store.Status); err != nil {
			return storeError(err)
		}
		return auditStore(ctx, tx, actor, "update", &before, store)
	})
	return store, err
}

func write(ctx context.Context, db *pgxpool.Pool, actor auth.Identity, operation func(pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		if err := tenancy.LockActive(ctx, tx, actor.TenantID); err != nil {
			return err
		}
		if err := requireManagement(ctx, tx, actor); err != nil {
			return err
		}
		return operation(tx)
	})
}

func readStore(row pgx.Row) (ManagedStore, error) {
	var result ManagedStore
	err := row.Scan(&result.ID, &result.Code, &result.Name, &result.Address, &result.Status)
	return result, err
}

func validField(value string, max int, required bool) bool {
	return (!required || value != "") && !strings.ContainsRune(value, 0) && utf8.RuneCountInString(value) <= max
}

func storeError(err error) error {
	var conflict *pgconn.PgError
	if errors.As(err, &conflict) && conflict.Code == "23505" && conflict.ConstraintName == "stores_tenant_id_code_key" {
		return ErrCodeTaken
	}
	return err
}

func auditStore(ctx context.Context, tx pgx.Tx, actor auth.Identity, action string, before *ManagedStore, after ManagedStore) error {
	_, err := tx.Exec(ctx, `INSERT INTO store_admin_events(tenant_id,actor_id,store_id,action,before_data,after_data)
		VALUES ($1,$2,$3,$4,$5,$6)`, actor.TenantID, actor.UserID, after.ID, action, before, after)
	return err
}
