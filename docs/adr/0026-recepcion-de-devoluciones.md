# ADR 0026 — Recepción de devoluciones

La recepción se registra contra una venta confirmada de la misma sucursal. El cajero accede a las ventas de sus sesiones y el supervisor a las de las sucursales asignadas. El flujo opera con caja abierta o cerrada, ya que registra el ingreso físico de prendas.

El operador elige cantidades, motivo y destinos de la sucursal. Las prendas vendibles vuelven a una ubicación vendible. Para cuarentena se elige una ubicación no vendible. Una misma variante puede distribuirse entre ambos destinos. La operación bloquea la venta antes de validar las cantidades devueltas acumuladas y controla el stock dentro de la misma transacción. Un reintento conserva la recepción original.

El importe de referencia usa el precio histórico de cada artículo. El ticket original, los pagos, el arqueo y las cuentas por cobrar conservan sus valores. El registro físico es inmutable y queda disponible para la resolución comercial. El reintegro en efectivo se documenta en el [ADR 0027](0027-reintegros-en-efectivo.md). Quedan pendientes las políticas de otros medios, cambios y compensaciones de saldos, así como los plazos aplicables.
