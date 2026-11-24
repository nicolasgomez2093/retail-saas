package catalog

import (
	"bufio"
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"retail-platform/api/internal/modules/auth"
	"retail-platform/api/internal/platform/csvtext"

	"github.com/jackc/pgx/v5"
)

var ErrCSVInvalid = errors.New("CSV inválido")
var ErrCSVLimit = errors.New("el archivo supera el límite de importación")

const MaxCSVBytes = 2 << 20
const maxCSVRows = 1000

var csvHeader = []string{"product_ref", "name", "description", "category", "brand", "product_status",
	"sku", "size", "color", "variant_status", "barcodes"}

type CSVError struct {
	Line int
	Err  error
}

func (err *CSVError) Error() string { return fmt.Sprintf("Línea %d: %s", err.Line, err.Err) }
func (err *CSVError) Unwrap() error { return err.Err }

type ImportResult struct {
	Products int `json:"products"`
	Variants int `json:"variants"`
}

type csvProduct struct {
	Name, Description, Category, Brand, Status string
}

type csvRow struct {
	line    int
	ref     string
	product csvProduct
	variant *Variant
}

func (s Service) ImportCSV(ctx context.Context, actor auth.Identity, input io.Reader) (ImportResult, error) {
	rows, err := parseCSV(input)
	if err != nil {
		return ImportResult{}, err
	}
	var result ImportResult
	err = s.withAccess(ctx, actor, true, func(tx pgx.Tx, _ bool) error {
		products := make(map[string]string)
		for _, row := range rows {
			productID := products[row.ref]
			if productID == "" {
				product, err := importedProduct(ctx, tx, actor.TenantID, row.product)
				if err != nil {
					return &CSVError{Line: row.line, Err: err}
				}
				productID = product.ID
				products[row.ref] = productID
				result.Products++
			}
			if row.variant != nil {
				row.variant.ProductID = productID
				if _, err := protectIdentifiers(ctx, tx, actor, nil, row.variant, identifierAuthorization{}); err != nil {
					return &CSVError{Line: row.line, Err: err}
				}

				if err := insertVariant(ctx, tx, actor.TenantID, row.variant, true); err != nil {
					return &CSVError{Line: row.line, Err: err}
				}
				result.Variants++
			}
		}
		return nil
	})
	if err != nil {
		return ImportResult{}, err
	}
	return result, nil
}

func parseCSV(input io.Reader) ([]csvRow, error) {
	buffered := bufio.NewReader(input)
	if prefix, _ := buffered.Peek(3); bytes.Equal(prefix, []byte{0xef, 0xbb, 0xbf}) {
		_, _ = buffered.Discard(3)
	}
	reader := csv.NewReader(buffered)
	if preview, _ := buffered.Peek(buffered.Size()); len(preview) > 0 {
		first, _, _ := bytes.Cut(preview, []byte{'\n'})
		if bytes.Contains(first, []byte{';'}) {
			reader.Comma = ';'
		}
	}
	reader.FieldsPerRecord = len(csvHeader)
	header, err := reader.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%w: falta el encabezado", ErrCSVInvalid)
		}
		return nil, csvReadError(err)
	}
	if !slices.Equal(header, csvHeader) {
		return nil, fmt.Errorf("%w: usá el encabezado de la plantilla", ErrCSVInvalid)
	}
	var rows []csvRow
	firstByRef := make(map[string]csvRow)
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, csvReadError(err)
		}
		if len(rows) == maxCSVRows {
			return nil, ErrCSVLimit
		}
		line, _ := reader.FieldPos(0)
		row, err := parseRecord(line, record)
		if err != nil {
			return nil, &CSVError{Line: line, Err: err}
		}
		if first, found := firstByRef[row.ref]; found {
			if first.product != row.product || first.variant == nil || row.variant == nil {
				return nil, &CSVError{Line: line, Err: fmt.Errorf("%w: las filas con una misma referencia deben repetir los datos del producto y tener SKU", ErrCSVInvalid)}
			}
		} else {
			firstByRef[row.ref] = row
		}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("%w: agregá al menos una fila", ErrCSVInvalid)
	}
	return rows, nil
}

