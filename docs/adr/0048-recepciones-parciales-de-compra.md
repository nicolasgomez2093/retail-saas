# ADR 0048 — Recepciones parciales de compra

El propietario y el encargado registran recepciones de órdenes de sus sucursales activas asignadas. Cada recepción tiene una ubicación activa de esa sucursal, una fecha y una observación opcional. Para cada línea se informan las unidades aceptadas y rechazadas. Su suma en una recepción debe caber en lo pendiente en ese momento.

Las unidades aceptadas ingresan al stock y reducen el pendiente. Los rechazos quedan asentados y siguen pendientes, al igual que las unidades que todavía no llegaron. La orden permanece abierta hasta el primer ingreso, pasa a recepción parcial mientras falten unidades y queda recibida cuando toda la cantidad pedida fue aceptada.

La recepción, los movimientos de stock y el estado de la orden se confirman en una transacción. El bloqueo compartido con las escrituras del inventario serializa los ingresos y la clave de idempotencia recupera la misma recepción. Las recepciones y sus líneas quedan disponibles en un historial paginado. El costo de las unidades aceptadas se puede registrar al recibir, según el [ADR 0049](0049-costo-unitario-de-recepcion.md).

El CSV exporta todas las líneas del historial de una orden con los mismos permisos. Incluye la fecha en la zona horaria del comercio, la ubicación, la observación y los costos de recepción en la moneda de la orden. El costo desconocido queda vacío y el costo cero se conserva.

El resumen abarca el historial completo de la orden. Separa unidades aceptadas con y sin costo y acumula los rechazos registrados. El importe conocido se calcula multiplicando las unidades aceptadas de cada línea por su costo registrado al recibir. El total queda sin valor si falta algún costo. Con cero unidades aceptadas, el valor total es cero.
