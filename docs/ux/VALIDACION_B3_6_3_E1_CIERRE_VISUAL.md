# B3.6.3 / E1 — Cierre visual de Inventario → Existencias

**Listo para aceptación integral.** Las correcciones de presentación quedaron implementadas y se revisaron con Chromium y API local.

## Cambios

- Una barra compacta reúne el escáner, **Acciones** y **Actualizar**. Acciones conserva los enlaces autorizados y el contexto de ubicación. El escáner cerrado ocupa un solo control.
- Búsqueda, ubicación y disponibilidad quedan a la vista. **Más filtros** concentra los otros criterios e indica cuántos están seleccionados. Los controles, el resumen y la tabla se alinean con el ancho disponible. A 390 px se muestran filas compactas.
- El detalle ordena primero las ubicaciones con unidades vendibles y después el resto de las ubicaciones con unidades. Ceros registrados, mínimos configurados y ubicaciones inactivas con datos quedan visibles. Las ubicaciones activas sin registros se consultan en un desplegable con nombre, código y estado. Se conservan las distinciones entre cero, sin saldo y error.
- Los controles conservan el foco visible con el color de acento de la aplicación. Tab, Enter y Escape permiten recorrer o cerrar acciones, filtros y el detalle desplegable. La navegación por teclado al detalle mantiene el enlace de regreso enfocado.
- Carga, ausencia de coincidencias, página agotada, URL inválida, errores y sesión vencida comparten la misma presentación de avisos. Se conservaron los contratos, permisos, reintentos y cálculos de E1.

## Archivos

- [Existences.tsx](../../apps/web/src/features/inventory/Existences.tsx) concentra la presentación y las interacciones de Existencias.
- [style.css](../../apps/web/src/style.css) aplica estilos al ancho disponible y al foco dentro de esta vista y su encabezado.

Respaldo de ambos originales en `/home/nicolas/Documentos/RespaldosRetail/Existencias-E1-Visual-oz5gv176`. Se cotejaron contenido, tamaño y SHA-256 antes de editar y se volvió a validar la copia al finalizar. [Manifiesto](../../../Documentos/RespaldosRetail/Existencias-E1-Visual-oz5gv176/MANIFIESTO.txt) · [Verificación](../../../Documentos/RespaldosRetail/Existencias-E1-Visual-oz5gv176/evidencia/respaldo-verificado.txt).

## Pruebas

| Comprobación | Resultado |
|---|---|
| `make check` | Pasó. Lint, compilación y 11 pruebas frontend. [Log](../../../Documentos/RespaldosRetail/Existencias-E1-Visual-oz5gv176/evidencia/make-check.log). |
| `GIN_MODE=release make test-db` | Pasó en PostgreSQL 17 descartable. Incluye roles, aislamiento tenant/sucursal, inventario y regresiones comerciales. [Log](../../../Documentos/RespaldosRetail/Existencias-E1-Visual-oz5gv176/evidencia/test-db.log). |
| Chromium/CDP con API real | Pasaron diez recorridos con datos sintéticos. Se verificaron búsqueda, código con ceros iniciales, acciones con teclado, detalle, historial, filtros avanzados, URL inválida sin GET, reintentos y 401 real por sesión vencida. El 500 se inyectó deliberadamente para validar la presentación y la recuperación. [Resultados y métricas](../../../Documentos/RespaldosRetail/Existencias-E1-Visual-oz5gv176/evidencia/chromium-resultados.json). |
| Concurrencia | A → B → A mostró 7, 42 y 11 en las consultas correspondientes. Al liberar la respuesta de A con 7, se mantuvo el saldo vigente de 11. |
| Responsive | 16 combinaciones de estado y ancho, sin desbordamiento horizontal del documento ni controles de la vista fuera del ancho medido. Los controles principales medidos alcanzaron al menos 44 px de alto. Se revisaron visualmente las capturas e iteraron los cortes de columnas, la barra del detalle móvil y las tarjetas estrechas de 1440 px. |

## Capturas

