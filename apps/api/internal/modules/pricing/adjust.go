package pricing

import (
	"context"
	"strings"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/platform/money"

	"github.com/jackc/pgx/v5"
)

// MaxAdjustProducts bounds one bulk change so the preview stays readable and the transaction short.
const MaxAdjustProducts = 500

// AdjustInput raises or lowers, in one step, the prices a list already has for the variants of some products.
// Mode "percent" takes Value as a percentage (10 = 10 %), mode "amount" as money. Round, when set, takes the new
// price to the nearest multiple of that amount (for example 100 for prices like $ 12.300).
type AdjustInput struct {
	ProductIDs []string `json:"productIds"`
	Mode       string   `json:"mode"`
	Direction  string   `json:"direction"`
	Value      string   `json:"value"`
	Round      string   `json:"round"`
	Apply      bool     `json:"apply"`
}

type AdjustLine struct {
	VariantID   string `json:"variantId"`
	ProductID   string `json:"productId"`
	ProductName string `json:"productName"`
	SKU         string `json:"sku"`
	Size        string `json:"size"`
	Color       string `json:"color"`
	Before      string `json:"before"`
	After       string `json:"after"`
}

type AdjustResult struct {
	Currency string       `json:"currency"`
	Items    []AdjustLine `json:"items"`
	// WithoutPrice counts active variants of the chosen products with no price in this list: they are left as they are.
	WithoutPrice int  `json:"withoutPrice"`
	Applied      bool `json:"applied"`
}

// newPrice applies the change in exact minor units: percentages in hundredths of a percent, half up to the cent.
func newPrice(before int64, input AdjustInput, value, round int64) (int64, bool) {
	after := before
	if input.Mode == "percent" {
		factor := int64(10000) + value
		if input.Direction == "down" {
			factor = int64(10000) - value
		}
		after = (before*factor + 5000) / 10000
	} else if input.Direction == "down" {
		after = before - value
	} else {
		after = before + value
	}
	if round > 0 {
		after = (after + round/2) / round * round
	}
	return after, after >= 0 && after <= money.MaxCents
}

func (s Service) Adjust(ctx context.Context, actor auth.Identity, listID string, input AdjustInput) (AdjustResult, error) {
	value, valid := money.Parse(strings.TrimSpace(input.Value))
	round := int64(0)
	if input.Round != "" {
		var ok bool
		if round, ok = money.Parse(input.Round); !ok || round <= 0 || round > 100000000 {
			return AdjustResult{}, ErrInvalid
		}
	}
	if !validID(listID) || !valid || value == 0 || len(input.ProductIDs) == 0 || len(input.ProductIDs) > MaxAdjustProducts ||
		(input.Mode != "percent" && input.Mode != "amount") || (input.Direction != "up" && input.Direction != "down") ||
		(input.Mode == "percent" && (value > 100000 || input.Direction == "down" && value >= 10000)) {
		return AdjustResult{}, ErrInvalid
	}
	for _, id := range input.ProductIDs {
		if !validID(id) {
			return AdjustResult{}, ErrInvalid
		}
	}
	result := AdjustResult{Items: []AdjustLine{}}
	err := s.manage(ctx, actor, input.Apply, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `SELECT t.currency_code FROM price_lists l JOIN tenants t ON t.id=l.tenant_id
			WHERE l.tenant_id=$1 AND l.id=$2`, actor.TenantID, listID).Scan(&result.Currency); err != nil {
			return noRows(err)
		}
		lock := ""
		if input.Apply {
			lock = " FOR UPDATE OF i"
		}
		rows, err := tx.Query(ctx, `SELECT v.id::text,p.id::text,p.name,v.sku,coalesce(v.size,''),coalesce(v.color,''),i.amount::text,''
			FROM products p JOIN product_variants v ON (v.tenant_id,v.product_id)=(p.tenant_id,p.id)
			JOIN price_list_items i ON (i.tenant_id,i.price_list_id,i.variant_id)=(v.tenant_id,$2,v.id)
			WHERE p.tenant_id=$1 AND p.id=ANY($3::uuid[]) AND p.status='active' AND v.status='active'
			ORDER BY lower(p.name),p.id,v.size,v.color,v.sku`+lock, actor.TenantID, listID, input.ProductIDs)
		if err != nil {
			return err
		}
		lines, err := pgx.CollectRows(rows, pgx.RowToStructByPos[AdjustLine])
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM products p JOIN product_variants v ON (v.tenant_id,v.product_id)=(p.tenant_id,p.id)
			LEFT JOIN price_list_items i ON (i.tenant_id,i.price_list_id,i.variant_id)=(v.tenant_id,$2,v.id)
			WHERE p.tenant_id=$1 AND p.id=ANY($3::uuid[]) AND p.status='active' AND v.status='active' AND i.variant_id IS NULL`,
			actor.TenantID, listID, input.ProductIDs).Scan(&result.WithoutPrice); err != nil {
			return err
		}
		for index := range lines {
			before, ok := money.Parse(lines[index].Before)
			if !ok {
				return ErrInvalid
			}
			after, ok := newPrice(before, input, value, round)
			if !ok {
				return ErrNegative
			}
			lines[index].Before = money.Format(before)
			lines[index].After = money.Format(after)
		}
		result.Items = lines
		if !input.Apply {
			return nil
		}
		for _, line := range lines {
			if line.Before == line.After {
				continue
			}
			if _, err := tx.Exec(ctx, `UPDATE price_list_items SET amount=$4::text::numeric
				WHERE tenant_id=$1 AND price_list_id=$2 AND variant_id=$3`, actor.TenantID, listID, line.VariantID, line.After); err != nil {
				return err
			}
			before, after := line.Before, line.After
			if err := audit(ctx, tx, actor, "set_price", &listID, nil, &line.VariantID, Item{Amount: &before}, Item{Amount: &after}); err != nil {
				return err
			}
		}
		result.Applied = true
		return nil
	})
	return result, err
}
