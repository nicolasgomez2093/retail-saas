// Package storefront publishes a commerce catalog on a public page. One branch provides the
// prices and the availability; shoppers send their cart to the commerce WhatsApp, so no order,
// reservation or stock movement is created here.
package storefront

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/tenancy"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrInvalid = errors.New("verificá los datos de la tienda online")
var ErrNotFound = errors.New("tienda, producto o sucursal inexistente")
var ErrIncomplete = errors.New("para publicar la tienda elegí la sucursal y cargá el WhatsApp del local")

// MaxAdminProducts caps the product list of the settings page.
const MaxAdminProducts = 2000

var whatsappPattern = regexp.MustCompile(`^[1-9][0-9]{7,14}$`)

type Settings struct {
	Enabled     bool    `json:"enabled"`
	StoreID     *string `json:"storeId"`
	WhatsApp    string  `json:"whatsapp"`
	Title       string  `json:"title"`
	Welcome     string  `json:"welcome"`
	ShowSoldOut bool    `json:"showSoldOut"`
}

type StoreOption struct {
	ID   string `json:"id"`
	Code string `json:"code"`
	Name string `json:"name"`
}

// Product statuses, from the point of view of the public page.
const (
	StatusPublished = "published" // visible with at least one available variant
	StatusSoldOut   = "sold_out"  // visible as «Agotado», or not shown when sold-out products are hidden
	StatusHidden    = "hidden"    // hidden by the commerce
	StatusNoPhoto   = "no_photo"  // not published until it has a photo
	StatusNoPrice   = "no_price"  // no active variant with a price in the branch price list
)

type AdminProduct struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Category  *string `json:"category"`
	ImageID   *string `json:"imageId"`
	Hidden    bool    `json:"hidden"`
	Status    string  `json:"status"`
	PriceFrom *string `json:"priceFrom"`
	PriceTo   *string `json:"priceTo"`
	Variants  int     `json:"variants"`
	Available int     `json:"available"`
}

type AdminSummary struct {
	Visible   int `json:"visible"`
	Hidden    int `json:"hidden"`
	NoPhoto   int `json:"noPhoto"`
	NoPrice   int `json:"noPrice"`
	SoldOut   int `json:"soldOut"`
	Truncated int `json:"truncated"`
}

type Admin struct {
	Settings
	Slug         string         `json:"slug"`
	CommerceName string         `json:"commerceName"`
	Currency     string         `json:"currency"`
	Stores       []StoreOption  `json:"stores"`
	Products     []AdminProduct `json:"products"`
	Summary      AdminSummary   `json:"summary"`
}

type Service struct {
	db     *pgxpool.Pool
	images *imageCache
}

func New(db *pgxpool.Pool) Service { return Service{db: db, images: newImageCache(64 << 20)} }

// Admin returns the settings and how each active product would look on the public page.
// Without a chosen branch, the first active branch is used for the preview.
func (s Service) Admin(ctx context.Context, actor auth.Identity) (Admin, error) {
	var result Admin
	err := s.manage(ctx, actor, false, func(tx pgx.Tx) error {
		return readAdmin(ctx, tx, actor.TenantID, &result)
	})
	return result, err
}

func readAdmin(ctx context.Context, tx pgx.Tx, tenantID string, result *Admin) error {
	result.ShowSoldOut = true
	if err := tx.QueryRow(ctx, `SELECT t.slug,t.name,t.currency_code,coalesce(f.enabled,false),f.store_id::text,coalesce(f.whatsapp,''),
			coalesce(f.title,''),coalesce(f.welcome,''),coalesce(f.show_sold_out,true)
		FROM tenants t LEFT JOIN storefronts f ON f.tenant_id=t.id WHERE t.id=$1`, tenantID).
		Scan(&result.Slug, &result.CommerceName, &result.Currency, &result.Enabled, &result.StoreID, &result.WhatsApp,
			&result.Title, &result.Welcome, &result.ShowSoldOut); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, "SELECT id,code,name FROM stores WHERE tenant_id=$1 AND status='active' ORDER BY code,id", tenantID)
	if err != nil {
		return err
	}
	if result.Stores, err = pgx.CollectRows(rows, pgx.RowToStructByPos[StoreOption]); err != nil {
		return err
	}
	preview := ""
	if result.StoreID != nil {
		preview = *result.StoreID
	} else if len(result.Stores) > 0 {
		preview = result.Stores[0].ID
	}
	rows, err = tx.Query(ctx, `WITH offer AS (
			SELECT v.product_id,price.amount,`+availableSQL+` AS available
			FROM store_price_list_assignments a
			JOIN price_list_items price ON (price.tenant_id,price.price_list_id)=(a.tenant_id,a.price_list_id)
			JOIN product_variants v ON (v.tenant_id,v.id)=(price.tenant_id,price.variant_id) AND v.status='active'
			WHERE a.tenant_id=$1 AND a.store_id=NULLIF($2,'')::uuid
		)
		SELECT p.id,p.name,c.name,
			(SELECT i.id::text FROM product_images i WHERE (i.tenant_id,i.product_id)=(p.tenant_id,p.id) ORDER BY i.is_primary DESC,i.created_at,i.id LIMIT 1),
			p.storefront_hidden,min(o.amount)::text,max(o.amount)::text,count(o.product_id)::int,count(*) FILTER (WHERE o.available)::int
		FROM products p LEFT JOIN categories c ON (c.tenant_id,c.id)=(p.tenant_id,p.category_id)
		LEFT JOIN offer o ON o.product_id=p.id
		WHERE p.tenant_id=$1 AND p.status='active'
		GROUP BY p.tenant_id,p.id,c.name
		ORDER BY lower(p.name),p.id LIMIT $3`, tenantID, preview, MaxAdminProducts+1)
	if err != nil {
		return err
	}
	result.Products, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (AdminProduct, error) {
		var product AdminProduct
		err := row.Scan(&product.ID, &product.Name, &product.Category, &product.ImageID, &product.Hidden,
			&product.PriceFrom, &product.PriceTo, &product.Variants, &product.Available)
		return product, err
	})
	if err != nil {
		return err
	}
	if len(result.Products) > MaxAdminProducts {
		result.Products = result.Products[:MaxAdminProducts]
		result.Summary.Truncated = 1
	}
	for index := range result.Products {
		product := &result.Products[index]
		switch {
		case product.Hidden:
			product.Status = StatusHidden
			result.Summary.Hidden++
		case product.ImageID == nil:
			product.Status = StatusNoPhoto
			result.Summary.NoPhoto++
		case product.Variants == 0:
			product.Status = StatusNoPrice
			result.Summary.NoPrice++
		case product.Available == 0:
			product.Status = StatusSoldOut
			result.Summary.SoldOut++
			if result.ShowSoldOut {
				result.Summary.Visible++
			}
		default:
			product.Status = StatusPublished
			result.Summary.Visible++
		}
	}
	return nil
}

