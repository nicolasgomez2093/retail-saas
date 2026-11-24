# ADR 0050 — Costo promedio ponderado por sucursal y variante

Se adopta el costo promedio ponderado para el MVP, agrupado por sucursal y variante. Las recepciones aceptadas con costo confirmado actualizan el promedio en la misma transacción que el stock. Las ventas guardan un snapshot inmutable del costo unitario vigente; ventas anteriores no se recalculan.

El costo inicial del stock ya existente queda desconocido porque no hay datos suficientes para reconstruirlo. Una recepción con costo conocido solo establece o actualiza el promedio cuando la base de stock también es conocida. Una recepción sin costo, un ingreso manual, una devolución o una recepción de transferencia invalidan el costo de la sucursal. El costo vuelve a estar disponible cuando el stock llega a cero y luego se recibe stock con costo confirmado. El costo cero informado expresamente sí es válido.

No se prorratean fletes, impuestos ni otros gastos de adquisición. La valuación resumida, los reportes de margen, FIFO y la asignación de costos en devoluciones quedan fuera de esta decisión. La consulta del costo unitario y valor por línea de inventario se agregó en [ADR 0051](0051-consulta-de-valuacion-de-inventario.md). El costo faltante se expone como `null`, nunca como cero.
