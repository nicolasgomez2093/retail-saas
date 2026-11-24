# ADR 0024 — Reservas de mercadería

Una reserva requiere un cliente y una ubicación vendible de la misma sucursal. El usuario con permiso de operación de caja puede administrar reservas de sus sucursales asignadas sin abrir una sesión de caja. Al reservar, el sistema incrementa el stock reservado y conserva el físico. Las ventas normales, los ajustes y las transferencias siguen respetando las unidades apartadas.

El vendedor puede convertir la reserva completa en una venta desde el carrito. Se aplican los precios vigentes de la sucursal al confirmar el cobro. La transacción consume las unidades reservadas, registra su salida física y vincula la reserva con la venta. Ante un fallo, la reserva conserva sus unidades. La venta fiada de una reserva exige conservar el cliente original.

La fecha de vencimiento es opcional y se interpreta en la hora del dispositivo al ingresarla. Después de la fecha, la reserva queda bloqueada para la venta hasta liberar su stock mediante **Registrar vencimiento** o **Cancelar reserva**. La cancelación y el vencimiento liberan únicamente el stock reservado. Las señas se detallan en el [ADR 0025](0025-senas-de-reservas.md). Queda pendiente la política de vencimiento automático.
