# ADR 0044 — Otro cambio de menor valor en fiados

Cuando se recibe una prenda que se entregó como reemplazo y el nuevo cambio cuesta menos, el operador decide cuánto aplicar a la cuenta por cobrar. El máximo es el menor entre la diferencia y la deuda vigente de esa venta. Si la compensación cubre toda la diferencia, se puede confirmar con la caja cerrada.

Si queda un importe a favor del cliente, se reintegra íntegramente en una caja abierta. El operador elige entre los medios efectivamente cobrados de la venta y de la cadena de cambios. Se aplica la misma imputación por medio y fuente que en las ventas pagadas. El arqueo registra solo el efectivo devuelto.

La salida de mercadería, la compensación y el reintegro se confirman en una sola transacción. El historial registra el saldo resultante y los medios reintegrados. El reintento recupera el cambio original. El reintegro directo y la compensación sin entregar otro reemplazo se detallan en el [ADR 0045](0045-devolucion-directa-de-un-reemplazo.md).
