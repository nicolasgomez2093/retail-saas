# B3.6.3 / E1 — Validación de Inventario → Existencias

**APTO CON CONDICIONES para revisión humana.** Los recorridos críticos de consulta, alcance y concurrencia pasaron. Quedan las verificaciones manuales indicadas al final.

## Implementación

- `#/inventario/existencias` y su alias `#/stock` conservan la consulta en URL. Buscan por nombre o SKU y filtran por categoría, marca, estado de producto o variante, disponibilidad, ubicación y mínimo. Una URL inválida exige corrección explícita y bloquea las consultas de Existencias. La sucursal procede del selector global.
- El listado agrupa las variantes de la página por producto, con tabla de escritorio y filas compactas en móvil. Pagina en el servidor de a 25 filas con `hasMore`. Distingue catálogo vacío, búsqueda sin coincidencias, página agotada, producto sin variantes, cero y saldo sin registrar. El detalle conserva los filtros y muestra el desglose de sucursal; la navegación por teclado mueve el foco al enlace de regreso.
- El escáner reutiliza `BarcodeScanner` y la búsqueda exacta de Catálogo, incluidos los ceros iniciales. El resultado dirige al detalle de stock de la sucursal activa. Los accesos a Movimientos, Ajustes y mínimos, Conteos, Transferencias y Catálogo respetan los permisos y el contexto de ubicación que la operación admite. La aplicación actual carece de un acceso dedicado a reservas desde Inventario.
- Ubicaciones, categorías y marcas tienen estados de consulta y reintentos propios. Una falla auxiliar deja disponible el listado ya autorizado. La lectura se puede actualizar manualmente. Los cambios de filtros, ruta o contexto invalidan las lecturas anteriores. El cambio global de sucursal conserva los filtros que tienen sentido en ambas y vuelve a la página 1 si había detalle, ubicación o página específica.
- La autorización se verifica en PostgreSQL para la identidad autenticada con tenant, sucursal activa asignada y permiso `inventory:read`. El frontend cancela o descarta lecturas pendientes mediante el ámbito observable, la generación de la consulta y la intención vigente. El flujo existente del selector global y su bloqueo durante operaciones pendientes siguen aplicando.

## API y disponibilidad

La lectura anterior `GET /api/v1/inventory` permanece disponible para los flujos existentes. No podía paginar por variante, incluir productos sin variantes o distinguir saldo inexistente de cero. Se agregaron dos lecturas documentadas en [OpenAPI](../openapi.yaml).

| Endpoint | Resultado |
|---|---|
| `GET /api/v1/inventory/existences?store_id=…` | Página filtrada, resumen del conjunto filtrado y permisos de acceso contextual. |
| `GET /api/v1/inventory/existences/{variantId}?store_id=…` | Identificación de variante, saldo de sucursal y ubicaciones. Incluye ubicaciones inactivas con saldo o mínimo de la variante. |

La sucursal se verifica antes de consultar saldos. Un filtro por ubicación requiere una ubicación activa de esa sucursal. Para la consulta por ubicación, «Sin saldo registrado» significa que no hay fila de saldo para la variante en esa ubicación. Al abrir el detalle se ven todas las ubicaciones aplicables de la sucursal.

- **Físico** y **reservado** son los saldos registrados por ubicación. **Vendible** usa la diferencia en ubicaciones activas y vendibles únicamente para productos y variantes activos. Una ubicación en cuarentena o inactiva puede aportar unidades físicas y aporta cero al vendible. El stock en tránsito se incorpora cuando la recepción confirma el saldo de destino.
- **Sin saldo registrado** corresponde a `null`. Un registro en cero o con disponibilidad cero se muestra como `0`. Un error de consulta conserva su propio estado. La lectura es una fotografía consistente de PostgreSQL y no promete disponibilidad al momento de una futura operación.
- **Alerta de mínimo** compara la disponibilidad de cada ubicación activa con su mínimo configurado, siguiendo la regla de Inventario. El resumen cuenta variantes distintas. Los productos físicos se cuentan por UUID de producto. El frontend solo presenta cantidades recibidas.
- La lista usa una consulta agregada sin llamadas por fila. Se aplican filtros antes de paginar. El detalle usa una lectura de la variante y una consulta de sus ubicaciones dentro de la misma transacción de solo lectura.
- La entrega no necesita migraciones ni modifica movimientos, recepciones, reservas, POS, permisos del servidor ni cálculos contables.

## Validación

