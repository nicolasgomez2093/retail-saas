# Compras, proveedores, recepción y reposición — validación

Implementado y validado el 23/11/2026. **Pendiente de revisión humana.** No se inició otra etapa (no se tocaron Devoluciones, Cambios, Reportes ni Transferencias).

Evidencia: `/home/nicolas/Documentos/RespaldosRetail/Retail-Compras-20261123-191357/evidencia`. Contiene capturas, 35 comparaciones lado a lado, el PDF de etiquetas, los JSON de cada recorrido, los logs y los scripts reproducibles en `scripts/`.

## Qué existía y se reutilizó

- Proveedores con nombre, correo, teléfono y estado.
- Órdenes de compra idempotentes con costo opcional y nombre histórico del proveedor.
- Recepciones inmutables con unidades aceptadas y rechazadas.
- Movimientos `purchase_receipt` auditados (`postMovement`) y costo promedio ponderado.
- Filtros, resumen y CSV de órdenes y recepciones.
- Permisos `suppliers:*` y `purchases:*` por sucursal asignada.
- De entregas anteriores: búsqueda de catálogo y por código de barras, alta rápida (ADR 0053) y diálogo de etiquetas.

## Qué se creó o cambió

- **Migración 046 y ADR 0054:**
  - Proveedores: CUIT/identificación fiscal (única) y persona de contacto.
  - Pedidos: número `OC-0001` y referencia o comprobante; estado `CANCELLED` con fecha, usuario y motivo.
  - Recepciones: motivo de las unidades no aceptadas (`damaged`, `wrong_item`, `excess`, `other`).
- **API:**
  - `GET /purchase-orders/{id}` y `POST /purchase-orders/{id}/cancel`.
  - Cada línea del pedido informa `productId` y `rejected`; cada línea de recepción, `size`, `color`, `rejectionReason` y `stockBefore`/`stockAfter` del movimiento.
  - El resumen suma `pendingOrders`, `receivedOrders` y `currency`.
  - Un proveedor duplicado responde `409 supplier_duplicate`; operar sobre un pedido recibido o cancelado responde `409 purchase_order_closed`.
- **Política de excedente:** las unidades aceptadas nunca superan lo pendiente. Lo de más se registra como no aceptado con motivo «Excedente».
- **Interfaz:**
  - Compras, Proveedores, Nuevo pedido, Recibir mercadería, Resultado y el modal «Pedido OC-…».
  - Escaneo con lecturas repetidas.
  - Producto nuevo mediante el alta rápida existente, con vuelta al pedido.
  - Etiquetas sugeridas por recepción y reimpresión desde el historial.
- **Navegación:** Proveedores pasa al grupo Compras, que ahora se resalta en el menú lateral. Se quitaron las pestañas heredadas «Todas / Pendientes»; el filtro de estado las reemplaza.

## Pruebas ejecutadas

| Comando o recorrido | Resultado |
|---|---|
| `make check` | Código 0: gofmt, vet, ESLint, 28 pruebas Node (3 nuevas de importes exactos, costos y pendientes), typecheck y build. |
| `make test-db` (PostgreSQL 17 descartable) | Código 0. Las pruebas de compras existentes quedaron adaptadas a la nueva regla de excedente y de homónimos, más el subtest nuevo «compras con referencia, diferencias y cancelación». |
| `e2e-compras.mjs` (API, Vite y Chrome reales) | Ver «Escenario de aceptación» más abajo. |
| `casos-compras.mjs` | Ver «Casos adicionales» más abajo. |
| `responsive.mjs` | 7 pantallas a 390, 768, 1024, 1440 y 720 px (1440 al 200 %): sin desborde horizontal ni controles cortados. |
| `estados.mjs` | Botón primario y secundario en reposo, hover, active y focus-visible, idénticos al HTML en las 8 propiedades medidas. |

**Escenario de aceptación (`e2e-compras.mjs`):**
- Proveedor creado una sola vez pese al doble clic.
- Pedido de 35 unidades con total exacto ARS 620.006. Un solo envío, y el stock no cambia al registrarlo.
- Recepción de 30 por escaneo y teclado: Remera M pasa de 7 a 17 y el pedido queda en «Recepción parcial» con 5 pendientes.
- 30 etiquetas sugeridas (10, 5, 12 y 3) con los códigos existentes, impresión solo por acción explícita, PDF de 30 páginas y stock sin cambios.
- Segunda recepción de 5: el pedido queda «Recibida».
- Tras recargar:
  - 2 recepciones con 35 unidades aceptadas.
  - 6 movimientos de inventario sin duplicados.
  - Los SKU y los códigos no cambiaron y no hay variantes duplicadas.

**Casos adicionales (`casos-compras.mjs`):**
- Diferencias: 1 unidad dañada y 2 de excedente; solo lo aceptado entra al stock.
- Cancelación con motivo; una recepción posterior responde 409.
- Producto nuevo creado desde el pedido sin perder el borrador.
- Variante inactiva y código inexistente: no se agregan.
- Cantidades y costos inválidos: no se envían.
- Corte de red después de confirmar: se reintenta con la misma clave y suma una sola vez.
- Recepciones concurrentes: una responde 201 y la otra 409.

## Comparación visual

`evidencia/comparaciones/<pantalla>-<ancho>.png`: HTML aprobado a la izquierda y React a la derecha.

Pares comparados:
- Lista ↔ Compras.
- Detalle ↔ modal «Pedido».
- Recepción ↔ modal «Recepcionar mercadería».
- Nuevo pedido ↔ modal «Crear pedido».
- Proveedores ↔ Clientes, el listado de contactos equivalente.

Diferencias detectadas y corregidas durante el recorrido:
- Panel heredado que envolvía la página.
- Subtítulo en negrita.
- Reglas globales de inputs y botones que ganaban por especificidad.
- Clase `actions` que chocaba con una global.
- Formulario anidado que recargaba la página al buscar.
- Columna de variante angosta.
- Costo sin formato.
- Diferencias aplastadas a 390 px.
- Filtros truncados en móvil.
- Pie de totales con celdas vacías.
- Enlace «Cambiar» partido.

Diferencias intencionales:
- El HTML carga un pedido de un solo renglón y recibe de a un renglón por modal. Para cargar varios renglones con teclado y escáner, Nuevo pedido y Recepción extienden `.card`, `.sidebox` y la tabla a pantalla completa.
- El detalle conserva el modal del HTML.

## Limitaciones

- No se probó con impresora térmica ni con lector USB físico. El escaneo se validó con entrada de teclado y la impresión con el PDF de Chrome.
- No hay stock en cuarentena: las unidades no aceptadas quedan registradas con su motivo y no ingresan al stock (ADR 0054).
- No se puede aceptar mercadería no pedida dentro del mismo pedido: hace falta otro pedido.
- No hay borrador de pedido guardado en el servidor.
- La búsqueda por código solo encuentra variantes activas, igual que en Vender. Una variante inactiva aparece como «No encontramos», sin decir que está inactiva.
- Los permisos de rol en la interfaz (encargado o cajero) se validaron con las pruebas de integración de la API, no con el navegador. La base de demostración tiene un solo usuario.
- El entorno Docker local no se reconstruyó. Para usar esta entrega ahí hay que ejecutar `make migrate` y `make up`, que aplican las migraciones 045 y 046.
