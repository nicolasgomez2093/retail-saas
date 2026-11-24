# ADR 0005 — Configuración de sucursales y cambios de rol

Los propietarios tienen el permiso `stores:manage` para listar, crear, editar, desactivar y reactivar sucursales del comercio. El listado administrativo incluye sucursales inactivas. El listado operativo conserva su filtro por asignación y estado activo. La creación asigna la nueva sucursal al propietario que realiza el alta, y el código es único por comercio. La desactivación conserva las asignaciones para recuperar el alcance al reactivar la sucursal.

El propietario puede reemplazar los roles de una cuenta de personal por uno de los tres roles operativos. Las cuentas de propietario se administran por un flujo separado. El encargado conserva el alcance de administración establecido en ADR 0004. Un cambio efectivo de rol revoca las sesiones de la cuenta y conserva las asignaciones salvo que se envíe un reemplazo explícito.

La API verifica el tenant autenticado y serializa los cambios junto con las escrituras de usuarios mediante un bloqueo de fila sobre el comercio. Los cambios y su auditoría se confirman juntos. Los eventos de sucursal registran el actor, la sucursal y sus datos antes y después. Los cambios de rol usan la auditoría existente de usuarios.
