# ADR 0059 — Costo del inventario, CSV de existencias y pagos a proveedores

## Contexto

Los comercios preguntan cuánto vale lo que tienen en el local, quieren llevarse el inventario a una planilla y necesitan saber cuánto le deben a cada proveedor. Las respuestas que había no alcanzaban:

- **Costo del inventario:** el costo promedio ya existía (ADR de costo promedio ponderado), pero solo se conocía para la mercadería que entró por compras con costo. El stock inicial, las devoluciones y los ajustes lo dejaban «desconocido», y sin una forma de cargarlo la valuación decía «No disponible». Además, una transferencia entre ubicaciones de la misma sucursal también borraba el costo, aunque fueran las mismas unidades.
- **CSV:** la única descarga era la valuación por ubicación, no el detalle por prenda.
- **Pagos a proveedores:** los pedidos no registraban pagos, así que no había forma de ver lo pagado ni el saldo.

## Decisión

- **Existencias muestra el valor del stock filtrado:**
  - **Costo del inventario** (costo promedio × unidades físicas), con las unidades sin costo informadas aparte. No se valúan en cero.
  - **Valor a precio de venta**, con la lista de precios de la sucursal.
  - **Ganancia potencial**, solo cuando todas las unidades tienen costo y precio.
  - Una columna «Costo unit.» por variante, con el total de esa variante.
- **Quién ve los costos:** solo quien tiene `purchases:read` (dueño y encargado). La cajera y el operador de inventario ven Existencias sin costos, y el CSV que descargan no trae esas columnas.
- **Cargar costo** (`PUT /inventory/costs`, requiere `purchases:manage`):
  - Fija el costo de las unidades que la variante tiene hoy en la sucursal, para una o varias variantes de un producto, con la opción «mismo costo para todas».
  - Cada carga queda en `inventory_cost_revisions` con el costo anterior, las unidades, el motivo y quién la hizo.
  - Las compras siguientes promedian desde ese valor.
  - Sin unidades no hay nada que valuar: responde 409.
- **Transferencias:** una transferencia dentro de la misma sucursal ya no invalida el costo. Entre sucursales sigue invalidándolo, porque el costo es por sucursal.
- **CSV de existencias** (`GET /inventory/existences/csv`):
  - Usa los mismos filtros que la pantalla, pero trae todas las variantes, no solo la página.
  - Incluye el código de barras, las cantidades, el mínimo, el precio y su valor y, si corresponde, el costo y su valor.
  - El tope es de 20.000 variantes. Más que eso responde 409 y pide filtrar.
- **Pagos a proveedores** (`purchase_order_payments`):
  - El total de un pedido son las unidades pedidas a su costo acordado. En un pedido cancelado, solo las unidades aceptadas.
  - Si un renglón que cuenta no tiene costo, el pedido queda «Sin total»: se puede pagar, pero no hay control de saldo.
  - Los medios son efectivo, transferencia, cheque (con fecha de cobro opcional, para los diferidos), tarjeta u otro, con comprobante y nota.
  - La fecha de pago no puede ser futura.
  - Un pago no puede superar el saldo.
  - Los pagos no se borran: se anulan con motivo y quedan tachados.
  - Registrar un pago no mueve la caja. La pantalla lo aclara.
- **Compras:**
  - Suma la métrica «Saldo a pagar a proveedores», con los pedidos con saldo y los cheques todavía no cobrables.
  - Suma un filtro «Con saldo a pagar / Pagados» y una columna de estado de pago.
  - El CSV de órdenes agrega número, total, pagado y saldo.
- **Migración 047.**

## Consecuencias

- El costo cargado a mano es una decisión de quien administra: queda auditado, pero no se valida contra facturas.
- Un pago en efectivo que sale de la caja hay que registrarlo también como retiro en Caja. Vincularlos queda para más adelante.
- El saldo a favor de un pedido (por ejemplo, cancelado después de pagarse) no compensa la deuda de otros pedidos del mismo proveedor.
