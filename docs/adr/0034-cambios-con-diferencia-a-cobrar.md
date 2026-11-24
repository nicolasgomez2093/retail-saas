# ADR 0034 — Cambios con diferencia a cobrar

En una devolución de venta pagada sin fiado, se puede entregar un reemplazo de mayor valor registrando el cobro de la diferencia. El importe a cobrar surge del precio vigente del reemplazo menos el importe histórico de la recepción. Al confirmar se verifican los precios, el stock disponible y el importe informado.

El cobro se registra en una caja abierta de la sucursal y con un solo medio de pago. Para efectivo se registra el importe recibido y el vuelto. Solo la diferencia neta entra al arqueo. Para medios electrónicos, el operador asienta el pago por el canal usado. El cambio, la salida de stock y el cobro se confirman en una sola transacción. Los reintentos conservan el cambio y el pago originales, incluso después del cierre.

El historial muestra el valor del reemplazo, el cobro y su medio. Los cambios de igual importe siguen operando con caja cerrada. El pago combinado se detalla en el [ADR 0035](0035-pago-mixto-en-cambios.md). Los reemplazos de menor valor se detallan en el [ADR 0036](0036-cambios-con-diferencia-a-reintegrar.md).
