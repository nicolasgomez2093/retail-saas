package suppliers

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/tenancy"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrInvalid = errors.New("verificá los datos del proveedor")
var ErrNotFound = errors.New("proveedor inexistente")

// ErrDuplicate rejects a second supplier with the same name or tax id, which
// also absorbs an accidental double submission of the same form.
type ErrDuplicate struct {
	Field string `json:"field"`
	ID    string `json:"id"`
	Name  string `json:"name"`
}

func (e ErrDuplicate) Error() string {
	if e.Field == "taxId" {
		return "ya existe un proveedor con esa identificación fiscal: " + e.Name
	}
	return "ya existe un proveedor con ese nombre"
}

const fields = "id,name,email,phone,status,tax_id,contact_name"

type Supplier struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Email       string `json:"email"`
	Phone       string `json:"phone"`
	Status      string `json:"status"`
	TaxID       string `json:"taxId"`
	ContactName string `json:"contactName"`
}

type Input struct {
	Name        string `json:"name"`
	Email       string `json:"email"`
	Phone       string `json:"phone"`
	TaxID       string `json:"taxId"`
	ContactName string `json:"contactName"`
	// AllowDuplicateName confirms a homonym on purpose. Without it a second
	// supplier with the same name is rejected, which also stops double submits.
	AllowDuplicateName bool `json:"allowDuplicateName"`
}

type Changes struct {
	Name               *string `json:"name"`
	Email              *string `json:"email"`
	Phone              *string `json:"phone"`
	Status             *string `json:"status"`
	TaxID              *string `json:"taxId"`
	ContactName        *string `json:"contactName"`
	AllowDuplicateName bool    `json:"allowDuplicateName"`
}

type Page struct {
	Items     []Supplier `json:"items"`
	HasMore   bool       `json:"hasMore"`
	CanManage bool       `json:"canManage"`
}

type Service struct{ db *pgxpool.Pool }

func New(db *pgxpool.Pool) Service { return Service{db: db} }

func (s Service) List(ctx context.Context, actor auth.Identity, query string, limit, offset int) (Page, error) {
	query = strings.TrimSpace(query)
	if !validText(query, 160) || limit < 1 || limit > 100 || offset < 0 || offset > 100000 {
		return Page{}, ErrInvalid
	}
	var page Page
	err := s.withAccess(ctx, actor, false, func(tx pgx.Tx, manage bool) error {
		rows, err := tx.Query(ctx, "SELECT "+fields+` FROM suppliers WHERE tenant_id=$1
			AND ($2 OR status='active')
			AND (strpos(lower(name),lower($3))>0 OR strpos(lower(email),lower($3))>0 OR strpos(phone,$3)>0
			     OR strpos(lower(tax_id),lower($3))>0 OR strpos(lower(contact_name),lower($3))>0)
			ORDER BY lower(name),id LIMIT $4 OFFSET $5`, actor.TenantID, manage, query, limit+1, offset)
		if err != nil {
			return err
		}
		page.Items, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Supplier])
		page.CanManage = manage
		if len(page.Items) > limit {
			page.Items = page.Items[:limit]
			page.HasMore = offset+limit <= 100000
		}
		return err
	})
	return page, err
}

func (s Service) Create(ctx context.Context, actor auth.Identity, data Input) (Supplier, error) {
	if !normalize(Changes{Name: &data.Name, Email: &data.Email, Phone: &data.Phone, TaxID: &data.TaxID, ContactName: &data.ContactName}) {
		return Supplier{}, ErrInvalid
	}
	var supplier Supplier
	err := s.withAccess(ctx, actor, true, func(tx pgx.Tx, _ bool) error {
		if err := checkDuplicate(ctx, tx, actor.TenantID, "", data.AllowDuplicateName, &data.Name, &data.TaxID); err != nil {
			return err
		}
		var err error
		supplier, err = scanSupplier(tx.QueryRow(ctx, `INSERT INTO suppliers(tenant_id,name,email,phone,tax_id,contact_name,created_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7) RETURNING `+fields, actor.TenantID, data.Name, data.Email, data.Phone, data.TaxID, data.ContactName, actor.UserID))
		return err
	})
	return supplier, err
}

