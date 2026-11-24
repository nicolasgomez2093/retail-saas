# ADR 0057 — Filtros de Reportes, cobros, compras y Caja en el menú

## Contexto

Reportes respondía una sola pregunta (qué se vendió) con un filtro de período. Los comercios preguntan cosas distintas al cerrar el día: cuánto se vendió hoy, cuánto entró en efectivo, qué mercadería llegó y quién debe. Una venta, un cobro y un fiado son hechos distintos, y mezclarlos da cifras que no coinciden con la caja. Además, «Gestión» en el menú obligaba a adivinar dónde abrir la caja, y las tablas de Existencias, Clientes y Fiados no seguían el diseño aprobado.

## Decisión

- **Cuatro vistas en Reportes**, con los mismos filtros arriba:
  - **Ventas:** lo vendido, con el fiado incluido.
  - **Cobros:** el dinero que entró y salió.
  - **Compras:** la mercadería recibida.
  - **Fiados:** lo que deben los clientes.
- **Filtros de período:** Hoy, Ayer, 7 días, 30 días, Este mes y Personalizado. El encabezado resume la selección, por ejemplo «Ventas · Hoy · Efectivo».
- **Medio de pago en Ventas** (`method` en `GET /reports/sales`, su CSV y `GET /sales`): conserva las ventas cobradas, al menos en parte, con ese medio, incluida la seña. `on_credit` conserva las ventas fiadas. Las devoluciones y cambios siguen a su venta. Una venta mixta suma completa, y la pantalla lo aclara y deriva a Cobros.
- **Cobros** (`GET /reports/collections`):
  - Une pagos de ventas, señas, abonos de fiados, cobros de diferencias de cambios y reintegros. Los reintegros llevan signo negativo.
  - Cada movimiento cuenta el día en que ocurrió y con su medio real. Un fiado no suma hasta que se abona.
  - Los pagos de venta siguen la sesión de caja de la venta, y los demás movimientos, la suya. Quien no supervisa ve solo sus sesiones.
- **Compras** (`GET /reports/purchases`):
  - Muestra las recepciones del período, valuadas al costo cargado en cada recepción. Las unidades sin costo no suman importe y se informan aparte.
  - Las órdenes emitidas cuentan por su fecha. Las pendientes son las de hoy.
  - Exige `purchases:read`, y la pestaña se oculta cuando el acceso a compras está denegado.
- **Fiados:**
  - Agrega el estado `partial` («Con abonos»): con saldo y con algún abono o compensación.
  - El saldo por cobrar es el actual. El fiado otorgado y lo abonado responden al período.
- **Gráficos sin dependencias nuevas:**
  - Columnas por día de una sola serie, en el verde de la marca, con tooltip por puntero y por teclado.
  - Barras horizontales para los desgloses. El valor siempre va en texto y la barra solo indica la proporción.
  - Las tablas siguen disponibles como vista de datos.
- **Menú:**
  - En escritorio, «Caja» reemplaza a «Gestión».
  - En el celular, «Más» abre el centro de accesos, ahora «Más opciones».
  - Las páginas de administración quedan bajo «Configuración», desde donde también se abren las listas de precios.
- **Tablas:** Existencias, Clientes y Fiados usan la tabla del diseño aprobado, con estado como etiqueta (Disponible, Stock bajo, Agotado, Pendiente, Con abonos, Saldado) y acciones como botones. Se quitó la navegación duplicada «Directorio / Cuentas corrientes».
- **Sin migraciones.**

## Consecuencias

- El filtro por medio en Ventas y el de Cobros responden preguntas distintas a propósito. Para cuadrar la caja se usa Cobros → Efectivo, que no incluye el fondo inicial ni los movimientos manuales.
- Las alturas de los gráficos usan `Number` solo para dibujar; los importes mostrados conservan el decimal exacto de la API.
