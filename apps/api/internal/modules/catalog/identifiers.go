package catalog

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"retail-platform/api/internal/modules/auth"

	"github.com/jackc/pgx/v5"
)

const maxSKUNumber = int64(999999999999)

var (
	ErrIdentifierRetired  = errors.New("el identificador está retirado. Consultá su historial antes de solicitar la reutilización")
	ErrBarcodeReuse       = errors.New("el código pertenece al historial de otra variante y sus etiquetas anteriores podrían seguir en circulación")
	ErrInternalExists     = errors.New("la variante ya tiene un código interno. Conservá ese código o retiralo en una operación separada")
	ErrIdentifierCapacity = errors.New("no hay un identificador libre en el intervalo consultado. Revisá la configuración de numeración")
	ErrVariantCreateKey   = errors.New("la clave ya se usó con otra alta de variante")
	identifierPrefix      = regexp.MustCompile(`^([A-Z][A-Z0-9_-]{0,11})?$`)
	variantCreateKey      = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)
)

type IdentifierSettings struct {
	Prefix   string `json:"skuPrefix"`
	Width    int    `json:"skuWidth"`
	Revision int64  `json:"revision"`
}

type Suggestion struct {
	SKU     string `json:"sku"`
	Ordinal int64  `json:"ordinal"`
}

type IdentifierEvent struct {
	Value     string    `json:"value"`
	VariantID string    `json:"variantId"`
	Action    string    `json:"action"`
	Internal  bool      `json:"internal"`
	ActorID   *string   `json:"actorId"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"createdAt"`
}

type identifierCounters struct {
	IdentifierSettings
	sku, barcode int64
}

func counters(ctx context.Context, tx pgx.Tx, tenantID string) (identifierCounters, error) {
	result := identifierCounters{IdentifierSettings: IdentifierSettings{Width: 6, Revision: 1}}
	err := tx.QueryRow(ctx, `SELECT sku_prefix,sku_width,revision,sku_number,barcode_number
		FROM catalog_identifier_sequences WHERE tenant_id=$1`, tenantID).
		Scan(&result.Prefix, &result.Width, &result.Revision, &result.sku, &result.barcode)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return result, err
}

func ensureCounters(ctx context.Context, tx pgx.Tx, tenantID string) error {
	_, err := tx.Exec(ctx, "INSERT INTO catalog_identifier_sequences(tenant_id) VALUES ($1) ON CONFLICT DO NOTHING", tenantID)
	return err
}

func (s Service) IdentifierSettings(ctx context.Context, actor auth.Identity) (IdentifierSettings, error) {
	var settings IdentifierSettings
	err := s.withAccess(ctx, actor, false, func(tx pgx.Tx, manage bool) error {
		if !manage {
			return auth.ErrForbidden
		}
		values, err := counters(ctx, tx, actor.TenantID)
		settings = values.IdentifierSettings
		return err
	})
	return settings, err
}

func (s Service) ConfigureIdentifiers(ctx context.Context, actor auth.Identity, input IdentifierSettings, expected int64) (IdentifierSettings, error) {
	input.Prefix = strings.ToUpper(strings.TrimSpace(input.Prefix))
	if !identifierPrefix.MatchString(input.Prefix) {
		return IdentifierSettings{}, &FieldError{"skuPrefix", "Usá hasta 12 caracteres ASCII; el prefijo empieza con una letra."}
	}
	if input.Width < 4 || input.Width > 12 {
		return IdentifierSettings{}, &FieldError{"skuWidth", "Elegí entre 4 y 12 dígitos."}
	}
	err := s.withAccess(ctx, actor, true, func(tx pgx.Tx, _ bool) error {
		if err := ensureCounters(ctx, tx, actor.TenantID); err != nil {
			return err
		}
		active, err := counters(ctx, tx, actor.TenantID)
		if err != nil {
			return err
		}
		if err := requireRevision(expected, active.Revision); err != nil {
			return err
		}
		if active.sku >= maxOrdinal(input.Width) {
			return ErrIdentifierCapacity
		}
		return tx.QueryRow(ctx, `UPDATE catalog_identifier_sequences SET sku_prefix=$2,sku_width=$3,revision=revision+1
			WHERE tenant_id=$1 RETURNING revision`, actor.TenantID, input.Prefix, input.Width).Scan(&input.Revision)
	})
	return input, err
}

