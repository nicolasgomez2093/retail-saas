# Alta rápida de productos, variantes, SKU, códigos, stock y etiquetas — validación

Implementado y validado el 23/11/2026. **Pendiente de revisión humana.** No se inició otra etapa.

Evidencia completa: `/home/nicolas/Documentos/RespaldosRetail/Retail-AltaRapida-20261123-162144/evidencia` (capturas, comparaciones, JSON de cada recorrido, PDF de etiquetas, logs y scripts reproducibles en `scripts/`).

## Qué existía y se reutilizó

Numeración de SKU por comercio con bloqueo del comercio e índice único; código interno Code 128 y su historial de retiros; maestros de talles y colores; lotes de variantes idempotentes; listas de precios por sucursal con auditoría; movimientos de stock auditados (`postMovement`); diálogo de etiquetas con JsBarcode, vista previa, 60 × 40 / 100 × 40 mm y «Usar stock vendible»; escaneo en Vender; pestañas de la ficha (se conservan para consultas y ediciones).

## Qué se creó o cambió

- `POST /api/v1/quick-products`: una transacción crea producto, variantes, SKU, códigos, precios y stock inicial, con idempotencia y permisos por operación (ver ADR 0053).
- Migración 045: `product_variants.options` (atributos adicionales) y `catalog_quick_creations` (resultado inmutable).
- Combinaciones duplicadas: lote, edición y verificación de maestro comparan también `options`.
- Ticket de venta: los atributos adicionales se guardan junto al talle en la instantánea.
- Código interno nuevo de 16 dígitos (los anteriores no entraban en 60 × 40 mm). Los existentes no cambian.
- Pantalla «Crear producto» de carga rápida (`QuickProductCreate.tsx`, `quick-create.css`, `quickProduct.ts`): nombre, tipo simple o con variantes, «Más detalles», plantillas (Ropa, Pantalones, Jeans, Calzado, Infantil, Cinturones, Perfumería, Talle único, Personalizado), valores propios, atributos nuevos, matriz editable, precio general con excepciones, cantidad en bloque, código interno o existente con escáner (con o sin Enter), SKU manual opcional, resumen lateral, revisión y «Guardar y terminar» / «Guardar y preparar etiquetas».
- Etiquetas: cantidad sugerida = unidades ingresadas («Usar unidades ingresadas»), atributos adicionales en la etiqueta.
- Ficha, vista rápida, POS y selector de variantes muestran los atributos adicionales.
- Existencias (pedido durante la entrega): un único estado vacío cuando la sucursal no tiene ubicaciones, una sola línea para fallas de filtros auxiliares y eliminación automática de un filtro de ubicación de otra sucursal.

## Pruebas ejecutadas

| Comando / recorrido | Resultado |
|---|---|
| `make check` | Código 0 (gofmt, vet, ESLint, 25 pruebas Node, typecheck, build). Advertencia de tamaño de bundle preexistente. |
| `make test-db` (PostgreSQL 17 descartable) | Código 0. Incluye el subtest «Alta rápida transaccional»: sin lista → 409 sin escrituras, cajero 403, remera 6×10, reintento idempotente, otro cuerpo → 409, jean de 3 atributos, color inactivo rechazado, combinación repetida, simple con código de fabricante y precio 0, código duplicado, cantidad negativa y precio inválido, nombre vacío y ausencia de datos parciales. |
| `e2e.mjs` (API + Vite + Chrome reales) | Remera Prueba Integral: 6 variantes, 6 SKU y 6 códigos únicos, ARS 24.500, 60 unidades, un solo POST pese al doble clic, 60 etiquetas (PDF de 60 páginas de 60 × 40 mm, impresión solo por acción explícita), reimpresión con los mismos códigos, escaneo en Vender, venta, stock 9 + 5 × 10 = 59 tras recargar, precios y códigos intactos. |
| `types.mjs` | Simple con escaneo sin Enter, pantalón 8, jean 4 (largo persistido y en POS), calzado 10, atributo personalizado (persistencia, etiqueta, búsqueda), errores de nombre, fila, código y SKU duplicados, corte de red después de confirmar con reintento sin duplicar producto ni stock, altas concurrentes con SKU y códigos distintos. |
| `pos.mjs` | Código inexistente, variante inactiva, lecturas repetidas (cantidad 2), sin precio (bloqueado) y sin stock (rechazo del servidor al confirmar). |
| `compare.mjs` | Reposo, hover, active y focus-visible del botón primario y del secundario idénticos al HTML (color, borde, sombra, elevación, radio, alto, contorno). |

Responsive: 390, 768, 1024, 1440 y 720 px (equivalente a 1440 al 200 %), sin desborde horizontal y con aserción del ancho de la columna principal.

## Comparación visual

`evidencia/comparaciones/`: HTML aprobado (izquierda) y React (derecha) para Crear producto y Variantes en los cuatro anchos, Precios, Etiquetas (1440 y 390) y Vender. Defectos detectados y corregidos durante el recorrido: ancho limitado por `.app-shell form`, choque con una clase `.quick-card` existente, círculos pendientes, matriz en tarjetas a 1440, columna colapsada en móvil, enlaces cortados, borde de hover del primario y estado active.

Diferencias intencionales: el HTML no tiene carga unificada. Se extendieron sus `.card`, `.panelhead`, `.formgrid`, `.chip`, `.note`, `.grid2`, `.sidebox`, la tabla y el modal. A 768 px el prototipo comprime el formulario («Ren», placeholders cortados); React pasa a una columna.

## Limitaciones

- No probado con impresora térmica ni lector USB físico: la impresión se validó con el PDF generado por Chrome y el escaneo con entrada de teclado simulada.
- Los códigos internos `RI-…` emitidos antes de este cambio siguen necesitando el formato de 100 × 40 mm.
- Los atributos adicionales no se corrigen desde el editor individual de variantes.
- La foto y el costo no se cargan en el alta: la foto se agrega en la ficha y el costo al recepcionar compras.
- El arrastre de avisos entre vistas reportado en Existencias no se reprodujo en el entorno de prueba; se corrigió la causa probable (filtros de otra sucursal) y el apilamiento de avisos.
- El entorno Docker local (`retail-platform-api-1`) no se reconstruyó: para usar el alta rápida ahí hay que ejecutar `make migrate` y `make up` (no se modificó infraestructura).
