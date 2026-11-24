# ADR 0006 — Precio faltante por sucursal

Cuando una sucursal tiene una lista propia, esa lista determina el precio de cada variante en la sucursal. Si la lista no tiene un precio aplicable para la variante, el resultado es **Sin precio**, aunque la lista general sí tenga un importe para esa variante.

La API debe representar la ausencia de precio explícitamente. La búsqueda del catálogo puede devolver la variante y su código de barras. La confirmación de una venta debe exigir que cada variante tenga un precio aplicable en la sucursal.

## Casos de aceptación

- Lista propia con precio aplicable: la variante tiene el importe de esa lista.
- Lista propia sin precio aplicable y lista general con precio: la variante está sin precio en esa sucursal.
- Búsqueda por código de barras de una variante sin precio: la variante sigue siendo identificable.
- Confirmación de venta con una variante sin precio: la API rechaza la operación.
