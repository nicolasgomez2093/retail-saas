package storefront

import (
	"context"
	"errors"
	"regexp"
	"strconv"

	"github.com/jackc/pgx/v5"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

type PublicCategory struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// PublicProduct is one card of the catalog: one product, whatever its sizes and colors.
type PublicProduct struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	CategoryID *string `json:"categoryId"`
	Brand      *string `json:"brand"`
	ImageID    string  `json:"imageId"`
	PriceFrom  string  `json:"priceFrom"`
	PriceTo    string  `json:"priceTo"`
	Available  bool    `json:"available"`
}

type PublicStore struct {
	Slug       string           `json:"slug"`
	Name       string           `json:"name"`
	Welcome    string           `json:"welcome"`
	WhatsApp   string           `json:"whatsapp"`
	Currency   string           `json:"currency"`
	Branch     string           `json:"branch"`
	Address    string           `json:"address"`
	Categories []PublicCategory `json:"categories"`
	Products   []PublicProduct  `json:"products"`
}

// PublicVariant tells only whether a size and color can be bought, never how many units there are.
type PublicVariant struct {
	ID        string  `json:"id"`
	Size      string  `json:"size"`
	Color     string  `json:"color"`
	ColorHex  *string `json:"colorHex"`
	Price     string  `json:"price"`
	Available bool    `json:"available"`
}

type PublicProductDetail struct {
	PublicProduct
	Description string          `json:"description"`
	Category    *string         `json:"category"`
	Images      []string        `json:"images"`
	Variants    []PublicVariant `json:"variants"`
}

// published lists the products of an enabled storefront: active, not hidden, with a photo and
// at least one active variant priced in the branch price list. Sold-out products are listed only
// when the commerce chose to show them.
const publishedSQL = `WITH sf AS (
		SELECT f.tenant_id,f.store_id,f.show_sold_out,f.title,f.welcome,f.whatsapp,t.slug,t.name,t.currency_code,s.name AS branch,s.address
		FROM storefronts f JOIN tenants t ON t.id=f.tenant_id
		JOIN stores s ON (s.tenant_id,s.id)=(f.tenant_id,f.store_id)
		WHERE t.slug=$1 AND t.status='active' AND f.enabled AND s.status='active'
	), offer AS (
		SELECT v.tenant_id,v.product_id,v.id AS variant_id,price.amount,` + availableSQL + ` AS available
		FROM sf JOIN store_price_list_assignments a ON (a.tenant_id,a.store_id)=(sf.tenant_id,sf.store_id)
		JOIN price_list_items price ON (price.tenant_id,price.price_list_id)=(a.tenant_id,a.price_list_id)
		JOIN product_variants v ON (v.tenant_id,v.id)=(price.tenant_id,price.variant_id) AND v.status='active'
	), published AS (
		SELECT p.tenant_id,p.id,p.name,p.description,p.category_id,p.brand_id,
			(SELECT i.id::text FROM product_images i WHERE (i.tenant_id,i.product_id)=(p.tenant_id,p.id)
			 ORDER BY i.is_primary DESC,i.created_at,i.id LIMIT 1) AS image_id,
			min(o.amount) AS price_from,max(o.amount) AS price_to,bool_or(o.available) AS available
		FROM sf JOIN products p ON p.tenant_id=sf.tenant_id AND p.status='active' AND NOT p.storefront_hidden
		JOIN offer o ON (o.tenant_id,o.product_id)=(p.tenant_id,p.id)
		GROUP BY p.tenant_id,p.id,sf.show_sold_out
		HAVING sf.show_sold_out OR bool_or(o.available)
	)`

func (s Service) Public(ctx context.Context, slug string) (PublicStore, error) {
	var result PublicStore
	if !slugPattern.MatchString(slug) || len(slug) > 80 {
		return result, ErrNotFound
	}
	err := pgx.BeginTxFunc(ctx, s.db, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `WITH sf AS (SELECT f.title,f.welcome,f.whatsapp,t.slug,t.name,t.currency_code,s.name AS branch,s.address
				FROM storefronts f JOIN tenants t ON t.id=f.tenant_id JOIN stores s ON (s.tenant_id,s.id)=(f.tenant_id,f.store_id)
				WHERE t.slug=$1 AND t.status='active' AND f.enabled AND s.status='active')
			SELECT slug,coalesce(NULLIF(title,''),name),welcome,whatsapp,currency_code,branch,address FROM sf`, slug).
			Scan(&result.Slug, &result.Name, &result.Welcome, &result.WhatsApp, &result.Currency, &result.Branch, &result.Address)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, publishedSQL+`
			SELECT id::text,name,category_id::text,(SELECT b.name FROM brands b WHERE (b.tenant_id,b.id)=(published.tenant_id,published.brand_id)),
				image_id,price_from::text,price_to::text,available
			FROM published WHERE image_id IS NOT NULL ORDER BY available DESC,lower(name),id`, slug)
		if err != nil {
			return err
		}
		if result.Products, err = pgx.CollectRows(rows, pgx.RowToStructByPos[PublicProduct]); err != nil {
			return err
		}
		rows, err = tx.Query(ctx, publishedSQL+`
			SELECT DISTINCT c.id::text,c.name FROM published JOIN categories c ON (c.tenant_id,c.id)=(published.tenant_id,published.category_id)
			WHERE published.image_id IS NOT NULL ORDER BY c.name,c.id::text`, slug)
		if err != nil {
			return err
		}
		result.Categories, err = pgx.CollectRows(rows, pgx.RowToStructByPos[PublicCategory])
		return err
	})
	return result, err
}

