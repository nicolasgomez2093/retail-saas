# ADR 0036 — Cambios con diferencia a reintegrar

En una venta pagada sin fiado, el operador puede entregar un reemplazo de menor valor y asentar el reintegro de la diferencia. El importe surge del valor histórico de la recepción menos el precio vigente del reemplazo. El operador confirma en una caja abierta y registra los importes por los medios originales.

La diferencia usa el mismo registro de reintegros que una devolución. El sistema valida el saldo disponible por medio y lo reduce para las siguientes operaciones de la venta. El egreso en efectivo corresponde solo a la parte devuelta en ese medio. Los componentes electrónicos se asientan por separado.

El cambio, la salida de stock y el reintegro se confirman en una transacción. El historial vincula la entrega del reemplazo y su diferencia reintegrada con la misma recepción. Los reintentos recuperan la operación registrada. Los cambios de igual importe en ventas fiadas se detallan en el [ADR 0037](0037-cambios-en-fiados.md). El cobro en cambios de fiados de mayor valor se detalla en el [ADR 0038](0038-cambios-de-fiado-con-diferencia-a-cobrar.md). Los reemplazos de menor valor en fiados se detallan en el [ADR 0039](0039-cambios-de-fiado-de-menor-valor.md). La recepción de prendas de reemplazo se detalla en el [ADR 0040](0040-recepcion-de-prendas-de-reemplazo.md).
