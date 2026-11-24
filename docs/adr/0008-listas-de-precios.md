# ADR 0008 — Listas de precios y asignación explícita

El propietario crea listas reutilizables. Cada sucursal activa puede tener una lista asignada y varias sucursales pueden compartirla. Una lista general se usa asignándola a las sucursales correspondientes. Si la sucursal carece de asignación o de un importe para la variante en su lista, el precio resuelto es nulo. Esto implementa la regla del ADR 0006.

Los importes usan NUMERIC(18,2) y se transmiten como texto decimal para preservar centavos. Un importe explícito de cero es válido y se distingue de la ausencia. Todas las listas usan la moneda del comercio. Las vigencias y promociones quedan pendientes.

La consulta operativa exige permiso de lectura, sucursal activa asignada al usuario y producto activo del mismo comercio. Las escrituras y la auditoría se confirman juntas. Las FKs compuestas vinculan lista, variante y sucursal al comercio. Repetir la asignación o el precio actual conserva el estado sin generar otro evento.
