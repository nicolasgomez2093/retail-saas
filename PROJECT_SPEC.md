# Retail SaaS — Especificación funcional y técnica para implementación

Versión: 1.0 | Fecha: 2026-11-18 | Idioma del producto: español (Argentina)

> Documento fuente para iniciar el desarrollo con Codex. Las decisiones marcadas **CONFIRMADA** provienen del relevamiento. Las marcadas **PROPUESTA** son recomendaciones técnicas que se deben validar antes de introducir complejidad irreversible. No afirmar que se inspeccionó el software de referencia por dentro.

## 1. Visión del producto

Crear un sistema integral de gestión comercial SaaS, multiempresa (multi-tenant) y multisucursal, inicialmente especializado en locales de indumentaria, extensible a otros comercios. Se ofrecerá como PWA instalable, con backend Go y frontend React/TypeScript. El producto cubrirá catálogo, variantes de talle/color, etiquetas y lectura de códigos de barras, inventario por depósito, POS, tickets internos, pagos, cajas, compras parciales, transferencias, reservas, ventas fiadas, cuentas por cobrar, costos/márgenes opcionales y reportes.

Referencia funcional pública aportada por el cliente: https://gestionintegral.site/ . Es referencia de categoría y necesidades, **no** fuente verificada de sus módulos internos ni autorización para copiar diseño/código.

### Decisiones confirmadas
- SaaS comercializable a múltiples negocios; aislamiento estricto entre tenants.
- Múltiples sucursales desde la primera versión.
- Precios globales o diferenciados por sucursal; cada negocio decide su política.
- Depósitos/ubicaciones separados dentro de sucursales; posibilidad de depósito central como extensión del modelo.
- Compra con recepciones parciales.
- Venta fiada: entregar prendas, registrar deuda y cobrar después (posibles abonos parciales). La pregunta de detalle sobre cuotas quedó sin respuesta expresa, pero el requisito de fiado con cobro posterior sí está confirmado.
- Reservas de mercadería.
- Medios de pago: efectivo, transferencia, débito, QR, tarjeta de crédito; pagos mixtos.
- Costos y márgenes detallados, pero carga de costos opcional.
- Ticket comercial interno inicialmente, no comprobante fiscal.
- Futuro adaptador fiscal ARCA / posible integración con proyecto llamado «Facturitas» (identidad y licencia exacta por verificar antes de integrarlo).
- Online-first: ante corte de Internet, el comercio puede esperar; NO implementar venta offline ni sincronización de ventas en MVP.
- Hardware todavía no comprado: priorizar lector USB/Bluetooth modo teclado y validación real de impresora térmica.

### Propuestas técnicas (no confirmadas por el usuario como elecciones definitivas)
- Backend Go + Gin; monolito modular; PostgreSQL; migraciones SQL versionadas; GORM o SQLC según evaluación.
- Frontend React + TypeScript + Vite, Tailwind + shadcn/ui, TanStack Query, React Hook Form + Zod, Zustand solo cuando corresponda.
- Monorepo, Docker Compose local, CI, OpenAPI como contrato, cliente TypeScript generado.
- Registro de comercios administrado por operador en la fase inicial; autoservicio en fase futura.
- Code 128 para códigos internos; no inventar GTIN/GS1 válidos.
- Despliegue con API contenedorizada, PostgreSQL administrado y almacenamiento de archivos compatible S3.

## 2. Roles y acceso

Roles base: PLATFORM_ADMIN, OWNER, STORE_MANAGER, CASHIER, INVENTORY_OPERATOR. Implementar RBAC extensible, permisos por acción y asignaciones de sucursal. PLATFORM_ADMIN administra el servicio pero no debe tener acceso irrestricto por defecto a los datos comerciales; acceso de soporte requiere autorización y auditoría. Un usuario puede operar únicamente en las sucursales habilitadas. Derivar tenant de autenticación, nunca confiar en tenant_id del cuerpo de solicitudes. Validar en cada acceso a recurso tanto tenant como sucursal.

## 3. Módulos y requisitos

