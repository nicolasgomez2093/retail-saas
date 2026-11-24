# ADR 0041 — Otro cambio por el mismo importe

Si una prenda entregada como reemplazo vuelve al local, se puede usar su recepción para registrar otro cambio por el mismo importe. El precio histórico de las unidades recibidas define el importe de referencia. Las prendas nuevas se valoran según los precios vigentes de la sucursal. El sistema valida que los importes coincidan al confirmar.

La entrega usa el mismo flujo de cambios. Se descuenta el stock de la ubicación vendible, se resuelve la recepción en una sola transacción y se conserva el registro ante reintentos. Cada recepción admite un cambio y el historial muestra el vínculo con el cambio que entregó las prendas. Si vuelven las prendas del nuevo cambio, pueden recibirse desde ese cambio.

La operación puede registrarse con caja cerrada. Los cobros, el saldo del fiado y el ticket original conservan sus importes. El cobro de una diferencia se detalla en el [ADR 0042](0042-otro-cambio-con-diferencia-a-cobrar.md). El reintegro de la diferencia en ventas pagadas se detalla en el [ADR 0043](0043-otro-cambio-con-diferencia-a-reintegrar.md).
