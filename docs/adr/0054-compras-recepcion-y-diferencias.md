# ADR 0054 — Compras: proveedores, pedidos, recepción con diferencias y cancelación

## Contexto

Ya existían proveedores, órdenes de compra (`OPEN`, `PARTIALLY_RECEIVED`, `RECEIVED`), recepciones idempotentes e inmutables con unidades aceptadas y rechazadas, movimientos `purchase_receipt` y costo promedio ponderado. Para que un comercio repusiera mercadería sin ayuda faltaban:

- Un número legible de pedido y la referencia del comprobante.
- Una forma de cerrar lo que el proveedor no va a entregar.
- El motivo de las unidades no aceptadas, y una política para el excedente: antes, aceptado + rechazado no podía superar lo pendiente.
- Datos fiscales y de contacto del proveedor, y una regla contra altas dobles.
- Una interfaz alineada con el HTML aprobado, con escaneo y etiquetas.

## Decisión

- **Migración 046 (aditiva).**
  - Proveedores: `suppliers.tax_id`, único por comercio cuando no está vacío, y `contact_name`.
  - Pedidos: `purchase_orders.number`, correlativo por comercio (`OC-0001`, asignado bajo el bloqueo del comercio; las órdenes previas se numeran por fecha), y `reference`.
  - Cancelación: estado `CANCELLED` con `cancelled_at`, `cancelled_by` y `cancel_reason`.
  - Recepciones: `purchase_receipt_items.rejection_reason` (`damaged`, `wrong_item`, `excess`, `other`). Las recepciones confirmadas no se tocan: las anteriores quedan con motivo nulo.
- **Sin estado borrador en el servidor.** El pedido se arma en pantalla y se registra de una vez. Un borrador persistente agregaría un estado sin contrato que lo respalde.
- **Registrar no mueve stock.** Solo `POST /purchase-orders/{id}/receipts` genera ingresos: un movimiento `purchase_receipt` por variante, con la recepción, el usuario, la ubicación y los saldos anterior y posterior. El historial de la recepción muestra esos saldos (`stockBefore`, `stockAfter`).
- **Diferencias sin cuarentena.** El modelo no tiene stock en cuarentena y no se simula.
  - Las unidades no aceptadas quedan registradas en la recepción con su motivo, no ingresan al stock ni al costo y no consumen lo pendiente.
  - Una unidad dañada sigue pendiente, a la espera de la reposición, hasta que llegue o se cancele el pedido.
  - El excedente se registra como no aceptado con motivo `excess`: las aceptadas nunca superan lo pendiente.
  - Un producto equivocado se registra como `wrong_item` en el renglón que debía cubrir.
  - Incorporar mercadería no pedida exige otro pedido, para que siga siendo trazable.
- **Cancelación** (`POST /purchase-orders/{id}/cancel`):
  - Lo ya recibido queda en el stock y en el historial; lo pendiente deja de contarse y el pedido no admite más recepciones (`409 purchase_order_closed`).
  - Cancelar dos veces devuelve la cancelación original.
  - Un pedido recibido completo no se cancela.
- **Duplicados de proveedor.**
  - Un nombre igual a otro proveedor (sin distinguir mayúsculas ni espacios) responde `409 supplier_duplicate` con el proveedor existente. Eso también frena un doble envío.
  - El homónimo sigue siendo posible con `allowDuplicateName`, la regla que ya probaba el historial por proveedor.
  - La identificación fiscal no se repite nunca.
- **Concurrencia e idempotencia.** Siguen como estaban: bloqueo del comercio, clave de idempotencia con huella del cuerpo y verificación de lo pendiente dentro de la transacción. Los campos nuevos se serializan con `omitempty`, así las claves de solicitudes anteriores conservan su huella.
- **Interfaz.**
  - Compras reproduce `.head`, `.metrics`, `.card`, `.variantrow`, `.pill` y el modal «Pedido» del HTML.
  - El pedido nuevo y la recepción extienden esos patrones a pantalla completa, porque un modal de 530 px no admite carga de varios renglones.
  - Un producto inexistente se crea con el alta rápida existente (ADR 0053) y vuelve al pedido sin perder el borrador.
  - Las etiquetas reutilizan el diálogo existente con las unidades aceptadas como sugerencia y los códigos ya asignados.

## Consecuencias

- Los informes que lean `status` deben contemplar `CANCELLED`. El CSV exporta el valor tal cual.
- Una unidad dañada que el proveedor no repone queda pendiente hasta que alguien cancela el pedido. Es deliberado: no se da por perdida sin una decisión explícita.
- El encargado (`purchases:manage`) puede recibir sin el permiso de ajuste de inventario, como antes: la recepción es una operación de compras con su propio movimiento auditado.
