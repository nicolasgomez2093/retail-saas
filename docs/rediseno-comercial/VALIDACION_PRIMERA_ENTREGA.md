# Primera entrega integrada del diseño final retail

**APTO CON CONDICIONES para revisión humana.** Implementación local basada en /home/nicolas/Descargas/retail_mockup_final.html y PROMPT_CODEX_RETAIL_FINAL.md. La aceptación visual definitiva corresponde al usuario.

## Resultado

Shell con tokens del diseño, sidebar y navegación móvil, Inicio compacto con cuatro métricas y accesos, Caja y avisos operativos. La ficha incorpora Datos generales, Variantes, Precios, Mercadería, Etiquetas y Resumen. Se reutilizan altas, revisiones, matriz, lectura de precios, ajustes y cobro existentes.

El ensayo con API real creó un producto y seis variantes, asignó precios individuales incluido cero, registró seis ajustes con motivo y completó una venta. El carrito mantuvo unidades y total al navegar a Inicio y volver. La venta produjo una operación en historial, descontó una unidad de stock y abrió el ticket con impresión explícita.

## Archivos

Modificados
- [apps/web/src/app/App.tsx](../../apps/web/src/app/App.tsx)
- [apps/web/src/app/Navigation.tsx](../../apps/web/src/app/Navigation.tsx)
- [apps/web/src/features/home/Home.tsx](../../apps/web/src/features/home/Home.tsx)
- [apps/web/src/features/catalog/Catalog.tsx](../../apps/web/src/features/catalog/Catalog.tsx)
- [apps/web/src/features/pos/PointOfSale.tsx](../../apps/web/src/features/pos/PointOfSale.tsx)
- [apps/web/src/features/catalog/LabelDialog.tsx](../../apps/web/src/features/catalog/LabelDialog.tsx)

Nuevos
- [apps/web/src/app/commercial.css](../../apps/web/src/app/commercial.css)
- [apps/web/src/features/catalog/CommercialProductPanels.tsx](../../apps/web/src/features/catalog/CommercialProductPanels.tsx)

Backend, permisos, contratos y migraciones de la aplicación se conservaron. Las migraciones existentes se ejecutaron únicamente en PostgreSQL descartable para preparar fixtures.

## Contratos y garantías conservadas

- POST /products y PATCH con expectedRevision; altas individuales y matriz /products/:id/variants/preview y /batch.
- Configuración /price-lists y escritura individual /price-lists/:id/items/:variantId. El acceso a configuración y edición está autorizado por pricing:manage en el servicio actual. Se confirma alcance de lista compartida.
- Existencias, ubicaciones y ajustes existentes. La ficha monta Adjustments y conserva revisión, recuperación por clave, motivo y movimientos.
- POST /sales, consulta de historial y /sales/:id/ticket. Se conserva caja personal, precio validado y confirmación idempotente.
- Etiquetas usan JsBarcode/Code 128 y dimensiones actuales. El lote de impresión conserva la selección de códigos, límite y bloqueo de símbolos demasiado anchos.
- Errores auxiliares de red conservan los editores; una autorización rechazada oculta o bloquea su operación. Lecturas y escrituras del contexto anterior se descartan.

## Pruebas ejecutadas

| Verificación | Resultado y evidencia |
| --- | --- |
| make check | Salida 0. Incluye lint, Go, 14 pruebas Node y compilación. [Log](../../../Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658/evidencia/make-check.log) |
| GIN_MODE=release make test-db | Salida 0 en PostgreSQL 17 descartable, puerto local 33019. [Log](../../../Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658/evidencia/test-db.log) |
| Chromium con API real | Siete grupos de integración, sin excepciones JS. [Resultados](../../../Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658/evidencia/chromium-resultados.json) |
| Errores y teclado | Lectura auxiliar 503 recuperada, valor inválido conservado sin PUT, Escape y retorno de foco, Tab y precio válido confirmado. [Resultados](../../../Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658/evidencia/errores-teclado.json) |
| Responsive final | 25 mediciones sin desbordamiento horizontal y cero excepciones. [Resultados](../../../Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658/evidencia/revision-visual.json) |
| Comparación con HTML | 16 capturas de referencia en los mismos anchos; fuentes remotas bloqueadas. [Métricas](../../../Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658/evidencia/referencia-metricas.json) |