func (s Service) PublicProduct(ctx context.Context, slug, productID string) (PublicProductDetail, error) {
	var result PublicProductDetail
	if !slugPattern.MatchString(slug) || len(slug) > 80 || !validID(productID) {
		return result, ErrNotFound
	}
	err := pgx.BeginTxFunc(ctx, s.db, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, publishedSQL+`
			SELECT id::text,name,category_id::text,(SELECT b.name FROM brands b WHERE (b.tenant_id,b.id)=(published.tenant_id,published.brand_id)),
				image_id,price_from::text,price_to::text,available,description,
				(SELECT c.name FROM categories c WHERE (c.tenant_id,c.id)=(published.tenant_id,published.category_id)),
				ARRAY(SELECT i.id::text FROM product_images i WHERE (i.tenant_id,i.product_id)=(published.tenant_id,published.id)
					ORDER BY i.is_primary DESC,i.created_at,i.id)
			FROM published WHERE id=$2::uuid AND image_id IS NOT NULL`, slug, productID).
			Scan(&result.ID, &result.Name, &result.CategoryID, &result.Brand, &result.ImageID, &result.PriceFrom, &result.PriceTo,
				&result.Available, &result.Description, &result.Category, &result.Images)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		// Sizes and colors follow the order of the commerce attribute lists, then their names.
		rows, err := tx.Query(ctx, publishedSQL+`
			SELECT o.variant_id::text,v.size,v.color,color.hex,o.amount::text,o.available
			FROM offer o JOIN product_variants v ON (v.tenant_id,v.id)=(o.tenant_id,o.variant_id)
			LEFT JOIN catalog_attributes size ON (size.tenant_id,size.kind,size.id)=(v.tenant_id,'sizes',v.size_id)
			LEFT JOIN catalog_attributes color ON (color.tenant_id,color.kind,color.id)=(v.tenant_id,'colors',v.color_id)
			WHERE o.product_id=$2::uuid
			ORDER BY size.position NULLS LAST,v.size,color.position NULLS LAST,v.color,v.sku,v.id`, slug, productID)
		if err != nil {
			return err
		}
		result.Variants, err = pgx.CollectRows(rows, pgx.RowToStructByPos[PublicVariant])
		return err
	})
	return result, err
}

// Image sizes served to shoppers: catalog cards and the product page.
const (
	SizeCard   = 480
	SizeDetail = 1080
)

// PublicImage returns a photo of a product shown by an enabled storefront, reduced to fit in a
// square of side pixels. Photos never change once uploaded, so reduced copies are cached.
func (s Service) PublicImage(ctx context.Context, slug, imageID string, side int) ([]byte, error) {
	if !slugPattern.MatchString(slug) || len(slug) > 80 || !validID(imageID) || side != SizeCard && side != SizeDetail {
		return nil, ErrNotFound
	}
	var shown bool
	if err := s.db.QueryRow(ctx, publishedSQL+`
		SELECT EXISTS(SELECT 1 FROM published JOIN product_images i ON (i.tenant_id,i.product_id)=(published.tenant_id,published.id)
		WHERE i.id=$2::uuid AND published.image_id IS NOT NULL)`, slug, imageID).Scan(&shown); err != nil {
		return nil, err
	}
	if !shown {
		return nil, ErrNotFound
	}
	key := imageID + "/" + strconv.Itoa(side)
	if body, ok := s.images.get(key); ok {
		return body, nil
	}
	var original []byte
	if err := s.db.QueryRow(ctx, "SELECT jpeg_data FROM product_images WHERE id=$1::uuid", imageID).Scan(&original); err != nil {
		return nil, err
	}
	body, err := shrink(original, side)
	if err != nil {
		return nil, err
	}
	s.images.put(key, body)
	return body, nil
}
