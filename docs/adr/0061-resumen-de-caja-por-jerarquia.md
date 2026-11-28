# ADR 0061 — Resumen de caja ordenado por importancia

## Contexto

La pantalla de Caja mostraba saldo inicial, ingresos, egresos y efectivo esperado con el mismo peso, sin decir cuánto se vendió ni con qué medios se cobró. Quien atiende la caja quiere saber primero dos cosas: cuánto se vendió y cuánto efectivo tiene que haber en el cajón. El resto (gastos, transferencias, fiado, etc.) es detalle.

## Decisión

- **API:** `GET /api/v1/cash-sessions/{id}/summary?store_id=` devuelve, para una sesión:
  - Total y cantidad de ventas de la sesión.
  - Monto vendido a fiado y cantidad de esas ventas.
  - Lo cobrado por medio de pago. Suma ventas, cobros de fiados, señas y diferencias de cambios de la sesión, y resta las devoluciones.
  - Los movimientos del cajón agrupados por tipo.
  - Tiene el mismo alcance que la sesión: quien supervisa ve todas y quien opera ve solo las propias.
  - Solo lee datos que ya existen; no hay tablas nuevas.
- **Pantalla, en tres niveles:**
  1. Dos números grandes:
     - «Vendido», con la cantidad de ventas y lo fiado.
     - «Efectivo que debería haber». Al cerrar muestra lo contado y si sobra o falta, con color.
  2. Acciones: registrar un gasto o ingreso, y el arqueo, que recuerda cuánto debería haber.
  3. Detalle:
     - Cobros por medio de pago, con el fiado aparte como «queda a cobrar».
     - «Cómo se llega al efectivo»: saldo inicial ± cada tipo de movimiento = efectivo esperado.
     - La lista de movimientos queda plegada.
- Si el resumen no carga, la caja sigue funcionando con los totales de efectivo de antes.

## Consecuencias

- Las ventas se cuentan en la sesión donde se registraron. Los cobros posteriores (fiados, señas) se cuentan en la sesión donde se cobraron.
- «Vendido» incluye lo fiado. El efectivo esperado solo cambia por lo que entró o salió del cajón.
