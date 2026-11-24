# ADR 0009 — Categorías y marcas del catálogo

El propietario puede crear y renombrar las categorías y marcas de su comercio. Los nombres se comparan sin distinguir mayúsculas y cada catálogo tiene su propio espacio de nombres. Las categorías de esta entrega son planas. Cada producto puede asociar una categoría y una marca, ambas opcionales. Los productos existentes conservan sus datos y quedan sin clasificar hasta que el propietario los edite.

Las asociaciones usan FKs compuestas por comercio. Al renombrar una categoría o marca se conserva su identificador y la asociación de los productos. En una edición de producto, un campo omitido conserva su valor y un campo nulo quita la clasificación correspondiente.

El listado combina los filtros de categoría y marca con la búsqueda de nombre o SKU. El detalle y la búsqueda por código muestran las clasificaciones asociadas.