### 3.1 Identidad y SaaS
- Autenticación segura, cierre de sesión, recuperación de contraseña, usuarios, roles, permisos, sucursales asignadas.
- Crear/configurar tenant, estado, moneda, zona horaria, numeración y preferencias.
- Crear, editar y desactivar sucursales.
- Planes/suscripciones en modelo de datos; automatización de cobro SaaS fuera del MVP.
- Auditoría de ajustes de stock, cambios de precio, descuentos, devoluciones, anulaciones y permisos.

### 3.2 Catálogo
- Productos padre con nombre, descripción, categoría, marca, fotos y estado.
- Variantes vendibles por talle/color y atributos configurables; SKU único por tenant.
- Uno o más códigos por variante, código primario y etiquetas imprimibles.
- Buscar por código, SKU, nombre; importación CSV y exportación básica.
- Precio de venta por lista; listas globales y asignaciones por sucursal, con herencia/fallback explícito.
- No borrar físicamente artículos que aparezcan en operaciones históricas.

### 3.3 Inventario
- Ubicaciones por sucursal: salón, depósito principal, otros depósitos. Saldo por variante y ubicación.
- Stock físico, reservado y disponible; disponible = físico - reservado, ajustado por cualquier restricción de disponibilidad aplicable.
- Ingresos, egresos, ajustes con motivo, conteos físicos, movimientos inmutables, historial y stock mínimo.
- Transferencias dentro de sucursal y entre sucursales; estados DRAFT, DISPATCHED, RECEIVED, CANCELLED, con mercadería en tránsito. No acreditar destino hasta recepción.
- Evitar stock negativo por defecto. Bloquear/actualizar atómicamente para impedir sobreventa concurrente.

### 3.4 POS / ventas
- POS rápido con foco de lectura, entrada por escáner modo teclado, búsqueda manual, carrito, cantidad, descuentos autorizados, cliente opcional salvo fiado.
- Precios resueltos según lista aplicable a sucursal; capturar precio y descuentos históricos por ítem.
- Pagos efectivo, transferencia, débito, crédito, QR y mixtos; efectivo puede tener vuelto.
- Confirmación transaccional: venta, ítems, pagos, salida de stock, efectos de caja, auditoría/evento; idempotencia por operación.
- Ticket interno numerado, reimpresión, historial, devoluciones/cambios y anulaciones mediante compensaciones.
- La impresión ocurre después de confirmar la venta: fallo de impresora NO revierte ni duplica la operación.
- No prometer factura fiscal ni validez tributaria del ticket interno.

### 3.5 Fiados y cuentas por cobrar
- Venta fiada: mercadería entregada, stock disminuido, venta confirmada y saldo financiero pendiente.
- Cliente identificado obligatoriamente para fiado.
- Registrar total, anticipo si existe, deuda, pagos posteriores, historial, vencimientos opcionales, permisos y límites de crédito configurables.
- Una venta confirmada puede estar entregada y seguir financieramente pendiente; no mezclar estado comercial con estado de cobranza.
- Registrar abonos parciales y su impacto en caja; evitar aplicar un pago dos veces.
- Si hay pagos en cuotas acordadas, modelar vencimientos como ampliación o diseñar extensibilidad; no asumir cálculo de intereses.

### 3.6 Reservas y señas
- Reservar cantidades por variante y ubicación/sucursal; descontar de disponible, NO de físico.
- Estados ACTIVE, FULFILLED, CANCELLED, EXPIRED; vencimiento y liberación controlada.
- Reservas con o sin seña; seña como anticipo de cliente, no ingreso por venta automáticamente.
- Convertir reserva a venta sin liberar indebidamente la unidad para otro cajero; transacción atómica.
- Política de vencimiento y tratamiento de señas por definir con el negocio; no automatizar decisiones comerciales sin configuración.

### 3.7 Compras y proveedores
- Proveedores, órdenes de compra, líneas con cantidades/costos opcionales, recepciones parciales múltiples.
- Cada recepción aumenta stock únicamente por cantidad efectivamente aceptada.
- Registrar faltantes, rechazos, diferencias y trazabilidad de recepciones.
- Gastos adicionales de compra opcionales, por ejemplo flete.

