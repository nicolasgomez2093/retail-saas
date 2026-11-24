package catalog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"slices"
	"strings"

	"retail-platform/api/internal/modules/auth"

	"github.com/jackc/pgx/v5"
)

type VariantInput struct {
	SKU      string   `json:"sku"`
	Size     string   `json:"size"`
	Color    string   `json:"color"`
	SizeID   *string  `json:"sizeId"`
	ColorID  *string  `json:"colorId"`
	Barcodes []string `json:"barcodes"`
}

type VariantCreate struct {
	VariantInput
	GenerateInternal bool   `json:"generateInternal"`
	ReuseRetired     bool   `json:"reuseRetired"`
	Reason           string `json:"reason"`
	ConfirmSensitive bool   `json:"confirmSensitive"`
}

type VariantChanges struct {
	GenerateInternal bool            `json:"generateInternal"`
	ReuseRetired     bool            `json:"reuseRetired"`
	ExpectedRevision int64           `json:"expectedRevision"`
	Reason           string          `json:"reason"`
	ConfirmSensitive bool            `json:"confirmSensitive"`
	SKU              *string         `json:"sku"`
	Size             *string         `json:"size"`
	Color            *string         `json:"color"`
	Status           *string         `json:"status"`
	SizeID           json.RawMessage `json:"sizeId"`
	ColorID          json.RawMessage `json:"colorId"`
	Barcodes         json.RawMessage `json:"barcodes"`
}

// VariantOption is an additional named attribute (beyond size and color)
// that is part of the variant identity, such as a jean length.
type VariantOption struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type Variant struct {
	batchCreated    bool
	Options         []VariantOption `json:"options"`
	InternalBarcode *string         `json:"internalBarcode"`
	Revision        int64           `json:"revision"`
	ID              string          `json:"id"`
	ProductID       string          `json:"productId"`
	Status          string          `json:"status"`
	VariantInput
}

