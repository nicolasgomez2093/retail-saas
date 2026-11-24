# Implementación por recorridos comerciales

Plan pendiente de aprobación del prototipo. Cada entrega debe poder demostrarse desde su primera integración con datos de un comercio local de prueba. La demo es una condición continua de aceptación.

## Primera entrega · Crear y preparar un producto

Recorrido demostrable con API real en entorno controlado
1. Abrir Productos y crear una remera con nombre y clasificación opcional.
2. Guardar los datos generales y volver a abrir la ficha incompleta.
3. Seleccionar S/M/L y Crudo/Negro, revisar seis combinaciones, excluir una y editar un SKU.
4. Confirmar el lote y verificar los UUID realmente creados.
5. Guardar precios por fila, incluyendo cero, y comprobar la resolución de la sucursal.
6. Registrar mercadería en ubicación vendible mediante ajustes con motivo y revisión.
7. Revisar cada variante, identificar datos pendientes o desconocidos y consultar en Vender.
8. Comprobar la búsqueda y la variante en el POS existente. La transformación completa del cobro pertenece a la siguiente entrega.

## Archivos y componentes de primera entrega

| Área | Trabajo propuesto |
| --- | --- |
| apps/web/src/app/App.tsx y Navigation.tsx | Aplicar shell aprobado, preservando rutas, permisos, historial y contexto |
| apps/web/src/features/catalog/Catalog.tsx | Componer catálogo y ficha integrada. Separar secciones de presentación conservando operaciones |
| Componentes de catálogo existentes | Reutilizar VariantMatrix.tsx, CatalogAttributes.tsx (AttributePicker), ProductPhotos.tsx, LabelDialog.tsx, CatalogCSV.tsx y ProductQuickView.tsx; conservar sus APIs y el control de conflictos |
| Nuevos componentes candidatos | ProductWorkspace, ProductPreparationSummary y adaptadores de paneles de precio/mercadería; nombres propuestos |
| Precios | Reutilizar VariantPrice.tsx y PricingAdmin.tsx; resolver alcance compartido y precondiciones antes de habilitar edición integrada |
| Inventario | Reutilizar InventoryVariantPicker y Adjustments.tsx con la lógica segura de ajustes E3; adaptar presentación contextual sin duplicar contabilidad |
| Estilos | Tokens y componentes aplicados primero al recorrido, con regresión de consumidores antes de extender globalmente |

La refactorización se limita a lo indispensable para la integración aprobada. Los nombres de componentes nuevos son candidatos, no símbolos existentes.

## Contratos que se reutilizan

| Operación | Ruta real en httpapi | Decisión |
| --- | --- | --- |
| Datos generales | POST /products, GET/PATCH /products/:id | Conservar campos y expectedRevision en edición |
| Variantes | POST /products/:id/variants, PATCH /products/:id/variants/:variantId | Alta individual alternativa y revisión independiente |
| Matriz | POST /products/:id/variants/preview y /batch | Máximo actual de 50, lote atómico e idempotencia existentes |
| Sugerencia SKU | GET /catalog/sku-suggestion | Sugerencia editable, sin reserva |
| Precio local | GET /stores/:id/variants/:variantId/price | amount nullable, currency, listId y reason |
| Configuración de precios | GET /price-lists y PUT /stores/:id/price-list | Alcance autorizado y asignación real |
| Precio por variante | GET/PUT /price-lists/:id/items/:variantId | Una confirmación por fila |
| Existencias | GET /inventory/existences y /:variantId | Fuente de disponibilidad y desglose |
| Ajuste y recuperación | POST /inventory/adjustments, GET /inventory/adjustments/result | Operación auditable con precondición e idempotencia |
| Escaneo | GET /barcodes/:code/lookup | Código exacto y ceros iniciales |

