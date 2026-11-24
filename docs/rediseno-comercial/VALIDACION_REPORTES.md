# Reportes — validación

Implementado y validado el 23/11/2026. **Pendiente de revisión humana.** No se tocaron Inventario físico, Transferencias ni Promociones.

Evidencia: `/home/nicolas/Documentos/RespaldosRetail/Retail-Reportes-20261123-225534/evidencia`. Contiene capturas, 10 comparaciones lado a lado, el CSV descargado, los JSON de cada recorrido, los logs y los scripts reproducibles en `scripts/`.

## Qué existía y se reutilizó

- `GET /reports/sales`:
  - margen por línea con el costo guardado al vender;
  - resumen por moneda, ventas por día, medios de pago, categorías y variantes más vendidas;
  - exportación CSV.
- Historial de ventas con búsqueda y fechas (de la entrega de Cambios).
- El sistema visual `pu-` derivado del HTML: `.head`, `.metrics`, `.card`, `.panelbody`, `.statrow`, `.note`, `.filters`, `.search`, chips, tabla y paginador.

## Qué se creó o cambió

- **`activity` en el reporte:** ventas, unidades, importe vendido y ticket promedio; devoluciones y cambios con su cantidad, unidades e importe; y el neto. Cada operación cuenta en su propia fecha y se respetan la búsqueda, las fechas y el alcance del usuario. Detalle en ADR 0056.
- **`topProducts`:** productos por unidades que se llevaron los clientes, netas de devoluciones.
- **`payments` en `GET /sales`:** los medios cobrados de cada venta, incluida la seña.
- **Pantalla «Reportes»,** reconstruida sobre el HTML:
  - Encabezado con «↓ Exportar ventas CSV».
  - Período: Hoy, 7 días, 30 días (predeterminado), Este mes o Personalizado, con filtro por SKU o producto.
  - Indicadores:
    - **Facturación neta**, con el desglose «Vendido · devuelto · cambios».
    - **Ventas**, con las unidades vendidas, devueltas y entregadas en cambios.
    - **Ticket promedio.**
  - «Ventas registradas» (N.º, unidades, medios, cambios, importe y hora, con paginación) y «Productos más vendidos».
  - Medios de pago, margen bruto, ventas por día, ventas por categoría y detalle por línea con margen.
  - Las consultas relacionadas (valuación de inventario, órdenes de compra y fiados) quedan al pie.
  - La vista «?dia=» desde Inicio se conserva.
- **Menú:** «Informes de ventas» pasa a llamarse «Reportes», como en el HTML.
- **Sin migraciones.** Se eliminó `SalesCharts.tsx` porque ya no se usaba.

## Pruebas ejecutadas

| Comando o recorrido | Resultado |
|---|---|
| `make check` | Código 0: gofmt, vet, ESLint, 28 pruebas Node, typecheck y build. |
| `make test-db` | Código 0. El subtest «operación única de cambio o devolución» ahora verifica que dos devoluciones restan 2 × precio del neto sin alterar las ventas, el período vacío, que otro comercio recibe 404 y los medios de la venta en el historial. |
| `api-reportes.mjs` | Ver detalle abajo. |
| `e2e-reportes.mjs` (React, API y Chrome reales) | 8 pasos. Ver detalle abajo. |
| `responsive.mjs` | Reporte con datos y vacío a 390, 768, 1024, 1440 y 720 px (1440 al 200 %): sin desborde y sin controles ni importes cortados. |
| `estados.mjs` | «Aplicar» (primario) y «Exportar ventas CSV» (secundario) idénticos al HTML en reposo, hover, active y focus-visible. |

**Contra la API (`api-reportes.mjs`).** Se registraron cuatro ventas:
- jean de 45.000 cambiado por una campera de 52.000, con 7.000 cobrados;
- tres remeras, una devuelta con reintegro de 15.000;
- fiado de 50.000;
- jean 40 cambiado por un jean 42.

Resultados:

| Dato | Valor |
|---|---|
| Vendido | 185.000 |
| Devuelto | 105.000 |
| Entregado en cambios | 97.000 |
| **Neto** | **177.000**, que coincide con 185.000 + 7.000 cobrados − 15.000 reintegrados |
| Ticket promedio | 46.250 |
| Productos más vendidos | Remera 4 u. (5 − 1), Cinturón 2, Campera 1, Jean 1 (3 − 2) |

También se verificó:
- con el filtro «jean», 2 ventas y un neto de 45.000;
- un período sin operaciones;
- los medios por venta: efectivo, transferencia y, en el fiado, ninguno.

**Por la interfaz (`e2e-reportes.mjs`):**
1. 30 días: indicadores, desglose, productos y ventas registradas con sus medios.
2. Hoy: las mismas cifras.
3. Filtro «jean».
4. Fechas invertidas: aviso, sin consulta.
5. Período sin operaciones: estados vacíos.
6. CSV del período descargado (5 líneas).
7. Una venta nueva (+30.000) y una devolución registrada por la API (−15.000) aparecen al volver: neto de 192.000.
8. Vista de un día desde Inicio y «Elegir otro período».

Sin errores de JavaScript.

## Comparación visual

`evidencia/comparaciones/`: HTML aprobado a la izquierda y React a la derecha, «Reportes» con datos y vacío en los cinco anchos.

| Elemento | Coincide con el HTML |
|---|---|
| Encabezado y botón | Sí |
| Tres indicadores | Sí, con importes a 21 px |
| Columnas «Ventas registradas» y «Productos más vendidos» | Sí, 1,45 : 0,8 |
| Filas y nota | Sí |

Corregido durante la comparación:
- El filtro estaba limitado por el ancho máximo global de formularios.
- La tabla diaria se apilaba en media tarjeta.
- Los importes se cortaban a 390 px: se pasó a dos columnas, como el HTML bajo 380 px.
- Montos que empujaban fuera de la fila.
- Porcentaje de categoría cortado.
- Título de la barra superior distinto del HTML.

## Limitaciones

- **Margen bruto:** no descuenta el costo de lo devuelto. Cambiarlo requiere decidir cómo se valúa la mercadería que vuelve, sobre todo la que va a «Revisión y fallados».
- **Búsqueda en «Ventas registradas»:** usa la búsqueda del historial (nombre parcial, y SKU o código de barras exactos). Los indicadores aceptan además SKU parcial.
- **Fecha de las devoluciones:** cuentan en el período en que se registran, no en el de la venta original.
- **Gráfico de 7 días:** se reemplazó por la tabla «Ventas por día» del período elegido.
- **CSV:** es el detalle por línea con margen que ya existía. No trae una fila por venta como el ejemplo del HTML.
- **Docker local:** no se reconstruyó. Esta entrega no tiene migraciones.
