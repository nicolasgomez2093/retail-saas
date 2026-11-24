# ADR 0023 — Abonos de fiados

Los abonos se registran contra una deuda y una caja abierta de la sucursal donde se originó. Conservan las reglas de acceso de la cuenta por cobrar. El cajero opera sus propias ventas fiadas y sus sesiones de caja. El supervisor puede operar las ventas y sesiones de sus sucursales asignadas.

Cada abono tiene un medio y un importe aplicado. Para efectivo se registra el importe neto que entra en caja. Los medios electrónicos quedan asociados a la sesión sin aumentar el arqueo. El abono, el nuevo saldo y el movimiento en efectivo se escriben en una sola transacción. Una clave de idempotencia identifica la operación y un bloqueo de la cuenta serializa los cobros concurrentes.

El historial conserva los abonos y el saldo después de cada uno. Una cuenta con saldo cero aparece en **Saldados**. Las reimpresiones de la venta conservan el importe fiado original. Los abonos no modifican el stock ni el ticket histórico.

El resumen de fiados abarca todas las cuentas visibles de la sucursal y agrupa los importes por moneda. Informa el saldo actual y la cantidad de cuentas pendientes y saldadas. Presenta los abonos posteriores a la venta y las compensaciones de deuda como importes distintos. El filtro de estado y la paginación se aplican al listado.

La búsqueda por cliente usa su nombre actual y el nombre guardado al vender. Acepta coincidencias parciales sin distinguir mayúsculas y minúsculas. El número de venta debe coincidir exactamente y admite ceros iniciales. La búsqueda se aplica al listado y al resumen.

La descarga CSV aplica el estado, la búsqueda y los permisos del listado a todas las cuentas que coincidan. Incluye el nombre guardado al vender, la fecha en UTC, el importe fiado original y el saldo vigente. Los importes conservan sus decimales y los nombres reciben la protección de texto para planillas.

El desglose por medio de pago muestra la cantidad y el importe de los abonos posteriores a la venta, por moneda. Usa las mismas cuentas que el resumen. Los anticipos del momento de la venta, las compensaciones y los reintegros pertenecen a sus propias operaciones.

La tabla de clientes con mayor saldo agrupa las cuentas pendientes visibles por cliente y moneda. Muestra hasta diez clientes por moneda, ordenados por saldo descendente y, en caso de empate, por nombre e ID. Usa el nombre actual y la búsqueda aplicada al resumen.

Al elegir un cliente de la tabla se abre su listado de cuentas pendientes. El filtro usa su ID para distinguir clientes con el mismo nombre. Se aplica al listado, al resumen y a la exportación CSV. Desde ese listado también se pueden consultar sus cuentas saldadas.

La actividad reciente muestra hasta diez abonos por fecha de cobro descendente. Aplica la sucursal, la búsqueda, el cliente y los permisos del resumen. El estado y la página corresponden al listado de cuentas. La tabla usa el nombre actual del cliente, la zona horaria del comercio y un acceso al ticket original.