### 3.8 Caja
- Cajas físicas/lógicas por sucursal; apertura y cierre por sesión, una sesión abierta por caja.
- Saldo inicial, movimientos manuales con motivo, cobros, devoluciones, arqueo, efectivo esperado/declarado, diferencias.
- Distinguir caja física de conciliación de transferencias, tarjetas y QR. No considerar todos los medios como efectivo en caja.

### 3.9 Costos y rentabilidad
- Costos de compra opcionales, costo histórico, gastos de adquisición asignables, comisiones de medios de pago.
- Proponer costo promedio ponderado para MVP, con snapshot de costo al vender y posibilidad de evolucionar a capas FIFO; elegir política explícita antes de implementar cálculos definitivos.
- Margen bruto = (venta neta - costo de mercadería) / venta neta; contribución después de comisiones/gastos variables separada del margen bruto.
- Si costo falta, guardar NULL, no 0. Mostrar rentabilidad «no disponible/parcial» en lugar de un margen ficticio.
- Reportes por venta, variante, categoría y sucursal; no mezclar resultados con/sin costo sin indicarlo.

### 3.10 Reportes
- Dashboard por comercio y sucursal; ventas por período, productos más vendidos, ventas por medio de pago, existencias y movimientos, deudas y cobros, costos/márgenes cuando haya datos.
- Exportación CSV. Fechas almacenadas en UTC, presentación según zona horaria del tenant.

### 3.11 Fiscal futuro
- Mantener interfaz FiscalProvider desacoplada, sin emisión ARCA en MVP.
- Investigar identidad, repositorio, licencia, mantenimiento y API del proyecto «Facturitas» antes de seleccionarlo; no suponer que es abierto.
- Preparar extensibilidad para documentos fiscales, CAE, puntos de venta, errores/reintentos y credenciales seguras, sin almacenar secretos en código.

## 4. Reglas de negocio críticas

1. Aislamiento tenant en todas las entidades operativas y consultas; FK compuestas o mecanismos equivalentes para impedir referencias cruzadas.
2. Una variante puede existir en varias ubicaciones; la ubicación determina saldo físico.
3. Movimiento confirmado no se edita ni borra: se compensa con otro movimiento.
4. Una venta requiere caja abierta y permisos de sucursal.
5. El precio se resuelve por sucursal y queda congelado en el detalle de venta.
6. La API recalcula totales y descuentos; nunca confía en totales enviados por frontend.
7. Confirmación de venta en una transacción de BD, con bloqueo consistente de saldos; idempotencia por tenant y clave.
8. No vender unidades reservadas por otros clientes.
9. Fiado entrega mercadería y genera deuda; reserva no entrega y mantiene stock físico.
10. Una devolución no excede cantidad vendida menos devoluciones previas; puede volver a stock vendible o cuarentena según estado.
11. Pagos aplicados no exceden deuda pendiente salvo regla explícita para saldos a favor.
12. Transferencias: stock origen disminuye al despachar; destino aumenta al recibir; diferencia se documenta.
13. Recepción parcial nunca marca como recibidas unidades pendientes.
14. Tickets internos tienen numeración única por serie/tenant/sucursal y pueden reimprimirse.
15. Si no hay conexión, no se confirma una venta localmente; mostrar error claro y permitir reintento seguro cuando vuelva.
16. Los datos históricos de venta no se modifican retroactivamente por cambios de catálogo/precios/costos.
17. Costos faltantes no equivalen a cero.
18. Los medios de pago deben ser configurables y extensibles; no guardar PAN completo ni datos sensibles de tarjetas.

## 5. Modelo de datos inicial (propuesta)

Todos los identificadores: UUID. Importes: NUMERIC(18,2) u otra precisión validada, nunca float. Cantidades: INTEGER (prendas unitarias); extender a NUMERIC si se comercializan productos fraccionados. created_at/updated_at UTC. tenant_id obligatorio en entidades de negocio. Claves únicas compuestas por tenant. Definir índices según consultas y FK compuestas para aislamiento.