func maxOrdinal(width int) int64 {
	result := int64(1)
	for i := 0; i < width; i++ {
		result *= 10
	}
	return result - 1
}

func (s Service) SuggestSKU(ctx context.Context, actor auth.Identity, after int64) (Suggestion, error) {
	if after < 0 || after > maxSKUNumber {
		return Suggestion{}, ErrInvalid
	}
	var result Suggestion
	err := s.withAccess(ctx, actor, false, func(tx pgx.Tx, manage bool) error {
		if !manage {
			return auth.ErrForbidden
		}
		state, err := counters(ctx, tx, actor.TenantID)
		if err != nil {
			return err
		}
		from := max(state.sku, after) + 1
		value, n, err := nextIdentifier(ctx, tx, actor.TenantID, "sku", state.Prefix, state.Width, from, nil)
		result = Suggestion{value, n}
		return err
	})
	return result, err
}

func nextIdentifier(ctx context.Context, tx pgx.Tx, tenantID, kind, prefix string, width int, from int64, exclude []string) (string, int64, error) {
	var n int64
	var value string
	upper := min(from+4095, maxOrdinal(width))
	// Fast path for sparse identifiers. This read is advisory for SKU. Barcode allocation invokes it
	// while holding the tenant write lock and commits the counter with the code.
	err := tx.QueryRow(ctx, `SELECT x.label,n
		FROM generate_series($5::bigint,$6::bigint) n
		CROSS JOIN LATERAL (SELECT $3::text || lpad(n::text,$4::int,'0') AS label) x
		WHERE NOT EXISTS(SELECT 1 FROM catalog_identifier_events e
			WHERE e.tenant_id=$1 AND e.kind=$2 AND e.action='retired'
			AND e.normalized=CASE WHEN $2='sku' THEN lower(x.label) ELSE x.label END)
		AND ($2<>'sku' OR NOT EXISTS(SELECT 1 FROM product_variants v WHERE v.tenant_id=$1 AND lower(v.sku)=lower(x.label)))
		AND ($2<>'barcode' OR NOT EXISTS(SELECT 1 FROM barcodes b WHERE b.tenant_id=$1 AND b.code=x.label))
		AND NOT (x.label=ANY(COALESCE($7::text[],ARRAY[]::text[])))
		ORDER BY n LIMIT 1`, tenantID, kind, prefix, width, from, upper, exclude).Scan(&value, &n)
	if !errors.Is(err, pgx.ErrNoRows) {
		return value, n, err
	}
	if upper == maxOrdinal(width) {
		return "", 0, ErrIdentifierCapacity
	}
	// A saturated window can contain old manual numbers. Find the first gap
	// after it by looking at assigned/retired values, without walking the
	// remainder of the numeric range.
	err = tx.QueryRow(ctx, `WITH occupied(label) AS (
		SELECT lower(sku) FROM product_variants WHERE tenant_id=$1 AND $2='sku'
		UNION ALL SELECT code FROM barcodes WHERE tenant_id=$1 AND $2='barcode'
		UNION ALL SELECT normalized FROM catalog_identifier_events WHERE tenant_id=$1 AND kind=$2 AND action='retired'
		UNION ALL SELECT CASE WHEN $2='sku' THEN lower(label) ELSE label END FROM unnest($7::text[]) label
	), serials AS MATERIALIZED (
		SELECT DISTINCT CASE WHEN substring(label FROM char_length($3::text)+1)~'^[0-9]+$'
			THEN substring(label FROM char_length($3::text)+1)::bigint END AS n
		FROM occupied
		WHERE char_length(label)=char_length($3::text)+$4::int
			AND starts_with(label,CASE WHEN $2='sku' THEN lower($3::text) ELSE $3 END)
	), candidates(n) AS (
		SELECT $5::bigint
		UNION ALL SELECT n+1 FROM serials WHERE n >= $5::bigint AND n < $6::bigint
	)
	SELECT $3 || lpad(c.n::text,$4,'0'),c.n FROM candidates c
	WHERE c.n <= $6::bigint AND NOT EXISTS(SELECT 1 FROM serials s WHERE s.n=c.n)
	ORDER BY c.n LIMIT 1`, tenantID, kind, prefix, width, upper+1, maxOrdinal(width), exclude).Scan(&value, &n)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrIdentifierCapacity
	}
	return value, n, err
}

