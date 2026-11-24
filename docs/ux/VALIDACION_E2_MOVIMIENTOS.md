# B3.6.3 / E2 — Validación de Inventario → Movimientos

**APTO PARA REVISIÓN HUMANA.** La consulta, los filtros, la navegación con Existencias y la presentación responsive quedaron implementados y validados.

## Funcionalidad

- La vista `#/inventario/movimientos` y el alias `#/stock/movimientos` muestran el historial físico de la sucursal activa. Hay tabla en anchos suficientes y filas compactas en móvil. El detalle muestra fecha local, cantidad con signo, físico anterior y posterior, SKU histórico, producto, atributos disponibles, ubicación, motivo, referencias y responsable.
- La consulta admite ajustes, conteos, despachos y recepciones de transferencias, compras, ventas, devoluciones y salidas por cambio. La sucursal actual se autoriza en cada petición. Un extremo de transferencia de otra sucursal se muestra únicamente si está dentro del alcance asignado. Un ajuste o conteo informa que origen y destino no aplican a la corrección física.
- La búsqueda usa nombre o SKU histórico. Los filtros incluyen tipo, fecha, ubicación afectada, variante, responsable y UUID del movimiento o documento. Se mantienen en URL, Atrás/Adelante y al volver desde el detalle. La URL cruda se valida antes de consultar. Un cambio de sucursal descarta ubicación, detalle, referencias y cursores de la sucursal anterior.
- La paginación usa hasta 25 registros en orden descendente por fecha y UUID, con cursores para ir a movimientos más antiguos o más recientes. Un ingreso concurrente en la primera página no desplaza las filas que debe devolver el cursor siguiente.
- Desde el detalle de Existencias se abre Movimientos por UUID de variante. El enlace inverso consulta el stock actual de esa variante. La cantidad histórica pertenece al físico de la ubicación y conserva el significado de la operación registrada.
- Los estados de carga, vacío, búsqueda sin coincidencias, página agotada, error, detalle inexistente, permiso insuficiente y sesión vencida se presentan de forma separada. La relectura corresponde a la vista actual. El control por generación, ámbito y URL invalida respuestas anteriores.

## API e integridad

Se agregaron `GET /api/v1/inventory/movements/history` y `GET /api/v1/inventory/movements/history/{id}`. Se conserva el contrato de la lectura anterior por ubicación. El listado utiliza una consulta relacional paginada y lecturas acotadas para ubicación o cursor, sin una petición por fila. La autenticación y `inventory:read` se aplican en servidor, con aislamiento por tenant y sucursal activa asignada. Se actualizó [OpenAPI](../openapi.yaml). La implementación es de lectura y utiliza el esquema existente.

El SKU procede del registro inmutable. Los snapshots disponibles de líneas de venta, devolución, cambio y compra aportan producto y atributos. Para movimientos que no guardan estos datos, la vista identifica las etiquetas actuales del catálogo. El nombre del responsable y algunos nombres de ubicación proceden de registros actuales. Se mantienen UUID y referencias, incluso con productos, variantes o ubicaciones inactivos. Las referencias comerciales se muestran como identificadores de trazabilidad. Los enlaces existentes de navegación no ofrecen un destino directo uniforme para esas entidades.

## Resultados

| Criterio | Evidencia |
|---|---|
| Compilación, lint y pruebas unitarias | Pasó `make check`, con 14 pruebas frontend, incluidas tres nuevas de URL, calendario y navegación. [Log](../../../Documentos/RespaldosRetail/Movimientos-E2-9fjn_zjw/evidencia/make-check.log). |
| PostgreSQL descartable y API | Pasó `GIN_MODE=release make test-db` con PostgreSQL 17. La integración cubrió paginación con inserción concurrente, desempate por UUID, filtros combinados, snapshots, inactivos, 401/403/404, cursor de otra sucursal, aislamiento y alcance de transferencias. [Log](../../../Documentos/RespaldosRetail/Movimientos-E2-9fjn_zjw/evidencia/test-db.log). |
| Chromium con API real | Pasaron siete recorridos. Incluyeron lista de 25 + 3, filtros por teclado, historial, enlace en ambos sentidos con Existencias, despacho y recepción reales, URL inválida sin GET, 500 inyectado con recuperación, detalle inexistente y 401 real. [Resultados y traza](../../../Documentos/RespaldosRetail/Movimientos-E2-9fjn_zjw/evidencia/chromium-resultados.json). |
| Lecturas fuera de orden | A → B → A por sucursal mantuvo `+11` tras liberar la respuesta anterior de `-2`. El recorrido por filtro mantuvo `+12` tras liberar la respuesta anterior de `+11`. |
| Responsive y accesibilidad | Se verificaron 20 combinaciones de estado y ancho en Chromium. No hubo desbordamiento horizontal ni controles fuera del ancho medido. Los controles medidos de la vista tenían al menos 44 px de alto. Se inspeccionaron las capturas y se corrigieron tamaños de encabezados, saltos de línea y jerarquía móvil. Tab, Enter, Escape y el foco de regreso se comprobaron con teclado. |

## Capturas

