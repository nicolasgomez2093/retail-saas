# ADR 0020 — Ticket interno y reimpresión

Cada venta confirmada tiene un número correlativo por sucursal. El ticket muestra el comercio, la sucursal y su código, la fecha en hora del comercio, las líneas de venta, el total, el efectivo y el vuelto. La consulta usa los datos fijados al confirmar, incluidos los importes y datos de catálogo. Para ventas anteriores a esta entrega, la migración completa los datos de cabecera con los del comercio y la sucursal al momento de actualizar la base.

La vista previa se abre después de confirmar la venta. El operador puede imprimirla o guardar un PDF desde el navegador y reabrirla desde el historial incluso con la caja cerrada. El ticket y el historial reutilizan los permisos de lectura de ventas. El historial se filtra por sucursal y número y se pagina de a 25 en la interfaz.

La impresión usa diseños para papel de 58 y 80 mm. El tamaño del papel se elige también en el cuadro de impresión. Cancelar o repetir la impresión conserva la venta, la numeración, el stock y la caja. La validación en impresoras físicas queda pendiente del equipo que use el comercio.
