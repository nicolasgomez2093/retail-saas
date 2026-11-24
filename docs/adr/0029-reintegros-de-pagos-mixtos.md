# ADR 0029 — Reintegros de pagos mixtos

En ventas pagadas con varios medios, el cajero registra los importes reintegrados por los medios originales. El sistema muestra el saldo disponible de cada uno. El desglose puede usar uno o más medios, debe cubrir el importe histórico completo de la recepción y respeta los reintegros anteriores de la misma venta. La distribución la indica el operador.

El reintegro, sus importes por medio y el egreso en efectivo se guardan en una transacción. Las operaciones de una misma venta se serializan antes de calcular saldos disponibles. Solo la parte en efectivo impacta el arqueo de la sesión. Los componentes electrónicos quedan registrados por su medio.

Se admite un reintegro por recepción. Los reintentos normalizan el orden y los decimales del desglose. Los reintegros históricos pasan a tener un único importe por su medio original. Los clientes anteriores conservan el contrato de un solo medio. Los reintegros con seña aplicada se detallan en el [ADR 0030](0030-reintegros-con-sena-aplicada.md). Quedan pendientes las ventas fiadas y los cambios.
