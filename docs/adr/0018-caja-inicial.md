# ADR 0018 — Caja física y arqueo

Las cajas pertenecen a una sucursal. El propietario puede dar de alta cajas en sus sucursales activas asignadas. El cajero abre y opera sus propias sesiones. El propietario y el encargado pueden supervisar sesiones de sus sucursales asignadas. El cajero ve si otra sesión ocupa la caja y conserva acceso solo a los importes y movimientos de sus propias sesiones.

La base admite una sola sesión abierta por caja. La apertura registra el saldo inicial con un importe mayor o igual a cero. Los ingresos y egresos manuales exigen un importe positivo y un motivo. El tipo determina el signo del movimiento. El efectivo esperado es saldo inicial más los movimientos confirmados. Un egreso que supera ese efectivo se rechaza.

Al cerrar, el sistema guarda el efectivo esperado, el declarado y su diferencia, calculada como declarado menos esperado. La diferencia puede ser negativa, cero o positiva. Las operaciones de caja se serializan por comercio y bloquean la sesión al escribir. Los movimientos confirmados son inmutables. Los importes se guardan como NUMERIC(18,2) y la API los intercambia como texto decimal exacto.

La apertura y los movimientos usan claves de idempotencia por comercio. Un cierre repetido con el mismo declarado devuelve el arqueo registrado. Esta entrega cubre efectivo físico. Los cobros de ventas en efectivo se describen en ADR 0019. La conciliación de medios electrónicos queda para otra etapa.
