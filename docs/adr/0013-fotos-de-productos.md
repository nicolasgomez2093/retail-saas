# ADR 0013 — Fotos de productos

Cada producto admite hasta seis fotos. El propietario puede agregarlas, elegir la principal y quitarlas. La primera foto se elige como principal. Si se quita, se promueve la foto restante más antigua. Los cambios de la galería son transaccionales y cada foto pertenece al mismo comercio que su producto.

El detalle del catálogo entrega metadatos y el navegador obtiene el JPEG mediante un endpoint autenticado. El personal puede leer fotos de productos activos de su comercio. Los propietarios también pueden acceder a las fotos de sus productos inactivos.

La API acepta JPEG o PNG de hasta 5 MiB, 4096 píxeles por lado y 12 megapíxeles. Valida el contenido, lo guarda como JPEG sin los metadatos originales y convierte la transparencia en fondo blanco. La interfaz aplica la orientación de la imagen y reduce el lado mayor a 1600 píxeles cuando hace falta. [Especificación de ImageBitmap](https://html.spec.whatwg.org/multipage/imagebitmap-and-animations.html#imagebitmap)

Los bytes se guardan en una tabla separada de PostgreSQL para mantener su ciclo de vida en la misma transacción y reutilizar los respaldos locales. La tabla de productos y sus listados conservan solo los datos de catálogo. Si el volumen de fotos requiere almacenamiento de objetos, los identificadores y el contrato de lectura pueden conservarse al migrar los bytes.
