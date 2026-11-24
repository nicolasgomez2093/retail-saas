# ADR 0027 — Reintegros en efectivo

El cajero registra un reintegro desde una recepción de devolución existente. La primera implementación abarca ventas pagadas íntegramente en efectivo y sin seña aplicada. Requiere acceso a la venta y a una sesión de caja abierta de la misma sucursal. El operador confirma la entrega de efectivo por el importe histórico de la recepción.

La API controla que el importe sea positivo, que la suma de reintegros de la venta esté cubierta por su pago original y que la caja tenga efectivo suficiente. Guarda un reintegro por recepción y su egreso de caja en una sola transacción. El reintento conserva el reintegro original incluso después del cierre de la sesión. El historial de la recepción muestra el reintegro y la venta conserva su ticket histórico.

La extensión a reintegros electrónicos de un solo medio se detalla en el [ADR 0028](0028-reintegros-por-medio-original.md).
