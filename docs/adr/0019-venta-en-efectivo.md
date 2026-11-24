# ADR 0019 — Primera venta de mostrador en efectivo

El operador selecciona una caja abierta y una ubicación vendible de la misma sucursal. El carrito sale de una sola ubicación. Los permisos de caja se aplican también a la venta. Un cajero opera sus sesiones y un supervisor puede intervenir en las de sus sucursales asignadas.

La API resuelve cada precio dentro de la transacción y calcula el total. Una variante sin precio bloquea la venta. El precio visto por el operador sirve para detectar un cambio entre el carrito y la confirmación. Si cambió, la pantalla actualiza los importes y pide confirmar nuevamente. El efectivo recibido debe cubrir el total. El vuelto se calcula con aritmética exacta y la caja recibe solo el neto. Un precio cero explícito conserva ese importe.

La transacción guarda la venta, el pago, las líneas con datos y precios históricos, los movimientos de stock, el efecto en caja y la numeración correlativa por sucursal. Bloquea la sesión y las filas de stock en orden de variante y protege las reservas. Las ventas fallidas revierten los cambios, incluida la numeración. El identificador del operador y la hora quedan registrados en la venta y sus movimientos.

La clave de idempotencia tiene alcance de comercio. Un reintento con la misma clave, actor y contenido devuelve la venta original, incluso después de cerrar la caja, sujeto al acceso vigente. Si el navegador pierde la respuesta, mantiene el intento original en la pantalla para reintentarlo sin editar el carrito. La consulta de una venta devuelve los datos fijados al confirmar.

Esta entrega registra y muestra una venta numerada en efectivo. El ticket imprimible se detalla en ADR 0020. Los otros medios de pago, descuentos y devoluciones siguen su propia secuencia.
