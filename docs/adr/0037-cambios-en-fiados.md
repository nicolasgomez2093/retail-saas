# ADR 0037 — Cambios de igual importe en fiados

Una recepción de una venta fiada admite un cambio cuando las prendas de reemplazo suman exactamente el valor histórico de la recepción. Se validan los precios vigentes y el stock de una ubicación vendible. La operación puede registrarse con la caja cerrada. La cuenta conserva su saldo, sus abonos y los datos del ticket original.

El cambio y las operaciones que resuelven la recepción en un fiado comparten el bloqueo de la cuenta por cobrar. Se valida si esa recepción ya tuvo compensaciones o un reintegro. Una vez confirmado el cambio, la recepción queda resuelta. Las demás recepciones de la venta se gestionan por separado.

El registro del cambio incluye las prendas entregadas y su salida de stock. El mismo reintento devuelve el cambio original. El cobro de un reemplazo de mayor valor se detalla en el [ADR 0038](0038-cambios-de-fiado-con-diferencia-a-cobrar.md). Los reemplazos de menor valor en fiados se detallan en el [ADR 0039](0039-cambios-de-fiado-de-menor-valor.md). La recepción de prendas de reemplazo se detalla en el [ADR 0040](0040-recepcion-de-prendas-de-reemplazo.md).
