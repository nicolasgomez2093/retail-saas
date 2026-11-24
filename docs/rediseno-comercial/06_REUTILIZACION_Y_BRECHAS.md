# Capacidades verificadas y brechas

Rutas relativas al grupo API. Evidencia en apps/api/internal/platform/httpapi salvo indicación.

| Operación | Soporte y endpoint verificado | Clasificación y cambio |
| --- | --- | --- |
| Crear/editar producto | POST /products, PATCH /products/:id, GET /products/:id; catalog.go | ADAPTAR composición, conservar revisión y validaciones |
| Variantes | POST /products/:id/variants, PATCH /products/:id/variants/:variantId | REUTILIZAR dominio, REDISEÑAR integración de formularios |
| Matriz | POST /products/:id/variants/preview y /batch; catalog_batches.go | REUTILIZAR límites e idempotencia, mantener errores por fila |
| Talles/colores | catalog_attributes.go registra GET/POST/PATCH por tipo y GET /catalog/pending-attributes | REUBICAR administración avanzada; adaptar selección contextual |
| SKU | GET /catalog/sku-suggestion; catalog_identifiers.go | REUTILIZAR sugerencia sin reserva |
| Código exacto | GET /barcodes/:code/lookup; catalog.go | REUTILIZAR ceros y resolución exacta |
| Imágenes | catalog_photos.go registra POST, GET y PUT /:imageId/primary sobre su path | ADAPTAR galería existente, conservar permisos y errores |
| Precio | GET /stores/:id/variants/:variantId/price; pricing.go | REUTILIZAR amount nullable, currency, reason y listId |
| Guardar precio | PUT /price-lists/:id/items/:variantId con amount; pricing.go | ADAPTAR UI y confirmar alcance compartido |
| Lista de sede | PUT /stores/:id/price-list con listId | REUBICAR en configuración contextual autorizada |
| Existencias | GET /inventory/existences y /:variantId; inventory_existences.go | REUTILIZAR stock calculado en servidor |
| Entrada justificada | POST /inventory/adjustments y GET /inventory/adjustments/result; inventory.go | REUTILIZAR revisión e idempotencia; integrar en ficha |
| Conteos | count_workflow.go y httpapi/counts.go | REUTILIZAR borrador y cierre atómico |
| Venta y resultado | POST /sales, GET /sales/:id y /ticket; sales.go | REDISEÑAR interacción conservando transacción y snapshots |
| Caja | GET/POST /cash-sessions, GET /:id/movements, POST /:id/close; cash.go | ADAPTAR resumen personal y arqueo |
| Compras | GET/POST /purchase-orders, POST /:id/receipts; purchases.go | ADAPTAR reposición contextual, conservar recepción parcial |
| Clientes | GET/POST /customers; customers.go | REUTILIZAR alta y búsqueda; no prometer edición inexistente |
| Fiados | GET /receivables, POST /:id/payments; sales.go | REUBICAR en contexto cliente y caja |
| Informes | GET /reports/sales, /reports/inventory y exportaciones | ADAPTAR presentación a datos existentes |
| CSV | GET /catalog/csv/template y /export, POST /import | REUBICAR Herramientas; conservar altas y resultado incierto |

Brechas propuestas
- AMPLIAR BACKEND con lectura agregada paginada de preparación comercial por variante/sucursal si la composición existente genera N+1. Definir campos, autorización y frescura antes de programar. No existe evidencia en lo leído de un contrato único que resuelva todo.
- Evaluar precondición de precio antes de edición concurrente integrada. SetItem actual recibe amount y no expectedRevision en httpapi/pricing.go. No presentar el futuro formulario como protegido por revisión de producto.
- Una carga comercial completa atraviesa producto, variantes, precios y stock en transacciones distintas. Mostrar avance real recuperable; cualquier comando compuesto nuevo requiere contrato e idempotencia propios.
- RETIRAR DE LA EXPERIENCIA PRINCIPAL parámetros de identificadores, auditoría y migración de atributos. Mantener acceso administrativo y código.
- No se propone migración de datos para cambiar el shell. Nuevos campos de preparación sólo si no pueden derivarse. Ningún snapshot se reescribe. Las identidades UUID y las autorizaciones siguen vigentes.

Los roles OWNER, STORE_MANAGER, CASHIER e INVENTORY_OPERATOR son perfiles existentes en PROJECT_SPEC.md. El diseño debe resolver permisos efectivos por operación; no deducir acceso sólo del nombre del rol.
