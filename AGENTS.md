# Instrucciones para Codex

Lee `PROJECT_SPEC.md` antes de desarrollar. Este repositorio es un SaaS multi-tenant/multisucursal de gestión comercial para indumentaria, con Go + React PWA. Las decisiones confirmadas, propuestas, reglas de negocio, modelo lógico, fases y criterios de aceptación están en ese archivo.

Reglas obligatorias:
- Nunca confiar en `tenant_id` enviado por el cliente; aislar por identidad autenticada y permisos de sucursal.
- No usar `float` para dinero. No interpretar costo faltante como costo cero.
- Ventas, stock, pagos y caja requieren integridad transaccional e idempotencia.
- No desarrollar facturación ARCA ni modo offline en MVP.
- Implementar por fases verticales con pruebas y documentación.
- No agregar dependencias de infraestructura sin necesidad demostrada.
- Registrar dudas en `docs/open-questions.md` y decisiones en `docs/adr/`.
- No inventar políticas comerciales no decididas.

Primer objetivo: proponer plan y construir solo fundaciones de Fase 0 y la base de Fase 1, con pruebas y comandos de ejecución local.
