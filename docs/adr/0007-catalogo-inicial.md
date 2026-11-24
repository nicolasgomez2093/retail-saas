# ADR 0007 — Catálogo inicial

El catálogo pertenece al comercio. Los roles operativos pueden consultar el catálogo activo y el propietario puede administrar productos y variantes, incluso consultar productos inactivos. Las FKs compuestas vinculan cada variante y código con su comercio. La API deriva el comercio de la sesión.

El producto contiene nombre, descripción y estado. La variante contiene SKU y atributos opcionales de talle y color. El SKU es único por comercio sin distinguir mayúsculas. Una variante puede cargarse antes de tener un código de barras. Al crearla con códigos, el primero es el principal. Los códigos se guardan como texto y se buscan con coincidencia exacta. Este paso acepta letras ASCII, dígitos, guion, guion bajo y punto, con una letra o dígito inicial. El sistema conserva ceros iniciales.

Las variantes y sus códigos se crean juntos. Un conflicto revierte la operación. Desactivar un producto conserva sus variantes y códigos y los retira de la búsqueda operativa. Las entregas de categorías y marcas se documentan en ADR 0009. La edición de variantes se documenta en ADR 0010. Las etiquetas se documentan en ADR 0011 y las fotos de productos en ADR 0013. La resolución de precios se rige por ADR 0006.
