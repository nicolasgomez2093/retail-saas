package catalog

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"retail-platform/api/internal/modules/auth"

	"github.com/jackc/pgx/v5"
)

const VariantBatchLimit = 50

var ErrBatchLimit = errors.New("seleccioná entre 1 y 50 variantes por operación")

type BatchAttributeRef struct {
	ID       string `json:"id"`
	Revision int64  `json:"revision"`
}

type BatchVariantInput struct {
	Size             *BatchAttributeRef `json:"size"`
	Color            *BatchAttributeRef `json:"color"`
	SKU              string             `json:"sku"`
	GenerateInternal bool               `json:"generateInternal"`
	Status           string             `json:"status"`
}

type ExistingCombination struct {
	ID      string `json:"id"`
	SKU     string `json:"sku"`
	Status  string `json:"status"`
	Product string `json:"product,omitempty"`
}

type VariantBatchIssue struct {
	Row      int                  `json:"row"`
	Field    string               `json:"field"`
	Code     string               `json:"code"`
	Message  string               `json:"message"`
	Existing *ExistingCombination `json:"existing,omitempty"`
}

type VariantBatchError struct {
	Issues []VariantBatchIssue
}

func (e *VariantBatchError) Error() string {
	return "El lote no se guardó. Revisá las filas indicadas."
}

type BatchPreviewRow struct {
	Row    int                 `json:"row"`
	SKU    string              `json:"sku"`
	Size   string              `json:"size"`
	Color  string              `json:"color"`
	Issues []VariantBatchIssue `json:"issues"`
}

type VariantBatchResult struct {
	ID       string    `json:"id"`
	Variants []Variant `json:"variants"`
}

