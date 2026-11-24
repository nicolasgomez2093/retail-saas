# ADR 0052 — Reporte de margen bruto por venta y variante

El reporte muestra una fila por variante de cada venta y usa el precio y el costo unitario snapshot que quedaron guardados al confirmar. El costo de mercadería es el costo unitario por cantidad. La ganancia bruta es la venta neta menos el costo de mercadería; el porcentaje de margen es esa diferencia dividida por la venta neta. Si falta el costo snapshot, costo, ganancia y porcentaje se devuelven como `null`. Si la venta neta es cero, el porcentaje también es `null`.

El alcance de lectura coincide con el historial: cajeros ven las ventas de sus sesiones y supervisores ven las ventas de la sucursal asignada. El reporte pagina líneas y admite un rango opcional de instantes RFC 3339 con límite inferior inclusivo y superior exclusivo.

El cálculo describe el margen bruto original al confirmar la venta. No descuenta devoluciones, comisiones de pago, fletes ni otros gastos variables. Las fechas se presentan en la zona horaria del tenant.

La pantalla permite filtrar por días del comercio. Los parámetros `from_date` y `to_date` admiten límites independientes e incluyen ambos días cuando se ingresan juntos. PostgreSQL compara la fecha local de cada venta. Los clientes que necesitan filtrar por instante conservan `from` y `to` como alternativa.

La descarga CSV usa la misma consulta de margen y exporta todas las filas de la sucursal y del período aplicado. Las fechas llevan el desplazamiento de la zona horaria del comercio. El archivo conserva los importes decimales y deja vacías las celdas correspondientes a valores desconocidos. Los SKU y nombres usan la protección de texto del CSV de catálogo.

El resumen usa todas las líneas del período y agrupa por moneda. Muestra la venta neta total, cuánto corresponde a líneas con y sin costo y cuántas líneas carecen de costo. La ganancia y el margen corresponden a las líneas con costo. El porcentaje divide la ganancia de ese conjunto por su venta neta. Una venta neta de cero deja el porcentaje sin valor. Un período sin ventas devuelve un resumen vacío.

La búsqueda por SKU o producto usa los datos guardados al confirmar la venta. Acepta coincidencias parciales sin distinguir mayúsculas y minúsculas. La tabla, el resumen y la descarga CSV aplican el mismo filtro junto con el período y el acceso a la sucursal.

El ranking muestra hasta diez variantes según las unidades al confirmar la venta. Aplica los filtros y permisos de todo el reporte, sin depender de la página visible. Agrupa por ID de variante para mantener juntas las ventas de una variante renombrada y muestra SKU, nombre, talle y color actuales del catálogo. El CSV conserva el detalle de las ventas.

El resumen por medio agrupa importes por moneda. Cuenta el efectivo aplicado después del vuelto, los pagos electrónicos, las señas aplicadas al entregar y el saldo fiado inicial. Los abonos y reintegros posteriores pertenecen a sus propias operaciones. Cada venta cuenta una vez por medio. Cuando la búsqueda encuentra alguna línea, el resumen usa los importes registrados para la venta completa.

El desglose diario muestra los 31 días más recientes con ventas dentro de los filtros. Agrupa por fecha local del comercio y por moneda. Cuenta cada venta una vez por día y moneda, y suma las unidades y la venta neta de las líneas que coinciden con la búsqueda. El cálculo usa todo el resultado filtrado, antes de paginar el detalle.
