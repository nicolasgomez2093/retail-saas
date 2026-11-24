# Corrección visual y UX de la primera entrega

Estado: **implementada y validada técnicamente, pendiente de aceptación humana**. Se conserva la referencia `/home/nicolas/Descargas/retail_mockup_final.html`. No se avanzó a otra entrega.

## Correcciones

- POS: filas uniformes, nombres visibles hasta dos líneas con texto completo en el título y al consultar variantes, jerarquía de precio/disponibilidad y acciones alineadas. Se eliminó el posicionamiento flotante heredado de Favorito. Selector de variantes compacto con precio y stock efectivamente consultados; Agregar permanece deshabilitado durante lecturas o ante falta de precio, stock, estado activo o información confirmada. El backend sigue autorizando cada venta.
- Precios: fila de identidad/SKU, importe actual, campo editable y Guardar en escritorio; agrupación compacta en móvil. Precio cero sigue siendo configurado. Se conservan PUT individuales, confirmación de listas compartidas, datos editados y errores.
- Resumen: identidad/SKU, estado, precio, stock vendible y preparación por variante. Distingue Lista para vender, Sin precio, Sin stock, Inactiva, Sin saldo registrado y No verificada. La indicación corresponde a las lecturas actuales y no garantiza la venta futura.
- Inicio: espaciado más compacto y accesos rápidos en una franja cuando hay ancho suficiente. Se mantienen indicadores reales y acceso a Venta en curso.
- Pestañas: desplazamiento horizontal local con flechas visibles y pestaña activa accesible al cambiar de ancho. Se conserva foco visible y no se introduce scroll horizontal del documento. Carrito lateral en escritorio y barra inferior sobre la navegación móvil.

## Archivos

Modificados:

- `apps/web/src/app/commercial.css`
- `apps/web/src/features/catalog/CommercialProductPanels.tsx`
- `apps/web/src/features/catalog/Catalog.tsx`
- `apps/web/src/features/inventory/InventoryVariantPicker.tsx`

Creados:

- `apps/web/src/features/catalog/commercialStatus.ts`
- `apps/web/commercial-status.test.js`
- Este informe.

`Home.tsx` fue respaldado y permanece idéntico. No se modificaron backend, contratos, migraciones, permisos ni dependencias. No se utilizó Git ni se desplegó.

## Ejecución y resultados

Entorno local aislado: PostgreSQL 17 descartable, base `retail_ux`, puerto 33029; API 31989, Vite 31990 y Chromium/CDP 31988. Solo fixtures sintéticos identificados como prueba. Ninguna base comercial se utilizó.

| Verificación | Resultado | Evidencia |
|---|---|---|
| `make check` | Código de salida 0; Go, verificación frontend, 16 pruebas Node y compilación satisfactorios | `make-check.log` |
| `GIN_MODE=release TEST_DATABASE_URL=<PostgreSQL descartable> make test-db` | Código de salida 0 | `test-db.log` |
| Crear producto y matriz de seis variantes | Ejecutado desde interfaz con API real | `chromium-resultados.json`, `alta-preparacion.log` |
| Precios individuales, incluido cero; ajustes válidos de stock | Satisfactorio | `chromium-resultados.json` |
| Carrito, Inicio, retomar, cobro en efectivo y ticket | Satisfactorio; una venta y reducción de stock comprobadas | `venta-ticket.log`, `chromium-resultados.json` |
| Fiado sin cliente y con cliente | Exige cliente; venta real genera deuda 10,00 y no aumenta efectivo esperado | `ux-resultados.json` |
| Etiquetas | PDF con código real y ceros iniciales; invocación de impresión solo tras acción explícita | `ux-resultados.json`, `etiquetas.pdf` |
| Precio ausente/cero, stock cero, inactiva y fallo de consulta | Resumen distingue todos los estados; POS impide agregar las no disponibles | `ux-resultados.json`, capturas `resumen-estados-*` y `pos-estados-*` |
| Error 503 de lectura y recuperación | Fallo controlado de consulta, conservando identidad; reintento específico satisfactorio | `errores-teclado.json` |
| Importe inválido | Valor conservado y cero PUT hasta corrección | `errores-teclado.json` |
| Modal, Escape, retorno de foco y Tab | Satisfactorio | `errores-teclado.json` |
| URL inválida | Error conservado, sin GET de productos | `chromium-resultados.json` |
| Responsive | 390, 768, 1024 y 1440 px; sin overflow; acciones sin solapamiento; pestaña activa visible | `revision-visual.json`, `ux-resultados.json` |

Se registraron 16 grupos de aserciones funcionales Chromium y 78 mediciones de ancho en los archivos JSON finales. No se detectaron excepciones JavaScript en esos recorridos. Las primeras ejecuciones adicionales detectaron esperas insuficientes del script y una superposición real de Favorito; se corrigieron y se repitieron los casos. Los scripts conservados incluyen las esperas y aserciones finales.

## Capturas comparables

Todas se encuentran en [/home/nicolas/Documentos/RespaldosRetail/Retail-Correccion-Visual-20261123-130423/evidencia](/home/nicolas/Documentos/RespaldosRetail/Retail-Correccion-Visual-20261123-130423/evidencia). Se inspeccionaron visualmente las composiciones, además de medir anchos y geometría.

