# ADR 0012 — Importación y exportación CSV de catálogo

El propietario puede descargar una plantilla, importar altas y exportar el catálogo de su comercio. El importador acepta UTF-8 con BOM opcional, coma o punto y coma, hasta 2 MiB y 1000 filas de datos. La exportación produce CSV con coma y BOM. Incluye productos sin variantes y estados activos e inactivos.

```csv
product_ref,name,description,category,brand,product_status,sku,size,color,variant_status,barcodes
remera,Remera básica,Algodón,,,active,REM-M-AZ,M,Azul,active,000123|INTERNO-AZ
remera,Remera básica,Algodón,,,active,REM-L-AZ,L,Azul,active,000124
```

- `product_ref` agrupa filas dentro del archivo. Repetí los mismos datos de producto en todo el grupo. La referencia no tiene que ser un UUID. Cada grupo crea un producto con un identificador nuevo.
- Para un producto sin variantes, incluí una fila y dejá vacías las columnas desde `sku` hasta `barcodes`.
- Si cargás `category` o `brand`, esos nombres tienen que existir en el comercio. Los estados vacíos se guardan como activos.
- Separá los códigos con `|`. El primero queda como principal. En una planilla tratá los SKU y los códigos como texto para conservar ceros iniciales.
- Los precios, las fotos y las existencias usan sus propios flujos. El CSV cubre productos, clasificaciones existentes y variantes.
- Al exportar, las celdas con prefijos de fórmula o números con ceros iniciales reciben un apóstrofo protector. El importador interpreta esa convención. Para conservar un apóstrofo literal al comienzo de un valor que usa esa convención, escribilo dos veces.

Las altas se ejecutan en una transacción y respetan la unicidad de SKU y códigos por comercio. Reutilizar un `product_ref` en otro archivo inicia un producto nuevo. Un SKU o código existente cancela la carga y devuelve la línea del conflicto. La exportación de otro comercio puede importarse creando primero las categorías y marcas que necesita y dividiendo archivos que superen el límite de carga.