func parseRecord(line int, record []string) (csvRow, error) {
	for index, value := range record {
		record[index] = strings.TrimSpace(csvtext.Unescape(value))
	}
	product := csvProduct{Name: record[1], Description: record[2], Category: record[3], Brand: record[4], Status: csvStatus(record[5])}
	row := csvRow{line: line, ref: record[0], product: product}
	if !validText(row.ref, 80, true) || !validProduct(Product{Name: product.Name, Description: product.Description, Status: product.Status}) ||
		!validText(product.Category, 120, false) || !validText(product.Brand, 120, false) {
		return csvRow{}, fmt.Errorf("%w: verificá referencia, nombre, clasificación y estado del producto", ErrCSVInvalid)
	}
	if record[6] == "" {
		if record[7] != "" || record[8] != "" || record[9] != "" || record[10] != "" {
			return csvRow{}, fmt.Errorf("%w: una variante requiere SKU", ErrCSVInvalid)
		}
		return row, nil
	}
	var codes []string
	if record[10] != "" {
		codes = strings.Split(record[10], "|")
	}
	input, err := normalizeVariant(VariantInput{SKU: record[6], Size: record[7], Color: record[8], Barcodes: codes})
	status := csvStatus(record[9])
	if err != nil || (status != "active" && status != "inactive") {
		return csvRow{}, fmt.Errorf("%w: verificá SKU, talle, color, estado y códigos de la variante", ErrCSVInvalid)
	}
	row.variant = &Variant{Status: status, VariantInput: input}
	return row, nil
}

func csvStatus(value string) string {
	if value == "" {
		return "active"
	}
	return value
}

func csvReadError(err error) error {
	var parse *csv.ParseError
	if errors.As(err, &parse) {
		return &CSVError{Line: parse.StartLine, Err: fmt.Errorf("%w: revisá separador, comillas y cantidad de columnas", ErrCSVInvalid)}
	}
	return err
}

func importedProduct(ctx context.Context, tx pgx.Tx, tenantID string, row csvProduct) (Product, error) {
	product := Product{Name: row.Name, Description: row.Description, Status: row.Status}
	if row.Category != "" || row.Brand != "" {
		err := tx.QueryRow(ctx, `SELECT
			(SELECT id FROM categories WHERE tenant_id=$1 AND lower(name)=lower($2)),
			(SELECT id FROM brands WHERE tenant_id=$1 AND lower(name)=lower($3))`,
			tenantID, row.Category, row.Brand).Scan(&product.CategoryID, &product.BrandID)
		if err != nil {
			return Product{}, err
		}
		if (row.Category != "" && product.CategoryID == nil) || (row.Brand != "" && product.BrandID == nil) {
			return Product{}, fmt.Errorf("%w: la categoría o la marca debe existir en este comercio", ErrCSVInvalid)
		}
	}
	err := insertProduct(ctx, tx, tenantID, &product)
	return product, err
}

func (s Service) ExportCSV(ctx context.Context, actor auth.Identity, output io.Writer) error {
	return s.withAccess(ctx, actor, false, func(tx pgx.Tx, manage bool) error {
		if !manage {
			return auth.ErrForbidden
		}
		rows, err := tx.Query(ctx, `
			SELECT p.id::text,p.name,p.description,coalesce(c.name,''),coalesce(b.name,''),p.status,
				coalesce(v.sku,''),coalesce(v.size,''),coalesce(v.color,''),coalesce(v.status,''),
				coalesce((SELECT string_agg(code,'|' ORDER BY is_primary DESC,code)
					FROM barcodes WHERE (tenant_id,variant_id)=(v.tenant_id,v.id)),'')
			FROM products p
			LEFT JOIN categories c ON (c.tenant_id,c.id)=(p.tenant_id,p.category_id)
			LEFT JOIN brands b ON (b.tenant_id,b.id)=(p.tenant_id,p.brand_id)
			LEFT JOIN product_variants v ON (v.tenant_id,v.product_id)=(p.tenant_id,p.id)
			WHERE p.tenant_id=$1 ORDER BY p.name,p.id,v.sku`, actor.TenantID)
		if err != nil {
			return err
		}
		defer rows.Close()
		return writeCSV(output, func(writer *csv.Writer) error {
			record := make([]string, len(csvHeader))
			destinations := make([]any, len(record))
			for index := range record {
				destinations[index] = &record[index]
			}
			for rows.Next() {
				if err := rows.Scan(destinations...); err != nil {
					return err
				}
				for index, cell := range record {
					record[index] = csvtext.Escape(cell)
				}
				if err := writer.Write(record); err != nil {
					return err
				}
			}
			return rows.Err()
		})
	})
}

func WriteCSVTemplate(output io.Writer) error {
	return writeCSV(output, func(writer *csv.Writer) error {
		if err := writer.Write([]string{"remera", "Remera básica", "Algodón", "", "", "active",
			"REM-M-AZ", "M", "Azul", "active", "'000123"}); err != nil {
			return err
		}
		return writer.Write([]string{"remera", "Remera básica", "Algodón", "", "", "active",
			"REM-L-AZ", "L", "Azul", "active", "'000124"})
	})
}

func writeCSV(output io.Writer, records func(*csv.Writer) error) error {
	if _, err := io.WriteString(output, "\ufeff"); err != nil {
		return err
	}
	writer := csv.NewWriter(output)
	if err := writer.Write(csvHeader); err != nil {
		return err
	}
	if err := records(writer); err != nil {
		return err
	}
	writer.Flush()
	return writer.Error()
}
