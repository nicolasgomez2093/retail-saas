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

func (s Service) ExportReceiptsCSV(ctx context.Context, actor auth.Identity, storeID, orderID string, output io.Writer) error {
	return s.readReceipts(ctx, actor, storeID, orderID, nil, 0, nil, func(currency, timezone string, rows pgx.Rows) error {
		location, err := time.LoadLocation(timezone)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(output, "\ufeff"); err != nil {
			return err
		}
		writer := csv.NewWriter(output)
		if err := writer.Write([]string{"orden_id", "recepcion_id", "fecha_recepcion", "ubicacion", "observacion",
			"sku", "producto", "unidades_aceptadas", "unidades_rechazadas", "moneda", "costo_unitario_recepcion"}); err != nil {
			return err
		}
		for rows.Next() {
			receipt, err := scanReceipt(rows)
			if err != nil {
				return err
			}
			for _, item := range receipt.Items {
				cost := ""
				if item.UnitCost != nil {
					cost = *item.UnitCost
				}
				if err := writer.Write([]string{receipt.OrderID, receipt.ID, receipt.ReceivedAt.In(location).Format(time.RFC3339Nano),
					csvtext.Escape(receipt.LocationName), csvtext.Escape(receipt.Note), csvtext.Escape(item.SKU), csvtext.Escape(item.Product),
					strconv.FormatInt(int64(item.Accepted), 10), strconv.FormatInt(int64(item.Rejected), 10), currency, cost}); err != nil {
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
