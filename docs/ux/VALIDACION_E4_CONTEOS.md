# B3.6.3 / E4 — Validación de Inventario → Conteos

**APTO PARA REVISIÓN HUMANA.** Validación sobre PostgreSQL 17 descartable y Chromium con API Go local. Los datos usados son fixtures.

## Funcionalidades

- Historial de la sucursal activa con estado, ubicación, fecha, responsable y progreso. Permite buscar, filtrar y paginar de a 25 sesiones. Incluye ubicaciones archivadas al consultar el historial.
- Alta con ubicación y observación. El borrador admite hasta 100 variantes, búsqueda por nombre o SKU, escaneo exacto, corrección, eliminación y observaciones por línea. Vacío significa sin contar. El cero se guarda explícitamente. Guardar el borrador deja el stock intacto.
- Revisión del saldo vigente con cantidad observada, diferencias firmadas, totales y advertencias por reservas. Si el saldo cambia, el cierre informa las variantes en conflicto, conserva lo contado y exige una nueva revisión.
- Cierre atómico con movimientos `count_adjustment` para las diferencias efectivas. Las líneas no contadas conservan su saldo. Un conteo con todas las diferencias en cero se completa sin movimientos.
- Detalle con fecha de cierre, responsable, ubicación, saldo de referencia, cantidad contada, ajuste aplicado y observaciones. Los enlaces entre Conteos y Movimientos conservan la referencia histórica. Una confirmación exitosa permanece visible si el refresco falla.

## Contratos, permisos y almacenamiento

| Contrato bajo `/inventory/counts` | Permiso | Función |
| --- | --- | --- |
| `GET /history`, `GET /:id` | `inventory:read` | Historial y detalle |
| `POST /` con `draft: true` | `inventory:adjust` | Abre una sesión vacía con la clave de alta existente |
| `PUT /:id/draft` | `inventory:adjust` | Guarda líneas con revisión esperada |
| `GET /:id/review` | `inventory:adjust` | Relee los saldos y sus revisiones |
| `POST /:id/close` | `inventory:adjust` | Aplica diferencias y conserva el resultado por clave |
| `GET /:id/close-result` | `inventory:adjust` y actor original | Recupera el resultado por clave |
| `POST /:id/cancel` | `inventory:adjust` | Cancela un borrador y conserva el historial |

El backend valida tenant, sucursal y ubicación. La guarda transaccional existente y los bloqueos de saldos protegen la comparación de revisiones y el registro de movimientos. Los conflictos `count_stale`, `count_reserved` y `count_draft_stale` devuelven 409. Una clave confirmada queda vinculada al actor, la sesión y el contenido. Ante una respuesta perdida, la interfaz verifica primero mediante GET y ofrece reintentar explícitamente con la misma clave si aún no hay confirmación.

La migración [044_inventory_count_drafts.sql](../../apps/api/migrations/044_inventory_count_drafts.sql) agrega revisiones del borrador, datos de idempotencia y snapshots para el historial nuevo. Se mantiene el contrato legado para sus consumidores. El cierre legado rechaza sesiones administradas por el flujo nuevo. [OpenAPI](../openapi.yaml)

## Archivos

| Área | Archivos |
| --- | --- |
| Inventario Go | `counts.go`, `movement_history.go` y `count_workflow.go` (nuevo), en `apps/api/internal/modules/inventory/` |
| API y datos | `apps/api/internal/platform/httpapi/counts.go`, `integration_test.go` del mismo directorio, migración 044 (nueva) y `docs/openapi.yaml` |
| Interfaz | `StockCounts.tsx`, `Inventory.tsx` y `Movements.tsx` en `apps/web/src/features/inventory/`, además de `App.tsx`, `shared/api/client.ts` y `style.css` en `apps/web/src/` |

## Pruebas ejecutadas

| Ejecución | Resultado |
| --- | --- |
| `make check` | Salida 0. Go vet y tests, 14/14 pruebas Node, lint y compilación web. [Log](../../../Documentos/RespaldosRetail/Conteos-E4-mv4vo0gg/evidencia/make-check.log) |
| `GIN_MODE=release make test-db` | Salida 0. Incluye `TestIdentityAcrossTenants/conteos_fisicos_con_borrador_y_cierre` en PostgreSQL descartable. [Log](../../../Documentos/RespaldosRetail/Conteos-E4-mv4vo0gg/evidencia/test-db.log) |
| Chromium con API real | Seis grupos satisfactorios. [Resultados y traza HTTP](../../../Documentos/RespaldosRetail/Conteos-E4-mv4vo0gg/evidencia/chromium-resultados.json) · [Log](../../../Documentos/RespaldosRetail/Conteos-E4-mv4vo0gg/evidencia/chromium.log) |

