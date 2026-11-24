# ADR 0049 — Costo unitario de recepción

Cada línea con unidades aceptadas admite un costo unitario efectivo en la moneda de la orden. Se guarda como decimal exacto y corresponde solo a las unidades aceptadas. Las recepciones parciales de una línea pueden tener costos distintos. Un costo explícito de cero se conserva como tal.

El costo de la orden es una referencia que el operador puede copiar o reemplazar. Si omite el costo al recibir, se guarda NULL. Las recepciones anteriores a este registro también conservan un costo desconocido. El historial muestra el valor de cada recepción.

El costo, el movimiento de stock y la recepción se confirman juntos. Las solicitudes anteriores conservan su clave de reintento cuando omiten el costo. El método de costeo se definió en [ADR 0050](0050-costo-promedio-ponderado.md).
