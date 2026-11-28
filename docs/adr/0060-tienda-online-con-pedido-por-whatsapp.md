# ADR 0060 — Tienda online con pedido por WhatsApp

## Contexto

Los comercios quieren mostrar sus productos en internet y recibir pedidos sin armar otra tienda ni cargar el catálogo dos veces. El catálogo, las fotos, los precios por sucursal y el stock vendible ya están en el sistema. El cobro online y el envío no están decididos, así que el pedido tiene que cerrarse por un canal que el local ya usa.

## Decisión

- **Una tienda por comercio**, en `/tienda/<comercio>`, con el identificador corto (`slug`) del comercio.
  - La configura el dueño (`storefront:manage`) en «Tienda online», una opción propia del menú principal (`#/tienda-online`).
  - Se guarda en `storefronts` (migración 048).
- **Una sola sucursal publica.** Sus precios (lista asignada) y su stock vendible (ubicaciones vendibles activas, menos reservas) son los que ve el cliente.
- **Qué se publica:**
  - Cada producto activo con foto y con al menos una variante activa con precio en esa sucursal.
  - El comercio puede ocultar cualquier producto (`products.storefront_hidden`).
  - **Un producto es una sola ficha:** talles y colores se eligen adentro, no hay una tarjeta por variante.
  - Los productos agotados se muestran con la etiqueta «Agotado», salvo que el comercio elija ocultarlos.
- **Qué ve el cliente:**
  - Nombre, descripción, marca, categoría, fotos y precio.
  - Por cada talle y color, solo si está disponible.
  - **Nunca** cantidades, SKU, costos, ubicaciones ni identificadores del comercio.
- **API pública** (`/api/v1/public/storefronts/...`):
  - Sin sesión ni cookies.
  - Límite por dirección del cliente: 10 consultas por segundo, con ráfaga de 60.
  - Una tienda inexistente, desactivada o de un comercio o sucursal inactivos responde siempre 404 igual, para no revelar cuáles existen.
- **Fotos reducidas:**
  - Las fotos originales pueden tener hasta 4096 px.
  - Se sirven reducidas a 480 px (tarjetas) y a 1080 px (ficha), generadas en el servidor con la biblioteca estándar.
  - Las copias reducidas se guardan en memoria hasta 64 MB.
  - El navegador las puede guardar una hora.
- **Carrito:**
  - Queda en el navegador del cliente (`localStorage`), con un tope de 20 unidades por variante.
  - Al abrirlo se vuelven a consultar los precios y la disponibilidad. Lo agotado o retirado se marca y no entra en el pedido.
- **Pedido:**
  - «Enviar por WhatsApp» abre `wa.me` con el pedido escrito: productos con talle y color, cantidades, subtotales, total, nombre y comentario opcionales, y el enlace a la tienda.
  - No se guarda ningún pedido, ni se reserva stock, ni se mueve la caja.
  - El local confirma y registra la venta como siempre.
- **Página aparte:**
  - La tienda es otra página (`tienda.html`) con su propio código. El cliente no descarga el sistema interno: unos 70 KB comprimidos contra unos 230 KB.
  - Caddy la sirve para `/tienda/*`.
  - Está diseñada primero para el celular: grilla de dos columnas, acciones al alcance del pulgar, botón fijo del carrito y del pedido.
  - En tablet la ficha pasa a dos columnas (foto y opciones) y en la computadora el carrito muestra el resumen y el botón de WhatsApp fijos al costado.
  - Portada con el nombre del comercio, el mensaje de bienvenida y los tres pasos para comprar.
  - Botón «Consultas» y enlace «¿Dudas con el talle?» que abren WhatsApp con un mensaje inicial, sin pasar por el carrito.
  - Los talles conocidos se ordenan solos (XS…XXL y numéricos); si hay alguno que no reconoce, se respeta el orden del catálogo.
  - Si un color no tiene muestra cargada, se usa una para los nombres comunes (Negro, Celeste, Bordó…); si no la reconoce, va sin muestra.

## Consecuencias

- Entre que el cliente arma el carrito y el local lo lee, el stock puede cambiar. Por eso el mensaje y la tienda aclaran que el local confirma precio, stock y entrega.
- Un producto sin foto no aparece en la tienda. La pantalla «Tienda online» lo marca «Sin foto», con un enlace al catálogo para cargarla.
- La caché de fotos es por proceso: un reinicio la vacía y se vuelve a generar a pedido.
- Quedan para más adelante:
  - Registrar el pedido web en el sistema (para convertirlo en reserva).
  - Dominio propio por comercio.
  - Más de una sucursal publicando.
  - Cobro online.
