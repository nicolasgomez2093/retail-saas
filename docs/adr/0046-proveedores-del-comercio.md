# ADR 0046 — Proveedores del comercio

El padrón de proveedores pertenece al comercio. Cada ficha tiene un nombre y admite correo y teléfono opcionales. El identificador queda disponible para vincular las futuras órdenes de compra. La búsqueda usa nombre, correo o teléfono y pagina en orden alfabético.

El propietario puede crear, editar, desactivar y reactivar fichas. Los encargados con al menos una sucursal activa asignada pueden consultar proveedores activos. La API verifica estos permisos y obtiene el comercio de la sesión. Las fichas inactivas se conservan para mantener su identidad cuando existan compras vinculadas.

El padrón se usa al crear [órdenes de compra por sucursal](0047-ordenes-de-compra.md).
