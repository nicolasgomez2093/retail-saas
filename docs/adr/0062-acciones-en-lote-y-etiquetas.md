# ADR 0062 — Acciones en lote del catálogo y ajustes de etiquetas

## Contexto

Los comercios cambian precios por grupos («todas las remeras un 10 % más») y reimprimen etiquetas de varios productos a la vez. Hasta ahora había que ir prenda por prenda. El local usa una impresora térmica de etiquetas X-Printer XP-470B: hasta 108 mm de ancho, se maneja por driver.

## Decisión

- **Selección en el catálogo:**
  - Cada producto del listado tiene una casilla, y la cabecera tiene otra para elegir los de la página.
  - La selección sobrevive a búsquedas y páginas, con un tope de 200 productos.
  - Mientras haya productos elegidos, una barra fija ofrece «Imprimir etiquetas», «Cambiar precios» y «Quitar selección».
- **Etiquetas en lote:**
  - Juntan todas las variantes activas con código de barras de los productos elegidos, en una sola tirada (hasta 250 etiquetas).
  - Sale una etiqueta por variante; la cantidad de cada una se puede cambiar o usar el stock vendible.
- **Tamaños de etiqueta:** 60×40, 50×30, 50×25, 40×30, 100×50 y 100×40 mm.
  - La página de impresión toma el tamaño elegido.
  - El navegador recuerda el último tamaño usado.
  - El driver de la impresora tiene que tener cargado el mismo tamaño de papel.
- **Precio en las etiquetas:**
  - Es un ajuste del comercio (`tenants.label_show_price`, migración 049), en Configuración › Etiquetas.
  - Lo lee cualquier usuario y lo cambia quien tiene `catalog:manage`.
  - Sin precio, la etiqueta lleva nombre, talle, color y código de barras.
- **Cambio masivo de precios:** `POST /api/v1/price-lists/{id}/adjustments`.
  - Sube o baja en porcentaje o en pesos, con redondeo opcional (a $10, $100 o $1.000).
  - Siempre se muestra primero cómo quedan los precios (`apply=false`); se aplica solo lo que se vio. Cualquier cambio en el formulario borra la vista previa.
  - Cambia solo variantes activas que ya tienen precio en la lista. Las que no tienen precio se informan y no se inventan.
  - Las cuentas se hacen en centavos enteros y los porcentajes se redondean al centavo, con la mitad hacia arriba.
  - Si algún precio quedaría negativo, se rechaza todo el cambio.
  - Se escribe en una sola transacción, con un evento de auditoría por variante.
  - Requiere `pricing:manage`.
- **Fiados:**
  - El detalle de abonos se abre debajo del fiado, con tres números: fiado, abonado y saldo.
  - El formulario de abono va en una sola fila.
  - El desglose de abonos es desplegable.

## Consecuencias

- La impresión sigue usando el diálogo del navegador. Imprimir directo en el idioma propio de la impresora (TSPL) queda para más adelante, si hiciera falta más velocidad.
- Un cambio masivo no se deshace con un botón: se corrige con otro cambio. Por eso la vista previa es obligatoria.
