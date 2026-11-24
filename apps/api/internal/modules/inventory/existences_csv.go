package inventory

import (
	"context"
	"encoding/csv"
	"errors"
	"io"
	"strconv"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/platform/csvtext"

	"github.com/jackc/pgx/v5"
)

// ExistenceExportLimit caps one CSV; a larger catalog is exported by filters.
const ExistenceExportLimit = 20000

var ErrExportTooLarge = errors.New("hay demasiadas variantes para un solo archivo: filtrá por categoría, marca o ubicación")

// ExportExistencesCSV writes every variant that matches the Existencias
// filters (not only the visible page), one row per variant. Cost columns are
// included only for people who can see costs.
func (s Service) ExportExistencesCSV(ctx context.Context, actor auth.Identity, storeID string, filter ExistenceFilter, output io.Writer) error {
	filter.Page = 1
	filter, err := validExistenceFilter(filter)
	if err != nil {
		return err
	}
	return s.withStore(ctx, actor, storeID, false, func(tx pgx.Tx, allowed access) error {
		if filter.Location != "" {
			if err := requireLocation(ctx, tx, actor.TenantID, storeID, filter.Location); err != nil {
				return err
			}
		}
		page, err := queryExistences(ctx, tx, actor.TenantID, storeID, filter, "", ExistenceExportLimit, allowed.costs)
		if err != nil {
			return err
		}
		if page.HasMore {
			return ErrExportTooLarge
		}
		if _, err := io.WriteString(output, "\ufeff"); err != nil {
			return err
		}
		writer := csv.NewWriter(output)
		header := []string{"producto", "estado_producto", "sku", "codigo_barras", "talle", "color", "estado_variante",
			"fisico", "reservado", "vendible", "minimo", "ubicaciones_con_saldo", "alerta_minimo", "precio_venta", "valor_venta"}
		if allowed.costs {
			header = append(header, "costo_unitario", "valor_costo")
		}
		if err := writer.Write(header); err != nil {
			return err
		}
		text := func(value *string) string {
			if value == nil {
				return ""
			}
			return *value
		}
		number := func(value *int64) string {
			if value == nil {
				return ""
			}
			return strconv.FormatInt(*value, 10)
		}
		for _, row := range page.Items {
			if row.VariantID == nil {
				continue
			}
			minimum, alert := "", "no"
			if row.Minimum != nil {
				minimum = strconv.FormatInt(int64(*row.Minimum), 10)
			}
			if row.LowStock {
				alert = "si"
			}
			record := []string{csvtext.Escape(row.Product), statusText(&row.ProductStatus), csvtext.Escape(row.SKU), csvtext.Escape(row.Barcode),
				csvtext.Escape(row.Size), csvtext.Escape(row.Color), statusText(row.VariantStatus),
				number(row.OnHand), number(row.Reserved), number(row.Sellable), minimum, strconv.FormatInt(row.RecordedLocations, 10), alert,
				text(row.UnitPrice), text(row.RetailValue)}
			if allowed.costs {
				record = append(record, text(row.UnitCost), text(row.CostValue))
			}
			if err := writer.Write(record); err != nil {
				return err
			}
		}
		writer.Flush()
		return writer.Error()
	})
}

func statusText(status *string) string {
	switch {
	case status == nil:
		return ""
	case *status == "active":
		return "activo"
	case *status == "inactive":
		return "inactivo"
	}
	return *status
}