| Estado | 390 px | 768 px | 1024 px | 1440 px |
|---|---|---|---|---|
| Listado | [Ver](../../../Documentos/RespaldosRetail/Movimientos-E2-9fjn_zjw/evidencia/listado-390.png) | [Ver](../../../Documentos/RespaldosRetail/Movimientos-E2-9fjn_zjw/evidencia/listado-768.png) | [Ver](../../../Documentos/RespaldosRetail/Movimientos-E2-9fjn_zjw/evidencia/listado-1024.png) | [Ver](../../../Documentos/RespaldosRetail/Movimientos-E2-9fjn_zjw/evidencia/listado-1440.png) |
| Filtros abiertos | [Ver](../../../Documentos/RespaldosRetail/Movimientos-E2-9fjn_zjw/evidencia/filtros-390.png) | [Ver](../../../Documentos/RespaldosRetail/Movimientos-E2-9fjn_zjw/evidencia/filtros-768.png) | [Ver](../../../Documentos/RespaldosRetail/Movimientos-E2-9fjn_zjw/evidencia/filtros-1024.png) | [Ver](../../../Documentos/RespaldosRetail/Movimientos-E2-9fjn_zjw/evidencia/filtros-1440.png) |
| Detalle | [Ver](../../../Documentos/RespaldosRetail/Movimientos-E2-9fjn_zjw/evidencia/detalle-390.png) | [Ver](../../../Documentos/RespaldosRetail/Movimientos-E2-9fjn_zjw/evidencia/detalle-768.png) | [Ver](../../../Documentos/RespaldosRetail/Movimientos-E2-9fjn_zjw/evidencia/detalle-1024.png) | [Ver](../../../Documentos/RespaldosRetail/Movimientos-E2-9fjn_zjw/evidencia/detalle-1440.png) |
| Sin coincidencias | [Ver](../../../Documentos/RespaldosRetail/Movimientos-E2-9fjn_zjw/evidencia/vacio-390.png) | [Ver](../../../Documentos/RespaldosRetail/Movimientos-E2-9fjn_zjw/evidencia/vacio-768.png) | [Ver](../../../Documentos/RespaldosRetail/Movimientos-E2-9fjn_zjw/evidencia/vacio-1024.png) | [Ver](../../../Documentos/RespaldosRetail/Movimientos-E2-9fjn_zjw/evidencia/vacio-1440.png) |
| Error de lectura | [Ver](../../../Documentos/RespaldosRetail/Movimientos-E2-9fjn_zjw/evidencia/error-390.png) | [Ver](../../../Documentos/RespaldosRetail/Movimientos-E2-9fjn_zjw/evidencia/error-768.png) | [Ver](../../../Documentos/RespaldosRetail/Movimientos-E2-9fjn_zjw/evidencia/error-1024.png) | [Ver](../../../Documentos/RespaldosRetail/Movimientos-E2-9fjn_zjw/evidencia/error-1440.png) |

Escenarios adicionales: [transferencia](../../../Documentos/RespaldosRetail/Movimientos-E2-9fjn_zjw/evidencia/transferencia-390.png), [carga](../../../Documentos/RespaldosRetail/Movimientos-E2-9fjn_zjw/evidencia/carga-390.png), [URL inválida](../../../Documentos/RespaldosRetail/Movimientos-E2-9fjn_zjw/evidencia/url-invalida-390.png), [detalle inexistente](../../../Documentos/RespaldosRetail/Movimientos-E2-9fjn_zjw/evidencia/detalle-404-390.png) y [sesión vencida](../../../Documentos/RespaldosRetail/Movimientos-E2-9fjn_zjw/evidencia/sesion-vencida-390.png).

## Archivos

| Archivos | Responsabilidad |
|---|---|
| `apps/api/internal/modules/inventory/movement_history.go` | Lectura autorizada de lista y detalle, filtros, enriquecimiento y paginación. |
| `apps/api/internal/platform/httpapi/inventory_movement_history.go`, `apps/api/internal/platform/httpapi/inventory.go`, `docs/openapi.yaml` | Rutas, validación y contrato aditivo. |
| `apps/web/src/features/inventory/Movements.tsx`, `apps/web/src/features/inventory/movementQuery.ts`, `apps/web/src/style.css` | Vista, validación de URL y estilos. |
| `apps/web/src/app/App.tsx`, `apps/web/src/app/Navigation.tsx`, `apps/web/src/features/inventory/Inventory.tsx`, `apps/web/src/features/inventory/Existences.tsx`, `apps/web/src/shared/api/client.ts` | Navegación, contexto, sustitución del listado anterior y enlace por variante. |
| `apps/api/internal/platform/httpapi/integration_test.go`, `apps/web/movement-query.test.js` | Pruebas nuevas. |

Respaldo persistente de nueve originales en `/home/nicolas/Documentos/RespaldosRetail/Movimientos-E2-9fjn_zjw`. Se comprobó la igualdad de contenido antes de editar y se verificaron SHA-256 y tamaños al finalizar. [Manifiesto](../../../Documentos/RespaldosRetail/Movimientos-E2-9fjn_zjw/MANIFIESTO.txt) · [Verificación](../../../Documentos/RespaldosRetail/Movimientos-E2-9fjn_zjw/evidencia/respaldo-verificado.txt).

## Límites de validación

El esquema exige una ubicación afectada por movimiento. La cobertura de movimientos sin origen o destino se hizo con ajustes y con extremos de transferencia restringidos. Los snapshots ausentes se identifican como datos actuales. No se ejecutaron pruebas con lector de pantalla, dispositivo físico ni carga de producción.
