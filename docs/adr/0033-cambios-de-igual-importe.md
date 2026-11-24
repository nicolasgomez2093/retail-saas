# ADR 0033 — Cambios de igual importe

Desde una devolución recibida de una venta pagada se puede entregar mercadería de reemplazo por el mismo importe. El valor de la devolución toma los precios históricos de la venta. Las prendas de reemplazo usan los precios vigentes de la sucursal y deben tener una cotización válida. El operador elige una ubicación vendible y sus cantidades disponibles se validan antes de confirmar.

La transacción guarda las prendas entregadas y sus importes, descuenta el stock y vincula el cambio con la recepción. Cada recepción admite una resolución comercial. El cambio de menor valor puede incluir un reintegro por diferencia. La operación y el reintegro usan el mismo bloqueo de venta para que solo una resolución prospere. Reintentar devuelve el cambio original.

El cambio de igual importe opera con caja cerrada y conserva el ticket de la venta original. El historial de devoluciones muestra las prendas entregadas. El cobro de un reemplazo de mayor valor se detalla en el [ADR 0034](0034-cambios-con-diferencia-a-cobrar.md). El reintegro por un reemplazo de menor valor se detalla en el [ADR 0036](0036-cambios-con-diferencia-a-reintegrar.md). Los cambios de igual importe en ventas fiadas se detallan en el [ADR 0037](0037-cambios-en-fiados.md). La recepción de prendas de reemplazo se detalla en el [ADR 0040](0040-recepcion-de-prendas-de-reemplazo.md).
