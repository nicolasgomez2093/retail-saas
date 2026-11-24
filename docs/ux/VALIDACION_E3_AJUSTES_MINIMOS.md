# B3.6.3 / E3 — Validación de Inventario → Ajustes y mínimos

**APTO PARA REVISIÓN HUMANA.** Los ajustes y mínimos se validaron con la API local, PostgreSQL descartable y Chromium.

## Funcionalidad

- **Ajustar stock** reutiliza la búsqueda por nombre o SKU y el escáner exacto. Al seleccionar la variante muestra SKU, talle, color, sucursal, ubicación, físico, reservado y vendible. La entrada es una diferencia firmada, según el contrato existente. Distingue saldo ausente de cero registrado y explica el físico inicial de cero para un primer ingreso.
- La vista previa muestra físico anterior, diferencia y físico resultante. Motivo, límites y reserva se validan antes del diálogo. El usuario confirma explícitamente. El resultado contiene la referencia y enlaces al detalle de Existencias y al movimiento histórico.
- **Configurar mínimos** lista variantes activas por ubicación con búsqueda y paginación. Distingue mínimo ausente, cero y positivo. La alerta sigue la regla existente y se activa al alcanzar o bajar del mínimo del disponible de la ubicación. La cifra vendible se muestra como dato separado. Cambiar un mínimo conserva el físico y el historial.
- Existencias enlaza a Ajustes con variante y ubicación autorizadas. Movimientos muestra el ajuste con su signo y su trazabilidad. La presentación de fechas usa día, mes y año de cuatro cifras, el identificador del responsable queda en un desplegable seleccionable y «Más filtros» usa un fondo más suave con foco visible.
- Al enviar o verificar una escritura se bloquea temporalmente el cambio de sucursal. Las lecturas se cancelan o descartan al cambiar el ámbito. La sucursal activa se verifica en cada petición.

## Contratos, permisos y consistencia

| Operación | Garantía |
|---|---|
| `GET /api/v1/inventory/existences` y detalle existente | Devuelven el mínimo por ubicación filtrada y, para cada ubicación del detalle, `revision` del saldo y `minimumVersion`. Se mantiene la distinción entre `null` y cero. Requiere `inventory:read`. |
| `POST /api/v1/inventory/adjustments` | Reutiliza `Idempotency-Key` y admite `expectedRevision`. La pantalla siempre envía esa precondición. El servidor verifica ubicación activa, ficha activa en el flujo condicionado, disponibilidad reservada y revisión bajo la transacción que registra el saldo y el movimiento. `adjustment_stale` e `adjustment_inactive` se devuelven como 409. |
| `GET /api/v1/inventory/adjustments/result` | Consulta por clave con el mismo actor, comercio y sucursal. Requiere `inventory:adjust`. 404 indica que todavía no se observa un ajuste confirmado o que la operación queda fuera de alcance. |
| `PUT /api/v1/inventory/minimums` | Se conserva la granularidad tenant/ubicación/variante y `inventory:manage_minimums`. `expectedVersion` compara el token UUID de la última lectura o `unrecorded` si el mínimo no existe. El servidor valida antes de actualizar y devuelve `minimum_stale` ante un cambio. |

La revisión del saldo ya existente cambia aunque el físico vuelva al mismo valor. La migración `042_inventory_adjustment_precondition.sql` vincula la revisión a la clave mediante un campo aditivo del movimiento. La migración `043_inventory_minimum_version.sql` agrega un UUID de versión para cada mínimo. Los cambios efectivos renuevan ese token. Las migraciones se ejecutaron sobre PostgreSQL descartable. Las llamadas anteriores sin precondición mantienen su comportamiento compatible. Los detalles del contrato están en [OpenAPI](../openapi.yaml).

Si se pierde la respuesta, la pantalla mantiene localmente la clave y el cuerpo original en `sessionStorage`, por usuario y sucursal. La primera acción ofrecida es consultar el resultado. Si todavía no se encuentra, el reenvío explícito usa exactamente la misma clave y cuerpo. Un 2xx de ajuste conserva la referencia aunque el siguiente GET falle, y el reintento de ese caso es solo de lectura.

## Resultados de las pruebas

| Verificación | Resultado |
|---|---|
| `make check` | Pasó con 14 pruebas frontend, lint y compilación Go y TypeScript. [Log final](../../../Documentos/RespaldosRetail/Ajustes-Minimos-E3-vmv57mpu/evidencia/make-check-final.log). |
| `GIN_MODE=release make test-db` | Pasó con PostgreSQL 17 descartable. El caso E3 cubre presión concurrente con una sola confirmación, ingresos, egresos, ajuste hasta cero, tenant y sucursal, inactivos, permisos, otra carga con la misma clave, recuperación y mínimos. Un trigger de fixture hizo fallar la inserción del movimiento después de la escritura de saldo. El saldo y su revisión se revirtieron y la clave quedó sin movimiento confirmado. [Log final](../../../Documentos/RespaldosRetail/Ajustes-Minimos-E3-vmv57mpu/evidencia/test-db-final.log). |
| Chromium con API real | Pasaron nueve recorridos. Incluyen selección por SKU y código con ceros iniciales, validación, doble clic, 409, navegación histórica, 500 de refresco tras éxito, pérdida de 201, recuperación tras recargar, POST que no llegó, alcance de CASHIER y edición de mínimos. [Resultados y traza](../../../Documentos/RespaldosRetail/Ajustes-Minimos-E3-vmv57mpu/evidencia/chromium-resultados.json). |
| Cambio de sucursal | El selector se mantuvo deshabilitado durante el POST retenido. En paralelo, un GET antiguo de Central se liberó después de seleccionar Norte y la pantalla conservó el saldo de Norte. |
| Responsive y foco | Se midieron ocho combinaciones de flujo y ancho, con controles principales de al menos 44 px de alto y sin desbordamiento horizontal. Se inspeccionaron las capturas y se redujo el buscador tras elegir variante. Escape devuelve el foco al botón de revisión. Cerrar un mínimo devuelve el foco a su fila, incluso después de refrescarla. |

