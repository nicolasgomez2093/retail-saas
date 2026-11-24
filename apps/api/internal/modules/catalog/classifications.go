package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"retail-platform/api/internal/modules/auth"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var ErrClassificationNotFound = errors.New("categoría o marca inexistente")
var ErrClassificationName = errors.New("ese nombre ya está registrado")

type ClassificationKind string

const (
	Categories ClassificationKind = "categories"
	Brands     ClassificationKind = "brands"
)

type Term struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type Classifications struct {
	Categories []Term `json:"categories"`
	Brands     []Term `json:"brands"`
}

func (s Service) Classifications(ctx context.Context, actor auth.Identity) (Classifications, error) {
	var result Classifications
	err := s.withAccess(ctx, actor, false, func(tx pgx.Tx, _ bool) error {
		var err error
		result.Categories, err = listTerms(ctx, tx, actor.TenantID, Categories)
		if err != nil {
			return err
		}
		result.Brands, err = listTerms(ctx, tx, actor.TenantID, Brands)
		return err
	})
	return result, err
}

func (s Service) Terms(ctx context.Context, actor auth.Identity, kind ClassificationKind) ([]Term, error) {
	if kind != Categories && kind != Brands {
		return nil, ErrInvalid
	}
	var terms []Term
	err := s.withAccess(ctx, actor, false, func(tx pgx.Tx, _ bool) error {
		var err error
		terms, err = listTerms(ctx, tx, actor.TenantID, kind)
		return err
	})
	return terms, err
}

func (s Service) SaveTerm(ctx context.Context, actor auth.Identity, kind ClassificationKind, id, name string) (Term, error) {
	name = strings.TrimSpace(name)
	uuid, valid := parseID(id)
	if !validText(name, 120, true) || (id != "" && !valid) || (kind != Categories && kind != Brands) {
		return Term{}, ErrInvalid
	}
	table := pgx.Identifier{string(kind)}.Sanitize()
	var term Term
	err := s.withAccess(ctx, actor, true, func(tx pgx.Tx, _ bool) error {
		var row pgx.Row
		if id == "" {
			row = tx.QueryRow(ctx, "INSERT INTO "+table+"(tenant_id,name) VALUES ($1,$2) RETURNING id,name", actor.TenantID, name)
		} else {
			row = tx.QueryRow(ctx, "UPDATE "+table+" SET name=$3 WHERE tenant_id=$1 AND id=$2 RETURNING id,name", actor.TenantID, uuid, name)
		}
		err := row.Scan(&term.ID, &term.Name)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrClassificationNotFound
		}
		var conflict *pgconn.PgError
		if errors.As(err, &conflict) && conflict.ConstraintName == string(kind)+"_tenant_name" {
			return ErrClassificationName
		}
		return err
	})
	return term, err
}

func listTerms(ctx context.Context, tx pgx.Tx, tenantID string, kind ClassificationKind) ([]Term, error) {
	table := pgx.Identifier{string(kind)}.Sanitize()
	rows, err := tx.Query(ctx, "SELECT id,name FROM "+table+" WHERE tenant_id=$1 ORDER BY name,id", tenantID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[Term])
}

func applyClassification(raw json.RawMessage, current **string) error {
	if len(raw) == 0 {
		return nil
	}
	var id *string
	if err := json.Unmarshal(raw, &id); err != nil {
		return ErrInvalid
	}
	*current = id
	return nil
}

func validateClassifications(ctx context.Context, tx pgx.Tx, tenantID string, product Product) error {
	if product.CategoryID == nil && product.BrandID == nil {
		return nil
	}
	for _, id := range []*string{product.CategoryID, product.BrandID} {
		if id != nil {
			if _, valid := parseID(*id); !valid {
				return ErrInvalid
			}
		}
	}
	var exists bool
	err := tx.QueryRow(ctx, `SELECT
		($2::uuid IS NULL OR EXISTS(SELECT 1 FROM categories WHERE tenant_id=$1 AND id=$2)) AND
		($3::uuid IS NULL OR EXISTS(SELECT 1 FROM brands WHERE tenant_id=$1 AND id=$3))`,
		tenantID, product.CategoryID, product.BrandID).Scan(&exists)
	if err == nil && !exists {
		return ErrClassificationNotFound
	}
	return err
}
