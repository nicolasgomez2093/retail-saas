package users

import (
	"context"
	"errors"
	"slices"
	"strings"
	"unicode/utf8"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/stores"
	"retail-platform/api/internal/modules/tenancy"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrNotFound = errors.New("usuario inexistente")
var ErrEmailTaken = errors.New("el correo ya está registrado")
var ErrInvalid = errors.New("verificá nombre, correo, contraseña, rol y sucursales")

type User struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Email    string   `json:"email"`
	Status   string   `json:"status"`
	Roles    []string `json:"roles"`
	StoreIDs []string `json:"storeIds"`
}

type Directory struct {
	Items          []User         `json:"items"`
	Stores         []stores.Store `json:"stores"`
	Roles          []string       `json:"roles"`
	CanChangeRoles bool           `json:"canChangeRoles"`
}

type CreateInput struct {
	Name     string   `json:"name"`
	Email    string   `json:"email"`
	Password string   `json:"password"`
	Role     string   `json:"role"`
	StoreIDs []string `json:"storeIds"`
}

type UpdateInput struct {
	Name     *string   `json:"name"`
	Status   *string   `json:"status"`
	StoreIDs *[]string `json:"storeIds"`
	Role     *string   `json:"role"`
}

type Service struct {
	db        *pgxpool.Pool
	hashSlots chan struct{}
}

func New(db *pgxpool.Pool) *Service {
	return &Service{db: db, hashSlots: make(chan struct{}, 2)}
}

type scope struct {
	tenantWide bool
	stores     []stores.Store
	roles      []string
}

