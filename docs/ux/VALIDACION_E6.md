# B3.6.2 / E6 — Validación de matriz de variantes

**APTO CON CONDICIONES para revisión humana.** La creación masiva y su recuperación están verificadas. Los códigos que exceden el ancho seguro de 60 × 40 mm mantienen el bloqueo de impresión. Falta prueba física con impresora y lector.

## Funcionalidad y reglas

- **Generar variantes** está en el detalle de productos activos para usuarios con `catalog:manage`. Admite talles, colores, el cruce de ambos y la opción explícita sin atributos. Los maestros inactivos quedan fuera de las opciones nuevas.
- La selección muestra el total antes de generar la previsualización. Se pueden excluir filas o elegir solo las necesarias, incluso cuando el cruce inicial supera 50. La API rechaza más de 50 por lote y mantiene el límite de 64 KiB del cuerpo específico de E6.
- La previsualización permite editar SKU, seleccionar estado inicial, solicitar un código interno y excluir combinaciones. Reusa la secuencia de E5 sin reservar identificadores. Revisar y renovar sugerencias conserva los SKU editados manualmente. Actualizar los catálogos no descarta las selecciones. La adopción de revisiones nuevas se hace explícitamente.
- En el guardado se vuelven a verificar producto, comercio, permisos, estado y revisión de cada maestro, combinación y SKU. Se considera el UUID del maestro o el texto legado comparado sin mayúsculas ni espacios de borde. También se consideran variantes inactivas y atributos ausentes. Los conflictos incluyen fila, motivo y el UUID, SKU y estado de una variante existente cuando corresponde.
- Las combinaciones creadas por la matriz quedan protegidas también ante el alta o la edición individual posterior. Una reasociación que mantenga el texto tampoco puede duplicar una combinación protegida por UUID. Las combinaciones históricas se conservan. El comportamiento aceptado del alta individual y CSV fuera de las combinaciones de matriz permanece vigente. E6 no fusiona variantes ni impone una restricción retroactiva sobre duplicados heredados.
- Las salidas del editor con cambios se confirman. El intento incierto queda disponible para reintento explícito mientras se conserve el editor. No hay reenvío automático. Abandonar o recargar tras confirmar el aviso de salida descarta el intento local. La protección persistida en el servidor sigue vigente.

## Contrato y persistencia

| Operación | Ruta y resultado |
|---|---|
| Previsualizar | `POST /api/v1/products/{id}/variants/preview` devuelve 200 con SKU sugerido y errores por fila. |
| Confirmar o recuperar | `POST /api/v1/products/{id}/variants/batch` requiere `Idempotency-Key`. Devuelve 201 con UUID del lote y variantes, incluyendo códigos definitivos. |
| Conflicto | 409 identifica una clave usada por un actor o solicitud diferentes, o las filas que impiden guardar. Un tamaño fuera del rango 1–50 produce 400. |

Cada fila puede incluir `size` y `color` con `id` y `revision`. Los campos nulos dejan el atributo vacío. Se envían `sku`, `status` y `generateInternal`. El SKU vacío es válido solo en previsualización. La API está documentada en [OpenAPI](../openapi.yaml).

[041_catalog_variant_batches.sql](../../apps/api/migrations/041_catalog_variant_batches.sql) agrega la marca de origen de matriz y una tabla de resultados confirmados. No modifica movimientos ni snapshots históricos. Las claves se asocian con tenant, actor, producto y hash de contenido normalizado. La respuesta confirmada también cumple la función de tombstone. Se conserva sin vencimiento automático y la base rechaza actualizarla o borrarla.

El lote usa el lock de escritura del tenant, antes de leer las combinaciones y los SKU. Las altas, los códigos internos, los avances de secuencia y el resultado idempotente se confirman en la misma transacción. Un error en cualquier fila revierte el lote. Las variantes creadas no reciben saldos ni movimientos de stock.

## Pruebas ejecutadas

| Prueba | Resultado |
|---|---|
| `make check` | Pasó Go, vet, ESLint, las ocho pruebas Node, TypeScript y Vite. Sigue la advertencia de Vite sobre el paquete principal de 579,06 kB minificados. |
| `GIN_MODE=release make test-db` | Pasó sobre PostgreSQL 17 descartable. Incluye las cuatro formas de combinación, variantes inactivas, texto legado, duplicados, referencias ajenas, maestro inactivo u obsoleto, permiso denegado, máximo de 50 con SKU Unicode, rechazo de 51, SKU manuales y sugerencias, código interno, ausencia de saldos y regresiones E3/T03, E4 y E5. |
| Fallos de fila y carreras | Se forzó una colisión de escritura en primera, intermedia y última fila. Se verificó que no quedaron variantes, códigos, auditorías, contadores avanzados ni resultado parcial. Dos sesiones independientes compitieron por una combinación y por un SKU. La segunda esperó el lock de la primera mediante una barrera observada con `pg_blocking_pids`. En ambos casos un solo lote se confirmó. |
| Idempotencia | Se recuperaron UUID y código originales con el mismo cuerpo. Se rechazó la misma clave con distinto actor o contenido. Se comprobó la recuperación con un registro de fixture fechado ocho días antes y el rechazo de borrado del registro. |
| Chromium/CDP y API real | Se excluyeron filas de un cruce de 56, se detectó un duplicado inactivo, se conservó un SKU manual al renovar sugerencias y se recuperó el mismo lote después de perder deliberadamente la respuesta 201. Se verificaron cuerpo y clave idénticos, escaneo del código creado, foco al entrar y volver, Escape de etiqueta y ausencia de desbordamiento horizontal a 390, 768, 1024 y 1440 px. |
| Límites de ejecución | Las primeras compilaciones concurrentes agotaron el espacio de `/tmp`. Las dos verificaciones completas se repitieron con temporales en `/home`. Los recorridos comerciales completos en navegador, lector de pantalla y hardware físico quedan sin ejecutar en esta ronda. |

