package catalog

import (
	"context"
	"errors"
	"regexp"
	"strings"

	"retail-platform/api/internal/modules/auth"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type AttributeKind string

const (
	Sizes      AttributeKind = "sizes"
	Colors     AttributeKind = "colors"
	SizeGroups AttributeKind = "size-groups"
)

var (
	ErrAttributeCombination = errors.New("ya existe una variante con esa combinación de talle y color")
	ErrAttributeInactive    = errors.New("el valor de catálogo está inactivo")
	ErrAttributeChanged     = errors.New("cambió la etiqueta del catálogo. Actualizá las opciones y revisá la selección")
	hexColor                = regexp.MustCompile(`^#[0-9a-f]{6}$`)
)

type AttributeDuplicate struct{ Field string }

func (e *AttributeDuplicate) Error() string {
	return "ya existe un valor equivalente en el catálogo del comercio"
}

type AttributeData struct {
	Name     string  `json:"name"`
	Code     *string `json:"code"`
	Position int     `json:"position"`
	Status   string  `json:"status"`
	Hex      *string `json:"hex"`
	GroupID  *string `json:"groupId"`
}

type AttributeWrite struct {
	AttributeData
	ExpectedRevision int64 `json:"expectedRevision"`
}

type Attribute struct {
	ID string `json:"id"`
	AttributeData
	Revision int64 `json:"revision"`
}

type PendingVariant struct {
	Variant
	ProductName string `json:"productName"`
}

type PendingAttributes struct {
	Items   []PendingVariant `json:"items"`
	HasMore bool             `json:"hasMore"`
}

const attributeFields = "id,name,code,position,status,hex,group_id,revision"

func validAttributeKind(kind AttributeKind) bool {
	return kind == Sizes || kind == Colors || kind == SizeGroups
}

func (s Service) Attributes(ctx context.Context, actor auth.Identity, kind AttributeKind) ([]Attribute, error) {
	if !validAttributeKind(kind) {
		return nil, ErrInvalid
	}
	var result []Attribute
	err := s.withAccess(ctx, actor, false, func(tx pgx.Tx, _ bool) error {
		rows, err := tx.Query(ctx, "SELECT "+attributeFields+` FROM catalog_attributes a
			WHERE tenant_id=$1 AND kind=$2 ORDER BY position,lower(name),id`, actor.TenantID, kind)
		if err != nil {
			return err
		}
		result, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Attribute, error) { return scanAttribute(row) })
		return err
	})
	return result, err
}

func scanAttribute(row pgx.Row) (Attribute, error) {
	var a Attribute
	err := row.Scan(&a.ID, &a.Name, &a.Code, &a.Position, &a.Status, &a.Hex, &a.GroupID, &a.Revision)
	return a, writeError(err)
}

func (s Service) SaveAttribute(ctx context.Context, actor auth.Identity, kind AttributeKind, id string, input AttributeWrite) (Attribute, error) {
	if !validAttributeKind(kind) {
		return Attribute{}, ErrInvalid
	}
	if id != "" {
		if _, ok := parseID(id); !ok {
			return Attribute{}, ErrInvalid
		}
	}
	if id == "" && input.Status == "" {
		input.Status = "active"
	}
	if err := validateAttribute(kind, &input.AttributeData); err != nil {
		return Attribute{}, err
	}
	var result Attribute
	err := s.withAccess(ctx, actor, true, func(tx pgx.Tx, _ bool) error {
		var previous Attribute
		var err error
		if id != "" {
			previous, err = scanAttribute(tx.QueryRow(ctx, "SELECT "+attributeFields+
				" FROM catalog_attributes WHERE tenant_id=$1 AND kind=$2 AND id=$3", actor.TenantID, kind, id))
			if err != nil {
				return err
			}
			if err := requireRevision(input.ExpectedRevision, previous.Revision); err != nil {
				return err
			}
		}
		if input.GroupID != nil && (previous.GroupID == nil || *previous.GroupID != *input.GroupID) {
			if _, err := attributeName(ctx, tx, actor.TenantID, SizeGroups, "groupId", *input.GroupID); err != nil {
				return err
			}
		}
		if id == "" {
			result, err = scanAttribute(tx.QueryRow(ctx, `INSERT INTO catalog_attributes(tenant_id,kind,name,code,position,status,hex,group_id)
				VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING `+attributeFields,
				actor.TenantID, kind, input.Name, input.Code, input.Position, input.Status, input.Hex, input.GroupID))
		} else {
			result, err = scanAttribute(tx.QueryRow(ctx, `UPDATE catalog_attributes
				SET name=$4,code=$5,position=$6,status=$7,hex=$8,group_id=$9,revision=revision+1
				WHERE tenant_id=$1 AND kind=$2 AND id=$3 AND revision=$10 RETURNING `+attributeFields,
				actor.TenantID, kind, id, input.Name, input.Code, input.Position, input.Status, input.Hex, input.GroupID, input.ExpectedRevision))
			if errors.Is(err, ErrNotFound) {
				return ErrRevisionConflict
			}
		}
		var conflict *pgconn.PgError
		if errors.As(err, &conflict) && conflict.Code == "23505" {
			switch conflict.ConstraintName {
			case "catalog_attributes_name_key":
				return &AttributeDuplicate{Field: "name"}
			case "catalog_attributes_code_key":
				return &AttributeDuplicate{Field: "code"}
			}
		}
		return err
	})
	return result, err
}

