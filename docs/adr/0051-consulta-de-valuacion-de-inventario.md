# ADR 0051 — Consulta de valuación por ubicación

La consulta existente de inventario expone moneda, estado del costo, costo promedio unitario de la sucursal y valor de las unidades de cada ubicación. La consulta conserva el alcance actual de permisos y filtros.

El valor se calcula con aritmética decimal en PostgreSQL. Para una ubicación sin unidades, el valor es cero aunque no exista costo unitario. Si hay unidades con costo desconocido, el costo promedio y el valor de esas unidades se exponen como `null`; la interfaz los presenta como “No disponible”. No se suman filas con costo conocido para presentar un total de sucursal que podría ocultar stock desconocido.

El resumen por sucursal abarca el stock físico de sus ubicaciones activas, incluidas las unidades reservadas. Muestra unidades con y sin costo. El valor conocido suma únicamente las unidades con costo disponible. La valuación total está disponible si todas las unidades tienen costo, y es cero si no hay stock. Cada importe conserva la precisión decimal de PostgreSQL. Se aplica el acceso existente de lectura de inventario.

La respuesta incluye el desglose de las ubicaciones activas con las mismas reglas de cobertura y valor. Una ubicación vacía tiene valor total cero. El desglose y el resumen se calculan juntos, con el mismo costo promedio de la sucursal para cada variante.

La descarga CSV usa la misma consulta y los mismos permisos. Incluye una fila para la sucursal y otra por cada ubicación activa. Conserva los importes decimales, deja vacíos los valores desconocidos y protege los códigos y nombres que una planilla podría interpretar como fórmulas.
