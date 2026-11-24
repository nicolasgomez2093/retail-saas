# ADR 0032 — Reintegros de ventas fiadas

El importe reintegrable de una recepción es su valor histórico menos las compensaciones aplicadas a la deuda. Se registra un reintegro por recepción cuando los cobros previos disponibles cubren todo ese importe. El operador confirma el desglose, incluso cuando hay un solo medio. La cuenta pendiente y su compensación se muestran junto al reintegro.

El saldo reintegrable por medio suma el anticipo, la seña aplicada y los abonos posteriores, y descuenta reintegros previos de esa venta. Un ajuste de deuda reduce el saldo pendiente de la recepción y el fiado. Los registros de deuda por cobrar se conservan por separado de los cobros. El reintegro en efectivo se asienta en la caja de la sesión en que se realiza.

Abonos, compensaciones y reintegros de un mismo fiado serializan el cálculo en la cuenta por cobrar. La API compara el desglose explícito con el importe vigente de la recepción para detectar una compensación concurrente. Los reintentos conservan el reintegro original. Las devoluciones de señas de reservas canceladas o vencidas y los cambios de ventas fiadas quedan pendientes de sus reglas comerciales. Los cambios de igual importe para ventas pagadas se documentan en el [ADR 0033](0033-cambios-de-igual-importe.md).
