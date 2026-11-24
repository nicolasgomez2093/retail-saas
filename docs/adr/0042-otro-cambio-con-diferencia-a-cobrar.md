# ADR 0042 — Otro cambio con diferencia a cobrar

La recepción de prendas entregadas como reemplazo admite un nuevo cambio por prendas de mayor valor. La diferencia toma el precio vigente de las nuevas prendas y el importe histórico de la recepción. Para confirmarla, el operador registra el cobro en una caja abierta de la sucursal. Se admite uno o varios medios y se calcula el vuelto sobre la parte en efectivo.

La salida de stock y el cobro de la diferencia se confirman en una sola transacción. La caja incorpora solo el efectivo neto. El historial vincula el cobro con el nuevo cambio y muestra sus medios. En una venta fiada, el saldo y los abonos se conservan. El reintento recupera el cambio y el cobro registrados, incluso después del cierre.

Se conservan los cambios de igual importe con caja cerrada. Los cambios por prendas de menor valor en ventas pagadas se detallan en el [ADR 0043](0043-otro-cambio-con-diferencia-a-reintegrar.md).