func (s Service) AddVariant(ctx context.Context, actor auth.Identity, id, key string, input VariantCreate) (Variant, error) {
	_, valid := parseID(id)
	normalized, err := normalizeVariant(input.VariantInput)
	if !valid {
		return Variant{}, ErrInvalid
	}
	if key != "" && !variantCreateKey.MatchString(key) {
		return Variant{}, &FieldError{"idempotencyKey", "La clave de alta debe tener entre 1 y 128 caracteres permitidos."}
	}
	if err != nil {
		return Variant{}, err
	}
	variant := Variant{ProductID: id, Status: "active", VariantInput: normalized}
	input.VariantInput = normalized
	input.Reason = strings.TrimSpace(input.Reason)
	body, err := json.Marshal(struct {
		Product string
		Input   VariantCreate
	}{id, input})
	if err != nil {
		return Variant{}, err
	}
	hash := sha256.Sum256(body)
	err = s.withAccess(ctx, actor, true, func(tx pgx.Tx, _ bool) error {
		if key != "" {
			var creator string
			var previous, response []byte
			err := tx.QueryRow(ctx, `SELECT actor_id,request_hash,response FROM catalog_variant_creations
				WHERE tenant_id=$1 AND idempotency_key=$2`, actor.TenantID, key).Scan(&creator, &previous, &response)
			if err == nil {
				if creator != actor.UserID || !bytes.Equal(previous, hash[:]) {
					return ErrVariantCreateKey
				}
				return json.Unmarshal(response, &variant)
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
		for _, selection := range []struct {
			kind  AttributeKind
			field string
			id    *string
			label *string
		}{
			{Sizes, "sizeId", variant.SizeID, &variant.Size}, {Colors, "colorId", variant.ColorID, &variant.Color},
		} {
			if selection.id != nil {
				name, err := selectedAttribute(ctx, tx, actor.TenantID, selection.kind, selection.field, *selection.id, *selection.label)
				if err != nil {
					return err
				}
				*selection.label = name
			}
		}
		if err := verifyMasterCombination(ctx, tx, actor.TenantID, variant); err != nil {
			return err
		}
		if err := protectBatchCombination(ctx, tx, actor.TenantID, variant); err != nil {
			return err
		}
		if input.GenerateInternal {
			if err := generateInternal(ctx, tx, actor.TenantID, &variant); err != nil {
				return err
			}
		}
		reused, err := protectIdentifiers(ctx, tx, actor, nil, &variant, identifierAuthorization{input.ReuseRetired, input.ConfirmSensitive, input.Reason})
		if err != nil {
			return err
		}
		if err := insertVariant(ctx, tx, actor.TenantID, &variant, false); err != nil {
			return err
		}
		if err := logIdentifiers(ctx, tx, actor, variant.ID, "reused", input.Reason, reused); err != nil {
			return err
		}
		if key != "" {
			_, err = tx.Exec(ctx, `INSERT INTO catalog_variant_creations(tenant_id,idempotency_key,actor_id,variant_id,request_hash,response)
				VALUES($1,$2,$3,$4,$5,$6)`, actor.TenantID, key, actor.UserID, variant.ID, hash[:], variant)
		}
		return err
	})
	return variant, err
}

func insertVariant(ctx context.Context, tx pgx.Tx, tenantID string, variant *Variant, allowInactiveProduct bool) error {
	if variant.Options == nil {
		variant.Options = []VariantOption{}
	}
	options, err := json.Marshal(variant.Options)
	if err != nil {
		return err
	}
	err = tx.QueryRow(ctx, `INSERT INTO product_variants(tenant_id,product_id,sku,size,color,status,size_id,color_id,batch_created,options)
		SELECT tenant_id,id,$3,$4,$5,$6,$8,$9,$10,$11::text::jsonb FROM products WHERE tenant_id=$1 AND id=$2 AND ($7 OR status='active')
		RETURNING id,revision`, tenantID, variant.ProductID, variant.SKU, variant.Size, variant.Color,
		variant.Status, allowInactiveProduct, variant.SizeID, variant.ColorID, variant.batchCreated, string(options)).Scan(&variant.ID, &variant.Revision)
	if err != nil {
		return writeError(err)
	}
	if err := insertBarcodes(ctx, tx, tenantID, *variant); err != nil {
		return err
	}
	return advanceSKU(ctx, tx, tenantID, variant.SKU)
}

func (s Service) UpdateVariant(ctx context.Context, actor auth.Identity, productID, variantID string, changes VariantChanges) (Variant, error) {
	product, productOK := parseID(productID)
	id, variantOK := parseID(variantID)
	if !productOK || !variantOK || (changes.SKU == nil && changes.Size == nil &&
		changes.Color == nil && changes.Status == nil && len(changes.Barcodes) == 0 && len(changes.SizeID) == 0 && len(changes.ColorID) == 0 && !changes.GenerateInternal) {
		return Variant{}, ErrInvalid
	}
	var variant Variant
	err := s.withAccess(ctx, actor, true, func(tx pgx.Tx, _ bool) error {
		var err error
		variant, err = readVariant(tx.QueryRow(ctx, "SELECT "+variantFields+` FROM product_variants v
			WHERE v.tenant_id=$1 AND v.product_id=$2 AND v.id=$3`, actor.TenantID, product, id))
		if err != nil {
			return err
		}
		if err := requireRevision(changes.ExpectedRevision, variant.Revision); err != nil {
			return err
		}
		before := variant
		for _, field := range []struct{ target, value *string }{
			{&variant.SKU, changes.SKU}, {&variant.Status, changes.Status},
		} {
			if field.value != nil {
				*field.target = *field.value
			}
		}
		if len(changes.Barcodes) > 0 {
			var codes []string
			if err := json.Unmarshal(changes.Barcodes, &codes); err != nil || codes == nil {
				return ErrInvalid
			}
			variant.Barcodes = codes
		}
		variant.VariantInput, err = normalizeVariant(variant.VariantInput)
		if err != nil {
			return err
		}
		for _, selection := range []struct {
			kind  AttributeKind
			field string
			ref   **string
			label *string
			prior string
			id    json.RawMessage
			text  *string
		}{
			{Sizes, "sizeId", &variant.SizeID, &variant.Size, before.Size, changes.SizeID, changes.Size},
			{Colors, "colorId", &variant.ColorID, &variant.Color, before.Color, changes.ColorID, changes.Color},
		} {
			if err := applyVariantAttribute(ctx, tx, actor.TenantID, selection.kind, selection.field, selection.ref, selection.label, selection.prior, selection.id, selection.text); err != nil {
				return err
			}
		}
		if !validText(strings.TrimSpace(variant.Size), 60, false) {
			return &FieldError{"size", "Ingresá un talle de hasta 60 caracteres."}
		}
		if !validText(strings.TrimSpace(variant.Color), 60, false) {
			return &FieldError{"color", "Ingresá un color de hasta 60 caracteres."}
		}
		if variant.Status != "active" && variant.Status != "inactive" {
			return &FieldError{"status", "Elegí un estado válido."}
		}
		if changes.GenerateInternal {
			if err := generateInternal(ctx, tx, actor.TenantID, &variant); err != nil {
				return err
			}
		} else if variant.InternalBarcode != nil && !includesIdentifier(variant.Barcodes, *variant.InternalBarcode) {
			variant.InternalBarcode = nil
		}
		codesChanged := !slices.Equal(before.Barcodes, variant.Barcodes)
		sensitive := before.SKU != variant.SKU || before.Size != variant.Size || before.Color != variant.Color || codesChanged
		if sensitive {
			if err := authorizeCorrection(ctx, tx, actor, variant.ID, changes); err != nil {
				return err
			}
		}
		attributesChanged := before.Size != variant.Size || before.Color != variant.Color
		if attributesChanged || !sameAttributeRef(before.SizeID, variant.SizeID) || !sameAttributeRef(before.ColorID, variant.ColorID) {
			if err := protectBatchCombination(ctx, tx, actor.TenantID, variant); err != nil {
				return err
			}
		}
		if attributesChanged {
			if err := verifyMasterCombination(ctx, tx, actor.TenantID, variant); err != nil {
				return err
			}
		}

		var reused []identifierChange
		if codesChanged || before.SKU != variant.SKU {
			reused, err = protectIdentifiers(ctx, tx, actor, &before, &variant, identifierAuthorization{changes.ReuseRetired, changes.ConfirmSensitive, changes.Reason})
			if err != nil {
				return err
			}
		}

		err = tx.QueryRow(ctx, `UPDATE product_variants SET sku=$4,size=$5,color=$6,status=$7,revision=revision+1,size_id=$9,color_id=$10
			WHERE tenant_id=$1 AND product_id=$2 AND id=$3 AND revision=$8 RETURNING revision`, actor.TenantID, product, id,
			variant.SKU, variant.Size, variant.Color, variant.Status, changes.ExpectedRevision, variant.SizeID, variant.ColorID).Scan(&variant.Revision)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrRevisionConflict
		}
		if err != nil {
			return writeError(err)
		}
		if codesChanged {
			if _, err := tx.Exec(ctx, "DELETE FROM barcodes WHERE tenant_id=$1 AND variant_id=$2", actor.TenantID, id); err != nil {
				return err
			}
			if err := insertBarcodes(ctx, tx, actor.TenantID, variant); err != nil {
				return err
			}
		}
		if !sensitive {
			return nil
		}
		if err := logIdentifiers(ctx, tx, actor, variant.ID, "reused", changes.Reason, reused); err != nil {
			return err
		}
		if before.SKU != variant.SKU {
			if err := advanceSKU(ctx, tx, actor.TenantID, variant.SKU); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `INSERT INTO catalog_sensitive_events(tenant_id,actor_id,variant_id,action,reason,before_data,after_data)
			VALUES ($1,$2,$3,'correct_variant',$4,$5,$6)`, actor.TenantID, actor.UserID, variant.ID, strings.TrimSpace(changes.Reason), before, variant)
		return err
	})
	return variant, err
}

func readVariant(row pgx.Row) (Variant, error) {
	var variant Variant
	err := row.Scan(variantScanTargets(&variant)...)
	return variant, writeError(err)
}

func variantScanTargets(v *Variant) []any {
	return []any{&v.ID, &v.ProductID, &v.SKU, &v.Size, &v.Color, &v.Status, &v.Revision, &v.SizeID, &v.ColorID, &v.Barcodes, &v.InternalBarcode, &v.Options}
}

func selectedAttribute(ctx context.Context, tx pgx.Tx, tenantID string, kind AttributeKind, field, id, expectedLabel string) (string, error) {
	name, err := attributeName(ctx, tx, tenantID, kind, field, id)
	if err == nil && expectedLabel != "" && expectedLabel != name {
		return "", ErrAttributeChanged
	}
	return name, err
}

// The stored text is the effective identity, independent of later master edits.
func applyVariantAttribute(ctx context.Context, tx pgx.Tx, tenantID string, kind AttributeKind, field string, ref **string, label *string, prior string, raw json.RawMessage, text *string) error {
	*label = prior
	if len(raw) == 0 {
		if text != nil && strings.TrimSpace(*text) != strings.TrimSpace(prior) {
			*label = strings.TrimSpace(*text)
			*ref = nil
		}
		return nil
	}
	var id *string
	if err := json.Unmarshal(raw, &id); err != nil {
		return &FieldError{field, "Elegí un valor de catálogo válido."}
	}
	if id == nil {
		*ref = nil
		*label = ""
		if text != nil {
			*label = strings.TrimSpace(*text)
		}
		return nil
	}
	var expected string
	if text != nil {
		expected = strings.TrimSpace(*text)
	}
	name, err := selectedAttribute(ctx, tx, tenantID, kind, field, *id, expected)
	if err != nil {
		return err
	}
	*ref = id
	if strings.TrimSpace(prior) != name {
		*label = name
	}
	return nil
}

func normalizeVariant(input VariantInput) (VariantInput, error) {
	input.SKU, input.Size, input.Color = strings.TrimSpace(input.SKU), strings.TrimSpace(input.Size), strings.TrimSpace(input.Color)
	for _, field := range []struct {
		name, value, message string
		max                  int
		required             bool
	}{
		{"sku", input.SKU, "Ingresá un SKU de hasta 80 caracteres.", 80, true},
		{"size", input.Size, "Ingresá un talle de hasta 60 caracteres.", 60, false},
		{"color", input.Color, "Ingresá un color de hasta 60 caracteres.", 60, false},
	} {
		if !validText(field.value, field.max, field.required) {
			return VariantInput{}, &FieldError{field.name, field.message}
		}
	}
	if len(input.Barcodes) > 20 {
		return VariantInput{}, &FieldError{"barcodes", "Ingresá hasta 20 códigos."}
	}
	codes := make([]string, 0, len(input.Barcodes))
	seen := make(map[string]bool, len(input.Barcodes))
	for _, code := range input.Barcodes {
		code = strings.TrimSpace(code)
		if !barcodePattern.MatchString(code) || seen[code] {
			return VariantInput{}, &FieldError{"barcodes", "Ingresá códigos válidos y sin duplicados."}
		}
		seen[code] = true
		codes = append(codes, code)
	}
	if len(codes) > 1 {
		slices.Sort(codes[1:])
	}
	input.Barcodes = codes
	return input, nil
}

func insertBarcodes(ctx context.Context, tx pgx.Tx, tenantID string, variant Variant) error {
	if len(variant.Barcodes) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `INSERT INTO barcodes(tenant_id,variant_id,code,is_primary,is_internal)
		SELECT $1,$2,code,pos=1,COALESCE(code=$4::text,false) FROM unnest($3::text[]) WITH ORDINALITY AS codes(code,pos)`,
		tenantID, variant.ID, variant.Barcodes, variant.InternalBarcode)
	return writeError(err)
}

