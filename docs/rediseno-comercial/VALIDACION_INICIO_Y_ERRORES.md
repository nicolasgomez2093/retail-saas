# Corrección de Inicio y gestión de errores — primera entrega

Implementación realizada y validada técnicamente el 23/11/2026. **Pendiente de revisión y aceptación visual humana.** No se inicia la segunda entrega.

## Cambios entregados

Inicio se reconstruyó a partir de `/home/nicolas/Descargas/retail_mockup_final.html`: encabezado compacto, cuatro indicadores, venta en curso solo cuando existe carrito, accesos rápidos en una franja, Últimas ventas a la izquierda y Caja/Requiere atención a la derecha. En móvil las secciones se apilan. Se reutilizaron paleta, radios, proporciones y jerarquía del HTML; se corrigieron el contraste de la acción del estado vacío y los saltos de los importes a 1024 px detectados en la inspección de capturas.

Los datos siguen siendo reales: ventas del día, pagos efectivamente registrados sin sumar deuda pendiente, alertas según los mínimos existentes y acceso a revisión de preparación sin inventar cantidades. Fiados y compras pendientes se reúnen en Requiere atención en lugar de crear tarjetas adicionales. Los importes del HTML son ficticios y no se copian a la aplicación.

Categorías y marcas vacías se identifican en cada selector. Un fallo de lectura muestra un estado no disponible y recuperación discreta junto al campo, sin banners ni afirmaciones falsas de que la lista está vacía. La clasificación opcional ya no impide guardar un producto por un fallo auxiliar. Se preservan selecciones existentes y valores locales.

El POS consulta caja y ubicaciones de manera independiente. Diferencia carga, vacío válido, error y permiso denegado, conserva datos válidos durante fallos temporales y bloquea las operaciones hasta verificar el contexto necesario. Las respuestas obsoletas se descartan por intención de lectura, incluidas A→B→A y errores tardíos. Los tickets pendientes se invalidan al salir de la vista.

Los errores HTTP 401/403/5xx se presentan con textos comprensibles sin detalles de infraestructura. Se mantienen estados HTTP, códigos de máquina, validaciones y conflictos comerciales. No se cambiaron contratos ni reglas del backend. Los errores de escritura e incertidumbre se conservan junto a la operación, con datos locales y sin reintentos automáticos. Los avisos generales se descartan al navegar y el aviso de éxito del catálogo es transitorio.

## Archivos

Modificados:

- `apps/web/src/features/home/Home.tsx`
- `apps/web/src/app/commercial.css`
- `apps/web/src/app/App.tsx`
- `apps/web/src/features/catalog/Catalog.tsx`
- `apps/web/src/features/catalog/Classifications.tsx`
- `apps/web/src/shared/api/client.ts`
- `apps/web/src/features/pos/PointOfSale.tsx`
- `apps/web/src/features/inventory/InventoryVariantPicker.tsx`
- `apps/web/src/features/inventory/Existences.tsx`
- `apps/web/src/features/inventory/Inventory.tsx`

Creados:

- `apps/web/src/shared/lib/httpErrorMessage.ts`
- `apps/web/http-error-message.test.js`
- Este informe.

No se modificaron backend, permisos, esquema, migraciones, dependencias ni configuración. No se usó Git ni se desplegó. `home.css` se respaldó y permanece intacto.

## Pruebas de aceptación

Entorno local: API real, Vite y Chromium/CDP; PostgreSQL 17 descartable en puerto 33029, base `retail_ux`, exclusivamente fixtures sintéticos. Para simular fallos, CDP interceptó respuestas específicas; los demás requests utilizaron la API real. No se provocaron fallos sobre bases comerciales.

| Caso | Resultado ejecutado | Evidencia |
|---|---|---|
| 1. Categorías vacías | Lista vacía real; selector Sin categorías y cero banners | `aceptacion-inicio-errores.json` |
| 2. Marcas vacías | Lista vacía real; selector Sin marcas y cero banners | Mismo JSON |
| 3. Categorías/marcas 503 | Formulario utilizable; alta real confirmada con clasificación opcional; recuperación al volver | Mismo JSON; `clasificaciones-503-*` |
| 4. Sucursal sin ubicaciones | Sucursal real de fixture; selector vacío y venta bloqueada | Mismo JSON; `sucursal-sin-ubicaciones-390.png` |
| 5. Cambio rápido de sucursal | A→B→A descarta respuesta anterior en Inicio; POS conserva ubicación actual ante respuestas obsoletas 200, 403 y 503 | `aceptacion-inicio-errores.json`, `pos-contexto.json` |
| 6. Navegación repetida | Tres vueltas Inicio→Productos→Vender→Inventario→Inicio, sin alertas arrastradas | `aceptacion-inicio-errores.json` |
| 7. Fallo de venta | 503 controlado, resultado incierto visible y carrito conservado; un solo POST | Mismo JSON; `venta-incierta-carrito-conservado-390.png` |
| 8. Fallo de precio | PUT 503 conserva 12345,67 y el error al volver; un solo intento | `errores-teclado.json`; `precio-escritura-incierta-390.png` |
| 9. Permisos | Rol CASHIER real: POST de producto devuelve 403, acción de alta ausente. Lectura 403 bloquea Inventario/POS sin detalles internos | `permisos-reales.json`, `aceptacion-inicio-errores.json` |
| 10. Inicio y referencia | Capturas reales e inspección visual en 390/768/1024/1440, con/sin actividad; composición y anchos comprobados | `revision-inicio.json`, comparaciones inferiores |