| Pantalla | 390 | 768 | 1024 | 1440 |
|---|---|---|---|---|
| Inicio | [390](/home/nicolas/Documentos/RespaldosRetail/Retail-Correccion-Visual-20261123-130423/evidencia/final-inicio-390.png) | [768](/home/nicolas/Documentos/RespaldosRetail/Retail-Correccion-Visual-20261123-130423/evidencia/final-inicio-768.png) | [1024](/home/nicolas/Documentos/RespaldosRetail/Retail-Correccion-Visual-20261123-130423/evidencia/final-inicio-1024.png) | [1440](/home/nicolas/Documentos/RespaldosRetail/Retail-Correccion-Visual-20261123-130423/evidencia/final-inicio-1440.png) |
| POS listado | [390](/home/nicolas/Documentos/RespaldosRetail/Retail-Correccion-Visual-20261123-130423/evidencia/final-pos-listado-390.png) | [768](/home/nicolas/Documentos/RespaldosRetail/Retail-Correccion-Visual-20261123-130423/evidencia/final-pos-listado-768.png) | [1024](/home/nicolas/Documentos/RespaldosRetail/Retail-Correccion-Visual-20261123-130423/evidencia/final-pos-listado-1024.png) | [1440](/home/nicolas/Documentos/RespaldosRetail/Retail-Correccion-Visual-20261123-130423/evidencia/final-pos-listado-1440.png) |
| POS variantes y carrito | [390](/home/nicolas/Documentos/RespaldosRetail/Retail-Correccion-Visual-20261123-130423/evidencia/final-pos-390.png) | [768](/home/nicolas/Documentos/RespaldosRetail/Retail-Correccion-Visual-20261123-130423/evidencia/final-pos-768.png) | [1024](/home/nicolas/Documentos/RespaldosRetail/Retail-Correccion-Visual-20261123-130423/evidencia/final-pos-1024.png) | [1440](/home/nicolas/Documentos/RespaldosRetail/Retail-Correccion-Visual-20261123-130423/evidencia/final-pos-1440.png) |
| Precios | [390](/home/nicolas/Documentos/RespaldosRetail/Retail-Correccion-Visual-20261123-130423/evidencia/final-precios-390.png) | [768](/home/nicolas/Documentos/RespaldosRetail/Retail-Correccion-Visual-20261123-130423/evidencia/final-precios-768.png) | [1024](/home/nicolas/Documentos/RespaldosRetail/Retail-Correccion-Visual-20261123-130423/evidencia/final-precios-1024.png) | [1440](/home/nicolas/Documentos/RespaldosRetail/Retail-Correccion-Visual-20261123-130423/evidencia/final-precios-1440.png) |
| Resumen | [390](/home/nicolas/Documentos/RespaldosRetail/Retail-Correccion-Visual-20261123-130423/evidencia/final-resumen-390.png) | [768](/home/nicolas/Documentos/RespaldosRetail/Retail-Correccion-Visual-20261123-130423/evidencia/final-resumen-768.png) | [1024](/home/nicolas/Documentos/RespaldosRetail/Retail-Correccion-Visual-20261123-130423/evidencia/final-resumen-1024.png) | [1440](/home/nicolas/Documentos/RespaldosRetail/Retail-Correccion-Visual-20261123-130423/evidencia/final-resumen-1440.png) |
| Error de precios | [390](/home/nicolas/Documentos/RespaldosRetail/Retail-Correccion-Visual-20261123-130423/evidencia/error-precios-390.png) | [768](/home/nicolas/Documentos/RespaldosRetail/Retail-Correccion-Visual-20261123-130423/evidencia/error-precios-768.png) | [1024](/home/nicolas/Documentos/RespaldosRetail/Retail-Correccion-Visual-20261123-130423/evidencia/error-precios-1024.png) | [1440](/home/nicolas/Documentos/RespaldosRetail/Retail-Correccion-Visual-20261123-130423/evidencia/error-precios-1440.png) |

También se guardaron `resumen-estados-*`, `pos-estados-*`, `cobro-fiado-1440.png` y `etiquetas-validas-1440.png`. La prueba de reflujo equivalente a 200 % usa 720 píxeles CSS frente al escritorio de 1440: `reflujo200-inicio-720.png`, `reflujo200-precios-720.png`, `reflujo200-resumen-720.png` y `final-zoom200-720.png`. No se presenta como una prueba física del zoom del navegador del usuario.

## Respaldo e integridad

Respaldo persistente: [/home/nicolas/Documentos/RespaldosRetail/Retail-Correccion-Visual-20261123-130423](/home/nicolas/Documentos/RespaldosRetail/Retail-Correccion-Visual-20261123-130423). Antes de editar se copiaron los cinco archivos existentes previstos y se verificaron sus hashes. Se verificaron nuevamente los cinco originales al terminar; coincidencia SHA-256 completa. Los dos archivos nuevos no tenían versión anterior.

- `MANIFIESTO_SHA256.txt`: originales, rutas y tamaños.
- `evidencia/verificacion-respaldo-final.json`: comprobación final y hashes de los archivos entregados.
- Scripts Chromium y ejecutores conservados en `evidencia/retail-ux-*`.

## Límites y revisión

- Impresora, lector físico y lector de pantalla: **NO EJECUTADO — EQUIPO NO DISPONIBLE**. Se verificaron acciones explícitas, PDF, teclado, Escape y retorno de foco.
- El compilador mantiene el aviso de tamaño de bundle superior a 500 kB; la compilación finaliza correctamente. No se cambió infraestructura para esta corrección visual.
- No quedan defectos visuales detectados abiertos dentro de este alcance. La aprobación visual definitiva corresponde al usuario.

**Veredicto: APTO PARA REVISIÓN HUMANA.** Se detiene el trabajo en esta entrega.
