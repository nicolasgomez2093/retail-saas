# ADR 0022 — Fiado en el punto de venta

El punto de venta requiere elegir un cliente de la sucursal para registrar una venta fiada. El alta usa nombre y teléfono opcional. Cada registro pertenece a la sucursal donde se crea. Los usuarios habilitados para operar caja pueden registrar y buscar clientes en sus sucursales asignadas. En cuentas por cobrar, un cajero ve las ventas originadas en sus sesiones y un supervisor ve las de su sucursal asignada.

El saldo inicial se calcula a partir del precio vigente y el anticipo opcional. El anticipo puede repartirse entre los medios de pago existentes y debe dejar un saldo positivo. El efectivo neto del anticipo es el único importe que entra al arqueo. La venta, la salida de stock, el anticipo y la deuda se registran en una transacción idempotente.

El ticket conserva el nombre del cliente y el saldo al momento de vender. La consulta de fiados pendientes usa el saldo vigente de la cuenta por cobrar. Los abonos posteriores se detallan en el [ADR 0023](0023-abonos-de-fiados.md). Quedan pendientes las decisiones sobre límites, vencimientos y cuotas.
