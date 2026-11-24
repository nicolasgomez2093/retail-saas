# ADR 0035 — Pago mixto de la diferencia de un cambio

El operador puede indicar varios medios para el cobro de un reemplazo de mayor valor. Cada medio se registra una vez y los importes deben sumar exactamente la diferencia. El efectivo recibido y su vuelto se calculan contra la porción asignada a efectivo. Los componentes electrónicos se registran por su medio.

La confirmación guarda el cobro y sus componentes en la misma transacción que la salida de stock. Solo la parte neta en efectivo ingresa en la caja de la sesión. Los reintentos admiten un desglose equivalente con distinto orden o formato decimal y conservan el cambio original.

Los cobros ya registrados migran a un desglose de una línea y conservan sus claves de reintento. El contrato anterior de un solo medio sigue vigente. El reintegro por un reemplazo de menor valor se detalla en el [ADR 0036](0036-cambios-con-diferencia-a-reintegrar.md).
