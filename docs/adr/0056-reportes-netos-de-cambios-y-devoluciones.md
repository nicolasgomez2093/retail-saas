# ADR 0056 — Reportes netos de cambios y devoluciones

## Contexto

`GET /reports/sales` ya tenía margen bruto por línea, resumen, ventas por día, medios de pago, categorías y variantes más vendidas, todo sobre las líneas vendidas. Desde la entrega de cambios (ADR 0055), una venta puede tener devoluciones y reemplazos entregados en otra fecha. El reporte decía «Las devoluciones no están descontadas»: la facturación mostrada no coincidía con el dinero comprometido. La pantalla tampoco seguía el diseño aprobado (`.head`, `.metrics`, `.grid2`, «Ventas registradas» y «Productos más vendidos»).

## Decisión

- **Actividad del período (`activity`, por moneda).** Une tres tipos de líneas, cada una con su propia fecha:
  - Líneas vendidas: suman.
  - Unidades devueltas: restan, valuadas al precio cobrado. El precio sale de la línea de la venta o, si lo devuelto vino de un cambio, de la línea del cambio.
  - Reemplazos entregados en cambios: suman.

  `netSales = grossSales − returnedAmount + exchangeAmount`. Así, un cambio sin diferencia neto da cero, un cambio por algo más caro suma solo la diferencia, y el neto coincide con lo cobrado menos lo reintegrado. Se respetan la búsqueda por SKU o producto, el rango de fechas y el alcance del usuario: quien no supervisa ve solo las operaciones de ventas de sus sesiones de caja.
- **Ticket promedio:** `grossSales / saleCount`, el importe cobrado por venta antes de los cambios. Con cero ventas es nulo y no se inventa un cero.
- **Productos más vendidos (`topProducts`):** hasta 10, ordenados por unidades que se llevaron los clientes (vendidas y entregadas en cambios, menos devueltas). Agrupan por producto, como el HTML; las variantes más vendidas siguen disponibles en `topVariants`.
- **Historial de ventas:** cada venta informa sus medios (`payments`), incluida la seña de una reserva, para la fila «N artículos · medio» del HTML.
- **Sin migraciones.** Todo se calcula sobre las tablas inmutables existentes, en la misma transacción de lectura del reporte.
- **Margen bruto sin cambios:** sigue calculándose sobre las líneas vendidas con su costo guardado, y la pantalla lo aclara.
- **Interfaz:** «Reportes» reproduce el diseño aprobado y agrega los bloques que el HTML no dibuja (período, medios de pago, margen, ventas por día y por categoría, y detalle por línea) con las mismas piezas. Se eliminó `SalesCharts`, que quedó sin uso.

## Consecuencias

- Una devolución registrada hoy sobre una venta del mes pasado resta en el período de hoy, que es cuando sale el dinero o baja la deuda.
- El margen no descuenta el costo de lo devuelto. Hacerlo exige definir cómo se valúa la mercadería que vuelve, sobre todo la de «Revisión y fallados», y queda para otra decisión.
- La búsqueda de «Ventas registradas» usa el historial de ventas: coincide por nombre parcial, y por SKU o código de barras exactos. El resumen, en cambio, acepta coincidencias parciales de SKU.