| Prueba | Evidencia |
|---|---|
| `make check` | [Log](../../../Documentos/RespaldosRetail/Existencias-E1-59hk2ljy/evidencia/make-check.log). Incluye lint, tests y compilación Go y TypeScript. 11 pruebas frontend. |
| `GIN_MODE=release make test-db` con PostgreSQL 17 descartable | [Log](../../../Documentos/RespaldosRetail/Existencias-E1-59hk2ljy/evidencia/test-db.log). Cobertura de filtros, 401/403/404, roles OWNER, STORE_MANAGER, CASHIER e INVENTORY_OPERATOR, tenant y sucursal ajenos, mínimo, ubicaciones inactivas y paginación sobre 400 variantes. La medición local de las páginas 1 y 16 de un producto de 400 variantes, 80 con saldo, fue de 169,8 ms en total. Incluye lecturas después de compra, ajuste, reserva, despacho/recepción y devolución a cuarentena. |
| Chromium/CDP con API real y datos sintéticos | [Resultados y traza](../../../Documentos/RespaldosRetail/Existencias-E1-59hk2ljy/evidencia/chromium-resultados.json). Ocho recorridos incluyen búsqueda, escáner por teclado, historial, detalle, error auxiliar y recuperación, URL inválida sin GET, sesión vencida y una consulta por actualización sin GET por fila. |
| Respuesta tardía A → B → A | Chromium retuvo el GET original para A (7), verificó B (42) y la consulta vigente a A (11), y después liberó el GET original. La pantalla conservó 11. |
| Responsive | Chromium verificó dimensiones y ausencia de desbordamiento horizontal del documento. Se inspeccionaron encabezados, tabla, filas móviles y el detalle por ubicación. |
| OpenAPI | YAML parseado localmente y esquemas, rutas y tipos nulos comprobados. |

Capturas de [390 px](../../../Documentos/RespaldosRetail/Existencias-E1-59hk2ljy/evidencia/existencias-390.png), [768 px](../../../Documentos/RespaldosRetail/Existencias-E1-59hk2ljy/evidencia/existencias-768.png), [1024 px](../../../Documentos/RespaldosRetail/Existencias-E1-59hk2ljy/evidencia/existencias-1024.png) y [1440 px](../../../Documentos/RespaldosRetail/Existencias-E1-59hk2ljy/evidencia/existencias-1440.png). Evidencia adicional de [filas móviles](../../../Documentos/RespaldosRetail/Existencias-E1-59hk2ljy/evidencia/existencias-filas-390.png), [detalle móvil](../../../Documentos/RespaldosRetail/Existencias-E1-59hk2ljy/evidencia/existencias-detalle-390.png), [detalle de escritorio](../../../Documentos/RespaldosRetail/Existencias-E1-59hk2ljy/evidencia/existencias-detalle-1440.png) y [URL inválida](../../../Documentos/RespaldosRetail/Existencias-E1-59hk2ljy/evidencia/existencias-url-invalida-768.png).

## Archivos y respaldo

| Archivos | Cambio |
|---|---|
| `apps/api/internal/modules/inventory/existences.go`, `apps/api/internal/modules/inventory/inventory.go` | Agregación, detalle y reutilización de permisos de sucursal. |
| `apps/api/internal/platform/httpapi/inventory_existences.go`, `apps/api/internal/platform/httpapi/inventory.go`, `docs/openapi.yaml` | Validación y contratos para las dos lecturas. |
| `apps/web/src/features/inventory/Existences.tsx`, `apps/web/src/features/inventory/existencesQuery.ts`, `apps/web/src/style.css` | Vista, validación de URL y estilos localizados. |
| `apps/web/src/app/App.tsx`, `apps/web/src/app/Navigation.tsx`, `apps/web/src/features/inventory/Inventory.tsx`, `apps/web/src/shared/api/client.ts` | Contexto global, compatibilidad de enlaces y accesos a las operaciones existentes. |
| `apps/api/internal/platform/httpapi/integration_test.go`, `apps/web/existences-query.test.js` | Cobertura PostgreSQL y Node. |

Respaldo persistente de nueve originales en `/home/nicolas/Documentos/RespaldosRetail/Existencias-E1-59hk2ljy`. [Manifiesto SHA-256 y tamaños](../../../Documentos/RespaldosRetail/Existencias-E1-59hk2ljy/MANIFIESTO.txt) y [verificación de las copias](../../../Documentos/RespaldosRetail/Existencias-E1-59hk2ljy/evidencia/respaldo-verificado.txt).

## Límites y revisión

El navegador verificó teclado, foco, Enter y Escape. No se ejecutaron pruebas con lector de pantalla ni con un lector de códigos físico. La medición de 400 variantes es local y no representa una prueba de carga de producción. La regresión de operaciones comerciales se ejecutó en PostgreSQL; Chromium cubrió la consulta y el recorrido hacia Movimientos. El bloqueo del selector con una venta pendiente conserva la implementación existente. Ese recorrido de POS no se repitió en Chromium en esta entrega.