## Capturas

| Estado | 390 px | 768 px | 1024 px | 1440 px |
|---|---|---|---|---|
| Ajuste con vista previa | [Ver](../../../Documentos/RespaldosRetail/Ajustes-Minimos-E3-vmv57mpu/evidencia/ajuste-390.png) | [Ver](../../../Documentos/RespaldosRetail/Ajustes-Minimos-E3-vmv57mpu/evidencia/ajuste-768.png) | [Ver](../../../Documentos/RespaldosRetail/Ajustes-Minimos-E3-vmv57mpu/evidencia/ajuste-1024.png) | [Ver](../../../Documentos/RespaldosRetail/Ajustes-Minimos-E3-vmv57mpu/evidencia/ajuste-1440.png) |
| Mínimos con edición | [Ver](../../../Documentos/RespaldosRetail/Ajustes-Minimos-E3-vmv57mpu/evidencia/minimos-390.png) | [Ver](../../../Documentos/RespaldosRetail/Ajustes-Minimos-E3-vmv57mpu/evidencia/minimos-768.png) | [Ver](../../../Documentos/RespaldosRetail/Ajustes-Minimos-E3-vmv57mpu/evidencia/minimos-1024.png) | [Ver](../../../Documentos/RespaldosRetail/Ajustes-Minimos-E3-vmv57mpu/evidencia/minimos-1440.png) |

Las capturas adicionales muestran el [editor de ajuste en móvil](../../../Documentos/RespaldosRetail/Ajustes-Minimos-E3-vmv57mpu/evidencia/ajuste-editor-390.png), el [editor de mínimos en móvil](../../../Documentos/RespaldosRetail/Ajustes-Minimos-E3-vmv57mpu/evidencia/minimos-editor-390.png), la [confirmación](../../../Documentos/RespaldosRetail/Ajustes-Minimos-E3-vmv57mpu/evidencia/confirmacion-390.png), el [conflicto concurrente](../../../Documentos/RespaldosRetail/Ajustes-Minimos-E3-vmv57mpu/evidencia/conflicto-390.png), el [resultado con enlaces](../../../Documentos/RespaldosRetail/Ajustes-Minimos-E3-vmv57mpu/evidencia/confirmado-390.png), el [historial](../../../Documentos/RespaldosRetail/Ajustes-Minimos-E3-vmv57mpu/evidencia/movimientos-detalle-390.png) y el [rol de solo lectura](../../../Documentos/RespaldosRetail/Ajustes-Minimos-E3-vmv57mpu/evidencia/solo-lectura-390.png).

## Archivos y respaldo

| Archivos | Cambio |
|---|---|
| `apps/api/internal/modules/inventory/movements.go`, `minimums.go`, `existences.go` | Revisión, recuperación, garantías de mínimos y tokens de lectura. |
| `apps/api/internal/platform/httpapi/inventory.go`, `integration_test.go` | Rutas, códigos de error y pruebas reales. |
| `apps/api/migrations/042_inventory_adjustment_precondition.sql`, `043_inventory_minimum_version.sql` | Migraciones aditivas para las precondiciones. |
| `apps/web/src/features/inventory/Adjustments.tsx`, `Inventory.tsx`, `StockMinimum.tsx`, `Existences.tsx`, `Movements.tsx` | Flujo responsive, integración y acciones de teclado. |
| `apps/web/src/app/App.tsx`, `apps/web/src/shared/api/client.ts`, `apps/web/src/style.css`, `docs/openapi.yaml` | Ámbito de sucursal, cliente, estilos y contrato. |

Respaldo persistente de los 13 originales en `/home/nicolas/Documentos/RespaldosRetail/Ajustes-Minimos-E3-vmv57mpu`. SHA-256 y tamaños se cotejaron antes de editar y se verificaron nuevamente. [Manifiesto](../../../Documentos/RespaldosRetail/Ajustes-Minimos-E3-vmv57mpu/MANIFIESTO.txt) · [Verificación](../../../Documentos/RespaldosRetail/Ajustes-Minimos-E3-vmv57mpu/evidencia/respaldo-verificado.txt).

## Límites de validación

La revisión de accesibilidad se hizo en Chromium con teclado y emulación de anchos. No se ejecutó con lector de pantalla, móvil físico ni lector físico de códigos. El selector de fecha nativo conserva las convenciones de presentación del navegador y del sistema operativo.
