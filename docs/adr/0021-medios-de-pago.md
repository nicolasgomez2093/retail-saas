# ADR 0021 — Medios de pago en el punto de venta

El operador registra efectivo, transferencia, débito, crédito o QR. Puede combinar hasta cinco medios, con un importe por medio. El alcance actual es registrar cómo pagó el cliente al confirmar la venta. Las integraciones de cobro y la conciliación quedan fuera de este alcance inicial. El fiado queda para la fase de clientes y cuentas por cobrar, donde la venta registrará un saldo pendiente asociado al cliente.

La API valida en centavos el importe total contra los precios vigentes. Cada importe de un pago explícito debe ser positivo y la suma debe cubrir exactamente el total. Solo el efectivo admite indicar un recibido y generar vuelto. La caja física recibe exclusivamente el importe neto en efectivo. La venta, sus pagos, el stock y el movimiento de caja se registran en la misma transacción con una clave de idempotencia por comercio.

El campo original `cashReceived` sigue vigente para ventas totalmente en efectivo, incluidas las de precio cero explícito. Su representación normalizada conserva las claves de confirmaciones previas. La alternativa `payments` conserva una entrada por medio y normaliza importes y orden para los reintentos. El ticket usa los pagos persistidos y muestra el efectivo recibido y vuelto cuando corresponde.

El punto de venta registra el medio y su importe. El contrato carece de campos para números de tarjeta, credenciales o datos del proveedor.
