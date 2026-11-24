# Recorridos comerciales

## A — Preparar para vender

Desde Productos → Crear, ingresar nombre obligatorio, descripción, categoría y marca opcionales. Guardar confirma únicamente el producto. La ficha muestra secciones navegables y acciones siguientes. Imágenes opcionales con carga y error propios. Variantes permiten talle/color opcionales, maestros activos, SKU obligatorio sugerido editable y códigos opcionales; alta individual o matriz existente.

En Precios se identifica sucursal y lista aplicable. Ingresar importes decimales en moneda del comercio, conservando cero válido y ausencia como estados distintos. Advertir que una lista compartida puede afectar otras sedes antes de editarla. Sin lista asignada, ofrecer configuración autorizada contextual.

En Mercadería se elige una ubicación operativa y se registra una entrada mediante ajuste justificado existente o recepción de compra. No guardar un saldo como atributo del producto. Una revisión de saldo obsoleta obliga a recalcular y confirmar. Resultado incierto conserva clave y ofrece consulta segura.

Resumen por variante muestra actividad, precio resuelto y disponibilidad leída por sucursal. Falta de permiso o consulta fallida produce desconocido. Ofrecer Probar en Vender con variante identificada; el POS vuelve a validar. Los productos parciales permanecen recuperables, sin borradores persistentes nuevos. Edición posterior usa expectedRevision, conserva cambios y ofrece comparar ante 409.

## B — Venta completa

Inicio o Vender → buscar nombre/SKU o código exacto → seleccionar variante y ubicación válida → cantidad → cliente opcional, obligatorio en fiado → descuentos permitidos → elegir pagos y confirmar → resultado de venta y ticket interno. Entradas principales son variante, cantidad, pagos, cliente y sesión operativa. El servidor resuelve precio y valida disponibilidad. Mantener el carrito si falla una lectura. Una respuesta incierta exige recuperar la misma operación; imprimir después de confirmar. El resultado enlaza historial, caja y existencias. ARCA y venta offline quedan fuera.

## C — Inventario

Inventario → filtrar por producto/sede/ubicación → detalle de variante con físico, reservado y vendible → Ajustar con contexto → cantidad final y motivo → revisión → confirmación → movimiento y saldo actualizado. Conteos permite múltiples cantidades guardadas sin modificar stock y cierre atómico con revisiones. Transferencias conserva sus estados propios. Error auxiliar no borra saldos disponibles.

## D — Precios

Ficha → Precios → leer lista y sucursal → ingresar importe → confirmar alcance → guardar → volver a resolver precio de sucursal → consultar desde POS. El contrato actual escribe un importe por variante/lista. Edición de varias variantes debe informar cada resultado hasta disponer de operación atómica aprobada. Un PUT confirmado seguido de GET fallido sigue registrado. No cambiar silenciosamente de sede ante 404.

## E — Operación diaria

Inicio → revisar sesión propia de caja → abrir si corresponde → operar ventas/cobros → revisar movimientos y ventas → arqueo y cierre. Mostrar alertas confirmadas con acción concreta. Ocultar sección de avisos si no hay tareas. Los indicadores tienen carga y error independientes. Una caja abierta por otro usuario no implica que el cajero pueda cobrar.

## F — Reposición

Existencias con mínimo configurado → revisar variantes afectadas → crear orden de compra con proveedor y cantidades → recibir parcial en ubicación autorizada → confirmar cantidades aceptadas → consultar movimientos y faltantes. Costos opcionales conservan ausencia. La sugerencia de reposición automática y conversión masiva de mínimos a orden requieren contratos y decisiones adicionales; primera entrega permite iniciar orden con selección revisada.

En todos los recorridos se elimina el salto sin contexto entre producto, precio y stock mediante paneles del mismo espacio de trabajo. Cada escritura mantiene su propia transacción, revisión y recuperación; la ficha no promete atomicidad entre módulos.
