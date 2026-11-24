# ADR 0038 — Cambios de fiados con diferencia a cobrar

Una recepción pendiente de una venta fiada admite entregar un reemplazo de mayor valor si la diferencia se cobra en una caja abierta. Se usan los precios vigentes para el reemplazo y el importe histórico para la recepción. El importe puede cubrirse con uno o varios medios. Se exige cubrirlo al confirmar y se calcula el vuelto sobre el efectivo asignado.

La diferencia es un cobro del cambio y el saldo de la cuenta por cobrar conserva su valor. El cambio, sus líneas, la salida de stock y el cobro se confirman en una transacción. La parte neta en efectivo entra al arqueo de la sesión y el historial muestra el desglose del cobro. Se conservan los abonos y el ticket de la venta original.

El reintento conserva el cambio y el cobro registrados, incluso después de cerrar la caja. Los reemplazos de menor valor en fiados se detallan en el [ADR 0039](0039-cambios-de-fiado-de-menor-valor.md). La recepción de prendas de reemplazo se detalla en el [ADR 0040](0040-recepcion-de-prendas-de-reemplazo.md).
