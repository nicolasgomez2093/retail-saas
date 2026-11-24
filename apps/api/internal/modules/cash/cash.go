package cash

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/tenancy"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrInvalid = errors.New("verificá la sucursal, la caja y los datos ingresados")
var ErrNotFound = errors.New("sucursal, caja o sesión inexistente")
var ErrRegisterCode = errors.New("ya hay una caja con ese código en la sucursal")
var ErrOccupied = errors.New("la caja ya tiene una sesión abierta")
var ErrSessionState = errors.New("la sesión ya está cerrada o tiene otro cierre confirmado")
var ErrIdempotency = errors.New("la clave de idempotencia ya se usó con otros datos")
var ErrCashBalance = errors.New("el movimiento supera el efectivo esperado o el límite de importe")

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

type Register struct {
	ID            string  `json:"id"`
	Code          string  `json:"code"`
	Name          string  `json:"name"`
	Occupied      bool    `json:"occupied"`
	OpenSessionID *string `json:"openSessionId"`
}

type RegisterInput struct {
	StoreID string `json:"storeId"`
	Code    string `json:"code"`
	Name    string `json:"name"`
}

type Registers struct {
	Items       []Register `json:"items"`
	CanManage   bool       `json:"canManage"`
	CanDiscount bool       `json:"canDiscount"`
	Currency    string     `json:"currency"`
	Timezone    string     `json:"timezone"`
}

type Service struct{ db *pgxpool.Pool }

func New(db *pgxpool.Pool) Service { return Service{db: db} }

func (s Service) Registers(ctx context.Context, actor auth.Identity, storeID string) (Registers, error) {
	var result Registers
	err := s.withStore(ctx, actor, storeID, false, func(tx pgx.Tx, scope access) error {
		rows, err := tx.Query(ctx, `SELECT r.id,r.code,r.name,cs.id IS NOT NULL,
			CASE WHEN $4 OR cs.opened_by=$3 THEN cs.id ELSE NULL END
			FROM cash_registers r LEFT JOIN cash_sessions cs ON (cs.tenant_id,cs.cash_register_id)=(r.tenant_id,r.id) AND cs.status='OPEN'
			WHERE r.tenant_id=$1 AND r.store_id=$2 AND r.status='active' ORDER BY r.code,r.id`,
			actor.TenantID, storeID, actor.UserID, scope.supervise)
		if err != nil {
			return err
		}
		result.Items, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Register])
		if err != nil {
			return err
		}
		result.CanManage, result.Currency, result.Timezone = scope.manage, scope.currency, scope.timezone
		result.CanDiscount, err = CanDiscount(ctx, tx, actor)
		return err
	})
	return result, err
}

// CanDiscount checks the role within an already authorized store operation.
func CanDiscount(ctx context.Context, tx pgx.Tx, actor auth.Identity) (bool, error) {
	var allowed bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (
        SELECT 1 FROM user_roles WHERE tenant_id=$1 AND user_id=$2
        AND role_code IN ('OWNER','STORE_MANAGER')
    )`, actor.TenantID, actor.UserID).Scan(&allowed)
	return allowed, err
}

func (s Service) CreateRegister(ctx context.Context, actor auth.Identity, input RegisterInput) (Register, error) {
	input.Code, input.Name = strings.TrimSpace(input.Code), strings.TrimSpace(input.Name)
	if !validText(input.Code, 40) || !validText(input.Name, 160) {
		return Register{}, ErrInvalid
	}
	result := Register{Code: input.Code, Name: input.Name}
	err := s.withStore(ctx, actor, input.StoreID, true, func(tx pgx.Tx, scope access) error {
		if !scope.manage {
			return auth.ErrForbidden
		}
		err := tx.QueryRow(ctx, `INSERT INTO cash_registers(tenant_id,store_id,code,name)
			VALUES ($1,$2,$3,$4) RETURNING id`, actor.TenantID, input.StoreID, input.Code, input.Name).Scan(&result.ID)
		var conflict *pgconn.PgError
		if errors.As(err, &conflict) && conflict.ConstraintName == "cash_registers_store_code" {
			return ErrRegisterCode
		}
		return err
	})
	return result, err
}

type access struct {
	supervise, manage  bool
	currency, timezone string
}

func (s Service) withStore(ctx context.Context, actor auth.Identity, storeID string, writing bool, operation func(pgx.Tx, access) error) error {
	if !validID(storeID) {
		return ErrInvalid
	}
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
		var operate bool
		var scope access
		err := tx.QueryRow(ctx, `SELECT
			coalesce(bool_or(rp.permission_code='cash:operate'),false),
			coalesce(bool_or(rp.permission_code='cash:supervise'),false),
			coalesce(bool_or(rp.permission_code='cash:manage_registers'),false),t.currency_code,t.timezone
			FROM user_stores us JOIN stores s ON (s.tenant_id,s.id)=(us.tenant_id,us.store_id)
			JOIN users u ON (u.tenant_id,u.id)=(us.tenant_id,us.user_id) JOIN tenants t ON t.id=u.tenant_id
			LEFT JOIN user_roles ur ON (ur.tenant_id,ur.user_id)=(us.tenant_id,us.user_id)
			LEFT JOIN role_permissions rp ON rp.role_code=ur.role_code
			WHERE us.tenant_id=$1 AND us.user_id=$2 AND us.store_id=$3 AND s.status='active' AND t.status='active' AND u.status='active'
			GROUP BY us.store_id,t.currency_code,t.timezone`, actor.TenantID, actor.UserID, storeID).Scan(
			&operate, &scope.supervise, &scope.manage, &scope.currency, &scope.timezone)
		if err != nil {
			return notFound(err)
		}
		if !operate {
			return auth.ErrForbidden
		}
		return operation(tx, scope)
	})
}

func requireRegister(ctx context.Context, tx pgx.Tx, tenantID, storeID, id string) error {
	var found string
	return notFound(tx.QueryRow(ctx, "SELECT id FROM cash_registers WHERE tenant_id=$1 AND store_id=$2 AND id=$3 AND status='active'",
		tenantID, storeID, id).Scan(&found))
}

func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func validID(id string) bool {
	var uuid pgtype.UUID
	return len(id) == 36 && uuid.Scan(id) == nil && uuid.String() == id
}

func validText(value string, max int) bool {
	return value != "" && !strings.ContainsRune(value, 0) && utf8.ValidString(value) && utf8.RuneCountInString(value) <= max
}
