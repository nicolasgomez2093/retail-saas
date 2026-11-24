# ADR 0030 — Reintegros con seña aplicada

En una venta pagada íntegramente, la seña aplicada forma parte del importe reintegrable. El saldo por medio suma el anticipo y los pagos registrados al entregar y descuenta los reintegros anteriores de la venta. Los importes de un mismo medio se agrupan. También se admite una entrega cubierta por completo por la seña.

El reintegro requiere una recepción de devolución y una caja abierta de la misma sucursal. El operador usa los medios originales y el importe histórico de la recepción. La porción en efectivo se registra en la sesión del reintegro. Se conservan la seña, la venta y el arqueo de su sesión original como registros históricos.

Las señas de reservas canceladas o vencidas siguen pendientes de resolución comercial. Las compensaciones de deuda por devoluciones de fiados se detallan en el [ADR 0031](0031-compensaciones-de-fiado.md).
