# ADR 0025 — Señas de reservas

Se registra un anticipo por reserva activa con una sesión de caja abierta y un medio de pago. La seña en efectivo incrementa el arqueo de esa sesión mediante un movimiento identificado como anticipo. Una seña electrónica queda asentada con su medio e importe. Registrar la seña conserva el stock físico y reservado y deja la venta pendiente de confirmar.

Al entregar, la venta aplica el anticipo completo al total vigente. El operador cobra solo la diferencia y el ticket detalla la seña y los pagos de la entrega. Si el anticipo cubre el total, se entrega sin cobro adicional. Si supera el total vigente, el sistema bloquea la entrega hasta resolver la diferencia. Una seña ingresada después de cargar el carrito obliga a volver a cargar la reserva.

La cancelación o el vencimiento libera el stock y deja el anticipo pendiente de resolver. La política comercial definirá cuándo corresponde devolver, retener o reasignar el importe. La entrega vincula la seña a la venta en la misma transacción que consume la reserva. Los reintentos conservan el registro original y el anticipo permanece inmutable.

El reintegro de una venta con una seña aplicada se documenta en el [ADR 0030](0030-reintegros-con-sena-aplicada.md).
