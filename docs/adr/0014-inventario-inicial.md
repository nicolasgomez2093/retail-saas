# ADR 0014 — Ubicaciones y ajustes de inventario

Una ubicación activa pertenece a una sucursal y a su comercio. Puede ser salón o depósito y tiene una marca configurable de venta para etapas posteriores. El código es único por sucursal sin distinguir mayúsculas. Este modelo inicial abarca ubicaciones de sucursal. El depósito central independiente queda para una decisión de abastecimiento.

El personal puede consultar las ubicaciones, saldos y movimientos de sus sucursales activas asignadas. El propietario puede dar de alta ubicaciones en ese mismo alcance. Propietario, encargado y operador de inventario pueden ajustar unidades. Caja tiene acceso de lectura. Se puede ajustar el saldo de variantes inactivas para conciliar existencias físicas.

Cada ajuste exige una cantidad entera distinta de cero y un motivo. El saldo, la validación de disponibilidad y el movimiento se confirman juntos bajo el bloqueo de escritura del comercio y de la fila de saldo. El físico no puede quedar debajo del reservado. El disponible es la diferencia entre ambos. Los movimientos registran el SKU al momento de la confirmación y son inmutables en la base. Para corregir uno se registra otro ajuste con motivo.

La clave de idempotencia tiene alcance de comercio. Repetir la misma clave con el mismo actor, ubicación, variante, cantidad y motivo devuelve el movimiento original. Una clave ya confirmada con otros datos produce un conflicto. Los fallos previos a la confirmación revierten el saldo y el historial. La interfaz conserva la clave al repetir el mismo envío en el formulario. Los últimos 50 movimientos se presentan en la zona horaria del comercio.

Las transferencias se detallan en ADR 0015 y los conteos físicos en ADR 0016. El stock mínimo se detalla en ADR 0017.
