# ADR 0045 — Devolución directa de un reemplazo

Una recepción de prendas entregadas como reemplazo puede resolverse mediante reintegro, compensación de deuda o una combinación de ambos cuando la venta fue fiada. Se reutilizan las reglas de resolución de una recepción y los saldos de cobros de la cadena. El importe de referencia es el valor histórico de las prendas recibidas.

Cada compensación reduce la deuda vigente. El operador elige el importe hasta el menor entre la deuda y el valor pendiente de la recepción. Se pueden registrar compensaciones parciales con caja cerrada. Si queda un importe pendiente, se reintegra completo en una caja abierta cuando los cobros disponibles alcanzan. Por cada medio se usan primero los cobros de la cadena, del más cercano al más antiguo, y luego los de la venta original.

La recepción física ya registró el ingreso de stock. La compensación y el reintegro conservan ese movimiento y el ticket original. La recepción admite un reintegro y los reintentos recuperan la operación registrada. Si ya se aplicó una compensación o un reintegro, esa recepción se termina de resolver por esas vías. Una recepción resuelta con otro cambio conserva su registro y las nuevas prendas se devuelven desde ese cambio.
