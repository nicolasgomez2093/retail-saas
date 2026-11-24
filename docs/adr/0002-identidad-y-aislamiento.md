# ADR 0002 — Identidad, alcance y sesiones

El acceso usa correo normalizado y contraseña. Solo aparecen comercios activos donde ambos coinciden con un usuario activo. Si hay uno, se abre la sesión directamente. Si hay varios, un token de selección de cinco minutos permite elegir uno. La selección consume el token y verifica que la cuenta siga activa con la misma contraseña. El frontend conserva el token de selección en memoria. Los clientes anteriores pueden seguir enviando el slug como filtro opcional.

Tras autenticar, las consultas toman comercio e identidad de la sesión. Las FKs compuestas preservan el mismo tenant al asignar sucursales y vincular sesiones o roles con usuarios.

Los roles iniciales son plantillas compartidas y las asignaciones a usuarios son por tenant. El permiso `stores:read` habilita la lectura de sucursales activas asignadas explícitamente, incluso para OWNER. El permiso y las asignaciones se consultan juntos. La administración de plataforma queda pendiente de un flujo explícito de soporte y auditoría.

PostgreSQL almacena el SHA-256 de un token aleatorio de 256 bits. La cookie es HttpOnly, SameSite Strict, de host y Secure bajo HTTPS. Las sesiones expiran tras ocho horas y logout revoca la sesión en servidor. La identidad consulta el estado actual del usuario y del comercio. Login verifica contraseñas con Argon2id, ejecuta una verificación equivalente para identidades inexistentes y limita intentos globalmente por proceso y hashes concurrentes.

El entorno local permite crear un comercio demo con una contraseña elegida al ejecutar el comando. Para un despliegue se deben definir aprovisionamiento, gestión de sesiones vencidas, rate limiting compartido y políticas de recuperación y revocación.
