# ADR 0016 — Conteos físicos por ubicación

El personal consulta los conteos de sus sucursales activas asignadas. Quien tiene permiso para ajustar inventario puede abrir un conteo, confirmarlo o cancelar un conteo abierto. El conteo fija una ubicación, hasta cien variantes y su saldo esperado al abrirse. Las variantes sin saldo registrado se consideran con cero unidades esperadas.

Confirmar requiere indicar las unidades contadas de todas las variantes, incluido cero cuando corresponda. Las diferencias positivas y negativas se registran como movimientos vinculados al conteo. Los saldos y movimientos se confirman juntos. Una diferencia no puede dejar menos unidades físicas que las reservadas. Las cantidades coincidentes se guardan en la ficha del conteo y conservan el saldo.

La base incrementa la revisión de cada saldo cuando se modifica el físico o reservado. Si cambió una revisión desde la apertura, la confirmación se rechaza incluso si el saldo numérico volvió al valor esperado. Para seguir, cancelá el conteo abierto y creá uno nuevo. El historial conserva los conteos cancelados y confirmados.

La apertura usa clave de idempotencia por comercio. Repetir la confirmación con los mismos valores y observación devuelve el resultado confirmado. Cancelar un conteo abierto o reintentar una cancelación conserva las existencias.
