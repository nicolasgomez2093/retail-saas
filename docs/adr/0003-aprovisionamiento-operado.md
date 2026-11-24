# ADR 0003 — Aprovisionamiento por operador

El alta inicial se ejecuta con `retail provision-tenant` desde un entorno con acceso a PostgreSQL. Crea en una transacción el comercio, su primera sucursal, el propietario, el rol OWNER y la asignación de sucursal. El slug único arbitra altas concurrentes y los reintentos de un alta confirmada devuelven un conflicto. El comando devuelve los tres UUID al operador después del commit. El alta demo usa el mismo caso de uso.

El operador especifica la zona horaria y la moneda del comercio. La moneda se normaliza y valida por formato. La zona se verifica con la base IANA de Go. El correo usa la misma validación y normalización que login. La contraseña se recibe por entrada estándar y se persiste como hash Argon2id.

El acceso de operador se controla mediante acceso al entorno y a la base. Todavía quedan por definir la entrega de la credencial inicial y la administración delegada. Para habilitar escrituras desde la API, se deben definir permisos independientes por acción, derivar el tenant de la sesión, comprobar el alcance para cada recurso y auditar los cambios de roles y sucursales junto con la escritura. Consultar las preguntas abiertas antes de asignar permisos de administración a roles existentes.
