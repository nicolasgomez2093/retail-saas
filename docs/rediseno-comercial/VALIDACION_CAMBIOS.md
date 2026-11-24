# Cambios, devoluciones y reintegros — validación

Implementado y validado el 23/11/2026. **Pendiente de revisión humana.** No se tocaron Inventario físico, Transferencias, Promociones ni Reportes.

Evidencia: `/home/nicolas/Documentos/RespaldosRetail/Retail-Cambios-20261123-200217/evidencia`. Contiene capturas, 30 comparaciones lado a lado, el PDF del ticket de venta, el PDF del comprobante de devolución, los logs y los scripts reproducibles en `scripts/`.

## Qué existía y se reutilizó

- Devoluciones inmutables con destino vendible o en cuarentena (ubicación no vendible).
- Reintegros por los medios del pago original: solo el efectivo mueve la caja.
- Descuento del saldo del fiado.
- Cambios con salida de stock y cobro de la diferencia.
- Precio histórico neto de descuento (`sale_items.unit_price`).
- Ticket no fiscal y su impresión.
- Búsqueda de variantes con escaneo (de Compras) y alta rápida con código existente.

## Qué se creó o cambió

- **`POST /sales/{id}/return-operations` y `/preview`:** una sola transacción para lo que vuelve, el reemplazo, el cobro, el descuento del fiado y el reintegro. Usa los núcleos existentes, separados para recibir la transacción. Si el importe no queda resuelto responde `409 return_operation_unsettled`. Detalle en ADR 0055.
- **`GET /sales`:** acepta `q` (cliente, producto, SKU o código de barras) y `from_date`/`to_date`. Cada fila informa cliente, unidades, productos, operaciones y si fue fiado.
- **Interfaz:**
  - «Ventas, cambios y devoluciones» (historial con búsqueda y filas del HTML).
  - «Cambio o devolución» en cuatro secciones progresivas: venta original, qué devuelve (con escaneo y estado), qué se lleva y diferencia. Al costado, el resumen y la confirmación.
  - Comprobante registrado e imprimible.
  - Modal con el historial de la venta.
- **Sin migraciones.** El modelo ya tenía todas las tablas necesarias.

## Pruebas ejecutadas

| Comando o recorrido | Resultado |
|---|---|
| `make check` | Código 0: gofmt, vet, ESLint, 28 pruebas Node, typecheck y build. |
| `make test-db` | Código 0. Subtest nuevo «operación única de cambio o devolución»: vista previa al precio histórico, rechazo sin resolver o con cobro indebido sin dejar devolución ni mover stock, permisos (403 y otro comercio 404), reintento con la misma clave, conflicto de clave, un solo movimiento de caja, devolución duplicada 409 y filtros de búsqueda. |
| `api-escenarios.mjs` | Los 10 escenarios contra la API. |
| `e2e-cambios.mjs` (React, API y Chrome reales) | Ver detalle abajo. |
| `responsive.mjs` | 6 pantallas a 390, 768, 1024, 1440 y 720 px (1440 al 200 %): sin desborde ni controles cortados. |
| `estados.mjs` | Botones primario y secundario idénticos al HTML en reposo, hover, active y focus-visible. |

**Recorrido por la interfaz (`e2e-cambios.mjs`):**
- Alta con código del fabricante escaneado sin Enter.
- Venta por escaneo de dos códigos con cobro.
- Ticket no fiscal con productos y total, impresión explícita y PDF de una página.
- Escenarios:
  1. Cambio de talle 40 → 42 escaneando la etiqueta devuelta: diferencia 0, stock +1/−1 y sin movimiento de caja. Además, la segunda lectura avisa que no hay más unidades y un código ajeno a la venta se rechaza.
  2. Cambio por un producto más caro: se cobran 7.000 (vuelto 3.000) y la caja sube 7.000.
  3. Cambio por uno más barato: se reintegran 7.000 en efectivo. Con una venta con 10 % de descuento se reintegran los 40.500 pagados, no los 45.000 de lista.
  4. Devolución parcial de 1 de 3: el reintegro va por transferencia, la caja no cambia y quedan 2 disponibles.
  5. Producto defectuoso: +1 en «Revisión y fallados» y el stock vendible no cambia.
  6. Fiado impago: la deuda baja de 50.000 a 0 sin salida de caja.
  7. Fiado de 50.000 con 20.000 pagados: 30.000 cancelan la deuda y se reintegran 10.000; el pago original se conserva.
  8. Devolución duplicada: la pantalla muestra «Ya devuelto» y la API responde 409.
  9. Concurrencia desde dos pantallas: una confirma y la otra recibe el rechazo; un solo reintegro.
  10. Persistencia tras recargar: la venta original intacta, la operación vinculada y los movimientos `sale_return` y `sale_exchange`.
- Corte de red tras confirmar: se reintenta con la misma clave sin duplicar.
- Doble clic: un solo envío.
- Comprobante impreso.

## Comparación visual

`evidencia/comparaciones/`: HTML aprobado a la izquierda y React a la derecha. El HTML no tiene pantallas de cambios, así que se compararon contra sus equivalentes:

| React | HTML |
|---|---|
| Historial | Historial de ventas |
| Operación | Vender |
| Confirmación | Modal de recepción |
| Comprobante | «Venta registrada» |
| Historial de la venta | Modal de historial |

Se reutilizaron `.head`, `.card`, `.panelhead`, `.filters`/`.search`, `.variantrow`, `.pill`, `.btn`, `.chip`, `.note`, `.sidebox`, la tabla, `.modal` y el ticket.

Corregido durante el recorrido:
- Aviso de bloqueo de sucursal mostrado sin una operación en curso.
- Selector de estado cortado.
- Título duplicado.
- «Pagado» del fiado que mostraba 0.

## Limitaciones

- **Sin saldo a favor del cliente.** No existe en el modelo y no se inventó: lo que no cubre el fiado se reintegra.
- **Tarjetas:** el reintegro se registra de forma manual. El sistema no revierte el pago en el procesador.
- **Mercadería para revisar:** queda fuera del stock vendible. La decisión posterior se toma con las herramientas de inventario existentes, sin una pantalla propia.
- **Búsqueda por cliente:** solo funciona en ventas fiadas o de reservas.
- **Comprobante:** no muestra el nombre del usuario, porque la venta guarda solo su identificador. Es no fiscal y no hay integración con ARCA.
- **Pruebas físicas:** no se probó con lector USB ni impresora térmica físicos. El escaneo se simuló con entrada de teclado (con y sin Enter) y la impresión con el PDF de Chrome.
- **Componentes anteriores:** `SaleReturns`, `ReturnRefundForm`, `ReturnDebtCreditForm` y `ReturnExchangeForm` se eliminaron el 23/11/2026, al preparar la publicación del repositorio.
- **Docker local:** no se reconstruyó. No hay migraciones nuevas en esta entrega; para ver Compras y Alta rápida ahí siguen haciendo falta `make migrate` y `make up`.