Fuentes de contraste puntual durante esta corrección
- apps/api/internal/modules/catalog/previews.go, Preview y listPreviews, ya agrega foto, rango de precio y stock de productos del listado. Aprovechar esta capacidad antes de proponer otro agregado.
- apps/api/internal/modules/catalog/batch.go, VariantBatchLimit = 50.
- apps/api/internal/platform/httpapi/catalog_batches.go, readVariantBatchJSON con límite 64 KiB y errores por fila.
- apps/api/internal/modules/pricing/pricing.go, Resolved y SetItem; httpapi/pricing.go recibe amount sin expectedRevision.
- apps/api/internal/modules/sales/sales.go, ItemInput.discountPercent y control CanDiscount.
- apps/api/internal/modules/sales/payments.go acepta cash, transfer, debit, credit y qr.

## Brechas concretas y decisiones

- Precio concurrente. La revisión de producto no protege el PUT de precio. Definir precondición de precio y respuesta de conflicto si se exige evitar sobrescritura silenciosa. Diseñar extensión compatible antes de implementar esa garantía.
- Alcance de listas. Detectar las sedes reales que comparten lista y mostrar impacto según autorización. El prototipo usa una lista compartida de ejemplo. No asumir que todos los comercios comparten su lista.
- Preparación. El agregado de producto existente reduce N+1, pero no sustituye el desglose por variante ni una garantía futura del POS. Componer detalles bajo demanda; ampliar lectura sólo si falta información o las mediciones lo requieren.
- Asociación de cliente a venta común. sales.go rechaza CustomerID con OnCredit falso. En el prototipo se puede elegir cliente al cobrar para evaluar la interacción, pero vincularlo a una venta común requiere adaptar contrato/regla o limitarlo a fiado. Esta decisión debe aprobarse antes de esa parte de la entrega Venta.
- Nombre libre de marca del prototipo representa búsqueda/selección futura. En integración se envía brandId existente; crear marca exige operación y permiso existentes.
- Las imágenes locales ilustran selección; carga y validación definitivas reutilizan el mecanismo existente.
- Ningún cambio de navegación requiere reescribir datos históricos. Todo cambio de esquema por precondición debe ser incremental y probar datos anteriores.

## Secuencia por entrega

1. Aprobar las composiciones y estados del prototipo.
2. Verificar contratos de los paneles, permisos e impacto compartido.
3. Implementar interfaz del recorrido con fixtures controlados.
4. Adaptar backend únicamente para las brechas aprobadas.
5. Integrar con la API real y preservar identidad, sucursal, revisiones y claves.
6. Ejecutar pruebas funcionales y técnicas.
7. Revisar visualmente 390/768/1024/1440 y zoom, capturar e inspeccionar.
8. Corregir todos los defectos del alcance.
9. Demostrar el recorrido completo y solicitar aceptación.

## Entregas siguientes

| Entrega | Demo exigida en esa entrega |
| --- | --- |
| Venta completa | Preparar un artículo real, escanearlo, cobrar con los medios soportados, recuperar resultado y ticket |
| Operación diaria | Abrir sesión, vender, cobrar un fiado y revisar cierre con arqueo |
| Inventario y compras | Identificar necesidad, emitir orden, recibir parcial y verificar saldo y trazabilidad |
| Gestión e informes | Consultar ventas y cuentas con permisos y agregados reales |
| Consolidación integral | Repetir los recorridos ya demostrados con un comercio representativo y escenarios de error |

## Aceptación y pruebas de la primera entrega

- Producto incompleto válido; SKU obligatorio, atributos opcionales y valores inactivos conservados.
- Matriz editable, duplicados activos/inactivos, límite 50, rollback y recuperación idempotente.
- Precios nulos y cero diferenciados, lista compartida confirmada y resultados por fila.
- Ajuste con motivo, revisión obsoleta, reservas, respuesta perdida y ausencia de movimientos duplicados.
- Acceso entre tenants/sedes rechazado; permisos revocados y respuestas tardías descartadas.
- URL/historial, CSV, galería, etiquetas y navegación existentes conservados.
- Errores preservan datos; escrituras confirmadas siguen confirmadas si falla un GET.
- Chromium con API real, make check y PostgreSQL descartable para test-db.
- Revisión visual y teclado de todo el recorrido; ningún defecto visual conocido del alcance se pospone.

El prototipo valida una propuesta de interacción y utiliza estado en memoria. La aceptación de implementación exige la API y los datos reales del entorno aislado. La Etapa A y las futuras entregas requieren aprobación humana.