func advanceSKU(ctx context.Context, tx pgx.Tx, tenantID, value string) error {
	state, err := counters(ctx, tx, tenantID)
	if err != nil {
		return err
	}
	digits, match := strings.CutPrefix(strings.ToUpper(value), state.Prefix)
	if !match || len(digits) != state.Width {
		return nil
	}
	n, err := strconv.ParseInt(digits, 10, 64)
	if err != nil || n <= state.sku || fmt.Sprintf("%0*d", state.Width, n) != digits {
		return nil
	}
	if err := ensureCounters(ctx, tx, tenantID); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "UPDATE catalog_identifier_sequences SET sku_number=$2 WHERE tenant_id=$1", tenantID, n)
	return err
}

func generateInternal(ctx context.Context, tx pgx.Tx, tenantID string, v *Variant) error {
	if v.InternalBarcode != nil {
		return ErrInternalExists
	}
	if len(v.Barcodes) >= 20 {
		return &FieldError{"barcodes", "El límite de 20 incluye el código interno."}
	}
	state, err := counters(ctx, tx, tenantID)
	if err != nil {
		return err
	}
	code, n, err := nextIdentifier(ctx, tx, tenantID, "barcode", internalBarcodePrefix(tenantID), 10, state.barcode+1, v.Barcodes)
	if err != nil {
		return err
	}
	if err := ensureCounters(ctx, tx, tenantID); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE catalog_identifier_sequences SET barcode_number=$2 WHERE tenant_id=$1", tenantID, n); err != nil {
		return err
	}
	v.InternalBarcode = &code
	v.Barcodes = append(v.Barcodes, code)
	slices.Sort(v.Barcodes[1:])
	// The existing first code remains principal.
	return nil
}

// internalBarcodePrefix returns the prefix of new internal codes: "2" plus
// five digits derived from the tenant, followed by a ten-digit sequence.
// Sixteen digits are encoded by Code 128 in subset C (two digits per symbol
// character), so the symbol fits a 60 × 40 mm label with its quiet zones.
// Sixteen is not a GTIN length (8, 12, 13 or 14), so the value is never
// mistaken for an official EAN/UPC. Codes issued earlier keep their format.
func internalBarcodePrefix(tenantID string) string {
	namespace, err := strconv.ParseUint(strings.ReplaceAll(tenantID, "-", "")[:8], 16, 64)
	if err != nil {
		namespace = 0
	}
	return fmt.Sprintf("2%05d", namespace%100000)
}

type identifierChange struct {
	kind, value string
	internal    bool
}

type identifierAuthorization struct {
	reuse, confirmed bool
	reason           string
}

