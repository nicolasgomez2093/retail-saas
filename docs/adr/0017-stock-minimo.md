# ADR 0017 — Stock mínimo por variante y ubicación

El propietario y el encargado pueden configurar o quitar un mínimo en sus sucursales activas asignadas. El personal con lectura de inventario puede consultar el mínimo y filtrar las alertas. Cada combinación de ubicación y variante tiene su propio mínimo opcional.

La alerta compara el stock disponible, calculado como físico menos reservado, con el mínimo. Aparece cuando el disponible es igual al mínimo o menor. Cero es un mínimo explícito y nulo quita la configuración. Una variante configurada sin movimientos se muestra con cero unidades. Las variantes o productos inactivos conservan su mínimo y quedan fuera de las alertas hasta que vuelvan a estar activos.

La configuración comparte el alcance de comercio y sucursal con inventario y vive en una tabla separada de los saldos. Guardar o quitar un mínimo conserva el stock, la revisión de los conteos y los movimientos. Las alertas se calculan en la consulta de existencias.
