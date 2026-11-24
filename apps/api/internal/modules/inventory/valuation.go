package inventory

import (
	"context"
	"encoding/csv"
	"io"
	"strconv"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/platform/csvtext"

	"github.com/jackc/pgx/v5"
)

type Valuation struct {
	Currency  string              `json:"currency"`
	Locations []LocationValuation `json:"locations"`
	ValuationAmounts
}

func (s Service) ExportValuationCSV(ctx context.Context, actor auth.Identity, storeID string, output io.Writer) error {
	value, err := s.Valuation(ctx, actor, storeID)
	if err != nil {
		return err
	}
	if _, err := io.WriteString(output, "\ufeff"); err != nil {
		return err
	}
	writer := csv.NewWriter(output)
	if err := writer.Write([]string{"alcance", "sucursal_id", "codigo_ubicacion", "ubicacion", "moneda",
		"unidades_con_costo", "unidades_sin_costo", "valor_conocido", "valuacion_total"}); err != nil {
		return err
	}
	writeRow := func(scope, code, name string, totals ValuationAmounts) error {
		known, total := "", ""
		if totals.KnownValue != nil {
			known = *totals.KnownValue
		}
		if totals.TotalValue != nil {
			total = *totals.TotalValue
		}
		return writer.Write([]string{scope, storeID, csvtext.Escape(code), csvtext.Escape(name), value.Currency,
			strconv.FormatInt(totals.UnitsWithCost, 10), strconv.FormatInt(totals.UnitsWithoutCost, 10), known, total})
	}
	if err := writeRow("sucursal", "", "", value.ValuationAmounts); err != nil {
		return err
	}
	for _, row := range value.Locations {
		if err := writeRow("ubicacion", row.Code, row.Name, row.ValuationAmounts); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

type LocationValuation struct {
	LocationID string `json:"locationId"`
	Code       string `json:"code"`
	Name       string `json:"name"`
	ValuationAmounts
}

type ValuationAmounts struct {
	UnitsWithCost    int64   `json:"unitsWithCost"`
	UnitsWithoutCost int64   `json:"unitsWithoutCost"`
	KnownValue       *string `json:"knownValue"`
	TotalValue       *string `json:"totalValue"`
}

func (s Service) Valuation(ctx context.Context, actor auth.Identity, storeID string) (Valuation, error) {
	result := Valuation{Locations: []LocationValuation{}}
	err := s.withStore(ctx, actor, storeID, false, func(tx pgx.Tx, _ access) error {
		rows, err := tx.Query(ctx, `WITH totals AS (
			SELECT l.id,l.code,l.name,
				coalesce(sum(b.quantity_on_hand) FILTER (WHERE c.average_unit_cost IS NOT NULL),0) AS known_units,
				coalesce(sum(b.quantity_on_hand) FILTER (WHERE c.average_unit_cost IS NULL),0) AS unknown_units,
				sum(b.quantity_on_hand*c.average_unit_cost) AS known_value
			FROM stock_locations l
			LEFT JOIN stock_balances b ON (b.tenant_id,b.location_id)=(l.tenant_id,l.id) AND b.quantity_on_hand>0
			LEFT JOIN inventory_cost_balances c ON (c.tenant_id,c.store_id,c.variant_id)=(l.tenant_id,l.store_id,b.variant_id)
			WHERE l.tenant_id=$1 AND l.store_id=$2 AND l.status='active'
			GROUP BY GROUPING SETS ((),(l.id,l.code,l.name))
		)
		SELECT t.currency_code,coalesce(totals.id::text,''),coalesce(code,''),coalesce(totals.name,''),known_units,unknown_units,known_value::text,
			CASE WHEN unknown_units=0 THEN coalesce(known_value,0.00)::text END
		FROM tenants t CROSS JOIN totals WHERE t.id=$1 ORDER BY code NULLS FIRST,totals.id`, actor.TenantID, storeID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var location LocationValuation
			if err := rows.Scan(&result.Currency, &location.LocationID, &location.Code, &location.Name,
				&location.UnitsWithCost, &location.UnitsWithoutCost, &location.KnownValue, &location.TotalValue); err != nil {
				return err
			}
			if location.LocationID == "" {
				result.ValuationAmounts = location.ValuationAmounts
			} else {
				result.Locations = append(result.Locations, location)
			}
		}
		return rows.Err()
	})
	return result, err
}