// batchCombination also protects rows created through the matrix when the
// individual editor writes later. Legacy rows outside the matrix retain
// their existing write rules and are always considered for new batch rows.
func batchCombination(ctx context.Context, tx pgx.Tx, tenantID string, v Variant, all bool) (*ExistingCombination, error) {
	var result ExistingCombination
	err := tx.QueryRow(ctx, `SELECT v.id,v.sku,v.status FROM product_variants v
		WHERE v.tenant_id=$1 AND v.product_id=$2 AND v.id IS DISTINCT FROM NULLIF($3,'')::uuid
		AND (v.size_id=$4 OR lower(btrim(v.size))=lower(btrim($5::text)))
		AND (v.color_id=$6 OR lower(btrim(v.color))=lower(btrim($7::text)))
		AND lower(v.options::text)=lower($9::text::jsonb::text)
		AND ($8 OR v.batch_created OR EXISTS(
			SELECT 1 FROM product_variants current
			WHERE current.tenant_id=$1 AND current.id=NULLIF($3,'')::uuid AND current.batch_created))
		ORDER BY v.sku,v.id LIMIT 1`, tenantID, v.ProductID, v.ID, v.SizeID, v.Size, v.ColorID, v.Color, all, optionsKey(v.Options)).
		Scan(&result.ID, &result.SKU, &result.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &result, err
}

// optionsKey is the canonical JSON of the additional attributes. Comparisons
// lower-case it, so "L32" and "l32" are the same combination.
func optionsKey(options []VariantOption) string {
	if len(options) == 0 {
		return "[]"
	}
	body, _ := json.Marshal(options)
	return string(body)
}

func sameAttributeRef(before, after *string) bool {
	if before == nil || after == nil {
		return before == after
	}
	return *before == *after
}

func protectBatchCombination(ctx context.Context, tx pgx.Tx, tenantID string, v Variant) error {
	existing, err := batchCombination(ctx, tx, tenantID, v, false)
	if err != nil {
		return err
	}
	if existing != nil {
		return ErrAttributeCombination
	}
	return nil
}

func requireBatchProduct(ctx context.Context, tx pgx.Tx, tenantID, productID string) error {
	var id string
	return writeError(tx.QueryRow(ctx, "SELECT id FROM products WHERE tenant_id=$1 AND id=$2 AND status='active'", tenantID, productID).Scan(&id))
}

func checkBatchSize(productID string, rows []BatchVariantInput) error {
	if _, ok := parseID(productID); !ok {
		return ErrInvalid
	}
	if len(rows) < 1 || len(rows) > VariantBatchLimit {
		return ErrBatchLimit
	}
	return nil
}

func (s Service) PreviewVariantBatch(ctx context.Context, actor auth.Identity, productID string, rows []BatchVariantInput) ([]BatchPreviewRow, error) {
	if err := checkBatchSize(productID, rows); err != nil {
		return nil, err
	}
	var result []BatchPreviewRow
	err := s.withAccess(ctx, actor, false, func(tx pgx.Tx, manage bool) error {
		if !manage {
			return auth.ErrForbidden
		}
		if err := requireBatchProduct(ctx, tx, actor.TenantID, productID); err != nil {
			return err
		}
		var err error
		result, _, err = prepareVariantBatch(ctx, tx, actor.TenantID, productID, rows, true)
		return err
	})
	return result, err
}

func (s Service) CreateVariantBatch(ctx context.Context, actor auth.Identity, productID, key string, rows []BatchVariantInput) (VariantBatchResult, error) {
	if err := checkBatchSize(productID, rows); err != nil {
		return VariantBatchResult{}, err
	}
	if !variantCreateKey.MatchString(key) {
		return VariantBatchResult{}, &FieldError{"idempotencyKey", "Indicá una clave de idempotencia válida para el lote."}
	}
	for i := range rows {
		rows[i].SKU = strings.TrimSpace(rows[i].SKU)
		if rows[i].Status == "" {
			rows[i].Status = "active"
		}
	}
	body, err := json.Marshal(struct {
		ProductID string
		Rows      []BatchVariantInput
	}{productID, rows})
	if err != nil {
		return VariantBatchResult{}, err
	}
	hash := sha256.Sum256(body)
	result := VariantBatchResult{}
	err = s.withAccess(ctx, actor, true, func(tx pgx.Tx, _ bool) error {
		var creator string
		var oldHash, response []byte
		err := tx.QueryRow(ctx, `SELECT actor_id,request_hash,response FROM catalog_variant_batches
			WHERE tenant_id=$1 AND idempotency_key=$2`, actor.TenantID, key).Scan(&creator, &oldHash, &response)
		if err == nil {
			if creator != actor.UserID || !bytes.Equal(oldHash, hash[:]) {
				return ErrVariantCreateKey
			}
			return json.Unmarshal(response, &result)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if err := requireBatchProduct(ctx, tx, actor.TenantID, productID); err != nil {
			return err
		}
		preview, variants, err := prepareVariantBatch(ctx, tx, actor.TenantID, productID, rows, false)
		if err != nil {
			return err
		}
		var collected []VariantBatchIssue
		for _, row := range preview {
			collected = append(collected, row.Issues...)
		}
		if len(collected) > 0 {
			return &VariantBatchError{Issues: collected}
		}
		if err := tx.QueryRow(ctx, "SELECT gen_random_uuid()").Scan(&result.ID); err != nil {
			return err
		}
		for i := range variants {
			v := &variants[i]
			if rows[i].GenerateInternal {
				if err := generateInternal(ctx, tx, actor.TenantID, v); err != nil {
					return batchWriteFailure(i+1, err)
				}
			}
			if err := insertVariant(ctx, tx, actor.TenantID, v, false); err != nil {
				return batchWriteFailure(i+1, err)
			}
		}
		result.Variants = variants
		_, err = tx.Exec(ctx, `INSERT INTO catalog_variant_batches(id,tenant_id,product_id,actor_id,idempotency_key,request_hash,response)
			VALUES($1,$2,$3,$4,$5,$6,$7)`, result.ID, actor.TenantID, productID, actor.UserID, key, hash[:], result)
		return err
	})
	if err != nil {
		return VariantBatchResult{}, err
	}
	return result, nil
}

func batchWriteFailure(row int, err error) error {
	issue := VariantBatchIssue{Row: row, Field: "sku", Code: "variant_write_failed", Message: "No se pudo crear esta variante."}
	var field *FieldError
	switch {
	case errors.Is(err, ErrSKU):
		issue.Code, issue.Message = "sku_taken", err.Error()
	case errors.Is(err, ErrBarcode), errors.Is(err, ErrIdentifierCapacity):
		issue.Field, issue.Code, issue.Message = "generateInternal", "internal_barcode_unavailable", err.Error()
	case errors.As(err, &field):
		issue.Field, issue.Code, issue.Message = field.Field, "invalid_field", field.Message
	default:
		slog.Error("falló una fila del lote de Variantes", "row", row, "error_type", fmt.Sprintf("%T", err))
	}
	return &VariantBatchError{Issues: []VariantBatchIssue{issue}}
}

func prepareVariantBatch(ctx context.Context, tx pgx.Tx, tenantID, productID string, inputs []BatchVariantInput, preview bool) ([]BatchPreviewRow, []Variant, error) {
	result := make([]BatchPreviewRow, len(inputs))
	variants := make([]Variant, len(inputs))
	excluded := make([]string, 0, len(inputs))
	if preview {
		for _, entry := range inputs {
			// Suggestions may also have to avoid a manually entered SKU
			// with different letter case under the configured prefix.
			if value := strings.TrimSpace(entry.SKU); value != "" {
				excluded = append(excluded, value, strings.ToUpper(value))
			}
		}
	}
	candidates := identifierCounters{}
	if preview {
		var err error
		candidates, err = counters(ctx, tx, tenantID)
		if err != nil {
			return nil, nil, err
		}
	}
	seenPairs := make(map[[2]string]bool, len(inputs))
	seenSKU := make(map[string]bool, len(inputs))
	for i, input := range inputs {
		row := BatchPreviewRow{Row: i + 1, SKU: strings.TrimSpace(input.SKU), Issues: []VariantBatchIssue{}}
		add := func(field, code, message string) {
			row.Issues = append(row.Issues, VariantBatchIssue{Row: i + 1, Field: field, Code: code, Message: message})
		}
		v := Variant{ProductID: productID, Status: input.Status, batchCreated: true, VariantInput: VariantInput{Barcodes: []string{}}}
		if v.Status == "" {
			v.Status = "active"
		}
		if v.Status != "active" && v.Status != "inactive" {
			add("status", "invalid_field", "Elegí un estado inicial válido.")
		}
		for _, attr := range []struct {
			ref   *BatchAttributeRef
			kind  AttributeKind
			field string
			id    **string
			name  *string
		}{
			{input.Size, Sizes, "size", &v.SizeID, &v.Size}, {input.Color, Colors, "color", &v.ColorID, &v.Color},
		} {
			if attr.ref == nil {
				continue
			}
			name, code, err := batchAttribute(ctx, tx, tenantID, attr.kind, *attr.ref)
			if err != nil {
				return nil, nil, err
			}
			if code != "" {
				add(attr.field, code, "La opción cambió, está inactiva o ya no está disponible. Revisá el catálogo.")
				continue
			}
			*attr.name = name
			*attr.id = &attr.ref.ID
		}
		row.Size, row.Color = v.Size, v.Color
		if len(row.Issues) == 0 {
			var pair [2]string
			if v.SizeID != nil {
				pair[0] = *v.SizeID
			}
			if v.ColorID != nil {
				pair[1] = *v.ColorID
			}
			if seenPairs[pair] {
				add("combination", "batch_duplicate", "La combinación está repetida en el lote.")
			} else {
				seenPairs[pair] = true
				existing, err := batchCombination(ctx, tx, tenantID, v, true)
				if err != nil {
					return nil, nil, err
				}
				if existing != nil {
					add("combination", "attribute_combination", "La combinación ya existe.")
					row.Issues[len(row.Issues)-1].Existing = existing
				}
			}
		}
		if preview && row.SKU == "" && len(row.Issues) == 0 {
			value, n, err := nextIdentifier(ctx, tx, tenantID, "sku", candidates.Prefix, candidates.Width, candidates.sku+1, excluded)
			if errors.Is(err, ErrIdentifierCapacity) {
				add("sku", "identifier_capacity", err.Error())
			} else if err != nil {
				return nil, nil, err
			} else {
				row.SKU, candidates.sku = value, n
			}
		}
		if row.SKU != "" || !preview {
			if !validText(row.SKU, 80, true) {
				add("sku", "invalid_field", "Ingresá un SKU de hasta 80 caracteres.")
			} else {
				var normalized string
				var used, retired bool
				err := tx.QueryRow(ctx, `SELECT lower($2::text), EXISTS(
					SELECT 1 FROM product_variants WHERE tenant_id=$1 AND lower(sku)=lower($2::text)
				), EXISTS(SELECT 1 FROM catalog_identifier_events WHERE tenant_id=$1
					AND kind='sku' AND action='retired' AND normalized=lower($2::text))`, tenantID, row.SKU).Scan(&normalized, &used, &retired)
				if err != nil {
					return nil, nil, err
				}
				switch {
				case used:
					add("sku", "sku_taken", ErrSKU.Error())
				case retired:
					add("sku", "identifier_retired", ErrIdentifierRetired.Error())
				case seenSKU[normalized]:
					add("sku", "batch_duplicate", "El SKU está repetido en el lote.")
				}
				seenSKU[normalized] = true
			}
		}
		v.SKU = row.SKU
		result[i], variants[i] = row, v
	}
	return result, variants, nil
}

func batchAttribute(ctx context.Context, tx pgx.Tx, tenantID string, kind AttributeKind, ref BatchAttributeRef) (name, code string, err error) {
	id, valid := parseID(ref.ID)
	if !valid || ref.Revision < 1 {
		return "", "invalid_field", nil
	}
	var revision int64
	var status string
	err = tx.QueryRow(ctx, "SELECT name,status,revision FROM catalog_attributes WHERE tenant_id=$1 AND kind=$2 AND id=$3", tenantID, kind, id).Scan(&name, &status, &revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "attribute_unavailable", nil
	}
	if err != nil {
		return "", "", err
	}
	if status != "active" {
		return "", "attribute_inactive", nil
	}
	if revision != ref.Revision {
		return "", "attribute_changed", nil
	}
	return name, "", nil
}
