# ADR 0047 — Órdenes de compra por sucursal

El propietario y el encargado pueden registrar y consultar órdenes en sus sucursales activas asignadas. La orden vincula un proveedor activo del comercio con las variantes pedidas y sus cantidades. El proveedor, los productos y las variantes se comprueban al registrar la orden. El nombre del proveedor, el nombre del producto, el SKU, el talle y el color quedan registrados con la orden.

El costo por unidad es opcional y usa la moneda vigente del comercio. Se conserva como importe decimal exacto. Una línea sin costo informado guarda NULL y se muestra como «Sin costo informado». Un cero explícito se conserva como tal.

La orden queda abierta y registra las cantidades pedidas. El ingreso de stock corresponde a las [recepciones parciales o completas](0048-recepciones-parciales-de-compra.md). La cabecera y sus líneas se escriben juntas, y una clave de idempotencia permite recuperar la misma orden después de un reintento. La lista muestra las órdenes por sucursal, con la fecha en la zona horaria del comercio.

El listado admite un filtro por estado. **Pendientes de recepción** reúne órdenes abiertas y parcialmente recibidas. La búsqueda usa el proveedor, el SKU y el nombre de producto guardados al crear la orden, con coincidencias parciales sin distinguir mayúsculas. Una coincidencia incluye la orden completa. Ambos filtros se aplican antes de paginar.

El resumen cuenta las órdenes y las unidades pedidas, aceptadas y pendientes para todas las órdenes que coinciden con los filtros. Una coincidencia por SKU incluye las cantidades de toda la orden. Las unidades rechazadas quedan pendientes hasta que se acepten en una recepción.

La descarga CSV incluye todas las líneas de las órdenes que coinciden con los filtros y permisos del listado. Usa la fecha de la orden en la zona horaria del comercio y conserva el costo unitario original de cada línea. Un costo faltante queda vacío. El texto guardado en la orden usa la protección de celdas para planillas.

El período filtra por fecha de emisión en la zona horaria del comercio. Los límites son inclusivos y se pueden usar juntos o por separado. El listado, los totales y la descarga aplican el mismo período.