### Núcleo SaaS e identidad
- tenants(id, name, slug, status, timezone, currency_code, settings_json, created_at, updated_at)
- stores(id, tenant_id, code, name, address, status, created_at, updated_at)
- users(id, tenant_id, email, password_hash, name, status, created_at, updated_at)
- roles(id, tenant_id nullable para roles plantilla, code, name)
- permissions(id, code, description)
- role_permissions(role_id, permission_id)
- user_roles(user_id, role_id)
- user_stores(user_id, store_id)
- plans(id, code, name, limits_json)
- subscriptions(id, tenant_id, plan_id, status, starts_at, ends_at)
- audit_logs(id, tenant_id, actor_user_id, action, entity_type, entity_id, before_json, after_json, created_at)

### Catálogo y precios
- categories(id, tenant_id, parent_id nullable, name, status)
- brands(id, tenant_id, name)
- products(id, tenant_id, category_id, brand_id, name, description, status, created_at)
- product_variants(id, tenant_id, product_id, sku, attributes_json, status, created_at)
- barcodes(id, tenant_id, variant_id, code, symbology, is_primary)
- price_lists(id, tenant_id, name, currency_code, status, is_default)
- price_list_items(id, tenant_id, price_list_id, variant_id, price, effective_from nullable, effective_to nullable)
- store_price_list_assignments(id, tenant_id, store_id, price_list_id, effective_from)
- product_images(id, tenant_id, product_id, storage_key, sort_order)

### Ubicaciones e inventario
- stock_locations(id, tenant_id, store_id nullable, code, name, type, is_sellable, status)
- stock_balances(id, tenant_id, location_id, variant_id, quantity_on_hand, quantity_reserved, updated_at), UNIQUE(tenant_id,location_id,variant_id)
- stock_movements(id, tenant_id, location_id, variant_id, quantity_delta, movement_type, reference_type, reference_id, actor_id, created_at)
- stock_transfers(id, tenant_id, origin_location_id, destination_location_id, status, created_by, dispatched_at, received_at)
- stock_transfer_items(id, tenant_id, transfer_id, variant_id, requested_qty, dispatched_qty, received_qty)
- stock_counts(id, tenant_id, location_id, status, created_by, completed_at)
- stock_count_items(id, tenant_id, stock_count_id, variant_id, expected_qty, counted_qty)

### Ventas, cobros y caja
- customers(id, tenant_id, name, document_number nullable, phone nullable, email nullable, credit_limit nullable, status)
- cash_registers(id, tenant_id, store_id, code, name, status)
- cash_sessions(id, tenant_id, cash_register_id, opened_by, opening_cash, opened_at, closed_at, declared_cash, expected_cash, difference, status)
- sales(id, tenant_id, store_id, cash_session_id, customer_id nullable, sale_number, commercial_status, payment_status, subtotal, discount_total, total, idempotency_key, created_by, created_at)
- sale_items(id, tenant_id, sale_id, variant_id, product_name_snapshot, sku_snapshot, attributes_snapshot, quantity, unit_price, discount_amount, line_total, unit_cost_snapshot nullable)
- payment_methods(id, tenant_id, code, name, enabled)
- payments(id, tenant_id, sale_id nullable, receivable_id nullable, method_id, amount, status, external_reference nullable, created_at)
- cash_movements(id, tenant_id, cash_session_id, movement_type, payment_id nullable, amount, reason nullable, actor_id, created_at)
- receivables(id, tenant_id, sale_id, customer_id, original_amount, outstanding_amount, due_date nullable, status)
- receivable_payments(id, tenant_id, receivable_id, payment_id, applied_amount, created_at)
- returns(id, tenant_id, sale_id, store_id, status, reason, created_by, created_at)
- return_items(id, tenant_id, return_id, sale_item_id, quantity, disposition)

