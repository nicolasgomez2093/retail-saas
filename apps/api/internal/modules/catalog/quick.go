package catalog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/modules/inventory"
	"retail-platform/api/internal/platform/money"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Quick creation writes a product with all its commercial data in a single
// database transaction: either everything is confirmed or nothing is.
const (
	QuickVariantLimit  = VariantBatchLimit
	QuickAttributeMax  = 4
	QuickQuantityLimit = 100000
)

var (
	ErrQuickKeyRace  = errors.New("otra solicitud con la misma clave todavía está en curso. Reintentá en unos segundos")
	ErrQuickNoList   = errors.New("la sucursal no tiene una lista de precios asignada")
	ErrQuickLocation = errors.New("la ubicación no pertenece a la sucursal o está inactiva")
)

type QuickAttribute struct {
	Name string `json:"name"`
	Role string `json:"role"`
}

type QuickBarcode struct {
	Mode string `json:"mode"`
	Code string `json:"code"`
}

type QuickVariantInput struct {
	Values   []string     `json:"values"`
	SKU      string       `json:"sku"`
	Barcode  QuickBarcode `json:"barcode"`
	Price    *string      `json:"price"`
	Quantity int32        `json:"quantity"`
}

type QuickProductInput struct {
	Product    ProductInput        `json:"product"`
	StoreID    string              `json:"storeId"`
	LocationID string              `json:"locationId"`
	Attributes []QuickAttribute    `json:"attributes"`
	Variants   []QuickVariantInput `json:"variants"`
}

type QuickVariantResult struct {
	Variant    Variant `json:"variant"`
	Price      *string `json:"price"`
	Quantity   int32   `json:"quantity"`
	MovementID *string `json:"movementId"`
}

type QuickProductResult struct {
	Product     Product              `json:"product"`
	Variants    []QuickVariantResult `json:"variants"`
	Attributes  []QuickAttribute     `json:"attributes"`
	StoreID     string               `json:"storeId"`
	LocationID  *string              `json:"locationId"`
	PriceListID *string              `json:"priceListId"`
	Currency    string               `json:"currency"`
	Units       int64                `json:"units"`
}

type quickRow struct {
	input   QuickVariantInput
	variant Variant
	price   *string
}

func normalizeQuick(input *QuickProductInput) error {
	input.Product.Name = strings.TrimSpace(input.Product.Name)
	input.Product.Description = strings.TrimSpace(input.Product.Description)
	if err := validateProductFields(Product{Name: input.Product.Name, Description: input.Product.Description, Status: "active"}); err != nil {
		return err
	}
	if _, ok := parseID(input.StoreID); !ok {
		return &FieldError{"storeId", "Elegí una sucursal autorizada."}
	}
	if input.LocationID != "" {
		if _, ok := parseID(input.LocationID); !ok {
			return &FieldError{"locationId", "Elegí una ubicación válida."}
		}
	}
	if len(input.Attributes) > QuickAttributeMax {
		return &FieldError{"attributes", "Usá hasta 4 atributos por producto."}
	}
	roles := map[string]bool{}
	names := map[string]bool{}
	for i := range input.Attributes {
		attr := &input.Attributes[i]
		attr.Name = strings.TrimSpace(attr.Name)
		if !validText(attr.Name, 40, true) {
			return &FieldError{"attributes", "Cada atributo necesita un nombre de hasta 40 caracteres."}
		}
		key := strings.ToLower(attr.Name)
		if names[key] {
			return &FieldError{"attributes", "El atributo «" + attr.Name + "» está repetido."}
		}
		names[key] = true
		switch attr.Role {
		case "size", "color":
			if roles[attr.Role] {
				return &FieldError{"attributes", "Elegí un solo atributo de talle y uno de color."}
			}
			roles[attr.Role] = true
		case "option":
		default:
			return &FieldError{"attributes", "Indicá un tipo de atributo válido."}
		}
	}
	if len(input.Variants) < 1 || len(input.Variants) > QuickVariantLimit {
		return ErrBatchLimit
	}
	if len(input.Attributes) == 0 && len(input.Variants) != 1 {
		return &FieldError{"variants", "Un producto simple tiene una sola variante."}
	}
	for i := range input.Variants {
		row := &input.Variants[i]
		row.SKU = strings.TrimSpace(row.SKU)
		row.Barcode.Code = strings.TrimSpace(row.Barcode.Code)
		if len(row.Values) != len(input.Attributes) {
			return &FieldError{"variants", "Cada variante debe tener un valor por atributo."}
		}
		for j := range row.Values {
			row.Values[j] = strings.TrimSpace(row.Values[j])
		}
		if row.Price != nil {
			*row.Price = strings.TrimSpace(*row.Price)
		}
	}
	return nil
}