func (s Service) Update(ctx context.Context, actor auth.Identity, id string, changes Changes) (Supplier, error) {
	var uuid pgtype.UUID
	if len(id) != 36 || uuid.Scan(id) != nil || uuid.String() != id ||
		(changes == Changes{AllowDuplicateName: changes.AllowDuplicateName}) || !normalize(changes) {
		return Supplier{}, ErrInvalid
	}
	var supplier Supplier
	err := s.withAccess(ctx, actor, true, func(tx pgx.Tx, _ bool) error {
		if err := checkDuplicate(ctx, tx, actor.TenantID, id, changes.AllowDuplicateName, changes.Name, changes.TaxID); err != nil {
			return err
		}
		var err error
		// Orders keep their own supplier_name snapshot, so renaming never rewrites history.
		supplier, err = scanSupplier(tx.QueryRow(ctx, `UPDATE suppliers SET name=COALESCE($3,name),email=COALESCE($4,email),
			phone=COALESCE($5,phone),status=COALESCE($6,status),tax_id=COALESCE($7,tax_id),contact_name=COALESCE($8,contact_name)
			WHERE tenant_id=$1 AND id=$2 RETURNING `+fields,
			actor.TenantID, uuid, changes.Name, changes.Email, changes.Phone, changes.Status, changes.TaxID, changes.ContactName))
		return err
	})
	return supplier, err
}

func (s Service) withAccess(ctx context.Context, actor auth.Identity, writing bool, operation func(pgx.Tx, bool) error) error {
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
		var read, manage bool
		err := tx.QueryRow(ctx, `SELECT
			COALESCE(bool_or(rp.permission_code='suppliers:read'),false) AND EXISTS (
			    SELECT 1 FROM user_stores us JOIN stores s ON (s.tenant_id,s.id)=(us.tenant_id,us.store_id)
			    WHERE us.tenant_id=$1 AND us.user_id=$2 AND s.status='active'
			), COALESCE(bool_or(rp.permission_code='suppliers:manage'),false)
			FROM user_roles ur JOIN role_permissions rp ON rp.role_code=ur.role_code
			JOIN users u ON (u.tenant_id,u.id)=(ur.tenant_id,ur.user_id)
			JOIN tenants t ON t.id=u.tenant_id
			WHERE ur.tenant_id=$1 AND ur.user_id=$2 AND u.status='active' AND t.status='active'`,
			actor.TenantID, actor.UserID).Scan(&read, &manage)
		if err != nil {
			return err
		}
		if !manage && (writing || !read) {
			return auth.ErrForbidden
		}
		return operation(tx, manage)
	})
}

func normalize(changes Changes) bool {
	if changes.Name != nil {
		*changes.Name = strings.TrimSpace(*changes.Name)
		if *changes.Name == "" || !validText(*changes.Name, 160) {
			return false
		}
	}
	if changes.Email != nil {
		*changes.Email = strings.TrimSpace(*changes.Email)
		if *changes.Email != "" {
			email, valid := auth.NormalizeEmail(*changes.Email)
			if !valid {
				return false
			}
			*changes.Email = email
		}
	}
	if changes.Phone != nil {
		*changes.Phone = strings.TrimSpace(*changes.Phone)
		if !validText(*changes.Phone, 40) {
			return false
		}
	}
	if changes.TaxID != nil {
		*changes.TaxID = strings.ToUpper(strings.TrimSpace(*changes.TaxID))
		if !validText(*changes.TaxID, 20) {
			return false
		}
	}
	if changes.ContactName != nil {
		*changes.ContactName = strings.TrimSpace(*changes.ContactName)
		if !validText(*changes.ContactName, 120) {
			return false
		}
	}
	return changes.Status == nil || *changes.Status == "active" || *changes.Status == "inactive"
}

// checkDuplicate runs under the tenant write lock, so two concurrent creations
// of the same supplier cannot both pass it.
func checkDuplicate(ctx context.Context, tx pgx.Tx, tenantID, exceptID string, allowName bool, name, taxID *string) error {
	if name != nil && !allowName {
		var found ErrDuplicate
		err := tx.QueryRow(ctx, `SELECT id,name FROM suppliers WHERE tenant_id=$1 AND lower(btrim(name))=lower($2)
			AND ($3='' OR id<>NULLIF($3,'')::uuid) LIMIT 1`, tenantID, *name, exceptID).Scan(&found.ID, &found.Name)
		if err == nil {
			found.Field = "name"
			return found
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	if taxID != nil && *taxID != "" {
		var found ErrDuplicate
		err := tx.QueryRow(ctx, `SELECT id,name FROM suppliers WHERE tenant_id=$1 AND tax_id=$2
			AND ($3='' OR id<>NULLIF($3,'')::uuid) LIMIT 1`, tenantID, *taxID, exceptID).Scan(&found.ID, &found.Name)
		if err == nil {
			found.Field = "taxId"
			return found
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
	}
	return nil
}

func validText(value string, limit int) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= limit && strings.IndexFunc(value, unicode.IsControl) == -1
}

func scanSupplier(row pgx.Row) (Supplier, error) {
	var result Supplier
	err := row.Scan(&result.ID, &result.Name, &result.Email, &result.Phone, &result.Status, &result.TaxID, &result.ContactName)
	if errors.Is(err, pgx.ErrNoRows) {
		return Supplier{}, ErrNotFound
	}
	return result, err
}