### Reservas, compras y costos
- reservations(id, tenant_id, store_id, customer_id, status, expires_at nullable, created_by, created_at)
- reservation_items(id, tenant_id, reservation_id, variant_id, location_id, quantity, unit_price_snapshot nullable)
- customer_advances(id, tenant_id, customer_id, reservation_id nullable, amount, unapplied_amount, status, created_at)
- suppliers(id, tenant_id, name, contact_json, status)
- purchase_orders(id, tenant_id, supplier_id, store_id, status, ordered_at, created_by)
- purchase_order_items(id, tenant_id, purchase_order_id, variant_id, ordered_qty, unit_cost nullable)
- purchase_receipts(id, tenant_id, purchase_order_id, destination_location_id, status, received_at, received_by)
- purchase_receipt_items(id, tenant_id, receipt_id, purchase_order_item_id, accepted_qty, rejected_qty, unit_cost nullable)
- inventory_cost_entries(id, tenant_id, variant_id, location_id, reference_type, reference_id, quantity, unit_cost nullable, allocated_extra_cost nullable, created_at)
- fiscal_documents(id, tenant_id, sale_id nullable, provider, status, external_id nullable, payload_json nullable, created_at) [futuro]

NOTAS: Este es un esquema lógico inicial, NO DDL definitivo. Evitar duplicación de fuentes de verdad (por ejemplo, payments y receivable_payments); decidir inmutabilidad, conciliación, manejo de transacciones, constraints y cascadas antes de generar migraciones. Considerar modelar depósitos centrales sin store_id mediante entidad organizativa explícita, no permitir ubicaciones huérfanas. Para reservas con stock por ubicación, diseñar correctamente reservas concurrentes y su liberación. Evitar crear todas las tablas del futuro en el primer sprint.

## 6. API REST inicial (v1)

Autenticación: POST /api/v1/auth/login; POST /api/v1/auth/logout; GET /api/v1/me.
Tenants/Stores: GET /api/v1/stores; POST /api/v1/stores; PATCH /api/v1/stores/{id}.
Catálogo: GET/POST /api/v1/products; GET/PATCH /api/v1/products/{id}; POST /api/v1/products/{id}/variants; GET /api/v1/barcodes/{code}/lookup; GET /api/v1/price-lists.
Inventario: GET /api/v1/locations; GET /api/v1/inventory?store_id=...; POST /api/v1/inventory/adjustments; POST /api/v1/transfers; POST /api/v1/transfers/{id}/dispatch; POST /api/v1/transfers/{id}/receive.
Ventas: POST /api/v1/sales (Idempotency-Key obligatoria); GET /api/v1/sales/{id}; GET /api/v1/sales/{id}/ticket; POST /api/v1/sales/{id}/returns.
Caja: POST /api/v1/cash-sessions; POST /api/v1/cash-sessions/{id}/close; GET /api/v1/cash-sessions/{id}/movements.
Compras: POST /api/v1/purchase-orders; POST /api/v1/purchase-orders/{id}/receipts.
Clientes/deudas: GET/POST /api/v1/customers; GET /api/v1/receivables; POST /api/v1/receivables/{id}/payments.
Reservas: POST /api/v1/reservations; POST /api/v1/reservations/{id}/fulfill; POST /api/v1/reservations/{id}/cancel.
Reportes: GET /api/v1/reports/sales; GET /api/v1/reports/inventory; GET /api/v1/reports/receivables.

Diseñar OpenAPI con esquemas de request/response, errores estandarizados, paginación, filtros, ordenamiento, permisos, validaciones y ejemplos. No implementar endpoints sin definir antes semántica y estados.

## 7. Arquitectura

### Backend
Monolito modular Go. Capas por módulo: domain (entidades/reglas), application (casos de uso), infrastructure (persistencia/adaptadores), transport/http (handlers/DTO). Módulos: auth, tenancy, stores, catalog, pricing, inventory, transfers, sales, cash, customers, receivables, reservations, purchasing, reporting, audit. Paquete platform para config, DB, middleware, logging, auth y transacciones. Los módulos no deben llamar handlers entre sí; coordinar mediante servicios de aplicación y límites transaccionales explícitos.

### Frontend
React TypeScript PWA. Estructura `src/app`, `src/features/{auth,dashboard,products,pricing,inventory,transfers,pos,sales,cash,purchasing,customers,receivables,reservations,reports,settings}`, `src/shared/{api,components,hooks,lib,types}`. POS orientado a teclado y escáner. Rutas protegidas pero autorización efectiva en API. Gestión de estado remoto con TanStack Query. PWA online-first, caché de assets; no persistir datos sensibles sin evaluación.

