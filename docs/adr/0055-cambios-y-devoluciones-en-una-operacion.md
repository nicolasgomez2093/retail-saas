# ADR 0055 — Cambios y devoluciones en una sola operación

## Contexto

El modelo ya tenía devoluciones inmutables con destino `sellable` (vuelve a la venta) o `quarantine` (ubicación no vendible), reintegros con los medios del pago original (con movimiento de caja solo en efectivo), descuentos sobre el saldo del fiado y cambios con salida de stock y cobro de la diferencia. Todo eso se registraba en **dos pasos**: primero la recepción de la mercadería y después, en otra operación con otra clave, su resolución económica. Entre ambos podía quedar mercadería reingresada sin reintegro ni cambio. La búsqueda de ventas solo admitía el número.

## Decisión

- **`POST /sales/{id}/return-operations` registra todo en una transacción:** lo que vuelve, el reemplazo, el cobro, el descuento del fiado y el reintegro. No se duplicó lógica: las funciones existentes se separaron en núcleos que reciben la transacción (`postReturn`, `postExchange`, `postRefundForReturn` y `postDebtCreditForReturn`). Los endpoints anteriores siguen funcionando sobre esos mismos núcleos.
- **Todo el importe queda resuelto en la operación.** Si no, responde `409 return_operation_unsettled`.
  - Con un reemplazo más caro se cobra solo la diferencia.
  - Si queda a favor del cliente, primero se descuenta del saldo del fiado y después se reintegra con los medios del pago original.
  - Sin diferencia no hay cobro ni reintegro.
- **Precios:** lo devuelto se valúa con `sale_items.unit_price`, el precio ya neto de descuento que se cobró. El reemplazo se cotiza al precio vigente y debe coincidir con el que vio la persona (`expectedUnitPrice`).
- **Vista previa** (`POST …/return-operations/preview`): calcula con las mismas reglas, sin escribir nada. La interfaz muestra esos importes y la confirmación los vuelve a validar.
- **Idempotencia y concurrencia:**
  - La clave pública identifica la devolución.
  - Las claves derivadas del cambio, el reintegro y el descuento llevan `:`, que no está permitido en las claves públicas, así que no pueden chocar con ellas.
  - El bloqueo de la venta serializa dos operaciones sobre las mismas unidades: una se confirma y la otra recibe `return_quantity_exceeded`.
  - Un reintento con la misma clave devuelve la operación guardada sin mover stock ni dinero.
- **Mercadería dañada:** va a una ubicación no vendible del comercio, como ya permitía el modelo, y no a un contador aparte. Sin una ubicación no vendible, la interfaz no ofrece esa opción.
- **Historial de ventas:** acepta `q` y un rango de fechas.
  - `q` busca por cliente, nombre de producto, y por SKU o código de barras exactos.
  - El cliente solo existe en ventas fiadas o de reservas: las ventas al contado no guardan cliente y no se inventó esa relación.
  - Cada fila informa unidades, productos, cantidad de operaciones y si fue fiado.
- **Interfaz:** «Cambio o devolución» reutiliza el sistema visual ya extendido del HTML para Compras y el buscador de variantes con escaneo. El comprobante es no fiscal y usa la misma impresión que el ticket.

## Consecuencias

- **Sin saldo a favor:** no existe un saldo a favor del cliente independiente del fiado. Lo que no cubre el saldo pendiente se reintegra.
- **Pagos con tarjeta:** el sistema no revierte el pago en el procesador. El reintegro se registra como manual, por el medio del pago original, y no mueve el efectivo de la caja.
- **Revisión posterior:** una unidad «para revisar» queda fuera del stock vendible. Su destino posterior (reparación o vuelta a la venta) se resuelve con las herramientas de inventario existentes, que no forman parte de esta entrega.
