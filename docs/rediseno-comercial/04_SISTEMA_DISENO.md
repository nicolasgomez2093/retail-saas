# Sistema de diseño aplicado · Etapa A

Propuesta pendiente de aprobación visual y funcional. El sistema se aplica en [prototipo.html](prototipo.html), aislado de la aplicación. Los valores, ventas, identidades y permisos de la demostración son ficticios.

## Decisiones que guían la experiencia

- Una ficha progresiva reúne datos, variantes, precios, mercadería y resumen. Cada operación conserva su propio guardado y resultado.
- El catálogo prioriza nombre, clasificación, precio local y preparación; las variantes se consultan en contexto.
- La sucursal es un único contexto visible. Un carrito pendiente impide cambiarla en la simulación.
- Preparación se expresa por variante con estados confirmado, pendiente y desconocido. El POS vuelve a comprobar sus precondiciones.
- Caja e historial son accesibles desde Vender. Inicio destaca Caja, Compras e Informes dentro de la organización ya propuesta.
- Las confirmaciones se diseñan como diálogos, con consecuencias y retorno al contexto. La simulación utiliza memoria y se reinicia al recargar.

## Dirección visual

Una identidad sobria de comercio con verdes profundos, superficies cálidas y contraste tipográfico. El sidebar oscuro ancla la navegación; las superficies claras se reservan al trabajo. Los estados y los importes tienen mayor prioridad que los contenedores. Las fichas usan agrupación semántica y separadores, con resumen lateral en escritorio amplio.

| Token | Valor y uso |
| --- | --- |
| Fondo | #F4F6F2 |
| Superficie | #FFFFFF |
| Texto | #20342E |
| Texto secundario | #65756E |
| Borde | #DDE4DF |
| Acción | #20634D, texto blanco |
| Sidebar | #173E33 |
| Confirmación | #E6F1E4 con texto #2C643E |
| Advertencia | #FFF1D8 con texto #85591A |
| Error | #FAE8E7 con texto #A53439 |
| Desconocido | #E9EDF5 con texto #4D6079 |
| Foco | Contorno #A16C23 de 3 px con separación |

Tipografía nativa system-ui, disponible sin red ni instalación. Título de página 29/35 px y peso 650; sección 19/23; texto 14/21; ayuda 12/18; encabezados de tabla 10 px con espaciado moderado; importes con números tabulares. Se usa un h1 por pantalla. Escala espacial 4, 8, 12, 16, 20, 24 y 32 px. Radios 7–12 px según jerarquía.

## Composición e interacción

- Encabezado con título, explicación corta y una acción primaria. Contexto global separado de acciones de página.
- Catálogo en filas alineadas, miniatura ilustrativa, clasificación secundaria y preparación explícita. La miniatura CSS representa la prenda; la selección local de una imagen permite evaluar una fotografía propia.
- Ficha con secciones horizontales y resumen lateral. Los campos se agrupan por significado; nombre obligatorio, clasificación e imagen opcionales.
- Matriz editable con selección, SKU, solicitud explícita de código y errores por combinación. El estado existente es distinto del lote sin confirmar.
- Precios con una acción por fila. La confirmación expone lista compartida y sedes afectadas.
- Mercadería con cantidad física final, diferencia, ubicación y motivo. Revisión y confirmación preceden al resultado.
- POS con catálogo a la izquierda y carrito a la derecha. El importe final y Cobrar permanecen juntos. Móvil coloca el carrito debajo del catálogo.
- Diálogos nativos para elegir variante, confirmar operaciones y cobrar. Escape cierra cuando la operación lo permite; una escritura en curso mantiene su contexto.
- Mensajes de éxito breves en región de estado. Los errores recuperables permanecen junto a la operación y conservan los valores.

## Estados

El catálogo distingue inactivo, sin variantes, sin precio, sin disponibilidad y error de consulta. La carga tiene una composición propia. Una búsqueda vacía ofrece limpiar filtros. Una línea de precio confirma cero sin convertirlo en ausencia. Un saldo desconocido no se calcula como cero.

El cobro bloquea productos sin precio o mercadería y una caja personal no disponible. El error simulado de cobro ocurre antes del procesamiento y conserva el carrito; la recuperación de una escritura incierta en producción sigue requiriendo la clave y consulta del resultado real. El conflicto de inventario conserva la cantidad e indica volver a revisar.

## Responsive y accesibilidad

| Ancho | Decisión aplicada |
| --- | --- |
| 390 px | Navegación inferior, filas de catálogo apiladas, formularios de una columna, matriz en filas etiquetadas |
| 768 px | Navegación inferior y columnas adaptativas; carrito junto al catálogo cuando hay espacio |
| 1024 px | Sidebar compacto; ficha en una columna para evitar paneles estrechos; filas de catálogo redistribuidas |
| 1440 px | Sidebar de 218 px, ficha con resumen lateral y POS en dos paneles |
| Zoom 200% | Reflujo por ancho disponible, textos que pueden crecer y controles sin anchuras rígidas globales |

Etiquetas visibles, controles nativos, aria-selected en secciones, estado seleccionado de navegación, nombres de acciones y foco visible. Los diálogos usan showModal para contener el foco. Los controles principales tienen alturas de 42–44 px; los incrementadores de carrito son compactos. La revisión con lector de pantalla físico requiere ejecución específica.

La evidencia y los resultados realmente ejecutados se registran en el documento 05. La conformidad WCAG integral requiere pruebas adicionales, incluida tecnología de asistencia.
