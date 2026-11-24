# Preguntas abiertas

- ¿Cómo se entregará al propietario el acceso inicial fuera del entorno local? Definir recuperación, cambio de contraseña y revocación de todas las sesiones.
- Definir la gestión de propietarios existentes y la delegación de funciones adicionales. Precisar la autorización y auditoría de soporte de plataforma.
- ¿Se requiere RLS como defensa adicional? Definir usuario de migración y usuario de aplicación para el despliegue.
- ¿Dónde se desplegará y cómo se operarán TLS, limpieza de sesiones, límites de login compartidos, alertas y backups?
- Definir reglas de promociones, vigencias y precios mayoristas. Las listas iniciales se aplican por asignación explícita según [ADR 0008](adr/0008-listas-de-precios.md).
- Los detalles de depósito central, reservas, fiados, devoluciones, conciliación, gastos de compra asignables, fiscalización, hardware y volumen siguen pendientes según PROJECT_SPEC.md. El promedio ponderado de MVP queda definido en ADR 0050; la consulta de valuación por ubicación en ADR 0051 y el reporte de margen original de venta en ADR 0052.

- Definir el alcance de la configuración de medios de pago, quién puede administrarlos y cómo se manejará una desactivación mientras haya una venta en curso.
- Definir cómo resolver devoluciones comerciales: plazos, cambios por diferencia, medios de reintegro y tratamiento de ventas fiadas o cubiertas por señas.