func (s Service) QuickCreate(ctx context.Context, actor auth.Identity, key string, input QuickProductInput) (QuickProductResult, error) {
	if !variantCreateKey.MatchString(key) {
		return QuickProductResult{}, &FieldError{"idempotencyKey", "Indicá una clave de idempotencia válida para el alta."}
	}
	if err := normalizeQuick(&input); err != nil {
		return QuickProductResult{}, err
	}
	body, err := json.Marshal(input)
	if err != nil {
		return QuickProductResult{}, err
	}
	hash := sha256.Sum256(body)
	var result QuickProductResult
	err = s.withAccess(ctx, actor, true, func(tx pgx.Tx, _ bool) error {
		var creator string
		var previous, response []byte
		err := tx.QueryRow(ctx, `SELECT actor_id,request_hash,response FROM catalog_quick_creations
			WHERE tenant_id=$1 AND idempotency_key=$2`, actor.TenantID, key).Scan(&creator, &previous, &response)
		if err == nil {
			if creator != actor.UserID || !bytes.Equal(previous, hash[:]) {
				return ErrVariantCreateKey
			}
			return json.Unmarshal(response, &result)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		result, err = quickCreate(ctx, tx, actor, input)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO catalog_quick_creations(tenant_id,idempotency_key,actor_id,product_id,request_hash,response)
			VALUES ($1,$2,$3,$4,$5,$6)`, actor.TenantID, key, actor.UserID, result.Product.ID, hash[:], result)
		var conflict *pgconn.PgError
		if errors.As(err, &conflict) && conflict.Code == "23505" {
			return ErrQuickKeyRace
		}
		return err
	})
	if err != nil {
		return QuickProductResult{}, err
	}
	return result, nil
}

func quickCreate(ctx context.Context, tx pgx.Tx, actor auth.Identity, input QuickProductInput) (QuickProductResult, error) {
	result := QuickProductResult{StoreID: input.StoreID, Attributes: input.Attributes}
	if result.Attributes == nil {
		result.Attributes = []QuickAttribute{}
	}
	product := Product{Name: input.Product.Name, Description: input.Product.Description, Status: "active",
		CategoryID: input.Product.CategoryID, BrandID: input.Product.BrandID}
	if err := validateClassifications(ctx, tx, actor.TenantID, product); err != nil {
		return result, err
	}

	wantsPrices, wantsStock := false, false
	for _, row := range input.Variants {
		wantsPrices = wantsPrices || row.Price != nil
		wantsStock = wantsStock || row.Quantity > 0
	}
	var storeAllowed, pricing, adjust bool
	if err := tx.QueryRow(ctx, `SELECT
		EXISTS(SELECT 1 FROM user_stores us JOIN stores s ON (s.tenant_id,s.id)=(us.tenant_id,us.store_id)
			WHERE us.tenant_id=$1 AND us.user_id=$2 AND us.store_id=$3 AND s.status='active'),
		EXISTS(SELECT 1 FROM user_roles ur JOIN role_permissions rp ON rp.role_code=ur.role_code
			WHERE ur.tenant_id=$1 AND ur.user_id=$2 AND rp.permission_code='pricing:manage'),
		EXISTS(SELECT 1 FROM user_roles ur JOIN role_permissions rp ON rp.role_code=ur.role_code
			WHERE ur.tenant_id=$1 AND ur.user_id=$2 AND rp.permission_code='inventory:adjust')`,
		actor.TenantID, actor.UserID, input.StoreID).Scan(&storeAllowed, &pricing, &adjust); err != nil {
		return result, err
	}
	if !storeAllowed || (wantsPrices && !pricing) || (wantsStock && !adjust) {
		return result, auth.ErrForbidden
	}
	if err := tx.QueryRow(ctx, "SELECT currency_code FROM tenants WHERE id=$1", actor.TenantID).Scan(&result.Currency); err != nil {
		return result, err
	}
	if wantsPrices {
		err := tx.QueryRow(ctx, `SELECT price_list_id FROM store_price_list_assignments WHERE tenant_id=$1 AND store_id=$2`,
			actor.TenantID, input.StoreID).Scan(&result.PriceListID)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && result.PriceListID == nil) {
			return result, ErrQuickNoList
		}
		if err != nil {
			return result, err
		}
	}
	if wantsStock {
		if input.LocationID == "" {
			return result, &FieldError{"locationId", "Elegí la ubicación donde ingresa la mercadería."}
		}
		var id string
		err := tx.QueryRow(ctx, `SELECT id FROM stock_locations WHERE tenant_id=$1 AND store_id=$2 AND id=$3 AND status='active'`,
			actor.TenantID, input.StoreID, input.LocationID).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return result, ErrQuickLocation
		}
		if err != nil {
			return result, err
		}
		result.LocationID = &id
	}

	rows, issues, err := prepareQuickRows(ctx, tx, actor.TenantID, input)
	if err != nil {
		return result, err
	}
	if len(issues) > 0 {
		return result, &VariantBatchError{Issues: issues}
	}

	if err := insertProduct(ctx, tx, actor.TenantID, &product); err != nil {
		return result, err
	}
	result.Product = product
	for i := range rows {
		row := &rows[i]
		v := &row.variant
		v.ProductID = product.ID
		if v.SKU == "" {
			state, err := counters(ctx, tx, actor.TenantID)
			if err != nil {
				return result, err
			}
			v.SKU, _, err = nextIdentifier(ctx, tx, actor.TenantID, "sku", state.Prefix, state.Width, state.sku+1, nil)
			if err != nil {
				return result, batchWriteFailure(i+1, err)
			}
		}
		if row.input.Barcode.Mode == "internal" {
			if err := generateInternal(ctx, tx, actor.TenantID, v); err != nil {
				return result, batchWriteFailure(i+1, err)
			}
		}
		if err := insertVariant(ctx, tx, actor.TenantID, v, false); err != nil {
			return result, batchWriteFailure(i+1, err)
		}
		saved := QuickVariantResult{Variant: *v, Quantity: row.input.Quantity}
		if row.price != nil {
			if _, err := tx.Exec(ctx, `INSERT INTO price_list_items(tenant_id,price_list_id,variant_id,amount)
				VALUES ($1,$2,$3,$4::text::numeric)`, actor.TenantID, *result.PriceListID, v.ID, *row.price); err != nil {
				return result, err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO price_admin_events(tenant_id,actor_id,action,list_id,store_id,variant_id,before_data,after_data)
				VALUES ($1,$2,'set_price',$3,NULL,$4,$5,$6)`, actor.TenantID, actor.UserID, *result.PriceListID, v.ID,
				map[string]any{"amount": nil}, map[string]any{"amount": *row.price}); err != nil {
				return result, err
			}
			saved.Price = row.price
		}
		if row.input.Quantity > 0 {
			movement, err := inventory.PostInitialStock(ctx, tx, actor, *result.LocationID, v.ID, v.SKU, row.input.Quantity)
			if err != nil {
				return result, err
			}
			saved.MovementID = &movement.ID
			result.Units += int64(row.input.Quantity)
		}
		result.Variants = append(result.Variants, saved)
	}
	return result, nil
}