func authorizeCorrection(ctx context.Context, tx pgx.Tx, actor auth.Identity, variantID string, changes VariantChanges) error {
	if err := requireSensitiveAuthority(ctx, tx, actor, changes.Reason, changes.ConfirmSensitive); err != nil {
		return err
	}
	return checkSensitiveHistory(ctx, tx, actor.TenantID, variantID)
}

func requireSensitiveAuthority(ctx context.Context, tx pgx.Tx, actor auth.Identity, reason string, confirmed bool) error {
	var owner bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM user_roles WHERE tenant_id=$1 AND user_id=$2 AND role_code='OWNER'
	)`, actor.TenantID, actor.UserID).Scan(&owner); err != nil {
		return err
	}
	if !owner {
		return auth.ErrForbidden
	}
	if !validText(strings.TrimSpace(reason), 500, true) {
		return &FieldError{"reason", "Ingresá el motivo de la corrección (hasta 500 caracteres)."}
	}
	if !confirmed {
		return &FieldError{"confirmSensitive", "Marcá la casilla para confirmar la corrección."}
	}
	return nil
}

func checkSensitiveHistory(ctx context.Context, tx pgx.Tx, tenantID, variantID string) error {
	// withAccess holds the tenant write lock before reading the variant. Cash,
	// purchases and inventory take it before any operational reads or writes.
	var pending bool
	err := tx.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM reservation_items i
		JOIN reservations r ON (r.tenant_id,r.id)=(i.tenant_id,i.reservation_id)
		WHERE i.tenant_id=$1 AND i.variant_id=$2 AND (
			r.status NOT IN ('FULFILLED','CANCELLED','EXPIRED') OR
			(r.status IN ('CANCELLED','EXPIRED') AND EXISTS (
				SELECT 1 FROM reservation_deposits d
				WHERE (d.tenant_id,d.store_id,d.reservation_id)=(r.tenant_id,r.store_id,r.id)
			))
		)
		UNION ALL
		SELECT 1 FROM purchase_order_items i
		JOIN purchase_orders o ON (o.tenant_id,o.id)=(i.tenant_id,i.purchase_order_id)
		WHERE i.tenant_id=$1 AND i.variant_id=$2 AND o.status <> 'RECEIVED'
		UNION ALL
		SELECT 1 FROM stock_count_items i
		JOIN stock_counts c ON (c.tenant_id,c.id)=(i.tenant_id,i.count_id)
		WHERE i.tenant_id=$1 AND i.variant_id=$2 AND c.status NOT IN ('COMPLETED','CANCELLED')
		UNION ALL
		SELECT 1 FROM stock_transfer_items i
		JOIN stock_transfers t ON (t.tenant_id,t.id)=(i.tenant_id,i.transfer_id)
		WHERE i.tenant_id=$1 AND i.variant_id=$2 AND t.status NOT IN ('RECEIVED','CANCELLED')
		UNION ALL
		SELECT 1 FROM sale_return_items i
		JOIN sale_returns r ON (r.tenant_id,r.store_id,r.sale_id,r.id)=(i.tenant_id,i.store_id,i.sale_id,i.return_id)
		WHERE i.tenant_id=$1 AND i.variant_id=$2
			AND NOT EXISTS (SELECT 1 FROM sale_return_exchanges e
				WHERE (e.tenant_id,e.store_id,e.sale_id,e.return_id)=(r.tenant_id,r.store_id,r.sale_id,r.id))
			AND NOT EXISTS (SELECT 1 FROM sale_return_refunds f
				WHERE (f.tenant_id,f.store_id,f.sale_id,f.return_id)=(r.tenant_id,r.store_id,r.sale_id,r.id))
			AND (r.reference_amount = 0 OR r.reference_amount > COALESCE((
				SELECT sum(c.amount) FROM sale_return_debt_credits c
				WHERE (c.tenant_id,c.store_id,c.sale_id,c.return_id)=(r.tenant_id,r.store_id,r.sale_id,r.id)
			),0))
		UNION ALL
		SELECT 1 FROM stock_balances
		WHERE tenant_id=$1 AND variant_id=$2 AND quantity_reserved>0
	)`, tenantID, variantID).Scan(&pending)
	if err != nil {
		return err
	}
	if pending {
		return ErrSensitiveHistory
	}
	return nil
}
