# Piloto local en una sucursal

El ensayo reproduce una jornada breve con un comercio de prueba, una sucursal, una caja y un puesto de venta. Usar una base separada y productos, clientes e importes de prueba. Confirmar qué navegador, lector e impresora se usarán en el puesto real. Registrar quién opera y quién verifica el cierre.

## Preparar el puesto

1. Entrar con el propietario. Crear el cajero con acceso a la sucursal de prueba. Confirmar que el cajero entra con su correo y contraseña y ve la sucursal esperada.
2. Crear una caja y una ubicación vendible. En Productos, registrar una variante con código de barras, asignarle precio de ARS 100 en la lista de la sucursal y cargar cinco unidades.
3. Si están disponibles, configurar el lector en modo teclado con Enter como sufijo y elegir el ancho real del rollo. Para la etiqueta, elegir 60 × 40 mm. Imprimir al 100 % y desactivar encabezados y pies. Comprobar que la etiqueta se lea sin agregar dos veces la misma unidad.

## Ensayar la jornada

| Paso | Resultado esperado |
|---|---|
| En Caja, abrir con ARS 100 y registrar un ingreso de ARS 25 y un egreso de ARS 10 con motivo. | Efectivo esperado de ARS 115. Los movimientos quedan en la caja de la sucursal. |
| En Vender, escanear la prenda y registrar ARS 40 en efectivo y ARS 60 por QR. Ingresar ARS 50 de efectivo recibido. | Venta de ARS 100, vuelto de ARS 10 y existencias de cuatro unidades. El efectivo esperado pasa a ARS 155. |
| Abrir el ticket y recuperarlo desde Ventas. Si hay impresora, imprimirlo en el ancho del rollo. | Mismo número, prenda, importes y medios. Verificar márgenes, legibilidad y corte físico. |
| En Caja, hacer el arqueo con ARS 150 contados. | Cierre con ARS 155 esperados, ARS 150 contados y diferencia de −ARS 5. En Vender, el puesto solicita abrir una sesión para una nueva venta. |
| Exportar el reporte del día desde Inicio. | El listado y el CSV incluyen la venta de la sucursal y fecha seleccionadas. El reporte muestra el margen según los costos informados. |

Si la aplicación indica que el resultado de una venta quedó incierto, conservar la pantalla y usar **Reintentar confirmación** al recuperar la conexión. Ante un saldo descuadrado o una operación duplicada, interrumpir esa prueba y registrar sucursal, caja, referencia, hora, pasos y resultado observado. Guardar evidencia con datos de prueba, evitando contraseñas y cookies.

## Probar en teléfonos

Hacer el recorrido de la jornada en Safari para iPhone y Chrome para Android, con una instancia y un comercio de prueba accesibles desde el teléfono. Registrar modelo, versión del sistema y del navegador, orientación y resultado por paso.

- Entrar con el teclado del teléfono. Abrir el menú, cambiar de sucursal si hay dos disponibles y navegar a Caja y Vender. Verificar que el menú se cierre al elegir una sección.
- Cargar la prenda y confirmar el pago mixto. Con el teclado abierto, comprobar que se pueda llegar a los importes, leer el total y tocar la confirmación. Revisar que el selector de medios e importes siga siendo usable al girar el teléfono.
- Volver a abrir el ticket desde Ventas. En vertical, comprobar que se pueda leer y cerrar sin desplazamiento lateral de toda la página. Completar el arqueo desde Caja.

El ensayo automatizado en Chrome usó interacción táctil a 390 px y revisó el ticket a 320 px, con API real y PostgreSQL temporal. Queda por ejecutar en teléfonos físicos. [Resultados del ensayo táctil](frontend-redesign/TEST_PLAN.md).

## Ensayar recuperación

Tomar un respaldo de PostgreSQL del comercio de prueba y restaurarlo en una base vacía distinta. Usar credenciales y destinos de ensayo. Volver a abrir el ticket, el stock y la sesión cerrada consultando una instancia de la API conectada exclusivamente a la restauración. Comparar identificadores, importes, saldos y movimientos con los del origen, y anotar cuánto tardó el ensayo.

El ensayo local del 20 de noviembre de 2026 repitió el circuito de Caja y Vender desde Chrome. Restauró un respaldo en una segunda base temporal y verificó por la API el mismo ticket, las cuatro unidades físicas, los tres movimientos de efectivo y el arqueo de ARS 155 esperados y ARS 150 contados. Estos resultados corresponden a una base chica y no establecen tiempos de recuperación para producción.

## Cierre del ensayo

Registrar resultado por paso, modelo de lector e impresora si se usaron y cualquier diferencia. La validación en el puesto real queda pendiente del lector y la impresora, la salida audible con lector de pantalla cuando corresponda y la revisión del entorno operativo elegido. La evidencia del frontend y los recorridos con bases temporales están en [Plan y resultados de pruebas](frontend-redesign/TEST_PLAN.md).