### Monorepo propuesto
```
retail-platform/
  apps/api/cmd/
  apps/api/internal/platform/
  apps/api/internal/modules/
  apps/api/migrations/
  apps/web/src/
  apps/web/public/
  packages/api-client/
  infra/docker/
  docs/
  compose.yaml
  README.md
  AGENTS.md
```

### Infraestructura
Docker Compose para desarrollo con PostgreSQL y API, frontend dev server. Producción: HTTPS, frontend estático/CDN, API en contenedor, PostgreSQL administrado, storage de imágenes, backups y restauración probada, logs estructurados, métricas y alertas. Sin Redis/RabbitMQ/Kubernetes por defecto. Outbox transaccional si se agregan tareas asíncronas confiables.

## 8. Flujos transaccionales

### Confirmar venta
1. Recibir carrito, sucursal/caja, medios de pago, cliente cuando corresponda, clave de idempotencia.
2. Autenticar, validar tenant, permisos, caja abierta y sucursal.
3. Iniciar transacción; bloquear filas de stock en orden determinista.
4. Resolver precios/descuentos vigentes del servidor; validar disponibilidad sin consumir reservas ajenas.
5. Registrar venta, ítems y snapshots; pagos y/o cuenta por cobrar; movimientos y saldos de stock; efectos de caja; auditoría y evento si corresponde.
6. Commit. Retornar identificador y datos del ticket. Impresión posterior al commit.
7. Reintento con la misma clave devuelve el resultado original, sin duplicar venta.

### Venta fiada
Confirmar venta y salida de mercadería, registrar anticipo si existe, abrir receivable por saldo, permitir cobros posteriores asociados a caja/sucursal. Estado comercial y financiero separados.

### Reserva
Transacción de verificación de disponibilidad y aumento de reservado. En cumplimiento, convertir a venta y consumir reserva atómicamente. Cancelación/vencimiento libera reservado. Seña se aplica según política, sin duplicar ingresos.

### Transferencia
DRAFT: sin impacto. DISPATCHED: disminuir disponible/físico de origen y registrar tránsito. RECEIVED: registrar recepción real y aumentar destino. Registrar diferencias y evitar doble despacho/recepción.

### Compra parcial
Orden de compra con cantidades pedidas; cada recepción aceptada aumenta stock y actualiza acumulado recibido, sin exceder cantidad ordenada salvo permiso/regla explícita.

## 9. Seguridad, calidad y no funcionales

- Objetivo disponibilidad 99,5% mensual; metas p95: lookup código <300 ms en API y confirmación venta <1 s bajo carga de referencia (validar con pruebas, no prometer sin medición).
- TLS, hashes Argon2id, cookies seguras o sesiones/token con rotación, rate limiting de auth, validación de inputs, políticas de CORS, secretos fuera del repositorio.
- Protección tenant y sucursal en cada query; considerar PostgreSQL RLS como defensa adicional, con especial atención al pool de conexiones.
- Auditoría de operaciones críticas, datos históricos inmutables y reversos explícitos.
- Backups, restauración periódica; objetivos iniciales orientativos RPO 24 h, RTO 4 h, a validar con negocio.
- Accesibilidad objetivo WCAG 2.2 AA en flujos principales, navegadores modernos y uso de teclado.
- Tests unitarios de reglas; integración DB; API y permisos; E2E POS; concurrencia; multi-tenant; idempotencia; impresión con hardware real.

## 10. Criterios de aceptación críticos

1. Dos cajas intentan vender la última unidad: solo una venta se confirma.
2. Venta confirmada actualiza stock, caja y pagos de forma atómica.
3. Reintento tras timeout no duplica la venta.
4. Un usuario de tenant A nunca accede a recursos de tenant B, incluso manipulando IDs.
5. Transferencia no aumenta destino antes de recepción.
6. Reserva reduce disponible, no físico; vencimiento/cancelación libera disponibilidad.
7. Fiado reduce físico, genera deuda y admite pagos posteriores sin doble aplicación.
8. Compra parcial recibe 60/100 y deja 40 pendientes.
9. Cambio de precio no modifica ventas históricas.
10. Costo NULL muestra margen desconocido, nunca 100% ficticio.
11. Impresora fallida permite reimprimir sin duplicar venta.
12. Caja cierra con efectivo esperado/declarado y diferencia.