func protectIdentifiers(ctx context.Context, tx pgx.Tx, actor auth.Identity, before *Variant, after *Variant, authorization identifierAuthorization) ([]identifierChange, error) {
	var oldCodes []string
	skuChanged := true
	if before != nil {
		oldCodes = before.Barcodes
		if err := tx.QueryRow(ctx, "SELECT lower($1::text)<>lower($2::text)", before.SKU, after.SKU).Scan(&skuChanged); err != nil {
			return nil, err
		}
	}
	additions := make([]identifierChange, 0, len(after.Barcodes)+1)
	if skuChanged {
		additions = append(additions, identifierChange{"sku", after.SKU, false})
	}
	for _, code := range after.Barcodes {
		if !includesIdentifier(oldCodes, code) {
			additions = append(additions, identifierChange{"barcode", code, false})
		}
	}
	var reused []identifierChange
	for _, addition := range additions {
		origins, err := retiredOrigins(ctx, tx, actor.TenantID, addition)
		if err != nil {
			return nil, err
		}
		if len(origins) == 0 {
			continue
		}
		if !authorization.reuse {
			return nil, ErrIdentifierRetired
		}
		if err := requireSensitiveAuthority(ctx, tx, actor, authorization.reason, authorization.confirmed); err != nil {
			return nil, err
		}
		for _, origin := range origins {
			if err := checkSensitiveHistory(ctx, tx, actor.TenantID, origin.VariantID); err != nil {
				return nil, err
			}
			if addition.kind == "barcode" && origin.VariantID != after.ID {
				return nil, ErrBarcodeReuse
			}
			addition.internal = addition.internal || origin.Internal
		}
		if addition.internal {
			if after.InternalBarcode != nil && *after.InternalBarcode != addition.value {
				return nil, ErrInternalExists
			}
			value := addition.value
			after.InternalBarcode = &value
		}
		reused = append(reused, addition)
	}
	if before != nil {
		var retired []identifierChange
		if skuChanged {
			retired = append(retired, identifierChange{"sku", before.SKU, false})
		}
		for _, code := range oldCodes {
			if !includesIdentifier(after.Barcodes, code) {
				retired = append(retired, identifierChange{"barcode", code, before.InternalBarcode != nil && *before.InternalBarcode == code})
			}
		}
		if err := logIdentifiers(ctx, tx, actor, before.ID, "retired", authorization.reason, retired); err != nil {
			return nil, err
		}
	}
	return reused, nil
}

func includesIdentifier(codes []string, want string) bool {
	for _, code := range codes {
		if code == want {
			return true
		}
	}
	return false
}

type identifierOrigin struct {
	VariantID string
	Internal  bool
}

func retiredOrigins(ctx context.Context, tx pgx.Tx, tenantID string, id identifierChange) ([]identifierOrigin, error) {
	rows, err := tx.Query(ctx, `SELECT variant_id,bool_or(is_internal) FROM catalog_identifier_events
		WHERE tenant_id=$1 AND kind=$2 AND action='retired' AND normalized=CASE WHEN $2='sku' THEN lower($3::text) ELSE $3 END
		GROUP BY variant_id ORDER BY variant_id`, tenantID, id.kind, id.value)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[identifierOrigin])
}

func logIdentifiers(ctx context.Context, tx pgx.Tx, actor auth.Identity, id, action, reason string, values []identifierChange) error {
	for _, value := range values {
		if _, err := tx.Exec(ctx, `INSERT INTO catalog_identifier_events(tenant_id,kind,value,variant_id,action,is_internal,actor_id,reason)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, actor.TenantID, value.kind, value.value, id, action, value.internal, actor.UserID, strings.TrimSpace(reason)); err != nil {
			return err
		}
	}
	return nil
}

func (s Service) IdentifierHistory(ctx context.Context, actor auth.Identity, kind, value string) ([]IdentifierEvent, error) {
	value = strings.TrimSpace(value)
	if (kind != "sku" && kind != "barcode") || !validText(value, 80, true) || (kind == "barcode" && !barcodePattern.MatchString(value)) {
		return nil, ErrInvalid
	}
	var result []IdentifierEvent
	err := s.withAccess(ctx, actor, false, func(tx pgx.Tx, manage bool) error {
		if !manage {
			return auth.ErrForbidden
		}
		rows, err := tx.Query(ctx, `SELECT value,variant_id,action,is_internal,actor_id,reason,created_at FROM catalog_identifier_events
			WHERE tenant_id=$1 AND kind=$2 AND normalized=CASE WHEN $2='sku' THEN lower($3::text) ELSE $3 END
			ORDER BY created_at DESC,id LIMIT 50`, actor.TenantID, kind, value)
		if err != nil {
			return err
		}
		result, err = pgx.CollectRows(rows, pgx.RowToStructByPos[IdentifierEvent])
		return err
	})
	return result, err
}
