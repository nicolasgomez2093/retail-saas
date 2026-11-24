# B3.6.2 / E5 — Validación de SKU e identificadores

**Veredicto: APTO CON CONDICIONES para revisión humana.** La numeración, el alta recuperable, el código interno y el historial están implementados. El alcance comprobable de la reutilización y de los datos anteriores a la migración queda delimitado abajo.

## Funcionalidades y contrato

- Al abrir el alta se consulta una sugerencia de SKU. El usuario puede usarla, editarla, conservar su entrada manual o consultar otra. Consultar o cancelar carece de efectos persistentes. El conflicto definitivo de SKU se presenta junto al campo y conserva el formulario.
- Cada comercio tiene su contador en PostgreSQL. Formato inicial de seis dígitos, con prefijo vacío. En Catálogo → Herramientas → Numeración se puede elegir entre 4 y 12 dígitos y un prefijo opcional de hasta 12 caracteres ASCII, que comienza con letra y admite letras, números, guion y guion bajo. Una modificación de formato conserva el ordinal y valida la revisión de la configuración. Un SKU guardado que coincide con el formato vigente puede avanzar el contador. La búsqueda omite valores vigentes y retiros conocidos, incluso fuera de una primera ventana saturada. Los SKU manuales mantienen las reglas previas de texto y unicidad case-insensitive.
- El código interno se asigna al guardar cuando se solicita. Usa `RI-<12 caracteres hexadecimales del tenant>-<10 dígitos>` y un contador independiente. Es Code 128 y no tiene valor como GTIN o EAN. El primero se vuelve principal en ausencia de otros códigos. Si ya existía un principal, se conserva. Hay un máximo de un código interno vigente por variante y de 20 códigos totales. Para reemplazar uno interno, primero se lo retira con una corrección confirmada.
- El alta usa el mismo encabezado `Idempotency-Key` del proyecto. Si coinciden actor, tenant, producto y solicitud normalizada, el servidor devuelve el resultado original, incluidos el UUID y el código. La clave con distinto actor o contenido produce `409 idempotency_conflict`. Los clientes anteriores pueden omitir el encabezado. La UI genera la clave por intento y solo reenvía el mismo cuerpo al elegir Reintentar la misma alta. El borrador y la clave viven en el editor. No se introdujo expiración ni purga automática de claves confirmadas.
- El retiro de un SKU se realiza al reemplazarlo. Quitar un código de la lista activa registra su retiro. Desactivar una variante conserva sus asignaciones. Las consultas de historial se ejecutan por identificador exacto en el comercio. Los resultados muestran retiro, reutilización, origen y motivo. Los snapshots comerciales y los UUID permanecen intactos.
- En móvil se comprimieron los controles de Herramientas y del editor de variantes. El formulario distingue el motivo faltante de la casilla de confirmación faltante.

### Rutas

| Operación | Ruta |
|---|---|
| Sugerencia inicial o siguiente | `GET /api/v1/catalog/sku-suggestion?after=…` |
| Configuración de numeración | `GET` y `PATCH /api/v1/catalog/identifier-settings` |
| Historial exacto | `GET /api/v1/catalog/identifier-history?kind=sku|barcode&value=…` |
| Alta, generación, retiro y reutilización | POST y PATCH existentes de variantes. Se agregan `generateInternal` y `reuseRetired`. Las respuestas incluyen `internalBarcode`. |

Se reutilizan permisos de Catálogo. La escritura sensible exige OWNER, motivo, confirmación y, al editar, `expectedRevision`. [OpenAPI](../openapi.yaml)

## Matriz de retiro y reutilización

| Caso | Conducta verificada |
|---|---|
| Retiro con historial cerrado | Cambia la asignación vigente y registra el evento en la misma transacción. El snapshot conserva el valor original. |
| Retiro con operación abierta | T03 devuelve `409 sensitive_history`. Se conservan la asignación y la revisión. |
| Introducción de valor retirado sin confirmación explícita de reutilización | Devuelve `409 identifier_retired`. La importación CSV, que admite altas y carece de este flujo de confirmación, también bloquea los retiros conocidos. |
| Reutilización explícita de SKU | Permitida con unicidad vigente y luego de comprobar todas las variantes históricas con la misma guarda T03. Una referencia abierta o ambigua bloquea. La variante histórica mantiene UUID y snapshots. |
| Reutilización explícita de código en su variante original | Permitida si las referencias están cerradas y la asignación está libre. La condición de interno se conserva si corresponde. |
| Reutilización de código en otra variante | Devuelve `409 barcode_reuse_unsafe`, incluso con historial cerrado. El sistema carece de evidencia de retiro de etiquetas físicas previas. |
| Código retirado consultado con escáner | Devuelve 404 si carece de asignación operativa. El historial consulta el evento por otra ruta y con permiso. |
| Revisión obsoleta | Devuelve `409 revision_conflict`; conserva los identificadores. |