// Save stores the settings. Publishing requires an active branch and a WhatsApp number.
func (s Service) Save(ctx context.Context, actor auth.Identity, input Settings) (Admin, error) {
	input.WhatsApp = normalizeWhatsApp(input.WhatsApp)
	input.Title, input.Welcome = strings.TrimSpace(input.Title), strings.TrimSpace(input.Welcome)
	if input.WhatsApp != "" && !whatsappPattern.MatchString(input.WhatsApp) || !validText(input.Title, 80) || !validText(input.Welcome, 300) ||
		input.StoreID != nil && !validID(*input.StoreID) {
		return Admin{}, ErrInvalid
	}
	if input.Enabled && (input.StoreID == nil || input.WhatsApp == "") {
		return Admin{}, ErrIncomplete
	}
	var result Admin
	err := s.manage(ctx, actor, true, func(tx pgx.Tx) error {
		if input.StoreID != nil {
			var active bool
			err := tx.QueryRow(ctx, "SELECT status='active' FROM stores WHERE tenant_id=$1 AND id=$2", actor.TenantID, *input.StoreID).Scan(&active)
			if errors.Is(err, pgx.ErrNoRows) || err == nil && !active {
				return ErrNotFound
			}
			if err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO storefronts(tenant_id,enabled,store_id,whatsapp,title,welcome,show_sold_out,updated_at,updated_by)
			VALUES ($1,$2,$3,$4,$5,$6,$7,now(),$8)
			ON CONFLICT (tenant_id) DO UPDATE SET enabled=excluded.enabled,store_id=excluded.store_id,whatsapp=excluded.whatsapp,
				title=excluded.title,welcome=excluded.welcome,show_sold_out=excluded.show_sold_out,updated_at=now(),updated_by=excluded.updated_by`,
			actor.TenantID, input.Enabled, input.StoreID, input.WhatsApp, input.Title, input.Welcome, input.ShowSoldOut, actor.UserID); err != nil {
			return err
		}
		return readAdmin(ctx, tx, actor.TenantID, &result)
	})
	return result, err
}

// SetHidden shows or hides one product on the public page.
func (s Service) SetHidden(ctx context.Context, actor auth.Identity, productID string, hidden bool) error {
	if !validID(productID) {
		return ErrNotFound
	}
	return s.manage(ctx, actor, true, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, "UPDATE products SET storefront_hidden=$3 WHERE tenant_id=$1 AND id=$2", actor.TenantID, productID, hidden)
		if err == nil && tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return err
	})
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
			WHERE ur.tenant_id=$1 AND ur.user_id=$2 AND rp.permission_code='storefront:manage'
			AND u.status='active' AND t.status='active')`, actor.TenantID, actor.UserID).Scan(&allowed); err != nil {
			return err
		}
		if !allowed {
			return auth.ErrForbidden
		}
		return operation(tx)
	})
}

// availableSQL tells whether variant v has units to sell in the branch of a.store_id:
// active sellable locations, minus reservations. Quantities never leave the database.
const availableSQL = `coalesce((SELECT sum(b.quantity_on_hand-b.quantity_reserved) FROM stock_balances b
	JOIN stock_locations l ON (l.tenant_id,l.id)=(b.tenant_id,b.location_id)
	WHERE b.tenant_id=v.tenant_id AND b.variant_id=v.id AND l.store_id=a.store_id AND l.status='active' AND l.is_sellable),0)>0`

// normalizeWhatsApp keeps the digits of a number typed with spaces, dashes, parentheses or a leading +.
func normalizeWhatsApp(value string) string {
	var digits strings.Builder
	for _, r := range strings.TrimSpace(value) {
		switch {
		case r >= '0' && r <= '9':
			digits.WriteRune(r)
		case r == ' ' || r == '-' || r == '(' || r == ')' || r == '+' || r == '.':
		default:
			return value // keeps the invalid characters so validation rejects it
		}
	}
	return digits.String()
}

func validID(id string) bool {
	var uuid pgtype.UUID
	return len(id) == 36 && uuid.Scan(id) == nil && uuid.String() == id
}

func validText(value string, max int) bool {
	return !strings.ContainsRune(value, 0) && utf8.ValidString(value) && utf8.RuneCountInString(value) <= max
}