func loadScope(ctx context.Context, tx pgx.Tx, actor auth.Identity) (scope, error) {
	var allowed scope
	var assigned bool
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(bool_or(rp.permission_code='users:manage_tenant'),false),
		       COALESCE(bool_or(rp.permission_code='users:manage_assigned'),false)
		FROM user_roles ur
		JOIN role_permissions rp ON rp.role_code=ur.role_code
		JOIN users u ON (u.tenant_id,u.id)=(ur.tenant_id,ur.user_id)
		JOIN tenants t ON t.id=u.tenant_id
		WHERE ur.tenant_id=$1 AND ur.user_id=$2 AND u.status='active' AND t.status='active'`,
		actor.TenantID, actor.UserID).Scan(&allowed.tenantWide, &assigned)
	if err != nil {
		return scope{}, err
	}
	if !allowed.tenantWide && !assigned {
		return scope{}, auth.ErrForbidden
	}
	allowed.roles = []string{"CASHIER", "INVENTORY_OPERATOR"}
	if allowed.tenantWide {
		allowed.roles = append(allowed.roles, "STORE_MANAGER")
	}
	rows, err := tx.Query(ctx, `
		SELECT s.id,s.code,s.name FROM stores s
		WHERE s.tenant_id=$1 AND s.status='active' AND
		($3 OR EXISTS (
			SELECT 1 FROM user_stores us WHERE (us.tenant_id,us.store_id)=(s.tenant_id,s.id) AND us.user_id=$2))
		ORDER BY s.code`, actor.TenantID, actor.UserID, allowed.tenantWide)
	if err != nil {
		return scope{}, err
	}
	allowed.stores, err = pgx.CollectRows(rows, pgx.RowToStructByPos[stores.Store])
	return allowed, err
}

const scopedUsers = `
	SELECT u.id,u.name,u.email,u.status,assignments.roles,assignments.stores
	FROM users u
	CROSS JOIN LATERAL (
		SELECT ARRAY(SELECT role_code FROM user_roles WHERE (tenant_id,user_id)=(u.tenant_id,u.id) ORDER BY role_code) AS roles,
		       ARRAY(SELECT store_id::text FROM user_stores WHERE (tenant_id,user_id)=(u.tenant_id,u.id) ORDER BY store_id) AS stores
	) assignments
	WHERE u.tenant_id=$1 AND u.id<>$2 AND (NULLIF($3,'') IS NULL OR u.id=NULLIF($3,'')::uuid)
	  AND NOT ('OWNER'=ANY(assignments.roles))
	  AND ($4 OR (cardinality(assignments.roles)>0 AND assignments.roles <@ $5
	               AND cardinality(assignments.stores)>0 AND assignments.stores <@ $6))
	ORDER BY u.name,u.id`

func list(ctx context.Context, tx pgx.Tx, actor auth.Identity, access scope, id string) ([]User, error) {
	rows, err := tx.Query(ctx, scopedUsers, actor.TenantID, actor.UserID, id, access.tenantWide, access.roles, access.storeIDs())
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[User])
}

func (s *Service) List(ctx context.Context, actor auth.Identity) (Directory, error) {
	var directory Directory
	err := pgx.BeginTxFunc(ctx, s.db, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		access, err := loadScope(ctx, tx, actor)
		if err != nil {
			return err
		}
		directory = Directory{Stores: access.stores, Roles: access.roles, CanChangeRoles: access.tenantWide}
		directory.Items, err = list(ctx, tx, actor, access, "")
		return err
	})
	return directory, err
}

func (s *Service) Create(ctx context.Context, actor auth.Identity, input CreateInput) (User, error) {
	name := strings.TrimSpace(input.Name)
	email, valid := auth.NormalizeEmail(input.Email)
	if !valid || !validName(name) || len(input.Password) < 12 || len(input.Password) > 1024 || !validStores(input.StoreIDs) {
		return User{}, ErrInvalid
	}
	return s.write(ctx, actor, func(tx pgx.Tx, access scope) (User, error) {
		if !slices.Contains(access.roles, input.Role) || !access.includesStores(input.StoreIDs) {
			return User{}, auth.ErrForbidden
		}
		select {
		case s.hashSlots <- struct{}{}:
			defer func() { <-s.hashSlots }()
		default:
			return User{}, auth.ErrBusy
		}
		hash, err := auth.HashPassword(input.Password)
		if err != nil {
			return User{}, err
		}
		user := User{Name: name, Email: email, Status: "active", Roles: []string{input.Role}, StoreIDs: input.StoreIDs}
		err = tx.QueryRow(ctx, `INSERT INTO users(tenant_id,email,name,password_hash)
			VALUES ($1,$2,$3,$4) RETURNING id`, actor.TenantID, email, name, hash).Scan(&user.ID)
		var conflict *pgconn.PgError
		if errors.As(err, &conflict) && conflict.ConstraintName == "users_tenant_id_email_key" {
			return User{}, ErrEmailTaken
		}
		if err != nil {
			return User{}, err
		}
		if _, err := tx.Exec(ctx, "INSERT INTO user_roles(tenant_id,user_id,role_code) VALUES ($1,$2,$3)", actor.TenantID, user.ID, input.Role); err != nil {
			return User{}, err
		}
		if err := replaceStores(ctx, tx, actor.TenantID, user); err != nil {
			return User{}, err
		}
		return user, audit(ctx, tx, actor, "create", nil, user)
	})
}

func (s *Service) Update(ctx context.Context, actor auth.Identity, id string, input UpdateInput) (User, error) {
	if !validID(id) || input.Name == nil && input.Status == nil && input.StoreIDs == nil && input.Role == nil {
		return User{}, ErrInvalid
	}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if !validName(name) {
			return User{}, ErrInvalid
		}
		input.Name = &name
	}
	if input.Status != nil && *input.Status != "active" && *input.Status != "inactive" {
		return User{}, ErrInvalid
	}
	if input.StoreIDs != nil && !validStores(*input.StoreIDs) {
		return User{}, ErrInvalid
	}
	return s.write(ctx, actor, func(tx pgx.Tx, access scope) (User, error) {
		if input.Role != nil && (!access.tenantWide || !slices.Contains(access.roles, *input.Role)) {
			return User{}, auth.ErrForbidden
		}
		results, err := list(ctx, tx, actor, access, id)
		if err != nil {
			return User{}, err
		}
		if len(results) == 0 {
			return User{}, ErrNotFound
		}
		before := results[0]
		user := before
		if input.Name != nil {
			user.Name = *input.Name
		}
		if input.Status != nil {
			user.Status = *input.Status
		}
		if input.StoreIDs != nil {
			if !access.includesStores(*input.StoreIDs) {
				return User{}, auth.ErrForbidden
			}
			user.StoreIDs = *input.StoreIDs
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET name=$3,status=$4 WHERE tenant_id=$1 AND id=$2`,
			actor.TenantID, user.ID, user.Name, user.Status); err != nil {
			return User{}, err
		}
		roleChanged := input.Role != nil && !slices.Equal(user.Roles, []string{*input.Role})
		if roleChanged {
			user.Roles = []string{*input.Role}
			if _, err := tx.Exec(ctx, "DELETE FROM user_roles WHERE tenant_id=$1 AND user_id=$2", actor.TenantID, user.ID); err != nil {
				return User{}, err
			}
			if _, err := tx.Exec(ctx, "INSERT INTO user_roles(tenant_id,user_id,role_code) VALUES ($1,$2,$3)", actor.TenantID, user.ID, *input.Role); err != nil {
				return User{}, err
			}
		}
		if user.Status == "inactive" || roleChanged {
			if _, err := tx.Exec(ctx, "DELETE FROM sessions WHERE tenant_id=$1 AND user_id=$2", actor.TenantID, user.ID); err != nil {
				return User{}, err
			}
		}
		if input.StoreIDs != nil {
			if err := replaceStores(ctx, tx, actor.TenantID, user); err != nil {
				return User{}, err
			}
		}
		return user, audit(ctx, tx, actor, "update", &before, user)
	})
}

