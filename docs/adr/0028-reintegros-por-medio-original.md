# ADR 0028 — Reintegros por medio original

Se amplía el registro de reintegros a ventas pagadas íntegramente con uno de los medios registrados en el punto de venta. El reintegro usa el importe histórico de la recepción y el medio original de la venta. Se conserva el requisito de una caja abierta de la sucursal para asociar el registro a una sesión y al operador.

El operador asienta los reintegros por transferencia, débito, crédito o QR después de realizarlos por ese canal. Estos registros conservan el medio en el historial y el arqueo físico solo recibe egresos por reintegros en efectivo. Los reintegros ya registrados se migran como efectivo. Las solicitudes anteriores que omiten el medio siguen indicando efectivo.

Se conserva un reintegro por recepción, idempotencia por comercio y el límite del importe original. El desglose de pagos mixtos se detalla en el [ADR 0029](0029-reintegros-de-pagos-mixtos.md). Quedan pendientes las reglas para ventas con seña o fiadas y cambios.