func validateAttribute(kind AttributeKind, input *AttributeData) error {
	input.Name = strings.TrimSpace(input.Name)
	if !validText(input.Name, 60, true) {
		return &FieldError{"name", "Ingresá un nombre de hasta 60 caracteres."}
	}
	if input.Code != nil {
		code := strings.ToLower(strings.TrimSpace(*input.Code))
		input.Code = nil
		if code != "" {
			input.Code = &code
		}
	}
	if kind == Colors && input.Code == nil {
		code := strings.ToLower(input.Name)
		input.Code = &code
	}
	if input.Code != nil && (!validText(*input.Code, 60, true) || kind == SizeGroups) {
		return &FieldError{"code", "Ingresá un código de hasta 60 caracteres cuando corresponda."}
	}
	if input.Position < 0 || input.Position > 9999 {
		return &FieldError{"position", "Ingresá un orden entre 0 y 9999."}
	}
	if input.Status != "active" && input.Status != "inactive" {
		return &FieldError{"status", "Elegí un estado válido."}
	}
	if input.GroupID != nil && kind != Sizes {
		return &FieldError{"groupId", "Solo los talles pueden pertenecer a un grupo."}
	}
	if input.Hex != nil {
		hex := strings.ToLower(strings.TrimSpace(*input.Hex))
		if kind != Colors || !hexColor.MatchString(hex) {
			return &FieldError{"hex", "Ingresá un color con formato #rrggbb."}
		}
		input.Hex = &hex
	}
	return nil
}

func attributeName(ctx context.Context, tx pgx.Tx, tenantID string, kind AttributeKind, field, id string) (string, error) {
	parsed, ok := parseID(id)
	if !ok {
		return "", &FieldError{field, "Elegí un valor de catálogo válido."}
	}
	var name, status string
	err := tx.QueryRow(ctx, "SELECT name,status FROM catalog_attributes WHERE tenant_id=$1 AND kind=$2 AND id=$3", tenantID, kind, parsed).Scan(&name, &status)
	if err != nil {
		return "", writeError(err)
	}
	if status != "active" {
		return "", ErrAttributeInactive
	}
	return name, nil
}

func (s Service) PendingAttributeVariants(ctx context.Context, actor auth.Identity, query string, offset int) (PendingAttributes, error) {
	const limit = 25
	query = strings.TrimSpace(query)
	if !validText(query, 160, false) || offset < 0 || offset > 100000 {
		return PendingAttributes{}, ErrInvalid
	}
	result := PendingAttributes{}
	err := s.withAccess(ctx, actor, false, func(tx pgx.Tx, manage bool) error {
		if !manage {
			return auth.ErrForbidden
		}
		rows, err := tx.Query(ctx, `SELECT `+variantFields+`,p.name FROM product_variants v
			JOIN products p ON (p.tenant_id,p.id)=(v.tenant_id,v.product_id)
			WHERE v.tenant_id=$1 AND ((v.size_id IS NULL AND v.size<>'') OR (v.color_id IS NULL AND v.color<>''))
			AND ($2='' OR strpos(lower(v.sku),lower($2))>0 OR strpos(lower(p.name),lower($2))>0
				OR strpos(lower(v.size),lower($2))>0 OR strpos(lower(v.color),lower($2))>0)
			ORDER BY v.sku,v.id LIMIT $3 OFFSET $4`, actor.TenantID, query, limit+1, offset)
		if err != nil {
			return err
		}
		result.Items, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (PendingVariant, error) {
			var item PendingVariant
			args := append(variantScanTargets(&item.Variant), &item.ProductName)
			err := row.Scan(args...)
			return item, err
		})
		if len(result.Items) > limit {
			result.Items = result.Items[:limit]
			result.HasMore = offset+limit <= 100000
		}
		return err
	})
	return result, err
}

// Master-assigned combinations must not create duplicates. Existing legacy
// duplicates keep their UUIDs and can be linked without changing their texts.
func verifyMasterCombination(ctx context.Context, tx pgx.Tx, tenantID string, variant Variant) error {
	if variant.SizeID == nil && variant.ColorID == nil {
		return nil
	}
	var present bool
	err := tx.QueryRow(ctx, `SELECT EXISTS(
		SELECT 1 FROM product_variants
		WHERE tenant_id=$1 AND product_id=$2 AND id IS DISTINCT FROM NULLIF($3,'')::uuid
		  AND (size_id=$4 OR size COLLATE "C"=$5::text COLLATE "C")
		  AND (color_id=$6 OR color COLLATE "C"=$7::text COLLATE "C")
		  AND lower(options::text)=lower($8::text::jsonb::text)
	)`, tenantID, variant.ProductID, variant.ID, variant.SizeID, variant.Size, variant.ColorID, variant.Color, optionsKey(variant.Options)).Scan(&present)
	if err == nil && present {
		return ErrAttributeCombination
	}
	return err
}
