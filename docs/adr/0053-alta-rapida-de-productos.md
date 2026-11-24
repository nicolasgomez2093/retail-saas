# ADR 0053 — Alta rápida de productos en una sola transacción

## Contexto

Cargar mercadería nueva exigía crear el producto, abrir la matriz de variantes, cargar precios por variante y registrar ajustes de stock en pantallas separadas. Una falla intermedia dejaba datos parciales. Las variantes solo admitían talle y color, lo que no alcanza para un jean (cintura, largo y color) ni para una presentación de perfumería. Además, el código interno vigente (`RI-<12 hex>-<10 dígitos>`) necesita 72,7 mm en Code 128, así que no entraba en la etiqueta predeterminada de 60 × 40 mm.

## Decisión

- `POST /api/v1/quick-products` crea el producto, sus variantes, los SKU, los códigos de barras, los precios de la lista asignada a la sucursal y el stock inicial **en una sola transacción de PostgreSQL**. Todo se confirma o nada se registra. La clave `Idempotency-Key` guarda el resultado confirmado en `catalog_quick_creations`, una tabla inmutable. Un reintento con el mismo cuerpo devuelve el resultado original sin escribir otra vez; con otro cuerpo devuelve 409.
- Las validaciones de fila se informan todas juntas (`quick_product_invalid` con `rows`) antes de escribir nada.
- Permisos: siempre `catalog:manage` y la sucursal asignada al usuario. Además, `pricing:manage` si hay precios e `inventory:adjust` si hay cantidades. No se omiten las reglas existentes para simplificar.
- El stock inicial usa el mismo `postMovement` auditado que los ajustes: tipo `adjustment`, motivo «Stock inicial · alta de producto», con actor, ubicación, saldos previo y posterior, y bloqueo del costo. No se interpreta el costo faltante como cero.
- Precios: `null` significa sin precio y `0` es un precio cero explícito. Se auditan en `price_admin_events` como en la edición individual.
- **Atributos adicionales**: la migración 045 agrega `product_variants.options jsonb` (lista ordenada `{name, value}`, hasta 4). Talle y color siguen en sus columnas y maestros. Los valores nuevos se agregan al maestro; un valor inactivo se rechaza sin reactivarlo. Las comprobaciones de combinación duplicada (lote, edición y maestro) incluyen `options` sin distinguir mayúsculas. Las variantes existentes quedan con `[]` y no cambian su identidad.
- La venta guarda los atributos adicionales junto al talle en la instantánea `sale_items.size`, para que el ticket identifique la variante exacta (por ejemplo, «W32 · L32»).
- **Código interno nuevo**: 16 dígitos (`2` + 5 dígitos derivados del comercio + 10 de secuencia). En Code 128 subconjunto C ocupa unos 36 mm con zonas de silencio y entra en 60 × 40 mm. Dieciséis dígitos no es una longitud GTIN (8/12/13/14), así que no se confunde con un EAN/UPC, y al no llevar FNC1 no es GS1-128. Los códigos ya emitidos conservan su formato (estabilidad).
- Límite de 50 variantes por alta, el mismo que el lote existente. La interfaz pide confirmación desde 25 variantes y explica el límite.

## Consecuencias

- La interfaz de alta rápida no necesita simular atomicidad ni recuperar estados parciales: ante un corte de red ofrece reintentar el mismo guardado con la misma clave.
- Los códigos internos emitidos antes de este cambio (`RI-…`) siguen sin entrar en 60 × 40 mm. Para esos casos se debe elegir el formato de 100 × 40 mm o registrar un código adicional.
- Los valores de `options` no se corrigen desde el editor individual de variantes; se muestran en ficha, POS, etiquetas y ticket.
