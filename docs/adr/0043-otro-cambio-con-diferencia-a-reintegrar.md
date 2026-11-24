# ADR 0043 — Otro cambio con diferencia a reintegrar

Una recepción de reemplazos de una venta pagada sin fiado admite entregar prendas nuevas de menor valor. La diferencia surge del importe histórico de la recepción y el precio vigente de las prendas nuevas. El operador la distribuye en una caja abierta entre los medios por los que se cobró la venta y las diferencias de la cadena de cambios que dio origen a esa recepción.

Cada medio tiene un límite según sus cobros y lo ya reintegrado. Cuando un medio se usó en más de un cobro, el sistema consume primero los cobros de la cadena, del más cercano al más antiguo, y aplica el remanente a lo cobrado en la venta original. Las recepciones que comparten un cobro comparten también su saldo disponible. Se conserva la imputación de cada reintegro a los cobros del cambio y se registra en el arqueo solo la parte devuelta en efectivo.

La salida de stock, el reintegro y su imputación se confirman en una sola transacción. Los reintentos devuelven el cambio original. Los reintegros ya registrados conservan su imputación a la venta original. La compensación en fiados se detalla en el [ADR 0044](0044-otro-cambio-de-menor-valor-en-fiados.md). El reintegro directo de una prenda de reemplazo se detalla en el [ADR 0045](0045-devolucion-directa-de-un-reemplazo.md).