| Escenario | Evidencia |
| --- | --- |
| Creación, cantidad positiva, cero explícito, omisión, corrección y entradas inválidas | PostgreSQL y Chromium |
| Cierre sin diferencias, movimientos firmados y conservación de snapshots al renombrar el producto | PostgreSQL |
| Tenant y sucursal ajenos, rol sin escritura y permiso de lectura ausente | PostgreSQL. Chromium confirmó que CASHIER consulta el cierre sin acciones de escritura |
| Dos cierres concurrentes, rechazo del segundo cierre y recuperación con clave original | PostgreSQL |
| Saldo cambiado entre revisión y cierre, 409 con variantes, cantidades conservadas y ausencia de ajustes parciales | PostgreSQL y Chromium |
| Reservas y fallo inyectado en la inserción del segundo movimiento con reversión completa | PostgreSQL |
| Pérdida simulada de una respuesta real de cierre, recuperación tras recargar y doble clic con un solo POST | Chromium |
| GET posterior con 500, reintento exclusivo de lectura, navegación Conteos ↔ Movimientos y descarte de la respuesta tardía de otra sucursal | Chromium |

## Capturas

| Estado | Móvil | Tablet | Escritorio compacto | Escritorio |
| --- | --- | --- | --- | --- |
| Listado | [390 px](../../../Documentos/RespaldosRetail/Conteos-E4-mv4vo0gg/evidencia/listado-390.png) | [768 px](../../../Documentos/RespaldosRetail/Conteos-E4-mv4vo0gg/evidencia/listado-768.png) | [1024 px](../../../Documentos/RespaldosRetail/Conteos-E4-mv4vo0gg/evidencia/listado-1024.png) | [1440 px](../../../Documentos/RespaldosRetail/Conteos-E4-mv4vo0gg/evidencia/listado-1440.png) |
| Borrador | [390 px](../../../Documentos/RespaldosRetail/Conteos-E4-mv4vo0gg/evidencia/borrador-390.png) | [768 px](../../../Documentos/RespaldosRetail/Conteos-E4-mv4vo0gg/evidencia/borrador-768.png) | [1024 px](../../../Documentos/RespaldosRetail/Conteos-E4-mv4vo0gg/evidencia/borrador-1024.png) | [1440 px](../../../Documentos/RespaldosRetail/Conteos-E4-mv4vo0gg/evidencia/borrador-1440.png) |
| Revisión | [390 px](../../../Documentos/RespaldosRetail/Conteos-E4-mv4vo0gg/evidencia/revision-390.png) | [768 px](../../../Documentos/RespaldosRetail/Conteos-E4-mv4vo0gg/evidencia/revision-768.png) | [1024 px](../../../Documentos/RespaldosRetail/Conteos-E4-mv4vo0gg/evidencia/revision-1024.png) | [1440 px](../../../Documentos/RespaldosRetail/Conteos-E4-mv4vo0gg/evidencia/revision-1440.png) |
| Resultado | [390 px](../../../Documentos/RespaldosRetail/Conteos-E4-mv4vo0gg/evidencia/resultado-390.png) | [768 px](../../../Documentos/RespaldosRetail/Conteos-E4-mv4vo0gg/evidencia/resultado-768.png) | [1024 px](../../../Documentos/RespaldosRetail/Conteos-E4-mv4vo0gg/evidencia/resultado-1024.png) | [1440 px](../../../Documentos/RespaldosRetail/Conteos-E4-mv4vo0gg/evidencia/resultado-1440.png) |

Capturas adicionales a 390 px. [Confirmación](../../../Documentos/RespaldosRetail/Conteos-E4-mv4vo0gg/evidencia/confirmacion-390.png) · [Conflicto](../../../Documentos/RespaldosRetail/Conteos-E4-mv4vo0gg/evidencia/conflicto-390.png) · [Recarga fallida](../../../Documentos/RespaldosRetail/Conteos-E4-mv4vo0gg/evidencia/recarga-fallida-390.png) · [Solo lectura](../../../Documentos/RespaldosRetail/Conteos-E4-mv4vo0gg/evidencia/solo-lectura-390.png)

En las 16 mediciones no hubo desbordamiento horizontal ni controles fuera del ancho visible. La altura mínima de los controles medidos fue 44 px. Se verificaron Escape y el retorno de foco en el diálogo.

## Respaldo y limitaciones

- Respaldo persistente en `/home/nicolas/Documentos/RespaldosRetail/Conteos-E4-mv4vo0gg`. Se verificaron 11 originales antes de editar y las 11 copias volvieron a coincidir en SHA-256 y tamaño al finalizar. [Manifiesto](../../../Documentos/RespaldosRetail/Conteos-E4-mv4vo0gg/MANIFIESTO.txt) · [Verificación posterior](../../../Documentos/RespaldosRetail/Conteos-E4-mv4vo0gg/evidencia/respaldo-verificado.txt)
- En conteos legados sin snapshots, las etiquetas recuperadas se identifican como datos actuales.
- Se probó el escaneo mediante entrada de teclado. Quedan sin ejecutar las pruebas con lector físico y lector de pantalla.