## 11. Plan incremental de desarrollo

Fase 0: inicializar monorepo, AGENTS.md, README, compose, lint, tests, CI, OpenAPI, estructura modular y ADRs.
Fase 1: tenants, stores, auth, RBAC y usuarios asignados a sucursales.
Fase 2: catálogo, variantes, códigos de barras, listas de precios y asignaciones.
Fase 3: ubicaciones, inventario, movimientos, ajustes y transferencias.
Fase 4: POS, caja, ventas transaccionales, pagos y ticket interno.
Fase 5: clientes, fiados, cobros posteriores, reservas, señas y devoluciones.
Fase 6: proveedores, compras, recepciones parciales, costos y reportes.
Fase 7: pruebas integrales, hardware real, observabilidad, seguridad, backups, piloto y capacitación.

Implementar verticalmente (DB + API + UI + pruebas) y no desarrollar todo el backend antes de probar flujos. Cada fase debe tener pruebas y documentación actualizadas.

## 12. Pendientes de decisión (NO inventar requisitos)

- Política exacta de precios: listas por sucursal, prioridades de promociones, fechas de vigencia, precios mayoristas.
- Depósito central independiente y reglas de surtido/transferencia.
- Política de reserva: vencimiento, cancelación, seña, devoluciones.
- Política de fiados: límites, vencimientos, cuotas, mora/intereses, abonos y saldos a favor.
- Política de devoluciones: plazos, cambio por diferencia, destino de prenda devuelta.
- Comisiones de tarjetas/QR y conciliación con liquidaciones.
- Método de costeo definitivo (promedio ponderado vs FIFO), gastos prorrateables, impuestos y valuación de devoluciones.
- Dispositivos de impresión/escaneo concretos y entorno operativo del comercio.
- Facturación fiscal: identificar proyecto Facturitas y su licencia/API; validación tributaria por profesional competente.
- Hosting/proveedor, dominio, políticas de datos, soporte, suscripciones y precios del SaaS.
- Necesidades reales de volumen y rendimiento.

## 13. Instrucciones operativas para Codex

1. Leer este archivo completo antes de escribir código. Tratarlo como fuente de requisitos, no como diseño físico ya aprobado.
2. Comenzar con un inventario del repositorio y proponer un plan corto. Si está vacío, crear las fundaciones de Fase 0.
3. No implementar todas las funcionalidades en un único cambio. Entregar iteraciones pequeñas, ejecutables y verificables.
4. Priorizar aislamiento multi-tenant, integridad transaccional y mantenibilidad sobre complejidad prematura.
5. Documentar decisiones no obvias en `docs/adr/` y supuestos en `docs/open-questions.md`.
6. Generar migraciones SQL revisables, modelos y tests. No aplicar migraciones destructivas a producción sin aprobación.
7. Nunca incorporar secretos reales, claves fiscales, tarjetas ni credenciales en código o fixtures.
8. Mantener OpenAPI y frontend sincronizados; tipado estricto y manejo consistente de errores.
9. Antes de cada fase, presentar archivos a crear, contratos y criterios de aceptación; luego implementar y ejecutar tests.
10. Si una decisión pendiente bloquea una implementación irreversible, preguntar antes de asumir.

### Primera tarea concreta
Inicializar el monorepo, Docker Compose con PostgreSQL, esqueleto de Go/Gin, esqueleto de React/Vite/TypeScript PWA, healthcheck, migraciones iniciales de tenants/stores/users y autenticación base, estructura de documentación, Makefile, `.env.example`, pruebas mínimas y README de arranque local. Presentar primero plan de implementación y estructura de archivos. NO comenzar por POS antes de tener tenancy y permisos.