Las capturas de referencia son del HTML aprobado; las finales corresponden a la aplicación React con API real y fixtures. Se inspeccionaron las composiciones de escritorio y móvil y se corrigieron densidad de resultados, búsqueda móvil, grilla de métricas y retorno al buscador.

| Pantalla | Móvil | Tablet | Escritorio compacto | Escritorio |
| --- | --- | --- | --- | --- |
| Inicio | [390 px](../../../Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658/evidencia/final-inicio-390.png) | [768 px](../../../Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658/evidencia/final-inicio-768.png) | [1024 px](../../../Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658/evidencia/final-inicio-1024.png) | [1440 px](../../../Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658/evidencia/final-inicio-1440.png) |
| Catálogo | [390 px](../../../Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658/evidencia/final-catalogo-390.png) | [768 px](../../../Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658/evidencia/final-catalogo-768.png) | [1024 px](../../../Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658/evidencia/final-catalogo-1024.png) | [1440 px](../../../Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658/evidencia/final-catalogo-1440.png) |
| Precios | [390 px](../../../Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658/evidencia/final-precios-390.png) | [768 px](../../../Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658/evidencia/final-precios-768.png) | [1024 px](../../../Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658/evidencia/final-precios-1024.png) | [1440 px](../../../Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658/evidencia/final-precios-1440.png) |
| Mercadería | [390 px](../../../Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658/evidencia/final-mercaderia-390.png) | [768 px](../../../Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658/evidencia/final-mercaderia-768.png) | [1024 px](../../../Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658/evidencia/final-mercaderia-1024.png) | [1440 px](../../../Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658/evidencia/final-mercaderia-1440.png) |
| Resumen | [390 px](../../../Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658/evidencia/final-resumen-390.png) | [768 px](../../../Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658/evidencia/final-resumen-768.png) | [1024 px](../../../Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658/evidencia/final-resumen-1024.png) | [1440 px](../../../Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658/evidencia/final-resumen-1440.png) |
| POS | [390 px](../../../Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658/evidencia/final-pos-390.png) | [768 px](../../../Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658/evidencia/final-pos-768.png) | [1024 px](../../../Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658/evidencia/final-pos-1024.png) | [1440 px](../../../Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658/evidencia/final-pos-1440.png) |

[Cobro](../../../Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658/evidencia/cobro-1440.png) · [Venta confirmada](../../../Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658/evidencia/venta-confirmada-1440.png) · [Ticket](../../../Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658/evidencia/ticket-1440.png) · [Etiquetas](../../../Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658/evidencia/etiquetas-1440.png) · [Error de precio](../../../Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658/evidencia/error-precios-390.png) · [Emulación 200%](../../../Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658/evidencia/final-zoom200-720.png)

## Condiciones y límites concretos

- El backend no expone un conteo global de productos incompletos. Ese espacio ofrece revisión por variante desde la ficha y conserva un estado explícito sin inventar un total.
- La edición de precios conserva la semántica vigente de PUT individual, sin prometer revisión optimista ni atomicidad de varias filas.
- El contrato de ventas actual sólo permite asociar CustomerID cuando OnCredit es verdadero. La elección de cliente en una venta común requiere una adaptación específica del dominio y contrato; se conserva el flujo real actual.
- Códigos internos demasiado anchos para 60 × 40 mm siguen bloqueados. Se puede elegir otro código válido disponible. La vista previa no acredita lectura o impresión física.
- Lector de pantalla, lector físico e impresora no se ejecutaron. La impresión explícita del ticket se comprobó interceptando window.print; no se enviaron trabajos físicos.
- La navegación de Gestión mantiene las rutas y autorizaciones existentes agrupadas. Las otras áreas del prompt maestro quedan para sus entregas después de esta revisión.
- La compilación informa un chunk superior a 500 kB. El build finaliza correctamente; no se incorporaron dependencias.

## Respaldo

Ruta persistente `/home/nicolas/Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658`. Siete originales verificados por SHA-256 antes de editar y nuevamente al finalizar; home.css fue respaldado y permaneció intacto.

[Manifiesto original](../../../Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658/MANIFIESTO_SHA256.txt) · [Verificación final](../../../Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658/VERIFICACION_FINAL.txt) · [Hashes de los archivos resultantes](../../../Documentos/RespaldosRetail/DisenoFinal-Entrega1-20261123-122658/evidencia/ARCHIVOS_FINALES_SHA256.txt)

Se espera revisión humana de esta primera entrega antes de continuar.
