# ADR 0011 — Etiquetas del catálogo

Desde el detalle de una variante activa o el resultado del escaneo se puede preparar una etiqueta con producto, SKU, talle, color y un código de barras ya registrado. La selección inicial usa el código principal. La operación se realiza en el navegador con los datos que el usuario ya puede consultar en Catálogo.

El formato inicial es de 60 × 40 mm, con una etiqueta por página y entre 1 y 50 copias. Se genera un Code 128 interno en blanco y negro con JsBarcode. El ancho conserva el tamaño de módulo y su margen en el documento impreso. Cuando el símbolo supera el espacio disponible, la interfaz pide elegir un código más corto. [Documentación de Code 128 de JsBarcode](https://github.com/lindell/JsBarcode/wiki/CODE128)

La salida usa el cuadro de impresión del navegador, incluida la opción de guardar como PDF. La prueba local cubre tamaño, paginación y decodificación digital del PDF. La configuración del driver, la calidad térmica y la lectura del papel quedan para la validación con hardware real.
