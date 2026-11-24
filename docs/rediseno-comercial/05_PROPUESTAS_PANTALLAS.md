# Experiencia navegable · Etapa A

Abrir [prototipo.html](prototipo.html) directamente en Chromium, Chrome o un navegador moderno. Funciona desde archivo local. No requiere servidor ni conexión al backend. Recargar reinicia los productos creados, el carrito y las ventas.

## Cómo recorrer la propuesta

1. En Productos, elegir Crear producto.
2. Completar nombre, descripción, categoría y marca opcionales. Se puede seleccionar una imagen local.
3. Guardar. Los datos generales quedan confirmados en la simulación y se habilitan las demás secciones.
4. En Variantes, elegir S, M, L y Crudo, Negro. Previsualizar seis combinaciones, editar SKU, solicitar código interno o excluir una fila. Confirmar crea el lote seleccionado.
5. En Precios, ingresar un importe por variante, revisar el alcance compartido y guardar cada fila.
6. En Mercadería, seleccionar variante y Salón, ingresar cantidad física final y motivo. Revisar y confirmar el ajuste.
7. En Resumen, identificar precios y saldos pendientes por variante. Consultar en Vender.
8. Elegir una variante preparada, ajustar cantidades y descuentos, cobrar, confirmar y consultar el ticket simulado.
9. Volver al catálogo, historial o existencias para comprobar la coherencia de los datos simulados.

Se ofrecen una camisa sin precio, un pantalón sin stock, un buzo sin variantes, un producto inactivo y una campera con error de consulta. Una variante de la remera inicial está inactiva y permanece visible en su ficha.

## Pantallas y composición

| Pantalla | Distribución e interacción | Adaptación |
| --- | --- | --- |
| Inicio | Cabecera de jornada, ventas de demo, caja personal y accesos a productos, compras e informes | Resumen de dos columnas en móvil; accesos apilados |
| Productos | Filas con miniatura, nombre, clasificación, variantes, precio local, stock y preparación; búsqueda y filtros | Tabla alineada amplia; filas compactas apiladas |
| Crear | Información general, imagen local y estado. Un guardado inicial independiente | Formulario de dos columnas en escritorio y una en móvil |
| Editar | Datos generales, Variantes, Precios, Mercadería y Resumen en la misma ficha | Resumen lateral en ancho amplio; pasa debajo cuando falta espacio |
| Detalle | Identidad e imagen, estado y preparación por variante; acceso a editar y Vender | Ficha vertical con datos jerarquizados |
| Matriz | Selección de maestros, contador, combinaciones editables, exclusión y errores de SKU | Tabla en escritorio; filas con etiquetas en móvil |
| Precios | Lista y alcance, importe y guardado por variante, errores persistentes | Filas compactas con acción propia |
| Mercadería | Ubicación, saldo, cantidad final, diferencia, motivo y confirmación | Una operación legible en móvil, campos agrupados en escritorio |
| Resumen | Estado, precio y disponibilidad por variante con acciones de continuación | Dos columnas internas en móvil; grilla alineada amplia |
| Vender | Búsqueda y selección de producto a la izquierda, carrito y cobro a la derecha | Catálogo de dos columnas y acceso fijo al carrito en móvil |
| Cobro | Total, cliente, medios de pago, efectivo recibido y confirmación | Diálogo accesible adaptado al viewport |
| Resultado | Ticket interno, importe, vuelto y acciones siguientes | Documento centrado sin tabla ancha |
| Historial | Ventas realizadas en la sesión, importe y ticket | Filas adaptativas |
| Existencias | Consulta coherente con las operaciones simuladas | Desglose compacto por variante |
| Gestión | Descubribilidad de caja, compras, proveedores e informes | Accesos secundarios agrupados |

Gestión y Clientes sirven de contexto de navegación. Sus recorridos operativos completos se mantienen para entregas posteriores. Cada acción visible tiene un comportamiento o informa su alcance mediante un diálogo.

## Estados y escenarios para revisar

- Catálogo. Búsqueda sin coincidencias, carga y error con reintento desde Escenarios de revisión.
- Matriz. Duplicar un SKU, excluir una combinación existente, generar sólo talles, sólo colores o una combinación sin atributos. El lote con más de 50 filas bloquea la confirmación.
- Precios. Guardar cero y observar Configurado. Activar Próximo guardado con error conserva el importe y permite reintentar la misma fila.
- Mercadería. Activar Próximo ajuste con conflicto simula un cambio real del saldo de referencia. La cantidad ingresada se conserva y requiere nueva revisión.
- Resumen. La campera muestra precio y disponibilidad desconocidos. Reintentar recupera la lectura ficticia.
- POS. La camisa sin precio y el pantalón sin stock impiden agregar su variante. El escenario Caja no disponible permite consultar el catálogo y conserva el carrito, con cobro deshabilitado.
- Cobro. El escenario Error al confirmar interrumpe la simulación antes de procesar; conserva el carrito. Una nueva confirmación explícita permite terminar.
- Fiado requiere cliente. Pago mixto reparte el total entre efectivo y transferencia. La selección de cliente para venta común es una propuesta que requiere la adaptación identificada en 07.
- Los cambios de datos generales y de matriz sin confirmar solicitan descarte voluntario al salir. Cambiar secciones conserva sus valores en memoria.