| Estado | 390 px | 768 px | 1024 px | 1440 px |
|---|---|---|---|---|
| Listado | [Ver](../../../Documentos/RespaldosRetail/Existencias-E1-Visual-oz5gv176/evidencia/listado-390.png) | [Ver](../../../Documentos/RespaldosRetail/Existencias-E1-Visual-oz5gv176/evidencia/listado-768.png) | [Ver](../../../Documentos/RespaldosRetail/Existencias-E1-Visual-oz5gv176/evidencia/listado-1024.png) | [Ver](../../../Documentos/RespaldosRetail/Existencias-E1-Visual-oz5gv176/evidencia/listado-1440.png) |
| Detalle | [Ver](../../../Documentos/RespaldosRetail/Existencias-E1-Visual-oz5gv176/evidencia/detalle-390.png) | [Ver](../../../Documentos/RespaldosRetail/Existencias-E1-Visual-oz5gv176/evidencia/detalle-768.png) | [Ver](../../../Documentos/RespaldosRetail/Existencias-E1-Visual-oz5gv176/evidencia/detalle-1024.png) | [Ver](../../../Documentos/RespaldosRetail/Existencias-E1-Visual-oz5gv176/evidencia/detalle-1440.png) |
| Escáner activo | [Ver](../../../Documentos/RespaldosRetail/Existencias-E1-Visual-oz5gv176/evidencia/escaner-390.png) | [Ver](../../../Documentos/RespaldosRetail/Existencias-E1-Visual-oz5gv176/evidencia/escaner-768.png) | [Ver](../../../Documentos/RespaldosRetail/Existencias-E1-Visual-oz5gv176/evidencia/escaner-1024.png) | [Ver](../../../Documentos/RespaldosRetail/Existencias-E1-Visual-oz5gv176/evidencia/escaner-1440.png) |
| Error de lectura | [Ver](../../../Documentos/RespaldosRetail/Existencias-E1-Visual-oz5gv176/evidencia/error-lectura-390.png) | [Ver](../../../Documentos/RespaldosRetail/Existencias-E1-Visual-oz5gv176/evidencia/error-lectura-768.png) | [Ver](../../../Documentos/RespaldosRetail/Existencias-E1-Visual-oz5gv176/evidencia/error-lectura-1024.png) | [Ver](../../../Documentos/RespaldosRetail/Existencias-E1-Visual-oz5gv176/evidencia/error-lectura-1440.png) |

También se capturaron [filas móviles](../../../Documentos/RespaldosRetail/Existencias-E1-Visual-oz5gv176/evidencia/existencias-filas-390.png), [acciones con foco](../../../Documentos/RespaldosRetail/Existencias-E1-Visual-oz5gv176/evidencia/acciones-390.png), [filtros avanzados](../../../Documentos/RespaldosRetail/Existencias-E1-Visual-oz5gv176/evidencia/filtros-avanzados-390.png), [ubicaciones sin saldo desplegadas](../../../Documentos/RespaldosRetail/Existencias-E1-Visual-oz5gv176/evidencia/detalle-ubicaciones-desplegadas-390.png), [URL inválida](../../../Documentos/RespaldosRetail/Existencias-E1-Visual-oz5gv176/evidencia/existencias-url-invalida-768.png), [página agotada](../../../Documentos/RespaldosRetail/Existencias-E1-Visual-oz5gv176/evidencia/pagina-agotada-768.png), [sin coincidencias](../../../Documentos/RespaldosRetail/Existencias-E1-Visual-oz5gv176/evidencia/sin-coincidencias-390.png), [carga](../../../Documentos/RespaldosRetail/Existencias-E1-Visual-oz5gv176/evidencia/cargando-390.png) y [sesión vencida](../../../Documentos/RespaldosRetail/Existencias-E1-Visual-oz5gv176/evidencia/sesion-vencida-390.png).

## Pendientes

La revisión visual y de teclado en Chromium quedó completa. El escáner se comprobó mediante entrada de teclado. Quedan sin ejecutar las pruebas con lector de códigos físico, lector de pantalla y dispositivo móvil físico.
