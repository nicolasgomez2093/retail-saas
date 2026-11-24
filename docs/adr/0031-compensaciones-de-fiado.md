# ADR 0031 — Compensaciones de fiado por devoluciones

Desde una recepción de devolución el operador elige cuánto aplicar al saldo vigente del fiado de esa venta. El máximo es el menor entre la deuda actual y el valor histórico todavía pendiente de resolver de la recepción. Se admiten varias compensaciones parciales dentro de esos límites y con acceso a la venta y a su sucursal. La operación puede registrarse con la caja cerrada.

Los abonos y las compensaciones usan el mismo bloqueo de la cuenta por cobrar. El saldo y el registro inmutable de la compensación se guardan en una sola transacción. Cada ajuste conserva el saldo que dejó y se consulta en ambos historiales. Un reintento devuelve el ajuste original. El registro de la recepción y el ticket conservan los datos históricos.

La compensación reduce el saldo pendiente y deja pendiente de resolución la diferencia de la devolución. El reintegro de importes ya cobrados se detalla en el [ADR 0032](0032-reintegros-de-fiados.md). Los cambios requieren reglas adicionales.