func (s *Service) write(ctx context.Context, actor auth.Identity, operation func(pgx.Tx, scope) (User, error)) (User, error) {
	var user User
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		// Serializa cambios de cuentas y asignaciones del mismo comercio.
		if err := tenancy.LockActive(ctx, tx, actor.TenantID); err != nil {
			return err
		}
		access, err := loadScope(ctx, tx, actor)
		if err != nil {
			return err
		}
		user, err = operation(tx, access)
		return err
	})
	return user, err
}

func replaceStores(ctx context.Context, tx pgx.Tx, tenantID string, user User) error {
	if _, err := tx.Exec(ctx, "DELETE FROM user_stores WHERE tenant_id=$1 AND user_id=$2", tenantID, user.ID); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO user_stores(tenant_id,user_id,store_id)
		SELECT $1,$2,store_id::uuid FROM unnest($3::text[]) AS ids(store_id)`, tenantID, user.ID, user.StoreIDs)
	return err
}

func audit(ctx context.Context, tx pgx.Tx, actor auth.Identity, action string, before *User, after User) error {
	_, err := tx.Exec(ctx, `INSERT INTO user_admin_events(tenant_id,actor_id,user_id,action,before_data,after_data)
		VALUES ($1,$2,$3,$4,$5,$6)`, actor.TenantID, actor.UserID, after.ID, action, before, after)
	return err
}

func (s scope) storeIDs() []string {
	ids := make([]string, 0, len(s.stores))
	for _, store := range s.stores {
		ids = append(ids, store.ID)
	}
	return ids
}

func (s scope) includesStores(ids []string) bool {
	allowed := s.storeIDs()
	for _, id := range ids {
		if !slices.Contains(allowed, id) {
			return false
		}
	}
	return true
}

func validID(id string) bool {
	var parsed pgtype.UUID
	return len(id) == 36 && parsed.Scan(id) == nil && parsed.String() == id
}

func validStores(ids []string) bool {
	if len(ids) == 0 || len(ids) > 100 {
		return false
	}
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		if _, duplicate := seen[id]; duplicate || !validID(id) {
			return false
		}
		seen[id] = struct{}{}
	}
	return true
}

func validName(name string) bool {
	return name != "" && !strings.ContainsRune(name, 0) && utf8.RuneCountInString(name) <= 160
}