[Registro de make check](/home/nicolas/Documentos/RespaldosRetail/E6-19w3ftbz/make-check-final.log) · [Registro PostgreSQL](/home/nicolas/Documentos/RespaldosRetail/E6-19w3ftbz/test-db-final.log) · [Resultados Chromium](/home/nicolas/Documentos/RespaldosRetail/E6-19w3ftbz/evidencia/chromium.json)

## Ajustes pendientes de E5

- Los botones de sugerencia del alta individual quedan en una fila propia debajo del texto. Se comprobó la separación con las coordenadas de los controles a [768 px](/home/nicolas/Documentos/RespaldosRetail/E6-19w3ftbz/evidencia/sku-768.png) y [1024 px](/home/nicolas/Documentos/RespaldosRetail/E6-19w3ftbz/evidencia/sku-1024.png).
- Con el módulo de 0,254 mm y diez módulos de margen de lectura por lado, el símbolo completo del código interno de prueba necesitó **75,5 mm**. La etiqueta admite 54 mm con el margen de composición existente. El código de prueba excede incluso los 60 mm del papel, por lo que reducir el margen de composición no resuelve este caso. La UI informa las medidas y bloquea la impresión del código largo. Seleccionar un código alternativo válido habilita la impresión y conserva todos los datos del identificador. Chromium produjo un PDF de una página de aproximadamente 60 × 40 mm. La lectura y la impresión físicas quedan pendientes. [Bloqueo](/home/nicolas/Documentos/RespaldosRetail/E6-19w3ftbz/evidencia/etiqueta-bloqueada-390.png) · [Alternativa](/home/nicolas/Documentos/RespaldosRetail/E6-19w3ftbz/evidencia/etiqueta-alternativa-390.png) · [PDF](/home/nicolas/Documentos/RespaldosRetail/E6-19w3ftbz/evidencia/etiqueta-alternativa.pdf)

## Capturas, archivos y respaldo

- Matriz [390 px](/home/nicolas/Documentos/RespaldosRetail/E6-19w3ftbz/evidencia/matriz-390.png) · [768 px](/home/nicolas/Documentos/RespaldosRetail/E6-19w3ftbz/evidencia/matriz-768.png) · [1024 px](/home/nicolas/Documentos/RespaldosRetail/E6-19w3ftbz/evidencia/matriz-1024.png) · [1440 px](/home/nicolas/Documentos/RespaldosRetail/E6-19w3ftbz/evidencia/matriz-1440.png)
- [Límite excedido](/home/nicolas/Documentos/RespaldosRetail/E6-19w3ftbz/evidencia/limite-390.png) · [Resultado incierto](/home/nicolas/Documentos/RespaldosRetail/E6-19w3ftbz/evidencia/incierto-390.png) · [Resultado confirmado](/home/nicolas/Documentos/RespaldosRetail/E6-19w3ftbz/evidencia/confirmado-1440.png)
- [Manifiesto SHA-256 del respaldo permanente](/home/nicolas/Documentos/RespaldosRetail/E6-19w3ftbz/MANIFIESTO.txt), cotejado antes de editar y al finalizar.

| Área | Archivos |
|---|---|
| PostgreSQL | `apps/api/migrations/041_catalog_variant_batches.sql` |
| Catálogo Go | `apps/api/internal/modules/catalog/batch.go` y `variants.go` |
| HTTP e integración | `apps/api/internal/platform/httpapi/catalog_batches.go`, `catalog.go` e `integration_test.go` |
| Frontend | `apps/web/src/features/catalog/VariantMatrix.tsx`, `Catalog.tsx`, `LabelDialog.tsx`, `apps/web/src/shared/api/client.ts` y `apps/web/src/style.css` |
| Contrato e informe | `docs/openapi.yaml` y `docs/ux/VALIDACION_E6.md` |

La migración se ejecutó únicamente en PostgreSQL descartable. Quedan pendientes la aplicación a datos reales, la validación física de etiquetas y la aceptación humana.
