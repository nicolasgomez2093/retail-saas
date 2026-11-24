# B3.6.2 / E4 — Validación

**Veredicto: APTO para revisión humana.** Talles, colores y grupos están disponibles en Productos → Catálogo → Herramientas → Talles y colores. Los maestros pertenecen a cada comercio y las correcciones efectivas de variantes conservan las garantías de T03.

## Implementación

- Alta, consulta, edición y activación/desactivación de talles, colores y grupos, sin borrado físico. Los listados permiten buscar, filtrar por estado y ordenar por el valor configurado o por nombre. Los talles admiten un grupo opcional. Los colores admiten una muestra hexadecimal.
- Los selectores de variante admiten talle o color vacío, agrupan talles por el orden configurado y ofrecen búsqueda cuando hay más de ocho opciones. Un valor inactivo asociado sigue visible. Los formularios conservan los cambios ante errores y muestran la revisión actual antes de continuar tras un conflicto.
- La pantalla Asociaciones pendientes lista variantes con texto sin vínculo, con búsqueda y paginación. Abre el editor existente de la variante. Una asociación con el mismo texto no cambia su identidad. Una etiqueta diferente requiere OWNER, motivo, confirmación, revisión y validación de historial. Los códigos, precios y saldos siguen sus flujos existentes.
- El texto registrado en la variante es el que muestran la expansión, el escáner y la etiqueta. Renombrar o desactivar un maestro conserva ese texto y el vínculo. Aplicar explícitamente una etiqueta nueva pasa por T03. El servidor comprueba la etiqueta esperada contra el maestro para rechazar una selección que quedó obsoleta mientras se editaba.
- Los maestros tienen revisiones independientes. PostgreSQL garantiza unicidad por comercio y tipo para nombres y códigos equivalentes por mayúsculas y espacios de borde. Las relaciones compuestas validan comercio y tipo, tanto para los grupos como para las variantes. Bajo el lock de escritura del comercio, una asignación nueva mediante maestros rechaza una combinación de talle/color ya presente, incluida una variante inactiva. Una asociación histórica equivalente conserva el UUID y los duplicados heredados.

## Migración y API

La migración incremental [039_catalog_attributes.sql](../../apps/api/migrations/039_catalog_attributes.sql) agrega la tabla de maestros y las referencias opcionales `size_id` y `color_id`. Vincula automáticamente grafías inequívocas por comercio. Si un grupo de comparación contiene textos originales diferentes, por ejemplo `M` y `m`, los deja para revisión. Los textos vacíos quedan sin referencia. No hay maestros universales ni conversión de acentos o equivalencias semánticas. En la migración, UUID, revisiones, textos y snapshots anteriores conservan sus valores. La prueba usa datos anteriores a 039, incluyendo una variante con un movimiento de stock histórico.

- `GET` y `POST /api/v1/catalog/{sizes|colors|size-groups}`; `PATCH` de cada recurso por ID, con `expectedRevision`.
- `GET /api/v1/catalog/pending-attributes`, con búsqueda y paginación de 25.
- El alta y el PATCH de variantes aceptan referencias opcionales. El detalle, la búsqueda exacta por código y las respuestas de escritura las devuelven. Las integraciones que usan los textos históricos pueden seguir usándolos. Los archivos CSV mantienen su contrato; un valor textual posteriormente importado puede quedar pendiente de revisión.
- Las lecturas de maestros usan los permisos de consulta de Catálogo. Las escrituras de maestros y la consulta de pendientes requieren `catalog:manage`. El control OWNER de T03 se aplica aparte en correcciones sensibles. [OpenAPI](../openapi.yaml)

## Pruebas ejecutadas

| Prueba | Resultado |
|---|---|
| `make check` | Pasó lint, vet, Go, 8 pruebas Node, TypeScript y Vite. Sigue la advertencia de tamaño del paquete principal, 555,35 kB minificados. |
| `GIN_MODE=release make test-db` con PostgreSQL 17 descartable | Pasó. Incluye migración previa a 039 con seis variantes de dos comercios, UUID y revisión intactos, un snapshot de stock conservado, grafías ambiguas, valores ausentes y asociaciones inequívocas. La integración cubre aislamiento, permisos, duplicado concurrente, edición concurrente, inactivos, reasociación, renombre y T03 con reserva abierta o cerrada. |
| Regresión Go/API | Pasaron los escenarios existentes de Catálogo, CSV, fotos, búsqueda exacta, precios, stock, POS, compras, reservas y devoluciones, incluidas las carreras de T03. |
| Chromium/CDP con API real | Se crearon talle y color, se resolvió un conflicto de edición conservando un cambio concurrente en otro campo, se vinculó un texto equivalente, se corrigió un color con confirmación y se verificaron escáner, etiqueta y cierre con Escape. Se comprobó el ancho del documento a 390, 768, 1024 y 1440 px. |
| Lectores de pantalla, impresión física y recorridos UI exhaustivos de ventas y compras | No ejecutados en esta ronda. |

- Herramientas [390 px](/home/nicolas/Documentos/RespaldosRetail/E4-t6e8i3oq/evidencia/maestros-390.png) · [768 px](/home/nicolas/Documentos/RespaldosRetail/E4-t6e8i3oq/evidencia/maestros-768.png) · [1024 px](/home/nicolas/Documentos/RespaldosRetail/E4-t6e8i3oq/evidencia/maestros-1024.png) · [1440 px](/home/nicolas/Documentos/RespaldosRetail/E4-t6e8i3oq/evidencia/maestros-1440.png)
- Variantes [390 px](/home/nicolas/Documentos/RespaldosRetail/E4-t6e8i3oq/evidencia/variante-390.png) · [768 px](/home/nicolas/Documentos/RespaldosRetail/E4-t6e8i3oq/evidencia/variante-768.png) · [1024 px](/home/nicolas/Documentos/RespaldosRetail/E4-t6e8i3oq/evidencia/variante-1024.png) · [1440 px](/home/nicolas/Documentos/RespaldosRetail/E4-t6e8i3oq/evidencia/variante-1440.png)
- Registros [make check](/home/nicolas/Documentos/RespaldosRetail/E4-t6e8i3oq/make-check-final.log) · [PostgreSQL](/home/nicolas/Documentos/RespaldosRetail/E4-t6e8i3oq/test-db-final.log) · [Chromium](/home/nicolas/Documentos/RespaldosRetail/E4-t6e8i3oq/evidencia/chromium.json) · [Respaldo permanente SHA-256](/home/nicolas/Documentos/RespaldosRetail/E4-t6e8i3oq/MANIFIESTO.txt)

## Archivos

| Área | Archivos |
|---|---|
| Base de datos | `039_catalog_attributes.sql`, `catalog_attributes_test.go` |
| Catálogo Go | `attributes.go`, `catalog.go`, `variants.go` |
| HTTP Go | `catalog_attributes.go`, `catalog.go`, `integration_test.go` |
| React/TypeScript | `CatalogAttributes.tsx`, `Catalog.tsx`, `client.ts`, `style.css` |
| Contrato y validación | `docs/openapi.yaml`, este informe |

Las variantes con textos ambiguos requieren una decisión del comercio. Los datos reales no se migraron durante esta entrega. La validación de combinación agregada a E4 se aplica a asignaciones mediante maestros; mantiene la compatibilidad de altas textuales y CSV y conserva duplicados anteriores. La resolución de asociaciones es individual. E5 y E6 permanecen fuera del alcance.