También se repitieron creación de producto, matriz de seis variantes, SKU, precios individuales incluido cero, ajustes de stock, carrito/retomar, venta en efectivo, ticket, fiado con cliente y deuda, etiquetas con acción explícita y recuperación de errores. El fiado de prueba registró deuda de 10,00 sin incrementar el efectivo esperado de caja. La venta normal se confirmó una sola vez y redujo stock según su cantidad.

`ticket-contexto.json` verifica apertura del ticket existente, impresión únicamente por acción explícita y descarte de una respuesta tardía 503 después de navegar a Inicio. `errores-teclado.json` verifica importe inválido sin PUT, Escape y retorno de foco al control de guardar, Tab y recuperación de lectura.

| Comando | Resultado |
|---|---|
| `make check` | Código 0; comprobaciones Go, lint, 18 pruebas Node y compilación satisfactorias |
| `GIN_MODE=release TEST_DATABASE_URL=<base descartable> make test-db` | Código 0 |

Los logs son `make-check.log` y `test-db.log`. Los scripts reproducibles están conservados junto a los JSON. Las ejecuciones preliminares detectaron selectores/esperas del script que debieron ajustarse al montaje y a controles móviles; las aserciones finales se ejecutaron nuevamente. No se cuentan intentos incompletos como aprobaciones.

## Capturas y comparación

Evidencia completa: [/home/nicolas/Documentos/RespaldosRetail/Retail-Inicio-Errores-20261123-142041/evidencia](/home/nicolas/Documentos/RespaldosRetail/Retail-Inicio-Errores-20261123-142041/evidencia).

| Ancho | Inicio con actividad | Sin actividad | HTML vs implementación |
|---|---|---|---|
| 390 px | [Captura](/home/nicolas/Documentos/RespaldosRetail/Retail-Inicio-Errores-20261123-142041/evidencia/inicio-final-390.png) | [Captura](/home/nicolas/Documentos/RespaldosRetail/Retail-Inicio-Errores-20261123-142041/evidencia/inicio-sin-actividad-390.png) | [Lado a lado](/home/nicolas/Documentos/RespaldosRetail/Retail-Inicio-Errores-20261123-142041/evidencia/comparacion-inicio-390.png) |
| 768 px | [Captura](/home/nicolas/Documentos/RespaldosRetail/Retail-Inicio-Errores-20261123-142041/evidencia/inicio-final-768.png) | [Captura](/home/nicolas/Documentos/RespaldosRetail/Retail-Inicio-Errores-20261123-142041/evidencia/inicio-sin-actividad-768.png) | [Lado a lado](/home/nicolas/Documentos/RespaldosRetail/Retail-Inicio-Errores-20261123-142041/evidencia/comparacion-inicio-768.png) |
| 1024 px | [Captura](/home/nicolas/Documentos/RespaldosRetail/Retail-Inicio-Errores-20261123-142041/evidencia/inicio-final-1024.png) | [Captura](/home/nicolas/Documentos/RespaldosRetail/Retail-Inicio-Errores-20261123-142041/evidencia/inicio-sin-actividad-1024.png) | [Lado a lado](/home/nicolas/Documentos/RespaldosRetail/Retail-Inicio-Errores-20261123-142041/evidencia/comparacion-inicio-1024.png) |
| 1440 px | [Captura](/home/nicolas/Documentos/RespaldosRetail/Retail-Inicio-Errores-20261123-142041/evidencia/inicio-final-1440.png) | [Captura](/home/nicolas/Documentos/RespaldosRetail/Retail-Inicio-Errores-20261123-142041/evidencia/inicio-sin-actividad-1440.png) | [Lado a lado](/home/nicolas/Documentos/RespaldosRetail/Retail-Inicio-Errores-20261123-142041/evidencia/comparacion-inicio-1440.png) |

Las comparaciones conservan el contenido completo de ambas capturas e identifican el HTML ficticio y la API con fixtures. El aviso de prototipo y los datos demostrativos del HTML no se incorporaron al producto. La fuente externa del HTML se bloqueó en la comparación para mantener la prueba local.

El reflujo equivalente al 200 % está registrado en `inicio-reflujo200-720.png`; es emulación de ancho de contenido, no una afirmación de prueba con software de asistencia. Se verificó ausencia de scroll horizontal y se revisaron visualmente estados vacíos, alertas, precios y la navegación inferior.

## Respaldo y límites

Respaldo persistente: [/home/nicolas/Documentos/RespaldosRetail/Retail-Inicio-Errores-20261123-142041](/home/nicolas/Documentos/RespaldosRetail/Retail-Inicio-Errores-20261123-142041). `MANIFIESTO_SHA256.json` registra rutas, tamaños y hashes de 11 originales. Todos se verificaron antes de editar y nuevamente al finalizar. `evidencia/verificacion-final.json` registra los archivos efectivamente cambiados y sus hashes finales. Los respaldos anteriores se conservaron.

- Impresora física, lector físico y lector de pantalla: **NO EJECUTADO — EQUIPO NO DISPONIBLE**. Se probaron PDF, llamadas explícitas de impresión y teclado.
- Se conserva el aviso de bundle superior a 500 kB; la compilación pasa. No se introdujeron dependencias para esta corrección.
- No quedan fallos detectados abiertos en los diez casos ejecutados. La aceptación de fidelidad visual corresponde al usuario y no se deriva de la compilación ni de las métricas de ancho.

**Entrega detenida para revisión humana. La primera entrega no se declara aprobada.**
