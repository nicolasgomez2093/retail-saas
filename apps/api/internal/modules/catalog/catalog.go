package catalog

import (
	"context"
	"encoding/json"
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

var ErrInvalid = errors.New("verificá los datos del catálogo")
var ErrNotFound = errors.New("producto o código inexistente")
var ErrSKU = errors.New("el SKU ya está registrado en el comercio")
var ErrBarcode = errors.New("el código de barras ya está registrado en el comercio")

var ErrRevisionConflict = errors.New("otra persona modificó este registro")
var ErrSensitiveHistory = errors.New("la variante tiene operaciones pendientes o referencias cuyo cierre no se puede confirmar")

type FieldError struct {
	Field   string
	Message string
}

func (e *FieldError) Error() string { return e.Message }

func requireRevision(expected, current int64) error {
	if expected < 1 {
		return &FieldError{Field: "expectedRevision", Message: "Indicá la revisión que estás editando."}
	}
	if expected != current {
		return ErrRevisionConflict
	}
	return nil
}

var barcodePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,79}$`)

const productFields = "id,name,description,status,category_id,brand_id,revision"
const variantFields = `v.id,v.product_id,v.sku,v.size,v.color,v.status,v.revision,v.size_id,v.color_id,
	ARRAY(SELECT code FROM barcodes WHERE (tenant_id,variant_id)=(v.tenant_id,v.id)
	      ORDER BY is_primary DESC,code),
	(SELECT code FROM barcodes WHERE (tenant_id,variant_id)=(v.tenant_id,v.id) AND is_internal),v.options`

type Product struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Status      string  `json:"status"`
	CategoryID  *string `json:"categoryId"`
	BrandID     *string `json:"brandId"`
	Revision    int64   `json:"revision"`
}

type ProductInput struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	CategoryID  *string `json:"categoryId"`
	BrandID     *string `json:"brandId"`
}

type ProductChanges struct {
	ExpectedRevision int64           `json:"expectedRevision"`
	Name             *string         `json:"name"`
	Description      *string         `json:"description"`
	Status           *string         `json:"status"`
	CategoryID       json.RawMessage `json:"categoryId"`
	BrandID          json.RawMessage `json:"brandId"`
}

type Detail struct {
	Product  Product   `json:"product"`
	Variants []Variant `json:"variants"`
	Images   []Photo   `json:"images"`
}

type Match struct {
	Product Product `json:"product"`
	Variant Variant `json:"variant"`
}

type Page struct {
	Items     []Product `json:"items"`
	HasMore   bool      `json:"hasMore"`
	CanManage bool      `json:"canManage"`
	Previews  []Preview `json:"previews,omitempty"`
}

type Service struct {
	db         *pgxpool.Pool
	imageSlots chan struct{}
}

func New(db *pgxpool.Pool) Service { return Service{db: db, imageSlots: make(chan struct{}, 2)} }

func (s Service) List(ctx context.Context, actor auth.Identity, query, categoryID, brandID, storeID string, limit, offset int) (Page, error) {
	query = strings.TrimSpace(query)
	category, categoryOK := parseID(categoryID)
	brand, brandOK := parseID(brandID)
	store, storeOK := parseID(storeID)
	if (storeID != "" && !storeOK) || (categoryID != "" && !categoryOK) || (brandID != "" && !brandOK) || !validText(query, 160, false) || limit < 1 || limit > 100 || offset < 0 || offset > 100000 {
		return Page{}, ErrInvalid
	}
	var page Page
	err := s.withAccess(ctx, actor, false, func(tx pgx.Tx, manage bool) error {
		rows, err := tx.Query(ctx, "SELECT "+productFields+` FROM products p
			WHERE tenant_id=$1 AND ($3 OR status='active')
			AND ($6::uuid IS NULL OR category_id=$6) AND ($7::uuid IS NULL OR brand_id=$7)
			AND (
				$2='' OR strpos(lower(name),lower($2))>0 OR EXISTS (
					SELECT 1 FROM product_variants v WHERE (v.tenant_id,v.product_id)=(p.tenant_id,p.id)
					AND ($3 OR v.status='active') AND strpos(lower(v.sku),lower($2))>0))
			ORDER BY name,id LIMIT $4 OFFSET $5`, actor.TenantID, query, manage, limit+1, offset, category, brand)
		if err != nil {
			return err
		}
		page.Items, err = pgx.CollectRows(rows, pgx.RowToStructByPos[Product])
		page.CanManage = manage
		if len(page.Items) > limit {
			page.Items = page.Items[:limit]
			page.HasMore = offset+limit <= 100000
		}
		if err == nil && storeID != "" && len(page.Items) > 0 {
			page.Previews, err = listPreviews(ctx, tx, actor, store, page.Items)
		}
		return err
	})
	return page, err
}

func (s Service) Detail(ctx context.Context, actor auth.Identity, id string) (Detail, error) {
	uuid, valid := parseID(id)
	if !valid {
		return Detail{}, ErrInvalid
	}
	var detail Detail
	err := s.withAccess(ctx, actor, false, func(tx pgx.Tx, manage bool) error {
		err := readProduct(tx.QueryRow(ctx, "SELECT "+productFields+" FROM products WHERE tenant_id=$1 AND id=$2 AND ($3 OR status='active')",
			actor.TenantID, uuid, manage), &detail.Product)
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, "SELECT "+variantFields+` FROM product_variants v
			WHERE v.tenant_id=$1 AND v.product_id=$2 AND ($3 OR v.status='active') ORDER BY v.sku`,
			actor.TenantID, uuid, manage)
		if err != nil {
			return err
		}
		detail.Variants, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Variant, error) {
			return readVariant(row)
		})
		if err != nil {
			return err
		}
		detail.Images, err = listPhotos(ctx, tx, actor.TenantID, id)
		return err
	})
	return detail, err
}

func (s Service) Create(ctx context.Context, actor auth.Identity, input ProductInput) (Product, error) {
	product := Product{Name: strings.TrimSpace(input.Name), Description: strings.TrimSpace(input.Description), Status: "active", CategoryID: input.CategoryID, BrandID: input.BrandID}
	if err := validateProductFields(product); err != nil {
		return Product{}, err
	}
	err := s.withAccess(ctx, actor, true, func(tx pgx.Tx, _ bool) error {
		if err := validateClassifications(ctx, tx, actor.TenantID, product); err != nil {
			return err
		}
		return insertProduct(ctx, tx, actor.TenantID, &product)
	})
	return product, err
}

func insertProduct(ctx context.Context, tx pgx.Tx, tenantID string, product *Product) error {
	return readProduct(tx.QueryRow(ctx, `INSERT INTO products(tenant_id,name,description,status,category_id,brand_id)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING `+productFields,
		tenantID, product.Name, product.Description, product.Status, product.CategoryID, product.BrandID), product)
}

func (s Service) Update(ctx context.Context, actor auth.Identity, id string, changes ProductChanges) (Product, error) {
	uuid, valid := parseID(id)
	if !valid || (changes.Name == nil && changes.Description == nil && changes.Status == nil && len(changes.CategoryID) == 0 && len(changes.BrandID) == 0) {
		return Product{}, ErrInvalid
	}
	var product Product
	err := s.withAccess(ctx, actor, true, func(tx pgx.Tx, _ bool) error {
		if err := readProduct(tx.QueryRow(ctx, "SELECT "+productFields+" FROM products WHERE tenant_id=$1 AND id=$2",
			actor.TenantID, uuid), &product); err != nil {
			return err
		}
		if err := requireRevision(changes.ExpectedRevision, product.Revision); err != nil {
			return err
		}
		if changes.Name != nil {
			product.Name = strings.TrimSpace(*changes.Name)
		}
		if changes.Description != nil {
			product.Description = strings.TrimSpace(*changes.Description)
		}
		if changes.Status != nil {
			product.Status = *changes.Status
		}
		if err := validateProductFields(product); err != nil {
			return err
		}
		if err := applyClassification(changes.CategoryID, &product.CategoryID); err != nil {
			return err
		}
		if err := applyClassification(changes.BrandID, &product.BrandID); err != nil {
			return err
		}
		if err := validateClassifications(ctx, tx, actor.TenantID, product); err != nil {
			return err
		}
		err := tx.QueryRow(ctx, `UPDATE products SET name=$3,description=$4,status=$5,category_id=$6,brand_id=$7,
			revision=revision+1 WHERE tenant_id=$1 AND id=$2 AND revision=$8 RETURNING revision`,
			actor.TenantID, uuid, product.Name, product.Description, product.Status, product.CategoryID, product.BrandID,
			changes.ExpectedRevision).Scan(&product.Revision)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrRevisionConflict
		}
		return err
	})
	return product, err
}

func (s Service) Lookup(ctx context.Context, actor auth.Identity, code string) (Match, error) {
	code = strings.TrimSpace(code)
	if !barcodePattern.MatchString(code) {
		return Match{}, ErrInvalid
	}
	var match Match
	err := s.withAccess(ctx, actor, false, func(tx pgx.Tx, _ bool) error {
		err := tx.QueryRow(ctx, "SELECT p.id,p.name,p.description,p.status,p.category_id,p.brand_id,p.revision,"+variantFields+`
			FROM barcodes b
			JOIN product_variants v ON (v.tenant_id,v.id)=(b.tenant_id,b.variant_id)
			JOIN products p ON (p.tenant_id,p.id)=(v.tenant_id,v.product_id)
			WHERE b.tenant_id=$1 AND b.code=$2 AND p.status='active' AND v.status='active'`, actor.TenantID, code).Scan(
			&match.Product.ID, &match.Product.Name, &match.Product.Description, &match.Product.Status, &match.Product.CategoryID, &match.Product.BrandID, &match.Product.Revision,
			&match.Variant.ID, &match.Variant.ProductID, &match.Variant.SKU, &match.Variant.Size, &match.Variant.Color, &match.Variant.Status, &match.Variant.Revision, &match.Variant.SizeID, &match.Variant.ColorID, &match.Variant.Barcodes, &match.Variant.InternalBarcode, &match.Variant.Options)
		return writeError(err)
	})
	return match, err
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
		err := tx.QueryRow(ctx, `SELECT coalesce(bool_or(rp.permission_code='catalog:read'),false),
			coalesce(bool_or(rp.permission_code='catalog:manage'),false)
			FROM user_roles ur JOIN role_permissions rp ON rp.role_code=ur.role_code
			JOIN users u ON (u.tenant_id,u.id)=(ur.tenant_id,ur.user_id)
			JOIN tenants t ON t.id=u.tenant_id
			WHERE ur.tenant_id=$1 AND ur.user_id=$2 AND u.status='active' AND t.status='active'`,
			actor.TenantID, actor.UserID).Scan(&read, &manage)
		if err != nil {
			return err
		}
		if (writing && !manage) || (!writing && !read && !manage) {
			return auth.ErrForbidden
		}
		return operation(tx, manage)
	})
}

func parseID(id string) (pgtype.UUID, bool) {
	var uuid pgtype.UUID
	valid := len(id) == 36 && uuid.Scan(id) == nil && uuid.String() == id
	return uuid, valid
}

func readProduct(row pgx.Row, product *Product) error {
	return writeError(row.Scan(&product.ID, &product.Name, &product.Description, &product.Status, &product.CategoryID, &product.BrandID, &product.Revision))
}

func validateProductFields(product Product) error {
	if !validText(product.Name, 160, true) {
		return &FieldError{"name", "Ingresá un nombre de hasta 160 caracteres."}
	}
	if !validText(product.Description, 2000, false) {
		return &FieldError{"description", "Ingresá una descripción de hasta 2000 caracteres."}
	}
	if product.Status != "active" && product.Status != "inactive" {
		return &FieldError{"status", "Elegí un estado válido."}
	}
	return nil
}

func validProduct(product Product) bool {
	return validText(product.Name, 160, true) && validText(product.Description, 2000, false) &&
		(product.Status == "active" || product.Status == "inactive")
}

func validText(value string, max int, required bool) bool {
	return (!required || value != "") && !strings.ContainsRune(value, 0) && utf8.ValidString(value) && utf8.RuneCountInString(value) <= max
}

func writeError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var conflict *pgconn.PgError
	if errors.As(err, &conflict) && conflict.Code == "23505" {
		switch conflict.ConstraintName {
		case "variants_tenant_sku":
			return ErrSKU
		case "barcodes_pkey":
			return ErrBarcode
		}
	}
	return err
}