## Límites del prototipo

La asignación de SKU/códigos, los ajustes, los pagos y los tickets son simulaciones. Las sugerencias de SKU no reservan identificadores. La foto sólo se lee localmente. La autorización no se prueba con esta maqueta. No hay impresión física ni comprobante fiscal. El estado favorable de preparación describe datos consultados y no garantiza una venta futura.

Las solicitudes de precio de cada fila siguen siendo independientes. El prototipo no ofrece atomicidad de precios. El conflicto de inventario representa el flujo de recuperación existente. El diseño de conflicto de precio depende de la precondición pendiente detallada en 07.

## Validación y evidencia

La validación del prototipo se realiza con Chromium/CDP, archivo local y datos ficticios. Las capturas son estados renderizados reales de esta propuesta. La revisión del producto sigue pendiente de aprobación humana.

Las capturas y registros citados se retiraron del repositorio el 23/11/2026 para publicarlo. Se conservan en `~/Documentos/RespaldosRetail/Retail-Limpieza-20261123-231630/original/docs/rediseno-comercial/evidencias`.

| Familia | 390 px | 768 px | 1024 px | 1440 px |
| --- | --- | --- | --- | --- |
| Catálogo | `catalogo-390.png` | `catalogo-768.png` | `catalogo-1024.png` | `catalogo-1440.png` |
| Matriz | `matriz-390.png` | `matriz-768.png` | `matriz-1024.png` | `matriz-1440.png` |
| Resumen | `resumen-390.png` | `resumen-768.png` | `resumen-1024.png` | `resumen-1440.png` |
| POS | `pos-390.png` | `pos-768.png` | `pos-1024.png` | `pos-1440.png` |

Otros estados
- Crear producto (`crear-1440.png`), error de precio (`precio-error-1440.png`), conflicto de stock (`stock-conflicto-1440.png`).
- Caja no disponible (`pos-caja-no-disponible-1440.png`), cobro (`cobro-1440.png`), error recuperable (`cobro-error-1440.png`), resultado (`resultado-1440.png`).
- Catálogo vacío móvil (`vacio-390.png`), error móvil (`catalogo-error-390.png`), datos desconocidos (`desconocido-390.png`), código inexistente (`escaner-inexistente-390.png`).
- Emulación de zoom 200% (`zoom-200.png`). Viewport de 720 × 500 y escala de rasterización 2 equivalentes al espacio disponible de una ventana de 1440 × 1000 a 200%; no se operó el menú de zoom del navegador.
- Resultados automatizados (`resultados.json`) y registro de ejecución (`validacion.log`).

La inspección visual detectó y corrigió el recorte de sucursal móvil, una acción de fila con salto de línea incómodo y un desbordamiento al ampliar. La prueba funcional detectó y corrigió la colisión de un nombre JavaScript con el historial nativo que impedía la confirmación de salida. Se añadió acceso fijo al carrito móvil para evitar volver a buscarlo debajo del catálogo.

Se calcularon los contrastes principales con luminancia relativa WCAG. Texto sobre blanco 13,19:1; ayuda sobre blanco 4,86:1; acción verde/blanco 7,11:1; advertencia 5,47:1 y error 5,64:1. Estos valores cubren los pares indicados, no certifican todos los estados posibles.

No se ejecutó lector de pantalla ni hardware físico de escaneo. Las pruebas de teclado usan eventos Chromium/CDP. La integración productiva, permisos reales, concurrencia PostgreSQL y recuperación real de ventas se verificarán en la implementación aprobada.

## Resultado de la ejecución final

Doce grupos funcionales satisfactorios y 17 mediciones de ancho sin desbordamiento. Cero excepciones JavaScript y cero solicitudes HTTP externas. Se comprobó el recorrido completo hasta la venta y también la preparación de las cinco variantes mediante operaciones individuales de precio y mercadería.

Evidencia adicional de la edición y el resultado completo
- Filas editables de matriz en móvil (`matriz-edicion-390.png`).
- Cinco variantes preparadas en escritorio (`preparacion-completa-1440.png`).
- Cinco variantes preparadas en móvil (`preparacion-completa-390.png`).

Los originales de prototipo.html y los documentos 04, 05 y 07 se respaldaron en /home/nicolas/Documentos/RespaldosRetail/Rediseno-A-20261123-094131. El manifiesto SHA-256 y VERIFICACION_FINAL.txt acreditan las cuatro copias legibles. El script CDP usado se conserva en ese respaldo como evidencia reproducible.

Los documentos 01, 02, 03 y 06 se conservaron. La precisión nueva sobre el agregado Preview existente se registra en 07 para evitar proponer un endpoint redundante. La aplicación productiva, sus pruebas y la base de datos permanecieron sin cambios.