// prepareQuickRows validates every row before anything is written and
// reports all problems together, so the person can correct them at once.
func prepareQuickRows(ctx context.Context, tx pgx.Tx, tenantID string, input QuickProductInput) ([]quickRow, []VariantBatchIssue, error) {
	rows := make([]quickRow, len(input.Variants))
	var issues []VariantBatchIssue
	add := func(row int, field, code, message string, existing *ExistingCombination) {
		issues = append(issues, VariantBatchIssue{Row: row, Field: field, Code: code, Message: message, Existing: existing})
	}
	masters := map[string]masterValue{}
	seenCombination := map[string]int{}
	seenSKU := map[string]int{}
	seenCode := map[string]int{}
	for i, entry := range input.Variants {
		n := i + 1
		v := Variant{Status: "active", batchCreated: true, Options: []VariantOption{},
			VariantInput: VariantInput{SKU: entry.SKU, Barcodes: []string{}}}
		valid := true
		for j, value := range entry.Values {
			attr := input.Attributes[j]
			if !validText(value, 60, true) {
				add(n, "values", "invalid_field", "Completá «"+attr.Name+"» con hasta 60 caracteres.", nil)
				valid = false
				continue
			}
			switch attr.Role {
			case "option":
				v.Options = append(v.Options, VariantOption{Name: attr.Name, Value: value})
			case "size", "color":
				kind := Sizes
				if attr.Role == "color" {
					kind = Colors
				}
				master, err := resolveMaster(ctx, tx, tenantID, kind, value, masters)
				if err != nil {
					return nil, nil, err
				}
				if master.inactive {
					add(n, "values", "attribute_inactive", "«"+value+"» está desactivado en Talles y colores. Activalo o elegí otro valor.", nil)
					valid = false
					continue
				}
				if attr.Role == "size" {
					v.Size, v.SizeID = master.name, master.id
				} else {
					v.Color, v.ColorID = master.name, master.id
				}
			}
		}
		if valid {
			key := strings.ToLower(strings.Join(entry.Values, "\x1f"))
			if first, repeated := seenCombination[key]; repeated {
				add(n, "values", "batch_duplicate", "La combinación repite la fila "+strconv.Itoa(first)+".", nil)
			} else {
				seenCombination[key] = n
			}
		}
		if entry.Quantity < 0 || entry.Quantity > QuickQuantityLimit {
			add(n, "quantity", "invalid_field", "Ingresá una cantidad entre 0 y 100.000 unidades.", nil)
		}
		if entry.Price != nil {
			cents, ok := money.Parse(*entry.Price)
			if !ok {
				add(n, "price", "invalid_field", "Ingresá un precio válido, con hasta dos decimales.", nil)
			} else {
				formatted := money.Format(cents)
				rows[i].price = &formatted
			}
		}
		if entry.SKU != "" {
			if !validText(entry.SKU, 80, true) {
				add(n, "sku", "invalid_field", "Ingresá un SKU de hasta 80 caracteres.", nil)
			} else {
				var used, retired bool
				if err := tx.QueryRow(ctx, `SELECT
					EXISTS(SELECT 1 FROM product_variants WHERE tenant_id=$1 AND lower(sku)=lower($2::text)),
					EXISTS(SELECT 1 FROM catalog_identifier_events WHERE tenant_id=$1 AND kind='sku' AND action='retired' AND normalized=lower($2::text))`,
					tenantID, entry.SKU).Scan(&used, &retired); err != nil {
					return nil, nil, err
				}
				key := strings.ToLower(entry.SKU)
				switch {
				case used:
					add(n, "sku", "sku_taken", ErrSKU.Error(), nil)
				case retired:
					add(n, "sku", "identifier_retired", ErrIdentifierRetired.Error(), nil)
				case seenSKU[key] > 0:
					add(n, "sku", "batch_duplicate", "El SKU repite la fila "+strconv.Itoa(seenSKU[key])+".", nil)
				}
				seenSKU[key] = n
			}
		}
		switch entry.Barcode.Mode {
		case "internal":
		case "existing":
			code := entry.Barcode.Code
			if !barcodePattern.MatchString(code) {
				add(n, "barcode", "invalid_field", "Escaneá o ingresá un código de hasta 80 letras, números, guiones o puntos.", nil)
				break
			}
			if first := seenCode[code]; first > 0 {
				add(n, "barcode", "batch_duplicate", "El código repite la fila "+strconv.Itoa(first)+".", nil)
				break
			}
			seenCode[code] = n
			existing, err := barcodeOwner(ctx, tx, tenantID, code)
			if err != nil {
				return nil, nil, err
			}
			if existing != nil {
				add(n, "barcode", "barcode_taken", "El código ya pertenece a "+existing.Product+" · SKU "+existing.SKU+".", existing)
				break
			}
			var retired bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM catalog_identifier_events
				WHERE tenant_id=$1 AND kind='barcode' AND action='retired' AND normalized=$2)`, tenantID, code).Scan(&retired); err != nil {
				return nil, nil, err
			}
			if retired {
				add(n, "barcode", "identifier_retired", "El código fue usado antes por otra variante y sus etiquetas podrían seguir en circulación. Usá otro código.", nil)
				break
			}
			v.Barcodes = []string{code}
		default:
			add(n, "barcode", "invalid_field", "Elegí generar un código interno o usar un código existente.", nil)
		}
		rows[i].input, rows[i].variant = entry, v
	}
	return rows, issues, nil
}

func barcodeOwner(ctx context.Context, tx pgx.Tx, tenantID, code string) (*ExistingCombination, error) {
	var owner ExistingCombination
	err := tx.QueryRow(ctx, `SELECT v.id,v.sku,v.status,p.name FROM barcodes b
		JOIN product_variants v ON (v.tenant_id,v.id)=(b.tenant_id,b.variant_id)
		JOIN products p ON (p.tenant_id,p.id)=(v.tenant_id,v.product_id)
		WHERE b.tenant_id=$1 AND b.code=$2`, tenantID, code).Scan(&owner.ID, &owner.SKU, &owner.Status, &owner.Product)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &owner, err
}

type masterValue struct {
	id       *string
	name     string
	inactive bool
}

// resolveMaster links a size or color to the tenant's attribute catalog,
// adding the value when it does not exist yet. Inactive values are not
// reactivated silently. A color whose code collides with another color is
// stored as text, and Talles y colores lists it for review.
func resolveMaster(ctx context.Context, tx pgx.Tx, tenantID string, kind AttributeKind, value string, cache map[string]masterValue) (masterValue, error) {
	cacheKey := string(kind) + "\x1f" + strings.ToLower(value)
	if found, ok := cache[cacheKey]; ok {
		return found, nil
	}
	lookup := func() (masterValue, bool, error) {
		var result masterValue
		var id, status string
		err := tx.QueryRow(ctx, `SELECT id,name,status FROM catalog_attributes
			WHERE tenant_id=$1 AND kind=$2 AND lower(btrim(name))=lower(btrim($3::text))`, tenantID, kind, value).Scan(&id, &result.name, &status)
		if errors.Is(err, pgx.ErrNoRows) {
			return result, false, nil
		}
		result.id, result.inactive = &id, status != "active"
		return result, err == nil, err
	}
	result, found, err := lookup()
	if err != nil {
		return result, err
	}
	if !found {
		var code *string
		if kind == Colors {
			lower := strings.ToLower(value)
			code = &lower
		}
		if _, err := tx.Exec(ctx, `INSERT INTO catalog_attributes(tenant_id,kind,name,code,position)
			SELECT $1,$2,$3,$4,LEAST(9999,COALESCE(max(position),0)+1) FROM catalog_attributes WHERE tenant_id=$1 AND kind=$2
			ON CONFLICT DO NOTHING`, tenantID, kind, value, code); err != nil {
			return result, err
		}
		result, found, err = lookup()
		if err != nil {
			return result, err
		}
		if !found {
			result = masterValue{name: value}
		}
	}
	cache[cacheKey] = result
	return result, nil
}
