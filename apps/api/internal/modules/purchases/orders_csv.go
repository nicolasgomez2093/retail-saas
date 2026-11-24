package purchases

import (
	"context"
	"encoding/csv"
	"io"
	"strconv"
	"time"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/platform/csvtext"

	"github.com/jackc/pgx/v5"
)

func (s Service) ExportOrdersCSV(ctx context.Context, actor auth.Identity, storeID string, filter ListFilter, output io.Writer) error {
	filter, err := normalizeOrderFilter(storeID, filter)
	if err != nil {
		return err
	}
	return s.withStore(ctx, actor, storeID, false, func(tx pgx.Tx, access orderAccess) error {
		location, err := time.LoadLocation(access.timezone)
		if err != nil {
			return err
		}
		rows, err := queryOrderList(ctx, tx, orderFilterArgs(actor.TenantID, storeID, access.timezone, filter), nil, 0)
		if err != nil {
			return err
		}
		defer rows.Close()
		if _, err := io.WriteString(output, "\ufeff"); err != nil {
			return err
		}
		writer := csv.NewWriter(output)
		if err := writer.Write([]string{"orden_id", "sucursal_id", "fecha_orden", "proveedor", "estado", "moneda",
			"sku", "producto", "talle", "color", "unidades_pedidas", "unidades_aceptadas", "unidades_pendientes", "costo_unitario_orden"}); err != nil {
			return err
		}
		for rows.Next() {
			order, err := scanOrder(rows)
			if err != nil {
				return err
			}
			for _, line := range order.Items {
				cost := ""
				if line.UnitCost != nil {
					cost = *line.UnitCost
				}
				if err := writer.Write([]string{order.ID, order.StoreID, order.OrderedAt.In(location).Format(time.RFC3339Nano),
					csvtext.Escape(order.SupplierName), order.Status, order.Currency, csvtext.Escape(line.SKU),
					csvtext.Escape(line.Product), csvtext.Escape(line.Size), csvtext.Escape(line.Color),
					strconv.FormatInt(int64(line.Quantity), 10), strconv.FormatInt(int64(line.Received), 10),
					strconv.FormatInt(int64(line.Quantity-line.Received), 10), cost}); err != nil {
					return err
				}
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		writer.Flush()
		return writer.Error()
	})
}