Los escritores de referencias y Catálogo comparten el lock por tenant de T03. La inserción de un código, el incremento del contador, el CAS de la variante y los registros de auditoría se confirman juntos. La carrera se probó con conexiones independientes y una barrera de PostgreSQL. Si gana una reserva, la reutilización se rechaza. Si gana la reutilización, la reserva usa el SKU vigente de su propia variante, sin trasladar su UUID ni cambiar snapshots anteriores.

## Migración y límites

La nueva [040_catalog_identifiers.sql](../../apps/api/migrations/040_catalog_identifiers.sql) agrega contadores y configuración, marca de código interno, eventos históricos inmutables y altas idempotentes. Reconstruye retiros observables a partir de snapshots de SKU y de la auditoría previa. Un evento reconstruido deja el actor vacío porque no demuestra quién retiró el identificador. Los códigos retirados antes de que existiera una auditoría que los registrara no pueden reconstruirse. La protección de esos códigos queda limitada a los registros disponibles y a la unicidad vigente. La migración se probó en PostgreSQL descartable y queda pendiente en datos reales.

## Pruebas ejecutadas

| Prueba | Resultado |
|---|---|
| `make check` | Pasó. Incluye Go, vet, ESLint, 8 pruebas Node, TypeScript y Vite. Se mantiene la advertencia de tamaño del paquete principal. |
| `GIN_MODE=release make test-db` | Pasó sobre PostgreSQL 17 descartable. Cubre migración previa, configuración y aislamiento, dos usuarios con la misma sugerencia y el mismo SKU, recuperación idempotente, escaneo exacto, colisión histórica, búsqueda en una ventana saturada, códigos internos concurrentes, propietario, revisión obsoleta, retiros y reutilizaciones con ambas carreras. |
| Regresión Go/API | Pasaron las suites existentes de E3/T03 y E4, Catálogo, CSV, precios, fotos, POS, compras, reservas, devoluciones y movimientos. |
| Chromium/CDP y API real | Pasó. Se configuró la numeración, se eligió y editó una sugerencia, se recuperó un conflicto de SKU, se perdió la respuesta posterior al 201 y se recuperaron el mismo UUID y código con un único reintento explícito. Se probaron el principal externo, retiro de código, validaciones separadas, búsqueda por código interno, vista previa de etiqueta y Escape. Los cuatro anchos quedaron sin desbordamiento horizontal del documento. |
| Hardware físico, lector de pantalla y recorridos UI exhaustivos de los módulos comerciales | No ejecutados en esta ronda. |

- Editor [390 px](/home/nicolas/Documentos/RespaldosRetail/E5-g03dowc_/evidencia/editor-390.png) · [768 px](/home/nicolas/Documentos/RespaldosRetail/E5-g03dowc_/evidencia/editor-768.png) · [1024 px](/home/nicolas/Documentos/RespaldosRetail/E5-g03dowc_/evidencia/editor-1024.png) · [1440 px](/home/nicolas/Documentos/RespaldosRetail/E5-g03dowc_/evidencia/editor-1440.png)
- [Numeración móvil](/home/nicolas/Documentos/RespaldosRetail/E5-g03dowc_/evidencia/herramientas-390.png) · [Respuesta incierta](/home/nicolas/Documentos/RespaldosRetail/E5-g03dowc_/evidencia/reintento-390.png) · [Etiqueta Code 128](/home/nicolas/Documentos/RespaldosRetail/E5-g03dowc_/evidencia/etiqueta-390.png)
- [Registro de make check](/home/nicolas/Documentos/RespaldosRetail/E5-g03dowc_/make-check-acceptance.log) · [Registro PostgreSQL](/home/nicolas/Documentos/RespaldosRetail/E5-g03dowc_/test-db-acceptance.log) · [Evidencia Chromium](/home/nicolas/Documentos/RespaldosRetail/E5-g03dowc_/evidencia/chromium.json)
- [Manifiesto permanente SHA-256](/home/nicolas/Documentos/RespaldosRetail/E5-g03dowc_/MANIFIESTO.txt), cotejado antes de editar y al finalizar.

## Archivos

| Área | Archivos |
|---|---|
| Migración y prueba de datos previos | `apps/api/migrations/040_catalog_identifiers.sql`, `catalog_identifiers_test.go` |
| Dominio Catálogo | `apps/api/internal/modules/catalog/identifiers.go`, `catalog.go`, `variants.go`, `csv.go` |
| API y pruebas | `apps/api/internal/platform/httpapi/catalog_identifiers.go`, `catalog.go`, `integration_test.go` |
| Interfaz | `apps/web/src/features/catalog/CatalogIdentifiers.tsx`, `Catalog.tsx`, `apps/web/src/shared/api/client.ts`, `apps/web/src/style.css` |
| Contrato e informe | `docs/openapi.yaml`, `docs/ux/VALIDACION_E5.md` |

Quedan pendientes la revisión humana, la prueba física de lectura e impresión y el tratamiento comercial de las etiquetas que pudieron quedar en circulación. La reasignación entre variantes se mantiene bloqueada para códigos de barras. E6 queda fuera de esta entrega.
