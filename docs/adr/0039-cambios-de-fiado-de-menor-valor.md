# ADR 0039 — Cambios de fiados de menor valor

Cuando el reemplazo cuesta menos que el importe histórico de la recepción, el operador indica cuánto de la diferencia aplica al saldo vigente de ese fiado. Puede aplicar hasta el menor entre la diferencia y el saldo por cobrar. El resto de la diferencia se reintegra íntegramente por los medios de cobro originales, dentro de lo disponible por medio. Se necesita una caja abierta cuando hay un reintegro. Solo la parte reintegrada en efectivo afecta el arqueo.

El sistema valida los importes bajo el mismo bloqueo de la cuenta que usan los abonos. Confirma la salida de stock, la compensación y el reintegro en una sola transacción. La recepción muestra el cambio y sus importes resueltos. La cuenta por cobrar muestra el ajuste y el saldo resultante. El ticket y los abonos originales conservan sus importes. El reintento recupera el cambio registrado sin repetir los movimientos.

La recepción de prendas de reemplazo se detalla en el [ADR 0040](0040-recepcion-de-prendas-de-reemplazo.md).
