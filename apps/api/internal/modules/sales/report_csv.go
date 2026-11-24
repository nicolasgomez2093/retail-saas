package sales

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

func (s Service) ExportSalesMarginCSV(ctx context.Context, actor auth.Identity, storeID string, filter SalesMarginFilter, output io.Writer) error {
	filter.Offset = 0
	return s.readSalesMargin(ctx, actor, storeID, filter, nil, nil, func(timezone string, rows pgx.Rows) error {
		location, err := time.LoadLocation(timezone)
		if err != nil {
			return err
		}
		if _, err := io.WriteString(output, "\ufeff"); err != nil {
			return err
		}
		writer := csv.NewWriter(output)
		if err := writer.Write([]string{"numero_venta", "fecha", "moneda", "sku", "producto", "unidades", "venta_neta",
			"estado_costo", "costo_unitario", "costo_mercaderia", "ganancia_bruta", "margen_bruto_porcentaje"}); err != nil {
			return err
		}
		for rows.Next() {
			line, err := scanSalesMarginLine(rows)
			if err != nil {
				return err
			}
			if err := writer.Write([]string{
				line.SaleNumber, line.SoldAt.In(location).Format(time.RFC3339Nano), line.Currency, csvtext.Escape(line.SKU),
				csvtext.Escape(line.Product), strconv.FormatInt(int64(line.Quantity), 10), line.NetSales, line.CostStatus,
				csvAmount(line.UnitCostSnapshot), csvAmount(line.CostOfGoods), csvAmount(line.GrossProfit), csvAmount(line.GrossMarginPct),
			}); err != nil {
				return err
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		writer.Flush()
		return writer.Error()
	})
}

func csvAmount(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func (s Service) ExportReceivablesCSV(ctx context.Context, actor auth.Identity, storeID string, filter ReceivableFilter, output io.Writer) error {
	return s.readReceivables(ctx, actor, storeID, filter, nil, 0, nil, func(rows pgx.Rows) error {
		if _, err := io.WriteString(output, "\ufeff"); err != nil {
			return err
		}
		writer := csv.NewWriter(output)
		if err := writer.Write([]string{"sucursal_id", "numero_venta", "fecha_venta_utc", "cliente", "estado", "moneda",
			"importe_fiado_original", "saldo_pendiente"}); err != nil {
			return err
		}
		label := map[string]string{"pending": "pendiente", "partial": "pendiente_con_abonos", "settled": "saldado"}[filter.Status]
		for rows.Next() {
			row, err := scanReceivable(rows)
			if err != nil {
				return err
			}
			if err := writer.Write([]string{storeID, row.SaleNumber, row.CreatedAt.Format(time.RFC3339Nano),
				csvtext.Escape(row.CustomerName), label, row.Currency, row.OriginalAmount, row.OutstandingAmount}); err != nil {
				return err
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		writer.Flush()
		return writer.Error()
	})
}
